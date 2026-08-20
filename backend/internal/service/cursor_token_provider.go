package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	// cursorForceRefreshTTL bounds how long a rejected credential keeps forcing
	// a refresh. It only has to outlive the burst of requests that raced the
	// 401, not the credential itself.
	cursorForceRefreshTTL = 15 * time.Minute

	// cursorForceRefreshWindow is a refresh window wide enough that
	// NeedsRefresh always answers true. Rotating a rejected credential cannot
	// depend on its expiry: upstream refuses plenty of JWTs that are still
	// nowhere near expiring.
	cursorForceRefreshWindow = 100 * 365 * 24 * time.Hour

	// Lock-race wait budget, used when another worker is already refreshing
	// this account. It mirrors the Grok/OpenAI providers: a short wait for the
	// winner to publish its token beats sending a credential we already know is
	// stale.
	cursorLockInitialWait = 100 * time.Millisecond
	cursorLockMaxWait     = 800 * time.Millisecond
	cursorLockMaxAttempts = 5
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

	// errCursorAccessTokenRejected marks a credential upstream has already
	// refused which a refresh failed to rotate. Handing it back would only
	// reproduce the same 401, so the account fails over instead.
	errCursorAccessTokenRejected = errors.New("cursor access token was rejected upstream and could not be refreshed")
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

	cacheKey := CursorTokenCacheKey(account)
	accessToken := strings.TrimSpace(account.GetCursorAccessToken())
	needsUpgrade := cursorpkg.IsWebSessionToken(accessToken)
	hasWebSession := strings.TrimSpace(account.GetCursorWebSessionToken()) != ""
	hasRefreshSource := strings.TrimSpace(account.GetCursorAPIKey()) != "" ||
		strings.TrimSpace(account.GetCursorRefreshToken()) != "" ||
		hasWebSession

	// A credential upstream has already refused must not be served again, from
	// either the cache or the account row, until a refresh rotates it.
	rejected := p.rejectedFingerprint(ctx, cacheKey)
	tokenRejected := rejected != "" && rejected == cursorTokenFingerprint(accessToken)

	if !tokenRejected {
		if cached, ok := p.cachedToken(ctx, cacheKey, rejected); ok {
			return cached, nil
		}
	}

	expiresAt := p.tokenExpiry(account)
	tokenFresh := accessToken != "" && !needsUpgrade && expiresAt != nil && time.Until(*expiresAt) > cursorTokenRefreshSkew
	if tokenFresh && !tokenRejected {
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
		if tokenRejected {
			return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenRejected, account)
		}
		if expiresAt != nil && !time.Now().Before(*expiresAt) {
			return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenExpired, account)
		}
		return accessToken, nil
	}

	if p.refreshAPI == nil || p.executor == nil {
		if !tokenRejected && accessToken != "" && !needsUpgrade && (expiresAt == nil || time.Now().Before(*expiresAt)) {
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

	// A rejected credential is rotated regardless of its expiry: upstream
	// refuses plenty of JWTs that still look fresh.
	refreshWindow := cursorTokenRefreshSkew
	if tokenRejected {
		refreshWindow = cursorForceRefreshWindow
	}
	result, err := p.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(refreshCtx), account, p.executor, refreshWindow)
	if err != nil {
		if p.refreshPolicy.OnRefreshError == ProviderRefreshErrorUseExistingToken &&
			!tokenRejected && accessToken != "" && !needsUpgrade &&
			(expiresAt == nil || time.Now().Before(*expiresAt)) {
			// A transient refresh failure should not drop a still-valid token.
			return accessToken, nil
		}
		return "", withCursorCredentialFailureSnapshot(err, account)
	}
	if result != nil && result.LockHeld {
		// Another worker owns the refresh. Waiting for it to publish beats
		// racing it, and is the only correct answer when the token we hold was
		// just rejected.
		if p.refreshPolicy.OnLockHeld == ProviderLockHeldWaitForCache || tokenRejected {
			if token, ok := p.waitForRefreshedToken(refreshCtx, cacheKey, rejected); ok {
				return token, nil
			}
		}
		if tokenRejected {
			return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenRejected, account)
		}
		if accessToken != "" && !needsUpgrade && (expiresAt == nil || time.Now().Before(*expiresAt)) {
			return accessToken, nil
		}
		return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenExpired, account)
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
	if rejected != "" && rejected == cursorTokenFingerprint(refreshed) {
		// The refresh reported success but produced the same JWT upstream just
		// refused. Returning it would spin the request through the identical
		// 401, so the account fails over and the marker stays armed.
		return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenRejected, account)
	}
	newExpiry := p.tokenExpiry(account)
	if newExpiry != nil && !time.Now().Before(*newExpiry) {
		return "", withCursorCredentialFailureSnapshot(errCursorAccessTokenExpired, account)
	}
	p.clearRejectedFingerprint(ctx, cacheKey)
	p.cacheToken(ctx, account, refreshed, newExpiry)
	return refreshed, nil
}

