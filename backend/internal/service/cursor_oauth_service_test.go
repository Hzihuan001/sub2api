//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

// fakeCursorOAuthClient records which credential lifecycle leg was taken, so a
// test can assert the ordering between the API-key, deep-link and web-session
// paths rather than just the returned token.
type fakeCursorOAuthClient struct {
	apiKeyResp *cursorpkg.TokenResponse
	apiKeyErr  error
	apiKeyCall int

	refreshResp *cursorpkg.TokenResponse
	refreshErr  error
	refreshCall int

	webResp   *cursorpkg.TokenResponse
	webErr    error
	webCall   int
	webCookie string
}

func (f *fakeCursorOAuthClient) ExchangeUserAPIKey(_ context.Context, _, _ string) (*cursorpkg.TokenResponse, error) {
	f.apiKeyCall++
	return f.apiKeyResp, f.apiKeyErr
}

func (f *fakeCursorOAuthClient) RefreshToken(_ context.Context, _, _ string) (*cursorpkg.TokenResponse, error) {
	f.refreshCall++
	return f.refreshResp, f.refreshErr
}

func (f *fakeCursorOAuthClient) PollDeepLink(_ context.Context, _, _, _ string) (*cursorpkg.TokenResponse, error) {
	return nil, errors.New("not used")
}

func (f *fakeCursorOAuthClient) ExchangeWebSession(_ context.Context, workosSessionToken, _ string) (*cursorpkg.TokenResponse, error) {
	f.webCall++
	f.webCookie = workosSessionToken
	return f.webResp, f.webErr
}

func cursorClientTokenResponse(t *testing.T) *cursorpkg.TokenResponse {
	t.Helper()
	return &cursorpkg.TokenResponse{
		AccessToken:  makeCursorTypedJWT(t, time.Now().Add(time.Hour), cursorpkg.TokenTypeSession),
		RefreshToken: "deep-link-refresh",
		AuthID:       "auth0|user_01",
	}
}

func TestCursorOAuthServiceUpgradesCookieOnlyAccount(t *testing.T) {
	client := &fakeCursorOAuthClient{webResp: cursorClientTokenResponse(t)}
	svc := NewCursorOAuthService(nil, client)

	webToken := makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeWeb)
	account := newCursorTestAccount(map[string]any{"access_token": webToken})

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, client.webCall)
	require.Equal(t, 0, client.apiKeyCall)
	require.Equal(t, 0, client.refreshCall)
	require.Equal(t, cursorpkg.TokenTypeSession, cursorpkg.TokenType(info.AccessToken))
	require.Equal(t, "deep-link-refresh", info.RefreshToken)
	// The cookie is carried forward so a later upgrade can be replayed, and it
	// is sent upstream in the browser's own encoding.
	require.Equal(t, "user_01%3A%3A"+webToken, info.WebSessionToken)
	require.Equal(t, "user_01%3A%3A"+webToken, client.webCookie)

	creds := svc.BuildAccountCredentials(info)
	require.Equal(t, info.AccessToken, creds["access_token"])
	require.Equal(t, info.WebSessionToken, creds["web_session_token"])
}

func TestCursorOAuthServicePrefersAPIKeyOverWebSession(t *testing.T) {
	client := &fakeCursorOAuthClient{
		apiKeyResp: cursorClientTokenResponse(t),
		webResp:    cursorClientTokenResponse(t),
	}
	svc := NewCursorOAuthService(nil, client)
	account := newCursorTestAccount(map[string]any{
		"access_token":      makeCursorTypedJWT(t, time.Now().Add(-time.Hour), cursorpkg.TokenTypeSession),
		"api_key":           "crsr_key",
		"web_session_token": "user_01%3A%3Aweb-cookie",
	})

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, client.apiKeyCall)
	require.Equal(t, 0, client.webCall)
	require.Equal(t, "crsr_key", info.APIKey)
	// The stored cookie survives an API-key refresh.
	require.Equal(t, "user_01%3A%3Aweb-cookie", info.WebSessionToken)
}

func TestCursorOAuthServiceFallsBackToWebSessionWhenRefreshTokenIsDead(t *testing.T) {
	client := &fakeCursorOAuthClient{
		refreshErr: errors.New("refresh token revoked"),
		webResp:    cursorClientTokenResponse(t),
	}
	svc := NewCursorOAuthService(nil, client)
	account := newCursorTestAccount(map[string]any{
		"access_token":      makeCursorTypedJWT(t, time.Now().Add(-time.Hour), cursorpkg.TokenTypeSession),
		"refresh_token":     "stale-refresh",
		"web_session_token": "user_01%3A%3Aweb-cookie",
	})

	info, err := svc.RefreshAccountToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 1, client.refreshCall)
	require.Equal(t, 1, client.webCall)
	require.Equal(t, cursorpkg.TokenTypeSession, cursorpkg.TokenType(info.AccessToken))
}

