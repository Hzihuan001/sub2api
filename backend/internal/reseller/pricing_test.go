package reseller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResellerPricingHandlersRequireAuthenticatedTenant(t *testing.T) {
	handler := NewHandler(NewService(nil, nil, nil))
	for _, serve := range []func(*gin.Context){handler.Pricing, handler.PricingChanges} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
		serve(ctx)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
	}
}

func TestResellerPricingChangesReturnsCursorAndHonorsCancellation(t *testing.T) {
	handler := NewHandler(NewService(nil, nil, nil))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(resellerIDContextKey, int64(1))
	ctx.Request = httptest.NewRequest(http.MethodGet, "/?after=stale-cursor", nil)
	handler.PricingChanges(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "cursor")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))

	cursor, _ := service.ResellerPricingChangeCursor()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	recorder = httptest.NewRecorder()
	ctx, _ = gin.CreateTestContext(recorder)
	ctx.Set(resellerIDContextKey, int64(1))
	ctx.Request = httptest.NewRequest(http.MethodGet, "/?after="+url.QueryEscape(cursor), nil).WithContext(canceled)
	handler.PricingChanges(ctx)
	require.Empty(t, recorder.Body.String())
}
