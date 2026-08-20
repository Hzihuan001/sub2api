package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cursor mints two different JWTs for the same account and only one of them can
// drive a conversation:
//
//   - type "web": what the browser stores in the WorkosCursorSessionToken
//     cookie. It authenticates the dashboard and unary RPCs such as
//     AvailableModels, but StreamUnifiedChatWithTools rejects it with
//     ERROR_NOT_LOGGED_IN.
//   - type "session": what the desktop client obtains through the
//     loginDeepControl PKCE handshake. This is the credential chat requires.
//
// ExchangeWebSession upgrades the former into the latter without a browser, by
// replaying the handshake the login page performs and authorizing it with the
// web cookie the operator already pasted.
const (
	// TokenTypeWeb is the "type" claim of a browser cookie token.
	TokenTypeWeb = "web"
	// TokenTypeSession is the "type" claim of a client (chat-capable) token.
	TokenTypeSession = "session"
)

const (
	// WebsiteBaseURL hosts the browser login pages and the cookie-authenticated
	// deep-link approval API (api2.cursor.sh does not serve either).
	WebsiteBaseURL = "https://www.cursor.com"

	// EndpointLoginDeepCallbackControl approves a pending loginDeepControl
	// handshake from the server side, authenticated by the
	// WorkosCursorSessionToken cookie instead of a human clicking
	// "Yes, Log In".
	//
	// Contract (confidence: high — matches JiuZ-Chn/Cursor-To-OpenAI
	// src/routes/cursor.js, whose README documents exactly this
	// "cookie in, client JWT out" API, and is consistent with the browser
	// flow described by cursor-client2login and the Cursor SDK's
	// login-flow.d.ts):
	//
	//	POST https://www.cursor.com/api/auth/loginDeepCallbackControl
	//	Cookie: WorkosCursorSessionToken=<userId%3A%3AwebJWT>
	//	Content-Type: application/json
	//	{"uuid":"<handshake uuid>","challenge":"<pkce challenge>"}
	//
	// The response body is not used: /auth/poll is the source of truth for
	// whether the handshake completed.
	EndpointLoginDeepCallbackControl = "/api/auth/loginDeepCallbackControl"

	// SessionCookieName is the browser cookie that carries the web session.
	SessionCookieName = "WorkosCursorSessionToken"

	// cookieSeparatorEncoded is how a browser stores the "::" between the user
	// id and the JWT inside the cookie value.
	cookieSeparatorEncoded = "%3A%3A"
)

// Defaults for the exchange. The approval call impersonates a browser (it is a
// website API) while the poll impersonates the desktop client (it is the
// client's own endpoint), matching the reference implementations.
const (
	defaultBrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/132.0.6834.210 Safari/537.36"
	defaultClientUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Cursor/" + DefaultClientVersion + " Chrome/132.0.6834.210 Electron/34.3.4 Safari/537.36"

	defaultPollAttempts = 30
	defaultPollInterval = time.Second

	// maxAuthBody bounds an auth response body; these are small JSON documents.
	maxAuthBody = 1 << 20
)

var (
	// ErrWebSessionUnauthorized reports that the WorkosCursorSessionToken was
	// rejected: it is expired, revoked, or was never a valid web session.
	ErrWebSessionUnauthorized = errors.New("cursor: web session token is unauthorized")

	// ErrDeepLoginPending reports that the handshake was never completed within
	// the polling budget. For the automatic path this means the approval did
	// not take effect; for the manual path it means nobody clicked approve.
	ErrDeepLoginPending = errors.New("cursor: deep-link login has not completed")

	// ErrWebSessionNotUpgraded reports that the exchange returned another web
	// token instead of a client one. Storing it would silently reproduce the
	// ERROR_NOT_LOGGED_IN failure on the next chat turn.
	ErrWebSessionNotUpgraded = errors.New("cursor: exchange returned a web token, not a client token")
)

