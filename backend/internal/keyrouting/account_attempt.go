package keyrouting

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AccountAttemptMetadata is a value snapshot of one real inference dispatch.
// It must not be reconstructed from the last account selected by a handler.
type AccountAttemptMetadata struct {
	Site             string
	AttemptID        string
	APIKeyID         int64
	GroupID          int64
	AccountID        int64
	Model            string
	UpstreamModel    string
	Platform         string
	Endpoint         string
	UpstreamEndpoint string
	PolicyRevision   uint64
}

type AccountAttemptStatusSource string

const (
	AttemptStatusNone     AccountAttemptStatusSource = "none"
	AttemptStatusUpstream AccountAttemptStatusSource = "upstream"
	AttemptStatusClient   AccountAttemptStatusSource = "client"
)

type AccountAttemptCancellationCause string

const (
	AttemptNotCancelled       AccountAttemptCancellationCause = "none"
	AttemptClientCancelled    AccountAttemptCancellationCause = "client_cancelled"
	AttemptFirstContentExpiry AccountAttemptCancellationCause = "first_content_timeout"
	AttemptFinished           AccountAttemptCancellationCause = "finished"
)

var (
	ErrAccountAttemptClientCancelled = errors.New("account attempt client cancelled")
	ErrAccountAttemptFirstContent    = errors.New("account attempt first-content deadline expired")
	ErrAccountAttemptFinished        = errors.New("account attempt finished")
)

// Vetoes are monotonic: a late usage/content observation still forbids replay
// when the timer has already cancelled the detached upstream context.
type AccountAttemptReplayVeto uint32

const (
	AttemptVetoFirstContent AccountAttemptReplayVeto = 1 << iota
	AttemptVetoUsage
	AttemptVetoSideEffect
	AttemptVetoResponseCommitted
	AttemptVetoUnknownProtocol
	AttemptVetoCompleteResponse
	AttemptVetoClientCancelled
	AttemptVetoNativeGuard
)

// AccountAttemptResult describes the native terminal result, not a new billing
// record. UpstreamStatusCode must be an actual HTTP response, not a parser's
// synthetic retry classification. A synthetic client status never becomes an
// exact-status rule sample.
type AccountAttemptResult struct {
	Outcome               RouteOutcomeKind
	UpstreamStatusCode    int
	ClientStatusCode      int
	ClientStatusSynthetic bool
	// VerifiedFailure is proof supplied by a native protocol parser, not by
	// error text, a synthetic status or a generic retryable-error type. It
	// never overrides output, usage, completion or cancellation replay vetoes.
	VerifiedFailure bool
	// VerifiedFirstContentCancellation requires wire-local cancellation and
	// complete no-content protocol evidence. Expiry alone is never proof.
	VerifiedFirstContentCancellation bool
	UsageKnown                       bool
	SideEffect                       bool
	ResponseCommitted                bool
	CompleteResponse                 bool
}

// AccountAttemptSample is frozen once. Later safety vetoes can prevent replay,
// but cannot rewrite the terminal sample or produce a second health event.
type AccountAttemptSample struct {
	Metadata                         AccountAttemptMetadata
	Dispatched                       bool
	Outcome                          RouteOutcomeKind
	StatusCode                       int
	StatusSource                     AccountAttemptStatusSource
	UpstreamStatusCode               int
	ClientStatusCode                 int
	ClientStatusSynthetic            bool
	VerifiedFailure                  bool
	VerifiedFirstContentCancellation bool
	StartedAt                        time.Time
	FinishedAt                       time.Time
	Elapsed                          time.Duration
	FirstContentElapsed              time.Duration
	FirstContentObserved             bool
	FirstContentExpired              bool
	UsageKnown                       bool
	SideEffect                       bool
	ResponseCommitted                bool
	ReplayVeto                       AccountAttemptReplayVeto
	CancellationCause                AccountAttemptCancellationCause
}

// AccountAttempt owns exactly one inference attempt and its cleanup barrier.
// The existing FirstContentGuard is the single shared safety latch for raw
// usage readers, hosted-tool protection and protocol writers. A zero timeout
// disables only the timer, never usage/content replay protection.
type AccountAttempt struct {
	mu              sync.Mutex
	finishOnce      sync.Once
	metadata        AccountAttemptMetadata
	guard           *FirstContentGuard
	parent          context.Context
	child           context.Context
	startedAt       time.Time
	stopClientWatch func() bool
	dispatched      bool
	finished        bool
	cleanupComplete bool
	clientCancelled bool
	verifiedFailure bool
	veto            AccountAttemptReplayVeto
	sample          AccountAttemptSample
}

func NewAccountAttempt(metadata AccountAttemptMetadata, firstContentTimeout time.Duration) *AccountAttempt {
	if metadata.AttemptID == "" {
		metadata.AttemptID = uuid.NewString()
	}
	return &AccountAttempt{metadata: metadata, guard: NewFirstContentGuard(firstContentTimeout)}
}

