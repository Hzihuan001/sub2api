package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func runRoleGuard(t *testing.T, role string, guard gin.HandlerFunc) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUserRole), role)
		c.Next()
	})
	r.GET("/", guard, func(c *gin.Context) { c.Status(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Code
}

func TestAdminOnlyAcceptsBothAdministrativeRoles(t *testing.T) {
	require.Equal(t, http.StatusNoContent, runRoleGuard(t, service.RoleAdmin, AdminOnly()))
	require.Equal(t, http.StatusNoContent, runRoleGuard(t, service.RoleManager, AdminOnly()))
	require.Equal(t, http.StatusForbidden, runRoleGuard(t, service.RoleUser, AdminOnly()))
}

func TestSuperAdminOnlyRejectsManager(t *testing.T) {
	require.Equal(t, http.StatusNoContent, runRoleGuard(t, service.RoleAdmin, SuperAdminOnly()))
	require.Equal(t, http.StatusForbidden, runRoleGuard(t, service.RoleManager, SuperAdminOnly()))
}
