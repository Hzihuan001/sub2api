package moshureseller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func connectionRows(base string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(base, "00000000-0000-0000-0000-000000000001", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil)
}

func TestSameTenantReauthorizationRefreshesKeysWithoutResettingLocalData(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ExpectedResellerID int64 `json:"expected_reseller_id"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.EqualValues(t, 1, req.ExpectedResellerID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"tenant":{"id":1,"name":"L1","protocol_version":"v1"},"access_token":"access","refresh_token":"refresh","expires_in":900,"catalog":{"protocol_version":"v1","reseller_id":1,"catalog_version":2,"products":[{"id":3,"moshu_group_id":7,"product_code":"gpt","display_name":"GPT","platform":"openai","enabled":true,"effective_at":"2026-09-07T00:00:00Z"}]},"credentials":[{"product_id":3,"api_key":"replacement-key"}]}}`))
	}))
	defer server.Close()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT base_url").WillReturnRows(connectionRows(server.URL))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO moshu_reseller_connections").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO moshu_products").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE moshu_products SET authorized=FALSE").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("WITH changed AS").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("UPDATE moshu_products SET credential_ciphertext=NULL").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("WITH changed AS").WithArgs(int64(3), "replacement-key").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT base_url").WillReturnRows(connectionRows(server.URL))
	group, account := int64(5), int64(2)
	mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(true, 0.5, &group, &account))
	result, err := NewService(db, testEncryptor{}, nil).Enroll(context.Background(), server.URL, "code")
	require.NoError(t, err)
	require.True(t, result.Connected)
	require.Len(t, result.Products, 1)
	require.True(t, result.Products[0].Selected)
	require.Equal(t, group, *result.Products[0].LocalGroupID)
	require.Equal(t, account, *result.Products[0].LocalAccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDifferentTenantResponseNeverCommitsLocally(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"tenant":{"id":2,"protocol_version":"v1"},"catalog":{"protocol_version":"v1"}}}`))
	}))
	defer server.Close()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT base_url").WillReturnRows(connectionRows(server.URL))
	_, err = NewService(db, testEncryptor{}, nil).Enroll(context.Background(), server.URL, "code")
	require.ErrorIs(t, err, ErrInvalidInput)
	require.ErrorContains(t, err, "主站未确认")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamEnrollmentErrorsDoNotLogLocalAdminOut(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   string
		local  int
	}{
		{401, `{"message":"enrollment code is invalid or expired"}`, "enrollment code is invalid or expired", 400},
		{400, `{"message":"不支持切换代理商"}`, "不支持切换代理商", 400},
		{500, `{"message":"private SQL details"}`, "HTTP 500", 502},
	} {
		t.Run(tc.want, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := newProtocolClient().exchange(context.Background(), server.URL, "secret-code", "instance")
			require.ErrorContains(t, err, tc.want)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			NewHandler(nil).writeError(c, err)
			require.Equal(t, tc.local, w.Code)
			require.NotContains(t, w.Body.String(), "secret-code")
			require.NotContains(t, w.Body.String(), "private SQL details")
		})
	}
}