func TestCursorOAuthServiceSurfacesRefreshErrorWithoutWebSession(t *testing.T) {
	client := &fakeCursorOAuthClient{refreshErr: errors.New("refresh token revoked")}
	svc := NewCursorOAuthService(nil, client)
	account := newCursorTestAccount(map[string]any{
		"access_token":  makeCursorTypedJWT(t, time.Now().Add(-time.Hour), cursorpkg.TokenTypeSession),
		"refresh_token": "stale-refresh",
	})

	_, err := svc.RefreshAccountToken(context.Background(), account)
	require.Error(t, err)
	require.Equal(t, 0, client.webCall)
}

func TestCursorOAuthServiceRejectsNonUpgradedExchange(t *testing.T) {
	// Cursor answered with another browser token: persisting it would only
	// reproduce ERROR_NOT_LOGGED_IN on the next turn.
	webToken := makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeWeb)
	client := &fakeCursorOAuthClient{webResp: &cursorpkg.TokenResponse{AccessToken: webToken}}
	svc := NewCursorOAuthService(nil, client)
	account := newCursorTestAccount(map[string]any{"access_token": webToken})

	_, err := svc.RefreshAccountToken(context.Background(), account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "client token")
}

func TestCursorOAuthServiceImportFromCookieRetainsWebSession(t *testing.T) {
	svc := NewCursorOAuthService(nil, &fakeCursorOAuthClient{})
	webToken := makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeWeb)

	info, err := svc.ImportFromCookie(context.Background(), "user_01::"+webToken)
	require.NoError(t, err)
	require.Equal(t, webToken, info.AccessToken)
	require.Equal(t, "user_01%3A%3A"+webToken, info.WebSessionToken)
	require.Equal(t, cursorpkg.CredentialSourceCookie, info.Source)

	// A deep-link (client) credential is not a web session and must not be
	// recorded as one.
	clientToken := makeCursorTypedJWT(t, time.Now().Add(time.Hour), cursorpkg.TokenTypeSession)
	info, err = svc.ImportFromCookie(context.Background(), clientToken)
	require.NoError(t, err)
	require.Empty(t, info.WebSessionToken)
}

func TestCursorAccountWebSessionTokenFallsBackToAccessToken(t *testing.T) {
	webToken := makeCursorTypedJWT(t, time.Now().Add(60*24*time.Hour), cursorpkg.TokenTypeWeb)

	// Accounts imported before web_session_token existed still expose the
	// cookie through access_token.
	legacy := newCursorTestAccount(map[string]any{"access_token": webToken})
	require.Equal(t, webToken, legacy.GetCursorWebSessionToken())

	// The explicit key wins once the upgrade has rewritten access_token.
	upgraded := newCursorTestAccount(map[string]any{
		"access_token":      makeCursorTypedJWT(t, time.Now().Add(time.Hour), cursorpkg.TokenTypeSession),
		"web_session_token": "user_01%3A%3Aweb-cookie",
	})
	require.Equal(t, "user_01%3A%3Aweb-cookie", upgraded.GetCursorWebSessionToken())

	// A client-token-only account has no web session at all.
	clientOnly := newCursorTestAccount(map[string]any{
		"access_token": makeCursorTypedJWT(t, time.Now().Add(time.Hour), cursorpkg.TokenTypeSession),
	})
	require.Empty(t, clientOnly.GetCursorWebSessionToken())
}

// The stored cookie can mint client tokens on its own, so it must be treated
// exactly like an access token: never echoed to the frontend, and never wiped
// by a full-object account edit that does not carry it back.
func TestCursorWebSessionTokenIsSensitive(t *testing.T) {
	require.True(t, IsSensitiveCredentialKey("web_session_token"))

	merged := MergePreservingSensitiveCreds(
		map[string]any{"web_session_token": "user_01%3A%3Aweb-cookie", "base_url": "https://old"},
		map[string]any{"base_url": "https://new"},
	)
	require.Equal(t, "user_01%3A%3Aweb-cookie", merged["web_session_token"])
	require.Equal(t, "https://new", merged["base_url"])
}

func TestClassifyCursorWebSessionFailure(t *testing.T) {
	class := classifyCursorCredentialFailure(errCursorWebSessionNotUpgraded)
	require.Equal(t, CursorCredentialReasonWebSession, class.reason)
	require.Equal(t, NextAccountRetry, class.action)
	require.Equal(t, GatewayFailureScopeAccount, class.scope)
}
