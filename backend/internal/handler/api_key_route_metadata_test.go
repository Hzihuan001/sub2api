package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRouteMetadataRejectsInvalidAndServerManagedWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"name":"key","key_route_scope":"unknown"}`,
		`{"name":"key","key_route_scope":""}`,
		`{"name":"key","key_route_revision":123}`,
		`{"name":"key","key_route_revision":null}`,
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			t.Run(method+body, func(t *testing.T) {
				h := NewAPIKeyHandler(nil) // validation must precede any service mutation
				router := gin.New()
				router.Use(func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7}) })
				router.POST("/keys", h.Create)
				router.PUT("/keys/:id", h.Update)
				path := "/keys"
				if method == http.MethodPut {
					path += "/1"
				}
				req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)
				require.Equal(t, http.StatusBadRequest, rec.Code)
			})
		}
	}
}

func TestAPIKeyRouteMetadataDTOIsExplicitAndReadOnly(t *testing.T) {
	legacy := dto.APIKeyFromService(&service.APIKey{ID: 1})
	require.Equal(t, service.KeyRouteScopeGroupV1, legacy.KeyRouteScope)
	account := dto.APIKeyFromService(&service.APIKey{ID: 2, KeyRouteScope: service.KeyRouteScopeAccountV2, KeyRouteRevision: 77})
	require.Equal(t, service.KeyRouteScopeAccountV2, account.KeyRouteScope)
	require.EqualValues(t, 77, account.KeyRouteRevision)
}
