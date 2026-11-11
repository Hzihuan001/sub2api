//go:build unit

package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"syscall"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type openAIImagesFailoverAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r openAIImagesFailoverAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r openAIImagesFailoverAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r openAIImagesFailoverAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r openAIImagesFailoverAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r openAIImagesFailoverAccountRepo) accountsForPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

type openAIImagesFailoverHTTPUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	accountIDs []int64
	responses  []*http.Response
	errs       []error
}

func (u *openAIImagesFailoverHTTPUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	callIndex := len(u.accountIDs)
	u.accountIDs = append(u.accountIDs, accountID)
	if callIndex < len(u.errs) && u.errs[callIndex] != nil {
		err := u.errs[callIndex]
		u.mu.Unlock()
		return nil, err
	}
	if callIndex < len(u.responses) {
		resp := u.responses[callIndex]
		u.mu.Unlock()
		return resp, nil
	}
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_img_failover"},
		},
		Body: io.NopCloser(bytes.NewBufferString(
			"data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"image backend unavailable\"}}\n\n",
		)),
	}, nil
}

func (u *openAIImagesFailoverHTTPUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func openAIImagesRetrySuccessResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{"req_img_retry"},
		},
		Body: io.NopCloser(bytes.NewBufferString("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_img_retry\",\"status\":\"completed\",\"output\":[{\"type\":\"image_generation_call\",\"result\":\"aW1hZ2U=\"}]}}\n\n")),
	}
}

func TestOpenAIGatewayHandlerImages_RetriesTransientServerErrorOnSameAccount(t *testing.T) {
	upstream := &openAIImagesFailoverHTTPUpstream{responses: []*http.Response{{
		StatusCode: 524,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString("{\"error\":{\"type\":\"server_error\",\"message\":\"gateway timeout\"}}")),
	}, openAIImagesRetrySuccessResponse()}}
	testOpenAIImagesSameAccountRetry(t, upstream, []int64{1, 1}, http.StatusOK, "failover")
}

func TestOpenAIGatewayHandlerImages_RetriesConnectionResetOnSameAccount(t *testing.T) {
	resetErr := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	upstream := &openAIImagesFailoverHTTPUpstream{
		errs:      []error{resetErr},
		responses: []*http.Response{nil, openAIImagesRetrySuccessResponse()},
	}
	testOpenAIImagesSameAccountRetry(t, upstream, []int64{1, 1}, http.StatusOK, "request_error")
}

func TestOpenAIGatewayHandlerImages_ConnectionResetExhaustionStaysOnSameAccount(t *testing.T) {
	resetErr := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	upstream := &openAIImagesFailoverHTTPUpstream{errs: []error{resetErr, resetErr, resetErr, resetErr}}
	testOpenAIImagesSameAccountRetry(t, upstream, []int64{1, 1, 1, 1}, http.StatusBadGateway, "request_error")
}

func TestOpenAIGatewayHandlerImages_HTTP2ConnectionFailureRetriesStayOnSameAccount(t *testing.T) {
	for _, message := range []string{
		"http2: client connection force closed via ClientConn.Close",
		"http2: client connection lost",
	} {
		t.Run(message, func(t *testing.T) {
			transportErr := &url.Error{Op: "Post", URL: "https://upstream.invalid/v1/responses", Err: errors.New(message)}
			t.Run("recovery", func(t *testing.T) {
				upstream := &openAIImagesFailoverHTTPUpstream{
					errs:      []error{transportErr},
					responses: []*http.Response{nil, openAIImagesRetrySuccessResponse()},
				}
				testOpenAIImagesSameAccountRetry(t, upstream, []int64{1, 1}, http.StatusOK, "request_error")
			})
			t.Run("exhaustion", func(t *testing.T) {
				upstream := &openAIImagesFailoverHTTPUpstream{errs: []error{transportErr, transportErr, transportErr, transportErr}}
				testOpenAIImagesSameAccountRetry(t, upstream, []int64{1, 1, 1, 1}, http.StatusBadGateway, "request_error")
			})
		})
	}
}

func testOpenAIImagesSameAccountRetry(t *testing.T, upstream *openAIImagesFailoverHTTPUpstream, wantCalls []int64, wantStatus int, wantEventKind string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	groupID := int64(3130)
	accounts := []service.Account{
		{
			ID:          1,
			Name:        "image-account-1",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    0,
			Credentials: map[string]any{"access_token": "token-1"},
		},
		{
			ID:          2,
			Name:        "image-account-2",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Status:      service.StatusActive,
			Schedulable: true,
			Concurrency: 0,
			Priority:    1,
			Credentials: map[string]any{"access_token": "token-2"},
		},
	}
	accountRepo := openAIImagesFailoverAccountRepo{accounts: accounts}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	concurrencyService := service.NewConcurrencyService(nil)
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	body := []byte(`{"model":"gpt-image-1","prompt":"draw a cat","quality":"high","size":"1536x1024"}`)
	core, observedLogs := observer.New(zap.DebugLevel)
	requestCtx := logger.IntoContext(context.Background(), zap.New(core))
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body)).WithContext(requestCtx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      99,
		GroupID: &groupID,
		Group: &service.Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
		User: &service.User{ID: 100},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 100, Concurrency: 0})

	handler.Images(c)

	accountSelectingLogs := observedLogs.FilterMessage("openai.images.account_selecting").All()
	require.Len(t, accountSelectingLogs, 1, "retries must not rerun account selection")
	loggedFields := make(map[string]string)
	for _, field := range accountSelectingLogs[0].Context {
		loggedFields[field.Key] = field.String
	}
	require.Equal(t, "high", loggedFields["img_quality"])
	require.Equal(t, "1536x1024", loggedFields["img_size"])
	require.NotContains(t, loggedFields, "prompt")

	require.Equal(t, wantCalls, upstream.calls(), "body=%s logs=%#v", rec.Body.String(), observedLogs.All())
	require.Equal(t, wantStatus, rec.Code)
	if wantStatus == http.StatusOK {
		require.Equal(t, "aW1hZ2U=", gjson.GetBytes(rec.Body.Bytes(), "data.0.b64_json").String())
	} else {
		require.Equal(t, "upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.type").String())
		require.NotContains(t, rec.Body.String(), "No available compatible accounts")
	}

	rawEvents, ok := c.Get(service.OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*service.OpsUpstreamErrorEvent)
	require.True(t, ok)
	wantEventCount := len(wantCalls)
	if wantStatus == http.StatusOK {
		wantEventCount--
	}
	require.Len(t, events, wantEventCount)
	for _, event := range events {
		require.Equal(t, wantEventKind, event.Kind)
	}
}
