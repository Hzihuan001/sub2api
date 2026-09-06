//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestResellerSettlementTriggerUsesReservedPricingSnapshot(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()

	var userID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO users (email,password_hash,role,status,balance,concurrency)
		VALUES ($1,'test','user','active',100,1) RETURNING id`,
		"reseller-trigger-"+uuid.NewString()+"@example.com").Scan(&userID))

	var groupID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO groups (name,platform,rate_multiplier)
		VALUES ($1,'openai',0.35) RETURNING id`, "reseller-trigger-"+uuid.NewString()).Scan(&groupID))

	var accountID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO accounts (name,platform,type,status)
		VALUES ($1,'openai','apikey','active') RETURNING id`, "reseller-trigger-"+uuid.NewString()).Scan(&accountID))

	var apiKeyID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id,key,name,group_id,status)
		VALUES ($1,$2,'reseller-trigger',$3,'active') RETURNING id`,
		userID, "sk-rs_"+uuid.NewString(), groupID).Scan(&apiKeyID))

	var resellerID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO reseller_tenants (user_id,name)
		VALUES ($1,$2) RETURNING id`, userID, "reseller-trigger-"+uuid.NewString()).Scan(&resellerID))

	var productID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO reseller_products
		(reseller_id,moshu_group_id,product_code,display_name,platform,
		 cost_rate_multiplier,price_catalog_version)
		VALUES ($1,$2,$3,'Trigger product','openai',0.35,7) RETURNING id`,
		resellerID, groupID, "trigger-"+uuid.NewString()).Scan(&productID))

	requestID := uuid.New()
	_, err := tx.ExecContext(ctx, `
		INSERT INTO reseller_request_reservations
		(reseller_id,request_id,product_id,api_key_id,moshu_group_id,
		 price_catalog_version,cost_rate_multiplier)
		VALUES ($1,$2,$3,$4,$5,7,0.35)`, resellerID, requestID, productID, apiKeyID, groupID)
	require.NoError(t, err)

	// Catalog changes after request admission must not rewrite in-flight pricing.
	_, err = tx.ExecContext(ctx, `
		UPDATE reseller_products SET cost_rate_multiplier=0.99,price_catalog_version=8
		WHERE id=$1`, productID)
	require.NoError(t, err)

	var settlementsBefore int
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reseller_request_settlements
		WHERE reseller_id=$1 AND request_id=$2`, resellerID, requestID).Scan(&settlementsBefore))
	require.Zero(t, settlementsBefore, "reservation must not allocate a settlement cursor row")

	var usageLogID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		INSERT INTO usage_logs
		(user_id,api_key_id,account_id,group_id,request_id,model,requested_model,
		 upstream_model,input_tokens,output_tokens,cache_creation_tokens,
		 cache_read_tokens,total_cost,actual_cost)
		VALUES ($1,$2,$3,$4,$5,'mapped-model','public-model','mapped-model',
		 100,20,5,10,1.25,0.4375) RETURNING id`,
		userID, apiKeyID, accountID, groupID, "client:"+requestID.String()).Scan(&usageLogID))

	var (
		settledUsageID int64
		status         string
		requestedModel string
		upstreamModel  string
		inputTokens    int64
		actualCost     float64
		costRate       float64
		catalogVersion int64
	)
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT usage_log_id,status,requested_model,upstream_model,input_tokens,
		       actual_cost,cost_rate_multiplier,price_catalog_version
		FROM reseller_request_settlements
		WHERE reseller_id=$1 AND request_id=$2`, resellerID, requestID).Scan(
		&settledUsageID, &status, &requestedModel, &upstreamModel, &inputTokens,
		&actualCost, &costRate, &catalogVersion,
	))
	require.Equal(t, usageLogID, settledUsageID)
	require.Equal(t, "completed", status)
	require.Equal(t, "public-model", requestedModel)
	require.Equal(t, "mapped-model", upstreamModel)
	require.Equal(t, int64(100), inputTokens)
	require.InDelta(t, 0.4375, actualCost, 0.0000001)
	require.InDelta(t, 0.35, costRate, 0.0000001)
	require.Equal(t, int64(7), catalogVersion)
}
