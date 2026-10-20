//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCustomKeyRouteSiteStableNamespace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	repo := newAPIKeyRepositoryWithSQL(integrationEntClient, integrationDB)
	first, err := repo.GetKeyRouteSiteID(ctx)
	require.NoError(t, err)
	parsed, err := uuid.Parse(first)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, parsed)
	// Repeat the full real migration runner, including checksum validation;
	// never rewrite published migration records to make this check pass.
	require.NoError(t, ApplyMigrations(ctx, integrationDB))
	second, err := repo.GetKeyRouteSiteID(ctx)
	require.NoError(t, err)
	require.Equal(t, first, second)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM custom_key_route_site`).Scan(&count))
	require.Equal(t, 1, count)
}
