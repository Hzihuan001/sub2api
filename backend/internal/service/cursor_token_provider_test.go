//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func newCursorTestAccount(credentials map[string]any) *Account {
	return &Account{
		ID:          7301,
		Platform:    PlatformCursor,
		Type:        AccountTypeOAuth,
		Status:      StatusActive,
		Credentials: credentials,
	}
}

func makeCursorTestJWT(t *testing.T, exp time.Time) string {
	t.Helper()
	return makeCursorTypedJWT(t, exp, "")
}

// makeCursorTypedJWT builds a Cursor credential with an explicit "type" claim:
// "web" for the browser cookie, "session" for the client token.
func makeCursorTypedJWT(t *testing.T, exp time.Time, tokenType string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := map[string]any{"exp": exp.Unix(), "sub": "auth0|user_01"}
	if tokenType != "" {
		claims["type"] = tokenType
	}
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return fmt.Sprintf("%s.%s.%s", header, base64.RawURLEncoding.EncodeToString(payload), "sig")
}

func TestCursorTokenProviderReturnsFreshStoredToken(t *testing.T) {
	account := newCursorTestAccount(map[string]any{
		"access_token": "fresh-token",
		"expires_at":   time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339),
	})
	provider := NewCursorTokenProvider(nil, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "fresh-token", token)
}

func TestCursorTokenProviderUsesJWTExpiryFallback(t *testing.T) {
	jwt := makeCursorTestJWT(t, time.Now().Add(3*time.Hour))
	account := newCursorTestAccount(map[string]any{"access_token": jwt})
	provider := NewCursorTokenProvider(nil, nil)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, jwt, token)
}

func TestCursorTokenProviderExpiredWithoutRefreshSourceFails(t *testing.T) {
	account := newCursorTestAccount(map[string]any{
		"access_token": "stale-token",
		"expires_at":   time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
	})
	provider := NewCursorTokenProvider(nil, nil)

	_, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.ErrorIs(t, err, errCursorAccessTokenExpired)
}

func TestCursorTokenProviderMissingTokenWithoutRefreshSourceFails(t *testing.T) {
	account := newCursorTestAccount(map[string]any{})
	provider := NewCursorTokenProvider(nil, nil)

	_, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.ErrorIs(t, err, errCursorAccessTokenMissing)
}

// A web cookie stays valid for weeks, so expiry alone would happily hand it to
// the chat path — where Cursor answers ERROR_NOT_LOGGED_IN. The provider must
// treat it as unusable and demand an upgrade instead.
func TestCursorTokenProviderNeverServesWebSessionToken(t *testing.T) {
	webToken := makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeWeb)
	account := newCursorTestAccount(map[string]any{"access_token": webToken})
	provider := NewCursorTokenProvider(nil, nil)

	_, err := provider.GetAccessToken(context.Background(), account)
	require.ErrorIs(t, err, errCursorRefreshNotConfigured)

	// The same account holding a client token of identical freshness is served
	// straight from storage.
	clientToken := makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeSession)
	account = newCursorTestAccount(map[string]any{"access_token": clientToken})
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, clientToken, token)
}

func TestCursorTokenProviderRejectsNonCursorAccount(t *testing.T) {
	provider := NewCursorTokenProvider(nil, nil)
	_, err := provider.GetAccessToken(context.Background(), &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	})
	require.Error(t, err)
}

type fakeCursorOAuthTokenService struct {
	tokenInfo *CursorTokenInfo
	err       error
	calls     int
}

func (f *fakeCursorOAuthTokenService) RefreshAccountToken(_ context.Context, _ *Account) (*CursorTokenInfo, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.tokenInfo, nil
}

func (f *fakeCursorOAuthTokenService) BuildAccountCredentials(tokenInfo *CursorTokenInfo) map[string]any {
	creds := map[string]any{"access_token": tokenInfo.AccessToken}
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.ExpiresAt > 0 {
		creds["expires_at"] = time.Unix(tokenInfo.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	return creds
}

func TestCursorTokenRefresherNeedsRefresh(t *testing.T) {
	refresher := NewCursorTokenRefresher(&fakeCursorOAuthTokenService{})

	expired := newCursorTestAccount(map[string]any{
		"access_token": "stale",
		"api_key":      "crsr_abc",
		"expires_at":   time.Now().Add(-time.Minute).UTC().Format(time.RFC3339),
	})
	require.True(t, refresher.CanRefresh(expired))
	require.True(t, refresher.NeedsRefresh(expired, time.Hour))

	fresh := newCursorTestAccount(map[string]any{
		"access_token": "fresh",
		"api_key":      "crsr_abc",
		"expires_at":   time.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339),
	})
	require.False(t, refresher.NeedsRefresh(fresh, time.Hour))

	// Accounts with no refresh source are never candidates.
	noSource := newCursorTestAccount(map[string]any{"access_token": "only"})
	require.False(t, refresher.CanRefresh(noSource))
	require.False(t, refresher.NeedsRefresh(noSource, time.Hour))
}

func TestCursorTokenRefresherTreatsWebSessionAsRefreshable(t *testing.T) {
	refresher := NewCursorTokenRefresher(&fakeCursorOAuthTokenService{})

	// A cookie-imported account has neither api_key nor refresh_token, yet its
	// web token still has to be replaced — the upgrade is its only refresh.
	cookieOnly := newCursorTestAccount(map[string]any{
		"access_token": makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeWeb),
	})
	require.True(t, refresher.CanRefresh(cookieOnly))
	require.True(t, refresher.NeedsRefresh(cookieOnly, time.Hour))

	// Once upgraded, the account follows the ordinary expiry window again.
	upgraded := newCursorTestAccount(map[string]any{
		"access_token":      makeCursorTypedJWT(t, time.Now().Add(6*time.Hour), cursorpkg.TokenTypeSession),
		"refresh_token":     "refresh-1",
		"web_session_token": "user_01%3A%3Aweb-cookie",
		"expires_at":        time.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339),
	})
	require.True(t, refresher.CanRefresh(upgraded))
	require.False(t, refresher.NeedsRefresh(upgraded, time.Hour))
}

func TestCursorTokenRefresherRefreshPreservesOperatorOverrides(t *testing.T) {
	fake := &fakeCursorOAuthTokenService{tokenInfo: &CursorTokenInfo{
		AccessToken: "new-access",
		ExpiresAt:   time.Now().Add(time.Hour).Unix(),
	}}
	refresher := NewCursorTokenRefresher(fake)
	account := newCursorTestAccount(map[string]any{
		"access_token":  "old-access",
		"refresh_token": "keep-refresh",
		"base_url":      "https://relay.example.com",
		"model_mapping": map[string]any{"claude-4.5-sonnet": "claude-4.5-sonnet"},
	})

	creds, err := refresher.Refresh(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, fake.calls)
	require.Equal(t, "new-access", creds["access_token"])
	// The merge keeps operator overrides and untouched credential fields.
	require.Equal(t, "https://relay.example.com", creds["base_url"])
	require.Equal(t, "keep-refresh", creds["refresh_token"])
	require.Contains(t, creds, "model_mapping")
}
