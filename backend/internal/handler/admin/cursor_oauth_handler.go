package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const cursorSSOImportConcurrency = 3

// CursorOAuthHandler mirrors GrokOAuthHandler for the Cursor platform. The
// route paths and request/response shapes follow the frontend contract in
// frontend/src/api/admin/cursor.ts (grok paths with grok→cursor).
type CursorOAuthHandler struct {
	cursorOAuthService *service.CursorOAuthService
	adminService       service.AdminService
}

func NewCursorOAuthHandler(
	cursorOAuthService *service.CursorOAuthService,
	adminService service.AdminService,
) *CursorOAuthHandler {
	return &CursorOAuthHandler{
		cursorOAuthService: cursorOAuthService,
		adminService:       adminService,
	}
}

// cursorTokenInfoResponse is the wire shape shared by every token-returning
// endpoint (frontend CursorTokenInfo).
type cursorTokenInfoResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	Sub          string `json:"sub,omitempty"`
	Status       string `json:"status,omitempty"`
}

func cursorTokenInfoResponseFrom(info *service.CursorTokenInfo) *cursorTokenInfoResponse {
	if info == nil {
		return nil
	}
	return &cursorTokenInfoResponse{
		AccessToken:  info.AccessToken,
		RefreshToken: info.RefreshToken,
		APIKey:       info.APIKey,
		ExpiresAt:    info.ExpiresAt,
		Sub:          info.UserID,
	}
}

func (h *CursorOAuthHandler) GetCapabilities(c *gin.Context) {
	caps := h.cursorOAuthService.GetCapabilities()
	response.Success(c, gin.H{
		// Cursor has no first-party password login; the frontend gates the
		// password panel on this flag.
		"password_auth_enabled":  false,
		"deep_link_enabled":      caps.DeepLinkEnabled,
		"api_key_import_enabled": caps.APIKeyImportEnabled,
		"cookie_import_enabled":  caps.CookieImportEnabled,
	})
}

type CursorGenerateAuthURLRequest struct {
	ProxyID     *int64 `json:"proxy_id"`
	RedirectURI string `json:"redirect_uri"`
}

// GenerateAuthURL creates a loginDeepControl PKCE challenge. The response maps
// the challenge onto the Grok-shaped contract: session_id carries the deep-link
// UUID and state carries the PKCE verifier (both must be echoed back to poll).
func (h *CursorOAuthHandler) GenerateAuthURL(c *gin.Context) {
	var req CursorGenerateAuthURLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		req = CursorGenerateAuthURLRequest{}
	}
	result, err := h.cursorOAuthService.GenerateAuthURL(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"auth_url":   result.AuthURL,
		"session_id": result.UUID,
		"state":      result.Verifier,
	})
}

type CursorPollRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	State     string `json:"state" binding:"required"`
	ProxyID   *int64 `json:"proxy_id"`
}

// Poll checks the deep-link login once. While the user has not confirmed in
// the browser it returns HTTP 200 with {"status":"pending"} per the frontend
// polling contract.
func (h *CursorOAuthHandler) Poll(c *gin.Context) {
	var req CursorPollRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	info, err := h.cursorOAuthService.Poll(c.Request.Context(), req.SessionID, req.State, req.ProxyID)
	if err != nil {
		if status := infraerrors.FromError(err); status != nil && status.Reason == "CURSOR_OAUTH_PENDING" {
			response.Success(c, &cursorTokenInfoResponse{Status: "pending"})
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cursorTokenInfoResponseFrom(info))
}

type CursorExchangeCodeRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
	State     string `json:"state"`
	ProxyID   *int64 `json:"proxy_id"`
}

// ExchangeCode is the manual-paste path kept for structural parity with Grok.
// The pasted "code" is a Cursor credential: a crsr_ User API Key (exchanged
// upstream) or a WorkosCursorSessionToken cookie (parsed locally).
func (h *CursorOAuthHandler) ExchangeCode(c *gin.Context) {
	var req CursorExchangeCodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	info, err := h.importCursorCredential(c.Request.Context(), req.Code, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cursorTokenInfoResponseFrom(info))
}

type CursorRefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
	RT           string `json:"rt"`
	ProxyID      *int64 `json:"proxy_id"`
}

func (h *CursorOAuthHandler) RefreshToken(c *gin.Context) {
	var req CursorRefreshTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		refreshToken = strings.TrimSpace(req.RT)
	}
	if refreshToken == "" {
		response.BadRequest(c, "refresh_token is required")
		return
	}
	info, err := h.cursorOAuthService.RefreshToken(c.Request.Context(), refreshToken, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cursorTokenInfoResponseFrom(info))
}

type CursorSSOTokenRequest struct {
	SSOToken string `json:"sso_token"`
	ProxyID  *int64 `json:"proxy_id"`
}

// ValidateSSOToken converts a pasted credential (WorkosCursorSessionToken
// cookie or crsr_ API key) into account credentials. Never echoes the raw
// input back.
func (h *CursorOAuthHandler) ValidateSSOToken(c *gin.Context) {
	var req CursorSSOTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	info, err := h.importCursorCredential(c.Request.Context(), req.SSOToken, req.ProxyID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cursorTokenInfoResponseFrom(info))
}

