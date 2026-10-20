package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
)

var (
	ErrKeyAccountForwardClosed = errors.New("key account native forward scope is closed")
	ErrKeyAccountDispatchBusy  = errors.New("key account inference dispatch is still active")
	ErrKeyAccountBodyPending   = errors.New("key account preceding response body is not finished")
	ErrKeyAccountReplayBlocked = errors.New("key account inference replay is not safe")
)

// KeyAccountForwardOptions contains an immutable native-Forward identity. The
// helper replaces account/platform/upstream-model/upstream-endpoint with the
// actual dispatch. Public model/endpoint remain the frozen health identity.
// BeforeDispatch is the final admission and request-budget port; an error
// prevents the send. ObserveAttempt is observational: write failures are
// returned by Finish, never used to bypass native usage settlement.
type KeyAccountForwardOptions struct {
	Metadata            keyrouting.AccountAttemptMetadata
	FirstContentTimeout time.Duration
	// Resolve this dispatch's legal-alternative snapshot, not a later timer
	// callback. Zero keeps latency observation but never cancels the last path.
	FirstContentTimeoutForDispatch func(context.Context, keyrouting.AccountAttemptMetadata) (time.Duration, error)
	BeforeDispatch                 func(context.Context, keyrouting.AccountAttemptMetadata) error
	ObserveAttempt                 func(context.Context, keyrouting.AccountAttemptSample) error
	// ReplayUnsafe synchronously checks pre-existing native client output.
	// Callers must not mutate Gin or invoke this callback from timer/readers.
	// Current inference may finish, but no timer or internal replay is allowed.
	ReplayUnsafe func() bool
}

type keyAccountForwardScopeContextKey struct{}

// KeyAccountForwardScope owns wire attempts, not account slots or billing.
// Native internal retries retain their existing slot owner. Only CleanupAck,
// called after the complete native Forward and its resource releases, permits
// the outer coordinator to start a different account/group.
type KeyAccountForwardScope struct {
	mu          sync.Mutex
	parent      context.Context
	opts        KeyAccountForwardOptions
	current     *keyAccountWireAttempt
	wires       []*keyAccountWireAttempt
	dispatching bool
	finished    bool
	finishReady bool
	finishDone  chan struct{}
	cleanup     bool
	lastSample  keyrouting.AccountAttemptSample
	// Media-assisted multi-step chains may finish their first native main
	// inference, but cannot replay it after auxiliary work has been dispatched.
	auxiliaryDispatched bool
	auxiliaryPending    int
}

func WithKeyAccountForwardScope(ctx context.Context, opts KeyAccountForwardOptions) (context.Context, *KeyAccountForwardScope) {
	if ctx == nil {
		ctx = context.Background()
	}
	scope := &KeyAccountForwardScope{parent: ctx, opts: opts, finishDone: make(chan struct{})}
	ctx = context.WithValue(ctx, keyAccountForwardScopeContextKey{}, scope)
	return keyrouting.WithFirstContentGuardProvider(ctx, scope), scope
}

func KeyAccountForwardScopeFromContext(ctx context.Context) *KeyAccountForwardScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(keyAccountForwardScopeContextKey{}).(*KeyAccountForwardScope)
	return scope
}

// KeyAccountResponseContext returns the immutable inference-wire context of a
// tracked response. Native protocol proof must use it, never the scope's later
// CurrentAttempt or resp.Request. The native response.Request is unchanged.
func KeyAccountResponseContext(response *http.Response) context.Context {
	if response == nil {
		return nil
	}
	if body, ok := response.Body.(*keyAccountWireBody); ok {
		return body.context
	}
	// Native transforms may replace the Body (for example a Kiro event-stream
	// pipe). They must delegate the fixed context captured from that response,
	// never resolve the scope's mutable CurrentAttempt at lookup time.
	if body, ok := response.Body.(interface{ KeyAccountWireContext() context.Context }); ok {
		return body.KeyAccountWireContext()
	}
	return nil
}

func (s *KeyAccountForwardScope) CurrentAttempt() *keyrouting.AccountAttempt {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	return s.current.attempt
}

func (s *KeyAccountForwardScope) Guard() *keyrouting.FirstContentGuard {
	return s.CurrentFirstContentGuard()
}

