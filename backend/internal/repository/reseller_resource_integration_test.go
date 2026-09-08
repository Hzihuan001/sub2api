//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestResellerResourceCreationRecoversPartialFailures(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	groups := newGroupRepositoryWithSQL(client, integrationDB)
	accounts := newAccountRepositoryWithSQL(client, integrationDB, nil)
	suffix := time.Now().UnixNano()
	var productID int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO moshu_products(remote_product_id,product_code,display_name,platform,moshu_group_id,cost_rate_multiplier,price_catalog_version,effective_at) VALUES($1,$2,'test','openai',1,1,1,NOW()) RETURNING id`, suffix, fmt.Sprint(suffix)).Scan(&productID))
	resourceCtx := service.WithResellerResourceProduct(ctx, productID)
	group := &service.Group{Name: fmt.Sprintf("reseller-group-%d", suffix), Platform: "openai", Status: "active", SubscriptionType: "standard", RateMultiplier: 0}
	require.NoError(t, groups.Create(resourceCtx, group))
	// Simulate a lost response / account creation failure: next request has no local ID.
	retryGroup := &service.Group{Name: group.Name, Platform: "openai", Status: "active", SubscriptionType: "standard"}
	require.NoError(t, groups.Create(resourceCtx, retryGroup))
	require.Equal(t, group.ID, retryGroup.ID)

	// Inject a failure after the account insert, while linking it to its product.
	functionName := fmt.Sprintf("fail_reseller_link_%d", suffix)
	triggerName := functionName + "_trigger"
	_, err := integrationDB.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.id=%d AND NEW.local_account_id IS NOT NULL THEN RAISE EXCEPTION 'forced reseller link failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER %s BEFORE UPDATE ON moshu_products FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, productID, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON moshu_products; DROP FUNCTION IF EXISTS %s()", triggerName, functionName))
	})
	newAccount := func() *service.Account {
		return &service.Account{Name: fmt.Sprintf("reseller-account-%d", suffix), Platform: "openai", Type: "apikey", Status: "active", Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{}}
	}
	failed := newAccount()
	require.ErrorContains(t, accounts.Create(resourceCtx, failed), "forced reseller link failure")
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM accounts WHERE name=$1`, failed.Name).Scan(&count))
	require.Zero(t, count)
	_, err = integrationDB.Exec(fmt.Sprintf("DROP TRIGGER %s ON moshu_products", triggerName))
	require.NoError(t, err)
	account := newAccount()
	require.NoError(t, accounts.Create(resourceCtx, account))
	// BindGroups fails after creation. The product already retains the account ID.
	require.Error(t, accounts.BindGroups(ctx, account.ID, []int64{int64(9223372036854775807)}))
	retryAccount := newAccount()
	require.NoError(t, accounts.Create(resourceCtx, retryAccount))
	require.Equal(t, account.ID, retryAccount.ID)
	require.NoError(t, accounts.BindGroups(ctx, retryAccount.ID, []int64{group.ID}))
	var storedGroup, storedAccount int64
	require.NoError(t, integrationDB.QueryRow(`SELECT local_group_id,local_account_id FROM moshu_products WHERE id=$1`, productID).Scan(&storedGroup, &storedAccount))
	require.Equal(t, group.ID, storedGroup)
	require.Equal(t, account.ID, storedAccount)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM accounts WHERE name=$1`, account.Name).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRow(`SELECT COUNT(*) FROM groups WHERE name=$1`, group.Name).Scan(&count))
	require.Equal(t, 1, count)
}

func TestResellerAccountCanBeCreatedBeforeSalesGroup(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	accounts := newAccountRepositoryWithSQL(client, integrationDB, nil)
	suffix := time.Now().UnixNano()
	var productID int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO moshu_products(remote_product_id,product_code,display_name,platform,moshu_group_id,cost_rate_multiplier,price_catalog_version,effective_at) VALUES($1,$2,'test-only','openai',1,1,1,NOW()) RETURNING id`, suffix, fmt.Sprint(suffix)).Scan(&productID))
	resourceCtx := service.WithResellerResourceProduct(ctx, productID)
	account := &service.Account{
		Name: fmt.Sprintf("reseller-test-account-%d", suffix), Platform: "openai", Type: "apikey",
		Status: "active", Schedulable: false, Credentials: map[string]any{"api_key": "synthetic"}, Extra: map[string]any{},
	}

	require.NoError(t, accounts.Create(resourceCtx, account))

	var storedGroup sql.NullInt64
	var storedAccount int64
	require.NoError(t, integrationDB.QueryRow(`SELECT local_group_id,local_account_id FROM moshu_products WHERE id=$1`, productID).Scan(&storedGroup, &storedAccount))
	require.False(t, storedGroup.Valid)
	require.Equal(t, account.ID, storedAccount)
}
