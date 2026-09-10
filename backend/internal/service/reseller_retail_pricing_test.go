package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func resellerRetailFixture(t *testing.T, group *Group, channel *Channel, rate float64) (*BillingService, *ResellerPricingCalculator) {
	t.Helper()
	cfg := &config.Config{}
	billing := NewBillingService(cfg, &PricingService{cfg: cfg, pricingData: map[string]*LiteLLMModelPricing{"gpt-4o": {InputCostPerToken: 5e-6, OutputCostPerToken: 15e-6}}})
	snapshot, err := billing.ExportResellerPricing(group, channel, rate)
	require.NoError(t, err)
	calculator, err := CompileResellerPricing(snapshot, 7, cfg)
	require.NoError(t, err)
	return billing, calculator
}

func TestResellerRetailAndCostIndependentLadders(t *testing.T) {
	for _, source := range []string{"group", "channel", "upstream", "free_group", "free_channel"} {
		t.Run(source, func(t *testing.T) {
			defer ReplaceResellerPricing(nil)
			mainGroup := &Group{Platform: "openai", ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: floatSnapshotTest(0.001)}}}
			billing, calculator := resellerRetailFixture(t, mainGroup, nil, 0.2)
			local := &Group{ID: 7, Platform: "openai", RateMultiplier: 1.7}
			localChannel := Channel{ID: 1, Status: "active", GroupIDs: []int64{7}}
			price := 0.004
			if source == "free_group" || source == "free_channel" {
				price = 0
			}
			rule := ChannelModelPricing{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: &price}
			switch source {
			case "group", "free_group":
				local.ModelPricing = []ChannelModelPricing{rule}
				localChannel.ModelPricing = []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: floatSnapshotTest(0.009)}}
			case "channel", "free_channel":
				localChannel.ModelPricing = []ChannelModelPricing{rule}
			}
			channels := &ChannelService{frozenPricing: true}
			channels.cache.Store(populateChannelCache([]Channel{localChannel}, map[int64]string{7: "openai"}))
			resolver := NewModelPricingResolver(channels, billing)
			ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: calculator})
			key, err := PinResellerPricing(&APIKey{ID: 1, GroupID: &local.ID, Group: local})
			require.NoError(t, err)
			input := CostInput{Ctx: context.Background(), Group: key.Group, GroupID: &local.ID, Model: "gpt-4o", Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 1.7, Resolver: resolver}
			retail, err := billing.CalculateCostUnified(input)
			require.NoError(t, err)
			wantBase := price * 1000
			if source == "upstream" {
				wantBase = 1
			}
			require.InDelta(t, wantBase, retail.TotalCost, 1e-9)
			require.InDelta(t, wantBase*1.7, retail.ActualCost, 1e-9)
			costInput := input
			costInput.RateMultiplier = calculator.CostRate()
			cost, err := calculator.Calculate(costInput)
			require.NoError(t, err)
			require.InDelta(t, 1, cost.TotalCost, 1e-9)
			require.InDelta(t, 0.2, cost.ActualCost, 1e-9)
			require.Equal(t, local.ModelPricing, key.Group.ModelPricing)

			// A new upstream price and cost rate never overwrite local sale rules.
			mainGroup.ModelPricing[0].InputPrice = floatSnapshotTest(0.002)
			_, next := resellerRetailFixture(t, mainGroup, nil, 0.3)
			ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: next})
			fresh, err := PinResellerPricing(&APIKey{Group: local})
			require.NoError(t, err)
			input.Group = fresh.Group
			updated, err := billing.CalculateCostUnified(input)
			require.NoError(t, err)
			if source == "upstream" {
				wantBase = 2
			}
			require.InDelta(t, wantBase*1.7, updated.ActualCost, 1e-9)
		})
	}
}

func TestResellerRetailFallbackUsesMainChannelAndDefault(t *testing.T) {
	for _, explicitChannel := range []bool{true, false} {
		t.Run(map[bool]string{true: "channel", false: "default"}[explicitChannel], func(t *testing.T) {
			defer ReplaceResellerPricing(nil)
			var channel *Channel
			if explicitChannel {
				channel = &Channel{ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: floatSnapshotTest(0.003)}}}
			}
			billing, calculator := resellerRetailFixture(t, &Group{Platform: "openai"}, channel, 0.25)
			ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: calculator})
			key, err := PinResellerPricing(&APIKey{Group: &Group{ID: 7, Platform: "openai"}})
			require.NoError(t, err)
			input := CostInput{Model: "gpt-4o", Group: key.Group, GroupID: &key.Group.ID, Tokens: UsageTokens{InputTokens: 1000}, RateMultiplier: 2}
			retail, err := billing.CalculateCostUnified(input)
			require.NoError(t, err)
			input.RateMultiplier = 0.25
			cost, err := calculator.Calculate(input)
			require.NoError(t, err)
			require.InDelta(t, cost.TotalCost, retail.TotalCost, 1e-9)
			require.InDelta(t, cost.ActualCost*8, retail.ActualCost, 1e-9)
			if explicitChannel {
				require.InDelta(t, 3, retail.TotalCost, 1e-9)
			}
		})
	}
}

