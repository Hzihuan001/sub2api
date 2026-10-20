package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const keyAccountForwardStateKey = "custom.key_account_forward"

type keyAccountCandidateSelector interface {
	AccountGroupHasCandidate(context.Context, *service.APIKey, service.KeyRoutingRequest, int64) (bool, error)
	AccountFirstContentTimeout(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, int64) (time.Duration, error)
}

// This state owns only the custom adapter. Native Forward retains ownership
// of parsing, account slots, reservations and billing. Keep its context out of
// the next native request builder; a finished guard must never be reused.
type keyAccountForwardState struct {
	scope       *service.KeyAccountForwardScope
	session     *service.KeyAccountRoutingSession
	parent      context.Context
	writer      *keyAccountForwardWriter
	groupID     int64
	account     int64
	ended       bool
	canReplay   bool
	dispatchErr error
}

func keyAccountForwardStateFromContext(c *gin.Context) *keyAccountForwardState {
	if c == nil {
		return nil
	}
	value, _ := c.Get(keyAccountForwardStateKey)
	state, _ := value.(*keyAccountForwardState)
	return state
}

// Install immediately before native Forward, after account acquisition. Real
// dispatch and timer start still belong to KeyAccountInferenceDo*. A setup
// error is passed through BeforeDispatch so it cannot silently bypass a gate.
func keyAccountForwardContext(c *gin.Context, native context.Context, account *service.Account) context.Context {
	if c == nil || c.Request == nil {
		return native
	}
	session := service.GetKeyAccountRoutingSession(native)
	if session == nil {
		return native
	}
	rememberKeyAccountPricingGate(c, native)
	key, keyOK := servermiddleware.GetAPIKeyFromContext(c)
	request, requestOK := keyRouteRequest(c)
	var setupErr error
	var metadata keyrouting.AccountAttemptMetadata
	if !keyOK || key == nil || key.GroupID == nil || account == nil || !requestOK {
		setupErr = service.ErrKeyAccountRouteScope
	} else {
		metadata, setupErr = session.Metadata(*key.GroupID, account.ID, request.Model, account.Platform)
	}
	root := key
	if key != nil {
		root = cloneKeyRoutingPrimaryKey(c, key)
	}
	selectorValue, _ := c.Get(keyRoutingSelectorContextKey)
	selector, selectorOK := selectorValue.(keyAccountCandidateSelector)
	state := &keyAccountForwardState{session: session, parent: c.Request.Context(), groupID: metadata.GroupID, account: metadata.AccountID}
	baseWriter := c.Writer
	options := service.KeyAccountForwardOptions{
		Metadata:     metadata,
		ReplayUnsafe: func() bool { return baseWriter.Written() || baseWriter.Size() > 0 },
		BeforeDispatch: func(ctx context.Context, actual keyrouting.AccountAttemptMetadata) error {
			if setupErr != nil {
				state.dispatchErr = setupErr
				return setupErr
			}
			state.dispatchErr = session.BeforeDispatch(ctx, actual)
			return state.dispatchErr
		},
		FirstContentTimeoutForDispatch: func(ctx context.Context, actual keyrouting.AccountAttemptMetadata) (time.Duration, error) {
			if baseWriter.Written() || baseWriter.Size() > 0 || !keyAccountNativeSwitchAvailable(c, actual.GroupID) {
				return 0, nil
			}
			if !selectorOK {
				return 0, service.ErrKeyAccountRouteScope
			}
			return selector.AccountFirstContentTimeout(ctx, root, request, actual.GroupID, actual.AccountID)
		},
		ObserveAttempt: func(ctx context.Context, sample keyrouting.AccountAttemptSample) error {
			_, err := session.Observe(ctx, sample)
			if err != nil {
				// An observational failure must not replace a successful native
				// result, suppress settlement, or authorize another generation.
				slog.WarnContext(ctx, "key_account_attempt_observation_failed", "key_id", sample.Metadata.APIKeyID, "group_id", sample.Metadata.GroupID, "account_id", sample.Metadata.AccountID, "attempt_id", sample.Metadata.AttemptID, "error", err)
			}
			return err
		},
	}
	ctx, scope := service.WithKeyAccountForwardScope(native, options)
	state.scope = scope
	state.writer = newKeyAccountForwardWriter(c.Writer, scope)
	c.Set(keyAccountForwardStateKey, state)
	c.Writer = state.writer
	c.Request = c.Request.WithContext(ctx)
	return ctx
}

