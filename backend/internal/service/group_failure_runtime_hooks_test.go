//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupRuntimeAccountRepo struct {
	AccountRepository
	overloads, rateLimits, modelLimits, temporary, permanent int
	until                                                    time.Time
}

func (r *groupRuntimeAccountRepo) SetOverloaded(_ context.Context, _ int64, until time.Time) error {
	r.overloads++
	r.until = until
	return nil
}
func (r *groupRuntimeAccountRepo) SetRateLimited(_ context.Context, _ int64, until time.Time) error {
	r.rateLimits++
	r.until = until
	return nil
}
func (r *groupRuntimeAccountRepo) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	r.modelLimits++
	return nil
}
func (r *groupRuntimeAccountRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	r.temporary++
	return nil
}
func (r *groupRuntimeAccountRepo) SetError(context.Context, int64, string) error {
	r.permanent++
	return nil
}
func (r *groupRuntimeAccountRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

func newGroupRuntimeFixture(t *testing.T, rules map[GroupFailureKind]GroupFailureRule) (context.Context, *OpenAIGatewayService, *groupRuntimeAccountRepo, *Account) {
	t.Helper()
	settings := groupFailurePolicyTestService(&groupFailurePolicySettingRepo{})
	_, err := settings.UpdateGroupFailurePolicy(context.Background(), 1, &GroupFailurePolicy{Rules: rules})
	require.NoError(t, err)
	repo := &groupRuntimeAccountRepo{}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}
	limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
	limiter.SetSettingService(settings)
	svc := &OpenAIGatewayService{cfg: cfg, accountRepo: repo, rateLimitService: limiter}
	limiter.SetAccountRuntimeBlocker(svc)
	account := &Account{ID: 801, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 10,
		Credentials: map[string]any{"pool_mode": true, "pool_mode_retry_count": 10}}
	return context.WithValue(context.Background(), ctxkey.Group, &Group{ID: 1}), svc, repo, account
}

func groupRuntimeRules(threshold int) map[GroupFailureKind]GroupFailureRule {
	rules := map[GroupFailureKind]GroupFailureRule{}
	for _, kind := range []GroupFailureKind{GroupFailureOverload, GroupFailureServerError, GroupFailureTimeout, GroupFailureRateLimit} {
		rules[kind] = GroupFailureRule{Threshold: threshold, WindowSeconds: 60, CooldownSeconds: 120}
	}
	return rules
}

