package moshureseller

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"
)

func TestPersistSettlementUpdatesOnlySameOrNewerRevision(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT mp.local_group_id").WithArgs(int64(7), "00000000-0000-0000-0000-000000000001").
		WillReturnRows(sqlmock.NewRows([]string{"group", "sales", "usage", "charge", "estimated"}).AddRow(3, 1.4, 9, 2.5, 1.1))
	mock.ExpectExec(regexp.QuoteMeta("WHERE moshu_request_profit_records.remote_revision <= EXCLUDED.remote_revision")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE usage_logs SET account_stats_cost").WithArgs(1.2, int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
	err = NewService(db, testEncryptor{}, nil).persistSettlement(context.Background(), RemoteSettlement{
		ID: 4, Revision: 3, RequestID: "00000000-0000-0000-0000-000000000001", ProductID: 7,
		ProductCode: "deepseek", ActualCost: 1.2, Status: "completed", RequestSource: "monitor",
	})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSyncSettlementsSurfacesReconciliationFailure(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"items":[],"next_cursor":0}}`))
	}))
	defer server.Close()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(server.URL, "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
	mock.ExpectQuery("SELECT last_remote_id").WillReturnRows(sqlmock.NewRows([]string{"cursor"}).AddRow(10))
	mock.ExpectExec("UPDATE moshu_settlement_cursors").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("WITH matches AS").WillReturnError(errors.New("injected reconciliation failure"))
	mock.ExpectExec("UPDATE moshu_reseller_connections SET status='error'").WillReturnResult(sqlmock.NewResult(0, 1))
	_, err = NewService(db, testEncryptor{}, nil).SyncSettlements(context.Background())
	require.ErrorContains(t, err, "injected reconciliation failure")
	require.NoError(t, mock.ExpectationsWereMet())
}
