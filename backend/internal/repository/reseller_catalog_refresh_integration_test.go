//go:build integration

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/reseller"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResellerCatalogRefreshesGroupChangesWithoutReauthorization(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	ctx := context.Background()
	db := integrationDB
	suffix := uuid.NewString()
	var userID, groupID int64
	require.NoError(t, db.QueryRow(`INSERT INTO users(email,password_hash,role,status) VALUES($1,'test','user','active') RETURNING id`, suffix+"@example.test").Scan(&userID))
	require.NoError(t, db.QueryRow(`INSERT INTO groups(name,platform,rate_multiplier,model_allowlist) VALUES($1,'openai',1,'{"enabled":true,"models":["old-model"]}') RETURNING id`, suffix).Scan(&groupID))
	sut := reseller.NewService(db, nil, nil)
	tenant, err := sut.CreateTenant(ctx, userID, suffix, nil)
	require.NoError(t, err)
	product, err := sut.UpsertProduct(ctx, tenant.ID, groupID, "test", "Sales name", true)
	require.NoError(t, err)
	ungranted, err := sut.Catalog(ctx, tenant.ID)
	require.NoError(t, err)
	require.Empty(t, ungranted.Products, "new products require enrollment before becoming available")
	code, _, err := sut.CreateEnrollment(ctx, tenant.ID, 0, []int64{product.ID}, 0)
	require.NoError(t, err)
	_, err = sut.ExchangeEnrollment(ctx, code, uuid.NewString(), "127.0.0.1", tenant.ID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE groups SET rate_multiplier=2.5,model_allowlist='{"enabled":true,"models":["new-model"]}' WHERE id=$1`, groupID)
	require.NoError(t, err)
	catalog, err := sut.Catalog(ctx, tenant.ID)
	require.NoError(t, err)
	require.Len(t, catalog.Products, 1)
	current := catalog.Products[0]
	require.Equal(t, product.ID, current.ID)
	require.Equal(t, 2.5, current.CostRateMultiplier)
	require.Equal(t, []string{"new-model"}, current.Models)
	require.Equal(t, "Sales name", current.DisplayName)
	require.Equal(t, product.PriceCatalogVersion+1, current.PriceCatalogVersion)
	repeated, err := sut.Catalog(ctx, tenant.ID)
	require.NoError(t, err)
	require.Equal(t, current.PriceCatalogVersion, repeated.Products[0].PriceCatalogVersion)
	_, err = db.Exec(`UPDATE groups SET status='disabled' WHERE id=$1`, groupID)
	require.NoError(t, err)
	catalog, err = sut.Catalog(ctx, tenant.ID)
	require.NoError(t, err)
	require.Empty(t, catalog.Products)
	var enabled bool
	require.NoError(t, db.QueryRow(`SELECT enabled FROM reseller_products WHERE id=$1`, product.ID).Scan(&enabled))
	require.True(t, enabled) // Source availability must not overwrite admin authorization.
}
