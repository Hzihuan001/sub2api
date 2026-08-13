package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// cursorDefaultAccessTokenTTL is used only when the access-token JWT carries no
// exp claim (the auth endpoints normally return short-lived ~1h tokens).
const cursorDefaultAccessTokenTTL = time.Hour

// CursorOAuthClient is the HTTP boundary for Cursor's authentication endpoints.
// It is implemented in the repository layer (repository/cursor_oauth_client.go).
type CursorOAuthClient interface {
	// ExchangeUserAPIKey exchanges a crsr_ user API key for a session token.
	ExchangeUserAPIKey(ctx context.Context, apiKey, proxyURL string) (*cursorpkg.TokenResponse, error)
	// RefreshToken exchanges a deep-link refresh token for a fresh session token.
	RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*cursorpkg.TokenResponse, error)
	// PollDeepLink polls the loginDeepControl flow once for a completed login.
	PollDeepLink(ctx context.Context, id, verifier, proxyURL string) (*cursorpkg.TokenResponse, error)
	// ExchangeWebSession upgrades a WorkosCursorSessionToken browser cookie
	// into a chat-capable client credential.
	ExchangeWebSession(ctx context.Context, workosSessionToken, proxyURL string) (*cursorpkg.TokenResponse, error)
}

// CursorOAuthTokenService is the narrow refresh port used by the Cursor token
// provider and refresher (mirrors GrokOAuthTokenService).
type CursorOAuthTokenService interface {
	RefreshAccountToken(ctx context.Context, account *Account) (*CursorTokenInfo, error)
	BuildAccountCredentials(tokenInfo *CursorTokenInfo) map[string]any
}

// CursorTokenInfo is the normalized outcome of any Cursor credential import or
// refresh (cookie / API key / deep link).
type CursorTokenInfo struct {
	AccessToken  string
	RefreshToken string
	APIKey       string
	UserID       string
	BaseURL      string
	ExpiresAt    int64 // unix seconds; 0 when unknown
	Source       string
	// WebSessionToken is the WorkosCursorSessionToken cookie the credential was
	// derived from. It is kept so the deep-link upgrade can be replayed after
	// the client token (and its refresh token) stop working.
	WebSessionToken string
}

// CursorOAuthService orchestrates Cursor credential import and refresh over the
// CursorOAuthClient HTTP boundary.
type CursorOAuthService struct {
	proxyRepo   ProxyRepository
	oauthClient CursorOAuthClient
	config      *config.Config
}

func NewCursorOAuthService(proxyRepo ProxyRepository, oauthClient CursorOAuthClient, configs ...*config.Config) *CursorOAuthService {
	svc := &CursorOAuthService{
		proxyRepo:   proxyRepo,
		oauthClient: oauthClient,
	}
	if len(configs) > 0 {
		svc.config = configs[0]
	}
	return svc
}

type CursorOAuthCapabilities struct {
	// DeepLinkEnabled reports whether the PKCE deep-link login flow can run
	// (always true; the browser step is operator-driven).
	DeepLinkEnabled bool `json:"deep_link_enabled"`
	// APIKeyImportEnabled reports whether crsr_ API key import is available.
	APIKeyImportEnabled bool `json:"api_key_import_enabled"`
	// CookieImportEnabled reports whether WorkosCursorSessionToken cookie import
	// is available.
	CookieImportEnabled bool `json:"cookie_import_enabled"`
}

func (s *CursorOAuthService) GetCapabilities() CursorOAuthCapabilities {
	return CursorOAuthCapabilities{
		DeepLinkEnabled:     true,
		APIKeyImportEnabled: true,
		CookieImportEnabled: true,
	}
}

// CursorAuthURLResult carries the deep-link challenge material back to the
// admin UI so it can open the browser page and poll for completion.
type CursorAuthURLResult struct {
	AuthURL  string `json:"auth_url"`
	UUID     string `json:"uuid"`
	Verifier string `json:"verifier"`
}

