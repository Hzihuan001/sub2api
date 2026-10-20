package keyrouting

// Account health is a Key-local routing hint, not an account suspension or a
// billing ledger. Native authorization, quota and safety gates always win.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrAccountHealthIdentity       = errors.New("invalid key account health identity")
	ErrAccountHealthAttempt        = errors.New("invalid key account health attempt")
	ErrAccountHealthStaleRevision  = errors.New("stale key account health policy revision")
	ErrAccountHealthPolicyConflict = errors.New("key account health policy changed without a new revision")
	ErrAccountHealthCapacity       = errors.New("key account health storage capacity reached")
	ErrAccountHealthCorrupt        = errors.New("corrupt key account health state")
	ErrAccountHealthBusy           = errors.New("key account health compare-and-swap retries exhausted")
)

// PolicyRevision must be an authoritative monotonic revision, not a policy
// content hash. A -> B -> A must allocate three revisions. Site is a stable
// deployment identifier; Model is the public, pre-mapping requested model.
type AccountHealthIdentity struct {
	Site           string `json:"site"`
	APIKeyID       int64  `json:"api_key_id"`
	GroupID        int64  `json:"group_id"`
	AccountID      int64  `json:"account_id"`
	Model          string `json:"model"`
	Endpoint       string `json:"endpoint"`
	PolicyRevision uint64 `json:"policy_revision"`
}

func (i AccountHealthIdentity) Normalized() AccountHealthIdentity {
	i.Site, i.Model, i.Endpoint = strings.TrimSpace(i.Site), strings.TrimSpace(i.Model), strings.TrimSpace(i.Endpoint)
	return i
}

func (i AccountHealthIdentity) Validate() error {
	i = i.Normalized()
	if i.Site == "" || len(i.Site) > 128 || i.APIKeyID <= 0 || i.GroupID <= 0 || i.AccountID <= 0 ||
		i.Model == "" || len(i.Model) > 256 || i.Endpoint == "" || len(i.Endpoint) > 128 || i.PolicyRevision == 0 {
		return ErrAccountHealthIdentity
	}
	return nil
}

