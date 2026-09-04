//go:build unit

package service

import (
	"context"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type operatorRolePolicyRepoStub struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *operatorRolePolicyRepoStub) Get(context.Context, string) (*Setting, error) {
	return nil, ErrSettingNotFound
}
func (s *operatorRolePolicyRepoStub) GetValue(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}
func (s *operatorRolePolicyRepoStub) Set(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}
func (s *operatorRolePolicyRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (s *operatorRolePolicyRepoStub) SetMultiple(context.Context, map[string]string) error {
	return nil
}
func (s *operatorRolePolicyRepoStub) GetAll(context.Context) (map[string]string, error) {
	return nil, nil
}
func (s *operatorRolePolicyRepoStub) Delete(context.Context, string) error { return nil }

func TestOperatorRolePolicyDefaultsAndRoundTrip(t *testing.T) {
	repo := &operatorRolePolicyRepoStub{values: map[string]string{}}
	svc := NewSettingService(repo, &config.Config{})

	defaults, err := svc.GetOperatorRolePolicy(context.Background())
	require.NoError(t, err)
	require.True(t, defaults.Allows(authz.PermissionUsersRead))
	require.False(t, defaults.Allows(authz.PermissionFinanceUserChargeRead))

	requested := authz.DefaultOperatorPolicy()
	requested.Permissions[string(authz.PermissionDashboardRead)] = false
	requested.Permissions[string(authz.PermissionFinanceUserChargeRead)] = true
	saved, err := svc.SetOperatorRolePolicy(context.Background(), requested)
	require.NoError(t, err)
	require.False(t, saved.Allows(authz.PermissionDashboardRead))
	require.True(t, saved.Allows(authz.PermissionFinanceUserChargeRead))

	cached := svc.GetOperatorRolePolicyCached(context.Background())
	require.False(t, cached.Allows(authz.PermissionDashboardRead))
	require.True(t, cached.Allows(authz.PermissionFinanceUserChargeRead))
}
