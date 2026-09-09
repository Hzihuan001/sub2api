package reseller

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestCreateTenantsSharingBillingAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	s := NewService(db, nil, nil)
	for i, name := range []string{"API station", "COS station"} {
		mock.ExpectQuery("INSERT INTO reseller_tenants").WithArgs(int64(10), name, sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "name", "status", "protocol", "instance", "cidrs", "created", "updated"}).
				AddRow(i+1, 10, name, "active", "v1", nil, []byte(`[]`), time.Now(), time.Now()))
		tenant, err := s.CreateTenant(context.Background(), 10, name, nil)
		require.NoError(t, err)
		require.Equal(t, int64(i+1), tenant.ID)
		require.Equal(t, int64(10), tenant.UserID)
	}
	mock.ExpectQuery("INSERT INTO reseller_tenants").WillReturnError(&pq.Error{Code: "23505", Constraint: "reseller_tenants_name_active_uidx"})
	_, err = s.CreateTenant(context.Background(), 10, "COS station", nil)
	require.ErrorIs(t, err, ErrInvalidInput)
	require.ErrorContains(t, err, "代理商名称已被使用")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBillingRebindOnlyRevokesTargetTenantCredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id,user_id,name,status").WithArgs(int64(1)).WillReturnRows(tenantRows())
	mock.ExpectQuery("SELECT id FROM users").WithArgs(int64(20)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(20))
	mock.ExpectExec(`UPDATE api_keys SET status='inactive',updated_at=NOW\(\) WHERE id IN \(SELECT api_key_id FROM reseller_credentials WHERE reseller_id=\$1\) AND deleted_at IS NULL`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE reseller_credentials SET status='revoked',updated_at=NOW\(\) WHERE reseller_id=\$1 AND status IN`).WithArgs(int64(1)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`UPDATE reseller_tenants SET user_id=\$2,updated_at=NOW\(\) WHERE id=\$1`).WithArgs(int64(1), int64(20)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	tenant, err := NewService(db, nil, nil).ChangeBillingAccount(context.Background(), 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(20), tenant.UserID)
	require.NoError(t, mock.ExpectationsWereMet())
}
