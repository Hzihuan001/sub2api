package custom

import (
	"fmt"
	"sort"
	"strings"
)

// FeatureID is a stable identifier for a custom feature.  It is deliberately
// independent of routes, database table names, and display labels so those
// details can change without changing settings or upgrade tooling.
type FeatureID string

const (
	FeatureOperator    FeatureID = "operator"
	FeaturePromptAudit FeatureID = "prompt-audit"
	FeatureImageStudio FeatureID = "image-studio"
	FeatureBranding    FeatureID = "branding"
	FeatureUsageExtras FeatureID = "usage-extras"
)

// FeatureManifest describes the stable identity and settings namespace of a
// custom feature.  A manifest is metadata only; registering one does not
// enable or execute the feature.
type FeatureManifest struct {
	ID                FeatureID
	SettingsNamespace string
	EnabledByDefault  bool
}

// BuiltInFeatureManifests returns the feature identities currently reserved by
// this distribution.  The returned slice is sorted by ID and copied so callers
// cannot mutate the package-level list.
func BuiltInFeatureManifests() []FeatureManifest {
	manifests := []FeatureManifest{
		{ID: FeatureOperator, SettingsNamespace: SettingNamespace(FeatureOperator)},
		{ID: FeaturePromptAudit, SettingsNamespace: SettingNamespace(FeaturePromptAudit)},
		{ID: FeatureImageStudio, SettingsNamespace: SettingNamespace(FeatureImageStudio)},
		{ID: FeatureBranding, SettingsNamespace: SettingNamespace(FeatureBranding)},
		{ID: FeatureUsageExtras, SettingsNamespace: SettingNamespace(FeatureUsageExtras)},
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].ID < manifests[j].ID })
	return manifests
}

// SettingNamespace returns the reserved settings prefix for a feature.
// Keeping this format stable prevents future upstream settings from colliding
// with custom values.
func SettingNamespace(id FeatureID) string {
	return "custom." + string(id)
}

// NamespacedSettingKey returns a fully qualified custom setting key.  Keys are
// intentionally restricted to non-empty dot-separated identifiers so they are
// safe to use in existing flat settings stores.
func NamespacedSettingKey(id FeatureID, key string) (string, error) {
	if !validFeatureID(id) {
		return "", fmt.Errorf("invalid custom feature id %q", id)
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid custom setting key %q", key)
	}
	for _, part := range strings.Split(key, ".") {
		if !validIdentifier(part) {
			return "", fmt.Errorf("invalid custom setting key %q", key)
		}
	}
	return SettingNamespace(id) + "." + key, nil
}

// ValidateFeatureManifests validates a feature manifest collection before it
// is exposed to application wiring.  IDs and settings namespaces must be
// unique; the input is not modified.
func ValidateFeatureManifests(manifests []FeatureManifest) error {
	ids := make(map[FeatureID]struct{}, len(manifests))
	namespaces := make(map[string]struct{}, len(manifests))
	for _, manifest := range manifests {
		if !validFeatureID(manifest.ID) {
			return fmt.Errorf("invalid custom feature id %q", manifest.ID)
		}
		namespace := strings.TrimSpace(manifest.SettingsNamespace)
		if namespace == "" {
			return fmt.Errorf("feature %q has empty settings namespace", manifest.ID)
		}
		if _, ok := ids[manifest.ID]; ok {
			return fmt.Errorf("duplicate custom feature id %q", manifest.ID)
		}
		if _, ok := namespaces[namespace]; ok {
			return fmt.Errorf("duplicate custom settings namespace %q", namespace)
		}
		ids[manifest.ID] = struct{}{}
		namespaces[namespace] = struct{}{}
	}
	return nil
}

func validFeatureID(id FeatureID) bool {
	return validIdentifier(string(id))
}

func validIdentifier(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (i > 0 && (r == '-' || r == '_')) {
			continue
		}
		return false
	}
	return true
}
