//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResellerRecordUsageIndependentCustomerAndAccountMoney(t *testing.T) {
	for _, gateway := range []string{"openai", "generic"} {
		for _, retailPrice := range []float64{0, 0.004} {
			t.Run(gateway+"/"+map[bool]string{true: "free", false: "paid"}[retailPrice == 0], func(t *testing.T) {
				defer ReplaceResellerPricing(nil)
				main := &Group{Platform: "openai", ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: floatSnapshotTest(0.001)}}}
				billing, calculator := resellerRetailFixture(t, main, nil, 0.2)
				ReplaceResellerPricing(map[int64]*ResellerPricingCalculator{7: calculator})
				local := &Group{ID: 7, Platform: "openai", RateMultiplier: 1.7, ModelPricing: []ChannelModelPricing{{Platform: "openai", Models: []string{"gpt-4o"}, InputPrice: &retailPrice}}}
				key, err := PinResellerPricing(&APIKey{ID: 1, GroupID: &local.ID, Group: local})
				require.NoError(t, err)
				logs := &openAIRecordUsageLogRepoStub{inserted: true}
				commands := &openAIRecordUsageBillingRepoStub{}
				account := &Account{ID: 3, Type: "apikey", Platform: "openai", Extra: map[string]any{"quota_limit": 100.0}}
				channels := &ChannelService{frozenPricing: true}
				channels.cache.Store(populateChannelCache(nil, map[int64]string{7: "openai"}))
				resolver := NewModelPricingResolver(channels, billing)
				if gateway == "openai" {
					svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, commands, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
					svc.billingService = billing
					svc.resolver = resolver
					svc.channelService = channels
					err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: key, User: &User{ID: 2}, Account: account, Result: &OpenAIForwardResult{RequestID: "split-openai", Model: "gpt-4o", Usage: OpenAIUsage{InputTokens: 1000}, Duration: time.Second}})
				} else {
					svc := newGatewayRecordUsageServiceWithBillingRepoForTest(logs, commands, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{})
					svc.billingService = billing
					svc.resolver = resolver
					svc.channelService = channels
					err = svc.RecordUsage(context.Background(), &RecordUsageInput{APIKey: key, User: &User{ID: 2}, Account: account, Result: &ForwardResult{RequestID: "split-generic", Model: "gpt-4o", Usage: ClaudeUsage{InputTokens: 1000}, Duration: time.Second}})
				}
				require.NoError(t, err)
				require.NotNil(t, logs.lastLog)
				require.InDelta(t, retailPrice*1000*1.7, logs.lastLog.ActualCost, 1e-9)
				require.InDelta(t, 1, *logs.lastLog.AccountStatsCost, 1e-9)
				require.InDelta(t, 0.2, *logs.lastLog.AccountRateMultiplier, 1e-9)
				require.InDelta(t, 0.2, commands.lastCmd.AccountQuotaCost, 1e-9)
				require.InDelta(t, retailPrice*1000*1.7, commands.lastCmd.BalanceCost, 1e-9)
				require.InDelta(t, 1, commands.lastCmd.ResellerBaseCost, 1e-9)
			})
		}
	}
}
