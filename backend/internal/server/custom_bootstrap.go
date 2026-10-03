package server

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/custom"
)

// installBuiltinCustomProviders adds metadata adapters that are owned by the
// distribution itself to the process-wide custom host. The adapter is
// intentionally installed during router bootstrap, after any build-specific
// providers may have been configured, and only when an operator provider is
// not already present.
//
// OperatorPolicyProvider is metadata-only. The existing authorization
// middleware continues to read operator_policy.go's explicit tables, so this
// bootstrap step cannot grant or revoke a request by itself.
func installBuiltinCustomProviders() error {
	host := custom.DefaultHost()
	if host == nil {
		return fmt.Errorf("custom host is nil")
	}
	providers := host.Providers()
	if providers == nil {
		return fmt.Errorf("custom provider set is nil")
	}
	for _, provider := range providers.Permissions() {
		if provider.ID() == authz.NewOperatorPolicyProvider().ID() {
			return nil
		}
	}

	// Rebuild a validated set while preserving every provider already supplied
	// by a custom build. CustomProviderSet is immutable, so this avoids mutating
	// a set that route registration may already be inspecting.
	set, err := custom.NewCustomProviderSet(customBootstrapHooks{
		routes:      providers.Routes(),
		permissions: append(providers.Permissions(), authz.NewOperatorPolicyProvider()),
		settings:    providers.Settings(),
		workers:     providers.Workers(),
		enrichers:   providers.UsageEnrichers(),
	})
	if err != nil {
		return fmt.Errorf("build custom provider set: %w", err)
	}
	custom.ConfigureProviderSet(set)
	return nil
}

type customBootstrapHooks struct {
	routes      []custom.RouteProvider
	permissions []custom.PermissionProvider
	settings    []custom.SettingProvider
	workers     []custom.WorkerProvider
	enrichers   []custom.UsageEnricher
}

func (h customBootstrapHooks) RouteProviders() []custom.RouteProvider {
	return h.routes
}

func (h customBootstrapHooks) PermissionProviders() []custom.PermissionProvider {
	return h.permissions
}

func (h customBootstrapHooks) SettingProviders() []custom.SettingProvider {
	return h.settings
}

func (h customBootstrapHooks) WorkerProviders() []custom.WorkerProvider {
	return h.workers
}

func (h customBootstrapHooks) UsageEnrichers() []custom.UsageEnricher {
	return h.enrichers
}