// GenerateAuthURL builds a loginDeepControl PKCE challenge. The verifier is
// returned to the caller (never persisted) so Poll can complete the exchange.
func (s *CursorOAuthService) GenerateAuthURL(ctx context.Context) (*CursorAuthURLResult, error) {
	verifier, challenge, id, err := cursorpkg.NewDeepLinkChallenge()
	if err != nil {
		return nil, infraerrors.Newf(http.StatusInternalServerError, "CURSOR_OAUTH_CHALLENGE_FAILED", "failed to generate challenge: %v", err)
	}
	return &CursorAuthURLResult{
		AuthURL:  cursorpkg.BuildLoginDeepControlURL(challenge, id),
		UUID:     id,
		Verifier: verifier,
	}, nil
}

// Poll performs a single poll of the deep-link login flow.
func (s *CursorOAuthService) Poll(ctx context.Context, id, verifier string, proxyID *int64) (*CursorTokenInfo, error) {
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	verifier = strings.TrimSpace(verifier)
	if id == "" || verifier == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_INVALID_INPUT", "uuid and verifier are required")
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	resp, err := s.oauthClient.PollDeepLink(ctx, id, verifier, proxyURL)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusAccepted, "CURSOR_OAUTH_PENDING", "login has not completed yet")
	}
	return s.tokenInfoFromResponse(resp, "", cursorpkg.CredentialSourceDeepLink), nil
}

// ImportFromAPIKey exchanges a crsr_ user API key for a session token and
// returns a token info that retains the API key for later re-exchange.
func (s *CursorOAuthService) ImportFromAPIKey(ctx context.Context, apiKey string, proxyID *int64) (*CursorTokenInfo, error) {
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_NO_API_KEY", "api_key is required")
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	resp, err := s.oauthClient.ExchangeUserAPIKey(ctx, apiKey, proxyURL)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_INVALID_TOKEN_RESPONSE", "cursor api key exchange returned no access token")
	}
	info := s.tokenInfoFromResponse(resp, apiKey, cursorpkg.CredentialSourceAPIKey)
	// The refresh token returned by the API-key exchange cannot be replayed at
	// /oauth/token; drop it so the refresher always re-exchanges the API key.
	info.RefreshToken = ""
	return info, nil
}

// ImportFromCookie parses a WorkosCursorSessionToken cookie ("userId::JWT")
// into a token info. No network call is made; the JWT is used as-is.
//
// A web cookie cannot drive StreamUnifiedChatWithTools on its own, so the
// cookie is also retained as web_session_token: the token provider upgrades it
// into a client credential the first time the account is scheduled.
func (s *CursorOAuthService) ImportFromCookie(ctx context.Context, cookie string) (*CursorTokenInfo, error) {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_NO_COOKIE", "cookie is required")
	}
	jwt, uid := cursorpkg.ParseToken(cookie)
	if strings.TrimSpace(jwt) == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_INVALID_COOKIE", "cookie did not contain a JWT")
	}
	info := &CursorTokenInfo{
		AccessToken: jwt,
		UserID:      uid,
		Source:      cursorpkg.CredentialSourceCookie,
	}
	if cursorpkg.IsWebSessionToken(jwt) {
		info.WebSessionToken = cursorpkg.NormalizeSessionCookie(cookie)
	}
	if exp, ok := cursorpkg.JWTExpiry(jwt); ok {
		info.ExpiresAt = exp.Unix()
	}
	return info, nil
}

// RefreshToken exchanges a raw deep-link refresh token for a fresh session
// token (admin validation path; account-less).
func (s *CursorOAuthService) RefreshToken(ctx context.Context, refreshToken string, proxyID *int64) (*CursorTokenInfo, error) {
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_NO_REFRESH_TOKEN", "refresh_token is required")
	}
	proxyURL, err := s.proxyURL(ctx, proxyID)
	if err != nil {
		return nil, err
	}
	resp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_INVALID_TOKEN_RESPONSE", "cursor token refresh returned no access token")
	}
	info := s.tokenInfoFromResponse(resp, "", cursorpkg.CredentialSourceDeepLink)
	if info.RefreshToken == "" {
		info.RefreshToken = refreshToken
	}
	return info, nil
}

