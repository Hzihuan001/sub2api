package moshureseller

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectStationReplacement(mock sqlmock.Sqlmock, fail bool) {
	mock.ExpectQuery("SELECT id,remote_product_id,moshu_group_id,platform").WillReturnRows(sqlmock.NewRows([]string{"id", "remote", "group", "platform"}).AddRow(3, 3, 7, "openai"))
	mock.ExpectExec("UPDATE moshu_products SET authorized=FALSE,product_code=").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("WITH changed AS").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_products SET credential_ciphertext=NULL").WillReturnResult(sqlmock.NewResult(0, 1))
	update := mock.ExpectExec("UPDATE moshu_products SET remote_product_id=").WithArgs(int64(3), int64(9))
	if fail {
		update.WillReturnError(errors.New("write failed"))
		return
	}
	update.WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_settlement_cursors SET last_remote_id=0").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_reseller_connections SET last_settlement_sync_at=NULL").WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestReplacementEnrollmentPreservesStationAndRemapsChannel(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[fail], func(t *testing.T) {
			t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "preserve_station", body["reauthorization_mode"])
				require.Equal(t, "refresh", body["previous_refresh_token"])
				require.EqualValues(t, 1, body["expected_reseller_id"])
				_, _ = w.Write([]byte(`{"code":0,"data":{"reauthorization_mode":"preserve_station","tenant":{"id":2,"name":"New","protocol_version":"v1"},"access_token":"access","refresh_token":"new-refresh","expires_in":900,"catalog":{"protocol_version":"v1","reseller_id":2,"catalog_version":2,"products":[{"id":9,"moshu_group_id":7,"product_code":"gpt","display_name":"GPT","platform":"openai","enabled":true,"effective_at":"2026-09-07T00:00:00Z"}]},"credentials":[{"product_id":9,"api_key":"new-billing-key"}]}}`))
			}))
			defer server.Close()
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectQuery("SELECT base_url").WillReturnRows(connectionRows(server.URL))
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO moshu_reseller_connections").WillReturnResult(sqlmock.NewResult(1, 1))
			expectStationReplacement(mock, fail)
			if fail {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("INSERT INTO moshu_products").WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE moshu_products SET authorized=FALSE").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("WITH changed AS").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("UPDATE moshu_products SET credential_ciphertext=NULL").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("WITH changed AS").WithArgs(int64(9), "new-billing-key").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
				mock.ExpectQuery("SELECT base_url").WillReturnRows(connectionRows(server.URL))
				group, account := int64(5), int64(2)
				mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(true, 0.5, &group, &account))
			}
			result, err := NewService(db, testEncryptor{}, nil).Enroll(context.Background(), server.URL, "new-code")
			if fail {
				require.ErrorContains(t, err, "write failed")
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 5, *result.Products[0].LocalGroupID)
				require.EqualValues(t, 2, *result.Products[0].LocalAccountID)
				require.Equal(t, 0.5, *result.Products[0].SalesRateMultiplier)
			}
			// Any writes outside the explicit upstream tables fail SQL expectations.
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReplacementDoesNotMatchUnrelatedGroupByName(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectBegin()
	tx, err := db.Begin()
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id,remote_product_id,moshu_group_id,platform").WillReturnRows(sqlmock.NewRows([]string{"id", "remote", "group", "platform"}).AddRow(3, 3, 7, "openai"))
	mock.ExpectExec("UPDATE moshu_products SET authorized=FALSE,product_code=").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("WITH changed AS").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_products SET credential_ciphertext=NULL").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_settlement_cursors").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE moshu_reseller_connections").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()
	require.NoError(t, prepareStationReauthorizationTx(context.Background(), tx, RemoteCatalog{Products: []RemoteProduct{{ID: 9, MoshuGroupID: 8, Platform: "openai", ProductCode: "gpt"}}}))
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}