// Called by the existing post-Forward hook AFTER account-slot and native
// serial-lock release. Shared user concurrency remains owned by the handler.
// A TTFT restart returns from that handler before dispatch can re-enter it,
// so its remaining deferred request cleanup also completes first.
func finishKeyAccountNativeForward(c *gin.Context, result any, forwardErr error) bool {
	state := keyAccountForwardStateFromContext(c)
	if state == nil || state.ended {
		return false
	}
	state.ended = true
	terminal := keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure}
	if forwardErr == nil {
		terminal.Outcome, terminal.CompleteResponse = keyrouting.OutcomeSuccess, true
	} else {
		var failover *service.UpstreamFailoverError
		var transport net.Error
		switch {
		case errors.As(forwardErr, &failover):
			terminal.Outcome = keyRouteOutcome(failover)
			// A native failover code may be synthesized after HTTP 200.
			// The transport supplies actual status provenance instead.
		case errors.Is(forwardErr, context.DeadlineExceeded):
			terminal.Outcome = keyrouting.OutcomeTimeout
		case errors.As(forwardErr, &transport) && transport.Timeout():
			terminal.Outcome = keyrouting.OutcomeTimeout
		case errors.Is(forwardErr, io.ErrUnexpectedEOF):
			terminal.Outcome = keyrouting.OutcomeFailure
		}
	}
	terminal.ResponseCommitted = state.writer.ResponseWriter.Written() || state.writer.ResponseWriter.Size() > 0
	sample, err := state.scope.Finish(result, terminal)
	if err != nil {
		slog.WarnContext(state.parent, "key_account_forward_finish_failed", "account_id", state.account, "error", err)
	}
	clean := state.scope.CleanupAck()
	safe := clean && state.scope.CanStartNext() && state.parent.Err() == nil && !c.IsAborted()
	// A fresh cooling/path rejection may occur after native slot waiting and
	// before any wire exists. It is safe reselection, not an upstream failure.
	admissionRejected := errors.Is(state.dispatchErr, service.ErrKeyAccountRouteCooling) || errors.Is(state.dispatchErr, keyrouting.ErrAccountRouteRepeatedPath)
	if !sample.Dispatched && admissionRejected && clean && !terminal.ResponseCommitted && !state.scope.ReplayBlocked() && state.parent.Err() == nil && !c.IsAborted() {
		safe = true
	}
	state.canReplay = safe
	state.writer.finalize(safe && forwardErr != nil)
	c.Writer = state.writer.ResponseWriter
	c.Request = c.Request.WithContext(state.parent)
	if !safe || (!sample.FirstContentExpired && !admissionRejected) || forwardErr == nil {
		return false
	}
	if err := state.session.ExcludeRequestAccount(state.groupID, state.account); err != nil {
		return false
	}
	// Do not manufacture an upstream 504 or globally stop the account. The
	// outer dispatcher waits for the full handler to return before retrying.
	c.Set("custom.key_account_restart", true)
	c.Set("custom.key_route_restart", true)
	c.Set("custom.key_account_count_switch", sample.FirstContentExpired || (admissionRejected && sample.Dispatched))
	return true
}

func keyAccountForwardReplayBlocked(c *gin.Context) bool {
	state := keyAccountForwardStateFromContext(c)
	return state != nil && (!state.ended || !state.canReplay)
}

func keyAccountExcludeFailed(ctx context.Context, groupID *int64, accountID int64) {
	if session := service.GetKeyAccountRoutingSession(ctx); session != nil && groupID != nil {
		_ = session.ExcludeRequestAccount(*groupID, accountID)
	}
}

// The native writer can emit a harmless SSE prelude before semantic content.
// Observe each real wire's guard dynamically, but keep raw readers frozen to
// their wire. The writer never starts a timer and never runs from a timer.
type keyAccountForwardWriter struct {
	gin.ResponseWriter
	mu      sync.Mutex
	scope   *service.KeyAccountForwardScope
	current *keyrouting.AccountAttempt
	writer  *firstContentWriter
}

func newKeyAccountForwardWriter(base gin.ResponseWriter, scope *service.KeyAccountForwardScope) *keyAccountForwardWriter {
	return &keyAccountForwardWriter{ResponseWriter: base, scope: scope, writer: newFirstContentWriter(base, keyrouting.NewFirstContentGuard(0), true)}
}

func (w *keyAccountForwardWriter) active() *firstContentWriter {
	attempt := w.scope.CurrentAttempt()
	if attempt == nil || attempt == w.current {
		return w.writer
	}
	if w.current != nil && w.current.ReplayBlocked() {
		// The transport must forbid dispatching another wire in this state.
		attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
	}
	w.current = attempt
	w.writer.guard = attempt.Guard()
	w.writer.observe = attempt.ObserveFirstContent
	return w.writer
}

func (w *keyAccountForwardWriter) Header() http.Header {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active().Header()
}
func (w *keyAccountForwardWriter) Status() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active().Status()
}
func (w *keyAccountForwardWriter) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active().Size()
}
func (w *keyAccountForwardWriter) Written() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active().Written()
}
func (w *keyAccountForwardWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active().WriteHeader(status)
}
func (w *keyAccountForwardWriter) WriteHeaderNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active().WriteHeaderNow()
}
func (w *keyAccountForwardWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active().Write(p)
}
func (w *keyAccountForwardWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *keyAccountForwardWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active().Flush()
}
func (w *keyAccountForwardWriter) finalize(discard bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.active().Finalize(discard)
}
