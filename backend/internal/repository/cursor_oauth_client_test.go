package repository

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Cursor answers /auth/poll with 404 for the whole time the user has not yet
// confirmed in the browser. Every other non-2xx is a real failure and must stay
// one, or a broken flow would poll forever.
func TestCursorDeepLinkPollPendingOnlyCovers404(t *testing.T) {
	require.True(t, cursorDeepLinkPollPending(http.StatusNotFound))

	for _, status := range []int{
		http.StatusOK,
		http.StatusAccepted,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	} {
		require.False(t, cursorDeepLinkPollPending(status), "status %d must not be treated as pending", status)
	}
}