// AuthorizePassword is a structural mirror of the Grok endpoint. Cursor has no
// first-party password login, so it always rejects (capabilities advertise
// password_auth_enabled=false).
func (h *CursorOAuthHandler) AuthorizePassword(c *gin.Context) {
	response.Error(c, http.StatusBadRequest, "CURSOR_OAUTH_PASSWORD_UNSUPPORTED: password login is not supported for Cursor")
}

func (h *CursorOAuthHandler) importCursorCredential(ctx context.Context, credential string, proxyID *int64) (*service.CursorTokenInfo, error) {
	credential = strings.TrimSpace(credential)
	if credential == "" {
		return nil, infraerrors.New(http.StatusBadRequest, "CURSOR_OAUTH_NO_CREDENTIAL", "credential is required")
	}
	if cursorpkg.IsUserAPIKey(credential) {
		return h.cursorOAuthService.ImportFromAPIKey(ctx, credential, proxyID)
	}
	return h.cursorOAuthService.ImportFromCookie(ctx, credential)
}

func (h *CursorOAuthHandler) RefreshAccountToken(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformCursor {
		response.BadRequest(c, "Account platform does not match Cursor OAuth endpoint")
		return
	}
	if !account.IsOAuth() {
		response.BadRequest(c, "Cannot refresh non-OAuth account credentials")
		return
	}
	tokenInfo, err := h.cursorOAuthService.RefreshAccountToken(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	newCredentials := h.cursorOAuthService.BuildAccountCredentials(tokenInfo)
	newCredentials = service.MergeCredentials(account.Credentials, newCredentials)
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		newCredentials["base_url"] = baseURL
	}
	updatedAccount, err := h.adminService.UpdateAccount(c.Request.Context(), accountID, &service.UpdateAccountInput{
		Credentials: newCredentials,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.AccountFromService(updatedAccount))
}

type CursorSSOToOAuthRequest struct {
	SSOTokens          []string       `json:"sso_tokens"`
	SSOToken           string         `json:"sso_token"`
	Name               string         `json:"name"`
	Notes              *string        `json:"notes"`
	ProxyID            *int64         `json:"proxy_id"`
	GroupIDs           []int64        `json:"group_ids"`
	Credentials        map[string]any `json:"credentials"`
	Extra              map[string]any `json:"extra"`
	Concurrency        int            `json:"concurrency"`
	LoadFactor         *int           `json:"load_factor"`
	Priority           int            `json:"priority"`
	RateMultiplier     *float64       `json:"rate_multiplier"`
	ExpiresAt          *int64         `json:"expires_at"`
	AutoPauseOnExpired *bool          `json:"auto_pause_on_expired"`
}

type CursorSSOToOAuthItemResult struct {
	Index   int          `json:"index"`
	Name    string       `json:"name,omitempty"`
	Account *dto.Account `json:"account,omitempty"`
	Error   string       `json:"error,omitempty"`
}

type CursorSSOToOAuthResponse struct {
	Created []CursorSSOToOAuthItemResult `json:"created"`
	Failed  []CursorSSOToOAuthItemResult `json:"failed"`
}

type cursorSSOImportJob struct {
	index int
	token string
}

type cursorSSOImportWorkerResult struct {
	created bool
	item    CursorSSOToOAuthItemResult
}

// CreateAccountsFromSSO batch-imports pasted Cursor credentials (cookies or
// crsr_ API keys) as OAuth accounts, mirroring the Grok SSO import contract.
func (h *CursorOAuthHandler) CreateAccountsFromSSO(c *gin.Context) {
	var req CursorSSOToOAuthRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	tokens := normalizeCursorImportTokens(req.SSOTokens, req.SSOToken)
	if len(tokens) == 0 {
		response.BadRequest(c, "sso_tokens is required")
		return
	}

	ctx := c.Request.Context()
	workerCount := cursorSSOImportConcurrency
	if len(tokens) < workerCount {
		workerCount = len(tokens)
	}
	jobs := make(chan cursorSSOImportJob)
	items := make([]cursorSSOImportWorkerResult, len(tokens))
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				items[job.index] = h.safeCreateCursorAccountFromToken(ctx, req, job.token, job.index+1, len(tokens))
			}
		}()
	}
	for i, token := range tokens {
		jobs <- cursorSSOImportJob{index: i, token: token}
	}
	close(jobs)
	wg.Wait()

	result := CursorSSOToOAuthResponse{
		Created: make([]CursorSSOToOAuthItemResult, 0, len(tokens)),
		Failed:  make([]CursorSSOToOAuthItemResult, 0),
	}
	for _, item := range items {
		if item.created {
			result.Created = append(result.Created, item.item)
		} else {
			result.Failed = append(result.Failed, item.item)
		}
	}
	response.Success(c, result)
}

