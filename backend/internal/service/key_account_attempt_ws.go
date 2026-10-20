package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// This adapter is only for HTTP Responses forwarded over native upstream WSv2.
// Pool handshakes/prewarm and client WebSocket ingress never create attempts.
// HTTP provenance remains absent: a structured WS error is not an HTTP status.
type keyAccountWSAttempt struct {
	wire       *keyAccountWireAttempt
	ctx        context.Context
	proof      *keyrouting.AccountAttemptPreludeProof
	failure    *UpstreamFailoverError
	outcome    keyrouting.RouteOutcomeKind
	cleanupErr error
	// Native collectors stay owned by this synchronous Forward call. They are
	// also fed complete late frames drained during broken-lease cleanup.
	nativeObserve func([]byte)
	nativeResult  func() *OpenAIForwardResult
}

func beginKeyAccountWSAttempt(ctx context.Context, account *Account, wsURL, model string) (*keyAccountWSAttempt, context.Context, error) {
	scope := KeyAccountForwardScopeFromContext(ctx)
	if scope == nil {
		return nil, ctx, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wsURL, nil)
	if err != nil {
		return nil, nil, err
	}
	wire, wireCtx, err := scope.begin(req, account, model)
	if err != nil {
		return nil, nil, err
	}
	// A WS lease has no HTTP body, but it still needs an explicit cleanup
	// barrier. It is closed only after lease release and reader quiescence.
	wire.mu.Lock()
	wire.body = &keyAccountWireBody{ReadCloser: io.NopCloser(strings.NewReader("")), context: wireCtx}
	wire.mu.Unlock()
	return &keyAccountWSAttempt{wire: wire, ctx: wireCtx, proof: keyrouting.NewAccountAttemptPreludeProof("/v1/responses", true), outcome: keyrouting.OutcomeFailure}, wireCtx, nil
}

func (a *keyAccountWSAttempt) sent(err error) {
	if a == nil {
		return
	}
	a.wire.scope.mu.Lock()
	a.wire.scope.dispatching = false
	a.wire.scope.mu.Unlock()
	if err != nil {
		// An interrupted write is not a complete request/frame boundary. It
		// cannot prove the server did not already receive the full inference.
		a.wire.mu.Lock()
		a.wire.transportErr = err
		a.wire.mu.Unlock()
		a.wire.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
	}
}

// Observe raw complete WS messages before model/tool rewriting. Observe every
// concatenated document, including a tail the native terminal parser ignores.
func (a *keyAccountWSAttempt) observe(message []byte) {
	if a == nil {
		return
	}
	if documents, repaired := splitOpenAIConcatenatedJSONDocuments(message); repaired {
		for _, document := range documents {
			a.observeDocument(document)
		}
		return
	}
	a.observeDocument(message)
}

func (a *keyAccountWSAttempt) observeDocument(message []byte) {
	if a.nativeObserve != nil {
		a.nativeObserve(message)
	}
	if len(message) > 64<<10 {
		a.wire.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
		return
	}
	root := keyAccountProtocolObject(message)
	if root == nil || !keyAccountWSUniqueJSON(gjson.ParseBytes(message), 0) {
		a.wire.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
		return
	}
	observeKeyAccountDecodedProtocolPayload(a.ctx, root)
	event := keyAccountProtocolString(root["type"])
	if (event == "response.output_text.delta" || event == "response.refusal.delta") && keyAccountFailureNonempty(root["delta"]) {
		a.wire.attempt.ObserveFirstContent()
	}
	if isOpenAIWSTerminalEvent(event) && event != "response.failed" {
		a.wire.attempt.PreventReplay(keyrouting.AttemptVetoCompleteResponse)
		return
	}
	if event == "error" || event == "response.failed" {
		code, verified := keyAccountWSFailure(root)
		if !verified {
			a.wire.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
			return
		}
		a.wire.attempt.MarkVerifiedFailure()
		status, outcome := keyAccountWSTransient(code)
		if status == 0 {
			a.wire.attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
			return
		}
		a.outcome = outcome
		a.failure = &UpstreamFailoverError{StatusCode: status, ResponseBody: append([]byte(nil), message...)}
		a.wire.mu.Lock()
		a.wire.transportErr = a.failure // Structured WS failure, never HTTP provenance.
		a.wire.mu.Unlock()
		return
	}
	a.proof.Observe(append(append([]byte("data: "), message...), '\n', '\n'))
	if !a.proof.Safe() {
		// Includes unknown frames, tool/reasoning events, nonempty item/output
		// and malformed preludes. They cannot authorize another inference.
		a.wire.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
	}
}

func keyAccountWSUniqueJSON(value gjson.Result, depth int) bool {
	if depth > 16 {
		return false
	}
	if value.IsObject() && hasDuplicateJSONObjectKeys(value) {
		return false
	}
	valid := true
	if value.IsObject() || value.IsArray() {
		value.ForEach(func(_, nested gjson.Result) bool {
			valid = keyAccountWSUniqueJSON(nested, depth+1)
			return valid
		})
	}
	return valid
}

