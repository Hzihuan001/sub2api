//go:build unit

package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func runFinancialFilterRequest(t *testing.T, role, route string, payload gin.H) map[string]any {
	t.Helper()
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyUserRole), role)
		c.Next()
	})
	router.Use(OperatorFinancialResponseFilter(nil))
	router.GET(route, func(c *gin.Context) { c.JSON(http.StatusOK, payload) })

	recorder := httptest.NewRecorder()
	requestPath := strings.ReplaceAll(route, ":id", "1")
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, requestPath, nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &decoded))
	return decoded
}

func TestOperatorFinancialResponseFilterRedactsNestedFinancialFields(t *testing.T) {
	decoded := runFinancialFilterRequest(t, service.RoleOperator, "/api/v1/admin/usage", gin.H{
		"data": gin.H{
			"total_tokens":       123,
			"actual_cost":        2.5,
			"total_cost":         3.5,
			"account_stats_cost": 1.5,
			"profit_margin":      0.4,
		},
	})
	data := decoded["data"].(map[string]any)
	require.Equal(t, float64(123), data["total_tokens"])
	require.NotContains(t, data, "actual_cost")
	require.NotContains(t, data, "total_cost")
	require.NotContains(t, data, "account_stats_cost")
	require.NotContains(t, data, "profit_margin")
}

func TestOperatorFinancialResponseFilterRedactsBalanceHistoryValue(t *testing.T) {
	decoded := runFinancialFilterRequest(t, service.RoleOperator, "/api/v1/admin/users/:id/balance-history", gin.H{
		"data": gin.H{"items": []gin.H{{"id": 1, "value": 20}}, "total_recharged": 20},
	})
	data := decoded["data"].(map[string]any)
	require.NotContains(t, data, "total_recharged")
	item := data["items"].([]any)[0].(map[string]any)
	require.NotContains(t, item, "value")
}

func TestAdminFinancialResponseIsUnchanged(t *testing.T) {
	decoded := runFinancialFilterRequest(t, service.RoleAdmin, "/api/v1/admin/usage", gin.H{
		"data": gin.H{"actual_cost": 2.5},
	})
	data := decoded["data"].(map[string]any)
	require.Equal(t, 2.5, data["actual_cost"])
}
