package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFirstContentDispatchSamplesOnlyCountedFastFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind keyrouting.RouteOutcomeKind
		want int
	}{
		{"provider parameter ignored", keyrouting.OutcomeIgnored, 0},
		{"upstream failure counted", keyrouting.OutcomeFailure, 1},
		{"upstream rate limit counted", keyrouting.OutcomeRateLimit, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, base := newDispatchTestContext(t)
			selector := &firstContentTestSelector{dispatchTestSelector: base}
			c.Set(keyRoutingSelectorContextKey, selector)
			KeyRouteDispatch(func(c *gin.Context) {
				guard := keyrouting.GetFirstContentGuard(c.Request.Context())
				guard.Detach(c.Request.Context())
				recordKeyRouteFailure(c, 400, tc.kind, false)
				c.JSON(http.StatusBadRequest, gin.H{"error": "fixture failure"})
			})(c)
			require.Len(t, selector.latencies, tc.want)
		})
	}
}

func TestFirstContentDispatchDoesNotSampleLocalAdmission(t *testing.T) {
	c, base := newDispatchTestContext(t)
	selector := &firstContentTestSelector{dispatchTestSelector: base}
	c.Set(keyRoutingSelectorContextKey, selector)
	KeyRouteDispatch(func(c *gin.Context) {
		// Native admission never dispatched an upstream request.
		c.JSON(http.StatusForbidden, gin.H{"error": "fixture local admission"})
	})(c)
	require.Empty(t, selector.latencies)
}