// cachedToken returns a cached access token when it is still safely usable.
// The cache is a fast path only: it is trusted when its own JWT expiry is
// comfortably ahead and it is neither a browser credential nor the credential
// upstream just refused.
func (p *CursorTokenProvider) cachedToken(ctx context.Context, cacheKey, rejectedFingerprint string) (string, bool) {
	if p.tokenCache == nil {
		return "", false
	}
	cached, err := p.tokenCache.GetAccessToken(ctx, cacheKey)
	if err != nil {
		return "", false
	}
	cached = strings.TrimSpace(cached)
	if cached == "" || cursorpkg.IsWebSessionToken(cached) {
		return "", false
	}
	if rejectedFingerprint != "" && rejectedFingerprint == cursorTokenFingerprint(cached) {
		return "", false
	}
	if exp, ok := cursorpkg.JWTExpiry(cached); ok && time.Until(exp) <= cursorTokenRefreshSkew {
		return "", false
	}
	return cached, true
}

// waitForRefreshedToken polls the cache with backoff while another worker holds
// the refresh lock, returning the token that worker publishes.
func (p *CursorTokenProvider) waitForRefreshedToken(ctx context.Context, cacheKey, rejectedFingerprint string) (string, bool) {
	if p.tokenCache == nil {
		return "", false
	}
	wait := cursorLockInitialWait
	for i := 0; i < cursorLockMaxAttempts; i++ {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", false
		case <-timer.C:
		}
		if token, ok := p.cachedToken(ctx, cacheKey, rejectedFingerprint); ok {
			return token, true
		}
		if wait < cursorLockMaxWait {
			wait *= 2
			if wait > cursorLockMaxWait {
				wait = cursorLockMaxWait
			}
		}
	}
	return "", false
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

// InvalidateToken is called after upstream refuses a credential (401/403). It
// drops the cached copy and records which JWT was refused.
//
// Dropping the cache alone would be a no-op: the same token also lives in the
// account row, so the next GetAccessToken would hand back the very credential
// upstream just rejected and the account would 401 in a loop until the JWT
// finally expired. The fingerprint is what turns the next call into a real
// refresh.
func (p *CursorTokenProvider) InvalidateToken(ctx context.Context, account *Account) error {
	if p == nil || p.tokenCache == nil || account == nil {
		return nil
	}
	cacheKey := CursorTokenCacheKey(account)
	err := p.tokenCache.DeleteAccessToken(ctx, cacheKey)
	if fingerprint := cursorTokenFingerprint(account.GetCursorAccessToken()); fingerprint != "" {
		if setErr := p.tokenCache.SetAccessToken(ctx, cursorForceRefreshCacheKey(cacheKey), fingerprint, cursorForceRefreshTTL); setErr != nil && err == nil {
			err = setErr
		}
	}
	return err
}

func (p *CursorTokenProvider) rejectedFingerprint(ctx context.Context, cacheKey string) string {
	if p.tokenCache == nil {
		return ""
	}
	fingerprint, err := p.tokenCache.GetAccessToken(ctx, cursorForceRefreshCacheKey(cacheKey))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(fingerprint)
}

func (p *CursorTokenProvider) clearRejectedFingerprint(ctx context.Context, cacheKey string) {
	if p.tokenCache == nil {
		return
	}
	_ = p.tokenCache.DeleteAccessToken(ctx, cursorForceRefreshCacheKey(cacheKey))
}

func cursorForceRefreshCacheKey(cacheKey string) string {
	return cacheKey + ":rejected"
}

// cursorTokenFingerprint identifies a credential without storing it a second
// time: the marker only ever has to answer "is this the same token?".
func cursorTokenFingerprint(token string) string {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return hex.EncodeToString(sum[:8])
}

// CursorTokenCacheKey is the distributed-lock / access-token cache key for a
// Cursor account.
func CursorTokenCacheKey(account *Account) string {
	if account == nil {
		return "cursor:account:0"
	}
	return "cursor:account:" + strconv.FormatInt(account.ID, 10)
}
