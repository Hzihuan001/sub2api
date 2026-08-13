package cursor

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// signedJWT builds an unsigned JWT carrying the given claims. Only the payload
// is ever read, so the header and signature are placeholders.
func signedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func webJWT(t *testing.T) string {
	t.Helper()
	return signedJWT(t, map[string]any{
		"sub":   "auth0|user_01WEB",
		"type":  TokenTypeWeb,
		"scope": "openid profile email offline_access",
		"aud":   "https://cursor.com",
		"iss":   "https://authentication.cursor.sh",
		"iat":   time.Now().Add(-time.Hour).Unix(),
		"exp":   time.Now().Add(60 * 24 * time.Hour).Unix(),
	})
}

func clientJWT(t *testing.T) string {
	t.Helper()
	return signedJWT(t, map[string]any{
		"sub":  "auth0|user_01WEB",
		"type": TokenTypeSession,
		"exp":  time.Now().Add(time.Hour).Unix(),
	})
}

func TestParseTokenClaims(t *testing.T) {
	claims, ok := ParseTokenClaims(webJWT(t))
	require.True(t, ok)
	require.Equal(t, "auth0|user_01WEB", claims.Subject)
	require.Equal(t, "user_01WEB", claims.UserID)
	require.Equal(t, TokenTypeWeb, claims.Type)
	require.Equal(t, "https://cursor.com", claims.Audience)
	require.Equal(t, "https://authentication.cursor.sh", claims.Issuer)
	require.True(t, claims.IsWeb())
	require.False(t, claims.ExpiresAt.IsZero())
	require.False(t, claims.IssuedAt.IsZero())

	_, ok = ParseTokenClaims("crsr_not_a_jwt")
	require.False(t, ok)
}

func TestTokenType(t *testing.T) {
	require.Equal(t, TokenTypeWeb, TokenType(webJWT(t)))
	require.Equal(t, TokenTypeSession, TokenType(clientJWT(t)))
	// The cookie forms resolve to the same claim as the bare JWT.
	require.Equal(t, TokenTypeWeb, TokenType("user_01WEB::"+webJWT(t)))
	require.Equal(t, TokenTypeWeb, TokenType("user_01WEB%3A%3A"+webJWT(t)))
	// Non-JWT credentials (crsr_ API keys) carry no type.
	require.Equal(t, "", TokenType("crsr_abcdef"))
	require.Equal(t, "", TokenType(""))
}

func TestIsWebSessionToken(t *testing.T) {
	require.True(t, IsWebSessionToken(webJWT(t)))
	require.False(t, IsWebSessionToken(clientJWT(t)))
	require.False(t, IsWebSessionToken("crsr_abcdef"))
}

func TestParseTokenAcceptsEncodedCookieSeparator(t *testing.T) {
	jwt := webJWT(t)
	token, uid := ParseToken("user_01WEB%3A%3A" + jwt)
	require.Equal(t, jwt, token)
	require.Equal(t, "user_01WEB", uid)

	// The encoding is matched case-insensitively (browsers and copy helpers
	// disagree on %3a vs %3A).
	token, uid = ParseToken("user_01WEB%3a%3a" + jwt)
	require.Equal(t, jwt, token)
	require.Equal(t, "user_01WEB", uid)

	// The decoded form and the bare JWT keep working unchanged.
	token, uid = ParseToken("user_01WEB::" + jwt)
	require.Equal(t, jwt, token)
	require.Equal(t, "user_01WEB", uid)

	token, uid = ParseToken(jwt)
	require.Equal(t, jwt, token)
	require.Equal(t, "user_01WEB", uid)
}

func TestNormalizeSessionCookie(t *testing.T) {
	jwt := webJWT(t)
	want := "user_01WEB" + cookieSeparatorEncoded + jwt

	require.Equal(t, want, NormalizeSessionCookie("user_01WEB::"+jwt))
	require.Equal(t, want, NormalizeSessionCookie("user_01WEB%3A%3A"+jwt))
	// A bare JWT still yields the cookie form because the sub claim carries
	// the user id.
	require.Equal(t, want, NormalizeSessionCookie(jwt))
	// Without a recoverable user id the bare value is used as-is.
	require.Equal(t, "opaque", NormalizeSessionCookie("  opaque  "))
	require.Equal(t, "", NormalizeSessionCookie("   "))
}

