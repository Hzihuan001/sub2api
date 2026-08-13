package cursor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Authentication endpoints. Credential lifecycle traffic always targets the
// official hosts; per-account base_url overrides only affect chat forwarding.
const (
	// EndpointExchangeUserAPIKey exchanges a crsr_ user API key for a session
	// access token. The key can be exchanged repeatedly.
	EndpointExchangeUserAPIKey = "/auth/exchange_user_api_key"

	// EndpointOAuthToken refreshes a deep-link session with its refresh token.
	// Note: refresh tokens returned by the API-key exchange are NOT accepted
	// here; re-exchange the API key instead.
	EndpointOAuthToken = "/oauth/token"

	// EndpointAuthPoll is polled during the loginDeepControl flow.
	EndpointAuthPoll = "/auth/poll"

	// DeepLinkLoginURL is the browser page that starts the PKCE deep-link login.
	DeepLinkLoginURL = "https://cursor.com/loginDeepControl"
)

// Credential source labels stored alongside imported Cursor credentials.
const (
	CredentialSourceCookie   = "cookie"
	CredentialSourceAPIKey   = "api_key"
	CredentialSourceDeepLink = "deep_link"
)

// TokenResponse is the tolerant decode target for every Cursor auth endpoint
// (API-key exchange, refresh, deep-link poll). Upstream builds have used both
// camelCase and snake_case field spellings, so both are accepted.
type TokenResponse struct {
	AccessToken  string
	RefreshToken string
	AuthID       string
	ExpiresIn    int64
}

// UnmarshalJSON accepts accessToken/access_token (and friends) transparently.
func (t *TokenResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		AccessToken      string `json:"accessToken"`
		AccessTokenSnake string `json:"access_token"`
		RefreshToken     string `json:"refreshToken"`
		RefreshSnake     string `json:"refresh_token"`
		AuthID           string `json:"authId"`
		AuthIDSnake      string `json:"auth_id"`
		ExpiresIn        int64  `json:"expiresIn"`
		ExpiresInSnake   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	t.AccessToken = firstNonEmptyString(raw.AccessToken, raw.AccessTokenSnake)
	t.RefreshToken = firstNonEmptyString(raw.RefreshToken, raw.RefreshSnake)
	t.AuthID = firstNonEmptyString(raw.AuthID, raw.AuthIDSnake)
	t.ExpiresIn = raw.ExpiresIn
	if t.ExpiresIn == 0 {
		t.ExpiresIn = raw.ExpiresInSnake
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// NewDeepLinkChallenge creates the PKCE material for a loginDeepControl flow:
// a random URL-safe verifier, its SHA-256 challenge (base64url, no padding)
// and the flow uuid.
func NewDeepLinkChallenge() (verifier, challenge, id string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", fmt.Errorf("cursor: generate verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	id = uuid.NewString()
	return verifier, challenge, id, nil
}

// BuildLoginDeepControlURL renders the browser URL that the operator opens to
// approve a deep-link login.
func BuildLoginDeepControlURL(challenge, id string) string {
	q := url.Values{}
	q.Set("challenge", challenge)
	q.Set("uuid", id)
	q.Set("mode", "login")
	return DeepLinkLoginURL + "?" + q.Encode()
}

// JWTExpiry extracts the exp claim from a JWT (or "userId::JWT") without
// verifying the signature. ok is false when the token is not a parseable JWT
// or carries no exp claim.
func JWTExpiry(raw string) (expiry time.Time, ok bool) {
	token, _ := ParseToken(raw)
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return time.Time{}, false
	}
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		return time.Time{}, false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(claims.Exp, 0), true
}

// IsUserAPIKey reports whether the credential looks like a crsr_ user API key.
func IsUserAPIKey(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "crsr_")
}
