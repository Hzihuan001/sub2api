package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResellerPricingPinnedAcrossUpdatesAndLegacyFallback(t *testing.T) {
	defer ReplaceResellerPricing(nil)
	billing := NewBillingService(&config.Config{}, nil)
	price := 0.001
	upstream := &Group{Platform: "openai", ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: &price}}}
	snapshot, err := billing.ExportResellerPricing(upstream, nil, 0.2)
	require.NoError(t, err)
	first, err := CompileResellerPricing(snapshot, 7, &config.Config{})
	require.NoError(t, err)
	ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: first, 8: nil})
	key := &APIKey{Group: &Group{ID: 7, Platform: "openai", RateMultiplier: 1.7}}
	pinned, err := PinResellerPricing(key)
	require.NoError(t, err)
	require.NotSame(t, key, pinned)
	require.Nil(t, key.Group.resellerPricing)
	before, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Ctx: context.Background(), Group: pinned.Group, Model: "gpt-4o", Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1.7})
	require.NoError(t, err)
	require.InDelta(t, 1.7, before.ActualCost, 1e-9)
	price = 0.002
	snapshot, err = billing.ExportResellerPricing(upstream, nil, 0.3)
	require.NoError(t, err)
	second, err := CompileResellerPricing(snapshot, 7, &config.Config{})
	require.NoError(t, err)
	ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: second, 8: nil})
	oldCost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Group: pinned.Group, Model: "gpt-4o", Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1.7})
	require.NoError(t, err)
	require.Equal(t, before, oldCost)
	newKey, err := PinResellerPricing(key)
	require.NoError(t, err)
	newCost, err := billing.CalculateTokenCostForRequest(TokenCostRequest{Group: newKey.Group, Model: "gpt-4o", Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1.7})
	require.NoError(t, err)
	require.InDelta(t, 3.4, newCost.ActualCost, 1e-9)
	_, err = PinResellerPricing(&APIKey{Group: &Group{ID: 8}})
	require.ErrorContains(t, err, "synchronizing")
	manual := &APIKey{Group: &Group{ID: 99}}
	got, err := PinResellerPricing(manual)
	require.NoError(t, err)
	require.Same(t, manual, got)
}

func TestResellerMediaCostUsesPinnedGroupAndDefaultPrices(t *testing.T) {
	defer ReplaceResellerPricing(nil)
	cfg := &config.Config{}
	upstream := NewBillingService(cfg, &PricingService{cfg: cfg, pricingData: map[string]*LiteLLMModelPricing{"custom-image": {OutputCostPerImage: 0.7, TokenPricingAbsent: true}}})
	videoPrice, audioPrice, searchPrice := 0.12, 0.8, 20.0
	group := &Group{Platform: "openai", VideoPrice720P: &videoPrice, AudioSTTPricePerHour: &audioPrice, SearchPricePer1k: &searchPrice, ImageRateIndependent: true, ImageRateMultiplier: 0.05}
	snapshot, err := upstream.ExportResellerPricing(group, nil, 0.2)
	require.NoError(t, err)
	calculator, err := CompileResellerPricing(snapshot, 7, cfg)
	require.NoError(t, err)
	ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: calculator})
	key, err := PinResellerPricing(&APIKey{Group: &Group{ID: 7, Platform: "openai", RateMultiplier: 1.7}})
	require.NoError(t, err)
	local := NewBillingService(cfg, nil)
	image := local.CalculateImageCost("custom-image", "1K", 2, imagePriceConfigFromAPIKey(key), 1.7)
	require.InDelta(t, 1.4, image.TotalCost, 1e-9)
	require.InDelta(t, 2.38, image.ActualCost, 1e-9)
	video := local.CalculateVideoCost("custom-video", "720p", 1, 10, videoPriceConfigFromAPIKey(key), 1.7)
	require.InDelta(t, 1.2, video.TotalCost, 1e-9)
	require.InDelta(t, 2.04, video.ActualCost, 1e-9)
	audio := local.CalculateAudioCost("stt", 1, groupAudioPriceConfigFromAPIKey(key), 1.7)
	require.InDelta(t, 0.8, audio.TotalCost, 1e-9)
	search := local.CalculateSearchCost(10, groupSearchPricePer1kFromAPIKey(key), 1.7)
	require.InDelta(t, 0.2, search.TotalCost, 1e-9)
	require.Equal(t, 0.05, resellerAccountRate(key.Group, &Account{}, time.Now(), true, false))
	require.Equal(t, 0.2, resellerAccountRate(key.Group, &Account{}, time.Now(), false, false))
	api := &OpenAIGatewayService{}
	require.Same(t, key, api.apiKeyWithFreshGroupMediaPricing(context.Background(), key))
}