func (a *AccountAttempt) Metadata() AccountAttemptMetadata {
	if a == nil {
		return AccountAttemptMetadata{}
	}
	return a.metadata
}

// Guard is always present, even without a TTFT rule. Native builders must use
// this guard rather than retaining a preceding account/group guard.
func (a *AccountAttempt) Guard() *FirstContentGuard {
	if a == nil {
		return nil
	}
	return a.guard
}

// Dispatch must be called immediately before the real inference request, after
// account admission and credential preparation. Repeated calls return the same
// child and do not reset the deadline. Parent cancellation is observed for
// replay eligibility while the native usage-drain path owns upstream cleanup.
func (a *AccountAttempt) Dispatch(parent context.Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	if a == nil {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.child != nil {
		return a.child
	}
	a.parent = parent
	if a.finished || parent.Err() != nil {
		if parent.Err() != nil {
			a.clientCancelled = true
			a.veto |= AttemptVetoClientCancelled
			a.guard.PreventReplay()
		}
		ctx, cancel := context.WithCancel(WithAccountAttempt(context.WithoutCancel(parent), a))
		cancel()
		a.child = ctx
		return ctx
	}
	a.dispatched = true
	a.startedAt = time.Now()
	a.child = a.guard.Detach(WithAccountAttempt(parent, a))
	a.stopClientWatch = context.AfterFunc(parent, a.ObserveClientCancellation)
	return a.child
}

func (a *AccountAttempt) ObserveFirstContent() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.veto |= AttemptVetoFirstContent
	a.guard.PreventReplay()
	if !a.dispatched || a.finished {
		return false
	}
	// Content is irrevocable even if expiry just won; do not drop a complete
	// late result and launch duplicate potentially billable inference work.
	return a.guard.Observe()
}

func (a *AccountAttempt) ObserveUsage() { a.PreventReplay(AttemptVetoUsage) }

func (a *AccountAttempt) ObserveSideEffect() { a.PreventReplay(AttemptVetoSideEffect) }

// MarkVerifiedFailure is an explicit native-parser proof that this dispatched
// attempt returned a protocol error, not an ambiguous or incomplete success.
// It grants no replay permission on its own and cannot reopen a frozen sample.
func (a *AccountAttempt) MarkVerifiedFailure() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.dispatched || a.finished {
		return false
	}
	a.verifiedFailure = true
	return true
}

func (a *AccountAttempt) VerifiedFailure() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.verifiedFailure
}

func (a *AccountAttempt) PreventReplay(reason AccountAttemptReplayVeto) {
	if a == nil {
		return
	}
	if reason == 0 {
		reason = AttemptVetoNativeGuard
	}
	a.mu.Lock()
	a.veto |= reason
	a.guard.PreventReplay()
	a.mu.Unlock()
}

// ObserveClientCancellation never cancels native usage drain. It only prevents
// another attempt and excludes this client-aborted operation from health rules.
func (a *AccountAttempt) ObserveClientCancellation() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.clientCancelled = true
	a.veto |= AttemptVetoClientCancelled
	a.guard.PreventReplay()
	a.mu.Unlock()
}

func (a *AccountAttempt) ReplayBlocked() bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.replayBlockedLocked()
}

func (a *AccountAttempt) replayBlockedLocked() bool {
	return a.veto != 0 || a.clientCancelled || (a.parent != nil && a.parent.Err() != nil) || a.guard.ReplayBlocked()
}

// CancellationCause is authoritative: FirstContentGuard intentionally uses
// context.WithCancel for native compatibility, so ctx.Err()/context.Cause alone
// cannot distinguish a Key-local TTFT expiry from client cancellation.
func (a *AccountAttempt) CancellationCause() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch a.cancellationCauseLocked() {
	case AttemptClientCancelled:
		return ErrAccountAttemptClientCancelled
	case AttemptFirstContentExpiry:
		return ErrAccountAttemptFirstContent
	case AttemptFinished:
		return ErrAccountAttemptFinished
	default:
		return nil
	}
}

func (a *AccountAttempt) cancellationCauseLocked() AccountAttemptCancellationCause {
	if a.clientCancelled || (a.parent != nil && a.parent.Err() != nil) {
		return AttemptClientCancelled
	}
	if a.guard.Expired() {
		return AttemptFirstContentExpiry
	}
	if a.finished {
		return AttemptFinished
	}
	return AttemptNotCancelled
}

