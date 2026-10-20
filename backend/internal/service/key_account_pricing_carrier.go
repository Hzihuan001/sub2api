package service

import (
	"context"
	"time"
)

// KeyAccountPricingKind separates native OpenAI and shared-gateway pricing
// contexts. A group re-entry may reuse only the kind that originally priced it.
type KeyAccountPricingKind string

const (
	KeyAccountPricingOpenAI  KeyAccountPricingKind = "openai"
	KeyAccountPricingGateway KeyAccountPricingKind = "gateway"
)

// KeyAccountPricingCarrier is a value-only allowlist, not a saved context. It
// intentionally carries no parser, wire, guard, scope or cancellation parent.
// Private fields prevent callers from fabricating or mutating a pricing gate.
type KeyAccountPricingCarrier struct {
	groupID      int64
	kind         KeyAccountPricingKind
	pricingAt    time.Time
	billingGroup Group
	hasBilling   bool
	gate         openAIProfitControlGate
	hasGate      bool
	suppressed   bool
}

func CaptureKeyAccountPricingCarrier(ctx context.Context, groupID int64, kind KeyAccountPricingKind) (KeyAccountPricingCarrier, bool) {
	var result KeyAccountPricingCarrier
	if ctx == nil || groupID <= 0 {
		return result, false
	}
	switch kind {
	case KeyAccountPricingOpenAI:
		result.pricingAt, _ = openAIPricingAtFromContext(ctx)
	case KeyAccountPricingGateway:
		result.pricingAt, _ = gatewayTokenRequestPricingAtFromContext(ctx)
		if group := gatewayTokenRequestBillingGroupFromContext(ctx); group != nil {
			result.billingGroup = keyAccountPricingBillingGroup(group)
			result.hasBilling = true
		}
	default:
		return result, false
	}
	if result.pricingAt.IsZero() {
		return result, false
	}
	result.groupID, result.kind = groupID, kind
	_, result.suppressed = ctx.Value(openAIProfitControlSuppressCtxKey{}).(struct{})
	return result.WithNativeGate(ctx), true
}

// WithNativeGate supplements the initial gateway timestamp after the native
// scheduler has installed its gate. It never refreshes price/time/configuration.
// A composite member's native gate remains valid only under native group checks.
func (c KeyAccountPricingCarrier) WithNativeGate(ctx context.Context) KeyAccountPricingCarrier {
	if ctx == nil || c.groupID <= 0 || c.pricingAt.IsZero() {
		return c
	}
	gate, _ := ctx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
	if gate != nil && gate.pricingAt.Equal(c.pricingAt) {
		c.gate, c.hasGate = *gate, true
	}
	return c
}

// Restore adds only the approved native pricing values to the clean ingress
// context. Its cancellation, deadline, routing and freshly parsed values remain
// those of dst. A foreign group or kind cannot reuse this carrier.
func (c KeyAccountPricingCarrier) Restore(dst context.Context, groupID int64, kind KeyAccountPricingKind) (context.Context, time.Time, bool) {
	if dst == nil || groupID != c.groupID || kind != c.kind || c.pricingAt.IsZero() {
		return dst, time.Time{}, false
	}
	switch kind {
	case KeyAccountPricingOpenAI:
		dst = context.WithValue(dst, openAIPricingAtCtxKey{}, c.pricingAt)
	case KeyAccountPricingGateway:
		dst = context.WithValue(dst, gatewayTokenRequestPricingAtCtxKey{}, c.pricingAt)
		if c.hasBilling {
			group := c.billingGroup
			dst = context.WithValue(dst, gatewayTokenRequestBillingGroupCtxKey{}, &group)
		}
	default:
		return dst, time.Time{}, false
	}
	// A fresh restore must not inherit a gate from an unrelated native route.
	var gate *openAIProfitControlGate
	if c.hasGate {
		value := c.gate
		gate = &value
	}
	dst = context.WithValue(dst, openAIProfitControlGateCtxKey{}, gate)
	if c.suppressed {
		dst = WithOpenAIProfitControlSuppressed(dst)
	}
	return dst, c.pricingAt, true
}

// gatewayTokenRequestBillingGroupFromContext is consumed solely by native
// profit pricing: validity, identity, base rate and peak multiplier. Copy only
// these scalars, never Group's maps, slices, nested account data or pointers.
// This partial group is private pricing data, never an authorization snapshot.
func keyAccountPricingBillingGroup(group *Group) Group {
	return Group{
		ID: group.ID, Platform: group.Platform, Status: group.Status, Hydrated: group.Hydrated,
		SubscriptionType: group.SubscriptionType, RateMultiplier: group.RateMultiplier,
		PeakRateEnabled: group.PeakRateEnabled, PeakStart: group.PeakStart, PeakEnd: group.PeakEnd,
		PeakRateMultiplier: group.PeakRateMultiplier,
	}
}