// MarkKeyAccountVerifiedFailure is a narrow proof port for a native parser
// that has actually decoded an upstream 2xx error envelope. A generic
// UpstreamFailoverError, synthetic 502, EOF, or matching error message is not
// proof. A captured old-wire context cannot mark a newer wire's response.
// This marker never clears usage/content/side-effect/completion vetoes.
func MarkKeyAccountVerifiedFailure(ctx context.Context) bool {
	scope := KeyAccountForwardScopeFromContext(ctx)
	if scope == nil {
		return false
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	if scope.finished || scope.dispatching || scope.current == nil {
		return false
	}
	wire := scope.current
	if bound := keyrouting.GetAccountAttempt(ctx); bound != nil && bound != wire.attempt {
		return false
	}
	wire.mu.Lock()
	status := wire.status
	wire.mu.Unlock()
	return status >= 200 && status <= 299 && wire.attempt.MarkVerifiedFailure()
}

// Before a send this deliberately returns nil: legacy request builders must
// not start a timer during signing/credential preparation. Once an error body
// is complete it also returns nil, so a subsequent native retry builder cannot
// detach using the preceding attempt's guard. Captured response readers always
// retain their immutable wire context rather than using this dynamic lookup.
func (s *KeyAccountForwardScope) CurrentFirstContentGuard() *keyrouting.FirstContentGuard {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	wire, finished := s.current, s.finished
	s.mu.Unlock()
	if wire == nil {
		return nil
	}
	if !finished && wire.retryableFailure() && wire.bodyReady() {
		return nil
	}
	return wire.attempt.Guard()
}

func (s *KeyAccountForwardScope) ReplayBlocked() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	wires := append([]*keyAccountWireAttempt(nil), s.wires...)
	auxiliary := s.auxiliaryDispatched
	s.mu.Unlock()
	if auxiliary {
		return true
	}
	if s.parent.Err() != nil {
		return true
	}
	if s.opts.ReplayUnsafe != nil && s.opts.ReplayUnsafe() {
		return true
	}
	for _, wire := range wires {
		if wire.attempt.ReplayBlocked() {
			return true
		}
	}
	return false
}

// Finish is called after native parsing has returned. It never closes a body
// to pretend parsing/settlement completed, releases no native slot, and writes
// no usage. The caller supplies native outcome/provenance and retains its
// existing partial-result billing path regardless of an observation error.
func (s *KeyAccountForwardScope) Finish(nativeResult any, terminal keyrouting.AccountAttemptResult) (keyrouting.AccountAttemptSample, error) {
	if s == nil {
		return keyrouting.AccountAttemptSample{Outcome: keyrouting.OutcomeIgnored}, nil
	}
	s.mu.Lock()
	if s.dispatching {
		s.mu.Unlock()
		return keyrouting.AccountAttemptSample{Outcome: keyrouting.OutcomeIgnored}, ErrKeyAccountDispatchBusy
	}
	if s.finished {
		done := s.finishDone
		s.mu.Unlock()
		<-done
		s.mu.Lock()
		sample := s.lastSample
		wires := append([]*keyAccountWireAttempt(nil), s.wires...)
		s.mu.Unlock()
		return sample, wireObservationErrors(wires)
	}
	s.finished = true
	wire := s.current
	wires := append([]*keyAccountWireAttempt(nil), s.wires...)
	auxiliary := s.auxiliaryDispatched
	s.mu.Unlock()
	sample := keyrouting.AccountAttemptSample{Metadata: s.opts.Metadata, Outcome: keyrouting.OutcomeIgnored, StatusSource: keyrouting.AttemptStatusNone}
	if wire != nil {
		if !auxiliary {
			terminal.UsageKnown = terminal.UsageKnown || KeyAccountForwardResultHasUsage(nativeResult)
		}
		sample = wire.finish(terminal)
	}
	s.mu.Lock()
	s.lastSample = sample
	s.finishReady = true
	close(s.finishDone)
	s.mu.Unlock()
	return sample, wireObservationErrors(wires)
}

