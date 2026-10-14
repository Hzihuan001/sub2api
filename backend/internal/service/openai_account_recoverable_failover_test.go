//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type openAIRecoverableFailureRepo struct {
	oauth429RateLimitRepo
	setErrorCalls int
}

func (r *openAIRecoverableFailureRepo) SetError(context.Context, int64, string) error {
	r.setErrorCalls++
	return nil
}

func newOpenAIRecoverableFailureGateway() (*OpenAIGatewayService, *openAIRecoverableFailureRepo) {
	repo := &openAIRecoverableFailureRepo{}
	rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	gateway := &OpenAIGatewayService{rateLimitService: rateLimits}
	rateLimits.SetAccountRuntimeBlocker(gateway)
	return gateway, repo
}

func TestOpenAIRecoverableFailover_QuotaWithoutResetStopsSameAccount(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusPaymentRequired} {
			t.Run(accountType+"/"+http.StatusText(statusCode), func(t *testing.T) {
				gateway, repo := newOpenAIRecoverableFailureGateway()
				account := &Account{ID: 8101, Platform: PlatformOpenAI, Type: accountType, Status: StatusActive,
					Credentials: map[string]any{"pool_mode": true}}
				body := []byte(`{"response":{"error":{"code":"insufficient_quota","type":"invalid_request_error"}}}`)
				before := time.Now()

				require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, nil, body, "gpt-5.5"))
				require.False(t, gateway.ShouldRetryOpenAIOAuth429(account, nil, body))
				require.Equal(t, 1, repo.setRateLimitedCalls)
				require.GreaterOrEqual(t, repo.lastRateLimitedUntil.Sub(before), 10*time.Minute)
				require.Zero(t, repo.setErrorCalls, "quota exhaustion is recoverable, never a permanent disable")
				require.Equal(t, StatusActive, account.Status)
				require.Nil(t, account.RateLimitResetAt, "do not mutate shared scheduler snapshots")
			})
		}
	}
}

func TestOpenAIRecoverableFailover_QuotaIsNotDisabledByTransient429FallbackSetting(t *testing.T) {
	gateway, repo := newOpenAIRecoverableFailureGateway()
	settings := newMockSettingRepo()
	settings.data[SettingKeyRateLimit429CooldownSettings] = `{"enabled":false,"cooldown_seconds":12}`
	gateway.rateLimitService.SetSettingService(NewSettingService(settings, &config.Config{}))
	account := &Account{ID: 8102, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	body := []byte(`{"error":{"code":"insufficient_balance"}}`)

	require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, nil, body))
	require.Equal(t, 1, repo.setRateLimitedCalls)
	require.WithinDuration(t, time.Now().Add(10*time.Minute), repo.lastRateLimitedUntil, time.Second)
	require.Zero(t, repo.setErrorCalls)
	require.False(t, gateway.ShouldRetryOpenAIOAuth429(account, nil, body))
}

