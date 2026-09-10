package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestResellerPricingSnapshotChannelTiersCacheAndTimeParity(t *testing.T) {
	cfg := &config.Config{}
	billing := NewBillingService(cfg, nil)
	inputPrice, outputPrice, cachePrice := 0.0001, 0.0002, 0.00003
	group := &Group{ID: 7, Platform: "openai", LongContextPricingEnabled: true}
	channel := Channel{ID: 9, Status: "active", GroupIDs: []int64{7}, ModelPricing: []ChannelModelPricing{{
		Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: &inputPrice, OutputPrice: &outputPrice, CacheReadPrice: &cachePrice,
		Intervals:   []PricingInterval{{MinTokens: 1000, InputMultiplier: floatSnapshotTest(2), CacheReadMultiplier: floatSnapshotTest(3)}},
		TimePricing: &ChannelTimePricing{Timezone: "UTC", Periods: []ChannelTimePricingPeriod{{StartTime: "00:00", EndTime: "12:00", Multiplier: 2}}},
	}}}
	cs := &ChannelService{frozenPricing: true}
	cs.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{7: "openai"}))
	snapshot, err := billing.ExportResellerPricing(group, &channel, 0.25)
	require.NoError(t, err)
	compiled, err := CompileResellerPricing(snapshot, 11, cfg)
	require.NoError(t, err)
	for _, hour := range []int{1, 13} {
		for _, count := range []int{100, 2000} {
			input := CostInput{Ctx: context.Background(), Model: "gpt-4o", Group: group, GroupID: &group.ID, Tokens: UsageTokens{InputTokens: count, OutputTokens: 100, CacheReadTokens: 250, CacheCreation5mTokens: 40, CacheCreation1hTokens: 30},
				RateMultiplier: 1.7, ServiceTier: "priority", PricingAt: time.Date(2026, 9, 10, hour, 0, 0, 0, time.UTC), Resolver: NewModelPricingResolver(cs, billing)}
			expected, err := billing.CalculateCostUnified(input)
			require.NoError(t, err)
			actual, err := compiled.Calculate(input)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
		}
	}
}
func floatSnapshotTest(value float64) *float64 { return &value }

func TestResellerPricingSnapshotPreservesAbsentTokenPrices(t *testing.T) {
	cfg := &config.Config{}
	source := &PricingService{cfg: cfg, pricingData: map[string]*LiteLLMModelPricing{
		"custom-image-only": {OutputCostPerImage: 0.7, TokenPricingAbsent: true},
		"custom-free-text":  {InputCostPerToken: 0, OutputCostPerToken: 0},
	}}
	billing := NewBillingService(cfg, source)
	snapshot, err := billing.ExportResellerPricing(&Group{Platform: "openai"}, nil, 0.2)
	require.NoError(t, err)
	compiled, err := CompileResellerPricing(snapshot, 7, cfg)
	require.NoError(t, err)
	require.True(t, compiled.billing.pricingService.GetModelPricing("custom-image-only").TokenPricingAbsent)
	require.False(t, compiled.billing.HasIdentifiedTokenPricing("custom-image-only"))
	require.True(t, compiled.billing.HasIdentifiedTokenPricing("custom-free-text"))
}

func TestResellerPricingSnapshotParityAndIsolation(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo} {
		t.Run(string(mode), func(t *testing.T) {
			cfg := &config.Config{}
			billing := NewBillingService(cfg, nil)
			price, zero, requestPrice := 12e-6, 0.0, 0.25
			group := &Group{ID: 7, Platform: "openai", Name: "private-name", RateMultiplier: 9,
				ModelAllowlist: GroupModelAllowlist{}, LongContextPricingEnabled: true}
			channel := Channel{ID: 25, Name: "private-channel", Status: "active", GroupIDs: []int64{7, 99},
				ModelPricing: []ChannelModelPricing{{ID: 100, ChannelID: 25, Platform: "openai", Models: []string{"gpt-4o"},
					BillingMode: mode, InputPrice: &price, CacheReadPrice: &zero, PerRequestPrice: &requestPrice}}}
			snapshot, err := billing.ExportResellerPricing(group, &channel, 0.3)
			require.NoError(t, err)
			require.Len(t, snapshot.Revision, 64)
			raw, err := json.Marshal(snapshot)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "private-name")
			require.NotContains(t, string(raw), "private-channel")
			require.Zero(t, snapshot.ChannelPrices[0].ID)
			sourceChannel := &ChannelService{frozenPricing: true}
			sourceChannel.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{7: "openai"}))
			resolver := NewModelPricingResolver(sourceChannel, billing)
			input := CostInput{Ctx: context.Background(), Model: "gpt-4o", Group: group, GroupID: &group.ID,
				Tokens:         UsageTokens{InputTokens: 1000, OutputTokens: 100, CacheReadTokens: 100},
				RateMultiplier: 1.7, RequestCount: 2, Resolver: resolver, PricingAt: time.Now()}
			expected, err := billing.CalculateCostUnified(input)
			require.NoError(t, err)
			calculator, err := CompileResellerPricing(snapshot, 123, cfg)
			require.NoError(t, err)
			actual, err := calculator.Calculate(input)
			require.NoError(t, err)
			require.Equal(t, expected, actual)
			// Editing a published transport object must not change compiled requests.
			*snapshot.ChannelPrices[0].InputPrice = 500
			after, err := calculator.Calculate(input)
			require.NoError(t, err)
			require.Equal(t, actual, after)
			local := &Group{ID: 123, Name: "local", RateMultiplier: 1.7, ImageRateMultiplier: 0.6,
				ModelAllowlist: group.ModelAllowlist}
			applied := calculator.ApplyPriceFields(local)
			require.Equal(t, local.ID, applied.ID)
			require.Equal(t, local.Name, applied.Name)
			require.Equal(t, local.RateMultiplier, applied.RateMultiplier)
			require.Equal(t, local.ImageRateMultiplier, applied.ImageRateMultiplier)
			require.Equal(t, 0.3, calculator.CostRate())
		})
	}
}

func TestResellerPricingSnapshotGroupPrecedenceAndRevision(t *testing.T) {
	cfg := &config.Config{}
	billing := NewBillingService(cfg, nil)
	zero, channelPrice := 0.0, 1.0
	group := &Group{ID: 5, Platform: "openai", LongContextPricingEnabled: false,
		ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"},
			InputPrice: &zero, OutputPrice: &zero, CacheReadPrice: &zero, CacheWritePrice: &zero}}}
	channel := &Channel{ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: &channelPrice}}}
	snapshot, err := billing.ExportResellerPricing(group, channel, 0)
	require.NoError(t, err)
	compiled, err := CompileResellerPricing(snapshot, 9, cfg)
	require.NoError(t, err)
	result, err := compiled.Calculate(CostInput{Model: "gpt-4o", Tokens: UsageTokens{InputTokens: 1000, OutputTokens: 100}, RateMultiplier: 3})
	require.NoError(t, err)
	require.Zero(t, result.TotalCost)
	same, err := billing.ExportResellerPricing(group, channel, 0)
	require.NoError(t, err)
	require.Equal(t, snapshot.Revision, same.Revision)
	changed, err := billing.ExportResellerPricing(group, channel, 0.2)
	require.NoError(t, err)
	require.NotEqual(t, snapshot.Revision, changed.Revision)
	changed.CostRate = 0.5
	_, err = CompileResellerPricing(changed, 9, cfg)
	require.ErrorContains(t, err, "revision mismatch")
}
