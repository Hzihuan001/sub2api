package keyrouting

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// RouteFailureIdentity scopes a failure counter to one API key, configured
// group, requested model, and endpoint. Keeping the model and endpoint in the
// identity is intentional: a noisy endpoint must not make an otherwise
// healthy model on the same group skip to a backup.
type RouteFailureIdentity struct {
	APIKeyID int64
	GroupID  int64
	Model    string
	Endpoint string
}

// RouteOutcomeKind is one final group result, never an individual account
// retry. Ignored outcomes (client cancellation, validation failures) do not
// affect rolling samples or consecutive failure counts.
type RouteOutcomeKind string

const (
	OutcomeSuccess   RouteOutcomeKind = "success"
	OutcomeFailure   RouteOutcomeKind = "failure"
	OutcomeTimeout   RouteOutcomeKind = "timeout"
	OutcomeRateLimit RouteOutcomeKind = "rate_limit"
	OutcomeIgnored   RouteOutcomeKind = "ignored"
)

// RouteFailureResult describes the outcome of recording one upstream error.
// Triggered is true when this error reached a configured threshold or when a
// matching cooldown was already active. The caller decides whether a backup
// group exists; the tracker does not perform routing or retries.
type RouteFailureResult struct {
	Triggered     bool
	RuleIndex     int
	Count         int
	CooldownUntil time.Time
}

type routeFailureKey struct {
	identity RouteFailureIdentity
	rule     string
	status   int
}

const (
	failureTrackerMaxIdentities = 10000
	failureTrackerMaxCounters   = 100000
	failureTrackerIdleTTL       = 24 * time.Hour
	failureTrackerSweepInterval = time.Minute
)

type routeFailureIdentityState struct {
	lastSeen time.Time
	keys     map[routeFailureKey]struct{}
}

// FailureTracker keeps short-lived rolling counters in process memory. It is
// deliberately independent from account scheduling: recording a failure can
// only cause a key/group candidate to be skipped by the caller. A future
// multi-instance implementation can replace this type behind the same small
// API with a Redis-backed implementation.
type FailureTracker struct {
	mu        sync.Mutex
	now       func() time.Time
	events    map[routeFailureKey][]time.Time
	outcomes  map[routeFailureKey][]RouteOutcomeKind
	cooldowns map[routeFailureKey]time.Time
	// The index bounds arbitrary model/endpoint cardinality and keeps normal
	// pruning local to one identity instead of scanning every route on every
	// request. Limits are fail-open: only new counters are dropped, never API
	// requests or counters already in use.
	identities        map[RouteFailureIdentity]*routeFailureIdentityState
	counterCount      int
	lastSweep         time.Time
	lastCapacitySweep time.Time
	maxIdentities     int
	maxCounters       int
	idleTTL           time.Duration
	sweepInterval     time.Duration
}

// NewFailureTracker creates a tracker using time.Now. The optional clock is
// useful for deterministic tests and should return a non-zero UTC time.
func NewFailureTracker(clock ...func() time.Time) *FailureTracker {
	now := time.Now
	if len(clock) > 0 && clock[0] != nil {
		now = clock[0]
	}
	return &FailureTracker{
		now:        now,
		events:     make(map[routeFailureKey][]time.Time),
		outcomes:   make(map[routeFailureKey][]RouteOutcomeKind),
		cooldowns:  make(map[routeFailureKey]time.Time),
		identities: make(map[RouteFailureIdentity]*routeFailureIdentityState),
	}
}

// IsCooling reports whether any currently configured rule has put this
// key/group/model/endpoint on cooldown. It does not require a status code:
// once a rule fires, all requests for that candidate are sent to the next
// eligible group until the configured cooldown expires.
func (t *FailureTracker) IsCooling(identity RouteFailureIdentity, policy RouteFailurePolicy) (bool, time.Time) {
	if t == nil {
		return false, time.Time{}
	}
	identity = normalizeIdentity(identity)
	now := t.nowTime()
	active := policyKeys(identity, policy)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	t.touchIdentityLocked(identity, now)
	// Remove expired counters and rule revisions that are no longer configured.
	t.pruneIdentityLocked(identity, active, now)
	var latest time.Time
	if state := t.identities[identity]; state != nil {
		for key := range state.keys {
			if until := t.cooldowns[key]; now.Before(until) && (latest.IsZero() || until.After(latest)) {
				latest = until
			}
		}
	}
	return !latest.IsZero(), latest
}