// HTTPDoer is the minimal http.Client surface the exchange needs, so callers
// can inject a proxy-bound or instrumented client (*http.Client satisfies it).
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenClaims are the unsigned JWT claims that decide how a Cursor credential
// may be used. Signatures are never verified here: the upstream is the only
// authority on validity, and we only need to route the token correctly.
type TokenClaims struct {
	Subject   string
	UserID    string
	Type      string
	Scope     string
	Audience  string
	Issuer    string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// IsWeb reports whether these claims describe a browser cookie token.
func (c TokenClaims) IsWeb() bool { return c.Type == TokenTypeWeb }

// ParseTokenClaims decodes the claims of a bare JWT or a "userId::JWT" cookie.
// ok is false when the input is not a parseable JWT.
func ParseTokenClaims(raw string) (claims TokenClaims, ok bool) {
	token, uid := ParseToken(raw)
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return TokenClaims{}, false
	}
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		return TokenClaims{}, false
	}
	var body struct {
		Sub   string `json:"sub"`
		Type  string `json:"type"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Iss   string `json:"iss"`
		Iat   int64  `json:"iat"`
		Exp   int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return TokenClaims{}, false
	}
	claims = TokenClaims{
		Subject:  strings.TrimSpace(body.Sub),
		UserID:   uid,
		Type:     strings.ToLower(strings.TrimSpace(body.Type)),
		Scope:    strings.TrimSpace(body.Scope),
		Audience: strings.TrimSpace(body.Aud),
		Issuer:   strings.TrimSpace(body.Iss),
	}
	if body.Iat > 0 {
		claims.IssuedAt = time.Unix(body.Iat, 0)
	}
	if body.Exp > 0 {
		claims.ExpiresAt = time.Unix(body.Exp, 0)
	}
	return claims, true
}

// TokenType returns the lowercased "type" claim ("web", "session", ...) or ""
// when the credential is not a parseable JWT (a crsr_ API key, for instance).
func TokenType(raw string) string {
	claims, ok := ParseTokenClaims(raw)
	if !ok {
		return ""
	}
	return claims.Type
}

// IsWebSessionToken reports whether a credential is a browser cookie token,
// which cannot drive StreamUnifiedChatWithTools and must be upgraded first.
func IsWebSessionToken(raw string) bool {
	return TokenType(raw) == TokenTypeWeb
}

// NormalizeSessionCookie renders a credential as the WorkosCursorSessionToken
// cookie *value* the website expects: "userId%3A%3AJWT" when the user id is
// known (either given explicitly or recoverable from the sub claim), otherwise
// the bare JWT. It accepts every shape an operator can paste: the raw browser
// value, the decoded "userId::JWT" form, or a bare JWT.
func NormalizeSessionCookie(raw string) string {
	token, uid := ParseToken(raw)
	if token == "" {
		return ""
	}
	if uid == "" {
		return token
	}
	return uid + cookieSeparatorEncoded + token
}

// decodeCookieSeparator turns the browser's percent-encoded "::" into the
// literal separator so a pasted cookie value parses like the decoded form.
// The scan is byte-wise rather than a ToUpper+Index so a stray non-ASCII rune
// in a mangled paste cannot shift the split point.
func decodeCookieSeparator(raw string) string {
	width := len(cookieSeparatorEncoded)
	for i := 0; i+width <= len(raw); i++ {
		if strings.EqualFold(raw[i:i+width], cookieSeparatorEncoded) {
			return raw[:i] + "::" + raw[i+width:]
		}
	}
	return raw
}

// DeepLogin is the PKCE material for one loginDeepControl handshake. The
// verifier is redeemable — anyone holding it plus the uuid can complete the
// login — so it must never be persisted or logged.
type DeepLogin struct {
	UUID      string
	Verifier  string
	Challenge string
	LoginURL  string
}

// BuildDeepLoginURL starts a deep-link login: it generates fresh PKCE material
// and renders the browser URL that approves it. Pair it with PollDeepLogin for
// the semi-automatic path (operator clicks "Yes, Log In", we poll), or let
// ExchangeWebSession approve it from a cookie for the fully automatic path.
func BuildDeepLoginURL() (*DeepLogin, error) {
	verifier, challenge, id, err := NewDeepLinkChallenge()
	if err != nil {
		return nil, err
	}
	return &DeepLogin{
		UUID:      id,
		Verifier:  verifier,
		Challenge: challenge,
		LoginURL:  BuildLoginDeepControlURL(challenge, id),
	}, nil
}

// ExchangeOptions tunes a web-session upgrade. The zero value is usable: it
// targets the production hosts with a shared default HTTP client.
type ExchangeOptions struct {
	// HTTPClient performs every call. A proxy-bound client keeps the exchange
	// on the same egress IP as the account's chat traffic.
	HTTPClient HTTPDoer
	// WebsiteBaseURL hosts loginDeepCallbackControl (default WebsiteBaseURL).
	WebsiteBaseURL string
	// APIBaseURL hosts /auth/poll (default DefaultBaseURL).
	APIBaseURL string
	// PollAttempts caps how many times the poll runs (default 30).
	PollAttempts int
	// PollInterval is the delay between polls (default 1s).
	PollInterval time.Duration
	// Sleep overrides the delay implementation in tests.
	Sleep func(context.Context, time.Duration) error
}

func (o ExchangeOptions) client() HTTPDoer {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (o ExchangeOptions) websiteURL(path string) string {
	base := strings.TrimSpace(o.WebsiteBaseURL)
	if base == "" {
		base = WebsiteBaseURL
	}
	return strings.TrimRight(base, "/") + path
}

func (o ExchangeOptions) apiURL(path string) string {
	base := strings.TrimSpace(o.APIBaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	return strings.TrimRight(base, "/") + path
}

func (o ExchangeOptions) attempts() int {
	if o.PollAttempts > 0 {
		return o.PollAttempts
	}
	return defaultPollAttempts
}

func (o ExchangeOptions) interval() time.Duration {
	if o.PollInterval > 0 {
		return o.PollInterval
	}
	return defaultPollInterval
}

func (o ExchangeOptions) sleep(ctx context.Context, d time.Duration) error {
	if o.Sleep != nil {
		return o.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ApproveDeepLogin authorizes a pending handshake with an already-authenticated
// web session, replacing the human click on the loginDeepControl page. It only
// submits the approval; PollDeepLogin decides whether it took effect.
func ApproveDeepLogin(ctx context.Context, opts ExchangeOptions, workosSessionToken, id, challenge string) error {
	cookie := NormalizeSessionCookie(workosSessionToken)
	if cookie == "" {
		return ErrWebSessionUnauthorized
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(challenge) == "" {
		return errors.New("cursor: deep-link approval needs a uuid and a challenge")
	}

	payload, err := json.Marshal(map[string]string{"uuid": id, "challenge": challenge})
	if err != nil {
		return fmt.Errorf("cursor: encode approval body: %w", err)
	}
	target := opts.websiteURL(EndpointLoginDeepCallbackControl)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("cursor: build approval request: %w", err)
	}
	req.Header.Set("accept", "*/*")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", defaultBrowserUserAgent)
	req.Header.Set("origin", strings.TrimSuffix(opts.websiteURL(""), "/"))
	req.Header.Set("referer", strings.TrimSuffix(opts.websiteURL(""), "/")+"/")
	req.Header.Set("cookie", SessionCookieName+"="+cookie)

	resp, err := opts.client().Do(req)
	if err != nil {
		return fmt.Errorf("cursor: approve deep-link login: %w", err)
	}
	defer drainAndClose(resp)

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return ErrWebSessionUnauthorized
	case resp.StatusCode >= 400:
		return fmt.Errorf("cursor: approve deep-link login: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// PollDeepLoginOnce performs a single /auth/poll. It returns (nil, nil) while
// the handshake is still pending, which is the normal state until the browser
// (or ApproveDeepLogin) completes it.
func PollDeepLoginOnce(ctx context.Context, opts ExchangeOptions, id, verifier string) (*TokenResponse, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(verifier) == "" {
		return nil, errors.New("cursor: deep-link poll needs a uuid and a verifier")
	}
	q := url.Values{}
	q.Set("uuid", id)
	q.Set("verifier", verifier)
	target := opts.apiURL(EndpointAuthPoll) + "?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("cursor: build poll request: %w", err)
	}
	req.Header.Set("accept", "*/*")
	req.Header.Set("user-agent", defaultClientUserAgent)

	resp, err := opts.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("cursor: poll deep-link login: %w", err)
	}
	defer drainAndClose(resp)

	// 404 is the documented "not approved yet" answer, not a routing error.
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cursor: poll deep-link login: unexpected status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAuthBody))
	if err != nil {
		return nil, fmt.Errorf("cursor: read poll response: %w", err)
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("cursor: decode poll response: %w", err)
	}
	// A 200 with an empty body is another way the backend says "still waiting".
	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, nil
	}
	return &token, nil
}

// PollDeepLogin polls until the handshake completes, the context is cancelled,
// or the attempt budget runs out (ErrDeepLoginPending).
func PollDeepLogin(ctx context.Context, opts ExchangeOptions, id, verifier string) (*TokenResponse, error) {
	attempts := opts.attempts()
	interval := opts.interval()
	var lastErr error
	for i := 0; i < attempts; i++ {
		token, err := PollDeepLoginOnce(ctx, opts, id, verifier)
		if err != nil {
			// A single failed poll is not fatal: the login may still complete.
			// Only the last error is reported if the budget then runs out.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
		}
		if token != nil {
			return token, nil
		}
		if i == attempts-1 {
			break
		}
		if err := opts.sleep(ctx, interval); err != nil {
			return nil, err
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("%w: last poll error: %v", ErrDeepLoginPending, lastErr)
	}
	return nil, ErrDeepLoginPending
}

// ExchangeWebSessionWithOptions upgrades a WorkosCursorSessionToken into a
// client credential by running the whole loginDeepControl handshake headlessly:
// generate PKCE material, approve it with the cookie, then poll for the tokens.
//
// It refuses to return another web token, so a caller can persist the result
// without re-checking the claim.
func ExchangeWebSessionWithOptions(ctx context.Context, workosSessionToken string, opts ExchangeOptions) (*TokenResponse, error) {
	if NormalizeSessionCookie(workosSessionToken) == "" {
		return nil, ErrWebSessionUnauthorized
	}
	login, err := BuildDeepLoginURL()
	if err != nil {
		return nil, err
	}
	if err := ApproveDeepLogin(ctx, opts, workosSessionToken, login.UUID, login.Challenge); err != nil {
		return nil, err
	}
	token, err := PollDeepLogin(ctx, opts, login.UUID, login.Verifier)
	if err != nil {
		return nil, err
	}
	if IsWebSessionToken(token.AccessToken) {
		return nil, ErrWebSessionNotUpgraded
	}
	return token, nil
}

// ExchangeWebSession is the convenience form of ExchangeWebSessionWithOptions
// for callers that only have an HTTP client and want the two tokens.
func ExchangeWebSession(ctx context.Context, httpClient HTTPDoer, workosSessionToken string) (accessToken, refreshToken string, err error) {
	token, err := ExchangeWebSessionWithOptions(ctx, workosSessionToken, ExchangeOptions{HTTPClient: httpClient})
	if err != nil {
		return "", "", err
	}
	return token.AccessToken, token.RefreshToken, nil
}

func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAuthBody))
	_ = resp.Body.Close()
}
