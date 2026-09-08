package reseller

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

const testInstance = "00000000-0000-0000-0000-000000000001"

func tenantRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "name", "status", "protocol_version", "instance_id", "allowed_cidrs", "created_at", "updated_at"}).
		AddRow(1, 10, "agent", "active", "v1", testInstance, []byte(`[]`), time.Now(), time.Now())
}
func productRows(ids ...int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "reseller_id", "moshu_group_id", "product_code", "display_name", "platform", "enabled", "cost_rate_multiplier", "price_catalog_version", "model_snapshot", "capabilities", "effective_at", "credential_configured", "created_at", "updated_at"})
	for _, id := range ids {
		rows.AddRow(id, 1, id, "product", "Product", "openai", true, 1, 1, []byte(`[]`), []byte(`{}`), time.Now(), true, time.Now(), time.Now())
	}
	return rows
}

func TestIncrementalEnrollmentPreservesGrantedProducts(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, reseller_id, product_ids, expires_at").WillReturnRows(sqlmock.NewRows([]string{"id", "reseller_id", "product_ids", "expires_at"}).AddRow(7, 1, []byte(`[3]`), time.Now().Add(time.Hour)))
	mock.ExpectQuery("SELECT id,user_id,name,status").WillReturnRows(tenantRows())
	mock.ExpectQuery("SELECT id FROM reseller_tenants WHERE instance_id").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT rp.id,rp.reseller_id").WillReturnRows(productRows(3))
	mock.ExpectQuery("SELECT EXISTS").WillReturnRows(sqlmock.NewRows([]string{"ok"}).AddRow(true))
	mock.ExpectExec("UPDATE reseller_credentials").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("INSERT INTO api_keys").WithArgs(int64(10), sqlmock.AnyArg(), "reseller:agent:product", int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(99))
	mock.ExpectExec("INSERT INTO reseller_credentials").WithArgs(int64(1), int64(3), int64(99)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE reseller_tenants SET instance_id").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE reseller_enrollment_codes SET used_at").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO reseller_refresh_tokens").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT rp.id,rp.reseller_id").WillReturnRows(productRows(2, 3))
	mock.ExpectCommit()
	result, err := NewService(db, nil, nil).ExchangeEnrollment(context.Background(), "test-code", testInstance, "127.0.0.1", 1)
	require.NoError(t, err)
	require.Len(t, result.Catalog.Products, 2, "old and new grants must both be returned")
	require.Len(t, result.Credentials, 1, "incremental exchange only rotates the requested product")
	require.EqualValues(t, 3, result.Credentials[0].ProductID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDifferentResellerRejectedBeforeAnyMutation(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, reseller_id, product_ids, expires_at").WillReturnRows(sqlmock.NewRows([]string{"id", "reseller_id", "product_ids", "expires_at"}).AddRow(7, 1, []byte(`[3]`), time.Now().Add(time.Hour)))
	mock.ExpectQuery("SELECT id,user_id,name,status").WillReturnRows(tenantRows())
	mock.ExpectRollback()
	_, err = NewService(db, nil, nil).ExchangeEnrollment(context.Background(), "test-code", testInstance, "127.0.0.1", 2)
	require.ErrorIs(t, err, ErrInvalidInput)
	require.ErrorContains(t, err, "不支持切换代理商")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestInstanceCannotBindToAnotherTenantWithoutExpectedID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id FROM reseller_tenants").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))
	mock.ExpectRollback()
	require.ErrorIs(t, checkInstanceBindingTx(context.Background(), tx, 1, testInstance), ErrInvalidInput)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCatalogHidesUnexchangedOrDeletedProducts(t *testing.T) {
	products := grantedProducts([]Product{{ID: 1, Enabled: true, CredentialConfigured: true}, {ID: 2, Enabled: true}, {ID: 3, Enabled: false, CredentialConfigured: true}})
	require.Len(t, products, 1)
	require.EqualValues(t, 1, products[0].ID)
}

func TestRevocationRollsBackIfKeyDeletionFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id,user_id,name,status").WillReturnRows(tenantRows())
	mock.ExpectExec("UPDATE reseller_products").WithArgs(int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE api_keys SET status='inactive',deleted_at").WithArgs(int64(1), int64(3)).WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()
	require.ErrorContains(t, NewService(db, nil, nil).DeleteProduct(context.Background(), 1, 3), "write failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeletedProductInvalidatesOldCodesAndKeysTogether(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id,user_id,name,status").WillReturnRows(tenantRows())
	mock.ExpectExec("UPDATE reseller_products").WithArgs(int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE api_keys SET status='inactive',deleted_at").WithArgs(int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE reseller_credentials SET status='revoked'").WithArgs(int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("UPDATE reseller_enrollment_codes SET used_at").WithArgs(int64(1), int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, NewService(db, nil, nil).DeleteProduct(context.Background(), 1, 3))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteTenantRetainsHistoryAndIsNotReversibleByStatus(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id,user_id,name,status").WillReturnRows(tenantRows())
	for _, query := range []string{"UPDATE api_keys SET status='inactive',deleted_at", "UPDATE reseller_credentials SET status='revoked'", "UPDATE reseller_refresh_tokens SET revoked_at", "UPDATE reseller_enrollment_codes SET used_at", "UPDATE reseller_products SET enabled=FALSE", "UPDATE reseller_tenants SET status='disabled',deleted_at"} {
		mock.ExpectExec(query).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	svc := NewService(db, nil, nil)
	require.NoError(t, svc.DeleteTenant(context.Background(), 1))
	mock.ExpectQuery("UPDATE reseller_tenants SET status=.*WHERE id=\\$1 AND deleted_at IS NULL").WillReturnError(sql.ErrNoRows)
	_, err = svc.UpdateTenant(context.Background(), 1, "active", nil)
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
