package moshureseller

import (
	"context"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// EffectiveCostRate follows the billing user's effective upstream group rate.
// Legacy overrides are retained in storage for rollback, but never affect billing.
func (p Product) EffectiveCostRate() float64 {
	return p.CostRateMultiplier
}

// SetProductCost keeps the old reset endpoint compatible. Manual overrides are
// no longer accepted, including requests from stale browser bundles.
func (s *Service) SetProductCost(ctx context.Context, id int64, rate *float64) (*Product, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if rate != nil {
		return nil, fmt.Errorf("%w: cost multiplier follows the upstream billing account automatically", ErrInvalidInput)
	}
	product, _, err := s.loadProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	if !product.Authorized {
		return nil, fmt.Errorf("%w: product authorization was revoked", ErrInvalidInput)
	}
	// Persist first; catalog reconciliation retries an interrupted account update.
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
