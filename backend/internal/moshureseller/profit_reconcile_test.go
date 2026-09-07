package moshureseller

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
