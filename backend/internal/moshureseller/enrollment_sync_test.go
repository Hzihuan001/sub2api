package moshureseller

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

type testEncryptor struct{}

func (testEncryptor) Encrypt(value string) (string, error) { return "cipher:" + value, nil }
func (testEncryptor) Decrypt(value string) (string, error) { return value, nil }

func TestEnrollmentSynchronizesOperationalKeyBeforeCommit(t *testing.T) {
	for _, failSync := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[failSync], func(t *testing.T) {
			t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"data":{"tenant":{"id":1,"name":"L1","protocol_version":"v1"},"access_token":"access","refresh_token":"refresh","expires_in":900,"catalog":{"protocol_version":"v1","catalog_version":1,"products":[{"id":3,"moshu_group_id":7,"product_code":"gpt","display_name":"GPT","platform":"openai","enabled":true,"cost_rate_multiplier":1,"price_catalog_version":1,"models":[],"capabilities":{},"effective_at":"2026-09-07T00:00:00Z"}]},"credentials":[{"product_id":3,"api_key":"replacement-key"}]}}`))
			}))
			defer server.Close()
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery("SELECT base_url").WillReturnError(sql.ErrNoRows)
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO moshu_reseller_connections").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO moshu_products").WillReturnResult(sqlmock.NewResult(1, 1))
			sync := mock.ExpectExec("WITH changed AS").WithArgs(int64(3), "replacement-key")
			if failSync {
				sync.WillReturnError(errors.New("outbox unavailable"))
				mock.ExpectRollback()
			} else {
				sync.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
				mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(false, 1, nil, nil))
				mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(pricingRows(false, 1, nil, nil))
				mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(server.URL, "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
				mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(false, 1, nil, nil))
				mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(server.URL, "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
				mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(false, 1, nil, nil))
			}
			var admin *testAccountAdmin
			if !failSync {
				admin = &testAccountAdmin{t: t, expectedBaseURL: server.URL}
			}
			_, err = NewService(db, testEncryptor{}, admin).Enroll(context.Background(), server.URL, "once")
			if failSync {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, admin.created)
				require.Equal(t, 1, admin.unschedulable)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUnchangedCatalogRetriesLocalRevocation(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotModified) }))
	defer server.Close()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(server.URL, "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
	mock.ExpectQuery("INSERT INTO moshu_catalog_sync_runs").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT local_group_id,local_account_id,platform").WillReturnError(errors.New("local retry failed"))
	mock.ExpectExec("UPDATE moshu_reseller_connections SET status='error'").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_catalog_sync_runs SET status='failed'").WillReturnResult(sqlmock.NewResult(0, 1))
	_, err = NewService(db, testEncryptor{}, nil).SyncCatalog(context.Background())
	require.ErrorContains(t, err, "local retry failed")
	require.NoError(t, mock.ExpectationsWereMet())
}