// RefreshAccountToken refreshes an existing Cursor account's session token,
// preferring the API-key exchange path, then the deep-link refresh token, and
// finally the stored WorkosCursorSessionToken cookie.
//
// The cookie is last because it is the slowest path (a full headless
// handshake), but it is also the only one a cookie-imported account has: such
// an account starts with a "web" access token that AvailableModels accepts and
// StreamUnifiedChatWithTools rejects, so its very first refresh is this
// upgrade.
func (s *CursorOAuthService) RefreshAccountToken(ctx context.Context, account *Account) (*CursorTokenInfo, error) {
	if account == nil || account.Platform != PlatformCursor {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_INVALID_ACCOUNT", "account is not a Cursor account")
	}
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}

	// Store and replay the cookie in the canonical browser form so a credential
	// imported as "userId::JWT" and one pasted straight from devtools produce
	// the same stored value.
	webSession := cursorpkg.NormalizeSessionCookie(account.GetCursorWebSessionToken())

	apiKey := strings.TrimSpace(account.GetCursorAPIKey())
	if apiKey != "" {
		resp, err := s.oauthClient.ExchangeUserAPIKey(ctx, apiKey, proxyURL)
		if err != nil {
			return nil, err
		}
		if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
			return nil, infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_INVALID_TOKEN_RESPONSE", "cursor api key exchange returned no access token")
		}
		info := s.tokenInfoFromResponse(resp, apiKey, cursorpkg.CredentialSourceAPIKey)
		info.RefreshToken = strings.TrimSpace(account.GetCursorRefreshToken())
		info.WebSessionToken = webSession
		return info, nil
	}

	refreshToken := strings.TrimSpace(account.GetCursorRefreshToken())
	if refreshToken != "" {
		info, err := s.refreshWithDeepLinkToken(ctx, refreshToken, proxyURL)
		if err == nil {
			info.WebSessionToken = webSession
			return info, nil
		}
		// A revoked deep-link refresh token is recoverable as long as the
		// browser session behind it is alive: mint a brand new client
		// credential instead of retiring the account.
		if webSession == "" {
			return nil, err
		}
	}

	if webSession != "" {
		return s.upgradeWebSession(ctx, webSession, proxyURL)
	}
	return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_NO_REFRESH_TOKEN", "no api_key, refresh_token or web session available")
}

func (s *CursorOAuthService) refreshWithDeepLinkToken(ctx context.Context, refreshToken, proxyURL string) (*CursorTokenInfo, error) {
	resp, err := s.oauthClient.RefreshToken(ctx, refreshToken, proxyURL)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_INVALID_TOKEN_RESPONSE", "cursor token refresh returned no access token")
	}
	info := s.tokenInfoFromResponse(resp, "", cursorpkg.CredentialSourceDeepLink)
	if info.RefreshToken == "" {
		info.RefreshToken = refreshToken
	}
	return info, nil
}

// upgradeWebSession turns a browser cookie into a chat-capable client
// credential. The cookie is carried into the result so a later refresh can
// replay the upgrade.
func (s *CursorOAuthService) upgradeWebSession(ctx context.Context, webSession, proxyURL string) (*CursorTokenInfo, error) {
	resp, err := s.oauthClient.ExchangeWebSession(ctx, webSession, proxyURL)
	if err != nil {
		return nil, err
	}
	if resp == nil || strings.TrimSpace(resp.AccessToken) == "" {
		return nil, infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_INVALID_TOKEN_RESPONSE", "cursor web session exchange returned no access token")
	}
	if cursorpkg.IsWebSessionToken(resp.AccessToken) {
		return nil, infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_WEB_SESSION_NOT_UPGRADED", "cursor returned another web token instead of a client token")
	}
	info := s.tokenInfoFromResponse(resp, "", cursorpkg.CredentialSourceCookie)
	info.WebSessionToken = webSession
	if info.UserID == "" {
		info.UserID = cursorpkg.ExtractUserID(webSession)
	}
	return info, nil
}

