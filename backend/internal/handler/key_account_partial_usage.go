package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// Compatibility handlers historically settle only complete responses. Keep
// their native success path unchanged, but retain actual partial usage when
// account_v2 prevents replay of a failed attempt. This adapter delegates to
// the existing worker and RecordUsage; it owns no billing calculation/write.
func (h *GatewayHandler) submitKeyAccountPartialForwardUsage(c *gin.Context, result *service.ForwardResult, key *service.APIKey, account *service.Account, subscription *service.UserSubscription, pricingAt time.Time, body []byte, mapping service.ChannelMappingResult, model string) {
	input := keyAccountPartialForwardUsageInput(c, result, key, account, subscription, pricingAt, body, mapping, model)
	if input == nil {
		return
	}
	input.APIKeyService = h.apiKeyService
	h.submitUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, input); err != nil {
			slog.ErrorContext(ctx, "key_account_partial_usage_record_failed", "key_id", input.APIKey.ID, "account_id", input.Account.ID, "error", err)
		}
	})
}

func keyAccountPartialForwardUsageInput(c *gin.Context, result *service.ForwardResult, key *service.APIKey, account *service.Account, subscription *service.UserSubscription, pricingAt time.Time, body []byte, mapping service.ChannelMappingResult, model string) *service.RecordUsageInput {
	if c == nil || c.Request == nil || service.GetKeyAccountRoutingSession(c.Request.Context()) == nil || key == nil || account == nil || !service.KeyAccountForwardResultHasUsage(result) {
		return nil
	}
	// Capture all Gin-derived fields now. No async callback may read a later
	// route's mutable Gin state, even when the error is returned after streaming.
	frozenKey := *key
	if key.GroupID != nil {
		groupID := *key.GroupID
		frozenKey.GroupID = &groupID
	}
	stampForwardRequestedReasoningEffort(result, service.RequestedReasoningEffortFromContext(c.Request.Context()))
	return &service.RecordUsageInput{
		Result:             result,
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), key),
		APIKey:             &frozenKey,
		User:               key.User,
		Account:            account,
		Subscription:       subscription,
		PricingAt:          pricingAt,
		InboundEndpoint:    GetInboundEndpoint(c),
		UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
		UserAgent:          c.GetHeader("User-Agent"),
		IPAddress:          ip.GetClientIP(c),
		RequestPayloadHash: service.HashUsageRequestPayload(body),
		SessionID:          service.ExtractClientSessionID(c),
		ChannelUsageFields: clientRequestedUsageFields(c, mapping, model, result.UpstreamModel),
	}
}
