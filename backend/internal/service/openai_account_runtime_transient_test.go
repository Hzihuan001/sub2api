package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type transientCooldownAccountRepo struct {
	AccountRepository
}

func (transientCooldownAccountRepo) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}

func TestHandleOpenAITransientError_BlocksOnlyRequestedModel(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       5105,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	firstShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"Upstream request failed","type":"upstream_error"}}`), "gpt-5.5")
	secondShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"Upstream request failed","type":"upstream_error"}}`), "gpt-5.5")

	require.False(t, firstShouldDisable)
	require.False(t, secondShouldDisable)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.6-terra"))
}

func TestHandleOpenAITransientError_TransientStatusesUseModelScope(t *testing.T) {
	for _, statusCode := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 520, 521, 522, 523, 524} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:       int64(5100 + statusCode),
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
			}

			firstShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), "gpt-5.5")
			secondShouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, statusCode, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), "gpt-5.5")

			require.False(t, firstShouldDisable)
			require.False(t, secondShouldDisable)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "status %d must not block the whole account", statusCode)
			require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"), "status %d should block the failing model", statusCode)
		})
	}
}

func TestHandleOpenAICompatibleTransientError_DeepSeekAndKimiUseModelScope(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi} {
		t.Run(platform, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
			account := &Account{
				ID:       5200,
				Platform: platform,
				Type:     AccountTypeAPIKey,
			}

			for range 2 {
				shouldDisable := svc.handleOpenAIAccountUpstreamError(
					context.Background(),
					account,
					524, // Cloudflare origin timeout returned by a provider CDN.
					http.Header{},
					[]byte(`{"error":{"message":"temporary upstream failure"}}`),
					"provider-model",
				)
				require.False(t, shouldDisable, "transient errors must not permanently disable %s", platform)
			}

			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account), "transient errors must not block the whole %s account", platform)
			require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "provider-model"), "the failing model should be cooled down for %s", platform)
			require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "other-model"), "an unrelated model must remain eligible for %s", platform)
		})
	}
}

func TestHandleOpenAICompatible52xFirstFailureGetsMinimumCooldown(t *testing.T) {
	for _, platform := range []string{PlatformDeepseek, PlatformKimi} {
		t.Run(platform, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
			account := &Account{ID: 5300, Platform: platform, Type: AccountTypeAPIKey}
			before := time.Now()

			shouldDisable := svc.handleOpenAIAccountUpstreamError(
				context.Background(), account, 524, http.Header{},
				[]byte(`{"error":{"message":"origin timeout"}}`), "provider-model",
			)

			require.False(t, shouldDisable)
			require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
			require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "provider-model"))
			state := svc.getOpenAIAccountModelTransientState()
			key, ok := openAIAccountModelTransientKey(account.ID, "provider-model")
			require.True(t, ok)
			state.mu.Lock()
			entry, exists := state.entries[key]
			state.mu.Unlock()
			require.True(t, exists)
			require.GreaterOrEqual(t, entry.blockUntil.Sub(before), openAIModelTransientGatewayCooldown-time.Second)
		})
	}
}

func TestHandleOpenAICompatible502FirstFailureKeepsExistingThreshold(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 5301, Platform: PlatformDeepseek, Type: AccountTypeAPIKey}

	shouldDisable := svc.handleOpenAIAccountUpstreamError(
		context.Background(), account, http.StatusBadGateway, http.Header{},
		[]byte(`{"error":{"message":"bad gateway"}}`), "provider-model",
	)

	require.False(t, shouldDisable)
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "provider-model"), "502 retains the existing two-failure threshold")
}

func TestHandleOpenAICompatible52xRepeatedFailureDoesNotShortenCooldown(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{ID: 5302, Platform: PlatformKimi, Type: AccountTypeAPIKey}

	call := func() {
		require.False(t, svc.handleOpenAIAccountUpstreamError(
			context.Background(), account, 524, http.Header{},
			[]byte(`{"error":{"message":"origin timeout"}}`), "provider-model",
		))
	}
	call()
	state := svc.getOpenAIAccountModelTransientState()
	key, ok := openAIAccountModelTransientKey(account.ID, "provider-model")
	require.True(t, ok)
	state.mu.Lock()
	firstUntil := state.entries[key].blockUntil
	state.mu.Unlock()

	require.True(t, firstUntil.After(time.Now().Add(openAIModelTransientGatewayCooldown-time.Second)))
	// The second failure occurs immediately and must not replace the existing
	// deadline with a shorter streak-based cooldown.
	call()
	state.mu.Lock()
	secondUntil := state.entries[key].blockUntil
	state.mu.Unlock()
	require.GreaterOrEqual(t, secondUntil, firstUntil)
}

func TestHandleOpenAITransientError_529RemainsOverloadOnly(t *testing.T) {
	require.False(t, shouldCooldownOpenAITransientUpstreamError(529, []byte(`{"error":{"message":"overloaded"}}`)))
}

func TestHandleOpenAITransientError_CanonicalModelIsNotMappedTwice(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       5107,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"public-alias": "upstream-a",
				"upstream-a":   "upstream-b",
			},
		},
	}
	canonicalModel := account.GetMappedModel("public-alias")
	require.Equal(t, "upstream-a", canonicalModel)

	for range 2 {
		svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadGateway, http.Header{}, []byte(`{"error":{"message":"temporary upstream failure"}}`), canonicalModel)
	}

	require.True(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "public-alias"))
	svc.ReportOpenAIAccountScheduleResult(account, canonicalModel, true, nil)
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "public-alias"))
}

func TestHandleOpenAITransientError_DoesNotBlockParameter400(t *testing.T) {
	svc := &OpenAIGatewayService{}
	svc.rateLimitService = NewRateLimitService(transientCooldownAccountRepo{}, nil, &config.Config{}, nil, nil)
	account := &Account{
		ID:       5103,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	shouldDisable := svc.handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusBadRequest, http.Header{}, []byte(`{"error":{"message":"Invalid type for input[0].arguments"}}`), "gpt-5.5")

	require.False(t, shouldDisable)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, svc.isOpenAIAccountModelRuntimeBlocked(account, "gpt-5.5"))
}

func TestHandleOpenAITransientError_HardDisableStillBlocksWholeAccount(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 5106, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}

	svc.BlockAccountScheduling(account, time.Now().Add(time.Minute), "upstream_disable")

	require.True(t, svc.isOpenAIAccountRuntimeBlocked(account))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.5"))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-5.6-sol"))
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
}
