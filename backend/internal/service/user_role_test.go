//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserManagementRolePredicates(t *testing.T) {
	admin := &User{Role: RoleAdmin}
	operator := &User{Role: RoleOperator}
	user := &User{Role: RoleUser}

	require.True(t, admin.IsAdmin())
	require.False(t, operator.IsAdmin(), "operator must not inherit strict admin bypasses")
	require.True(t, operator.IsOperator())
	require.True(t, admin.IsManagement())
	require.True(t, operator.IsManagement())
	require.False(t, user.IsManagement())
}
