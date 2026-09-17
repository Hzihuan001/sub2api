package service

import "strings"

// IsMoshuResellerManaged identifies the product accounts provisioned by the
// reseller enrollment flow. Protocol conversion and upstream account retries
// belong to the main station for these accounts.
func (a *Account) IsMoshuResellerManaged() bool {
	if a == nil || a.Type != AccountTypeAPIKey {
		return false
	}
	managed, _ := a.Extra["moshu_reseller_managed"].(bool)
	return managed
}

// HasMoshuResellerModelSnapshot distinguishes a synchronized empty catalog
// from an older account that has not received model metadata yet.
func (a *Account) HasMoshuResellerModelSnapshot() bool {
	if a == nil || a.Extra == nil {
		return false
	}
	raw, exists := a.Extra[MoshuResellerModelSnapshotExtraKey]
	if !exists {
		return false
	}
	switch raw.(type) {
	case nil, []string, []any:
		return true
	default:
		return false
	}
}

func (a *Account) GetMoshuResellerModelSnapshot() []string {
	if a == nil || a.Extra == nil {
		return nil
	}
	var values []string
	switch raw := a.Extra[MoshuResellerModelSnapshotExtraKey].(type) {
	case []string:
		values = append(values, raw...)
	case []any:
		for _, value := range raw {
			if model, ok := value.(string); ok {
				values = append(values, model)
			}
		}
	}
	seen := make(map[string]struct{}, len(values))
	models := make([]string, 0, len(values))
	for _, model := range values {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		models = append(models, model)
	}
	return models
}
