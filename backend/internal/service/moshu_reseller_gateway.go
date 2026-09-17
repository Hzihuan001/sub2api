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

// GetMoshuResellerModelSnapshot returns the model candidates synchronized from
// the main-site product catalog. The snapshot is metadata for management UIs
// and account tests; the local group remains the routing authorization source.
func (a *Account) GetMoshuResellerModelSnapshot() []string {
	if a == nil || a.Extra == nil {
		return nil
	}
	raw := a.Extra[MoshuResellerModelSnapshotExtraKey]
	values := make([]string, 0)
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, value := range typed {
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