// CleanupAck is an explicit outer-owner acknowledgement, not a body-close
// callback. Call only after native transport/parsing cleanup, slots and any
// reservations have been released. It refuses an unfinished/body-active scope.
func (s *KeyAccountForwardScope) CleanupAck() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finished || !s.finishReady || s.dispatching || s.auxiliaryPending != 0 {
		return false
	}
	for _, wire := range s.wires {
		if !wire.bodyClosed() {
			return false
		}
	}
	for _, wire := range s.wires {
		wire.attempt.MarkCleanupComplete()
	}
	s.cleanup = true
	return true
}

func (s *KeyAccountForwardScope) CanStartNext() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	ready := s.finished && s.cleanup && !s.dispatching && s.current != nil
	wire := s.current
	s.mu.Unlock()
	return ready && !s.ReplayBlocked() && wire.attempt.CanStartNext()
}

// KeyAccountInferenceDo and its variants are opt-in inference-only seams.
// Do not use them for auth refresh, model discovery, count_tokens or probes.
// No scope means an exact native pass-through (including request identity).
func KeyAccountInferenceDo(upstream HTTPUpstream, req *http.Request, proxyURL string, account *Account, upstreamModel string) (*http.Response, error) {
	return KeyAccountInferenceDoFunc(req, account, upstreamModel, func(req *http.Request) (*http.Response, error) {
		return upstream.Do(req, proxyURL, account.ID, account.Concurrency)
	})
}

func KeyAccountInferenceDoWithTLS(upstream HTTPUpstream, req *http.Request, proxyURL string, account *Account, upstreamModel string, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return KeyAccountInferenceDoFunc(req, account, upstreamModel, func(req *http.Request) (*http.Response, error) {
		return upstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, profile)
	})
}

// DoFunc wraps the complete native/plugin choice, not just HTTPUpstream, so an
// OAuth plugin cannot bypass the attempt boundary or be counted twice.
func KeyAccountInferenceDoFunc(req *http.Request, account *Account, upstreamModel string, do func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	if req == nil || KeyAccountForwardScopeFromContext(req.Context()) == nil {
		return do(req)
	}
	scope := KeyAccountForwardScopeFromContext(req.Context())
	wire, wireCtx, err := scope.begin(req, account, upstreamModel)
	if err != nil {
		return nil, err
	}
	response, doErr := do(req.WithContext(wireCtx))
	wire.received(response, doErr, wireCtx)
	scope.mu.Lock()
	scope.dispatching = false
	scope.mu.Unlock()
	return response, doErr
}

// Values come from the native builder; cancellation/deadline come from the
// inbound scope, not a preceding legacy WithoutCancel/guard child.
type keyAccountDispatchParent struct {
	context.Context
	values context.Context
}

func (c keyAccountDispatchParent) Value(key any) any { return c.values.Value(key) }