func TestBuildDeepLoginURL(t *testing.T) {
	login, err := BuildDeepLoginURL()
	require.NoError(t, err)
	require.NotEmpty(t, login.UUID)
	require.NotEmpty(t, login.Verifier)
	require.NotEmpty(t, login.Challenge)

	sum := sha256.Sum256([]byte(login.Verifier))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), login.Challenge)
	require.Contains(t, login.LoginURL, "challenge="+login.Challenge)
	require.Contains(t, login.LoginURL, "uuid="+login.UUID)
	require.Contains(t, login.LoginURL, "mode=login")

	// Every handshake must be unique or two concurrent upgrades would race for
	// the same one-time login id.
	other, err := BuildDeepLoginURL()
	require.NoError(t, err)
	require.NotEqual(t, login.UUID, other.UUID)
	require.NotEqual(t, login.Verifier, other.Verifier)
}

// exchangeServer stands in for both Cursor hosts: /api/auth/... is the website
// and /auth/poll is the API. Returning one base URL for both keeps the test
// close to the real call sequence.
type exchangeServer struct {
	server *httptest.Server

	approvals   atomic.Int64
	polls       atomic.Int64
	approveCode int
	pollAfter   int64
	tokens      string

	lastCookie    atomic.Value
	lastUUID      atomic.Value
	lastChallenge atomic.Value
	lastVerifier  atomic.Value
}

func newExchangeServer(t *testing.T, tokens string) *exchangeServer {
	t.Helper()
	es := &exchangeServer{approveCode: http.StatusOK, tokens: tokens}
	es.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case EndpointLoginDeepCallbackControl:
			es.approvals.Add(1)
			es.lastCookie.Store(r.Header.Get("cookie"))
			body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
			var payload struct {
				UUID      string `json:"uuid"`
				Challenge string `json:"challenge"`
			}
			_ = json.Unmarshal(body, &payload)
			es.lastUUID.Store(payload.UUID)
			es.lastChallenge.Store(payload.Challenge)
			w.WriteHeader(es.approveCode)
			_, _ = w.Write([]byte(`{}`))
		case EndpointAuthPoll:
			n := es.polls.Add(1)
			es.lastUUID.Store(r.URL.Query().Get("uuid"))
			es.lastVerifier.Store(r.URL.Query().Get("verifier"))
			if n <= es.pollAfter {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(es.tokens))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(es.server.Close)
	return es
}

func (es *exchangeServer) options() ExchangeOptions {
	return ExchangeOptions{
		HTTPClient:     es.server.Client(),
		WebsiteBaseURL: es.server.URL,
		APIBaseURL:     es.server.URL,
		PollAttempts:   5,
		Sleep:          func(context.Context, time.Duration) error { return nil },
	}
}

func loadString(v *atomic.Value) string {
	s, _ := v.Load().(string)
	return s
}

func TestExchangeWebSessionUpgradesWebToken(t *testing.T) {
	client := clientJWT(t)
	es := newExchangeServer(t, `{"accessToken":"`+client+`","refreshToken":"refresh-1","authId":"auth0|user_01WEB"}`)
	es.pollAfter = 2

	web := webJWT(t)
	token, err := ExchangeWebSessionWithOptions(context.Background(), "user_01WEB::"+web, es.options())
	require.NoError(t, err)
	require.Equal(t, client, token.AccessToken)
	require.Equal(t, "refresh-1", token.RefreshToken)
	require.Equal(t, "auth0|user_01WEB", token.AuthID)
	require.Equal(t, TokenTypeSession, TokenType(token.AccessToken))

	require.Equal(t, int64(1), es.approvals.Load())
	require.Equal(t, int64(3), es.polls.Load())

	// The approval must carry the browser cookie form, and the poll must reuse
	// the same handshake id the approval registered.
	require.Equal(t, SessionCookieName+"=user_01WEB"+cookieSeparatorEncoded+web, loadString(&es.lastCookie))
	require.NotEmpty(t, loadString(&es.lastVerifier))
	sum := sha256.Sum256([]byte(loadString(&es.lastVerifier)))
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), loadString(&es.lastChallenge))
}