// Record adds one upstream status to all matching configured rules. Counters
// for separate status codes remain independent even if they share a rule.
// Invalid/empty rules are ignored; configuration validation remains the
// responsibility of the admin/API layer and runtime must fail open when it
// receives malformed stale data.
func (t *FailureTracker) Record(identity RouteFailureIdentity, statusCode int, policy RouteFailurePolicy) RouteFailureResult {
	result := RouteFailureResult{RuleIndex: -1}
	if t == nil || statusCode < 100 || statusCode > 599 {
		return result
	}
	identity = normalizeIdentity(identity)
	now := t.nowTime()
	active := policyKeys(identity, policy)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	t.touchIdentityLocked(identity, now)
	t.pruneIdentityLocked(identity, active, now)
	if cooling := t.coolingResultLocked(identity, policy, now); cooling.Triggered {
		return cooling
	}

	for index, rule := range policy.Rules {
		if !validRouteFailureRule(rule) || rule.ruleType() != FailureRuleStatusCode || !containsStatus(rule.StatusCodes, statusCode) {
			continue
		}
		key := routeFailureKey{identity: identity, rule: routeFailureRuleKey(rule), status: statusCode}
		if !t.ensureCounterLocked(key, now) {
			continue
		}
		if until := t.cooldowns[key]; now.Before(until) {
			if !result.Triggered || until.After(result.CooldownUntil) {
				result = RouteFailureResult{Triggered: true, RuleIndex: index, CooldownUntil: until}
			}
			continue
		}
		cutoff := now.Add(-time.Duration(rule.WindowSeconds) * time.Second)
		events := t.events[key]
		kept := events[:0]
		for _, event := range events {
			if event.After(cutoff) {
				kept = append(kept, event)
			}
		}
		kept = append(kept, now)
		if len(kept) < rule.Threshold {
			t.events[key] = kept
			continue
		}
		until := now.Add(time.Duration(rule.CooldownSeconds) * time.Second)
		t.cooldowns[key] = until
		// Events before a cooldown cannot contribute to a later cooldown. Drop
		// them now to keep memory bounded for hot keys.
		delete(t.events, key)
		if !result.Triggered || until.After(result.CooldownUntil) {
			result = RouteFailureResult{Triggered: true, RuleIndex: index, Count: len(kept), CooldownUntil: until}
		}
	}
	return result
}

// RecordOutcome records a single completed group traversal. A group's own
// account retries must be exhausted before the caller records failure. Real
// successes occupy sample slots and reset consecutive failures; ignored
// outcomes do neither. Status-code counters are exclusively updated by Record.
func (t *FailureTracker) RecordOutcome(identity RouteFailureIdentity, outcome RouteOutcomeKind, policy RouteFailurePolicy) RouteFailureResult {
	result := RouteFailureResult{RuleIndex: -1}
	if t == nil {
		return result
	}
	switch outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomeTimeout, OutcomeRateLimit, OutcomeIgnored:
	default:
		return result
	}
	identity = normalizeIdentity(identity)
	now := t.nowTime()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	t.touchIdentityLocked(identity, now)
	t.pruneIdentityLocked(identity, policyKeys(identity, policy), now)
	if outcome == OutcomeIgnored {
		return result
	}
	// An in-flight success always resets consecutive failures, but cannot
	// cancel a cooldown already established by another completed request.
	if outcome == OutcomeSuccess {
		for _, rule := range policy.Rules {
			if rule.ruleType() == FailureRuleConsecutiveFailures {
				key := routeFailureKey{identity: identity, rule: routeFailureRuleKey(rule)}
				delete(t.outcomes, key)
				t.releaseEmptyCounterLocked(key)
			}
		}
	}
	if cooling := t.coolingResultLocked(identity, policy, now); cooling.Triggered {
		if outcome != OutcomeSuccess {
			return cooling
		}
		return result
	}
	for index, rule := range policy.Rules {
		kind := rule.ruleType()
		if !validRouteFailureRule(rule) || kind == FailureRuleStatusCode || kind == FailureRuleTTFT {
			continue
		}
		key := routeFailureKey{identity: identity, rule: routeFailureRuleKey(rule)}
		if kind == FailureRuleConsecutiveFailures && outcome == OutcomeSuccess {
			continue
		}
		if !t.ensureCounterLocked(key, now) {
			continue
		}
		samples := append(t.outcomes[key], outcome)
		count := len(samples)
		if kind != FailureRuleConsecutiveFailures {
			if len(samples) > rule.SampleSize {
				copy(samples, samples[len(samples)-rule.SampleSize:])
				samples = samples[:rule.SampleSize]
			}
			count = 0
			for _, sample := range samples {
				if (kind == FailureRuleTimeout && sample == OutcomeTimeout) || (kind == FailureRuleRateLimit && sample == OutcomeRateLimit) || (kind == FailureRuleSLASuccessRate && sample != OutcomeSuccess) {
					count++
				}
			}
		}
		triggered := count >= rule.Threshold
		if kind == FailureRuleSLASuccessRate {
			// SLA is a complete recent-N window, unlike count-based sample
			// rules which may trigger before filling the window. Use integer
			// cross multiplication so exactly the threshold rate stays healthy.
			triggered = len(samples) == rule.SampleSize && (len(samples)-count)*100 < rule.Threshold*rule.SampleSize
		}
		if !triggered {
			t.outcomes[key] = samples
			continue
		}
		until := now.Add(time.Duration(rule.CooldownSeconds) * time.Second)
		t.cooldowns[key] = until
		delete(t.outcomes, key)
		if !result.Triggered || until.After(result.CooldownUntil) {
			result = RouteFailureResult{Triggered: true, RuleIndex: index, Count: count, CooldownUntil: until}
		}
	}
	return result
}

