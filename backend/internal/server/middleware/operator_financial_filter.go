package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type bufferedOperatorResponseWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func (w *bufferedOperatorResponseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}

func (w *bufferedOperatorResponseWriter) WriteHeaderNow() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
}

func (w *bufferedOperatorResponseWriter) Write(data []byte) (int, error) {
	w.WriteHeaderNow()
	return w.body.Write(data)
}

func (w *bufferedOperatorResponseWriter) WriteString(data string) (int, error) {
	w.WriteHeaderNow()
	return w.body.WriteString(data)
}

func (w *bufferedOperatorResponseWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *bufferedOperatorResponseWriter) Size() int     { return w.body.Len() }
func (w *bufferedOperatorResponseWriter) Written() bool { return w.status != 0 }
func (w *bufferedOperatorResponseWriter) Flush()        { w.WriteHeaderNow() }

var operatorUserChargeFields = map[string]struct{}{
	"actual_cost": {}, "total_actual_cost": {}, "today_actual_cost": {},
	"user_cost": {}, "total_user_cost": {}, "avg_daily_user_cost": {},
	"rate_multiplier": {},
}

var operatorStandardCostFields = map[string]struct{}{
	"cost": {}, "today_cost": {}, "total_cost": {}, "standard_cost": {},
	"total_standard_cost": {}, "input_cost": {}, "output_cost": {},
	"cache_creation_cost": {}, "cache_read_cost": {}, "image_input_cost": {},
	"image_output_cost": {},
}

var operatorUpstreamCostFields = map[string]struct{}{
	"account_cost": {}, "today_account_cost": {}, "total_account_cost": {},
	"account_stats_cost": {}, "avg_daily_cost": {},
	"account_rate_multiplier": {},
}

var operatorProfitFields = map[string]struct{}{
	"profit": {}, "gross_profit": {}, "profit_margin": {}, "gross_margin": {},
	"margin": {}, "profit_rate": {},
}

var operatorUserBalanceFields = map[string]struct{}{
	"balance": {}, "frozen_balance": {}, "before_balance": {}, "after_balance": {},
	"total_recharged": {}, "balance_before": {}, "balance_after": {},
	"balance_notify_threshold": {},
}

func shouldFilterOperatorFinancialResponse(path string) bool {
	return strings.HasPrefix(path, "/api/v1/admin/dashboard/") ||
		strings.HasPrefix(path, "/api/v1/admin/usage") ||
		strings.HasPrefix(path, "/api/v1/admin/users") ||
		strings.HasPrefix(path, "/api/v1/admin/ops/")
}

func mergeDeniedFields(policy authz.OperatorPolicy, path string) map[string]struct{} {
	denied := make(map[string]struct{})
	add := func(fields map[string]struct{}) {
		for key := range fields {
			denied[key] = struct{}{}
		}
	}
	if !policy.Allows(authz.PermissionFinanceUserChargeRead) {
		add(operatorUserChargeFields)
	}
	if !policy.Allows(authz.PermissionFinanceStandardCostRead) {
		add(operatorStandardCostFields)
	}
	if !policy.Allows(authz.PermissionFinanceUpstreamCostRead) {
		add(operatorUpstreamCostFields)
	}
	if !policy.Allows(authz.PermissionFinanceProfitRead) {
		add(operatorProfitFields)
	}
	if strings.HasPrefix(path, "/api/v1/admin/users") && !policy.Allows(authz.PermissionFinanceUserBalanceRead) {
		add(operatorUserBalanceFields)
		if strings.HasSuffix(path, "/balance-history") {
			denied["value"] = struct{}{}
		}
	}
	return denied
}

func redactJSONFields(value any, denied map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if _, blocked := denied[key]; blocked {
				delete(typed, key)
				continue
			}
			redactJSONFields(child, denied)
		}
	case []any:
		for _, child := range typed {
			redactJSONFields(child, denied)
		}
	}
}

// OperatorFinancialResponseFilter strips disabled financial fields from JSON
// before they leave the server. UI hiding is therefore only presentation; this
// middleware is the security boundary for direct API calls.
func OperatorFinancialResponseFilter(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		path := c.FullPath()
		isStreaming := strings.EqualFold(c.GetHeader("Upgrade"), "websocket") ||
			strings.Contains(strings.ToLower(c.GetHeader("Accept")), "text/event-stream")
		if !ok || role != service.RoleOperator || isStreaming || !shouldFilterOperatorFinancialResponse(path) {
			c.Next()
			return
		}

		policy := authz.DefaultOperatorPolicy()
		if settingService != nil {
			policy = settingService.GetOperatorRolePolicyCached(c.Request.Context())
		}
		denied := mergeDeniedFields(policy, path)
		if len(denied) == 0 {
			c.Next()
			return
		}

		original := c.Writer
		buffered := &bufferedOperatorResponseWriter{ResponseWriter: original}
		c.Writer = buffered
		c.Next()
		c.Writer = original

		payload := buffered.body.Bytes()
		var decoded any
		if strings.Contains(strings.ToLower(buffered.Header().Get("Content-Type")), "application/json") && json.Unmarshal(payload, &decoded) == nil {
			redactJSONFields(decoded, denied)
			if encoded, err := json.Marshal(decoded); err == nil {
				payload = encoded
			}
		}

		original.Header().Del("Content-Length")
		original.WriteHeader(buffered.Status())
		_, _ = original.Write(payload)
	}
}