// doerFunc adapts a function to HTTPDoer so a test can answer both production
// hosts without any network access.
type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestExchangeWebSessionConvenienceForm(t *testing.T) {
	client := clientJWT(t)
	var approved, polled bool
	doer := doerFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case EndpointLoginDeepCallbackControl:
			approved = true
			require.Equal(t, WebsiteBaseURL, "https://"+req.URL.Host)
			return jsonResponse(http.StatusOK, `{}`), nil
		case EndpointAuthPoll:
			polled = true
			require.Equal(t, DefaultBaseURL, "https://"+req.URL.Host)
			// snake_case spellings decode the same as camelCase.
			return jsonResponse(http.StatusOK, `{"access_token":"`+client+`","refresh_token":"refresh-2"}`), nil
		}
		t.Fatalf("unexpected request to %s", req.URL)
		return nil, nil
	})

	access, refresh, err := ExchangeWebSession(context.Background(), doer, webJWT(t))
	require.NoError(t, err)
	require.Equal(t, client, access)
	require.Equal(t, "refresh-2", refresh)
	require.True(t, approved)
	require.True(t, polled)
}

func TestExchangeWebSessionRejectsUnauthorizedCookie(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	es.approveCode = http.StatusUnauthorized

	_, err := ExchangeWebSessionWithOptions(context.Background(), webJWT(t), es.options())
	require.ErrorIs(t, err, ErrWebSessionUnauthorized)
	require.Equal(t, int64(0), es.polls.Load())
}

func TestExchangeWebSessionRejectsEmptyCredential(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	_, err := ExchangeWebSessionWithOptions(context.Background(), "   ", es.options())
	require.ErrorIs(t, err, ErrWebSessionUnauthorized)
	require.Equal(t, int64(0), es.approvals.Load())
}

func TestExchangeWebSessionRejectsNonUpgradedToken(t *testing.T) {
	// The upstream answered, but with another web token: persisting it would
	// silently reproduce ERROR_NOT_LOGGED_IN on the next chat turn.
	es := newExchangeServer(t, `{"accessToken":"`+webJWT(t)+`"}`)

	_, err := ExchangeWebSessionWithOptions(context.Background(), webJWT(t), es.options())
	require.ErrorIs(t, err, ErrWebSessionNotUpgraded)
}

func TestPollDeepLoginTimesOutAsPending(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	es.pollAfter = 1000

	_, err := PollDeepLogin(context.Background(), es.options(), "uuid-1", "verifier-1")
	require.ErrorIs(t, err, ErrDeepLoginPending)
	require.Equal(t, int64(5), es.polls.Load())
}

func TestPollDeepLoginOnceReportsPending(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	es.pollAfter = 1

	token, err := PollDeepLoginOnce(context.Background(), es.options(), "uuid-1", "verifier-1")
	require.NoError(t, err)
	require.Nil(t, token)

	// A 200 with no access token means the same thing as a 404.
	token, err = PollDeepLoginOnce(context.Background(), es.options(), "uuid-1", "verifier-1")
	require.NoError(t, err)
	require.Nil(t, token)
}

func TestPollDeepLoginRequiresHandshakeMaterial(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	_, err := PollDeepLoginOnce(context.Background(), es.options(), "", "verifier")
	require.Error(t, err)
	_, err = PollDeepLoginOnce(context.Background(), es.options(), "uuid", "")
	require.Error(t, err)
}

func TestPollDeepLoginHonoursContextCancellation(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	es.pollAfter = 1000
	opts := es.options()
	opts.Sleep = nil
	opts.PollInterval = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PollDeepLogin(ctx, opts, "uuid-1", "verifier-1")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestApproveDeepLoginRequiresHandshakeMaterial(t *testing.T) {
	es := newExchangeServer(t, `{}`)
	err := ApproveDeepLogin(context.Background(), es.options(), webJWT(t), "", "challenge")
	require.Error(t, err)
	require.Equal(t, int64(0), es.approvals.Load())
}

func TestExchangeOptionsDefaults(t *testing.T) {
	var opts ExchangeOptions
	require.Equal(t, WebsiteBaseURL+EndpointLoginDeepCallbackControl, opts.websiteURL(EndpointLoginDeepCallbackControl))
	require.Equal(t, DefaultBaseURL+EndpointAuthPoll, opts.apiURL(EndpointAuthPoll))
	require.Equal(t, defaultPollAttempts, opts.attempts())
	require.Equal(t, defaultPollInterval, opts.interval())
	require.NotNil(t, opts.client())

	custom := ExchangeOptions{WebsiteBaseURL: "https://cursor.example.com/", APIBaseURL: "https://api.example.com/"}
	require.True(t, strings.HasPrefix(custom.websiteURL(EndpointLoginDeepCallbackControl), "https://cursor.example.com/api/"))
	require.Equal(t, "https://api.example.com"+EndpointAuthPoll, custom.apiURL(EndpointAuthPoll))
}
