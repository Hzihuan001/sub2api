package handler

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIsOpenAIResponseRecoveryEligibleErrorOnlyAllowsExplicitTransientHTTPFailures(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout} {
		require.True(t, isOpenAIResponseRecoveryEligibleError(&service.UpstreamFailoverError{StatusCode: status, ResponseBody: []byte(`{"error":"temporary"}`)}), status)
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict} {
		require.False(t, isOpenAIResponseRecoveryEligibleError(&service.UpstreamFailoverError{StatusCode: status, ResponseBody: []byte(`{"error":"request"}`)}), status)
	}
	require.False(t, isOpenAIResponseRecoveryEligibleError(&service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}), "missing body")
}
