package service

// This module adapts optional Key-local soft health to native scheduling. It
// does not authorize groups, choose accounts, acquire slots or perform billing.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

var (
	ErrKeyAccountRouteStale   = errors.New("key account route revision is stale")
	ErrKeyAccountRouteScope   = errors.New("invalid key account route session scope")
	ErrKeyAccountRouteCooling = errors.New("key-local account route is cooling")
)

type KeyAccountRoutingAccountLister interface {
	ListSchedulableByGroupID(context.Context, int64) ([]Account, error)
}

// An optional shared-store implementation can collapse the first candidate
// scan into bounded reads. Results must preserve input order and revisions.
// Runtime falls back to single reads cached for the current inbound request. Final
// account admission always refreshes the selected identity separately.
type KeyAccountRoutingHealthBatchReader interface {
	GetMany(context.Context, []keyrouting.AccountHealthIdentity) ([]keyrouting.AccountHealthSnapshot, error)
}

type KeyAccountRoutingRuntimeOptions struct {
	Site  string
	Clock func() time.Time
	// Optional soft health has its own short budget, never the inbound/model
	// deadline. Zero selects defaults; values above one second are clamped.
	HealthReadTimeout  time.Duration
	HealthWriteTimeout time.Duration
}

const (
	keyAccountHealthReadTimeout  = 150 * time.Millisecond
	keyAccountHealthWriteTimeout = 250 * time.Millisecond
	keyAccountHealthMaxTimeout   = time.Second
	// Match the repository batch reader's maximum accepted input size.
	keyAccountHealthBatchLimit = 1024
)

type KeyAccountRoutePolicySnapshot struct {
	GroupID        int64
	KeyRevision    int64
	GroupRevision  int64
	PolicyRevision uint64
	Policy         keyrouting.RouteFailurePolicy
}

type keyAccountRoutingFrozenPolicy struct {
	KeyAccountRoutePolicySnapshot
	groupRaw string
}

type KeyAccountRoutingRuntime struct {
	store     keyrouting.AccountHealthStore
	revisions KeyRouteRevisionReader
	accounts  KeyAccountRoutingAccountLister
	opts      KeyAccountRoutingRuntimeOptions
}

func NewKeyAccountRoutingRuntime(store keyrouting.AccountHealthStore, revisions KeyRouteRevisionReader, accounts KeyAccountRoutingAccountLister, opts KeyAccountRoutingRuntimeOptions) *KeyAccountRoutingRuntime {
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	opts.Site = strings.TrimSpace(opts.Site)
	if opts.HealthReadTimeout <= 0 {
		opts.HealthReadTimeout = keyAccountHealthReadTimeout
	}
	if opts.HealthWriteTimeout <= 0 {
		opts.HealthWriteTimeout = keyAccountHealthWriteTimeout
	}
	if opts.HealthReadTimeout > keyAccountHealthMaxTimeout {
		opts.HealthReadTimeout = keyAccountHealthMaxTimeout
	}
	if opts.HealthWriteTimeout > keyAccountHealthMaxTimeout {
		opts.HealthWriteTimeout = keyAccountHealthMaxTimeout
	}
	return &KeyAccountRoutingRuntime{store: store, revisions: revisions, accounts: accounts, opts: opts}
}

