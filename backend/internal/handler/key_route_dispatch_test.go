package handler

import (
	"context"
	"errors"
	"io"
	"net"
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

type dispatchOutcome struct {
	group int64
	kind  keyrouting.RouteOutcomeKind
}

type dispatchTestSelector struct {
	statusTriggered  bool
	outcomeTriggered bool
	statusErr        error
	next             map[int64]*service.APIKey
	statuses         []int64
	outcomes         []dispatchOutcome
	switched         []int64
	primaryIDs       []int64
}

func (s *dispatchTestSelector) RecordFailure(_ context.Context, key *service.APIKey, _ service.KeyRoutingRequest, group, status int64) (keyrouting.RouteFailureResult, error) {
	s.statuses = append(s.statuses, status)
	s.primaryIDs = append(s.primaryIDs, *key.GroupID)
	return keyrouting.RouteFailureResult{Triggered: s.statusTriggered}, s.statusErr
}

func (s *dispatchTestSelector) RecordOutcome(_ context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, group int64, kind keyrouting.RouteOutcomeKind) (keyrouting.RouteFailureResult, error) {
	s.outcomes = append(s.outcomes, dispatchOutcome{group: group, kind: kind})
	return keyrouting.RouteFailureResult{Triggered: s.outcomeTriggered}, nil
}

func (s *dispatchTestSelector) SelectNext(_ context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, group int64) (*service.APIKey, keyrouting.Plan, error) {
	s.switched = append(s.switched, group)
	next := s.next[group]
	if next == nil {
		return nil, keyrouting.Plan{}, keyrouting.ErrNoSchedulableAccounts
	}
	return next, keyrouting.Plan{SelectedGroupID: *next.GroupID}, nil
}

func (s *dispatchTestSelector) PrepareRouteContext(ctx context.Context, key *service.APIKey, request service.KeyRoutingRequest) (context.Context, string, error) {
	return service.WithResolvedTargetPlatform(ctx, key.Group.Platform), "upstream-" + request.Model, nil
}

func newDispatchTestContext(t *testing.T) (*gin.Context, *dispatchTestSelector) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	// Keep the underlying recorder available to tests that need to inspect the
	// buffered body after a key-local failover attempt.
	c.Set("test.response_recorder", recorder)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model","messages":[]}`))
	ctx := context.WithValue(request.Context(), ctxkey.KeyRoutingActive, true)
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingPrimaryGroupID, int64(1))
	c.Request = request.WithContext(ctx)
	primaryID, backupID := int64(1), int64(2)
	key := &service.APIKey{ID: 10, UserID: 20, GroupID: &primaryID, BackupGroupIDs: []int64{backupID}, User: &service.User{ID: 20}, Group: &service.Group{ID: primaryID, Name: "primary", Platform: service.PlatformOpenAI}}
	backup := &service.APIKey{ID: key.ID, UserID: key.UserID, GroupID: &backupID, BackupGroupIDs: key.BackupGroupIDs, User: key.User, Group: &service.Group{ID: backupID, Name: "backup", Platform: service.PlatformDeepseek}}
	selector := &dispatchTestSelector{next: map[int64]*service.APIKey{primaryID: backup}}
	c.Set(string(servermiddleware.ContextKeyAPIKey), key)
	c.Set(keyRoutingSelectorContextKey, selector)
	c.Set("custom.key_route_request", service.KeyRoutingRequest{Model: "model", Endpoint: service.CompositeRouteEndpointChatCompletions})
	c.Set("custom.key_route_base_context", ctx)
	c.Set("custom.key_route_body", []byte(`{"model":"model","messages":[]}`))
	return c, selector
}

