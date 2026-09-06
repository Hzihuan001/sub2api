package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMoshuOnlyAccountGuard(t *testing.T) {
	t.Setenv("MOSHU_ONLY_MODE", "true")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "list remains readable", method: http.MethodGet, path: "/api/v1/admin/accounts", wantStatus: http.StatusNoContent},
		{name: "credential export is blocked", method: http.MethodGet, path: "/api/v1/admin/accounts/data", wantStatus: http.StatusForbidden},
		{name: "connectivity test remains available", method: http.MethodPost, path: "/api/v1/admin/accounts/1/test", wantStatus: http.StatusNoContent},
		{name: "create is blocked", method: http.MethodPost, path: "/api/v1/admin/accounts", wantStatus: http.StatusForbidden},
		{name: "update is blocked", method: http.MethodPut, path: "/api/v1/admin/accounts/1", wantStatus: http.StatusForbidden},
		{name: "group assignment is editable", method: http.MethodPut, path: "/api/v1/admin/accounts/1", body: `{"group_ids":[2,3]}`, wantStatus: http.StatusNoContent},
		{name: "mixed channel confirmation may accompany assignment", method: http.MethodPut, path: "/api/v1/admin/accounts/1", body: `{"group_ids":[2],"confirm_mixed_channel_risk":true}`, wantStatus: http.StatusNoContent},
		{name: "other account fields remain blocked", method: http.MethodPut, path: "/api/v1/admin/accounts/1", body: `{"name":"changed"}`, wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := runMoshuOnlyGuard(t, moshuOnlyAccountGuard, tt.method, tt.path, tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", status, tt.wantStatus)
			}
		})
	}
}

func TestMoshuOnlyGroupGuard(t *testing.T) {
	t.Setenv("MOSHU_ONLY_MODE", "true")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "list remains readable", method: http.MethodGet, path: "/api/v1/admin/groups", wantStatus: http.StatusNoContent},
		{name: "retail multiplier is editable", method: http.MethodPut, path: "/api/v1/admin/groups/2", body: `{"rate_multiplier":0.4}`, wantStatus: http.StatusNoContent},
		{name: "group settings are editable", method: http.MethodPut, path: "/api/v1/admin/groups/2", body: `{"name":"retail"}`, wantStatus: http.StatusNoContent},
		{name: "create is available", method: http.MethodPost, path: "/api/v1/admin/groups", body: `{}`, wantStatus: http.StatusNoContent},
		{name: "delete is available", method: http.MethodDelete, path: "/api/v1/admin/groups/2", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := runMoshuOnlyGuard(t, moshuOnlyGroupGuard, tt.method, tt.path, tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d", status, tt.wantStatus)
			}
		})
	}
}

func TestMoshuOnlyAccountGuardRestoresAllowedRequestBody(t *testing.T) {
	t.Setenv("MOSHU_ONLY_MODE", "true")
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(moshuOnlyAccountGuard)
	router.PUT("/api/v1/admin/accounts/:id", func(c *gin.Context) {
		var payload struct {
			GroupIDs []int64 `json:"group_ids"`
		}
		if err := json.NewDecoder(c.Request.Body).Decode(&payload); err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		if len(payload.GroupIDs) != 2 || payload.GroupIDs[0] != 2 || payload.GroupIDs[1] != 3 {
			c.Status(http.StatusUnprocessableEntity)
			return
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/1", strings.NewReader(`{"group_ids":[2,3]}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}
}

func TestMoshuOnlyGuardDisabled(t *testing.T) {
	t.Setenv("MOSHU_ONLY_MODE", "false")
	status := runMoshuOnlyGuard(t, moshuOnlyMutationGuard, http.MethodPost, "/api/v1/admin/accounts", "{}")
	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", status, http.StatusNoContent)
	}
}

func TestMoshuOnlyReadOnlyGuard(t *testing.T) {
	t.Setenv("MOSHU_ONLY_MODE", "true")
	if status := runMoshuOnlyGuard(t, moshuOnlyReadOnlyGuard, http.MethodGet, "/api/v1/admin/system/version", ""); status != http.StatusNoContent {
		t.Fatalf("GET status = %d, want %d", status, http.StatusNoContent)
	}
	if status := runMoshuOnlyGuard(t, moshuOnlyReadOnlyGuard, http.MethodPost, "/api/v1/admin/system/update", "{}"); status != http.StatusForbidden {
		t.Fatalf("POST status = %d, want %d", status, http.StatusForbidden)
	}
}

func TestMoshuOnlyBackupGuard(t *testing.T) {
	t.Setenv("MOSHU_ONLY_MODE", "true")
	if status := runMoshuOnlyGuard(t, moshuOnlyBackupGuard, http.MethodPost, "/api/v1/admin/backups", "{}"); status != http.StatusNoContent {
		t.Fatalf("backup creation status = %d, want %d", status, http.StatusNoContent)
	}
	if status := runMoshuOnlyGuard(t, moshuOnlyBackupGuard, http.MethodPost, "/api/v1/admin/backups/1/restore", "{}"); status != http.StatusForbidden {
		t.Fatalf("restore status = %d, want %d", status, http.StatusForbidden)
	}
}

func runMoshuOnlyGuard(t *testing.T, guard gin.HandlerFunc, method, path, body string) int {
	t.Helper()
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(guard)
	router.Any("/*path", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder.Code
}
