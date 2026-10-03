// Package custom contains the narrow extension points used by the custom
// distribution of Sub2API.
//
// The package deliberately does not register anything by itself.  Upstream
// application wiring can depend on these small interfaces without depending
// on a particular custom feature.  Feature implementations live in their own
// packages and are assembled by the custom build.
package custom

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/gin-gonic/gin"
)

// RouteProvider registers routes owned by one custom feature.  A provider may
// implement only one side of the surface by making the other method a no-op.
// Registration order is deterministic and follows ID order.
type RouteProvider interface {
	ID() string
	RegisterAdminRoutes(router gin.IRouter)
	RegisterGatewayRoutes(router gin.IRouter)
}

// Permission describes a permission exposed by a custom feature.  Name is a
// stable machine-readable identifier; it must be unique within the permission
// provider set.
type Permission struct {
	Name        string
	Description string
}

// PermissionProvider exposes custom permissions without coupling the core
// authorization middleware to a feature package.
type PermissionProvider interface {
	ID() string
	Permissions() []Permission
}

// Setting describes a custom setting.  Value is intentionally left as an
// interface so the extension layer can carry typed JSON-compatible defaults
// without importing the core settings DTOs.
type Setting struct {
	Key         string
	Description string
	Default     any
}

// SettingProvider exposes settings owned by a custom feature.
type SettingProvider interface {
	ID() string
	Settings() []Setting
}

// WorkerProvider describes a background worker owned by a custom feature.
// The core application decides when to start and stop it; registering a
// provider alone has no side effects.
type WorkerProvider interface {
	ID() string
	Start(ctx context.Context) error
}

// UsageRecord is the extension-neutral portion of a usage event.  The core
// usage model stays in the upstream package; custom enrichers add optional
// JSON-compatible fields without changing the upstream model.
type UsageRecord struct {
	Values map[string]any
}

// UsageEnricher can add custom fields to a usage record.  Enrichment errors
// are returned to the caller so the caller can apply its existing fail-open or
// fail-closed policy explicitly.
type UsageEnricher interface {
	ID() string
	EnrichUsage(ctx context.Context, record *UsageRecord) error
}

// CustomHooks is the dependency-injection boundary for custom features.  The
// methods return providers, not concrete services, so upstream changes to
// service constructors do not leak into feature implementations.
type CustomHooks interface {
	RouteProviders() []RouteProvider
	PermissionProviders() []PermissionProvider
	SettingProviders() []SettingProvider
	WorkerProviders() []WorkerProvider
	UsageEnrichers() []UsageEnricher
}

// CustomProviderSet is the validated, deterministic collection of custom
// providers.  It has no side effects until the application explicitly invokes
// one of the providers.
type CustomProviderSet struct {
	routes      []RouteProvider
	permissions []PermissionProvider
	settings    []SettingProvider
	workers     []WorkerProvider
	enrichers   []UsageEnricher
}

// NewCustomProviderSet validates and normalizes a hook collection.  Providers
// are sorted by stable ID and duplicate IDs within a provider category are
// rejected.  The input slices are copied, so callers may safely reuse or
// mutate their own slices after construction.
func NewCustomProviderSet(hooks CustomHooks) (*CustomProviderSet, error) {
	if hooks == nil {
		return &CustomProviderSet{}, nil
	}

	set := &CustomProviderSet{
		routes:      append([]RouteProvider(nil), hooks.RouteProviders()...),
		permissions: append([]PermissionProvider(nil), hooks.PermissionProviders()...),
		settings:    append([]SettingProvider(nil), hooks.SettingProviders()...),
		workers:     append([]WorkerProvider(nil), hooks.WorkerProviders()...),
		enrichers:   append([]UsageEnricher(nil), hooks.UsageEnrichers()...),
	}

	if err := validateAndSort("route", set.routes, func(p RouteProvider) string { return p.ID() }); err != nil {
		return nil, err
	}
	if err := validateAndSort("permission", set.permissions, func(p PermissionProvider) string { return p.ID() }); err != nil {
		return nil, err
	}
	if err := validateAndSort("setting", set.settings, func(p SettingProvider) string { return p.ID() }); err != nil {
		return nil, err
	}
	if err := validateAndSort("worker", set.workers, func(p WorkerProvider) string { return p.ID() }); err != nil {
		return nil, err
	}
	if err := validateAndSort("usage enricher", set.enrichers, func(p UsageEnricher) string { return p.ID() }); err != nil {
		return nil, err
	}
	return set, nil
}

// Routes returns a copy of the ordered route providers.
func (s *CustomProviderSet) Routes() []RouteProvider {
	if s == nil {
		return nil
	}
	return append([]RouteProvider(nil), s.routes...)
}

// Permissions returns a copy of the ordered permission providers.
func (s *CustomProviderSet) Permissions() []PermissionProvider {
	if s == nil {
		return nil
	}
	return append([]PermissionProvider(nil), s.permissions...)
}

// Settings returns a copy of the ordered setting providers.
func (s *CustomProviderSet) Settings() []SettingProvider {
	if s == nil {
		return nil
	}
	return append([]SettingProvider(nil), s.settings...)
}

// Workers returns a copy of the ordered worker providers.
func (s *CustomProviderSet) Workers() []WorkerProvider {
	if s == nil {
		return nil
	}
	return append([]WorkerProvider(nil), s.workers...)
}

// UsageEnrichers returns a copy of the ordered usage enrichers.
func (s *CustomProviderSet) UsageEnrichers() []UsageEnricher {
	if s == nil {
		return nil
	}
	return append([]UsageEnricher(nil), s.enrichers...)
}

var (
	// ErrDuplicateProviderID indicates that two providers in the same category
	// use the same ID and therefore cannot have deterministic ownership.
	ErrDuplicateProviderID = errors.New("duplicate custom provider id")
	// ErrInvalidProvider indicates a nil provider or an empty provider ID.
	ErrInvalidProvider = errors.New("invalid custom provider")
)

type provider interface{ ID() string }

func validateAndSort[T provider](kind string, providers []T, idOf func(T) string) error {
	seen := make(map[string]struct{}, len(providers))
	for _, p := range providers {
		if any(p) == nil {
			return fmt.Errorf("%w: nil %s provider", ErrInvalidProvider, kind)
		}
		id := idOf(p)
		if id == "" {
			return fmt.Errorf("%w: %s provider has empty id", ErrInvalidProvider, kind)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%w: %s %q", ErrDuplicateProviderID, kind, id)
		}
		seen[id] = struct{}{}
	}
	sort.SliceStable(providers, func(i, j int) bool {
		return idOf(providers[i]) < idOf(providers[j])
	})
	return nil
}
