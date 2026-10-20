package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type keyAccountPricingUnsafeTestKey struct{}

func TestKeyAccountPricingCarrierRestoresOnlyWhitelistedValues(t *testing.T) {
	at := time.Date(2026, 10, 20, 1, 2, 3, 0, time.UTC)
	group := &Group{ID: 7, Platform: PlatformAnthropic, Status: StatusActive, Hydrated: true,
		SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: .25,
		PeakRateEnabled: true, PeakStart: "09:00", PeakEnd: "19:00", PeakRateMultiplier: 2,
		ModelRouting: map[string][]int64{"model": {41}}, SupportedModelScopes: []string{"claude"}}
	gate := &openAIProfitControlGate{groupID: 7, platform: PlatformAnthropic, threshold: .13, pricingAt: at}
	old, oldCancel := context.WithCancel(context.Background())
	old = context.WithValue(old, keyAccountPricingUnsafeTestKey{}, "old-parser-wire-and-scope")
	old = context.WithValue(old, gatewayTokenRequestPricingAtCtxKey{}, at)
	old = context.WithValue(old, gatewayTokenRequestBillingGroupCtxKey{}, group)
	old = context.WithValue(old, openAIProfitControlGateCtxKey{}, gate)
	carrier, ok := CaptureKeyAccountPricingCarrier(old, 7, KeyAccountPricingGateway)
	require.True(t, ok)
	oldCancel()
	group.RateMultiplier, group.PeakRateMultiplier = 99, 99
	gate.threshold = 99

	fresh, freshCancel := context.WithCancel(context.Background())
	defer freshCancel()
	fresh = context.WithValue(fresh, keyAccountPricingUnsafeTestKey{}, "new-parser")
	restored, frozen, ok := carrier.Restore(fresh, 7, KeyAccountPricingGateway)
	require.True(t, ok)
	require.Equal(t, at, frozen)
	require.NoError(t, restored.Err(), "old wire cancellation must not leak")
	require.Equal(t, "new-parser", restored.Value(keyAccountPricingUnsafeTestKey{}))
	gotGroup := gatewayTokenRequestBillingGroupFromContext(restored)
	require.NotNil(t, gotGroup)
	require.Equal(t, .25, gotGroup.RateMultiplier)
	require.Equal(t, 2.0, gotGroup.PeakRateMultiplier)
	require.Nil(t, gotGroup.ModelRouting)
	require.Nil(t, gotGroup.SupportedModelScopes)
	gotGate, _ := restored.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
	require.Equal(t, .13, gotGate.threshold)
	// Each restore yields fresh mutable native pointers; a caller cannot change
	// the stored carrier for a later same-group handler re-entry.
	gotGroup.RateMultiplier, gotGate.threshold = 88, 88
	second, _, ok := carrier.Restore(fresh, 7, KeyAccountPricingGateway)
	require.True(t, ok)
	require.Equal(t, .25, gatewayTokenRequestBillingGroupFromContext(second).RateMultiplier)
	secondGate, _ := second.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
	require.Equal(t, .13, secondGate.threshold)
	freshCancel()
	require.ErrorIs(t, restored.Err(), context.Canceled, "clean ingress owns cancellation")
}

func TestKeyAccountPricingCarrierSeparatesGroupAndKindAndPreservesDeadline(t *testing.T) {
	at := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
	old := context.WithValue(context.Background(), openAIPricingAtCtxKey{}, at)
	old = WithOpenAIProfitControlSuppressed(old)
	carrier, ok := CaptureKeyAccountPricingCarrier(old, 4, KeyAccountPricingOpenAI)
	require.True(t, ok)
	for _, mismatch := range []struct {
		id   int64
		kind KeyAccountPricingKind
	}{{5, KeyAccountPricingOpenAI}, {4, KeyAccountPricingGateway}} {
		ctx := context.Background()
		got, _, ok := carrier.Restore(ctx, mismatch.id, mismatch.kind)
		require.False(t, ok)
		require.Equal(t, ctx, got)
	}
	deadline := time.Now().Add(time.Hour)
	fresh, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	restored, _, ok := carrier.Restore(fresh, 4, KeyAccountPricingOpenAI)
	require.True(t, ok)
	gotDeadline, ok := restored.Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, gotDeadline)
	_, suppressed := restored.Value(openAIProfitControlSuppressCtxKey{}).(struct{})
	require.True(t, suppressed)
	require.Zero(t, GatewayTokenRequestPricingAtFromContext(restored))
	_, ok = CaptureKeyAccountPricingCarrier(nil, 4, KeyAccountPricingOpenAI)
	require.False(t, ok)
	_, ok = CaptureKeyAccountPricingCarrier(context.Background(), 4, KeyAccountPricingOpenAI)
	require.False(t, ok)
}

func TestKeyAccountPricingCarrierSupplementsOnlyMatchingNativeGate(t *testing.T) {
	group := &Group{ID: 1, Platform: PlatformAnthropic, Hydrated: true, Status: StatusActive}
	ctx, at := WithGatewayTokenRequestPricing(context.WithValue(context.Background(), ctxkey.Group, group))
	carrier, ok := CaptureKeyAccountPricingCarrier(ctx, 1, KeyAccountPricingGateway)
	require.True(t, ok)
	gate := &openAIProfitControlGate{groupID: 10, platform: PlatformOpenAI, threshold: .17, pricingAt: at}
	ctx = context.WithValue(ctx, openAIProfitControlGateCtxKey{}, gate)
	carrier = carrier.WithNativeGate(ctx)
	gate.threshold = 99
	other := &openAIProfitControlGate{groupID: 2, pricingAt: at.Add(time.Second), threshold: 55}
	carrier = carrier.WithNativeGate(context.WithValue(context.Background(), openAIProfitControlGateCtxKey{}, other))
	restored, _, ok := carrier.Restore(context.Background(), 1, KeyAccountPricingGateway)
	require.True(t, ok)
	got, _ := restored.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate)
	require.EqualValues(t, 10, got.groupID, "preserve the native composite member's installed gate")
	require.Equal(t, .17, got.threshold)
}
