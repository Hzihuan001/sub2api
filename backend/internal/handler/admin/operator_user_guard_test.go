//go:build unit

package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOperatorTargetUserWriteGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := newStubAdminService()
	base.users = []service.User{
		{ID: 1, Role: service.RoleUser},
		{ID: 2, Role: service.RoleOperator},
		{ID: 3, Role: service.RoleAdmin},
	}
	handler := NewUserHandler(base, nil, nil, nil, nil, nil, nil)

	for _, test := range []struct {
		id         string
		wantStatus int
	}{
		{id: "1", wantStatus: http.StatusOK},
		{id: "2", wantStatus: http.StatusForbidden},
		{id: "3", wantStatus: http.StatusForbidden},
	} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUserRole), service.RoleOperator)
			c.Next()
		})
		router.PUT("/api/v1/admin/users/:id", handler.OperatorTargetUserWriteGuard(), func(c *gin.Context) { c.Status(http.StatusOK) })
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/"+test.id, nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, test.wantStatus, recorder.Code)
	}
}

func TestOperatorBatchWithPrivilegedUserIsRejectedBeforeWrite(t *testing.T) {
	base := newStubAdminService()
	base.users = []service.User{{ID: 1, Role: service.RoleUser}, {ID: 2, Role: service.RoleAdmin}}
	serviceStub := &batchLimitsAdminServiceStub{stubAdminService: base}
	handler := NewUserHandler(serviceStub, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleOperator)
		c.Next()
	})
	router.POST("/api/v1/admin/users/batch-limits", handler.BatchUpdateLimits)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/batch-limits", bytes.NewBufferString(`{"user_ids":[1,2],"rpm_limit":10}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Empty(t, serviceStub.calls)
}

func TestOperatorAPIKeyWriteGuardProtectsPrivilegedOwners(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := newStubAdminService()
	base.users = []service.User{
		{ID: 1, Role: service.RoleUser},
		{ID: 2, Role: service.RoleOperator},
		{ID: 3, Role: service.RoleAdmin},
	}
	base.apiKeys = []service.APIKey{
		{ID: 10, UserID: 1},
		{ID: 20, UserID: 2},
		{ID: 30, UserID: 3},
	}
	handler := NewAdminAPIKeyHandler(base)

	for _, test := range []struct {
		keyID      string
		wantStatus int
	}{
		{keyID: "10", wantStatus: http.StatusOK},
		{keyID: "20", wantStatus: http.StatusForbidden},
		{keyID: "30", wantStatus: http.StatusForbidden},
	} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyUserRole), service.RoleOperator)
			c.Next()
		})
		router.PUT("/api/v1/admin/api-keys/:id", handler.OperatorTargetUserWriteGuard(), func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/"+test.keyID, nil)
		router.ServeHTTP(recorder, request)
		require.Equal(t, test.wantStatus, recorder.Code)
	}
}

func TestUsageFilterOptionsOnlyExposeNarrowLabels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	base := newStubAdminService()
	base.accounts[0].Credentials = map[string]any{"api_key": "must-not-leak"}
	base.accounts[0].Extra = map[string]any{"secret": "must-not-leak"}
	handler := NewUsageHandler(nil, nil, base, nil)
	router := gin.New()
	router.GET("/api/v1/admin/usage/filter-options", handler.GetFilterOptions)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/usage/filter-options?account_search=acct", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.Contains(t, body, `"groups":[{"id":2,"name":"group"}]`)
	require.Contains(t, body, `"accounts":[{"id":3,"name":"account"}]`)
	require.False(t, strings.Contains(body, "must-not-leak"))
	require.Equal(t, "acct", base.lastListAccounts.search)
}
