package moshureseller

import (
	"context"
	"fmt"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// EffectiveCostRate is the local account cost estimate, not an instruction to
// the upstream billing system. Settlement records always use its actual charge.
func (p Product) EffectiveCostRate() float64 {
	if p.CostRateOverride != nil {
		return *p.CostRateOverride
	}
	return p.CostRateMultiplier
}

// SetProductCost changes only local cost configuration, never creates a group,
// enables routing, changes customer pricing, or calls the upstream API.
// A nil rate restores catalog tracking; zero is a valid explicit override.
func (s *Service) SetProductCost(ctx context.Context, id int64, rate *float64) (*Product, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if rate != nil && (math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0 || *rate > 999999.9999) {
		return nil, fmt.Errorf("%w: cost multiplier must be between 0 and 999999.9999", ErrInvalidInput)
	}
	product, _, err := s.loadProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	if !product.Authorized {
		return nil, fmt.Errorf("%w: product authorization was revoked", ErrInvalidInput)
	}
	// Persist first: catalog reconciliation retries an interrupted account update
	// without losing the administrator's override or restoring the public rate.
	_, err = s.db.ExecContext(ctx, `UPDATE moshu_products SET cost_rate_override=$2,updated_at=NOW() WHERE id=$1`, id, rate)
	if err != nil {
		return nil, err
	}
	updated, _, err := s.loadProduct(ctx, id) // Read back DECIMAL precision.
	if err != nil {
		return nil, err
	}
	if updated.LocalAccountID != nil {
		account, err := s.admin.GetAccount(ctx, *updated.LocalAccountID)
		if err == nil {
			effectiveRate := updated.EffectiveCostRate()
			_, err = s.admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
				RateMultiplier: &effectiveRate, SkipMixedChannelCheck: true,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("cost setting saved; local account update pending, retry saving: %w", err)
		}
	}
	return updated, nil
}
