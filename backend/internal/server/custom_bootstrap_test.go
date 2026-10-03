package server

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/custom"
	"github.com/stretchr/testify/require"
)

type bootstrapPermissionProvider struct{}

func (bootstrapPermissionProvider) ID() string { return "fixture" }

func (bootstrapPermissionProvider) Permissions() []custom.Permission {
	return []custom.Permission{{Name: "fixture.read"}}
}

func TestInstallBuiltinCustomProvidersAddsOperatorMetadata(t *testing.T) {
	custom.ConfigureProviderSet(nil)
	t.Cleanup(func() { custom.ConfigureProviderSet(nil) })

	require.NoError(t, installBuiltinCustomProviders())
	host := custom.DefaultHost()
	require.NotNil(t, host)
	require.Len(t, host.Providers().Permissions(), 1)
	require.Equal(t, authz.NewOperatorPolicyProvider().ID(), host.Providers().Permissions()[0].ID())
}

func TestInstallBuiltinCustomProvidersPreservesConfiguredProviders(t *testing.T) {
	custom.ConfigureProviderSet(nil)
	t.Cleanup(func() { custom.ConfigureProviderSet(nil) })

	set, err := custom.NewCustomProviderSet(customBootstrapHooks{
		permissions: []custom.PermissionProvider{bootstrapPermissionProvider{}},
	})
	require.NoError(t, err)
	custom.ConfigureProviderSet(set)

	require.NoError(t, installBuiltinCustomProviders())
	providers := custom.DefaultHost().Providers().Permissions()
	require.Len(t, providers, 2)
	require.Equal(t, "fixture", providers[0].ID())
	require.Equal(t, authz.NewOperatorPolicyProvider().ID(), providers[1].ID())
}

func TestInstallBuiltinCustomProvidersIsIdempotent(t *testing.T) {
	custom.ConfigureProviderSet(nil)
	t.Cleanup(func() { custom.ConfigureProviderSet(nil) })

	require.NoError(t, installBuiltinCustomProviders())
	first := custom.DefaultHost().Providers()
	require.NoError(t, installBuiltinCustomProviders())
	second := custom.DefaultHost().Providers()
	require.Len(t, second.Permissions(), 1)
	require.Equal(t, first.Permissions()[0].ID(), second.Permissions()[0].ID())
}