// Finish is the sole terminal sample producer. It must run after Forward has
// returned; the caller must then acknowledge native slot/reservation cleanup
// with MarkCleanupComplete before CanStartNext can permit another dispatch.
func (a *AccountAttempt) Finish(result AccountAttemptResult) AccountAttemptSample {
	if a == nil {
		return AccountAttemptSample{Outcome: OutcomeIgnored, StatusSource: AttemptStatusNone}
	}
	a.finishOnce.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.verifiedFailure = a.verifiedFailure || result.VerifiedFailure
		if result.UsageKnown {
			a.veto |= AttemptVetoUsage
		}
		if result.SideEffect {
			a.veto |= AttemptVetoSideEffect
		}
		if result.ResponseCommitted {
			a.veto |= AttemptVetoResponseCommitted
		}
		if result.CompleteResponse || result.Outcome == OutcomeSuccess {
			a.veto |= AttemptVetoCompleteResponse
		}
		verifiedFirstContentCancellation := result.VerifiedFirstContentCancellation && a.guard.Expired() &&
			a.cancellationCauseLocked() == AttemptFirstContentExpiry && a.veto == 0 && !a.guard.ReplayBlocked()
		if result.Outcome != OutcomeSuccess && result.Outcome != OutcomeIgnored &&
			result.UpstreamStatusCode >= 100 && result.UpstreamStatusCode < 400 &&
			!a.verifiedFailure && !verifiedFirstContentCancellation {
			a.veto |= AttemptVetoUnknownProtocol
		}
		if a.veto != 0 {
			a.guard.PreventReplay()
		}
		if a.guard.ReplayBlocked() && a.veto == 0 {
			a.veto |= AttemptVetoNativeGuard
		}
		finishedAt := time.Now()
		elapsed, firstContent := a.guard.FirstContentElapsed()
		outcome := result.Outcome
		switch outcome {
		case OutcomeSuccess, OutcomeFailure, OutcomeTimeout, OutcomeRateLimit, OutcomeIgnored:
		default:
			outcome = OutcomeIgnored
		}
		cause := a.cancellationCauseLocked()
		if !a.dispatched || cause == AttemptClientCancelled {
			outcome = OutcomeIgnored
			if cause == AttemptClientCancelled {
				a.veto |= AttemptVetoClientCancelled
				a.guard.PreventReplay()
			}
		} else if a.guard.Expired() && outcome != OutcomeSuccess {
			outcome = OutcomeTimeout
		}
		sample := AccountAttemptSample{
			Metadata: a.metadata, Dispatched: a.dispatched, Outcome: outcome,
			StatusSource: AttemptStatusNone, UpstreamStatusCode: result.UpstreamStatusCode,
			ClientStatusCode: result.ClientStatusCode, ClientStatusSynthetic: result.ClientStatusSynthetic,
			VerifiedFailure:                  a.verifiedFailure,
			VerifiedFirstContentCancellation: verifiedFirstContentCancellation,
			StartedAt:                        a.startedAt, FinishedAt: finishedAt,
			FirstContentElapsed: elapsed, FirstContentObserved: firstContent, FirstContentExpired: a.guard.Expired(),
			UsageKnown: a.veto&AttemptVetoUsage != 0, SideEffect: a.veto&AttemptVetoSideEffect != 0,
			ResponseCommitted: a.veto&AttemptVetoResponseCommitted != 0,
			ReplayVeto:        a.veto, CancellationCause: cause,
		}
		if a.dispatched {
			sample.Elapsed = finishedAt.Sub(a.startedAt)
		}
		if a.dispatched && outcome != OutcomeIgnored {
			switch {
			case validAttemptHTTPStatus(result.UpstreamStatusCode):
				sample.StatusCode, sample.StatusSource = result.UpstreamStatusCode, AttemptStatusUpstream
			case validAttemptHTTPStatus(result.ClientStatusCode) && !result.ClientStatusSynthetic && !sample.FirstContentExpired:
				sample.StatusCode, sample.StatusSource = result.ClientStatusCode, AttemptStatusClient
			}
		}
		a.sample, a.finished = sample, true
		if a.stopClientWatch != nil {
			a.stopClientWatch()
		}
		a.guard.Finish()
	})
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sample
}

func validAttemptHTTPStatus(status int) bool { return status >= 100 && status <= 599 }

// MarkCleanupComplete acknowledges native release functions have all returned.
// Calling it before Finish is deliberately ignored: premature acknowledgement
// must not make a later, still-running Forward eligible for overlapping replay.
func (a *AccountAttempt) MarkCleanupComplete() {
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.finished {
		a.cleanupComplete = true
	}
	a.mu.Unlock()
}

func (a *AccountAttempt) CanStartNext() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dispatched && a.finished && a.cleanupComplete && a.sample.Outcome != OutcomeSuccess && a.sample.Outcome != OutcomeIgnored && !a.replayBlockedLocked()
}

type accountAttemptContextKey struct{}

// WithAccountAttempt binds values only. It neither starts a timer nor records
// dispatch; this allows preparation and admission to remain outside TTFT.
func WithAccountAttempt(ctx context.Context, attempt *AccountAttempt) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, accountAttemptContextKey{}, attempt)
	if attempt == nil {
		return ctx
	}
	return WithFirstContentGuard(ctx, attempt.Guard())
}

func GetAccountAttempt(ctx context.Context) *AccountAttempt {
	if ctx == nil {
		return nil
	}
	attempt, _ := ctx.Value(accountAttemptContextKey{}).(*AccountAttempt)
	return attempt
}