func (s *KeyAccountForwardScope) begin(req *http.Request, account *Account, upstreamModel string) (*keyAccountWireAttempt, context.Context, error) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return nil, nil, ErrKeyAccountForwardClosed
	}
	if s.dispatching {
		s.mu.Unlock()
		return nil, nil, ErrKeyAccountDispatchBusy
	}
	if s.auxiliaryPending != 0 {
		s.mu.Unlock()
		return nil, nil, ErrKeyAccountBodyPending
	}
	previous := s.current
	if previous != nil {
		if s.auxiliaryDispatched {
			previous.attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
			s.mu.Unlock()
			return nil, nil, ErrKeyAccountReplayBlocked
		}
		if s.opts.ReplayUnsafe != nil && s.opts.ReplayUnsafe() {
			previous.attempt.PreventReplay(keyrouting.AttemptVetoResponseCommitted)
			s.mu.Unlock()
			return nil, nil, ErrKeyAccountReplayBlocked
		}
		if !previous.bodyReady() {
			s.mu.Unlock()
			return nil, nil, ErrKeyAccountBodyPending
		}
		// A native 2xx retry could be an empty/protocol/tool response whose
		// billable semantics are not known at this seam. Never guess safe.
		if !previous.retryableFailure() {
			previous.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
			s.mu.Unlock()
			return nil, nil, ErrKeyAccountReplayBlocked
		}
	}
	s.dispatching = true
	s.mu.Unlock()
	clearBusy := func() {
		s.mu.Lock()
		s.dispatching = false
		s.mu.Unlock()
	}
	if previous != nil {
		// EOF alone ends reading, but still close the native tracked body
		// before reusing its transport. Close is idempotent at this wrapper.
		if err := previous.closeBody(); err != nil {
			previous.attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
			clearBusy()
			return nil, nil, fmt.Errorf("preceding key account body cleanup: %w", err)
		}
		previous.finish(previous.httpTerminal())
		if previous.attempt.ReplayBlocked() {
			clearBusy()
			return nil, nil, ErrKeyAccountReplayBlocked
		}
	}
	if err := s.parent.Err(); err != nil {
		clearBusy()
		return nil, nil, err
	}
	if err := req.Context().Err(); err != nil {
		clearBusy()
		return nil, nil, err
	}
	metadata := s.opts.Metadata
	metadata.AttemptID = uuid.NewString() // Every actual wire send gets a fresh identity.
	if account == nil || account.ID <= 0 || req.URL == nil {
		clearBusy()
		return nil, nil, errors.New("key account inference dispatch requires an account and URL")
	}
	metadata.AccountID, metadata.Platform = account.ID, account.Platform
	metadata.UpstreamModel = strings.TrimSpace(upstreamModel)
	metadata.UpstreamEndpoint = req.URL.Path
	if metadata.Endpoint == "" || metadata.UpstreamEndpoint == "" || metadata.UpstreamModel == "" {
		clearBusy()
		return nil, nil, errors.New("key account inference dispatch requires actual model and endpoint")
	}
	// Allocate identity before admission so the request coordinator may bind
	// the budget reservation to this exact send; this does not dispatch it.
	parent := keyAccountDispatchParent{Context: s.parent, values: req.Context()}
	timeout := s.opts.FirstContentTimeout
	if s.hasAuxiliaryDispatch() {
		timeout = 0 // Auxiliary work must not make the main inference replayable.
	} else if s.opts.FirstContentTimeoutForDispatch != nil {
		var err error
		timeout, err = s.opts.FirstContentTimeoutForDispatch(parent, metadata)
		if err != nil {
			clearBusy()
			return nil, nil, err
		}
	}
	if err := s.parent.Err(); err != nil {
		clearBusy()
		return nil, nil, err
	}
	if err := req.Context().Err(); err != nil {
		clearBusy()
		return nil, nil, err
	}
	// The final callback owns authoritative admission plus a conservative
	// dispatch-permit budget. Failed TTL preflight never spends that permit.
	// Cancellation in the tiny post-permit/pre-Do window does not manufacture
	// an actual-dispatch sample, usage or error-count event, nor reuse a permit.
	if s.opts.BeforeDispatch != nil {
		if err := s.opts.BeforeDispatch(parent, metadata); err != nil {
			clearBusy()
			return nil, nil, err
		}
	}
	if err := req.Context().Err(); err != nil {
		clearBusy()
		return nil, nil, err
	}
	attempt := keyrouting.NewAccountAttempt(metadata, timeout)
	if s.hasAuxiliaryDispatch() {
		attempt.PreventReplay(keyrouting.AttemptVetoNativeGuard)
	}
	if s.opts.ReplayUnsafe != nil && s.opts.ReplayUnsafe() {
		attempt.PreventReplay(keyrouting.AttemptVetoResponseCommitted)
	}
	wire := &keyAccountWireAttempt{scope: s, attempt: attempt, nativeContext: req.Context()}
	guardCtx := attempt.Dispatch(parent)
	if guardCtx.Err() != nil {
		attempt.Finish(keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeIgnored})
		clearBusy()
		return nil, nil, guardCtx.Err()
	}
	// Keep provider-owned header/deadline cancellation. The inference guard
	// detaches inbound cancellation for native usage draining, so forwarding
	// req.Context blindly would otherwise either lose native cancellation or
	// mistakenly classify it as a client abort. This watcher is wire-local.
	// Do not inherit guardCtx's anonymous context.Canceled. A separate bridge
	// preserves which cancellation won: this attempt's TTFT, a native timeout,
	// or native body cleanup. Reading context.Cause then provides real proof.
	wireCtx, cancel := context.WithCancelCause(context.WithoutCancel(guardCtx))
	wire.context, wire.nativeCancel = wireCtx, cancel
	wire.stopGuardWatch = context.AfterFunc(guardCtx, func() {
		cause := keyrouting.ErrAccountAttemptFinished
		if errors.Is(attempt.CancellationCause(), keyrouting.ErrAccountAttemptFirstContent) {
			cause = keyrouting.ErrAccountAttemptFirstContent
		}
		cancel(cause)
	})
	wire.stopNativeWatch = context.AfterFunc(req.Context(), func() {
		if s.parent.Err() == nil {
			cancel(context.Cause(req.Context()))
		}
	})
	s.mu.Lock()
	s.current = wire
	s.wires = append(s.wires, wire)
	s.mu.Unlock()
	return wire, wireCtx, nil
}

