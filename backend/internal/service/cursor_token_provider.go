package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

// Cursor access tokens are short-lived JWTs (~1h). Refresh up to this long
// before expiry so the request path rarely blocks on a refresh, matching the
// Grok warm-window strategy.
const (
	cursorTokenRefreshSkew      = 10 * time.Minute
	cursorTokenCacheSkew        = 5 * time.Minute
	cursorRequestRefreshTimeout = 8 * time.Second

	// cursorRequestUpgradeTimeout covers a headless web-session upgrade, which
	// is a multi-leg handshake rather than a single token call. It only ever
	// applies to an account still holding a browser cookie, and only until the
	// resulting client token is persisted.
	cursorRequestUpgradeTimeout = 25 * time.Second
)

var (
	errCursorAccessTokenMissing   = errors.New("cursor access token is missing")
	errCursorAccessTokenExpired   = errors.New("cursor access token is expired")
	errCursorRefreshNotConfigured = errors.New("cursor oauth refresh is not configured")
	errCursorCredentialsMissing   = errors.New("cursor account has no refreshable credential")

	// errCursorWebSessionNotUpgraded marks a credential that is authentic but
	// unusable for chat: a "web" JWT. Cursor answers AvailableModels with it
	// and rejects the conversation endpoint with ERROR_NOT_LOGGED_IN, so it is
	// treated as no credential at all until the deep-link upgrade succeeds.
	errCursorWebSessionNotUpgraded = errors.New("cursor web session token has not been upgraded to a client token")
)

// CursorTokenProvider resolves a usable Cursor access token for an account,
// refreshing on demand through the shared OAuthRefreshAPI. It mirrors the Grok
// provider but is simpler: Cursor has no proxy-bound CAS credential contract.
type CursorTokenProvider struct {
	accountRepo   AccountRepository
	tokenCache    GeminiTokenCache
	refreshAPI    *OAuthRefreshAPI
	executor      OAuthRefreshExecutor
	refreshPolicy ProviderRefreshPolicy
}

func NewCursorTokenProvider(accountRepo AccountRepository, tokenCache GeminiTokenCache) *CursorTokenProvider {
	return &CursorTokenProvider{
		accountRepo:   accountRepo,
		tokenCache:    tokenCache,
		refreshPolicy: CursorProviderRefreshPolicy(),
	}
}

func (p *CursorTokenProvider) SetRefreshAPI(api *OAuthRefreshAPI, executor OAuthRefreshExecutor) {
	p.refreshAPI = api
	p.executor = executor
}

func (p *CursorTokenProvider) SetRefreshPolicy(policy ProviderRefreshPolicy) {
	p.refreshPolicy = policy
}

