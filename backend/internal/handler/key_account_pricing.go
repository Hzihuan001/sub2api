package handler

import (
	"context"
	"time"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const keyAccountPricingCarriersKey = "custom.key_account_pricing_carriers"

type keyAccountPricingSlot struct {
	groupID int64
	kind    service.KeyAccountPricingKind
}

func keyAccountGatewayRequestPricing(c *gin.Context, ctx context.Context) (context.Context, time.Time) {
	var groupID *int64
	if c != nil {
		if key, ok := servermiddleware.GetAPIKeyFromContext(c); ok && key != nil {
			groupID = key.GroupID
		}
	}
	return keyAccountRequestPricing(c, ctx, groupID, service.KeyAccountPricingGateway, service.WithGatewayTokenRequestPricing)
}

func keyAccountOpenAIRequestPricing(c *gin.Context, ctx context.Context, groupID *int64, install func(context.Context, *int64) (context.Context, time.Time)) (context.Context, time.Time) {
	return keyAccountRequestPricing(c, ctx, groupID, service.KeyAccountPricingOpenAI, func(ctx context.Context) (context.Context, time.Time) {
		return install(ctx, groupID)
	})
}

// Non-account-scoped requests call the native installer exactly as before.
// Only a live request session owns the small, request-local per-group cache.
func keyAccountRequestPricing(c *gin.Context, ctx context.Context, groupID *int64, kind service.KeyAccountPricingKind, install func(context.Context) (context.Context, time.Time)) (context.Context, time.Time) {
	if c == nil || service.GetKeyAccountRoutingSession(ctx) == nil || groupID == nil || *groupID <= 0 {
		return install(ctx)
	}
	value, _ := c.Get(keyAccountPricingCarriersKey)
	carriers, _ := value.(map[keyAccountPricingSlot]service.KeyAccountPricingCarrier)
	if carriers == nil {
		carriers = make(map[keyAccountPricingSlot]service.KeyAccountPricingCarrier)
		c.Set(keyAccountPricingCarriersKey, carriers)
	}
	slot := keyAccountPricingSlot{groupID: *groupID, kind: kind}
	if carrier, exists := carriers[slot]; exists {
		if restored, at, ok := carrier.Restore(ctx, *groupID, kind); ok {
			return restored, at
		}
	}
	priced, at := install(ctx)
	if carrier, ok := service.CaptureKeyAccountPricingCarrier(priced, *groupID, kind); ok {
		carriers[slot] = carrier
	}
	return priced, at
}

// Called immediately before native Forward, after the scheduler may have
// added its profit gate. Refresh only already-frozen pricing slots; no context
// is saved, and a new group cannot inherit another group's timestamp.
func rememberKeyAccountPricingGate(c *gin.Context, ctx context.Context) {
	if c == nil || service.GetKeyAccountRoutingSession(ctx) == nil {
		return
	}
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.GroupID == nil {
		return
	}
	value, _ := c.Get(keyAccountPricingCarriersKey)
	carriers, _ := value.(map[keyAccountPricingSlot]service.KeyAccountPricingCarrier)
	for slot, carrier := range carriers {
		if slot.groupID == *key.GroupID {
			carriers[slot] = carrier.WithNativeGate(ctx)
		}
	}
}