// NewSession is a strict opt-in: historical/single-group Keys remain native.
// It freezes custom metadata only. Native auth/planner must still authorize
// each group, model, endpoint and account immediately before their use.
func (r *KeyAccountRoutingRuntime) NewSession(ctx context.Context, key *APIKey, request KeyRoutingRequest, budget *keyrouting.AccountRouteBudget) (*KeyAccountRoutingSession, error) {
	if key == nil || key.KeyRouteScope != KeyRouteScopeAccountV2 || len(key.BackupGroupIDs) == 0 {
		return nil, nil
	}
	if ctx == nil {
		return nil, ErrKeyAccountRouteScope
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.revisions == nil || r.opts.Site == "" || len(r.opts.Site) > 128 || budget == nil || key.ID <= 0 || key.GroupID == nil || *key.GroupID <= 0 || key.KeyRouteRevision <= 0 || len(key.BackupGroupIDs) > keyrouting.MaxBackupGroups {
		return nil, ErrKeyAccountRouteScope
	}
	request.Model, request.Endpoint = strings.TrimSpace(request.Model), strings.TrimSpace(request.Endpoint)
	if request.Model == "" || len(request.Model) > 256 || (request.Endpoint != CompositeRouteEndpointResponses && request.Endpoint != CompositeRouteEndpointChatCompletions && request.Endpoint != CompositeRouteEndpointMessages) {
		return nil, ErrKeyAccountRouteScope
	}
	ids := append([]int64{*key.GroupID}, key.BackupGroupIDs...)
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, ErrKeyAccountRouteScope
		}
		seen[id] = true
	}
	current, err := r.revisions.GetKeyRouteRevision(ctx, key.ID)
	if err != nil {
		return nil, fmt.Errorf("read Key route revision: %w", err)
	}
	if current != key.KeyRouteRevision {
		return nil, ErrKeyAccountRouteStale
	}
	s := &KeyAccountRoutingSession{runtime: r, keyID: key.ID, keyRevision: key.KeyRouteRevision, request: request, budget: budget, policies: make(map[int64]keyAccountRoutingFrozenPolicy, len(ids)), health: make(map[string]keyrouting.AccountHealthSnapshot), candidates: make(map[int64][]int64), localExcluded: make(map[int64]map[int64]struct{})}
	for _, groupID := range ids {
		frozen := keyAccountRoutingFrozenPolicy{KeyAccountRoutePolicySnapshot: KeyAccountRoutePolicySnapshot{GroupID: groupID, KeyRevision: key.KeyRouteRevision, PolicyRevision: uint64(key.KeyRouteRevision), Policy: keyrouting.RouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{}}}}
		if key.KeyRouteFailurePolicy != nil {
			frozen.Policy = *key.KeyRouteFailurePolicy.Clone()
			if err := frozen.Policy.Validate(); err != nil {
				return nil, fmt.Errorf("invalid frozen Key policy: %w", err)
			}
		} else {
			s.inherited = true
			raw, revision, err := r.revisions.GetGroupRoutePolicySnapshot(ctx, groupID)
			if err != nil {
				return nil, fmt.Errorf("read group %d route policy: %w", groupID, err)
			}
			if revision <= 0 {
				return nil, fmt.Errorf("%w: invalid group policy revision", ErrKeyAccountRouteScope)
			}
			policy, err := decodeKeyAccountRoutingGroupPolicy(raw)
			if err != nil {
				return nil, fmt.Errorf("decode group %d route policy: %w", groupID, err)
			}
			frozen.Policy, frozen.GroupRevision, frozen.groupRaw = policy, revision, raw
			if revision > key.KeyRouteRevision {
				frozen.PolicyRevision = uint64(revision)
			}
		}
		s.policies[groupID] = frozen
	}
	// Do not admit a session assembled across a concurrent Key metadata edit.
	current, err = r.revisions.GetKeyRouteRevision(ctx, key.ID)
	if err != nil {
		return nil, fmt.Errorf("verify frozen Key route revision: %w", err)
	}
	if current != s.keyRevision {
		return nil, ErrKeyAccountRouteStale
	}
	return s, nil
}

type KeyAccountRoutingSession struct {
	runtime       *KeyAccountRoutingRuntime
	keyID         int64
	keyRevision   int64
	request       KeyRoutingRequest
	inherited     bool
	policies      map[int64]keyAccountRoutingFrozenPolicy
	budget        *keyrouting.AccountRouteBudget
	mu            sync.Mutex
	health        map[string]keyrouting.AccountHealthSnapshot
	candidates    map[int64][]int64
	localExcluded map[int64]map[int64]struct{}
	// A timed-out optional backend is bypassed for the remaining inbound
	// request, so traversing many candidates cannot multiply that timeout.
	healthDeadline error
}

type keyAccountRoutingSessionContextKey struct{}

// Bind this on the original request context before group dispatch. It binds
// both transport session discovery and the existing native scheduling gate.
func WithKeyAccountRoutingSession(ctx context.Context, s *KeyAccountRoutingSession) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, keyAccountRoutingSessionContextKey{}, s)
	if s == nil {
		return WithKeyAccountRouteGate(ctx, nil)
	}
	return WithKeyAccountRouteGate(ctx, s)
}

