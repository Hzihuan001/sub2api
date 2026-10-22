package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type testKeyRouteFailoverSelector struct {
	record keyrouting.RouteFailureResult
	next   *service.APIKey
	seen   []int64
}

func (s *testKeyRouteFailoverSelector) RecordFailure(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, int64) (keyrouting.RouteFailureResult, error) {
	return s.record, nil
}

func (s *testKeyRouteFailoverSelector) SelectNext(_ context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, currentGroupID int64) (*service.APIKey, keyrouting.Plan, error) {
	s.seen = append(s.seen, currentGroupID)
	return s.next, keyrouting.Plan{SelectedGroupID: derefTestGroupID(s.next)}, nil
}

func derefTestGroupID(key *service.APIKey) int64 {
	if key == nil || key.GroupID == nil {
		return 0
	}
	return *key.GroupID
}

func newTestKeyRouteContext(t *testing.T) (*gin.Context, *service.APIKey, *service.APIKey, *testKeyRouteFailoverSelector) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request = request.WithContext(context.WithValue(request.Context(), ctxkey.KeyRoutingActive, true))
	c.Request = request
	primaryID, backupID := int64(1), int64(2)
	primary := &service.APIKey{ID: 9, UserID: 7, GroupID: &primaryID, BackupGroupIDs: []int64{backupID}, User: &service.User{ID: 7}, Group: &service.Group{ID: primaryID, Name: "主分组", Platform: service.PlatformOpenAI}}
	backup := &service.APIKey{ID: 9, UserID: 7, GroupID: &backupID, BackupGroupIDs: []int64{backupID}, User: &service.User{ID: 7}, Group: &service.Group{ID: backupID, Name: "备用分组", Platform: service.PlatformDeepseek}}
	selector := &testKeyRouteFailoverSelector{record: keyrouting.RouteFailureResult{Triggered: true}, next: backup}
	c.Set(keyRoutingSelectorContextKey, selector)
	return c, primary, backup, selector
}

func TestKeyRouteSwitchBeforeResponseAndAnnotatesContext(t *testing.T) {
	c, primary, backup, selector := newTestKeyRouteContext(t)
	service.BeginOpsRouteErrorAttempt(c, 0)
	c.Set(service.OpsUpstreamErrorsKey, []*service.OpsUpstreamErrorEvent{{UpstreamStatusCode: http.StatusServiceUnavailable, Message: "overloaded"}})
	got, switched := switchKeyRoute(c, primary, service.KeyRoutingRequest{Model: "model", Endpoint: service.CompositeRouteEndpointResponses}, selector)
	require.True(t, switched)
	require.Same(t, backup, got)
	require.Equal(t, []int64{1}, selector.seen)
	require.Equal(t, "备用分组", c.GetString("custom.key_route_switch_group"))
	events, _ := c.Get(service.OpsUpstreamErrorsKey)
	routeEvents := events.([]*service.OpsUpstreamErrorEvent)
	require.Equal(t, "备用分组", routeEvents[0].RouteSwitchGroup)
	require.Empty(t, service.OpsRouteSwitchLabel(c.Request.Context()), "switch state must not stick to later request errors")
	resolved, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
	require.True(t, ok)
	require.Equal(t, service.PlatformDeepseek, resolved)
}

func TestKeyRouteSwitchDoesNotSwitchAfterResponseBytes(t *testing.T) {
	c, primary, _, selector := newTestKeyRouteContext(t)
	_, err := c.Writer.Write([]byte("partial"))
	require.NoError(t, err)
	got, switched := switchKeyRoute(c, primary, service.KeyRoutingRequest{Model: "model", Endpoint: service.CompositeRouteEndpointResponses}, selector)
	require.False(t, switched)
	require.Same(t, primary, got)
	require.Empty(t, selector.seen)
}

func TestKeyRouteKnownRawUsageBlocksStatusAndSemanticSwitch(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status           int
		statusTriggered  bool
		outcomeTriggered bool
	}{
		{name: "status rule", status: 503, statusTriggered: true},
		{name: "semantic rule", status: 429, outcomeTriggered: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, selector := newDispatchTestContext(t)
			selector.statusTriggered, selector.outcomeTriggered = tc.statusTriggered, tc.outcomeTriggered
			guard := keyrouting.NewFirstContentGuard(0)
			defer guard.Finish()
			c.Request = c.Request.WithContext(keyrouting.WithFirstContentGuard(c.Request.Context(), guard))
			c.Writer = newFirstContentWriter(c.Writer, guard, true)
			var groups []int64
			KeyRouteDispatch(func(c *gin.Context) {
				key, _ := servermiddleware.GetAPIKeyFromContext(c)
				groups = append(groups, *key.GroupID)
				if *key.GroupID != 1 {
					c.JSON(http.StatusOK, gin.H{"backup": true})
					return
				}
				guard.Detach(c.Request.Context())
				// The native parser can receive usage before it emits anything,
				// then discard its partial result while returning a retryable error.
				body := keyrouting.ProtectUsageReader(c.Request.Context(), io.NopCloser(strings.NewReader("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n")))
				_, err := io.ReadAll(body)
				require.NoError(t, err)
				require.NoError(t, body.Close())
				require.False(t, c.Writer.Written())
				require.Equal(t, -1, c.Writer.Size())
				require.False(t, guard.ReplaySafe())
				if tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: tc.status}) {
					return
				}
				c.JSON(tc.status, gin.H{"error": "preserve the original native result"})
			})(c)
			require.Equal(t, []int64{1}, groups, "known usage must prohibit a second billable group even before writer output")
			require.Empty(t, selector.switched)
			require.Equal(t, []int64{int64(tc.status)}, selector.statuses, "failure observation must remain intact")
			require.Len(t, selector.outcomes, 1)
			require.False(t, c.GetBool("custom.key_route_restart"))
		})
	}
}