// GetAccessToken returns a valid Cursor access token, refreshing when the
// stored JWT is within the warm window. The returned token is the clean JWT
// (BuildHeaders re-parses "userId::JWT" if a cookie form leaks through).
//
// A stored "web" JWT (the WorkosCursorSessionToken cookie) is never returned,
// however long it stays unexpired: the conversation endpoint rejects it with
// ERROR_NOT_LOGGED_IN, so it is refreshed into a client token first.
func (p *CursorTokenProvider) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	if account == nil {
		return "", errors.New("account is nil")
	}
	if account.Platform != PlatformCursor || account.Type != AccountTypeOAuth {
		return "", errors.New("not a cursor oauth account")
	}

	accessToken := strings.TrimSpace(account.GetCursorAccessToken())
	needsUpgrade := cursorpkg.IsWebSessionToken(accessToken)
	hasWebSession := strings.TrimSpace(account.GetCursorWebSessionToken()) != ""
	hasRefreshSource := strings.TrimSpace(account.GetCursorAPIKey()) != "" ||
		strings.TrimSpace(account.GetCursorRefreshToken()) != "" ||
		hasWebSession

	expiresAt := p.tokenExpiry(account)
	tokenFresh := accessToken != "" && !needsUpgrade && expiresAt != nil && time.Until(*expiresAt) > cursorTokenRefreshSkew
	if tokenFresh {
		p.cacheToken(ctx, account, accessToken, expiresAt)
		return accessToken, nil
	}

	// No way to refresh: return the stored token if it is still usable, else error.
	if !hasRefreshSource {
		if accessToken == "" {
			return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenMissing, account)
		}
		if needsUpgrade {
			return "", withCursorCredentialFailureSnapshot(errCursorWebSessionNotUpgraded, account)
		}
		if expiresAt != nil && !time.Now().Before(*expiresAt) {
			return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenExpired, account)
		}
		return accessToken, nil
	}

	if p.refreshAPI == nil || p.executor == nil {
		if accessToken != "" && !needsUpgrade && (expiresAt == nil || time.Now().Before(*expiresAt)) {
			return accessToken, nil
		}
		return "", errCursorRefreshNotConfigured
	}

	timeout := cursorRequestRefreshTimeout
	if needsUpgrade {
		timeout = cursorRequestUpgradeTimeout
	}
	refreshCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := p.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(refreshCtx), account, p.executor, cursorTokenRefreshSkew)
	if err != nil {
		if accessToken != "" && !needsUpgrade && (expiresAt == nil || time.Now().Before(*expiresAt)) {
			// A transient refresh failure should not drop a still-valid token.
			return accessToken, nil
		}
		return "", withCursorCredentialFailureSnapshot(err, account)
	}
	if result != nil && result.Account != nil {
		account = result.Account
	}

	refreshed := strings.TrimSpace(account.GetCursorAccessToken())
	if refreshed == "" {
		return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenMissing, account)
	}
	if cursorpkg.IsWebSessionToken(refreshed) {
		// The refresh ran but produced (or kept) a browser credential. Handing
		// it to the chat path would only surface as ERROR_NOT_LOGGED_IN, so the
		// account fails over instead.
		return "", withCursorCredentialFailureSnapshot(errCursorWebSessionNotUpgraded, account)
	}
	newExpiry := p.tokenExpiry(account)
	if newExpiry != nil && !time.Now().Before(*newExpiry) {
		return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenExpired, account)
	}
	p.cacheToken(ctx, account, refreshed, newExpiry)
	return refreshed, nil
}

// tokenExpiry prefers the stored expires_at, falling back to the JWT exp claim.
func (p *CursorTokenProvider) tokenExpiry(account *Account) *time.Time {
	if expiresAt := account.GetCredentialAsTime("expires_at"); expiresAt != nil {
		return expiresAt
	}
	if exp, ok := cursorpkg.JWTExpiry(account.GetCursorAccessToken()); ok {
		return &exp
	}
	return nil
}

func (p *CursorTokenProvider) cacheToken(ctx context.Context, account *Account, token string, expiresAt *time.Time) {
	if p.tokenCache == nil || token == "" {
		return
	}
	ttl := 30 * time.Minute
	if expiresAt != nil {
		until := time.Until(*expiresAt)
		switch {
		case until > cursorTokenCacheSkew:
			ttl = until - cursorTokenCacheSkew
		case until > 0:
			ttl = until
		default:
			return
		}
	}
	_ = p.tokenCache.SetAccessToken(ctx, CursorTokenCacheKey(account), token, ttl)
}

func (p *CursorTokenProvider) InvalidateToken(ctx context.Context, account *Account) error {
	if p == nil || p.tokenCache == nil || account == nil {
		return nil
	}
	return p.tokenCache.DeleteAccessToken(ctx, CursorTokenCacheKey(account))
}

// CursorTokenCacheKey is the distributed-lock / access-token cache key for a
// Cursor account.
func CursorTokenCacheKey(account *Account) string {
	if account == nil {
		return "cursor:account:0"
	}
	return "cursor:account:" + strconv.FormatInt(account.ID, 10)
}
