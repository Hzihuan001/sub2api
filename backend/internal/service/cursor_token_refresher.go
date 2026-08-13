package service

import (
	"context"
	"errors"
	"strings"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

// CursorTokenRefresher implements OAuthRefreshExecutor for Cursor accounts.
// It can refresh through either credential source: a crsr_ API key (re-exchange)
// or a deep-link refresh token.
type CursorTokenRefresher struct {
	cursorOAuthService CursorOAuthTokenService
}

func NewCursorTokenRefresher(cursorOAuthService CursorOAuthTokenService) *CursorTokenRefresher {
	return &CursorTokenRefresher{cursorOAuthService: cursorOAuthService}
}

func (r *CursorTokenRefresher) CacheKey(account *Account) string {
	return CursorTokenCacheKey(account)
}

func (r *CursorTokenRefresher) CanRefresh(account *Account) bool {
	if account == nil || account.Platform != PlatformCursor || account.Type != AccountTypeOAuth {
		return false
	}
	return strings.TrimSpace(account.GetCursorAPIKey()) != "" ||
		strings.TrimSpace(account.GetCursorRefreshToken()) != "" ||
		strings.TrimSpace(account.GetCursorWebSessionToken()) != ""
}

func (r *CursorTokenRefresher) NeedsRefresh(account *Account, refreshWindow time.Duration) bool {
	if account == nil || !r.CanRefresh(account) {
		return false
	}
	accessToken := strings.TrimSpace(account.GetCursorAccessToken())
	if accessToken == "" {
		return true
	}
	// A web cookie can outlive its warm window by weeks and still be useless
	// for chat, so expiry says nothing about whether it needs replacing.
	if cursorpkg.IsWebSessionToken(accessToken) {
		return true
	}
	expiresAt := account.GetCredentialAsTime("expires_at")
	if expiresAt == nil {
		if exp, ok := cursorpkg.JWTExpiry(accessToken); ok {
			expiresAt = &exp
		}
	}
	if expiresAt == nil {
		return true
	}
	if refreshWindow < cursorTokenRefreshSkew {
		refreshWindow = cursorTokenRefreshSkew
	}
	return time.Until(*expiresAt) < refreshWindow
}

func (r *CursorTokenRefresher) Refresh(ctx context.Context, account *Account) (map[string]any, error) {
	if r == nil || r.cursorOAuthService == nil {
		return nil, errors.New("cursor oauth service is not configured")
	}
	tokenInfo, err := r.cursorOAuthService.RefreshAccountToken(ctx, account)
	if err != nil {
		return nil, err
	}
	newCredentials := r.cursorOAuthService.BuildAccountCredentials(tokenInfo)
	newCredentials = MergeCredentials(account.Credentials, newCredentials)
	// Operator overrides (base_url / model_mapping) are preserved by the merge;
	// re-pin an explicit base_url so a token refresh never resets it.
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		newCredentials["base_url"] = baseURL
	}
	return newCredentials, nil
}
