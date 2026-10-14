package custom

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type contributionPermissionProvider struct {
	id    string
	names []Permission
}

func (p contributionPermissionProvider) ID() string                { return p.id }
func (p contributionPermissionProvider) Permissions() []Permission { return p.names }

type contributionSettingProvider struct {
	id   string
	keys []Setting
}

func (p contributionSettingProvider) ID() string          { return p.id }
func (p contributionSettingProvider) Settings() []Setting { return p.keys }

func TestCustomContributionKeysAreUniqueAcrossAndWithinProviders(t *testing.T) {
	for _, sameProvider := range []bool{false, true} {
		t.Run(map[bool]string{false: "different_providers", true: "same_provider"}[sameProvider], func(t *testing.T) {
			permissions := []PermissionProvider{
				contributionPermissionProvider{"a", []Permission{{Name: "custom.fixture.read"}}},
				contributionPermissionProvider{"b", []Permission{{Name: "custom.fixture.read"}}},
			}
			settings := []SettingProvider{
				contributionSettingProvider{"a", []Setting{{Key: "custom.fixture.enabled", Default: true}}},
				contributionSettingProvider{"b", []Setting{{Key: "custom.fixture.enabled", Default: false}}},
			}
			if sameProvider {
				permissions = []PermissionProvider{contributionPermissionProvider{"a", []Permission{{Name: "custom.fixture.read"}, {Name: "custom.fixture.read"}}}}
				settings = []SettingProvider{contributionSettingProvider{"a", []Setting{{Key: "custom.fixture.enabled"}, {Key: "custom.fixture.enabled"}}}}
			}
			set, err := NewCustomProviderSet(testHooks{permissions: permissions})
			require.Nil(t, set)
			require.ErrorIs(t, err, ErrDuplicatePermission)
			require.Contains(t, err.Error(), "custom.fixture.read")
			set, err = NewCustomProviderSet(testHooks{settings: settings})
			require.Nil(t, set)
			require.ErrorIs(t, err, ErrDuplicateSettingKey)
			require.Contains(t, err.Error(), "custom.fixture.enabled")
		})
	}
}

func TestCustomContributionKeysRejectEmptyOrUntrimmedKeys(t *testing.T) {
	for _, key := range []string{"", " ", " custom.fixture.read", "custom.fixture.read\t"} {
		_, err := NewCustomProviderSet(testHooks{permissions: []PermissionProvider{contributionPermissionProvider{"a", []Permission{{Name: key}}}}})
		require.ErrorIs(t, err, ErrInvalidContribution)
		_, err = NewCustomProviderSet(testHooks{settings: []SettingProvider{contributionSettingProvider{"a", []Setting{{Key: key}}}}})
		require.ErrorIs(t, err, ErrInvalidContribution)
	}
}

func TestCustomProviderIDsRejectDuplicatesInEveryCategory(t *testing.T) {
	for name, hooks := range map[string]testHooks{
		"routes":      {routes: []RouteProvider{testRouteProvider{"same"}, testRouteProvider{"same"}}},
		"permissions": {permissions: []PermissionProvider{testPermissionProvider{"same"}, testPermissionProvider{"same"}}},
		"settings":    {settings: []SettingProvider{testSettingProvider{"same"}, testSettingProvider{"same"}}},
		"workers":     {workers: []WorkerProvider{testWorkerProvider{"same"}, testWorkerProvider{"same"}}},
		"usage":       {enrichers: []UsageEnricher{testUsageEnricher{"same"}, testUsageEnricher{"same"}}},
	} {
		t.Run(name, func(t *testing.T) {
			set, err := NewCustomProviderSet(hooks)
			require.Nil(t, set)
			require.ErrorIs(t, err, ErrDuplicateProviderID)
		})
	}
}

func TestCustomProviderIDsRejectTypedNilAndUntrimmedIDs(t *testing.T) {
	var nilRoute *testRouteProvider
	var nilPermission *testPermissionProvider
	var nilSetting *testSettingProvider
	var nilWorker *testWorkerProvider
	var nilEnricher *testUsageEnricher
	for _, hooks := range []testHooks{
		{routes: []RouteProvider{nilRoute}}, {permissions: []PermissionProvider{nilPermission}},
		{settings: []SettingProvider{nilSetting}}, {workers: []WorkerProvider{nilWorker}},
		{enrichers: []UsageEnricher{nilEnricher}}, {routes: []RouteProvider{nil}},
		{routes: []RouteProvider{testRouteProvider{" "}}}, {routes: []RouteProvider{testRouteProvider{" fixture"}}},
	} {
		require.NotPanics(t, func() {
			_, err := NewCustomProviderSet(hooks)
			require.ErrorIs(t, err, ErrInvalidProvider)
		})
	}
}

func TestCustomContributionValidationKeepsCategoriesIndependent(t *testing.T) {
	set, err := NewCustomProviderSet(testHooks{
		routes:      []RouteProvider{testRouteProvider{"fixture"}},
		permissions: []PermissionProvider{contributionPermissionProvider{"fixture", []Permission{{Name: "custom.fixture.enabled"}}}},
		settings:    []SettingProvider{contributionSettingProvider{"fixture", []Setting{{Key: "custom.fixture.enabled", Default: true}}}},
	})
	require.NoError(t, err)
	require.Len(t, set.Routes(), 1)
	require.Len(t, set.Permissions(), 1)
	require.Len(t, set.Settings(), 1)
}

func TestConfigureRejectsInvalidContributionsWithoutReplacingActiveHost(t *testing.T) {
	previous := DefaultHost().Providers()
	t.Cleanup(func() { ConfigureProviderSet(previous) })
	require.NoError(t, Configure(testHooks{routes: []RouteProvider{testRouteProvider{"active"}}}))
	active := DefaultHost()
	err := Configure(testHooks{settings: []SettingProvider{
		contributionSettingProvider{"a", []Setting{{Key: "same"}}},
		contributionSettingProvider{"b", []Setting{{Key: "same"}}},
	}})
	require.True(t, errors.Is(err, ErrDuplicateSettingKey))
	require.Same(t, active, DefaultHost())
}
