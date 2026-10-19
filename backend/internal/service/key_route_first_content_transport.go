package service

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

// A user-owned first-content deadline is not evidence of an unhealthy account
// or proxy. Preserve the transport error for the native handler's narrow guard
// hook rather than classifying it as a provider failure or synthetic status.
func keyRouteFirstContentExpired(ctx context.Context) bool {
	guard := keyrouting.GetFirstContentGuard(ctx)
	return guard != nil && guard.Expired()
}

func protectKeyRouteUsageResponse(ctx context.Context, response *http.Response) {
	if response != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		response.Body = keyrouting.ProtectUsageReader(ctx, response.Body)
	}
}
