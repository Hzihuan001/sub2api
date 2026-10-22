package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type fullEditAdminStub struct {
	*stubAdminService
	request *service.UpdateAPIKeyRequest
}

func (s *fullEditAdminStub) AdminUpdateAPIKey(_ context.Context, id int64, req service.UpdateAPIKeyRequest) (*service.AdminUpdateAPIKeyGroupIDResult, error) {
	s.request = &req
	return &service.AdminUpdateAPIKeyGroupIDResult{APIKey: &service.APIKey{ID: id, UserID: 2, Name: *req.Name}}, nil
}

func TestAdminAPIKeyFullEditMapsNativeFieldsAndClearsPolicies(t *testing.T) {
	svc := &fullEditAdminStub{stubAdminService: newStubAdminService()}
	router := setupAPIKeyHandler(svc)
	body := `{"name":"edited","group_id":2,"backup_group_ids":[3],"key_route_scope":"account_v2","key_route_failure_policy":null,"key_model_policy":null,"status":"inactive","ip_whitelist":[],"ip_blacklist":["192.0.2.1"],"quota":20,"expires_at":"","reset_quota":true,"rate_limit_5h":1,"rate_limit_1d":2,"rate_limit_7d":3,"reset_rate_limit_usage":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	got := svc.request
	require.NotNil(t, got)
	require.Equal(t, "edited", *got.Name)
	require.EqualValues(t, 2, *got.GroupID)
	require.Equal(t, []int64{3}, *got.BackupGroupIDs)
	require.Equal(t, service.KeyRouteScopeAccountV2, *got.KeyRouteScope)
	require.True(t, got.KeyRouteFailurePolicySet)
	require.Nil(t, got.KeyRouteFailurePolicy)
	require.True(t, got.KeyModelPolicySet)
	require.Nil(t, got.KeyModelPolicy)
	require.Equal(t, "inactive", *got.Status)
	require.Empty(t, *got.IPWhitelist)
	require.Equal(t, []string{"192.0.2.1"}, *got.IPBlacklist)
	require.Equal(t, 20.0, *got.Quota)
	require.True(t, got.ClearExpiration)
	require.True(t, *got.ResetQuota)
	require.Equal(t, 1.0, *got.RateLimit5h)
	require.Equal(t, 2.0, *got.RateLimit1d)
	require.Equal(t, 3.0, *got.RateLimit7d)
	require.True(t, *got.ResetRateLimitUsage)
}

func TestAdminAPIKeyFullEditRejectsMalformedFieldsBeforeMutation(t *testing.T) {
	for _, body := range []string{
		`{"name":"edited","expires_at":"tomorrow"}`,
		`{"name":"edited","key_route_failure_policy":[]}`,
		`{"name":"edited","key_model_policy":"bad"}`,
		`{"name":"edited","status":"expired"}`,
		`{"name":"edited","key_route_revision":1}`,
	} {
		t.Run(body, func(t *testing.T) {
			svc := &fullEditAdminStub{stubAdminService: newStubAdminService()}
			router := setupAPIKeyHandler(svc)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Nil(t, svc.request)
		})
	}
}
