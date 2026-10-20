package keyrouting

import (
	"context"
	"sync"
	"time"
)

type firstContentGuardContextKey struct{}

// FirstContentGuard owns only one group's upstream attempts. It never cancels
// the inbound request, writes a response, or decides which group to select.
// A zero timeout observes latency without interrupting the last candidate.
type FirstContentGuard struct {
	mu            sync.Mutex
	timeout       time.Duration
	started       time.Time
	firstContent  time.Time
	finishedAt    time.Time
	timer         *time.Timer
	expired       bool
	preventReplay bool
	finished      bool
	children      []context.CancelFunc
}

func NewFirstContentGuard(timeout time.Duration) *FirstContentGuard {
	return &FirstContentGuard{timeout: timeout}
}

func WithFirstContentGuard(ctx context.Context, guard *FirstContentGuard) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(withoutFirstContentGuardProvider(ctx), firstContentGuardContextKey{}, guard)
}

func GetFirstContentGuard(ctx context.Context) *FirstContentGuard {
	if ctx == nil {
		return nil
	}
	if provider := boundFirstContentGuardProvider(ctx); provider != nil {
		return provider.CurrentFirstContentGuard()
	}
	guard, _ := ctx.Value(firstContentGuardContextKey{}).(*FirstContentGuard)
	return guard
}

// Detach preserves request values while giving the guard, not the client or a
// legacy request-builder release callback, ownership of upstream cancellation.
// Repeated account attempts share the first dispatch's deadline.
func (g *FirstContentGuard) Detach(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if g == nil {
		return context.WithoutCancel(ctx)
	}
	child, cancel := context.WithCancel(context.WithoutCancel(ctx))
	g.mu.Lock()
	if g.started.IsZero() && !g.finished {
		g.started = time.Now()
		if g.timeout > 0 && g.firstContent.IsZero() && !g.preventReplay {
			g.timer = time.AfterFunc(g.timeout, g.expire)
		}
	}
	if g.finished || (g.expired && !g.preventReplay) {
		g.mu.Unlock()
		cancel()
		return child
	}
	g.children = append(g.children, cancel)
	g.mu.Unlock()
	return child
}

func (g *FirstContentGuard) expire() {
	g.mu.Lock()
	if g.finished || g.preventReplay || !g.firstContent.IsZero() {
		g.mu.Unlock()
		return
	}
	g.expired = true
	children := append([]context.CancelFunc(nil), g.children...)
	g.mu.Unlock()
	for _, cancel := range children {
		cancel()
	}
}

// Observe arbitrates semantic output against expiry. False means the deadline
// won and the caller must not commit buffered output. Known billable usage can
// override replay eligibility via PreventReplay, preserving native settlement.
func (g *FirstContentGuard) Observe() bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.finished || (g.expired && !g.preventReplay) {
		return false
	}
	if g.firstContent.IsZero() {
		g.firstContent = time.Now()
	}
	if g.timer != nil {
		g.timer.Stop()
	}
	return true
}

// PreventReplay disarms proactive cancellation as soon as known usage or an
// irreversible effect is seen. Calling it after expiry cannot undo transport
// cancellation, but still forbids launching a second billable attempt.
func (g *FirstContentGuard) PreventReplay() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.preventReplay = true
	if g.timer != nil {
		g.timer.Stop()
	}
	g.mu.Unlock()
}

func (g *FirstContentGuard) Expired() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.expired
}

func (g *FirstContentGuard) ReplaySafe() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.expired && !g.preventReplay && g.firstContent.IsZero()
}

// ReplayBlocked is the final cross-group replay veto, independent of the
// proactive deadline. Known usage or semantic output is irrevocable even when
// a native parser has not written anything and its error is otherwise retryable.
// An ordinary pre-output failure may still switch before the deadline expires.
func (g *FirstContentGuard) ReplayBlocked() bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.preventReplay || !g.firstContent.IsZero()
}

func (g *FirstContentGuard) FirstContentElapsed() (time.Duration, bool) {
	if g == nil {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started.IsZero() || g.firstContent.IsZero() {
		return 0, false
	}
	return g.firstContent.Sub(g.started), true
}

// AttemptElapsed includes dispatched attempts that failed before producing
// content. Local admission failures, which never called Detach, are not samples.
func (g *FirstContentGuard) AttemptElapsed() (time.Duration, bool) {
	if g == nil {
		return 0, false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started.IsZero() {
		return 0, false
	}
	end := g.firstContent
	if end.IsZero() {
		end = g.finishedAt
	}
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(g.started), true
}

// Finish is called only after the native handler has returned and released its
// slots. It stops the timer and closes any still-live upstream attempt contexts.
func (g *FirstContentGuard) Finish() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.finished {
		g.mu.Unlock()
		return
	}
	g.finished = true
	g.finishedAt = time.Now()
	if g.timer != nil {
		g.timer.Stop()
	}
	children := g.children
	g.children = nil
	g.mu.Unlock()
	for _, cancel := range children {
		cancel()
	}
}
