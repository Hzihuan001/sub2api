package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routeMetadataAdminStub struct {
	*stubAdminService
	scope *string
}

func (s *routeMetadataAdminStub) AdminUpdateAPIKeyRouting(_ context.Context, id int64, _ *int64, _ *[]int64, scope *string) (*service.AdminUpdateAPIKeyGroupIDResult, error) {
	s.scope = scope
	return &service.AdminUpdateAPIKeyGroupIDResult{APIKey: &service.APIKey{ID: id, KeyRouteScope: *scope, KeyRouteRevision: 81}}, nil
}

func TestAdminAPIKeyRouteMetadataExplicitScopeAndRevisionValidation(t *testing.T) {
	for _, body := range []string{`{"key_route_scope":"unknown"}`, `{"key_route_revision":123}`, `{"key_route_revision":null}`} {
		svc := &routeMetadataAdminStub{stubAdminService: newStubAdminService()}
		router := setupAPIKeyHandler(svc)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.Nil(t, svc.scope)
	}
	svc := &routeMetadataAdminStub{stubAdminService: newStubAdminService()}
	router := setupAPIKeyHandler(svc)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/api-keys/10", bytes.NewBufferString(`{"key_route_scope":"account_v2"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, svc.scope)
	require.Equal(t, service.KeyRouteScopeAccountV2, *svc.scope)
	require.Contains(t, rec.Body.String(), `"key_route_revision":81`)

	// Admin creation binds directly to the service DTO, so it must reject a
	// forged generation before trying its optional creation mutator too.
	router = gin.New()
	h := NewAdminAPIKeyHandler(svc)
	router.POST("/users/:id/keys", h.CreateForUser)
	req = httptest.NewRequest(http.MethodPost, "/users/7/keys", bytes.NewBufferString(`{"name":"key","key_route_revision":7}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
