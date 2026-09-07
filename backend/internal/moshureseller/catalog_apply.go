package moshureseller

import (
	"context"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"slices"
)

// Retry local application even on HTTP 304. Only upstream-owned fields change.
func (s *Service) applyCatalogConfiguration(ctx context.Context) error {
	products, err := s.listProducts(ctx)
	if err != nil {
		return err
	}
	for _, product := range products {
		if !product.Authorized {
			continue
		}
		if product.LocalGroupID != nil {
			group, err := s.admin.GetGroup(ctx, *product.LocalGroupID)
			if err != nil {
				return fmt.Errorf("load product %d group: %w", product.ID, err)
			}
			if group.Platform != product.Platform {
				return fmt.Errorf("product %d platform changed; migrate its local group explicitly", product.ID)
			}
			models := service.GroupModelsListConfig{Enabled: len(product.Models) > 0, Models: append([]string{}, product.Models...)}
			if group.ModelsListConfig.Enabled != models.Enabled || !slices.Equal(group.ModelsListConfig.Models, models.Models) {
				if _, err = s.admin.UpdateGroup(ctx, group.ID, &service.UpdateGroupInput{ModelsListConfig: &models}); err != nil {
					return fmt.Errorf("sync product %d models: %w", product.ID, err)
				}
			}
		}
		if product.LocalAccountID != nil {
			account, err := s.admin.GetAccount(ctx, *product.LocalAccountID)
			if err != nil {
				return fmt.Errorf("load product %d account: %w", product.ID, err)
			}
			if account.Platform != product.Platform {
				return fmt.Errorf("product %d platform changed; migrate its local account explicitly", product.ID)
			}
			if account.RateMultiplier == nil || *account.RateMultiplier != product.CostRateMultiplier {
				rate := product.CostRateMultiplier
				if _, err = s.admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
					Name: account.Name, Type: account.Type, Status: account.Status,
					Credentials: cloneMap(account.Credentials), Extra: cloneMap(account.Extra),
					GroupIDs: &account.GroupIDs, RateMultiplier: &rate, SkipMixedChannelCheck: true,
				}); err != nil {
					return fmt.Errorf("sync product %d cost: %w", product.ID, err)
				}
			}
		}
	}
	return nil
}
