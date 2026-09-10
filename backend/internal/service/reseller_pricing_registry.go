package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type resellerPricingSet struct {
	groups      map[int64]*ResellerPricingCalculator
	unavailable bool
}

var activeResellerPrices atomic.Pointer[resellerPricingSet]

func SuspendResellerPricing() { activeResellerPrices.Store(&resellerPricingSet{unavailable: true}) }

func ResellerPricingNeedsSync() bool {
	set := activeResellerPrices.Load()
	if set == nil {
		return false
	}
	if set.unavailable {
		return true
	}
	for _, calculator := range set.groups {
		if calculator == nil {
			return true
		}
	}
	return false
}

// ReplaceResellerPricing publishes an entire generation in one atomic swap.
// A nil entry marks a managed group whose valid price has not arrived yet.
func ReplaceResellerPricing(groups map[int64]*ResellerPricingCalculator) {
	copy := make(map[int64]*ResellerPricingCalculator, len(groups))
	for id, calculator := range groups {
		copy[id] = calculator
	}
	activeResellerPrices.Store(&resellerPricingSet{groups: copy})
}

// PinResellerPricing only reads memory and returns a request-owned API key copy.
// Refuse a managed group with no snapshot rather than silently charging default prices.
func PinResellerPricing(key *APIKey) (*APIKey, error) {
	if key == nil || key.Group == nil {
		return key, nil
	}
	set := activeResellerPrices.Load()
	if set == nil {
		return key, nil
	}
	if set.unavailable {
		return nil, fmt.Errorf("upstream pricing is unavailable; please retry shortly")
	}
	calculator, managed := set.groups[key.Group.ID]
	if !managed {
		return key, nil
	}
	if calculator == nil {
		return nil, fmt.Errorf("upstream pricing is synchronizing; please retry shortly")
	}
	copy := *key
	group := *key.Group
	copy.Group = &group
	copy.Group.resellerPricing = calculator
	copy.Group.resellerPricingAt = time.Now()
	return &copy, nil
}

func resellerPricingFromContext(ctx context.Context) *ResellerPricingCalculator {
	if ctx == nil {
		return nil
	}
	if group := gatewayTokenRequestBillingGroupFromContext(ctx); group != nil && group.resellerPricing != nil {
		return group.resellerPricing
	}
	if group, ok := ctx.Value(ctxkey.Group).(*Group); ok && group != nil {
		return group.resellerPricing
	}
	return nil
}

func resellerCalculator(input CostInput) *ResellerPricingCalculator {
	if input.Group != nil && input.Group.resellerPricing != nil {
		return input.Group.resellerPricing
	}
	return resellerPricingFromContext(input.Ctx)
}

func resellerAccountRate(group *Group, account *Account, at time.Time, image, video bool) float64 {
	if group == nil || group.resellerPricing == nil {
		return account.BillingRateMultiplier()
	}
	calculator := group.resellerPricing
	p := calculator.snapshot.Group
	if video && p.VideoRateIndependent {
		return p.VideoRateMultiplier
	}
	if image && p.ImageRateIndependent {
		return p.ImageRateMultiplier
	}
	if image || video {
		return calculator.CostRate()
	}
	peak := &Group{PeakRateEnabled: p.PeakRateEnabled, PeakStart: p.PeakStart, PeakEnd: p.PeakEnd, PeakRateMultiplier: p.PeakRateMultiplier}
	return calculator.CostRate() * peak.PeakMultiplierAt(at)
}

func (s *ResellerPricingCalculator) costKey(key *APIKey) *APIKey {
	copy := *key
	copy.Group = s.ApplyPriceFields(key.Group)
	copy.Group.resellerPricing = nil
	return &copy
}

func resellerUsageBaseCost(key *APIKey, usage *UsageLog) *float64 {
	if key != nil && key.Group != nil && key.Group.resellerPricing != nil && usage != nil {
		return usage.AccountStatsCost
	}
	return nil
}

func hasLocalResellerModelPrice(ctx context.Context, resolver *ModelPricingResolver, group *Group, model string) bool {
	if matchGroupModelPricing(group, model) != nil {
		return true
	}
	return resolver != nil && group != nil && resolver.lookupChannelPricingNormalized(ctx, group.ID, model) != nil
}

func (s *ResellerPricingCalculator) costModel(ctx context.Context, requested, upstream, response string, conflict bool) string {
	mapping := s.resolver.channelService.ResolveChannelMapping(ctx, s.groupID, requested)
	switch mapping.BillingModelSource {
	case BillingModelSourceRequested:
		return requested
	case BillingModelSourceChannelMapped:
		return firstNonEmpty(mapping.MappedModel, upstream, requested)
	case BillingModelSourceResponse:
		if !conflict && response != "" {
			resolved := s.resolver.Resolve(ctx, PricingInput{Model: response, GroupID: &s.groupID, Group: s.ApplyPriceFields(nil)})
			if resolved.Source == PricingSourceGroup || resolved.Source == PricingSourceChannel || s.billing.HasIdentifiedTokenPricing(response) {
				return response
			}
		}
	}
	return firstNonEmpty(upstream, requested)
}