func TestResellerFreeRetailStillRecordsAndConsumesUpstreamCost(t *testing.T) {
	_, calculator := resellerRetailFixture(t, &Group{Platform: "openai"}, nil, 0.2)
	base := 3.0
	params := &postUsageBillingParams{
		Cost: &CostBreakdown{TotalCost: 0, ActualCost: 0}, UpstreamBaseCost: &base,
		APIKey: &APIKey{ID: 1, Group: &Group{ID: 7, resellerPricing: calculator}}, User: &User{ID: 2},
		Account: &Account{ID: 3, Type: "apikey", Extra: map[string]any{"quota_limit": 100.0}}, AccountRateMultiplier: 0.2,
	}
	cmd := buildUsageBillingCommand("split-test", nil, params)
	require.Zero(t, cmd.BalanceCost)
	require.Zero(t, cmd.ResellerCustomerCharge)
	require.InDelta(t, 0.6, cmd.AccountQuotaCost, 1e-9)
	require.Equal(t, 3.0, cmd.ResellerBaseCost)
	require.Equal(t, calculator.Revision(), cmd.ResellerPricingRevision)
	base = 0
	params.Cost = &CostBreakdown{TotalCost: 4, ActualCost: 8}
	cmd = buildUsageBillingCommand("free-upstream", nil, params)
	require.Zero(t, cmd.AccountQuotaCost)
	require.Equal(t, 8.0, cmd.BalanceCost)
}

func TestResellerRetailImageOverridesUpstreamImageRules(t *testing.T) {
	for _, source := range []string{"flat_free", "group", "channel", "upstream"} {
		t.Run(source, func(t *testing.T) {
			defer ReplaceResellerPricing(nil)
			main := &Group{Platform: "openai", ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"custom-image"}, BillingMode: BillingModeImage, PerRequestPrice: floatSnapshotTest(0.7)}}}
			billing, calculator := resellerRetailFixture(t, main, nil, 0.2)
			local := &Group{ID: 7, Platform: "openai"}
			channel := Channel{ID: 1, Status: "active", GroupIDs: []int64{7}}
			rule := ChannelModelPricing{Platform: "openai", Models: []string{"custom-image"}, BillingMode: BillingModeImage, PerRequestPrice: floatSnapshotTest(0.3)}
			want := 0.3 * 2 * 1.7
			switch source {
			case "flat_free":
				local.ImagePrice1K = floatSnapshotTest(0)
				want = 0
			case "group":
				local.ModelPricing = []ChannelModelPricing{rule}
			case "channel":
				channel.ModelPricing = []ChannelModelPricing{rule}
			case "upstream":
				want = 0.7 * 2 * 1.7
			}
			cs := &ChannelService{frozenPricing: true}
			cs.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{7: "openai"}))
			ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: calculator})
			key, err := PinResellerPricing(&APIKey{ID: 1, Group: local, GroupID: &local.ID})
			require.NoError(t, err)
			svc := &OpenAIGatewayService{billingService: billing, resolver: NewModelPricingResolver(cs, billing)}
			result := &OpenAIForwardResult{Model: "custom-image", ImageCount: 2, ImageSize: "1K"}
			retail, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"custom-image"}, 1.7, 1.7, 1.7, 1.7, UsageTokens{}, "", nil, time.Now())
			require.NoError(t, err)
			require.InDelta(t, want, retail.ActualCost, 1e-9)
			upstream := &OpenAIGatewayService{billingService: calculator.billing, resolver: calculator.resolver}
			cost, err := upstream.calculateOpenAIRecordUsageCost(context.Background(), result, calculator.costKey(key), []string{"custom-image"}, 0.2, 0.2, 0.2, 0.2, UsageTokens{}, "", nil, time.Now())
			require.NoError(t, err)
			require.InDelta(t, 0.28, cost.ActualCost, 1e-9)
		})
	}
}

func TestResellerRetailVideoFallbackPreservesWholeMainLadder(t *testing.T) {
	for _, source := range []string{"local-flat", "local-channel", "main"} {
		t.Run(source, func(t *testing.T) {
			defer ReplaceResellerPricing(nil)
			main := &Group{Platform: "openai", VideoPrice720P: floatSnapshotTest(0.12)}
			mainChannel := &Channel{ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"custom-video"}, BillingMode: BillingModeVideo, PerRequestPrice: floatSnapshotTest(0.9)}}}
			billing, calculator := resellerRetailFixture(t, main, mainChannel, 0.2)
			local := &Group{ID: 7, Platform: "openai"}
			channel := Channel{ID: 1, Status: "active", GroupIDs: []int64{7}}
			wantBase := 1.2 // The main flat group price wins over its channel.
			if source == "local-flat" {
				local.VideoPrice720P = floatSnapshotTest(0)
				wantBase = 0
			}
			if source == "local-channel" {
				channel.ModelPricing = []ChannelModelPricing{{Platform: "openai", Models: []string{"custom-video"}, BillingMode: BillingModeVideo, PerRequestPrice: floatSnapshotTest(0.3)}}
				wantBase = 3
			}
			cs := &ChannelService{frozenPricing: true}
			cs.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{7: "openai"}))
			ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: calculator})
			key, err := PinResellerPricing(&APIKey{ID: 1, Group: local, GroupID: &local.ID})
			require.NoError(t, err)
			svc := &OpenAIGatewayService{billingService: billing, resolver: NewModelPricingResolver(cs, billing)}
			result := &OpenAIForwardResult{Model: "custom-video", VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 10}
			retail, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"custom-video"}, 1.7, 1.7, 1.7, 1.7, UsageTokens{}, "", nil, time.Now())
			require.NoError(t, err)
			require.InDelta(t, wantBase*1.7, retail.ActualCost, 1e-9)
			upstream := &OpenAIGatewayService{billingService: calculator.billing, resolver: calculator.resolver}
			cost, err := upstream.calculateOpenAIRecordUsageCost(context.Background(), result, calculator.costKey(key), []string{"custom-video"}, 0.2, 0.2, 0.2, 0.2, UsageTokens{}, "", nil, time.Now())
			require.NoError(t, err)
			require.InDelta(t, 0.24, cost.ActualCost, 1e-9)
		})
	}
}
