package moshureseller

import (
	"context"
	"errors"
	"fmt"
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
			// The local group owns model authorization. Catalog refreshes must not
			// overwrite a whitelist configured by the reseller administrator.
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
			credentials, passthroughChanged := withPassthroughCredentials(account.Credentials)
			extra, modelSnapshotChanged := withProductModelSnapshot(account.Extra, product.Models)
			extra, passthroughExtraChanged := withPassthroughExtra(extra, account.Platform, account.Type)
			if account.RateMultiplier == nil || *account.RateMultiplier != product.EffectiveCostRate() || passthroughChanged || passthroughExtraChanged || modelSnapshotChanged {
				rate := product.EffectiveCostRate()
				if _, err = s.admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
					Name: account.Name, Type: account.Type, Status: account.Status,
					Credentials: credentials, Extra: extra,
					GroupIDs: &account.GroupIDs, RateMultiplier: &rate, SkipMixedChannelCheck: true,
				}); err != nil {
					return fmt.Errorf("sync product %d cost: %w", product.ID, err)
				}
			}
		}
	}
	return nil
}

func withProductModelSnapshot(extra map[string]any, models []string) (map[string]any, bool) {
	normalized := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model != "" {
			normalized = append(normalized, model)
		}
	}
	raw, exists := extra[service.MoshuResellerModelSnapshotExtraKey]
	existing := resellerModelSnapshot(raw)
	if exists && slices.Equal(existing, normalized) {
		return extra, false
	}
	updated := cloneMap(extra)
	updated[service.MoshuResellerModelSnapshotExtraKey] = normalized
	return updated, true
}

func resellerModelSnapshot(raw any) []string {
	switch values := raw.(type) {
	case []string:
		return append([]string(nil), values...)
	case []any:
		models := make([]string, 0, len(values))
		for _, value := range values {
			if model, ok := value.(string); ok {
				models = append(models, model)
			}
		}
		return models
	default:
		return nil
	}
}

func withPassthroughCredentials(credentials map[string]any) (map[string]any, bool) {
	if _, exists := credentials["model_mapping"]; !exists {
		return credentials, false
	}
	updated := cloneMap(credentials)
	delete(updated, "model_mapping")
	return updated, true
}

func withPassthroughExtra(extra map[string]any, platform, accountType string) (map[string]any, bool) {
	key := ""
	switch {
	case platform == service.PlatformOpenAI:
		key = "openai_passthrough"
	case platform == service.PlatformAnthropic && accountType == service.AccountTypeAPIKey:
		key = "anthropic_passthrough"
	default:
		return extra, false
	}
	if enabled, ok := extra[key].(bool); ok && enabled {
		return extra, false
	}
	updated := cloneMap(extra)
	updated[key] = true
	return updated, true
}
