package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type keyAccountPricingParserTestKey struct{}

func TestKeyAccountPricingWrapperFreezesSameGroupAndSeparatesKinds(t *testing.T) {
	f := newAccountDispatchFixture(t)
	c := f.c
	base := c.Request.Context()
	key, _ := servermiddleware.GetAPIKeyFromContext(c)
	key.Group.Hydrated, key.Group.Status, key.Group.RateMultiplier = true, service.StatusActive, .25
	base = context.WithValue(base, ctxkey.Group, key.Group)
	calls := 0
	native := (&service.OpenAIGatewayService{}).WithOpenAIRequestPricingContext
	install := func(ctx context.Context, groupID *int64) (context.Context, time.Time) {
		calls++
		return native(ctx, groupID)
	}
	first, at := keyAccountOpenAIRequestPricing(c, base, key.GroupID, install)
	require.Equal(t, 1, calls)
	old, cancelOld := context.WithCancel(first)
	old = context.WithValue(old, keyAccountPricingParserTestKey{}, "old-parser")
	rememberKeyAccountPricingGate(c, old)
	cancelOld()
	clean := context.WithValue(base, keyAccountPricingParserTestKey{}, "new-parser")
	second, again := keyAccountOpenAIRequestPricing(c, clean, key.GroupID, install)
	require.Equal(t, 1, calls, "same-group handler re-entry must not reinstall or reprice")
	require.Equal(t, at, again)
	require.NoError(t, second.Err())
	require.Equal(t, "new-parser", second.Value(keyAccountPricingParserTestKey{}))
	require.Equal(t, at, service.OpenAIPricingAtFromContext(second))
	shared, sharedAt := keyAccountGatewayRequestPricing(c, clean)
	require.False(t, sharedAt.IsZero())
	require.Equal(t, sharedAt, service.GatewayTokenRequestPricingAtFromContext(shared))
	value, _ := c.Get(keyAccountPricingCarriersKey)
	carriers := value.(map[keyAccountPricingSlot]service.KeyAccountPricingCarrier)
	require.Len(t, carriers, 2, "the same group keeps separate native pricing kinds")
}

func TestKeyAccountPricingWrapperDifferentGroupUsesIndependentInstaller(t *testing.T) {
	f := newAccountDispatchFixture(t)
	c := f.c
	key, _ := servermiddleware.GetAPIKeyFromContext(c)
	base := c.Request.Context()
	native := (&service.OpenAIGatewayService{}).WithOpenAIRequestPricingContext
	calls := 0
	install := func(ctx context.Context, groupID *int64) (context.Context, time.Time) {
		calls++
		return native(ctx, groupID)
	}
	_, primaryAt := keyAccountOpenAIRequestPricing(c, base, key.GroupID, install)
	backup := f.selector.next[*key.GroupID]
	c.Set(string(servermiddleware.ContextKeyAPIKey), backup)
	backupBase := context.WithValue(base, ctxkey.Group, backup.Group)
	_, backupAt := keyAccountOpenAIRequestPricing(c, backupBase, backup.GroupID, install)
	require.Equal(t, 2, calls)
	_, again := keyAccountOpenAIRequestPricing(c, backupBase, backup.GroupID, install)
	require.Equal(t, backupAt, again)
	require.Equal(t, 2, calls)
	c.Set(string(servermiddleware.ContextKeyAPIKey), key)
	_, again = keyAccountOpenAIRequestPricing(c, base, key.GroupID, install)
	require.Equal(t, primaryAt, again)
	require.Equal(t, 2, calls)
}

func TestKeyAccountPricingWrapperDoesNotRecomputeSameGroupProfitGate(t *testing.T) {
	f := newAccountDispatchFixture(t)
	c := f.c
	key, _ := servermiddleware.GetAPIKeyFromContext(c)
	key.Group.Hydrated, key.Group.Status, key.Group.RateMultiplier = true, service.StatusActive, .25
	key.Group.ProfitControlEnabled, key.Group.ProfitMinMargin = true, .1
	base := context.WithValue(c.Request.Context(), ctxkey.Group, key.Group)
	base = context.WithValue(base, ctxkey.UserID, int64(0))
	native := (&service.OpenAIGatewayService{}).WithOpenAIRequestPricingContext
	first, firstAt := keyAccountOpenAIRequestPricing(c, base, key.GroupID, native)
	cost := .2
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, RateMultiplier: &cost}
	veto, _ := service.OpenAIProfitControlVeto(first, account)
	require.False(t, veto)
	// If same-group re-entry called the native installer again, a modified
	// downstream rate would produce a new .09 threshold and reject this .2
	// account. The carrier preserves the original native .225 gate instead.
	key.Group.RateMultiplier = .1
	second, secondAt := keyAccountOpenAIRequestPricing(c, base, key.GroupID, native)
	require.Equal(t, firstAt, secondAt)
	veto, _ = service.OpenAIProfitControlVeto(second, account)
	require.False(t, veto)
	fresh, _ := native(base, key.GroupID)
	veto, _ = service.OpenAIProfitControlVeto(fresh, account)
	require.True(t, veto, "the test must prove a fresh native gate really changed")
}

func TestKeyAccountPricingWrapperWithoutSessionIsExactNativePassThrough(t *testing.T) {
	c, _ := newDispatchTestContext(t)
	base := c.Request.Context()
	id := int64(1)
	calls := 0
	install := func(ctx context.Context, groupID *int64) (context.Context, time.Time) {
		calls++
		require.Equal(t, base, ctx)
		require.Same(t, &id, groupID)
		return ctx, time.Unix(int64(calls), 0)
	}
	for i := 1; i <= 3; i++ {
		ctx, at := keyAccountOpenAIRequestPricing(c, base, &id, install)
		require.Equal(t, base, ctx)
		require.Equal(t, time.Unix(int64(i), 0), at)
	}
	require.Equal(t, 3, calls)
	_, exists := c.Get(keyAccountPricingCarriersKey)
	require.False(t, exists, "ordinary requests must not allocate custom pricing state")
}
