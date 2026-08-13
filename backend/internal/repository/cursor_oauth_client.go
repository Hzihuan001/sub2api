package repository

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	sharedhttp "github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

// Budget for one headless web-session upgrade. The poll runs server-side right
// after the approval lands, so it normally succeeds on the first or second
// attempt; the ceiling exists so a stuck handshake fails the request instead of
// holding the account's refresh lock.
const (
	cursorWebSessionRequestTimeout = 30 * time.Second
	cursorWebSessionPollAttempts   = 12
	cursorWebSessionPollInterval   = 500 * time.Millisecond
)

// cursorOAuthClient talks to Cursor's authentication endpoints on api2.cursor.sh.
// Lifecycle traffic (exchange / refresh / poll) always uses the official host
// regardless of any per-account base_url forwarding override.
type cursorOAuthClient struct {
	baseURL string
}

// NewCursorOAuthClient builds the Cursor auth HTTP client bound to the official
// api2.cursor.sh host.
func NewCursorOAuthClient() service.CursorOAuthClient {
	return &cursorOAuthClient{baseURL: cursorpkg.DefaultBaseURL}
}

func (c *cursorOAuthClient) endpoint(path string) string {
	return strings.TrimRight(c.baseURL, "/") + path
}

// ExchangeUserAPIKey exchanges a crsr_ user API key for a session access token.
func (c *cursorOAuthClient) ExchangeUserAPIKey(ctx context.Context, apiKey, proxyURL string) (*cursorpkg.TokenResponse, error) {
	client, err := getSharedReqClient(reqClientOptions{ProxyURL: proxyURL, Timeout: 60 * time.Second, ForceHTTP2: true})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	var tokenResp cursorpkg.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "sub2api-cursor-oauth/1.0").
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetBody(map[string]any{"apiKey": apiKey}).
		SetSuccessResult(&tokenResp).
		Post(c.endpoint(cursorpkg.EndpointExchangeUserAPIKey))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		return nil, cursorOAuthStatusError("CURSOR_OAUTH_API_KEY_EXCHANGE_FAILED", "api key exchange failed", resp.StatusCode, resp.String())
	}
	return &tokenResp, nil
}

// RefreshToken exchanges a deep-link refresh token for a fresh session token.
func (c *cursorOAuthClient) RefreshToken(ctx context.Context, refreshToken, proxyURL string) (*cursorpkg.TokenResponse, error) {
	client, err := getSharedReqClient(reqClientOptions{ProxyURL: proxyURL, Timeout: 60 * time.Second, ForceHTTP2: true})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	var tokenResp cursorpkg.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "sub2api-cursor-oauth/1.0").
		SetHeader("Content-Type", "application/json").
		SetHeader("Accept", "application/json").
		SetBody(map[string]any{"refreshToken": refreshToken, "grant_type": "refresh_token"}).
		SetSuccessResult(&tokenResp).
		Post(c.endpoint(cursorpkg.EndpointOAuthToken))
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		return nil, cursorOAuthStatusError("CURSOR_OAUTH_TOKEN_REFRESH_FAILED", "token refresh failed", resp.StatusCode, resp.String())
	}
	return &tokenResp, nil
}

// ExchangeWebSession upgrades a WorkosCursorSessionToken browser cookie into a
// client credential by running the loginDeepControl handshake headlessly. The
// approval leg targets www.cursor.com (the website API) while the poll targets
// api2.cursor.sh, both through the account's proxy so the whole credential
// lifecycle shares the chat traffic's egress IP.
func (c *cursorOAuthClient) ExchangeWebSession(ctx context.Context, workosSessionToken, proxyURL string) (*cursorpkg.TokenResponse, error) {
	httpClient, err := sharedhttp.GetClient(sharedhttp.Options{
		ProxyURL:              proxyURL,
		Timeout:               cursorWebSessionRequestTimeout,
		ResponseHeaderTimeout: 20 * time.Second,
	})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	token, err := cursorpkg.ExchangeWebSessionWithOptions(ctx, workosSessionToken, cursorpkg.ExchangeOptions{
		HTTPClient:   httpClient,
		PollAttempts: cursorWebSessionPollAttempts,
		PollInterval: cursorWebSessionPollInterval,
	})
	if err != nil {
		return nil, cursorWebSessionExchangeError(err)
	}
	return token, nil
}

// cursorWebSessionExchangeError maps the package-level exchange failures onto
// the infra error contract so the failover classifier can tell "this cookie is
// dead" (retry another account) from "the handshake did not land in time".
func cursorWebSessionExchangeError(err error) error {
	switch {
	case errors.Is(err, cursorpkg.ErrWebSessionUnauthorized):
		return infraerrors.New(http.StatusUnauthorized, "CURSOR_OAUTH_WEB_SESSION_UNAUTHORIZED",
			"cursor web session cookie was rejected; re-import WorkosCursorSessionToken")
	case errors.Is(err, cursorpkg.ErrWebSessionNotUpgraded):
		return infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_WEB_SESSION_NOT_UPGRADED",
			"cursor returned another web token instead of a client token")
	case errors.Is(err, cursorpkg.ErrDeepLoginPending):
		return infraerrors.New(http.StatusBadGateway, "CURSOR_OAUTH_WEB_SESSION_PENDING",
			"cursor deep-link login did not complete in time")
	default:
		return infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_WEB_SESSION_EXCHANGE_FAILED",
			"cursor web session exchange failed: %v", logredact.RedactText(err.Error()))
	}
}

// PollDeepLink polls the loginDeepControl flow once. A 2xx with no access token
// (login still pending) surfaces as a nil-token response so callers can retry.
func (c *cursorOAuthClient) PollDeepLink(ctx context.Context, id, verifier, proxyURL string) (*cursorpkg.TokenResponse, error) {
	client, err := getSharedReqClient(reqClientOptions{ProxyURL: proxyURL, Timeout: 60 * time.Second, ForceHTTP2: true})
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_CLIENT_INIT_FAILED", "create HTTP client: %v", err)
	}
	q := url.Values{}
	q.Set("uuid", id)
	q.Set("verifier", verifier)
	var tokenResp cursorpkg.TokenResponse
	resp, err := client.R().
		SetContext(ctx).
		SetHeader("User-Agent", "sub2api-cursor-oauth/1.0").
		SetHeader("Accept", "application/json").
		SetSuccessResult(&tokenResp).
		Get(c.endpoint(cursorpkg.EndpointAuthPoll) + "?" + q.Encode())
	if err != nil {
		return nil, infraerrors.Newf(http.StatusBadGateway, "CURSOR_OAUTH_REQUEST_FAILED", "request failed: %v", err)
	}
	if !resp.IsSuccessState() {
		return nil, cursorOAuthStatusError("CURSOR_OAUTH_POLL_FAILED", "deep-link poll failed", resp.StatusCode, resp.String())
	}
	return &tokenResp, nil
}

func cursorOAuthStatusError(code, message string, statusCode int, body string) error {
	httpStatus := http.StatusBadGateway
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		httpStatus = statusCode
	}
	return infraerrors.Newf(httpStatus, code, "%s: status %d, body: %s", message, statusCode, logredact.RedactText(body))
}