// UpgradeWebSession runs the cookie upgrade for an account on demand (admin
// validation path). It never persists; the caller decides what to store.
func (s *CursorOAuthService) UpgradeWebSession(ctx context.Context, account *Account) (*CursorTokenInfo, error) {
	if account == nil || account.Platform != PlatformCursor {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_INVALID_ACCOUNT", "account is not a Cursor account")
	}
	if err := s.requireOAuthClient(); err != nil {
		return nil, err
	}
	webSession := cursorpkg.NormalizeSessionCookie(account.GetCursorWebSessionToken())
	if webSession == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_NO_COOKIE", "account has no stored web session token")
	}
	proxyURL, err := s.proxyURL(ctx, account.ProxyID)
	if err != nil {
		return nil, err
	}
	return s.upgradeWebSession(ctx, webSession, proxyURL)
}

// BuildAccountCredentials renders a CursorTokenInfo into the stored credentials
// shape agreed with the frontend: access_token / refresh_token / api_key /
// base_url plus expires_at and credential_source metadata.
func (s *CursorOAuthService) BuildAccountCredentials(tokenInfo *CursorTokenInfo) map[string]any {
	if tokenInfo == nil {
		return nil
	}
	creds := map[string]any{
		"access_token": tokenInfo.AccessToken,
	}
	expiresAt := tokenInfo.ExpiresAt
	if expiresAt <= 0 {
		if exp, ok := cursorpkg.JWTExpiry(tokenInfo.AccessToken); ok {
			expiresAt = exp.Unix()
		}
	}
	if expiresAt <= 0 {
		expiresAt = time.Now().Add(cursorDefaultAccessTokenTTL).Unix()
	}
	creds["expires_at"] = time.Unix(expiresAt, 0).UTC().Format(time.RFC3339)
	if tokenInfo.RefreshToken != "" {
		creds["refresh_token"] = tokenInfo.RefreshToken
	}
	if tokenInfo.APIKey != "" {
		creds["api_key"] = tokenInfo.APIKey
	}
	if tokenInfo.WebSessionToken != "" {
		creds["web_session_token"] = tokenInfo.WebSessionToken
	}
	uid := strings.TrimSpace(tokenInfo.UserID)
	if uid == "" {
		uid = cursorpkg.ExtractUserID(tokenInfo.AccessToken)
	}
	if uid != "" {
		creds["user_id"] = uid
	}
	if tokenInfo.BaseURL != "" {
		creds["base_url"] = tokenInfo.BaseURL
	}
	if tokenInfo.Source != "" {
		creds["credential_source"] = tokenInfo.Source
	}
	return creds
}

func (s *CursorOAuthService) tokenInfoFromResponse(resp *cursorpkg.TokenResponse, apiKey, source string) *CursorTokenInfo {
	info := &CursorTokenInfo{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		APIKey:       strings.TrimSpace(apiKey),
		UserID:       cursorpkg.ExtractUserID(resp.AccessToken),
		Source:       source,
	}
	if resp.ExpiresIn > 0 {
		info.ExpiresAt = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second).Unix()
	} else if exp, ok := cursorpkg.JWTExpiry(resp.AccessToken); ok {
		info.ExpiresAt = exp.Unix()
	}
	return info
}

func (s *CursorOAuthService) requireOAuthClient() error {
	if s == nil || s.oauthClient == nil {
		return infraerrors.New(http.StatusInternalServerError, "CURSOR_OAUTH_CLIENT_NOT_CONFIGURED", "cursor oauth client is not configured")
	}
	return nil
}

func (s *CursorOAuthService) proxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s.proxyRepo == nil {
		return "", infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_PROXY_NOT_AVAILABLE", "proxy repository is not available")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		if errors.Is(err, ErrProxyNotFound) {
			return "", infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
		}
		return "", infraerrors.New(http.StatusServiceUnavailable, "CURSOR_OAUTH_PROXY_LOOKUP_FAILED", "proxy lookup is temporarily unavailable")
	}
	if proxy == nil {
		return "", infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_PROXY_NOT_FOUND", "configured proxy was not found")
	}
	return proxy.URL(), nil
}