func (h *CursorOAuthHandler) safeCreateCursorAccountFromToken(ctx context.Context, req CursorSSOToOAuthRequest, token string, index, total int) (result cursorSSOImportWorkerResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("cursor_sso_import_worker_panic", "index", index, "recover", recovered)
			result = cursorSSOImportWorkerResult{
				item: CursorSSOToOAuthItemResult{
					Index: index,
					Error: fmt.Sprintf("internal worker panic: %v", recovered),
				},
			}
		}
	}()
	return h.createCursorAccountFromToken(ctx, req, token, index, total)
}

func (h *CursorOAuthHandler) createCursorAccountFromToken(ctx context.Context, req CursorSSOToOAuthRequest, token string, index, total int) cursorSSOImportWorkerResult {
	tokenInfo, err := h.importCursorCredential(ctx, token, req.ProxyID)
	if err != nil {
		return cursorSSOImportWorkerResult{item: CursorSSOToOAuthItemResult{Index: index, Error: cursorImportErrorMessage(err)}}
	}

	credentials := cursorSSOImportCredentials(h.cursorOAuthService.BuildAccountCredentials(tokenInfo), req.Credentials)
	name := cursorSSOImportAccountName(req.Name, tokenInfo, index, total)
	account, err := h.adminService.CreateAccount(ctx, &service.CreateAccountInput{
		Name:               name,
		Notes:              req.Notes,
		Platform:           service.PlatformCursor,
		Type:               service.AccountTypeOAuth,
		Credentials:        credentials,
		Extra:              cloneGrokSSOMap(req.Extra),
		ProxyID:            req.ProxyID,
		Concurrency:        req.Concurrency,
		LoadFactor:         req.LoadFactor,
		Priority:           req.Priority,
		RateMultiplier:     req.RateMultiplier,
		GroupIDs:           append([]int64(nil), req.GroupIDs...),
		ExpiresAt:          req.ExpiresAt,
		AutoPauseOnExpired: req.AutoPauseOnExpired,
	})
	if err != nil {
		return cursorSSOImportWorkerResult{item: CursorSSOToOAuthItemResult{Index: index, Name: name, Error: cursorImportErrorMessage(err)}}
	}
	return cursorSSOImportWorkerResult{
		created: true,
		item: CursorSSOToOAuthItemResult{
			Index:   index,
			Name:    name,
			Account: dto.AccountFromService(account),
		},
	}
}

// cursorSSOImportCredentials merges the exchanged credentials with operator
// config from the import request. Token fields always come from
// BuildAccountCredentials; only whitelisted operator keys pass through.
func cursorSSOImportCredentials(built map[string]any, reqCredentials map[string]any) map[string]any {
	allowedReqKeys := map[string]struct{}{
		"base_url": {}, "model_mapping": {},
		"header_override": {}, "header_overrides": {}, "header_override_enabled": {},
		"custom_headers": {},
	}
	ops := map[string]any{}
	for k, v := range reqCredentials {
		if _, ok := allowedReqKeys[k]; !ok {
			continue
		}
		if service.IsSensitiveCredentialKey(k) {
			continue
		}
		ops[k] = v
	}
	credentials := service.MergeCredentials(ops, built)
	for k := range credentials {
		if service.IsSensitiveCredentialKey(k) {
			// web_session_token is kept alongside the tokens: it is the only
			// thing that can mint a new client credential once the imported
			// cookie's access token is replaced.
			if k == "access_token" || k == "refresh_token" || k == "api_key" || k == "web_session_token" {
				continue
			}
			delete(credentials, k)
		}
	}
	if reqBaseURL, ok := reqCredentials["base_url"].(string); ok && strings.TrimSpace(reqBaseURL) != "" {
		credentials["base_url"] = strings.TrimSpace(reqBaseURL)
	}
	return service.SanitizeStoredCredentials(service.PlatformCursor, credentials)
}

func normalizeCursorImportTokens(tokens []string, single string) []string {
	items := make([]string, 0, len(tokens)+1)
	if strings.TrimSpace(single) != "" {
		items = append(items, single)
	}
	items = append(items, tokens...)
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		parts := strings.Split(strings.NewReplacer(",", "\n", "\r", "\n").Replace(item), "\n")
		for _, token := range parts {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			result = append(result, token)
		}
	}
	return result
}

func cursorSSOImportAccountName(base string, tokenInfo *service.CursorTokenInfo, index, total int) string {
	base = strings.TrimSpace(base)
	if base == "" && tokenInfo != nil && strings.TrimSpace(tokenInfo.UserID) != "" {
		base = "Cursor " + strings.TrimSpace(tokenInfo.UserID)
	}
	if base == "" {
		base = "Cursor OAuth Account"
	}
	if total > 1 {
		return base + " #" + strconv.Itoa(index)
	}
	return base
}

func cursorImportErrorMessage(err error) string {
	status := infraerrors.FromError(err)
	if status == nil {
		return ""
	}
	if status.Reason != "" {
		return status.Reason + ": " + status.Message
	}
	return status.Message
}