func TestGroupFailureRuntimeHTTPRulesDoNotPauseAccounts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"overload", 503, overloadGatewayPayload},
		{"server_error", 502, `{"error":{"code":"server_error","message":"upstream unavailable"}}`},
		{"timeout", 504, `{"error":{"message":"gateway timeout"}}`},
		{"rate_limit", 429, `{"error":{"code":"rate_limit_exceeded","message":"too many requests"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(2))
			for attempt := 1; attempt <= 3; attempt++ {
				svc.handleOpenAIAccountUpstreamError(ctx, account, tc.status, nil, []byte(tc.body), "gpt-test")
			}
			require.Zero(t, repo.overloads)
			require.False(t, svc.rateLimitService.IsAccountOverloadCooling(account.ID, time.Now()))
			require.Empty(t, svc.rateLimitService.groupFailures.entries)
			require.Nil(t, account.OverloadUntil, "shared scheduling snapshots stay immutable")
			// Native account-model transient protection and provider quota
			// handling may still act, but no custom global overload is written.
		})
	}
}

func TestGroupFailureRuntimeNativeHTTPAndStreamTimeout(t *testing.T) {
	for _, status := range []int{529, 500, 504, 429} {
		ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(2))
		account.Platform = PlatformAnthropic
		account.Credentials = nil
		svc.rateLimitService.HandleUpstreamError(ctx, account, status, nil, []byte(`{"error":{"message":"temporary failure"}}`))
		svc.rateLimitService.HandleUpstreamError(ctx, account, status, nil, []byte(`{"error":{"message":"temporary failure"}}`))
		require.Zero(t, repo.overloads)
	}
	ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(2))
	require.False(t, svc.rateLimitService.HandleStreamTimeout(ctx, account, "gpt-test"))
	require.False(t, svc.rateLimitService.HandleStreamTimeout(ctx, account, "gpt-test"))
	require.Zero(t, repo.overloads)
}

func TestGroupFailureRuntimeNoExplicitRuleDoesNotPause(t *testing.T) {
	for _, noGroup := range []bool{false, true} {
		ctx, svc, repo, account := newGroupRuntimeFixture(t, map[GroupFailureKind]GroupFailureRule{
			GroupFailureTimeout: {Threshold: 1, WindowSeconds: 60, CooldownSeconds: 120},
		})
		if noGroup {
			ctx = context.Background()
		}
		for attempt := 1; attempt <= 3; attempt++ {
			require.False(t, svc.handleOpenAIAccountUpstreamError(ctx, account, 503, nil, []byte(overloadGatewayPayload)))
		}
		require.Zero(t, repo.overloads)
		require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
	}
}

func TestGroupFailureRuntimeRequestAndContentErrorsNeverCount(t *testing.T) {
	ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(1))
	for _, body := range []string{
		`{"error":{"code":"content_policy_violation","message":"blocked by policy"}}`,
		`{"error":{"code":"invalid_parameter","message":"invalid parameter"}}`,
		`{"error":{"code":"context_length_exceeded","message":"context too large"}}`,
		`{"error":{"code":"invalid_parameter","message":"invalid parameter"},"prompt":"Our servers are currently overloaded"}`,
	} {
		for _, status := range []int{400, 403, 429, 503} {
			require.False(t, svc.handleOpenAIAccountUpstreamError(ctx, account, status, nil, []byte(body), "gpt-test"))
		}
	}
	require.Zero(t, repo.overloads)
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
	_, ok := groupHTTPFailureKind(account, 403, nil, []byte(`{"error":{"message":"Forbidden"}}`), "gpt-test")
	require.False(t, ok, "bare forbidden is not a configurable transient failure")
}

func TestGroupFailureRuntimeExplicitLimitsAndAccountRulesWin(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		headers  http.Header
		model    string
		expected string
	}{
		{"retry_after", 429, `{"error":{"code":"rate_limit_exceeded"}}`, http.Header{"Retry-After": {"600"}}, "gpt-test", "rate"},
		{"quota", 429, `{"error":{"code":"insufficient_quota"}}`, nil, "gpt-test", "rate"},
		{"image", 429, `{"error":{"message":"Rate limit reached for limit gpt-image"}}`, nil, "gpt-image-2", "model"},
		{"model_not_found", 404, `{"error":{"code":"model_not_found"}}`, nil, "gpt-missing", "model"},
		{"temporary_rule", 503, `{"error":{"message":"maintenance"}}`, nil, "gpt-test", "model"},
		{"custom_code", 503, `{"error":{"message":"server failure"}}`, nil, "gpt-test", "permanent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(1))
			account.Credentials = map[string]any{}
			if tc.name == "temporary_rule" {
				account.Credentials["temp_unschedulable_enabled"] = true
				account.Credentials["temp_unschedulable_rules"] = []any{map[string]any{"error_code": float64(503), "keywords": []any{"maintenance"}, "duration_minutes": float64(30)}}
			}
			if tc.name == "custom_code" {
				account.Credentials["custom_error_codes_enabled"] = true
				account.Credentials["custom_error_codes"] = []any{float64(503)}
			}
			svc.handleOpenAIAccountUpstreamError(ctx, account, tc.status, tc.headers, []byte(tc.body), tc.model)
			require.Zero(t, repo.overloads)
			switch tc.expected {
			case "rate":
				require.Equal(t, 1, repo.rateLimits)
				require.True(t, repo.until.After(time.Now().Add(590*time.Second)))
			case "model":
				require.Equal(t, 1, repo.modelLimits)
			case "permanent":
				require.Equal(t, 1, repo.permanent)
			}
		})
	}
}

func TestGroupFailureRuntimeSSEPairedFramesCountOnceAndDoNotReplayPartialOutput(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(2))
		for attempt := 1; attempt <= 2; attempt++ {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
				"data: {\"type\":\"error\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded\"}}\n\n" +
				"data: " + overloadGatewayPayload + "\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(ctx, resp, c, account, time.Now(), "gpt-test", "gpt-test")
			} else {
				_, err = svc.handleStreamingResponse(ctx, resp, c, account, time.Now(), "gpt-test", "gpt-test")
			}
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "partial output cannot be replayed")
			require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
		}
		require.Zero(t, repo.overloads)
	}
}

func TestGroupFailureRuntimeWSHTTPBridgeCountsOneFailurePerAttempt(t *testing.T) {
	ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(2))
	svc.cfg = newOpenAIWSV2TestConfig()
	upstream := &httpUpstreamRecorder{}
	svc.httpUpstream = upstream
	account.Credentials["api_key"] = "sk-test"
	payload := []byte(`{"type":"response.create","model":"gpt-test","input":"hi"}`)
	for turn := 1; turn <= 2; turn++ {
		stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
			"data: {\"type\":\"error\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded\"}}\n\n" +
			"data: " + overloadGatewayPayload + "\n\n"
		upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(ctx)
		result, err := svc.proxyOpenAIWSHTTPBridgeTurn(ctx, c, account, "sk-test", payload, len(payload), "gpt-test", "", "", "", "", turn, func([]byte) error { return nil })
		require.NoError(t, err)
		require.Equal(t, "response.failed", result.UpstreamTerminalEvent)
		require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-test"))
	}
	require.Zero(t, repo.overloads)
}

type groupRuntimeTimeoutError struct{}

func (groupRuntimeTimeoutError) Error() string   { return "upstream I/O timeout" }
func (groupRuntimeTimeoutError) Timeout() bool   { return true }
func (groupRuntimeTimeoutError) Temporary() bool { return true }

func TestGroupFailureRuntimeTransportTimeoutExcludesClientCancellation(t *testing.T) {
	for _, openAI := range []bool{false, true} {
		ctx, svc, repo, account := newGroupRuntimeFixture(t, groupRuntimeRules(2))
		invoke := func(callCtx context.Context, err error) error {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(callCtx)
			if openAI {
				return svc.handleOpenAIUpstreamTransportError(callCtx, c, account, err, false)
			}
			gateway := &GatewayService{accountRepo: repo, rateLimitService: svc.rateLimitService}
			return gateway.handleUpstreamTransportError(callCtx, c, account, err, OpsUpstreamErrorEvent{})
		}
		require.Error(t, invoke(ctx, groupRuntimeTimeoutError{}))
		require.Zero(t, repo.overloads)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		require.ErrorIs(t, invoke(canceled, context.Canceled), context.Canceled)
		require.Zero(t, repo.overloads)
		require.Error(t, invoke(ctx, groupRuntimeTimeoutError{}))
		require.Zero(t, repo.overloads)
	}
}
