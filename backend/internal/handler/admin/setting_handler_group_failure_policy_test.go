package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type groupFailurePolicyHandlerRepo struct {
	service.SettingRepository
	values map[string]string
	err    error
	writes int
}

func (r *groupFailurePolicyHandlerRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	if value, ok := r.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *groupFailurePolicyHandlerRepo) Set(_ context.Context, key, value string) error {
	if r.err != nil {
		return r.err
	}
	if r.values == nil {
		r.values = map[string]string{}
	}
	r.values[key] = value
	r.writes++
	return nil
}

type groupFailurePolicyHandlerGroups struct{}

func (groupFailurePolicyHandlerGroups) GetByID(_ context.Context, id int64) (*service.Group, error) {
	if id != 1 && id != 2 {
		return nil, service.ErrGroupNotFound
	}
	return &service.Group{ID: id}, nil
}

func groupFailurePolicyHandlerRouter(repo *groupFailurePolicyHandlerRepo) (*gin.Engine, *service.SettingService) {
	gin.SetMode(gin.TestMode)
	svc := service.NewSettingService(repo, nil)
	svc.SetDefaultSubscriptionGroupReader(groupFailurePolicyHandlerGroups{})
	h := NewSettingHandler(svc, nil, nil, nil, nil, nil, nil)
	router := gin.New()
	group := router.Group("/api/v1/admin/groups")
	group.Use(func(c *gin.Context) {
		if role := c.GetHeader("X-Test-Role"); role != "" {
			c.Set(string(middleware.ContextKeyUserRole), role)
		}
		c.Next()
	}, middleware.AdminRouteAuthorization(svc))
	group.GET("/:id/failure-policy", h.GetGroupFailurePolicy)
	group.PUT("/:id/failure-policy", h.UpdateGroupFailurePolicy)
	return router, svc
}

func groupFailurePolicyHandlerRequest(router *gin.Engine, method, id, role, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1/admin/groups/"+id+"/failure-policy", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Role", role)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGroupFailurePolicyHandlerRoundTripAndGroupIsolation(t *testing.T) {
	repo := &groupFailurePolicyHandlerRepo{}
	router, _ := groupFailurePolicyHandlerRouter(repo)
	const payload = `{"rules":{"overload":{"threshold":3,"window_seconds":300,"cooldown_seconds":600}}}`
	for _, tc := range []struct{ method, id, body, expected string }{
		{http.MethodGet, "1", "", `{"rules":{}}`},
		{http.MethodPut, "1", payload, payload},
		{http.MethodGet, "1", "", payload},
		{http.MethodGet, "2", "", `{"rules":{}}`},
		{http.MethodPut, "1", `{"rules":{}}`, `{"rules":{}}`},
		{http.MethodGet, "1", "", `{"rules":{}}`},
	} {
		rec := groupFailurePolicyHandlerRequest(router, tc.method, tc.id, service.RoleAdmin, tc.body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
		require.JSONEq(t, tc.expected, string(envelope.Data))
	}
	require.Equal(t, 2, repo.writes)
}

func TestGroupFailurePolicyHandlerRejectsInvalidJSONAndRules(t *testing.T) {
	for _, body := range []string{
		"", "null", "{}", `{"rules":null}`, `{"rules":[]}`, `{"rules":{},"enabled":true}`,
		`{"rules":{"other":{"threshold":1,"window_seconds":1,"cooldown_seconds":1}}}`,
		`{"rules":{"overload":{"threshold":1,"window_seconds":1,"cooldown_seconds":1,"enabled":true}}}`,
		`{"rules":{"overload":{"threshold":1.5,"window_seconds":1,"cooldown_seconds":1}}}`,
		`{"rules":{"overload":{"threshold":"1","window_seconds":1,"cooldown_seconds":1}}}`,
		`{"rules":{"overload":{"threshold":null,"window_seconds":1,"cooldown_seconds":1}}}`,
		`{"rules":{"overload":{"threshold":1,"window_seconds":3601,"cooldown_seconds":1}}}`,
		`{"rules":{"overload":{"threshold":1,"window_seconds":1,"cooldown_seconds":7201}}}`,
		`{"rules":{"overload":{"threshold":0,"window_seconds":1,"cooldown_seconds":1}}}`,
		`{"rules":{}} {"rules":{}}`, `{"rules":{}} trailing`,
	} {
		t.Run(body, func(t *testing.T) {
			repo := &groupFailurePolicyHandlerRepo{}
			router, _ := groupFailurePolicyHandlerRouter(repo)
			rec := groupFailurePolicyHandlerRequest(router, http.MethodPut, "1", service.RoleAdmin, body)
			require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			require.Zero(t, repo.writes)
		})
	}
}

func TestGroupFailurePolicyHandlerInvalidGroupAndStorageFailures(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, id := range []string{"0", "-1", "abc", "9223372036854775808"} {
			router, _ := groupFailurePolicyHandlerRouter(&groupFailurePolicyHandlerRepo{})
			require.Equal(t, http.StatusBadRequest, groupFailurePolicyHandlerRequest(router, method, id, service.RoleAdmin, `{"rules":{}}`).Code)
		}
		repo := &groupFailurePolicyHandlerRepo{}
		router, _ := groupFailurePolicyHandlerRouter(repo)
		require.Equal(t, http.StatusNotFound, groupFailurePolicyHandlerRequest(router, method, "99", service.RoleAdmin, `{"rules":{}}`).Code)
		require.Zero(t, repo.writes)
		repo.err = errors.New("database unavailable")
		require.Equal(t, http.StatusInternalServerError, groupFailurePolicyHandlerRequest(router, method, "1", service.RoleAdmin, `{"rules":{}}`).Code)
	}
}

func TestGroupFailurePolicyHandlerUsesGroupReadWritePermissions(t *testing.T) {
	for _, tc := range []struct {
		name, role           string
		read, write          bool
		getStatus, putStatus int
	}{
		{"anonymous", "", false, false, 401, 401},
		{"ordinary user", service.RoleUser, true, true, 403, 403},
		{"operator denied", service.RoleOperator, false, false, 403, 403},
		{"operator read only", service.RoleOperator, true, false, 200, 403},
		{"operator write requires read", service.RoleOperator, false, true, 403, 403},
		{"operator read write", service.RoleOperator, true, true, 200, 200},
		{"admin", service.RoleAdmin, false, false, 200, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &groupFailurePolicyHandlerRepo{}
			router, svc := groupFailurePolicyHandlerRouter(repo)
			policy := authz.FailClosedOperatorPolicy()
			policy.Permissions[string(authz.PermissionGroupsRead)] = tc.read
			policy.Permissions[string(authz.PermissionGroupsWrite)] = tc.write
			_, err := svc.SetOperatorRolePolicy(context.Background(), policy)
			require.NoError(t, err)
			repo.writes = 0
			require.Equal(t, tc.getStatus, groupFailurePolicyHandlerRequest(router, http.MethodGet, "1", tc.role, "").Code)
			require.Equal(t, tc.putStatus, groupFailurePolicyHandlerRequest(router, http.MethodPut, "1", tc.role, `{"rules":{}}`).Code)
			if tc.putStatus != 200 {
				require.Zero(t, repo.writes)
			}
		})
	}
}