// StorageKey deliberately omits revision: newer revisions fence old in-flight
// completions at the same identity instead of letting them reactivate a policy.
func (i AccountHealthIdentity) StorageKey() string {
	i = i.Normalized()
	i.PolicyRevision = 0
	data, _ := json.Marshal(i)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (i AccountHealthIdentity) KeyScope() string {
	i = i.Normalized()
	data, _ := json.Marshal(struct {
		Site string
		Key  int64
	}{i.Site, i.APIKeyID})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// A final result is submitted once per actual upstream dispatch. FinishedAt
// must be captured once at terminal completion (never regenerated on retry).
// Samples are ordered by this captured timestamp, not Redis arrival order;
// deployment clocks must be synchronized. A minute of positive skew is
// tolerated, but distinct replicas cannot promise a total real-world order.
// Local
// admission failures and client cancellations must be Dispatched=false or
// OutcomeIgnored. TTFT is independent evidence: a local first-content expiry
// is not a provider HTTP status or a provider timeout.
type AccountHealthAttempt struct {
	AttemptID                string
	Dispatched               bool
	StartedAt                time.Time
	FinishedAt               time.Time
	Outcome                  RouteOutcomeKind
	StatusCode               int
	FirstContent             *time.Duration
	FirstContentExpired      bool
	LocalFirstContentTimeout bool
}

type AccountHealthSnapshot struct {
	PolicyRevision uint64
	Cooling        bool
	CooldownUntil  time.Time
	RuleIndex      int
	Count          int
	Accepted       bool
	Duplicate      bool
}

// All errors, including capacity/Redis errors, are returned. Callers fail open
// for this optional hint; an error never means "no schedulable account".
type AccountHealthStore interface {
	Get(context.Context, AccountHealthIdentity) (AccountHealthSnapshot, error)
	Observe(context.Context, AccountHealthIdentity, RouteFailurePolicy, AccountHealthAttempt) (AccountHealthSnapshot, error)
	// Fence is an explicit eager invalidation hook for a newer policy revision.
	// Same-revision calls are idempotent, never a reset that late results undo.
	Fence(context.Context, AccountHealthIdentity) error
}

type AccountHealthOptions struct {
	Clock               func() time.Time
	IdleTTL             time.Duration
	AttemptTTL          time.Duration
	MaxIdentities       int
	MaxIdentitiesPerKey int
	// MaxIdentitiesPerSite bounds all key-local identities belonging to one
	// deployment. Redis enforces this through deterministic bucket quotas so
	// the state and its index remain in one cluster hash slot.
	MaxIdentitiesPerSite int
	SiteBucketCount      int
	MaxAttempts          int
	MaxStatusEvents      int
	MaxPayloadBytes      int
}

func (o AccountHealthOptions) Normalized() AccountHealthOptions {
	if o.Clock == nil {
		o.Clock = time.Now
	}
	if o.IdleTTL <= 0 || o.IdleTTL > 24*time.Hour {
		o.IdleTTL = 24 * time.Hour
	}
	// Keep a live revision fence longer than every allowed cooldown/window.
	if o.IdleTTL < 3*time.Hour {
		o.IdleTTL = 3 * time.Hour
	}
	if o.AttemptTTL <= 0 || o.AttemptTTL > o.IdleTTL {
		o.AttemptTTL = 15 * time.Minute
	}
	if o.AttemptTTL > o.IdleTTL {
		o.AttemptTTL = o.IdleTTL
	}
	if o.MaxIdentities <= 0 || o.MaxIdentities > 100000 {
		o.MaxIdentities = 10000
	}
	if o.MaxIdentitiesPerKey <= 0 || o.MaxIdentitiesPerKey > 4096 {
		o.MaxIdentitiesPerKey = 256
	}
	if o.MaxIdentitiesPerSite <= 0 || o.MaxIdentitiesPerSite > 1000000 {
		o.MaxIdentitiesPerSite = 10000
	}
	if o.SiteBucketCount <= 0 || o.SiteBucketCount > 64 {
		o.SiteBucketCount = 16
	}
	if o.SiteBucketCount > o.MaxIdentitiesPerSite {
		o.SiteBucketCount = o.MaxIdentitiesPerSite
	}
	if o.MaxAttempts <= 0 || o.MaxAttempts > 8192 {
		o.MaxAttempts = 1024
	}
	if o.MaxStatusEvents <= 0 || o.MaxStatusEvents > 10000 {
		o.MaxStatusEvents = 2048
	}
	if o.MaxPayloadBytes <= 0 || o.MaxPayloadBytes > 1024*1024 {
		o.MaxPayloadBytes = 128 * 1024
	}
	return o
}

// SiteScope is stable for a deployment and intentionally omits API key data.
// It is used only as a Redis hash-tag component; health state remains keyed by
// the full identity and never becomes shared between users.
func (i AccountHealthIdentity) SiteScope() string {
	i = i.Normalized()
	sum := sha256.Sum256([]byte(i.Site))
	return hex.EncodeToString(sum[:])
}

// SiteBucket deterministically shards a site's identities while preserving a
// hard aggregate upper bound of SiteBucketCount * floor(MaxIdentitiesPerSite /
// SiteBucketCount). A hot key cannot consume another bucket's quota.
func (i AccountHealthIdentity) SiteBucket(opts AccountHealthOptions) int {
	opts = opts.Normalized()
	sum := sha256.Sum256([]byte(i.KeyScope()))
	return int(sum[0]) % opts.SiteBucketCount
}

// SiteBucketLimit is the per-bucket quota used by the Redis adapter. Keeping
// the site index in the same Redis hash slot as the state/index keys makes the
// admission check atomic on both standalone Redis and Redis Cluster. The
// floor is deliberate: a site may reject a new identity slightly before the
// exact aggregate limit, but it can never exceed MaxIdentitiesPerSite.
func (o AccountHealthOptions) SiteBucketLimit() int {
	o = o.Normalized()
	limit := o.MaxIdentitiesPerSite / o.SiteBucketCount
	if limit < 1 {
		return 1
	}
	return limit
}

// Exported state is a versioned persistence DTO shared by the memory and Redis
// adapters. It is not returned to API callers and contains no credentials.
type AccountHealthState struct {
	Schema          int                   `json:"schema"`
	IdentityKey     string                `json:"identity_key"`
	PolicyRevision  uint64                `json:"policy_revision"`
	PolicyHash      string                `json:"policy_hash,omitempty"`
	UpdatedAt       int64                 `json:"updated_at"`
	Seen            map[string]int64      `json:"seen,omitempty"`
	Samples         []AccountHealthSample `json:"samples,omitempty"`
	StatusEvents    map[int][]int64       `json:"status_events,omitempty"`
	CooldownUntil   int64                 `json:"cooldown_until,omitempty"`
	CooldownRule    int                   `json:"cooldown_rule"`
	CooldownCount   int                   `json:"cooldown_count,omitempty"`
	WindowStartedAt int64                 `json:"window_started_at,omitempty"`
}

type AccountHealthSample struct {
	ID                  string           `json:"id"`
	At                  int64            `json:"at"`
	Outcome             RouteOutcomeKind `json:"outcome"`
	FirstContentNanos   *int64           `json:"first_content_nanos,omitempty"`
	FirstContentExpired bool             `json:"first_content_expired,omitempty"`
}

// PrepareAccountHealth is a pure copy-on-write revision fence/prune operation.
// Store adapters must atomically commit its returned state before using the
// snapshot; it is unsafe to expose a new revision without persisting the fence.
func PrepareAccountHealth(current *AccountHealthState, id AccountHealthIdentity, now time.Time, opts AccountHealthOptions) (*AccountHealthState, AccountHealthSnapshot, error) {
	id, opts = id.Normalized(), opts.Normalized()
	if err := id.Validate(); err != nil {
		return nil, AccountHealthSnapshot{}, err
	}
	if now.IsZero() {
		return nil, AccountHealthSnapshot{}, ErrAccountHealthCorrupt
	}
	if current != nil && (current.Schema != 1 || current.IdentityKey != id.StorageKey() || current.PolicyRevision == 0) {
		return nil, AccountHealthSnapshot{}, ErrAccountHealthCorrupt
	}
	// An expired payload cannot retain outcomes, but its revision still fences
	// lower revisions while physically present in a store.
	if current != nil && id.PolicyRevision < current.PolicyRevision {
		return nil, AccountHealthSnapshot{}, ErrAccountHealthStaleRevision
	}
	var state AccountHealthState
	if current == nil || id.PolicyRevision > current.PolicyRevision || now.UnixMilli()-current.UpdatedAt >= opts.IdleTTL.Milliseconds() {
		state = AccountHealthState{Schema: 1, IdentityKey: id.StorageKey(), PolicyRevision: id.PolicyRevision, CooldownRule: -1}
	} else {
		data, err := json.Marshal(current)
		if err != nil || len(data) > opts.MaxPayloadBytes {
			return nil, AccountHealthSnapshot{}, ErrAccountHealthCorrupt
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, AccountHealthSnapshot{}, ErrAccountHealthCorrupt
		}
	}
	state.UpdatedAt = now.UnixMilli()
	if state.Seen == nil {
		state.Seen = make(map[string]int64)
	}
	for attempt, at := range state.Seen {
		if at <= now.Add(-opts.AttemptTTL).UnixMilli() {
			delete(state.Seen, attempt)
		}
	}
	if state.CooldownUntil <= now.UnixMilli() {
		state.CooldownUntil, state.CooldownRule, state.CooldownCount = 0, -1, 0
	}
	return &state, accountHealthSnapshot(&state, now), nil
}

// AdvanceAccountHealth applies one terminal dispatch atomically. Returning an
// error means the returned state must not be committed by the adapter.
func AdvanceAccountHealth(current *AccountHealthState, id AccountHealthIdentity, policy RouteFailurePolicy, attempt AccountHealthAttempt, now time.Time, opts AccountHealthOptions) (*AccountHealthState, AccountHealthSnapshot, error) {
	opts = opts.Normalized()
	state, snapshot, err := PrepareAccountHealth(current, id, now, opts)
	if err != nil {
		return nil, snapshot, err
	}
	if err := policy.Validate(); err != nil {
		return nil, snapshot, fmt.Errorf("account health policy: %w", err)
	}
	hash := accountHealthPolicyHash(policy)
	if state.PolicyHash != "" && state.PolicyHash != hash {
		return nil, snapshot, ErrAccountHealthPolicyConflict
	}
	state.PolicyHash = hash
	if err := validateAccountHealthAttempt(attempt, now); err != nil {
		return nil, snapshot, err
	}
	if attempt.LocalFirstContentTimeout {
		attempt.Outcome = OutcomeFailure
	}
	if !attempt.Dispatched || attempt.Outcome == OutcomeIgnored {
		return state, snapshot, nil
	}
	if _, found := state.Seen[attempt.AttemptID]; found {
		snapshot.Duplicate = true
		return state, snapshot, nil
	}
	if len(state.Seen) >= opts.MaxAttempts {
		return nil, snapshot, ErrAccountHealthCapacity
	}
	finished := attempt.FinishedAt
	// Expired dedup IDs cannot be counted again by replaying their original
	// terminal event after the Seen TTL. Long-running requests are unaffected:
	// this bounds time since completion, not time since dispatch.
	if !finished.After(now.Add(-opts.AttemptTTL)) {
		return state, snapshot, nil
	}
	state.Seen[attempt.AttemptID] = now.UnixMilli()
	snapshot.Accepted = true
	// In-flight results cannot reopen or extend an existing cooldown. A fresh
	// post-cooldown attempt starts a fresh rule window, not old hot samples.
	if snapshot.Cooling || len(policy.Rules) == 0 {
		return state, snapshot, nil
	}
	// A delayed completion from before the last cooldown must not repopulate
	// the fresh window after it expires. StartedAt is supplied by the adapter.
	if state.WindowStartedAt > 0 && (finished.UnixMilli() <= state.WindowStartedAt || (!attempt.StartedAt.IsZero() && attempt.StartedAt.UnixMilli() < state.WindowStartedAt)) {
		return state, snapshot, nil
	}
	sample := AccountHealthSample{ID: attempt.AttemptID, At: finished.UnixNano(), Outcome: attempt.Outcome, FirstContentExpired: attempt.FirstContentExpired}
	if attempt.FirstContent != nil {
		value := int64(*attempt.FirstContent)
		sample.FirstContentNanos = &value
	}
	state.Samples = append(state.Samples, sample)
	sort.Slice(state.Samples, func(i, j int) bool {
		if state.Samples[i].At == state.Samples[j].At {
			return state.Samples[i].ID < state.Samples[j].ID
		}
		return state.Samples[i].At < state.Samples[j].At
	})
	if len(state.Samples) > 100 {
		state.Samples = state.Samples[len(state.Samples)-100:]
	}
	if state.StatusEvents == nil {
		state.StatusEvents = make(map[int][]int64)
	}
	maxWindow := 0
	for _, rule := range policy.Rules {
		if rule.ruleType() == FailureRuleStatusCode && rule.WindowSeconds > maxWindow {
			maxWindow = rule.WindowSeconds
		}
	}
	for status, events := range state.StatusEvents {
		kept := events[:0]
		for _, at := range events {
			if at > now.Add(-time.Duration(maxWindow)*time.Second).UnixMilli() {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(state.StatusEvents, status)
		} else {
			state.StatusEvents[status] = kept
		}
	}
	for index, rule := range policy.Rules {
		count, trigger := 0, false
		switch rule.ruleType() {
		case FailureRuleStatusCode:
			if !containsStatus(rule.StatusCodes, attempt.StatusCode) {
				continue
			}
			cutoff := now.Add(-time.Duration(rule.WindowSeconds) * time.Second).UnixMilli()
			if finished.UnixMilli() <= cutoff {
				continue
			}
			events := append(state.StatusEvents[attempt.StatusCode], finished.UnixMilli())
			kept := events[:0]
			for _, at := range events {
				if at > cutoff {
					kept = append(kept, at)
				}
			}
			state.StatusEvents[attempt.StatusCode] = kept
			count, trigger = len(kept), len(kept) >= rule.Threshold
		case FailureRuleConsecutiveFailures:
			for i := len(state.Samples) - 1; i >= 0 && state.Samples[i].Outcome != OutcomeSuccess; i-- {
				count++
			}
			trigger = count >= rule.Threshold
		case FailureRuleSLASuccessRate:
			if len(state.Samples) < rule.SampleSize {
				continue
			}
			for _, item := range state.Samples[len(state.Samples)-rule.SampleSize:] {
				if item.Outcome == OutcomeSuccess {
					count++
				}
			}
			trigger = count*100 < rule.Threshold*rule.SampleSize
		case FailureRuleTimeout, FailureRuleRateLimit:
			samples := state.Samples
			if len(samples) > rule.SampleSize {
				samples = samples[len(samples)-rule.SampleSize:]
			}
			for _, item := range samples {
				if (rule.ruleType() == FailureRuleTimeout && item.Outcome == OutcomeTimeout) || (rule.ruleType() == FailureRuleRateLimit && item.Outcome == OutcomeRateLimit) {
					count++
				}
			}
			trigger = count >= rule.Threshold
		case FailureRuleTTFT:
			var samples []AccountHealthSample
			for _, item := range state.Samples {
				if item.FirstContentNanos != nil || item.FirstContentExpired || item.Outcome != OutcomeSuccess {
					samples = append(samples, item)
				}
			}
			if len(samples) > rule.SampleSize {
				samples = samples[len(samples)-rule.SampleSize:]
			}
			for _, item := range samples {
				if item.FirstContentExpired || (item.FirstContentNanos != nil && time.Duration(*item.FirstContentNanos) > time.Duration(rule.FirstContentSeconds)*time.Second) {
					count++
				}
			}
			trigger = count >= rule.Threshold
		}
		if trigger {
			until := now.Add(time.Duration(rule.CooldownSeconds) * time.Second).UnixMilli()
			if until > state.CooldownUntil {
				state.CooldownUntil, state.CooldownRule, state.CooldownCount = until, index, count
			}
		}
	}
	events := 0
	for _, values := range state.StatusEvents {
		events += len(values)
	}
	if events > opts.MaxStatusEvents {
		return nil, snapshot, ErrAccountHealthCapacity
	}
	if state.CooldownUntil > now.UnixMilli() {
		state.Samples, state.StatusEvents = nil, nil
		state.WindowStartedAt = state.CooldownUntil
	}
	data, marshalErr := json.Marshal(state)
	if marshalErr != nil || len(data) > opts.MaxPayloadBytes {
		return nil, snapshot, ErrAccountHealthCapacity
	}
	updated := accountHealthSnapshot(state, now)
	updated.Accepted = true
	return state, updated, nil
}

func accountHealthSnapshot(state *AccountHealthState, now time.Time) AccountHealthSnapshot {
	result := AccountHealthSnapshot{PolicyRevision: state.PolicyRevision, RuleIndex: -1}
	if state.CooldownUntil > now.UnixMilli() {
		result.Cooling, result.CooldownUntil, result.RuleIndex, result.Count = true, time.UnixMilli(state.CooldownUntil).UTC(), state.CooldownRule, state.CooldownCount
	}
	return result
}

func accountHealthPolicyHash(policy RouteFailurePolicy) string {
	copy := policy.Clone()
	for i := range copy.Rules {
		sort.Ints(copy.Rules[i].StatusCodes)
		if copy.Rules[i].Type == "" {
			copy.Rules[i].Type = FailureRuleStatusCode
		}
	}
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validateAccountHealthAttempt(attempt AccountHealthAttempt, now time.Time) error {
	if !attempt.Dispatched || attempt.Outcome == OutcomeIgnored {
		return nil
	}
	if strings.TrimSpace(attempt.AttemptID) == "" || len(attempt.AttemptID) > 128 || attempt.FinishedAt.IsZero() || (attempt.StatusCode != 0 && (attempt.StatusCode < 100 || attempt.StatusCode > 599)) {
		return ErrAccountHealthAttempt
	}
	switch attempt.Outcome {
	case OutcomeSuccess, OutcomeFailure, OutcomeTimeout, OutcomeRateLimit:
	default:
		return ErrAccountHealthAttempt
	}
	if attempt.FirstContent != nil && (*attempt.FirstContent < 0 || *attempt.FirstContent > 24*time.Hour) {
		return ErrAccountHealthAttempt
	}
	if attempt.FirstContentExpired && attempt.FirstContent != nil {
		return ErrAccountHealthAttempt
	}
	if attempt.LocalFirstContentTimeout && (attempt.StatusCode != 0 || !attempt.FirstContentExpired || attempt.Outcome == OutcomeSuccess) {
		return ErrAccountHealthAttempt
	}
	finished := attempt.FinishedAt
	if finished.After(now.Add(time.Minute)) || (!attempt.StartedAt.IsZero() && attempt.StartedAt.After(finished)) {
		return ErrAccountHealthAttempt
	}
	return nil
}