func (t *FailureTracker) initLocked() {
	if t.events == nil {
		t.events = make(map[routeFailureKey][]time.Time)
	}
	if t.outcomes == nil {
		t.outcomes = make(map[routeFailureKey][]RouteOutcomeKind)
	}
	if t.cooldowns == nil {
		t.cooldowns = make(map[routeFailureKey]time.Time)
	}
	if t.identities == nil {
		t.identities = make(map[RouteFailureIdentity]*routeFailureIdentityState)
	}
	if t.maxIdentities <= 0 {
		t.maxIdentities = failureTrackerMaxIdentities
	}
	if t.maxCounters <= 0 {
		t.maxCounters = failureTrackerMaxCounters
	}
	if t.idleTTL <= 0 {
		t.idleTTL = failureTrackerIdleTTL
	}
	if t.sweepInterval <= 0 {
		t.sweepInterval = failureTrackerSweepInterval
	}
}

func (t *FailureTracker) touchIdentityLocked(identity RouteFailureIdentity, now time.Time) {
	t.sweepLocked(now, false)
	state := t.identities[identity]
	if state == nil {
		return // A read-only availability check must not allocate new identities.
	}
	if !now.Before(state.lastSeen.Add(t.idleTTL)) {
		t.deleteIdentityLocked(identity)
		return
	}
	state.lastSeen = now
}

func (t *FailureTracker) ensureCounterLocked(key routeFailureKey, now time.Time) bool {
	state := t.identities[key.identity]
	if state != nil {
		if _, exists := state.keys[key]; exists {
			return true
		}
	}
	if t.counterCount >= t.maxCounters || (state == nil && len(t.identities) >= t.maxIdentities) {
		t.sweepLocked(now, true)
		state = t.identities[key.identity]
		if t.counterCount >= t.maxCounters || (state == nil && len(t.identities) >= t.maxIdentities) {
			return false
		}
	}
	if state == nil {
		state = &routeFailureIdentityState{lastSeen: now, keys: make(map[routeFailureKey]struct{})}
		t.identities[key.identity] = state
	}
	state.lastSeen = now
	state.keys[key] = struct{}{}
	t.counterCount++
	return true
}

func (t *FailureTracker) releaseEmptyCounterLocked(key routeFailureKey) {
	if len(t.events[key]) > 0 || len(t.outcomes[key]) > 0 || !t.cooldowns[key].IsZero() {
		return
	}
	if state := t.identities[key.identity]; state != nil {
		if _, exists := state.keys[key]; exists {
			delete(state.keys, key)
			t.counterCount--
		}
		if len(state.keys) == 0 {
			delete(t.identities, key.identity)
		}
	}
}

func (t *FailureTracker) deleteCounterLocked(key routeFailureKey) {
	delete(t.events, key)
	delete(t.outcomes, key)
	delete(t.cooldowns, key)
	t.releaseEmptyCounterLocked(key)
}

func (t *FailureTracker) deleteIdentityLocked(identity RouteFailureIdentity) {
	state := t.identities[identity]
	if state == nil {
		return
	}
	for key := range state.keys {
		delete(t.events, key)
		delete(t.outcomes, key)
		delete(t.cooldowns, key)
		t.counterCount--
	}
	delete(t.identities, identity)
}

func (t *FailureTracker) sweepLocked(now time.Time, force bool) {
	// Repeated high-cardinality requests at capacity must not turn eviction
	// into an O(all identities) operation on every request.
	if force && !t.lastCapacitySweep.IsZero() && now.Before(t.lastCapacitySweep.Add(t.sweepInterval)) {
		return
	}
	if !force && !t.lastSweep.IsZero() && now.Before(t.lastSweep.Add(t.sweepInterval)) {
		return
	}
	if force {
		t.lastCapacitySweep = now
	}
	t.lastSweep = now
	for identity, state := range t.identities {
		if !now.Before(state.lastSeen.Add(t.idleTTL)) {
			t.deleteIdentityLocked(identity)
		} else {
			// Expired cooldowns and status windows need not consume a slot
			// for 24 hours. Recent-N samples retain their exact semantics.
			t.pruneIdentityLocked(identity, state.keys, now)
		}
	}
}

