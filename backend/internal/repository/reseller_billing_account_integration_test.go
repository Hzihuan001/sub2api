//go:build integration

package repository

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/reseller"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestResellerBillingAccountTransferPreservesOwnership(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	ctx := context.Background()
	db := integrationDB // Disposable integration container; services own their transactions.
	suffix := uuid.NewString()
	var oldUser, newUser, operator, groupID, tenantID, productID, keyID int64
	for _, item := range []struct {
		role string
		id   *int64
	}{{"admin", &oldUser}, {"user", &newUser}, {"operator", &operator}} {
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users (email,password_hash,role,status,balance,concurrency)
		VALUES ($1,'test',$2,'active',0,1) RETURNING id`, item.role+suffix+"@example.com", item.role).Scan(item.id))
	}
	sut := reseller.NewService(db, nil, nil)
	for _, id := range []int64{oldUser, operator} {
		_, err := sut.CreateTenant(ctx, id, "invalid-"+suffix, nil)
		require.ErrorIs(t, err, reseller.ErrInvalidInput)
	}
	// Reproduce the legacy misconfiguration: privileged owner with existing keys.
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO reseller_tenants (user_id,name) VALUES ($1,$2) RETURNING id`, oldUser, "legacy-"+suffix).Scan(&tenantID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups (name,platform,rate_multiplier) VALUES ($1,'openai',1) RETURNING id`, suffix).Scan(&groupID))
	product, err := sut.UpsertProduct(ctx, tenantID, groupID, "openai", "OpenAI", true)
	require.NoError(t, err)
	productID = product.ID
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys (user_id,key,name,group_id,status) VALUES ($1,$2,'legacy',$3,'active') RETURNING id`, oldUser, "sk-rs_"+suffix, groupID).Scan(&keyID))
	_, err = db.ExecContext(ctx, `INSERT INTO reseller_credentials (reseller_id,product_id,api_key_id) VALUES ($1,$2,$3)`, tenantID, productID, keyID)
	require.NoError(t, err)
	_, err = sut.RotateCredential(ctx, tenantID, productID)
	require.ErrorIs(t, err, reseller.ErrInvalidInput)
	_, err = sut.ChangeBillingAccount(ctx, tenantID, operator)
	require.ErrorIs(t, err, reseller.ErrInvalidInput)
	var owner int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT user_id FROM reseller_tenants WHERE id=$1`, tenantID).Scan(&owner))
	require.Equal(t, oldUser, owner, "invalid transfer must not change the billing account")
	_, err = sut.ChangeBillingAccount(ctx, tenantID, newUser)
	require.NoError(t, err)
	tenants, err := sut.ListTenants(ctx)
	require.NoError(t, err)
	var current *reseller.Tenant
	for i := range tenants {
		if tenants[i].ID == tenantID {
			current = &tenants[i]
			break
		}
	}
	require.NotNil(t, current)
	require.Equal(t, newUser, current.UserID)
	require.NotNil(t, current.BillingAccount)
	require.Equal(t, "user"+suffix+"@example.com", current.BillingAccount.Email)
	require.Zero(t, current.BillingAccount.Balance)
	var keyStatus, credentialStatus string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT k.user_id,k.status,c.status FROM api_keys k JOIN reseller_credentials c ON c.api_key_id=k.id WHERE k.id=$1`, keyID).Scan(&owner, &keyStatus, &credentialStatus))
	require.Equal(t, oldUser, owner, "old key and historical billing ownership must never be reassigned")
	require.Equal(t, "inactive", keyStatus)
	require.Equal(t, "revoked", credentialStatus)
	issued, err := sut.RotateCredential(ctx, tenantID, productID)
	require.ErrorIs(t, err, reseller.ErrInvalidInput, "a changed billing account requires reauthorization")
	code, _, err := sut.CreateEnrollment(ctx, tenantID, 0, []int64{productID}, 0)
	require.NoError(t, err)
	exchanged, err := sut.ExchangeEnrollment(ctx, code, uuid.NewString(), "127.0.0.1", tenantID)
	require.NoError(t, err)
	require.Len(t, exchanged.Credentials, 1)
	issued, err = sut.RotateCredential(ctx, tenantID, productID)
	require.NoError(t, err)
	var newKeyID int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id,user_id FROM api_keys WHERE key=$1`, issued.APIKey).Scan(&newKeyID, &owner))
	require.Equal(t, newUser, owner)
	request := func() error {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		c.Request.Header.Set("X-Reseller-Request-ID", uuid.NewString())
		c.Set("api_key", &service.APIKey{ID: newKeyID, Key: issued.APIKey, UserID: newUser, User: &service.User{ID: newUser}})
		return sut.BeginGatewayRequest(c)
	}
	require.ErrorIs(t, request(), reseller.ErrInsufficientBalance)
	_, err = db.ExecContext(ctx, `UPDATE users SET balance=5 WHERE id=$1`, newUser)
	require.NoError(t, err)
	require.NoError(t, request(), "funded reseller may use its own key")
	var oldBalance float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id=$1`, oldUser).Scan(&oldBalance))
	require.Zero(t, oldBalance, "transfer must not move money from the previous owner")
	_, err = db.ExecContext(ctx, `UPDATE api_keys SET user_id=$2 WHERE id=$1`, newKeyID, oldUser)
	require.NoError(t, err)
	require.ErrorIs(t, request(), reseller.ErrForbidden, "mismatched key ownership must be refused even when auth cache still has the reseller")
}