type keyAccountWireAttempt struct {
	mu                        sync.Mutex
	scope                     *KeyAccountForwardScope
	attempt                   *keyrouting.AccountAttempt
	status                    int
	transportErr              error
	nativeContext             context.Context
	context                   context.Context
	body                      *keyAccountWireBody
	finishOnce                sync.Once
	sample                    keyrouting.AccountAttemptSample
	observeErr                error
	nativeCancel              context.CancelCauseFunc
	stopNativeWatch           func() bool
	stopGuardWatch            func() bool
	ttftTransportCancellation bool
}

func (w *keyAccountWireAttempt) received(resp *http.Response, err error, ctx context.Context) {
	w.mu.Lock()
	w.transportErr = err
	w.ttftTransportCancellation = keyAccountFirstContentCancellationError(err) &&
		errors.Is(context.Cause(ctx), keyrouting.ErrAccountAttemptFirstContent) &&
		w.scope.parent.Err() == nil && (w.nativeContext == nil || w.nativeContext.Err() == nil)
	if resp != nil {
		w.status = resp.StatusCode
		if resp.Body != nil {
			mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
			body := &keyAccountWireBody{
				ReadCloser: keyrouting.ProtectUsageReader(ctx, resp.Body), onClose: w.cleanupNativeContext,
				context: ctx, proof: keyrouting.NewAccountAttemptPreludeProof(w.attempt.Metadata().UpstreamEndpoint, mediaType == "text/event-stream"),
				allowTTFTCancellation: func() bool {
					return w.scope.parent.Err() == nil && (w.nativeContext == nil || w.nativeContext.Err() == nil)
				},
			}
			w.body, resp.Body = body, body
		}
	}
	hasBody := w.body != nil
	w.mu.Unlock()
	if !hasBody {
		w.cleanupNativeContext()
	}
}

func (w *keyAccountWireAttempt) cleanupNativeContext() {
	w.mu.Lock()
	stop, stopGuard, cancel := w.stopNativeWatch, w.stopGuardWatch, w.nativeCancel
	w.stopNativeWatch, w.stopGuardWatch = nil, nil
	w.mu.Unlock()
	if stop != nil {
		stop()
	}
	if stopGuard != nil {
		stopGuard()
	}
	if cancel != nil {
		cancel(keyrouting.ErrAccountAttemptFinished)
	}
}

func (w *keyAccountWireAttempt) bodyReady() bool {
	w.mu.Lock()
	body := w.body
	w.mu.Unlock()
	return body == nil || body.ready()
}

func (w *keyAccountWireAttempt) bodyClosed() bool {
	w.mu.Lock()
	body := w.body
	w.mu.Unlock()
	return body == nil || body.isClosed()
}

func (w *keyAccountWireAttempt) closeBody() error {
	w.mu.Lock()
	body := w.body
	w.mu.Unlock()
	if body == nil {
		return nil
	}
	return body.Close()
}

func (w *keyAccountWireAttempt) retryableFailure() bool {
	w.mu.Lock()
	status, transportErr := w.status, w.transportErr
	w.mu.Unlock()
	return status >= 400 && status <= 599 || status == 0 && transportErr != nil || status >= 200 && status <= 299 && (w.attempt.VerifiedFailure() || w.firstContentCancellationVerified())
}

