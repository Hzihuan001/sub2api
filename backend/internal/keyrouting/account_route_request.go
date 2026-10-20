package keyrouting

import (
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrAccountRouteAttemptBudget = errors.New("key account route attempt budget exhausted")
	ErrAccountRouteTimeBudget    = errors.New("key account route time budget exhausted")
	ErrAccountRouteRepeatedPath  = errors.New("key account route already failed on this upstream path in another group")
	ErrAccountRouteInvalidPath   = errors.New("invalid key account route upstream path")
)

// AccountRouteFingerprint identifies an actual inference path, not a public
// alias or a price group. It is deliberately distinct from the historical
// health identity: the latter includes GroupID; this request-local identity
// prevents a second group from sending the exact same failed inference again.
type AccountRouteFingerprint struct {
	AccountID     int64
	UpstreamModel string
	Platform      string
	Endpoint      string
}

// AccountRouteBudget is shared by all groups in ONE inbound request. It has
// no connection to persisted counters, account status, or billing. Call
// TryDispatch only at an actual inference-dispatch boundary, after admission
// and resource acquisition; do not spend attempts for model listing, queueing,
// token refresh, or speculative readiness checks.
type AccountRouteBudget struct {
	mu          sync.Mutex
	now         func() time.Time
	started     time.Time
	maxElapsed  time.Duration
	maxAttempts int
	dispatched  int
	failed      map[AccountRouteFingerprint]int64
}

// NewAccountRouteBudget accepts the coordinator's existing retry/time limits.
// It does not silently increase native per-account or per-group retry limits.
// Both bounds are mandatory so re-entering a native handler cannot reset the
// whole request's budget and cause an unbounded cross-group loop.
func NewAccountRouteBudget(maxAttempts int, maxElapsed time.Duration) (*AccountRouteBudget, error) {
	return newAccountRouteBudget(maxAttempts, maxElapsed, time.Now)
}

func newAccountRouteBudget(maxAttempts int, maxElapsed time.Duration, now func() time.Time) (*AccountRouteBudget, error) {
	if maxAttempts < 1 || maxAttempts > 4096 || maxElapsed <= 0 || now == nil {
		return nil, errors.New("invalid key account route budget limits")
	}
	return &AccountRouteBudget{
		now: now, started: now(), maxAttempts: maxAttempts, maxElapsed: maxElapsed,
		failed: make(map[AccountRouteFingerprint]int64),
	}, nil
}

// CanDispatch is a read-only hint. TryDispatch repeats this check atomically
// at dispatch time. Repeated attempts on the same group remain the native
// retry loop's decision; revisiting that failed path through another group is
// disallowed even though the historical account cooldown is group-scoped.
func (b *AccountRouteBudget) CanDispatch(groupID int64, path AccountRouteFingerprint) error {
	path, err := normalizeAccountRouteFingerprint(path)
	if b == nil || groupID <= 0 || err != nil {
		return ErrAccountRouteInvalidPath
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.canDispatchLocked(groupID, path)
}

// CanPlanAttempts is a read-only preflight, not a dispatch permit. A proactive
// TTFT timer needs room for both the current send and one possible replacement;
// otherwise it could cancel the last send the request is allowed to make.
// Final TryDispatch still checks the live count and deadline atomically.
func (b *AccountRouteBudget) CanPlanAttempts(required int) error {
	return b.CanPlanAttemptsWithin(required, 0)
}

// CanPlanAttemptsWithin additionally requires the prospective timer to expire
// strictly before the whole-request deadline. It still reserves nothing: the
// eventual replacement must pass the live dispatch boundary independently.
func (b *AccountRouteBudget) CanPlanAttemptsWithin(required int, delay time.Duration) error {
	if b == nil || required < 1 || delay < 0 {
		return errors.New("invalid key account route planned attempt count")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.now().Add(delay).Before(b.started.Add(b.maxElapsed)) {
		return ErrAccountRouteTimeBudget
	}
	if required > b.maxAttempts-b.dispatched {
		return ErrAccountRouteAttemptBudget
	}
	return nil
}

// FailedAccountInOtherGroup is a deliberately conservative TTFT-probe hint.
// A probe without the final native mapping cannot prove that another group's
// occurrence of this account is a distinct path. Excluding it from that probe
// may leave a timer disabled, but must NEVER be used by actual scheduling:
// CanDispatch/TryDispatch continue to compare the full mapped fingerprint.
func (b *AccountRouteBudget) FailedAccountInOtherGroup(groupID, accountID int64) bool {
	if b == nil || groupID <= 0 || accountID <= 0 {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for path, originalGroup := range b.failed {
		if path.AccountID == accountID && originalGroup != groupID {
			return true
		}
	}
	return false
}

func (b *AccountRouteBudget) TryDispatch(groupID int64, path AccountRouteFingerprint) error {
	path, err := normalizeAccountRouteFingerprint(path)
	if b == nil || groupID <= 0 || err != nil {
		return ErrAccountRouteInvalidPath
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.canDispatchLocked(groupID, path); err != nil {
		return err
	}
	b.dispatched++
	return nil
}

// RecordFailure runs only after a real inference attempt has failed. Local
// validation, queue/admission failures and store errors must not be recorded.
// One fingerprint can only have been dispatched through its original group:
// TryDispatch rejects a later group once the first failure is known.
func (b *AccountRouteBudget) RecordFailure(groupID int64, path AccountRouteFingerprint) {
	path, err := normalizeAccountRouteFingerprint(path)
	if b == nil || groupID <= 0 || err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.failed[path]; !exists && len(b.failed) < b.maxAttempts {
		b.failed[path] = groupID
	}
}

func (b *AccountRouteBudget) Dispatched() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dispatched
}

func (b *AccountRouteBudget) canDispatchLocked(groupID int64, path AccountRouteFingerprint) error {
	if !b.now().Before(b.started.Add(b.maxElapsed)) {
		return ErrAccountRouteTimeBudget
	}
	if b.dispatched >= b.maxAttempts {
		return ErrAccountRouteAttemptBudget
	}
	if originalGroup, failed := b.failed[path]; failed && originalGroup != groupID {
		return ErrAccountRouteRepeatedPath
	}
	return nil
}

func normalizeAccountRouteFingerprint(path AccountRouteFingerprint) (AccountRouteFingerprint, error) {
	path.UpstreamModel = strings.TrimSpace(path.UpstreamModel)
	path.Platform = strings.ToLower(strings.TrimSpace(path.Platform))
	path.Endpoint = strings.TrimSpace(path.Endpoint)
	// Model identifiers and paths may be case-sensitive. Never fold their
	// casing to make different real upstream mappings appear identical.
	if path.AccountID <= 0 || path.UpstreamModel == "" || len(path.UpstreamModel) > 1024 ||
		path.Platform == "" || len(path.Platform) > 64 || path.Endpoint == "" || len(path.Endpoint) > 256 {
		return AccountRouteFingerprint{}, ErrAccountRouteInvalidPath
	}
	return path, nil
}