func keyAccountWSFailure(root map[string]any) (string, bool) {
	for name := range root {
		switch name {
		case "type", "error", "response", "code", "message", "param", "sequence_number", "event_id":
		default:
			return "", false
		}
	}
	errObject, _ := root["error"].(map[string]any)
	if keyAccountProtocolString(root["type"]) == "error" && root["response"] != nil {
		return "", false
	}
	if keyAccountProtocolString(root["type"]) == "response.failed" {
		response, _ := root["response"].(map[string]any)
		for name := range response {
			switch name {
			case "id", "object", "model", "status", "error", "output", "usage", "created_at", "service_tier":
			default:
				return "", false
			}
		}
		if response["usage"] != nil {
			// A terminal failure with metering belongs to native settlement,
			// even if an unfamiliar provider reports it in a nonnumeric form.
			return "", false
		}
		errObject, _ = response["error"].(map[string]any)
	}
	if errObject == nil && keyAccountProtocolString(root["type"]) == "error" {
		errObject = root
	} else {
		for name := range errObject {
			switch name {
			case "type", "code", "message", "param":
			default:
				return "", false
			}
		}
	}
	if !keyAccountProtocolErrorDetails(errObject) {
		return "", false
	}
	for name, value := range errObject {
		if name == "type" || name == "code" || name == "message" || name == "param" {
			if value != nil {
				switch value.(type) {
				case string:
				default:
					return "", false
				}
			}
		}
	}
	code := keyAccountProtocolString(errObject["code"])
	if code == "" {
		code = keyAccountProtocolString(errObject["type"])
	}
	return code, true
}

func keyAccountWSTransient(code string) (int, keyrouting.RouteOutcomeKind) {
	switch code {
	case "rate_limit_exceeded", "rate_limit_error", "429":
		return http.StatusTooManyRequests, keyrouting.OutcomeRateLimit
	case "timeout", "request_timeout", "408", "504":
		return http.StatusGatewayTimeout, keyrouting.OutcomeTimeout
	case "server_error", "internal_error", "internal_server_error", "upstream_error", "overloaded", "overloaded_error", "server_is_overloaded", "slow_down", "service_unavailable", "500", "502", "503":
		return http.StatusBadGateway, keyrouting.OutcomeFailure
	default:
		return 0, keyrouting.OutcomeFailure
	}
}

func (a *keyAccountWSAttempt) safeFailure() *UpstreamFailoverError {
	if a == nil || a.failure == nil || a.wire.scope.ReplayBlocked() {
		return nil
	}
	return a.failure
}

func (a *keyAccountWSAttempt) readFailed(err error) {
	if a == nil || err == nil {
		return
	}
	// An ordinary disconnect/EOF cannot prove no inference occurred. Only a
	// complete safe prelude plus THIS wire's local cancellation grants replay.
	verified := keyAccountFirstContentCancellationError(err) &&
		errors.Is(context.Cause(a.ctx), keyrouting.ErrAccountAttemptFirstContent) &&
		a.wire.scope.parent.Err() == nil && a.wire.nativeContext.Err() == nil && a.proof.Safe() && !a.wire.attempt.ReplayBlocked()
	a.wire.mu.Lock()
	a.wire.transportErr, a.wire.ttftTransportCancellation = err, verified
	a.wire.mu.Unlock()
	if !verified {
		a.wire.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
	}
}

// complete runs AFTER native release. A broken native reader must really exit;
// the bounded wait is a veto on timeout, never a fake cleanup acknowledgment.
func (a *keyAccountWSAttempt) complete(lease *openAIWSConnLease, clean bool, result *OpenAIForwardResult, nativeErr error, c *gin.Context) *OpenAIForwardResult {
	if a == nil {
		return result
	}
	quiescent := true
	if !clean && lease != nil && lease.conn != nil && lease.conn.readerLoopResults != nil {
		timer := time.NewTimer(250 * time.Millisecond)
		defer timer.Stop()
	readers:
		for {
			select {
			case payload, open := <-lease.conn.readerLoopResults:
				if !open {
					break readers
				}
				a.observe(payload) // Late usage/output remains a replay veto.
			case <-timer.C:
				quiescent = false
				a.wire.attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
				break readers
			}
		}
	}
	if quiescent {
		_ = a.wire.body.Close()
	} else {
		a.cleanupErr = ErrKeyAccountBodyPending
	}
	if a.nativeResult != nil {
		// Late metering must reach the same native settlement result, not just
		// the replay veto. There is still exactly one native billing call.
		if partial := a.nativeResult(); KeyAccountForwardResultHasUsage(partial) {
			if result == nil {
				result = partial
			} else {
				result.Usage, result.ImageCount, result.ImageOutputSizes = partial.Usage, partial.ImageCount, partial.ImageOutputSizes
			}
		}
	}
	terminal := keyrouting.AccountAttemptResult{Outcome: a.outcome, UsageKnown: KeyAccountForwardResultHasUsage(result)}
	if nativeErr == nil {
		terminal.Outcome, terminal.CompleteResponse = keyrouting.OutcomeSuccess, true
	}
	if c != nil && c.Writer != nil {
		terminal.ResponseCommitted = c.Writer.Written() || c.Writer.Size() > 0
	}
	a.wire.finish(terminal)
	a.wire.cleanupNativeContext()
	return result
}

// Native WS retry/recovery must obey the same replay barrier as HTTP Do. No
// inference yet (e.g. failed acquire) leaves the original retry path unchanged.
func keyAccountWSReplayAllowed(ctx context.Context) bool {
	scope := KeyAccountForwardScopeFromContext(ctx)
	if scope == nil {
		return true
	}
	scope.mu.Lock()
	wire := scope.current
	scope.mu.Unlock()
	return wire == nil || wire.bodyReady() && !scope.ReplayBlocked() && wire.retryableFailure()
}
