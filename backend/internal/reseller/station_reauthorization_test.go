package reseller

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectPreviousBindingRelease(mock sqlmock.Sqlmock, previousID int64) {
	mock.ExpectQuery("SELECT instance_id::text,deleted_at IS NOT NULL").WithArgs(previousID).WillReturnRows(sqlmock.NewRows([]string{"instance", "deleted"}).AddRow(testInstance, true))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(previousID, testInstance, sqlmock.AnyArg(), true).WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
	for _, query := range []string{"UPDATE api_keys SET status='inactive'", "UPDATE reseller_credentials SET status='revoked'", "UPDATE reseller_refresh_tokens SET revoked_at", "UPDATE reseller_enrollment_codes SET used_at", "UPDATE reseller_tenants SET instance_id=NULL"} {
		mock.ExpectExec(query).WithArgs(previousID).WillReturnResult(sqlmock.NewResult(0, 1))
	}
}

func TestPreviousStationProofRequiredBeforeReleasingBinding(t *testing.T) {
	for _, tc := range []struct {
		name, instance, token string
		valid, deleted        bool
	}{
		{"missing proof", testInstance, "", false, true},
		{"wrong proof", testInstance, "wrong", false, true},
		{"different instance", "00000000-0000-0000-0000-000000000002", "secret", true, true},
		{"deleted source recovery", testInstance, "secret", true, true},
		{"active source replacement", testInstance, "secret", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			tx, err := db.Begin()
			require.NoError(t, err)
			allowed := tc.token != "" && tc.instance == testInstance && tc.valid
			if tc.token != "" {
				mock.ExpectQuery("SELECT instance_id::text,deleted_at IS NOT NULL").WithArgs(int64(2)).WillReturnRows(sqlmock.NewRows([]string{"instance", "deleted"}).AddRow(tc.instance, tc.deleted))
				if tc.instance == testInstance {
					mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(2), testInstance, sqlmock.AnyArg(), tc.deleted).WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(tc.valid))
				}
			}
			if allowed {
				for _, query := range []string{"UPDATE api_keys SET status='inactive'", "UPDATE reseller_credentials SET status='revoked'", "UPDATE reseller_refresh_tokens SET revoked_at", "UPDATE reseller_enrollment_codes SET used_at", "UPDATE reseller_tenants SET instance_id=NULL"} {
					mock.ExpectExec(query).WithArgs(int64(2)).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			mock.ExpectRollback()
			_, err = NewService(db, nil, nil).releasePreviousBindingTx(context.Background(), tx, 2, testInstance, tc.token)
			if allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
