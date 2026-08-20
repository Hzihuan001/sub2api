//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminRouteAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		role       string
		method     string
		path       string
		route      string
		wantStatus int
	}{
		{name: "operator allowed", role: service.RoleOperator, method: http.MethodGet, path: "/api/v1/admin/dashboard/stats", route: "/api/v1/admin/dashboard/stats", wantStatus: http.StatusOK},
		{name: "operator websocket allowed", role: service.RoleOperator, method: http.MethodGet, path: "/api/v1/admin/ops/ws/qps", route: "/api/v1/admin/ops/ws/qps", wantStatus: http.StatusOK},
		{name: "operator settings denied", role: service.RoleOperator, method: http.MethodGet, path: "/api/v1/admin/settings", route: "/api/v1/admin/settings", wantStatus: http.StatusForbidden},
		{name: "operator unknown denied", role: service.RoleOperator, method: http.MethodGet, path: "/api/v1/admin/future", route: "/api/v1/admin/future", wantStatus: http.StatusForbidden},
		{name: "admin unknown allowed", role: service.RoleAdmin, method: http.MethodGet, path: "/api/v1/admin/future", route: "/api/v1/admin/future", wantStatus: http.StatusOK},
		{name: "ordinary user denied", role: service.RoleUser, method: http.MethodGet, path: "/api/v1/admin/dashboard/stats", route: "/api/v1/admin/dashboard/stats", wantStatus: http.StatusForbidden},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyUserRole), test.role)
				c.Next()
			})
			router.Use(AdminRouteAuthorization())
			router.Handle(test.method, test.route, func(c *gin.Context) { c.Status(http.StatusOK) })

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, nil)
			router.ServeHTTP(recorder, request)
			require.Equal(t, test.wantStatus, recorder.Code)
		})
	}
}
