package moshureseller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
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
				if errors.Is(err, service.ErrGroupNotFound) {
					if _, stopErr := s.deactivateProduct(ctx, &product); stopErr != nil {
						return fmt.Errorf("stop product %d after its local group was deleted: %w", product.ID, stopErr)
					}
					continue
				}
				return fmt.Errorf("load product %d group: %w", product.ID, err)
			}
			if group.Platform != product.Platform {
				return fmt.Errorf("product %d platform changed; migrate its local group explicitly", product.ID)
			}
			models := service.GroupModelAllowlist{Enabled: len(product.Models) > 0, Models: append([]string{}, product.Models...)}
			if group.ModelAllowlist.Enabled != models.Enabled || !slices.Equal(group.ModelAllowlist.Models, models.Models) {
				if _, err = s.admin.UpdateGroup(ctx, group.ID, &service.UpdateGroupInput{ModelAllowlist: &models}); err != nil {
					return fmt.Errorf("sync product %d models: %w", product.ID, err)
				}
			}
		}
		if product.LocalAccountID != nil {
			account, err := s.admin.GetAccount(ctx, *product.LocalAccountID)
			if err != nil {
				if errors.Is(err, service.ErrAccountNotFound) {
					if _, stopErr := s.deactivateProduct(ctx, &product); stopErr != nil {
						return fmt.Errorf("stop product %d after its local account was deleted: %w", product.ID, stopErr)
					}
					continue
				}
				return fmt.Errorf("load product %d account: %w", product.ID, err)
			}
			if account.Platform != product.Platform {
				return fmt.Errorf("product %d platform changed; migrate its local account explicitly", product.ID)
			}
			credentials, modelsChanged := withProductModelMapping(account.Credentials, product.Models)
			if account.RateMultiplier == nil || *account.RateMultiplier != product.CostRateMultiplier || modelsChanged {
				rate := product.CostRateMultiplier
				if _, err = s.admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
					Name: account.Name, Type: account.Type, Status: account.Status,
					Credentials: credentials, Extra: cloneMap(account.Extra),
					GroupIDs: &account.GroupIDs, RateMultiplier: &rate, SkipMixedChannelCheck: true,
				}); err != nil {
					return fmt.Errorf("sync product %d cost: %w", product.ID, err)
				}
			}
		}
	}
	return nil
}

func withProductModelMapping(credentials map[string]any, models []string) (map[string]any, bool) {
	mapping := make(map[string]any, len(models))
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" {
			mapping[model] = model
		}
	}
	current, exists := credentials["model_mapping"]
	if len(mapping) == 0 {
		if !exists {
			return credentials, false
		}
		updated := cloneMap(credentials)
		delete(updated, "model_mapping")
		return updated, true
	}
	if exists && reflect.DeepEqual(current, mapping) {
		return credentials, false
	}
	updated := cloneMap(credentials)
	updated["model_mapping"] = mapping
	return updated, true
}
