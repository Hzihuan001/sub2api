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

func TestBuiltInFeatureManifestsAreStableAndValid(t *testing.T) {
	manifests := BuiltInFeatureManifests()
	if err := ValidateFeatureManifests(manifests); err != nil {
		t.Fatalf("ValidateFeatureManifests() error = %v", err)
	}
	want := []FeatureID{
		FeatureBranding,
		FeatureImageStudio,
		FeatureOperator,
		FeaturePromptAudit,
		FeatureUsageExtras,
	}
	got := make([]FeatureID, len(manifests))
	for i, manifest := range manifests {
		got[i] = manifest.ID
		if manifest.SettingsNamespace != SettingNamespace(manifest.ID) {
			t.Errorf("feature %q namespace = %q, want %q", manifest.ID, manifest.SettingsNamespace, SettingNamespace(manifest.ID))
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("feature order = %v, want %v", got, want)
	}
}

func TestNamespacedSettingKey(t *testing.T) {
	got, err := NamespacedSettingKey(FeaturePromptAudit, "retention.days")
	if err != nil {
		t.Fatalf("NamespacedSettingKey() error = %v", err)
	}
	if got != "custom.prompt-audit.retention.days" {
		t.Fatalf("NamespacedSettingKey() = %q", got)
	}

	for _, key := range []string{"", ".days", "days.", "days..value", "days/value"} {
		if _, err := NamespacedSettingKey(FeaturePromptAudit, key); err == nil {
			t.Errorf("NamespacedSettingKey(%q) returned nil error", key)
		}
	}
}

func TestValidateFeatureManifestsRejectsDuplicateIdentity(t *testing.T) {
	_, err := NamespacedSettingKey(FeatureID("UpperCase"), "enabled")
	if err == nil {
		t.Fatal("NamespacedSettingKey() accepted invalid feature ID")
	}

	err = ValidateFeatureManifests([]FeatureManifest{
		{ID: FeatureOperator, SettingsNamespace: "custom.operator"},
		{ID: FeatureOperator, SettingsNamespace: "custom.operator-2"},
	})
	if err == nil {
		t.Fatal("ValidateFeatureManifests() accepted duplicate feature ID")
	}

	err = ValidateFeatureManifests([]FeatureManifest{
		{ID: FeatureOperator, SettingsNamespace: "custom.same"},
		{ID: FeatureBranding, SettingsNamespace: "custom.same"},
	})
	if err == nil {
		t.Fatal("ValidateFeatureManifests() accepted duplicate settings namespace")
	}
}

func providerIDs[T provider](providers []T) []string {
	ids := make([]string, len(providers))
	for i, p := range providers {
		ids[i] = p.ID()
	}
	return ids
}
