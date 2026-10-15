package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountGroupRecoveryRepo struct {
	service.AccountRepository
	writes int
}

func (*accountGroupRecoveryRepo) GetByID(context.Context, int64) (*service.Account, error) {
	return &service.Account{ID: 49, Status: service.StatusError}, nil
}

func (r *accountGroupRecoveryRepo) ClearError(context.Context, int64) error {
	r.writes++
	return nil
}

func (r *accountGroupRecoveryRepo) ClearRateLimit(context.Context, int64) error {
	r.writes++
	return nil
}

type accountGroupRecoveryCache struct {
	service.TempUnschedCache
	clears int
}

func (*accountGroupRecoveryCache) RecordAccountGroupFailure(context.Context, int64, int64, string, string, time.Duration, int, time.Duration) (time.Time, bool, error) {
	panic("unexpected failure observation")
}

func (c *accountGroupRecoveryCache) ClearAccountGroupFailures(context.Context, int64) error {
	c.clears++
	return errors.New("redis unavailable")
}

func TestAccountHandlerGroupFailureManualRecoveryRequiresSharedReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &accountGroupRecoveryRepo{}
	cache := &accountGroupRecoveryCache{}
	h := &AccountHandler{rateLimitService: service.NewRateLimitService(repo, nil, nil, nil, cache)}
	router := gin.New()
	router.POST("/api/v1/admin/accounts/:id/recover-state", h.RecoverState)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/49/recover-state", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, 1, cache.clears)
	require.Zero(t, repo.writes, "manual reset failure must precede ClearError and ClearRateLimit")
}
