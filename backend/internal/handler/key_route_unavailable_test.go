package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type unavailableKeyRouteSelector struct {
	next *service.APIKey
	err  error
	seen []int64
}

func (*unavailableKeyRouteSelector) RecordFailure(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, int64) (keyrouting.RouteFailureResult, error) {
	panic("empty candidate pools must not record fault rule outcomes")
}

func (s *unavailableKeyRouteSelector) SelectNext(_ context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, currentGroupID int64) (*service.APIKey, keyrouting.Plan, error) {
	s.seen = append(s.seen, currentGroupID)
	return s.next, keyrouting.Plan{SelectedGroupID: derefTestGroupID(s.next)}, s.err
}

func newUnavailableKeyRouteContext(t *testing.T) (*gin.Context, *unavailableKeyRouteSelector) {
	t.Helper()
	c, primary, backup, _ := newTestKeyRouteContext(t)
	primary.KeyRouteFailurePolicy = &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{}}
	selector := &unavailableKeyRouteSelector{next: backup}
	c.Set(string(servermiddleware.ContextKeyAPIKey), primary)
	c.Set(keyRoutingSelectorContextKey, selector)
	c.Set("custom.key_route_dispatch", true)
	c.Set("custom.key_route_request", service.KeyRoutingRequest{Model: "model", Endpoint: service.CompositeRouteEndpointResponses})
	return c, selector
}

func TestKeyRouteUnavailableSwitchesWithFaultRulesDisabledAndDoesNotCountFaults(t *testing.T) {
	c, selector := newUnavailableKeyRouteContext(t)
	require.True(t, tryKeyRouteSelectionUnavailable(c, fmt.Errorf("scheduler: %w", service.ErrNoAvailableAccounts)))
	require.Equal(t, []int64{1}, selector.seen)
	require.True(t, c.GetBool("custom.key_route_restart"))
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	require.True(t, ok)
	require.EqualValues(t, 2, *key.GroupID)
}

func TestKeyRouteUnavailableRejectsNonAvailabilityErrors(t *testing.T) {
	for _, err := range []error{nil, errors.New("database unavailable"), errors.New("no available accounts"), service.ErrNoAvailableCompactAccounts, context.Canceled} {
		c, selector := newUnavailableKeyRouteContext(t)
		require.False(t, tryKeyRouteSelectionUnavailable(c, err))
		require.Empty(t, selector.seen)
		require.False(t, c.GetBool("custom.key_route_restart"))
	}
}

func TestKeyRouteUnavailableNeverBypassesFailedSelectionOrPermission(t *testing.T) {
	for _, err := range []error{keyrouting.ErrNotAuthorized, keyrouting.ErrNoSchedulableAccounts, errors.New("database unavailable")} {
		c, selector := newUnavailableKeyRouteContext(t)
		selector.err = err
		require.False(t, tryKeyRouteUnavailable(c))
		require.Equal(t, []int64{1}, selector.seen)
		require.False(t, c.GetBool("custom.key_route_restart"))
		key, ok := servermiddleware.GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.EqualValues(t, 1, *key.GroupID)
	}
}

func TestKeyRouteUnavailableRequiresActiveUnwrittenMultiGroupRequest(t *testing.T) {
	for _, scenario := range []string{"no-dispatch", "inactive", "single-group", "canceled", "written-headers", "partial-output", "missing-key", "missing-request"} {
		t.Run(scenario, func(t *testing.T) {
			c, selector := newUnavailableKeyRouteContext(t)
			switch scenario {
			case "no-dispatch":
				c.Set("custom.key_route_dispatch", false)
			case "inactive":
				c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.KeyRoutingActive, false))
			case "single-group":
				key, _ := servermiddleware.GetAPIKeyFromContext(c)
				key.BackupGroupIDs = nil
			case "canceled":
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			case "written-headers":
				c.Writer.WriteHeaderNow()
			case "partial-output":
				_, err := c.Writer.Write([]byte("partial"))
				require.NoError(t, err)
			case "missing-key":
				c.Set(string(servermiddleware.ContextKeyAPIKey), nil)
			case "missing-request":
				c.Set("custom.key_route_request", nil)
			}
			require.False(t, tryKeyRouteUnavailable(c))
			require.Empty(t, selector.seen)
		})
	}
}

func TestKeyRouteUnavailableDispatchRestartsOnlyAfterHandlerReturns(t *testing.T) {
	c, selector := newUnavailableKeyRouteContext(t)
	var sequence []string
	KeyRouteDispatch(func(c *gin.Context) {
		sequence = append(sequence, "start")
		defer func() { sequence = append(sequence, "cleanup") }()
		key, _ := servermiddleware.GetAPIKeyFromContext(c)
		if *key.GroupID == 1 {
			require.True(t, tryKeyRouteUnavailable(c))
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})(c)
	require.Equal(t, []string{"start", "cleanup", "start", "cleanup"}, sequence)
	require.Equal(t, []int64{1}, selector.seen)
	require.Equal(t, http.StatusOK, c.Writer.Status())
}