func GetKeyAccountRoutingSession(ctx context.Context) *KeyAccountRoutingSession {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(keyAccountRoutingSessionContextKey{}).(*KeyAccountRoutingSession)
	return s
}

// Policy returns an immutable copy for timer/transport setup, not auth proof.
func (s *KeyAccountRoutingSession) Policy(groupID int64) (KeyAccountRoutePolicySnapshot, bool) {
	if s == nil {
		return KeyAccountRoutePolicySnapshot{}, false
	}
	p, ok := s.policies[groupID]
	if !ok {
		return KeyAccountRoutePolicySnapshot{}, false
	}
	result := p.KeyAccountRoutePolicySnapshot
	result.Policy = *p.Policy.Clone()
	return result, true
}

// ValidateRevision is deliberately separate from health reads. Metadata/auth
// errors stay hard errors; optional health-store failures alone may degrade.
// A request cache must never replace this authoritative admission/finish check.
func (s *KeyAccountRoutingSession) ValidateRevision(ctx context.Context, groupID int64) error {
	if ctx == nil {
		return ErrKeyAccountRouteScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.runtime == nil || s.runtime.revisions == nil {
		return ErrKeyAccountRouteScope
	}
	frozen, ok := s.policies[groupID]
	if !ok {
		return ErrKeyAccountRouteScope
	}
	revision, err := s.runtime.revisions.GetKeyRouteRevision(ctx, s.keyID)
	if err != nil {
		return fmt.Errorf("verify Key route revision: %w", err)
	}
	if revision != s.keyRevision {
		return ErrKeyAccountRouteStale
	}
	if s.inherited {
		raw, groupRevision, err := s.runtime.revisions.GetGroupRoutePolicySnapshot(ctx, groupID)
		if err != nil {
			return fmt.Errorf("verify group route revision: %w", err)
		}
		if groupRevision != frozen.GroupRevision || raw != frozen.groupRaw {
			return ErrKeyAccountRouteStale
		}
	}
	return nil
}

// Exclusions returns a read-only hint. Even an entirely cooled snapshot cannot
// authorize a group switch; the coordinator still checks native eligibility,
// request budget and replay safety. A partial store error returns an error,
// never a fabricated all-cooled set.
func (s *KeyAccountRoutingSession) Exclusions(ctx context.Context, groupID int64) (map[int64]struct{}, error) {
	if err := s.ValidateRevision(ctx, groupID); err != nil {
		return nil, err
	}
	result := s.RequestExclusions(groupID)
	frozen := s.policies[groupID]
	if len(frozen.Policy.Rules) == 0 {
		return result, nil
	}
	ids, err := s.candidateIDs(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if err = s.loadHealth(ctx, groupID, ids); err != nil {
		return nil, err
	}
	now := s.runtime.opts.Clock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		entry := s.health[s.identity(groupID, id).StorageKey()]
		if entry.Cooling && now.Before(entry.CooldownUntil) {
			result[id] = struct{}{}
		}
	}
	return result, nil
}

// Eligible always refreshes the final chosen account after native slot waits.
// It neither selects an alternative nor spends a dispatch budget.
func (s *KeyAccountRoutingSession) Eligible(ctx context.Context, groupID, accountID int64) (bool, error) {
	if err := s.ValidateRevision(ctx, groupID); err != nil {
		return false, err
	}
	return s.eligibleHealth(ctx, groupID, accountID, true)
}

// CandidateEligible is an advisory pool scan. The caller checks the group's
// authoritative revision once at scan entry; this method never authorizes or
// dispatches. Existing Eligible retains its authoritative final-check contract.
// Cached hints cannot replace Eligible or BeforeDispatch after a slot wait.
func (s *KeyAccountRoutingSession) CandidateEligible(ctx context.Context, groupID, accountID int64) (bool, error) {
	return s.eligibleHealth(ctx, groupID, accountID, false)
}

func (s *KeyAccountRoutingSession) eligibleHealth(ctx context.Context, groupID, accountID int64, fresh bool) (bool, error) {
	if err := s.validateGroupScope(ctx, groupID); err != nil {
		return false, err
	}
	if accountID <= 0 {
		return false, ErrKeyAccountRouteScope
	}
	if _, excluded := s.RequestExclusions(groupID)[accountID]; excluded {
		return false, nil
	}
	if len(s.policies[groupID].Policy.Rules) == 0 {
		return true, nil
	}
	if s.runtime.store == nil {
		return false, fmt.Errorf("%w: no health store", ErrKeyAccountSoftHealthUnavailable)
	}
	id := s.identity(groupID, accountID)
	if !fresh {
		if err := s.loadHealth(ctx, groupID, []int64{accountID}); err != nil {
			return false, err
		}
		s.mu.Lock()
		snapshot := s.health[id.StorageKey()]
		s.mu.Unlock()
		return !snapshot.Cooling || !s.runtime.opts.Clock().Before(snapshot.CooldownUntil), nil
	}
	child, cancel, err := s.healthContext(ctx, false)
	if err != nil {
		return false, err
	}
	defer cancel()
	snapshot, err := s.runtime.store.Get(child, id)
	if err != nil {
		return false, s.finishHealthError(ctx, child, err)
	}
	if snapshot.PolicyRevision != id.PolicyRevision {
		return false, ErrKeyAccountRouteStale
	}
	s.mu.Lock()
	s.health[id.StorageKey()] = snapshot
	s.mu.Unlock()
	return !snapshot.Cooling || !s.runtime.opts.Clock().Before(snapshot.CooldownUntil), nil
}

func (s *KeyAccountRoutingSession) Metadata(groupID, accountID int64, upstreamModel, platform string) (keyrouting.AccountAttemptMetadata, error) {
	if s == nil || accountID <= 0 {
		return keyrouting.AccountAttemptMetadata{}, ErrKeyAccountRouteScope
	}
	frozen, ok := s.policies[groupID]
	if !ok {
		return keyrouting.AccountAttemptMetadata{}, ErrKeyAccountRouteScope
	}
	path := keyrouting.AccountRouteFingerprint{AccountID: accountID, UpstreamModel: strings.TrimSpace(upstreamModel), Platform: strings.TrimSpace(platform), Endpoint: s.request.Endpoint}
	if path.UpstreamModel == "" || path.Platform == "" {
		return keyrouting.AccountAttemptMetadata{}, ErrKeyAccountRouteScope
	}
	return keyrouting.AccountAttemptMetadata{Site: s.runtime.opts.Site, APIKeyID: s.keyID, GroupID: groupID, AccountID: accountID, Model: s.request.Model, UpstreamModel: path.UpstreamModel, Platform: path.Platform, Endpoint: s.request.Endpoint, PolicyRevision: frozen.PolicyRevision}, nil
}

// CanDispatch is a frozen-scope/budget hint. BeforeDispatch/TryDispatch share
// the atomic spending boundary, immediately before a real inference send.
func (s *KeyAccountRoutingSession) CanDispatch(ctx context.Context, metadata keyrouting.AccountAttemptMetadata) error {
	if err := s.validateMetadataScope(ctx, metadata); err != nil {
		return err
	}
	if _, excluded := s.RequestExclusions(metadata.GroupID)[metadata.AccountID]; excluded {
		return ErrKeyAccountRouteCooling
	}
	return s.budget.CanDispatch(metadata.GroupID, keyAccountRoutingPath(metadata))
}

// TryDispatch is safe for direct callers too: it runs the same fresh health,
// authoritative revision and budget boundary as the transport callback.
func (s *KeyAccountRoutingSession) TryDispatch(ctx context.Context, metadata keyrouting.AccountAttemptMetadata) error {
	return s.BeforeDispatch(ctx, metadata)
}

// BeforeDispatch is the transport callback for one real inference send. Only
// optional health-store outages degrade; local exclusions, metadata/revision
// checks and the whole-request budget remain mandatory. It binds its own gate
// if called with a derived context that has lost the original session binding.
func (s *KeyAccountRoutingSession) BeforeDispatch(ctx context.Context, metadata keyrouting.AccountAttemptMetadata) error {
	if s == nil || ctx == nil {
		return ErrKeyAccountRouteScope
	}
	if err := s.validateMetadataScope(ctx, metadata); err != nil {
		return err
	}
	if err := s.budget.CanDispatch(metadata.GroupID, keyAccountRoutingPath(metadata)); err != nil {
		return err
	}
	allowed, err := s.eligibleHealth(ctx, metadata.GroupID, metadata.AccountID, true)
	if errors.Is(err, ErrKeyAccountSoftHealthUnavailable) && ctx.Err() == nil {
		bound, ok := keyAccountRouteGateFromContext(ctx).(*KeyAccountRoutingSession)
		if !ok || bound != s {
			ctx = WithKeyAccountRouteGate(ctx, s)
		}
		keyAccountRouteWarnDegraded(ctx, err)
		_, local := s.RequestExclusions(metadata.GroupID)[metadata.AccountID]
		allowed, err = !local, nil
	}
	if err != nil {
		return err
	}
	// Recheck AFTER the optional read. Configuration changes while Redis was
	// slow cannot bless an old admission; this is the sole SQL boundary here.
	if err := s.validateMetadata(ctx, metadata); err != nil {
		return err
	}
	if !allowed {
		return ErrKeyAccountRouteCooling
	}
	return s.budget.TryDispatch(metadata.GroupID, keyAccountRoutingPath(metadata))
}

// ExcludeRequestAccount is a coordinator decision after native same-account
// retries finish. Observe does not add this local exclusion; a configured
// persisted cooldown may independently disallow the next native admission.
func (s *KeyAccountRoutingSession) ExcludeRequestAccount(groupID, accountID int64) error {
	if s == nil || accountID <= 0 {
		return ErrKeyAccountRouteScope
	}
	if _, ok := s.policies[groupID]; !ok {
		return ErrKeyAccountRouteScope
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.localExcluded[groupID] == nil {
		s.localExcluded[groupID] = make(map[int64]struct{})
	}
	s.localExcluded[groupID][accountID] = struct{}{}
	return nil
}

// Independent of Redis: a soft-store error must not erase these exclusions.
func (s *KeyAccountRoutingSession) RequestExclusions(groupID int64) map[int64]struct{} {
	result := make(map[int64]struct{})
	if s == nil {
		return result
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.localExcluded[groupID] {
		result[id] = struct{}{}
	}
	return result
}

func (s *KeyAccountRoutingSession) Observe(ctx context.Context, sample keyrouting.AccountAttemptSample) (keyrouting.AccountHealthSnapshot, error) {
	var empty keyrouting.AccountHealthSnapshot
	if err := s.validateMetadata(ctx, sample.Metadata); err != nil {
		return empty, err
	}
	attempt, err := keyAccountRoutingHealthAttempt(sample)
	if err != nil {
		return empty, err
	}
	if !attempt.Dispatched || attempt.Outcome == keyrouting.OutcomeIgnored {
		return empty, nil
	}
	if attempt.Outcome != keyrouting.OutcomeSuccess {
		s.budget.RecordFailure(sample.Metadata.GroupID, keyAccountRoutingPath(sample.Metadata))
	}
	frozen := s.policies[sample.Metadata.GroupID]
	if len(frozen.Policy.Rules) == 0 {
		return empty, nil
	}
	if s.runtime.store == nil {
		return empty, fmt.Errorf("%w: no health store", ErrKeyAccountSoftHealthUnavailable)
	}
	id := s.identity(sample.Metadata.GroupID, sample.Metadata.AccountID)
	child, cancel, err := s.healthContext(ctx, true)
	if err != nil {
		return empty, err
	}
	defer cancel()
	result, err := s.runtime.store.Observe(child, id, frozen.Policy, attempt)
	if err != nil {
		s.mu.Lock()
		delete(s.health, id.StorageKey())
		s.mu.Unlock()
		return empty, s.finishHealthError(ctx, child, err)
	}
	s.mu.Lock()
	s.health[id.StorageKey()] = result
	s.mu.Unlock()
	return result, nil
}

// CoolingUntil is the earliest known health recovery, not proof that every
// native candidate is cooled. The coordinator owns pool-exhaustion decisions.
func (s *KeyAccountRoutingSession) CoolingUntil(groupID int64) time.Time {
	if s == nil {
		return time.Time{}
	}
	now := s.runtime.opts.Clock()
	var earliest time.Time
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, accountID := range s.candidates[groupID] {
		entry := s.health[s.identity(groupID, accountID).StorageKey()]
		if entry.Cooling && now.Before(entry.CooldownUntil) && (earliest.IsZero() || entry.CooldownUntil.Before(earliest)) {
			earliest = entry.CooldownUntil
		}
	}
	return earliest
}

func (s *KeyAccountRoutingSession) RetryAfter(groupID int64) time.Duration {
	until := s.CoolingUntil(groupID)
	if until.IsZero() {
		return 0
	}
	remaining := until.Sub(s.runtime.opts.Clock())
	if remaining <= 0 {
		return 0
	}
	return ((remaining + time.Second - 1) / time.Second) * time.Second
}

func (s *KeyAccountRoutingSession) identity(groupID, accountID int64) keyrouting.AccountHealthIdentity {
	return keyrouting.AccountHealthIdentity{Site: s.runtime.opts.Site, APIKeyID: s.keyID, GroupID: groupID, AccountID: accountID, Model: s.request.Model, Endpoint: s.request.Endpoint, PolicyRevision: s.policies[groupID].PolicyRevision}
}

func (s *KeyAccountRoutingSession) validateMetadata(ctx context.Context, metadata keyrouting.AccountAttemptMetadata) error {
	if err := s.validateMetadataScope(ctx, metadata); err != nil {
		return err
	}
	return s.ValidateRevision(ctx, metadata.GroupID)
}

func (s *KeyAccountRoutingSession) validateMetadataScope(ctx context.Context, metadata keyrouting.AccountAttemptMetadata) error {
	if err := s.validateGroupScope(ctx, metadata.GroupID); err != nil {
		return err
	}
	if s == nil {
		return ErrKeyAccountRouteScope
	}
	frozen, ok := s.policies[metadata.GroupID]
	if !ok || metadata.Site != s.runtime.opts.Site || metadata.APIKeyID != s.keyID || metadata.AccountID <= 0 || metadata.Model != s.request.Model || metadata.Endpoint != s.request.Endpoint || metadata.PolicyRevision != frozen.PolicyRevision || strings.TrimSpace(metadata.UpstreamModel) == "" || strings.TrimSpace(metadata.Platform) == "" {
		return ErrKeyAccountRouteScope
	}
	return nil
}

func (s *KeyAccountRoutingSession) validateGroupScope(ctx context.Context, groupID int64) error {
	if ctx == nil || s == nil || s.runtime == nil {
		return ErrKeyAccountRouteScope
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.policies[groupID]; !ok {
		return ErrKeyAccountRouteScope
	}
	return nil
}

func (s *KeyAccountRoutingSession) candidateIDs(ctx context.Context, groupID int64) ([]int64, error) {
	s.mu.Lock()
	cached, ok := s.candidates[groupID]
	s.mu.Unlock()
	if ok {
		return append([]int64(nil), cached...), nil
	}
	if s.runtime.accounts == nil {
		return nil, fmt.Errorf("%w: account lister unavailable", ErrKeyAccountRouteScope)
	}
	accounts, err := s.runtime.accounts.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("read native group candidates: %w", err)
	}
	ids := make([]int64, 0, len(accounts))
	seen := make(map[int64]bool, len(accounts))
	for _, account := range accounts {
		if account.ID > 0 && !seen[account.ID] {
			ids = append(ids, account.ID)
			seen[account.ID] = true
		}
	}
	s.mu.Lock()
	s.candidates[groupID] = append([]int64(nil), ids...)
	s.mu.Unlock()
	return ids, nil
}

func (s *KeyAccountRoutingSession) loadHealth(ctx context.Context, groupID int64, accountIDs []int64) error {
	if s.runtime.store == nil {
		return fmt.Errorf("%w: no health store", ErrKeyAccountSoftHealthUnavailable)
	}
	now := s.runtime.opts.Clock()
	var missing []keyrouting.AccountHealthIdentity
	s.mu.Lock()
	for _, accountID := range accountIDs {
		id := s.identity(groupID, accountID)
		snapshot, ok := s.health[id.StorageKey()]
		if !ok || (snapshot.Cooling && !now.Before(snapshot.CooldownUntil)) {
			missing = append(missing, id)
		}
	}
	s.mu.Unlock()
	if len(missing) == 0 {
		return nil
	}
	child, cancel, err := s.healthContext(ctx, false)
	if err != nil {
		return err
	}
	defer cancel()
	results := make([]keyrouting.AccountHealthSnapshot, len(missing))
	if batch, ok := s.runtime.store.(KeyAccountRoutingHealthBatchReader); ok {
		// Large groups share the same read deadline across all bounded calls.
		// Publish nothing until every chunk and revision has been validated.
		for start := 0; start < len(missing); start += keyAccountHealthBatchLimit {
			if err := child.Err(); err != nil {
				return s.finishHealthError(ctx, child, err)
			}
			end := min(start+keyAccountHealthBatchLimit, len(missing))
			loaded, err := batch.GetMany(child, missing[start:end])
			if err != nil {
				return s.finishHealthError(ctx, child, err)
			}
			if len(loaded) != end-start {
				return s.healthError(ctx, keyrouting.ErrAccountHealthCorrupt)
			}
			copy(results[start:end], loaded)
		}
	} else {
		for i, id := range missing {
			snapshot, err := s.runtime.store.Get(child, id)
			if err != nil {
				return s.finishHealthError(ctx, child, err)
			}
			results[i] = snapshot
		}
	}
	if err := child.Err(); err != nil {
		return s.finishHealthError(ctx, child, err)
	}
	for index, snapshot := range results {
		if snapshot.PolicyRevision != missing[index].PolicyRevision {
			return ErrKeyAccountRouteStale
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index, id := range missing {
		s.health[id.StorageKey()] = results[index]
	}
	return nil
}

func (s *KeyAccountRoutingSession) healthError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, keyrouting.ErrAccountHealthStaleRevision) {
		return fmt.Errorf("%w: %w", ErrKeyAccountRouteStale, err)
	}
	if errors.Is(err, keyrouting.ErrAccountHealthIdentity) || errors.Is(err, keyrouting.ErrAccountHealthAttempt) || errors.Is(err, keyrouting.ErrAccountHealthPolicyConflict) {
		return fmt.Errorf("%w: %w", ErrKeyAccountRouteScope, err)
	}
	return fmt.Errorf("%w: %w", ErrKeyAccountSoftHealthUnavailable, err)
}

func (s *KeyAccountRoutingSession) healthContext(parent context.Context, write bool) (context.Context, context.CancelFunc, error) {
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	cached := s.healthDeadline
	s.mu.Unlock()
	if cached != nil {
		return nil, nil, cached
	}
	timeout := s.runtime.opts.HealthReadTimeout
	if write {
		timeout = s.runtime.opts.HealthWriteTimeout
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, nil
}

func (s *KeyAccountRoutingSession) finishHealthError(parent, child context.Context, err error) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	if child.Err() != nil {
		err = child.Err()
	}
	wrapped := s.healthError(parent, err)
	var networkTimeout net.Error
	timedOut := errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkTimeout) && networkTimeout.Timeout())
	if timedOut && errors.Is(wrapped, ErrKeyAccountSoftHealthUnavailable) {
		s.mu.Lock()
		s.healthDeadline = wrapped
		s.mu.Unlock()
	}
	return wrapped
}

func decodeKeyAccountRoutingGroupPolicy(raw string) (keyrouting.RouteFailurePolicy, error) {
	policy := keyrouting.RouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{}}
	if strings.TrimSpace(raw) == "" {
		return policy, nil
	}
	var group GroupFailurePolicy
	if err := json.Unmarshal([]byte(raw), &group); err != nil {
		return policy, err
	}
	if err := validateGroupFailurePolicy(&group); err != nil {
		return policy, err
	}
	for _, rule := range group.Public().StatusRules {
		policy.Rules = append(policy.Rules, keyrouting.RouteFailureRule{StatusCodes: append([]int(nil), rule.StatusCodes...), Threshold: rule.Threshold, WindowSeconds: rule.WindowSeconds, CooldownSeconds: rule.CooldownSeconds})
	}
	return policy, policy.Validate()
}

func keyAccountRoutingPath(metadata keyrouting.AccountAttemptMetadata) keyrouting.AccountRouteFingerprint {
	endpoint := strings.TrimSpace(metadata.UpstreamEndpoint)
	if endpoint == "" {
		endpoint = metadata.Endpoint
	}
	return keyrouting.AccountRouteFingerprint{AccountID: metadata.AccountID, UpstreamModel: metadata.UpstreamModel, Platform: metadata.Platform, Endpoint: endpoint}
}

var _ KeyAccountRouteGate = (*KeyAccountRoutingSession)(nil)
