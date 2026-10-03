package custom

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

type testHooks struct {
	routes      []RouteProvider
	permissions []PermissionProvider
	settings    []SettingProvider
	workers     []WorkerProvider
	enrichers   []UsageEnricher
}

func (h testHooks) RouteProviders() []RouteProvider           { return h.routes }
func (h testHooks) PermissionProviders() []PermissionProvider { return h.permissions }
func (h testHooks) SettingProviders() []SettingProvider       { return h.settings }
func (h testHooks) WorkerProviders() []WorkerProvider         { return h.workers }
func (h testHooks) UsageEnrichers() []UsageEnricher           { return h.enrichers }

type testRouteProvider struct{ id string }

func (p testRouteProvider) ID() string                      { return p.id }
func (testRouteProvider) RegisterAdminRoutes(gin.IRouter)   {}
func (testRouteProvider) RegisterGatewayRoutes(gin.IRouter) {}

type testPermissionProvider struct{ id string }

func (p testPermissionProvider) ID() string              { return p.id }
func (testPermissionProvider) Permissions() []Permission { return nil }

type testSettingProvider struct{ id string }

func (p testSettingProvider) ID() string        { return p.id }
func (testSettingProvider) Settings() []Setting { return nil }

type testWorkerProvider struct{ id string }

func (p testWorkerProvider) ID() string                { return p.id }
func (testWorkerProvider) Start(context.Context) error { return nil }

type testUsageEnricher struct{ id string }

func (p testUsageEnricher) ID() string                                    { return p.id }
func (testUsageEnricher) EnrichUsage(context.Context, *UsageRecord) error { return nil }

func TestNewCustomProviderSetSortsProvidersByID(t *testing.T) {
	set, err := NewCustomProviderSet(testHooks{
		routes:      []RouteProvider{testRouteProvider{id: "z"}, testRouteProvider{id: "a"}},
		permissions: []PermissionProvider{testPermissionProvider{id: "z"}, testPermissionProvider{id: "a"}},
		settings:    []SettingProvider{testSettingProvider{id: "z"}, testSettingProvider{id: "a"}},
		workers:     []WorkerProvider{testWorkerProvider{id: "z"}, testWorkerProvider{id: "a"}},
		enrichers:   []UsageEnricher{testUsageEnricher{id: "z"}, testUsageEnricher{id: "a"}},
	})
	if err != nil {
		t.Fatalf("NewCustomProviderSet() error = %v", err)
	}

	if got := providerIDs(set.Routes()); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("route order = %v, want [a z]", got)
	}
	if got := providerIDs(set.Permissions()); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("permission order = %v, want [a z]", got)
	}
	if got := providerIDs(set.Settings()); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("setting order = %v, want [a z]", got)
	}
	if got := providerIDs(set.Workers()); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("worker order = %v, want [a z]", got)
	}
	if got := providerIDs(set.UsageEnrichers()); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("usage enricher order = %v, want [a z]", got)
	}
}

func TestNewCustomProviderSetRejectsDuplicateIDs(t *testing.T) {
	_, err := NewCustomProviderSet(testHooks{
		permissions: []PermissionProvider{
			testPermissionProvider{id: "prompt-audit"},
			testPermissionProvider{id: "prompt-audit"},
		},
	})
	if !errors.Is(err, ErrDuplicateProviderID) {
		t.Fatalf("error = %v, want ErrDuplicateProviderID", err)
	}
}

func TestNewCustomProviderSetRejectsEmptyID(t *testing.T) {
	_, err := NewCustomProviderSet(testHooks{
		routes: []RouteProvider{testRouteProvider{}},
	})
	if !errors.Is(err, ErrInvalidProvider) {
		t.Fatalf("error = %v, want ErrInvalidProvider", err)
	}
}

func TestNewCustomProviderSetCopiesInputAndOutputSlices(t *testing.T) {
	routes := []RouteProvider{testRouteProvider{id: "route"}}
	hooks := testHooks{routes: routes}
	set, err := NewCustomProviderSet(hooks)
	if err != nil {
		t.Fatalf("NewCustomProviderSet() error = %v", err)
	}

	routes[0] = testRouteProvider{id: "changed"}
	if got := providerIDs(set.Routes()); !reflect.DeepEqual(got, []string{"route"}) {
		t.Fatalf("set changed after input mutation: %v", got)
	}

	output := set.Routes()
	output[0] = testRouteProvider{id: "changed"}
	if got := providerIDs(set.Routes()); !reflect.DeepEqual(got, []string{"route"}) {
		t.Fatalf("set changed after output mutation: %v", got)
	}
}

func providerIDs[T provider](providers []T) []string {
	ids := make([]string, len(providers))
	for i, p := range providers {
		ids[i] = p.ID()
	}
	return ids
}