func TestOpenAIRecoverableFailover_QuotaExpiredResetUsesLongerConfiguredFallback(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"insufficient_quota","resets_at":1}}`,
		`{"response":{"error":{"code":"insufficient_quota","resets_in_seconds":0}}}`,
	} {
		gateway, repo := newOpenAIRecoverableFailureGateway()
		settings := newMockSettingRepo()
		settings.data[SettingKeyRateLimit429CooldownSettings] = `{"enabled":true,"cooldown_seconds":1200}`
		gateway.rateLimitService.SetSettingService(NewSettingService(settings, &config.Config{}))
		account := &Account{ID: 8108, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, nil, []byte(body)))
		require.WithinDuration(t, time.Now().Add(20*time.Minute), repo.lastRateLimitedUntil, time.Second)
	}
}

func TestOpenAIRecoverableFailover_RetryAfterBeyondShortWindowSwitches(t *testing.T) {
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeAPIKey} {
		t.Run(accountType, func(t *testing.T) {
			gateway, repo := newOpenAIRecoverableFailureGateway()
			account := &Account{ID: 8103, Platform: PlatformOpenAI, Type: accountType,
				Credentials: map[string]any{"pool_mode": true}}
			headers := http.Header{"Retry-After": []string{"60"}}
			body := []byte(`{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`)
			before := time.Now()

			require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body))
			require.False(t, gateway.ShouldRetryOpenAIOAuth429(account, headers, body))
			require.Equal(t, 1, repo.setRateLimitedCalls)
			require.GreaterOrEqual(t, repo.lastRateLimitedUntil.Sub(before), 60*time.Second)
			require.Zero(t, repo.setErrorCalls)
		})
	}
}

func TestOpenAIRecoverableFailover_Short429RecoveryWindow(t *testing.T) {
	gateway, _ := newOpenAIRecoverableFailureGateway()
	account := &Account{ID: 8104, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	shortWait := http.Header{"Retry-After": []string{"1"}}
	body := []byte(`{"error":{"code":"rate_limit_exceeded"}}`)

	require.Equal(t, 8*time.Second, openAIOAuth429RetryWindow)
	require.True(t, gateway.ShouldRetryOpenAIOAuth429(account, shortWait, body))
	require.Equal(t, time.Second, openAIOAuth429SameAccountRetryDelay(shortWait, gateway.openAIOAuth429RetryDeadline(account)))
	require.Equal(t, 500*time.Millisecond, openAIOAuth429SameAccountRetryDelay(http.Header{"Retry-After": []string{"0.5"}}, time.Now().Add(100*time.Millisecond)))
	gateway.openaiOAuth429RetryStartedAt.Store(account.ID, time.Now().Add(-7500*time.Millisecond))
	require.False(t, gateway.ShouldRetryOpenAIOAuth429(account, shortWait, body), "do not shorten Retry-After to the remaining window")
	gateway.openaiOAuth429RetryStartedAt.Store(account.ID, time.Now().Add(-9*time.Second))
	require.False(t, gateway.ShouldRetryOpenAIOAuth429(account, nil, body))
}

func TestOpenAIRecoverableFailover_RetryAfterAndQuotaResetUseLatest(t *testing.T) {
	now := time.Now()
	headers := http.Header{"Retry-After": []string{"120"}}
	headers.Set("x-codex-primary-used-percent", "100")
	headers.Set("x-codex-primary-reset-after-seconds", "60")
	headers.Set("x-codex-primary-window-minutes", "300")
	until := openAI429CooldownResetTime(headers, nil, now)
	require.NotNil(t, until)
	require.Equal(t, now.Add(120*time.Second), *until)
	headers.Set("Retry-After", now.Add(30*time.Second).UTC().Format(http.TimeFormat))
	until = openAI429CooldownResetTime(headers, nil, now)
	require.NotNil(t, until)
	require.WithinDuration(t, now.Add(time.Minute), *until, time.Second)
	until = openAI429CooldownResetTime(nil, []byte(`{"response":{"error":{"code":"insufficient_quota","resets_in_seconds":120}}}`), now)
	require.NotNil(t, until)
	require.WithinDuration(t, now.Add(120*time.Second), *until, time.Second)
}

func TestOpenAIRecoverableFailover_Pool5xxKeepsFirstRetryThenCoolsOnlyModel(t *testing.T) {
	for _, statusCode := range []int{500, 502, 503, 504, 520, 521, 522, 523, 524} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			gateway, repo := newOpenAIRecoverableFailureGateway()
			account := &Account{ID: 8105, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"pool_mode": true, "pool_mode_retry_status_codes": []any{float64(statusCode)}}}
			body := []byte(`{"error":{"message":"temporary gateway failure"}}`)

			first := gateway.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, nil, body, "gpt-5.5")
			require.Equal(t, isOpenAI52xUpstreamStatus(statusCode), first)
			require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, nil, body, "gpt-5.5"))
			require.True(t, gateway.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.5"))
			require.False(t, gateway.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.6"))
			require.False(t, gateway.isOpenAIAccountRuntimeBlocked(account))
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.setRateLimitedCalls)
		})
	}
}

func TestOpenAIRecoverableFailover_RequestErrorNeverCoolsAccount(t *testing.T) {
	gateway, repo := newOpenAIRecoverableFailureGateway()
	account := &Account{ID: 8106, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"pool_mode": true}}
	body := []byte(`{"error":{"code":"invalid_parameter","message":"invalid parameter: schema"}}`)

	for range 3 {
		require.False(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, nil, body, "gpt-5.5"))
	}
	require.False(t, gateway.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.5"))
	require.Zero(t, repo.setErrorCalls)
	require.Zero(t, repo.setRateLimitedCalls)
}

func TestOpenAIRecoverableFailover_RateLimitServicePoolQuotaIsRecoverable(t *testing.T) {
	gateway, repo := newOpenAIRecoverableFailureGateway()
	account := &Account{ID: 8107, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"pool_mode": true}}
	body := []byte(`{"error":{"code":"credits_exhausted"}}`)
	require.True(t, gateway.rateLimitService.HandleUpstreamError(context.Background(), account, http.StatusPaymentRequired, nil, body))
	require.Equal(t, 1, repo.setRateLimitedCalls)
	require.Zero(t, repo.setErrorCalls)
}

func TestOpenAIRecoverableFailover_CompatibleProviderSignals(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi, PlatformZhipu, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			t.Run("quota", func(t *testing.T) {
				gateway, repo := newOpenAIRecoverableFailureGateway()
				account := &Account{ID: 8110, Platform: platform, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"pool_mode": true}}
				body := []byte(`{"error":{"code":"insufficient_quota"}}`)
				headers := http.Header{"Retry-After": []string{"60"}}
				require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body))
				require.Equal(t, 1, repo.setRateLimitedCalls)
				require.WithinDuration(t, time.Now().Add(time.Minute), repo.lastRateLimitedUntil, time.Second)
				require.Zero(t, repo.setErrorCalls)
			})
			t.Run("request", func(t *testing.T) {
				gateway, repo := newOpenAIRecoverableFailureGateway()
				account := &Account{ID: 8111, Platform: platform, Type: AccountTypeAPIKey}
				body := []byte(`{"error":{"code":"invalid_parameter"}}`)
				for range 3 {
					require.False(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusServiceUnavailable, nil, body, "provider-model"))
				}
				require.False(t, gateway.isOpenAIAccountModelRuntimeBlocked(account, "provider-model"))
				require.Zero(t, repo.setErrorCalls)
			})
			t.Run("credential", func(t *testing.T) {
				gateway, repo := newOpenAIRecoverableFailureGateway()
				account := &Account{ID: 8112, Platform: platform, Type: AccountTypeAPIKey,
					Credentials: map[string]any{"pool_mode": true}}
				body := []byte(`{"error":{"code":"invalid_api_key"}}`)
				require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadRequest, nil, body))
				require.Equal(t, 1, repo.setErrorCalls, "reuse the existing API-key credential rejection handling")
				require.Zero(t, repo.setRateLimitedCalls)
			})
		})
	}
}

func TestOpenAIRecoverableFailover_Pool429PreservesClineWalletScope(t *testing.T) {
	for _, body := range []string{clineSpendLimitBody, clineInferenceCapBody, clineInsufficientCreditsBody} {
		repo := &commandCodeRateLimitRepo{}
		rateLimits := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
		gateway := &OpenAIGatewayService{rateLimitService: rateLimits}
		rateLimits.SetAccountRuntimeBlocker(gateway)
		account := clineTestAccount(8120)
		account.Credentials["pool_mode"] = true

		require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusTooManyRequests, nil, []byte(body), "deepseek/deepseek-v4-flash"))
		require.Len(t, repo.modelLimits, 1)
		require.Contains(t, repo.modelLimits, clineCreditsRateLimitKey)
		require.Zero(t, repo.rateLimitedCalls)
		require.Zero(t, repo.tempCalls)
		require.Zero(t, repo.setErrorCalls)
	}
}