// Expiry is a deadline fact, not protocol proof. Only this exact wire's typed
// TTFT cancellation plus a complete safe consumed prefix can authorize replay.
// A normal EOF/empty success and a close/native/client cancellation cannot.
func (w *keyAccountWireAttempt) firstContentCancellationVerified() bool {
	w.mu.Lock()
	status, ctx, body, ttftTransportCancellation := w.status, w.context, w.body, w.ttftTransportCancellation
	w.mu.Unlock()
	if !w.attempt.Guard().Expired() || w.scope.parent.Err() != nil ||
		!errors.Is(context.Cause(ctx), keyrouting.ErrAccountAttemptFirstContent) {
		return false
	}
	if status == 0 {
		return ttftTransportCancellation
	}
	return status >= 200 && status <= 299 && body != nil && body.firstContentCancellationVerified()
}

func (w *keyAccountWireAttempt) httpTerminal() keyrouting.AccountAttemptResult {
	w.mu.Lock()
	status, transportErr, nativeContext := w.status, w.transportErr, w.nativeContext
	w.mu.Unlock()
	outcome := keyrouting.OutcomeFailure
	if status == http.StatusTooManyRequests {
		outcome = keyrouting.OutcomeRateLimit
	} else if status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout || status == 522 || status == 524 {
		outcome = keyrouting.OutcomeTimeout
	} else if status < 400 && (keyAccountNativeTimeout(transportErr) || nativeContext != nil && errors.Is(nativeContext.Err(), context.DeadlineExceeded)) {
		outcome = keyrouting.OutcomeTimeout
	}
	return keyrouting.AccountAttemptResult{Outcome: outcome, UpstreamStatusCode: status}
}

// Only typed transport/deadline evidence is a timeout. Provider wording such
// as "timeout", "overload" or "524" in an error message is never sufficient.
func keyAccountNativeTimeout(err error) bool {
	var timeout net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
}

// net/http may return the explicit context cancellation cause instead of
// context.Canceled. Both are typed cancellation evidence, but callers still
// require this exact wire's CancelCause to be the local TTFT deadline.
func keyAccountFirstContentCancellationError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, keyrouting.ErrAccountAttemptFirstContent)
}

func (w *keyAccountWireAttempt) finish(terminal keyrouting.AccountAttemptResult) keyrouting.AccountAttemptSample {
	w.finishOnce.Do(func() {
		w.mu.Lock()
		status, transportErr := w.status, w.transportErr
		w.mu.Unlock()
		// This seam has direct HTTP provenance. Never promote a native parser's
		// synthesized failover code into a response that was never received.
		terminal.UpstreamStatusCode = 0
		if status >= 100 && status <= 599 {
			terminal.UpstreamStatusCode = status
		}
		terminal.VerifiedFailure = terminal.VerifiedFailure || w.attempt.VerifiedFailure()
		terminal.VerifiedFirstContentCancellation = w.firstContentCancellationVerified() && !terminal.UsageKnown &&
			!terminal.SideEffect && !terminal.ResponseCommitted && !terminal.CompleteResponse && !w.attempt.ReplayBlocked()
		if terminal.VerifiedFailure && terminal.Outcome == keyrouting.OutcomeSuccess {
			// Some native parsers intentionally return nil error to preserve
			// partial-usage billing. Their decoded provider error is still a
			// health failure; the completed-result veto must not be lost.
			terminal.CompleteResponse = true
			terminal.Outcome = keyrouting.OutcomeFailure
		}
		if terminal.Outcome != keyrouting.OutcomeSuccess && terminal.Outcome != keyrouting.OutcomeIgnored {
			if native := w.httpTerminal(); native.Outcome != keyrouting.OutcomeFailure || status >= 400 && status <= 599 {
				terminal.Outcome = native.Outcome
			}
			ambiguous := status >= 100 && status < 400 || status == 0 && transportErr == nil
			if ambiguous && !terminal.VerifiedFailure && !terminal.VerifiedFirstContentCancellation {
				w.attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
			}
		}
		sample := w.attempt.Finish(terminal)
		var err error
		if sample.Dispatched && w.scope.opts.ObserveAttempt != nil {
			err = w.scope.opts.ObserveAttempt(w.scope.parent, sample)
		}
		w.mu.Lock()
		w.sample, w.observeErr = sample, err
		w.mu.Unlock()
	})
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.sample
}

