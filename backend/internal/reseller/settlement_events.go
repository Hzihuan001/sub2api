package reseller

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (s *Service) ListSettlementEvents(ctx context.Context, resellerID int64, limit int) (*SettlementEventPage, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	// Confirmed events are retained for seven days for diagnosis. Cleanup is
	// opportunistic and never removes pending deliveries.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM reseller_settlement_events WHERE acknowledged_at IS NOT NULL AND acknowledged_at < NOW()-INTERVAL '7 days'`)
	rows, err := s.db.QueryContext(ctx, `
		SELECT e.id,e.revision,
		       rs.id,rs.revision,rs.request_source,rs.request_id::text,rs.upstream_request_id,rs.product_id,
		       rp.product_code,rp.display_name,rs.moshu_group_id,rs.requested_model,
		       rs.upstream_model,rs.service_tier,rs.input_tokens,rs.output_tokens,
		       rs.cache_creation_tokens,rs.cache_read_tokens,rs.image_count,
		       rs.video_count,rs.standard_cost,rs.cost_rate_multiplier,rs.actual_cost,
		       rs.price_catalog_version,rs.status,rs.error_type,rs.started_at,
		       rs.completed_at,rs.created_at
		FROM reseller_settlement_events e
		JOIN reseller_request_settlements rs ON rs.id=e.settlement_id
		JOIN reseller_products rp ON rp.id=rs.product_id
		WHERE e.reseller_id=$1 AND e.acknowledged_at IS NULL
		ORDER BY e.id ASC LIMIT $2`, resellerID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := &SettlementEventPage{Items: make([]SettlementEvent, 0, limit)}
	for rows.Next() {
		var event SettlementEvent
		var upstreamID, requestedModel, upstreamModel, tier, errorType sql.NullString
		var completedAt sql.NullTime
		item := &event.Settlement
		if err := rows.Scan(&event.EventID, &event.Revision,
			&item.ID, &item.Revision, &item.RequestSource, &item.RequestID, &upstreamID, &item.ProductID,
			&item.ProductCode, &item.DisplayName, &item.MoshuGroupID, &requestedModel,
			&upstreamModel, &tier, &item.InputTokens, &item.OutputTokens,
			&item.CacheCreationTokens, &item.CacheReadTokens, &item.ImageCount,
			&item.VideoCount, &item.StandardCost, &item.CostRateMultiplier,
			&item.ActualCost, &item.PriceCatalogVersion, &item.Status, &errorType,
			&item.StartedAt, &completedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		item.UpstreamRequestID = nullStringPtr(upstreamID)
		item.RequestedModel = nullStringPtr(requestedModel)
		item.UpstreamModel = nullStringPtr(upstreamModel)
		item.ServiceTier = nullStringPtr(tier)
		item.ErrorType = nullStringPtr(errorType)
		if completedAt.Valid {
			item.CompletedAt = &completedAt.Time
		}
		result.Items = append(result.Items, event)
	}
	return result, rows.Err()
}

func (s *Service) AcknowledgeSettlementEvents(ctx context.Context, resellerID int64, eventIDs []int64) error {
	if len(eventIDs) == 0 || len(eventIDs) > 500 {
		return ErrInvalidInput
	}
	normalized := normalizeIDs(eventIDs)
	if len(normalized) == 0 {
		return ErrInvalidInput
	}
	var known int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reseller_settlement_events
		WHERE reseller_id=$1 AND id=ANY($2::bigint[])`, resellerID, pgInt64Array(normalized)).Scan(&known); err != nil {
		return err
	}
	// Acknowledge is idempotent for known IDs, but silently accepting an ID
	// belonging to another tenant (or a typo) hides a delivery bug and can
	// make the proxy believe settlement data was persisted when it was not.
	if known != len(normalized) {
		return ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE reseller_settlement_events SET acknowledged_at=COALESCE(acknowledged_at,NOW())
		WHERE reseller_id=$1 AND id=ANY($2::bigint[])`, resellerID, pgInt64Array(normalized))
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != int64(len(normalized)) {
		return ErrNotFound
	}
	return nil
}

func (h *Handler) ListSettlementEvents(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "500"))
	result, err := h.service.ListSettlementEvents(c.Request.Context(), resellerID, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) AcknowledgeSettlementEvents(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	var input struct {
		EventIDs []int64 `json:"event_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	if err := h.service.AcknowledgeSettlementEvents(c.Request.Context(), resellerID, input.EventIDs); err != nil {
		if errors.Is(err, ErrInvalidInput) {
			response.BadRequest(c, "event_ids must contain between 1 and 500 items")
			return
		}
		h.writeError(c, err)
		return
	}
	response.Success(c, gin.H{"acknowledged": len(normalizeIDs(input.EventIDs))})
}
