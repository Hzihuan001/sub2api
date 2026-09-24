package service

import (
	"net/url"
	"os"
	"strings"
)

const moshuResellerInternalURLEnv = "MOSHU_RESELLER_URL"

// resolveMoshuResellerBaseURL keeps reseller-managed accounts on the private
// station-to-main path when one is configured.  The catalog deliberately
// stores the public main-station URL so that it remains portable, but using
// that URL for every gateway request sends traffic back through Cloudflare or
// another public reverse proxy.  That is the source of the intermittent 524 /
// stream disconnects seen on a reseller station even while the main station
// itself is healthy.  The internal URL is only applied to accounts created by
// the reseller sync and is validated before use; ordinary accounts keep their
// configured endpoint unchanged.
func (a *Account) resolveMoshuResellerBaseURL(configured string) string {
	if !a.IsMoshuResellerManaged() {
		return configured
	}
	internal := strings.TrimRight(strings.TrimSpace(os.Getenv(moshuResellerInternalURLEnv)), "/")
	if internal == "" {
		return configured
	}
	parsed, err := url.Parse(internal)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return configured
	}
	return internal
}

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

// IsMoshuResellerPassthrough reports the common provider-neutral passthrough
// policy used by accounts provisioned from the main station. Treat managed
// API-key accounts as passthrough for compatibility with rows written before
// the marker was introduced; the marker is still persisted for auditing and
// future provider-specific paths.
func (a *Account) IsMoshuResellerPassthrough() bool {
	if !a.IsMoshuResellerManaged() {
		return false
	}
	if a.Extra == nil {
		return true
	}
	if enabled, ok := a.Extra[MoshuResellerPassthroughExtraKey].(bool); ok {
		return enabled
	}
	return true
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

// IsMoshuResellerModelSupported reports whether a reseller-managed account's
// synchronized product catalog contains the requested public model.  The
// snapshot is authoritative for these accounts: an empty snapshot means that
// the main station authorized no models (or has not published a usable
// catalog), so callers must not fall back to a built-in provider catalog.
//
// The second return value tells callers that the account has an authoritative
// snapshot, including an explicitly empty one.  Older rows without a snapshot
// return (false, false) so they keep the legacy compatibility behavior until
// the next catalog sync populates the snapshot.
func (a *Account) IsMoshuResellerModelSupported(requestedModel string) (supported, authoritative bool) {
	if a == nil || !a.IsMoshuResellerManaged() || !a.HasMoshuResellerModelSnapshot() {
		return false, false
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return false, true
	}
	// Keep the same provider-specific normalization used by account mappings,
	// while also accepting the conventional models/ prefix returned by a few
	// OpenAI-compatible catalogs.
	candidates := []string{requestedModel}
	if normalized := normalizeRequestedModelForLookup(a.Platform, requestedModel); normalized != requestedModel {
		candidates = append(candidates, normalized)
	}
	if strings.HasPrefix(requestedModel, "models/") {
		candidates = append(candidates, strings.TrimPrefix(requestedModel, "models/"))
	}
	for _, candidate := range candidates {
		for _, model := range a.GetMoshuResellerModelSnapshot() {
			if strings.EqualFold(candidate, model) {
				return true, true
			}
		}
	}
	return false, true
}
