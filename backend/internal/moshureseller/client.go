package moshureseller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type protocolClient struct {
	http *http.Client
}

type apiEnvelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func newProtocolClient() *protocolClient {
	return &protocolClient{http: &http.Client{Timeout: 30 * time.Second}}
}

func validateBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("%w: invalid Moshu URL", ErrInvalidInput)
	}
	allowHTTP := envEnabled("MOSHU_RESELLER_ALLOW_HTTP")
	localhost := parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1"
	if parsed.Scheme != "https" && !allowHTTP && !localhost {
		return "", fmt.Errorf("%w: Moshu URL must use HTTPS", ErrInvalidInput)
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *protocolClient) exchange(ctx context.Context, baseURL, code, instanceID string) (*EnrollmentExchange, error) {
	var result EnrollmentExchange
	err := c.doJSON(ctx, http.MethodPost, baseURL+"/api/v1/reseller/v1/enrollments/exchange", "", "", map[string]any{
		"enrollment_code": code, "instance_id": instanceID,
	}, &result, nil)
	return &result, err
}

func (c *protocolClient) refresh(ctx context.Context, baseURL, refreshToken, instanceID string) (string, string, int64, error) {
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	err := c.doJSON(ctx, http.MethodPost, baseURL+"/api/v1/reseller/v1/tokens/refresh", "", "", map[string]any{
		"refresh_token": refreshToken, "instance_id": instanceID,
	}, &result, nil)
	return result.AccessToken, result.RefreshToken, result.ExpiresIn, err
}

func (c *protocolClient) catalog(ctx context.Context, baseURL, token, etag string) (*RemoteCatalog, string, bool, error) {
	var result RemoteCatalog
	responseETag := ""
	notModified := false
	err := c.doJSON(ctx, http.MethodGet, baseURL+"/api/v1/reseller/v1/catalog", token, etag, nil, &result, func(response *http.Response) {
		responseETag = response.Header.Get("ETag")
		notModified = response.StatusCode == http.StatusNotModified
	})
	return &result, responseETag, notModified, err
}

func (c *protocolClient) rotate(ctx context.Context, baseURL, token string, productID int64) (*RemoteCredential, error) {
	var result RemoteCredential
	err := c.doJSON(ctx, http.MethodPost, baseURL+"/api/v1/reseller/v1/credentials/rotate", token, "", map[string]any{"product_id": productID}, &result, nil)
	return &result, err
}

func (c *protocolClient) settlements(ctx context.Context, baseURL, token string, afterID int64) (*RemoteSettlementPage, error) {
	var result RemoteSettlementPage
	endpoint := fmt.Sprintf("%s/api/v1/reseller/v1/settlements?after_id=%d&limit=500", baseURL, afterID)
	err := c.doJSON(ctx, http.MethodGet, endpoint, token, "", nil, &result, nil)
	return &result, err
}

func (c *protocolClient) doJSON(ctx context.Context, method, endpoint, token, etag string, body any, out any, inspect func(*http.Response)) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if inspect != nil {
		inspect(resp)
	}
	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Moshu returned HTTP %d", resp.StatusCode)
	}
	envelope := apiEnvelope[json.RawMessage]{}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("invalid Moshu response")
	}
	if envelope.Code != 0 {
		return fmt.Errorf("Moshu rejected request: %s", envelope.Message)
	}
	if out != nil && len(envelope.Data) > 0 {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("invalid Moshu response data")
		}
	}
	return nil
}

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