func wireObservationErrors(wires []*keyAccountWireAttempt) error {
	var errs []error
	for _, wire := range wires {
		wire.mu.Lock()
		err := wire.observeErr
		wire.mu.Unlock()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type keyAccountWireBody struct {
	io.ReadCloser
	mu                    sync.Mutex
	readMu                sync.Mutex
	closeOnce             sync.Once
	closed                bool
	eof                   bool
	reading               int
	closeErr              error
	onClose               func()
	context               context.Context
	proof                 *keyrouting.AccountAttemptPreludeProof
	closing               bool
	ttftReadCancellation  bool
	allowTTFTCancellation func() bool
}

func (b *keyAccountWireBody) KeyAccountWireContext() context.Context { return b.context }

func (b *keyAccountWireBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return 0, http.ErrBodyReadAfterClose
	}
	b.reading++
	b.mu.Unlock()
	// ProtectUsageReader is a sequential observer. Register queued readers in
	// the cleanup barrier, but never let concurrent native reads race its state.
	b.readMu.Lock()
	defer b.readMu.Unlock()
	b.mu.Lock()
	if b.closed {
		b.reading--
		b.mu.Unlock()
		return 0, http.ErrBodyReadAfterClose
	}
	b.mu.Unlock()
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	if n > 0 && b.proof != nil {
		b.proof.Observe(p[:n])
	}
	b.reading--
	if errors.Is(err, io.EOF) {
		b.eof = true
	}
	if !b.closing && keyAccountFirstContentCancellationError(err) && errors.Is(context.Cause(b.context), keyrouting.ErrAccountAttemptFirstContent) &&
		(b.allowTTFTCancellation == nil || b.allowTTFTCancellation()) {
		b.ttftReadCancellation = true
	}
	b.mu.Unlock()
	return n, err
}

func (b *keyAccountWireBody) Close() error {
	b.closeOnce.Do(func() {
		b.mu.Lock()
		b.closing = true
		b.mu.Unlock()
		err := b.ReadCloser.Close()
		b.mu.Lock()
		b.closeErr, b.closed = err, true
		b.mu.Unlock()
		if b.onClose != nil {
			b.onClose()
		}
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeErr
}

func (b *keyAccountWireBody) firstContentCancellationVerified() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reading == 0 && !b.eof && b.ttftReadCancellation && b.proof.Safe()
}

func (b *keyAccountWireBody) ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reading == 0 && (b.closed || b.eof)
}

func (b *keyAccountWireBody) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed && b.reading == 0 && b.closeErr == nil
}

// Keep this observation aligned with the native billing vocabulary, including
// non-token charges. It observes only; no balances or usage rows are changed.
func KeyAccountForwardResultHasUsage(result any) bool {
	switch r := result.(type) {
	case *OpenAIForwardResult:
		if r == nil {
			return false
		}
		u := r.Usage
		return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheCreationInputTokens > 0 ||
			u.CacheReadInputTokens > 0 || u.ImageInputTokens > 0 || u.ImageOutputTokens > 0 ||
			u.ImageCacheReadTokens > 0 || u.KiroCredits > 0 || r.ImageCount > 0 ||
			r.VideoCount > 0 || r.VideoDurationSeconds > 0 || r.WebSearchCalls > 0 ||
			r.SearchCount > 0 || r.AudioUsage != nil && r.AudioUsage.DurationOrUnits > 0
	case *ForwardResult:
		if r == nil {
			return false
		}
		u := r.Usage
		return u.InputTokens > 0 || u.OutputTokens > 0 || u.CacheCreationInputTokens > 0 ||
			u.CacheReadInputTokens > 0 || u.CacheCreation5mTokens > 0 || u.CacheCreation1hTokens > 0 ||
			u.ImageOutputTokens > 0 || u.KiroCredits > 0 || r.ImageCount > 0 || r.SearchCount > 0 ||
			r.CacheStorageTokenHours > 0 || r.AudioUsage != nil && r.AudioUsage.DurationOrUnits > 0
	case *SystemOneForwardResult:
		return r != nil && KeyAccountForwardResultHasUsage(&r.ForwardResult)
	default:
		return false
	}
}
