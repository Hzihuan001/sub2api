package reseller

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestResellerAdmissionUsesBillingAccount(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_SERVER_ENABLED", "true")
	for _, tc := range []struct {
		name                  string
		keyOwner, cachedOwner int64
		balance               float64
		want                  error
	}{
		{"funded reseller", 12, 12, 1, nil},
		{"empty reseller", 12, 12, 0, ErrInsufficientBalance},
		{"overdrawn reseller", 12, 12, -1, ErrInsufficientBalance},
		{"key owned by administrator", 1, 1, 100, ErrForbidden},
		{"stale cached user", 12, 1, 100, ErrForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery("SELECT rc.reseller_id").WithArgs(int64(7)).WillReturnRows(
				sqlmock.NewRows([]string{"reseller_id", "product_id", "group_id", "catalog_version", "cost_rate_multiplier", "allowed_cidrs", "user_id", "available_balance"}).
					AddRow(2, 3, 4, 5, 0.35, []byte(`[]`), 12, tc.balance))
			if tc.want == nil {
				mock.ExpectExec("INSERT INTO reseller_request_reservations").WillReturnResult(sqlmock.NewResult(1, 1))
			}
			router := gin.New()
			var reached bool
			router.Use(func(c *gin.Context) {
				c.Set("api_key", &service.APIKey{ID: 7, Key: apiKeyPrefix + "test", UserID: tc.keyOwner, User: &service.User{ID: tc.cachedOwner}})
			})
			router.Use(NewHandler(&Service{db: db}).GatewayMiddleware())
			router.POST("/v1/messages", func(c *gin.Context) { reached = true; c.Status(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			req.Header.Set("X-Reseller-Request-ID", uuid.NewString())
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if tc.want != nil {
				require.Equal(t, http.StatusForbidden, response.Code)
				require.Contains(t, response.Body.String(), tc.want.Error())
				require.False(t, reached, "rejected request must never reach upstream or billing")
			} else {
				require.True(t, reached)
				require.Equal(t, http.StatusNoContent, response.Code)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestResellerCredentialBelongsToTenantUser(t *testing.T) {
	for _, eligible := range []bool{true, false} {
		t.Run(map[bool]string{true: "ordinary user", false: "privileged or disabled user"}[eligible], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectBegin()
			tx, err := db.BeginTx(context.Background(), nil)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT EXISTS").WithArgs(int64(12), int64(4)).WillReturnRows(sqlmock.NewRows([]string{"eligible"}).AddRow(eligible))
			if eligible {
				mock.ExpectExec("UPDATE reseller_credentials").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery("INSERT INTO api_keys").WithArgs(int64(12), sqlmock.AnyArg(), "reseller:L1:openai", int64(4)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(90))
				mock.ExpectExec("INSERT INTO reseller_credentials").WithArgs(int64(2), int64(3), int64(90)).WillReturnResult(sqlmock.NewResult(1, 1))
			}
			mock.ExpectRollback()
			s := NewService(db, nil, nil)
			_, err = s.rotateCredentialTx(context.Background(), tx, Tenant{ID: 2, UserID: 12, Name: "L1"}, Product{ID: 3, MoshuGroupID: 4, ProductCode: "openai"}, time.Minute)
			if eligible {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidInput)
			}
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCreateTenantRejectsInvalidBillingAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("INSERT INTO reseller_tenants").WithArgs(int64(1), "L1", sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
	_, err = NewService(db, nil, nil).CreateTenant(context.Background(), 1, "L1", nil)
	require.True(t, errors.Is(err, ErrInvalidInput))
	require.NoError(t, mock.ExpectationsWereMet())
}
