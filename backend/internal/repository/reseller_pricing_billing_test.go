package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestResellerPricingVersionCommitsWithBilling(t *testing.T) {
	for _, fail := range []bool{false, true} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		mock.ExpectBegin()
		mock.ExpectQuery("INSERT INTO usage_billing_dedup").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectQuery("SELECT request_fingerprint FROM usage_billing_dedup_archive").WillReturnError(sql.ErrNoRows)
		write := mock.ExpectExec("INSERT INTO moshu_request_pricing").WithArgs("request-1", int64(2), "version-1", 0.0, 0.0, 0.15)
		if fail {
			write.WillReturnError(sql.ErrConnDone)
			mock.ExpectRollback()
		} else {
			write.WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		sut := &usageBillingRepository{db: db}
		_, err = sut.Apply(context.Background(), &service.UsageBillingCommand{RequestID: "request-1", APIKeyID: 2, ResellerPricingRevision: "version-1", ResellerCostRate: 0.15})
		if fail {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	}
}
