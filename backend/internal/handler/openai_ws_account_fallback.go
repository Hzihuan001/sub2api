package handler

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var errOpenAIWSAccountFallbackEmptyPayload = errors.New("account fallback payload is empty")

// prepareOpenAIWSAccountFallback removes only server-side response references
// and disables stored per-session state for the next credential. User content
// already present in the request is preserved by the service sanitizer.
func prepareOpenAIWSAccountFallback(ctx context.Context, body []byte) (context.Context, []byte, error) {
	safeBody, err := service.RemoveOpenAIStoredResponseReferencesFromBody(body)
	if err != nil {
		return ctx, nil, err
	}
	if len(safeBody) == 0 {
		return ctx, nil, errOpenAIWSAccountFallbackEmptyPayload
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.OpenAIWSDisableStoredTurnState, true), safeBody, nil
}