func TestKeyRouteDispatchFinalOutcomeSwitchesAfterHandlerCleanup(t *testing.T) {
	c, selector := newDispatchTestContext(t)
	selector.outcomeTriggered = true
	var groups []int64
	cleaned := false
	KeyRouteDispatch(func(c *gin.Context) {
		key, ok := servermiddleware.GetAPIKeyFromContext(c)
		require.True(t, ok)
		groups = append(groups, *key.GroupID)
		if *key.GroupID == 1 {
			defer func() { cleaned = true }()
			require.True(t, tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: 429}))
			return
		}
		require.True(t, cleaned, "the first handler's reservations must be released before backup dispatch")
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformDeepseek, platform)
		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"upstream-model","messages":[]}`, string(body))
		recordKeyRouteSuccess(c)
		recordKeyRouteSuccess(c)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})(c)
	require.Equal(t, []int64{1, 2}, groups)
	require.Equal(t, []int64{1}, selector.primaryIDs)
	require.Equal(t, []dispatchOutcome{{group: 1, kind: keyrouting.OutcomeRateLimit}, {group: 2, kind: keyrouting.OutcomeSuccess}}, selector.outcomes)
	require.Equal(t, []int64{1}, selector.switched)
	require.Equal(t, "backup", c.GetString("custom.key_route_switch_group"))
}

func TestKeyRouteFinalHookDeduplicatesAndHonorsThreshold(t *testing.T) {
	c, selector := newDispatchTestContext(t)
	c.Set("custom.key_route_dispatch", true)
	failure := &service.UpstreamFailoverError{StatusCode: 503}
	require.False(t, tryFinalKeyRouteFailover(c, failure))
	require.False(t, tryFinalKeyRouteFailover(c, failure))
	recordKeyRouteSuccess(c)
	require.Equal(t, []int64{503}, selector.statuses)
	require.Equal(t, []dispatchOutcome{{group: 1, kind: keyrouting.OutcomeFailure}}, selector.outcomes)
	require.Empty(t, selector.switched)
}

func TestKeyRouteFinalHookRecordsWithoutReplayingStream(t *testing.T) {
	for _, write := range []struct {
		name  string
		apply func(*gin.Context)
	}{
		{name: "body", apply: func(c *gin.Context) { _, _ = c.Writer.Write([]byte("data: partial\n\n")) }},
		{name: "headers flushed", apply: func(c *gin.Context) { c.Writer.Flush() }},
	} {
		t.Run(write.name, func(t *testing.T) {
			c, selector := newDispatchTestContext(t)
			c.Set("custom.key_route_dispatch", true)
			selector.statusTriggered, selector.outcomeTriggered = true, true
			write.apply(c)
			require.False(t, tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: 504}))
			require.Equal(t, []dispatchOutcome{{group: 1, kind: keyrouting.OutcomeTimeout}}, selector.outcomes)
			require.Empty(t, selector.switched, "a stream must never be replayed after flushing")
		})
	}
}

func TestKeyRouteFinalHookCancellationAndPolicyReadFailure(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		c, selector := newDispatchTestContext(t)
		c.Set("custom.key_route_dispatch", true)
		ctx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(ctx)
		require.False(t, tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: 503}))
		recordKeyRouteSuccess(c)
		require.Empty(t, selector.statuses)
		require.Empty(t, selector.outcomes)
	})
	t.Run("policy error", func(t *testing.T) {
		c, selector := newDispatchTestContext(t)
		c.Set("custom.key_route_dispatch", true)
		selector.statusErr = errors.New("settings unavailable")
		selector.statusTriggered = true
		require.False(t, tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: 503}))
		require.Empty(t, selector.switched)
	})
}

func TestKeyRouteDispatchStopsOnWriteAbortCancelAndBound(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop func(*gin.Context)
		want int
	}{
		{name: "written", stop: func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) }, want: 1},
		{name: "aborted", stop: func(c *gin.Context) { c.Abort() }, want: 1},
		{name: "cancelled", stop: func(c *gin.Context) {
			ctx, cancel := context.WithCancel(c.Request.Context())
			cancel()
			c.Request = c.Request.WithContext(ctx)
		}, want: 1},
		{name: "bounded", stop: func(c *gin.Context) {}, want: keyrouting.MaxBackupGroups + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newDispatchTestContext(t)
			calls := 0
			KeyRouteDispatch(func(c *gin.Context) {
				calls++
				c.Set("custom.key_route_restart", true)
				tc.stop(c)
			})(c)
			require.Equal(t, tc.want, calls)
			if tc.name == "bounded" {
				require.Equal(t, http.StatusBadGateway, c.Writer.Status())
			}
		})
	}
}

func TestKeyRouteOutcomeClassifiesOnlyRealUpstreamFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   keyrouting.RouteOutcomeKind
	}{
		{408, keyrouting.OutcomeTimeout}, {504, keyrouting.OutcomeTimeout}, {522, keyrouting.OutcomeTimeout}, {524, keyrouting.OutcomeTimeout},
		{429, keyrouting.OutcomeRateLimit}, {500, keyrouting.OutcomeFailure}, {529, keyrouting.OutcomeFailure},
		{400, keyrouting.OutcomeIgnored}, {404, keyrouting.OutcomeIgnored}, {422, keyrouting.OutcomeIgnored},
	} {
		require.Equal(t, tc.want, keyRouteOutcome(&service.UpstreamFailoverError{StatusCode: tc.status}))
	}
}

func TestKeyRouteForwardFailureRecordsSemanticResultWithoutRestart(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want keyrouting.RouteOutcomeKind
	}{
		{name: "upstream deadline", err: context.DeadlineExceeded, want: keyrouting.OutcomeTimeout},
		{name: "network timeout", err: &net.DNSError{IsTimeout: true}, want: keyrouting.OutcomeTimeout},
		{name: "connection error", err: &net.OpError{Op: "read", Net: "tcp", Err: errors.New("connection reset")}, want: keyrouting.OutcomeFailure},
		{name: "truncated stream", err: io.ErrUnexpectedEOF, want: keyrouting.OutcomeFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, selector := newDispatchTestContext(t)
			c.Set("custom.key_route_dispatch", true)
			selector.statusTriggered, selector.outcomeTriggered = true, true
			// A previous group's diagnostic code must not be mistaken for this
			// transport failure's real upstream HTTP response.
			service.SetOpsUpstreamError(c, 429, "stale prior group error", "")
			recordKeyRouteForwardFailure(c, tc.err)
			recordKeyRouteForwardFailure(c, tc.err)
			recordKeyRouteSuccess(c)
			require.Empty(t, selector.statuses, "do not manufacture HTTP status counters for transport errors")
			require.Equal(t, []dispatchOutcome{{group: 1, kind: tc.want}}, selector.outcomes)
			require.Empty(t, selector.switched)
			require.False(t, c.GetBool("custom.key_route_restart"), "uncertain partial results must not be replayed")
		})
	}
}

func TestKeyRouteForwardFailureIgnoresUnknownCancelAndSizeErrors(t *testing.T) {
	for _, err := range []error{nil, errors.New("unknown error"), context.Canceled, service.ErrUpstreamResponseBodyTooLarge} {
		c, selector := newDispatchTestContext(t)
		c.Set("custom.key_route_dispatch", true)
		recordKeyRouteForwardFailure(c, err)
		require.Empty(t, selector.outcomes)
	}
	c, selector := newDispatchTestContext(t)
	c.Set("custom.key_route_dispatch", true)
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)
	recordKeyRouteForwardFailure(c, context.DeadlineExceeded)
	require.Empty(t, selector.outcomes, "a dead client context must not count as an upstream timeout")
}