func (t *FailureTracker) coolingResultLocked(identity RouteFailureIdentity, policy RouteFailurePolicy, now time.Time) RouteFailureResult {
	result := RouteFailureResult{RuleIndex: -1}
	for index, rule := range policy.Rules {
		if !validRouteFailureRule(rule) {
			continue
		}
		statuses := rule.StatusCodes
		revision := routeFailureRuleKey(rule)
		if rule.ruleType() != FailureRuleStatusCode {
			statuses = []int{0}
		}
		for _, status := range statuses {
			until := t.cooldowns[routeFailureKey{identity: identity, rule: revision, status: status}]
			if now.Before(until) && (!result.Triggered || until.After(result.CooldownUntil)) {
				result = RouteFailureResult{Triggered: true, RuleIndex: index, CooldownUntil: until}
			}
		}
	}
	return result
}

// Clear removes all counters and cooldowns for one identity. It is useful
// when an API key is deleted or its routing groups are replaced. Clearing is
// optional because policy changes are also isolated by the rule fingerprint.
func (t *FailureTracker) Clear(identity RouteFailureIdentity) {
	if t == nil {
		return
	}
	identity = normalizeIdentity(identity)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.deleteIdentityLocked(identity)
}

// ClearGroup removes route counters and cooldowns for every API key in one
// group. It is called when an administrator changes that group's rules so a
// removed or replaced policy cannot leave stale state behind.
func (t *FailureTracker) ClearGroup(groupID int64) {
	if t == nil || groupID <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for identity := range t.identities {
		if identity.GroupID == groupID {
			t.deleteIdentityLocked(identity)
		}
	}
}

func (t *FailureTracker) nowTime() time.Time {
	if t.now == nil {
		return time.Now()
	}
	return t.now()
}

func normalizeIdentity(identity RouteFailureIdentity) RouteFailureIdentity {
	identity.Model = strings.ToLower(strings.TrimSpace(identity.Model))
	identity.Endpoint = strings.ToLower(strings.TrimSpace(identity.Endpoint))
	return identity
}

func validRouteFailureRule(rule RouteFailureRule) bool {
	return validateRouteFailureRule(rule) == nil
}

func containsStatus(statusCodes []int, wanted int) bool {
	for _, statusCode := range statusCodes {
		if statusCode == wanted {
			return true
		}
	}
	return false
}

func policyKeys(identity RouteFailureIdentity, policy RouteFailurePolicy) map[routeFailureKey]struct{} {
	active := make(map[routeFailureKey]struct{}, len(policy.Rules))
	for _, rule := range policy.Rules {
		if validRouteFailureRule(rule) {
			statuses := rule.StatusCodes
			revision := routeFailureRuleKey(rule)
			if rule.ruleType() != FailureRuleStatusCode {
				statuses = []int{0}
			}
			for _, statusCode := range statuses {
				active[routeFailureKey{identity: identity, rule: revision, status: statusCode}] = struct{}{}
			}
		}
	}
	return active
}

func routeFailureRuleKey(rule RouteFailureRule) string {
	statusCodes := append([]int(nil), rule.StatusCodes...)
	sort.Ints(statusCodes)
	parts := make([]string, 0, len(statusCodes)+5)
	parts = append(parts, rule.ruleType())
	for _, statusCode := range statusCodes {
		parts = append(parts, stringInt(statusCode))
	}
	parts = append(parts, stringInt(rule.Threshold), stringInt(rule.WindowSeconds), stringInt(rule.SampleSize), stringInt(rule.CooldownSeconds), stringInt(rule.FirstContentSeconds))
	return strings.Join(parts, ",")
}

func stringInt(value int) string {
	// Avoid fmt in the hot path and keep the fingerprint unambiguous.
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	i := len(digits)
	for value > 0 {
		i--
		digits[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

func (t *FailureTracker) pruneIdentityLocked(identity RouteFailureIdentity, active map[routeFailureKey]struct{}, now time.Time) {
	state := t.identities[identity]
	if state == nil {
		return
	}
	for key := range state.keys {
		if _, ok := active[key]; !ok {
			t.deleteCounterLocked(key)
			continue
		}
		// Status windows are capped at one hour; semantic recent-N samples
		// are preserved until this identity has been idle for the full TTL.
		if events := t.events[key]; len(events) > 0 {
			kept := events[:0]
			for _, event := range events {
				if event.After(now.Add(-time.Hour)) {
					kept = append(kept, event)
				}
			}
			if len(kept) == 0 {
				delete(t.events, key)
			} else {
				t.events[key] = kept
			}
		}
		if until := t.cooldowns[key]; !until.IsZero() && !now.Before(until) {
			delete(t.cooldowns, key)
		}
		t.releaseEmptyCounterLocked(key)
	}
}
