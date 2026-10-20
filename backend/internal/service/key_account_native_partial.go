package service

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
)

// A failed dispatch with reported usage is a terminal billable partial, never
// a failover signal. Do not expose Unwrap: native handlers test errors.As for
// UpstreamFailoverError before they submit a partial result for settlement.
type keyAccountNativePartialError struct{ cause error }

func (e *keyAccountNativePartialError) Error() string { return e.cause.Error() }

func keyAccountKeepPartialError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*keyAccountNativePartialError); ok {
		return err
	}
	return &keyAccountNativePartialError{cause: err}
}

func keyAccountHasNativePartial(resp *http.Response, usage *ClaudeUsage) bool {
	return KeyAccountResponseContext(resp) != nil && usage.hasObservedTokens()
}

// Only callers holding an already decoded, actual native usage object may use
// this helper. It does not estimate tokens, price anything or submit a ledger.
func keyAccountPartialUsageResult(resp *http.Response, c *gin.Context, usage *ClaudeUsage, firstTokenMs *int, model, upstreamModel string, stream bool, start time.Time, err error) (*ForwardResult, error) {
	if err == nil || !keyAccountHasNativePartial(resp, usage) {
		return nil, err
	}
	return &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"), UpstreamHeaders: resp.Header,
		Usage: *usage, Model: model, UpstreamModel: upstreamModel, Stream: stream,
		UpstreamResponseModel:         observedUpstreamResponseModel(c),
		UpstreamResponseModelConflict: observedUpstreamResponseModelConflict(c),
		UpstreamResponseServiceTier:   observedUpstreamResponseServiceTier(c),
		Duration:                      time.Since(start), FirstTokenMs: firstTokenMs,
	}, keyAccountKeepPartialError(err)
}

// A decoded Gemini error carrying content/usage is still an error, but is not
// replay-safe. Keep the existing pure-transient failover signal only when the
// original immutable wire's monotonic safety evidence allows it.
func keyAccountGeminiProtocolError(ctx context.Context, resp *http.Response, payload []byte) error {
	if ctx == nil || keyrouting.GetAccountAttempt(ctx) == nil {
		return nil
	}
	root := keyAccountProtocolObject(payload)
	errObject, _ := root["error"].(map[string]any)
	code, err := strconv.Atoi(keyAccountProtocolString(errObject["code"]))
	if err != nil || code < 400 || code > 599 || !keyAccountProtocolErrorDetails(errObject) {
		return nil
	}
	if failure := keyAccountGeminiProtocolFailure(ctx, resp, payload); failure != nil {
		return failure
	}
	return fmt.Errorf("Gemini upstream error %d: %s", code, keyAccountProtocolString(errObject["message"]))
}
