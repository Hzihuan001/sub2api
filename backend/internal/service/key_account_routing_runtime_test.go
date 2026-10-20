package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

type accountRuntimeRevisionStub struct {
	keyRevision          int64
	groupRevision        map[int64]int64
	groupRaw             map[int64]string
	keyErr, groupErr     error
	keyReads, groupReads int
	onKeyRead            func(int)
}

func (r *accountRuntimeRevisionStub) GetKeyRouteRevision(_ context.Context, _ int64) (int64, error) {
	r.keyReads++
	if r.onKeyRead != nil {
		r.onKeyRead(r.keyReads)
	}
	return r.keyRevision, r.keyErr
}
func (r *accountRuntimeRevisionStub) GetGroupRoutePolicySnapshot(_ context.Context, id int64) (string, int64, error) {
	r.groupReads++
	return r.groupRaw[id], r.groupRevision[id], r.groupErr
}

type accountRuntimeListerStub struct {
	accounts map[int64][]Account
	calls    int
	err      error
}

func (l *accountRuntimeListerStub) ListSchedulableByGroupID(_ context.Context, id int64) ([]Account, error) {
	l.calls++
	return l.accounts[id], l.err
}

type accountRuntimeStoreStub struct {
	inner                  keyrouting.AccountHealthStore
	getErr, observeErr     error
	getCalls, observeCalls int
	getIDs                 []keyrouting.AccountHealthIdentity
	override               *keyrouting.AccountHealthSnapshot
}

func (s *accountRuntimeStoreStub) Get(ctx context.Context, id keyrouting.AccountHealthIdentity) (keyrouting.AccountHealthSnapshot, error) {
	s.getCalls++
	s.getIDs = append(s.getIDs, id)
	if s.getErr != nil {
		return keyrouting.AccountHealthSnapshot{}, s.getErr
	}
	if s.override != nil {
		return *s.override, nil
	}
	return s.inner.Get(ctx, id)
}
func (s *accountRuntimeStoreStub) Observe(ctx context.Context, id keyrouting.AccountHealthIdentity, p keyrouting.RouteFailurePolicy, a keyrouting.AccountHealthAttempt) (keyrouting.AccountHealthSnapshot, error) {
	s.observeCalls++
	if s.observeErr != nil {
		return keyrouting.AccountHealthSnapshot{}, s.observeErr
	}
	return s.inner.Observe(ctx, id, p, a)
}
func (s *accountRuntimeStoreStub) Fence(ctx context.Context, id keyrouting.AccountHealthIdentity) error {
	return s.inner.Fence(ctx, id)
}

type accountRuntimeBatchStore struct {
	*accountRuntimeStoreStub
	batchCalls int
	short      bool
}

func (s *accountRuntimeBatchStore) GetMany(ctx context.Context, ids []keyrouting.AccountHealthIdentity) ([]keyrouting.AccountHealthSnapshot, error) {
	s.batchCalls++
	if s.getErr != nil {
		return nil, s.getErr
	}
	results := make([]keyrouting.AccountHealthSnapshot, 0, len(ids))
	for _, id := range ids {
		snapshot, err := s.inner.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, snapshot)
	}
	if s.short && len(results) > 0 {
		return results[:len(results)-1], nil
	}
	return results, nil
}

func accountRuntimeFixture(t *testing.T, policy *KeyRouteFailurePolicy) (*KeyAccountRoutingSession, *accountRuntimeRevisionStub, *accountRuntimeStoreStub, *accountRuntimeListerStub, *APIKey, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	revision := &accountRuntimeRevisionStub{keyRevision: 10, groupRevision: map[int64]int64{1: 11, 2: 12}, groupRaw: map[int64]string{}}
	store := &accountRuntimeStoreStub{inner: keyrouting.NewMemoryAccountHealthStore(keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }})}
	lister := &accountRuntimeListerStub{accounts: map[int64][]Account{1: {{ID: 41}, {ID: 42}, {ID: 41}}, 2: {{ID: 41}, {ID: 43}}}}
	primary := int64(1)
	key := &APIKey{ID: 7, GroupID: &primary, BackupGroupIDs: []int64{2}, KeyRouteScope: KeyRouteScopeAccountV2, KeyRouteRevision: 10, KeyRouteFailurePolicy: policy}
	budget, err := keyrouting.NewAccountRouteBudget(8, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	runtime := NewKeyAccountRoutingRuntime(store, revision, lister, KeyAccountRoutingRuntimeOptions{Site: "local-acceptance", Clock: func() time.Time { return now }})
	session, err := runtime.NewSession(context.Background(), key, KeyRoutingRequest{Model: "public-model", Endpoint: CompositeRouteEndpointChatCompletions}, budget)
	if err != nil {
		t.Fatal(err)
	}
	return session, revision, store, lister, key, &now
}

func accountRuntimeStatusPolicy() *KeyRouteFailurePolicy {
	return &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{{StatusCodes: []int{502, 503}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600}}}
}

func accountRuntimeSample(t *testing.T, s *KeyAccountRoutingSession, groupID, accountID int64, attempt string, now time.Time) keyrouting.AccountAttemptSample {
	t.Helper()
	metadata, err := s.Metadata(groupID, accountID, "actual-model", "openai")
	if err != nil {
		t.Fatal(err)
	}
	metadata.AttemptID = attempt
	return keyrouting.AccountAttemptSample{Metadata: metadata, Dispatched: true, Outcome: keyrouting.OutcomeFailure, StatusCode: 503, StatusSource: keyrouting.AttemptStatusUpstream, UpstreamStatusCode: 503, StartedAt: now.Add(-time.Second), FinishedAt: now}
}

func TestKeyAccountRoutingRuntimeOptInAndFreeze(t *testing.T) {
	s, r, _, _, key, _ := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	key.KeyRouteFailurePolicy.Rules[0].StatusCodes[0] = 504
	frozen, ok := s.Policy(1)
	if !ok || frozen.PolicyRevision != 10 || frozen.GroupRevision != 0 || frozen.Policy.Rules[0].StatusCodes[0] != 502 {
		t.Fatalf("wrong frozen policy: %+v", frozen)
	}
	frozen.Policy.Rules[0].StatusCodes[0] = 500
	again, _ := s.Policy(1)
	if again.Policy.Rules[0].StatusCodes[0] != 502 {
		t.Fatal("Policy leaked mutable slices")
	}
	ctx := WithKeyAccountRoutingSession(context.Background(), s)
	if GetKeyAccountRoutingSession(ctx) != s || keyAccountRouteGateFromContext(ctx) != s {
		t.Fatal("session and gate not bound together")
	}
	ctx = WithKeyAccountRoutingSession(ctx, nil)
	if GetKeyAccountRoutingSession(ctx) != nil || keyAccountRouteGateFromContext(ctx) != nil {
		t.Fatal("clearing session retained old gate")
	}
	before := r.keyReads
	for _, mutation := range []func(*APIKey){func(k *APIKey) { k.KeyRouteScope = KeyRouteScopeGroupV1 }, func(k *APIKey) { k.BackupGroupIDs = nil }} {
		copy := *key
		mutation(&copy)
		got, err := s.runtime.NewSession(context.Background(), &copy, KeyRoutingRequest{}, nil)
		if err != nil || got != nil {
			t.Fatalf("non-opt-in changed native behavior: %v %+v", err, got)
		}
	}
	if r.keyReads != before {
		t.Fatal("native Key consulted metadata")
	}
	if _, err := s.runtime.NewSession(nil, key, s.request, s.budget); !errors.Is(err, ErrKeyAccountRouteScope) {
		t.Fatalf("nil context: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeInheritedPolicyAndRevision(t *testing.T) {
	s, r, store, _, _, now := accountRuntimeFixture(t, nil)
	r.groupRaw[1] = `{"rules":[{"status_codes":[503],"threshold":2,"window_seconds":60,"cooldown_seconds":600}]}`
	key := &APIKey{ID: 7, GroupID: pointerAccountRuntime(1), BackupGroupIDs: []int64{2}, KeyRouteScope: KeyRouteScopeAccountV2, KeyRouteRevision: 10}
	updated, err := s.runtime.NewSession(context.Background(), key, s.request, s.budget)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := updated.Policy(1)
	if p.PolicyRevision != 11 || p.GroupRevision != 11 || p.Policy.Rules[0].Threshold != 2 {
		t.Fatalf("wrong inherited snapshot: %+v", p)
	}
	sample := accountRuntimeSample(t, updated, 1, 41, "old-revision", *now)
	r.groupRevision[1] = 13
	r.groupRaw[1] = ""
	if _, err := updated.Eligible(context.Background(), 1, 41); !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("edited policy admitted old request: %v", err)
	}
	if _, err := updated.Observe(context.Background(), sample); !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("deleted rules accepted old terminal: %v", err)
	}
	if store.getCalls != 0 || store.observeCalls != 0 {
		t.Fatal("stale policy reached health store")
	}
	// Even an incorrect unchanged revision with changed raw must fail closed.
	r.groupRevision[1] = 11
	if err := updated.ValidateRevision(context.Background(), 1); !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("raw/revision snapshot mismatch accepted: %v", err)
	}
	// A Key edit also invalidates explicit policies, not only inheritance.
	explicit, rev, explicitStore, _, _, at := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	old := accountRuntimeSample(t, explicit, 1, 41, "old-key", *at)
	rev.keyRevision++
	if _, err := explicit.Observe(context.Background(), old); !errors.Is(err, ErrKeyAccountRouteStale) || explicitStore.observeCalls != 0 {
		t.Fatalf("old Key completion revived rules: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeConcurrentSnapshotAndHardErrors(t *testing.T) {
	s, r, _, _, key, _ := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	changeAt := r.keyReads + 2
	r.onKeyRead = func(read int) {
		if read == changeAt {
			r.keyRevision++
		}
	}
	if _, err := s.runtime.NewSession(context.Background(), key, s.request, s.budget); !errors.Is(err, ErrKeyAccountRouteStale) {
		t.Fatalf("changed snapshot accepted: %v", err)
	}
	r.onKeyRead = nil
	r.keyRevision = 10
	r.keyErr = errors.New("metadata db unavailable")
	if _, err := s.Eligible(context.Background(), 1, 41); err == nil || errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("metadata outage became optional health: %v", err)
	}
	r.keyErr = nil
	if _, err := s.Exclusions(context.Background(), 99); !errors.Is(err, ErrKeyAccountRouteScope) {
		t.Fatalf("unknown group accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Eligible(ctx, 1, 41); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel swallowed: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeNoActiveRules(t *testing.T) {
	s, _, store, lister, _, now := accountRuntimeFixture(t, &KeyRouteFailurePolicy{Rules: []KeyRouteFailureRule{}})
	store.getErr = errors.New("must not read health")
	store.observeErr = errors.New("must not write health")
	if got, err := s.Exclusions(context.Background(), 1); err != nil || len(got) != 0 {
		t.Fatalf("no-rule exclusions: %v %v", got, err)
	}
	if eligible, err := s.Eligible(context.Background(), 1, 41); err != nil || !eligible {
		t.Fatalf("no-rule admission: %v %v", eligible, err)
	}
	if _, err := s.Observe(context.Background(), accountRuntimeSample(t, s, 1, 41, "no-rule", *now)); err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 0 || store.observeCalls != 0 || lister.calls != 0 {
		t.Fatal("disabled rules touched health or candidate store")
	}
	if err := s.ExcludeRequestAccount(1, 41); err != nil {
		t.Fatal(err)
	}
	if eligible, err := s.Eligible(context.Background(), 1, 41); err != nil || eligible {
		t.Fatalf("request-local exclusion lost without active rules: %v %v", eligible, err)
	}
}

func TestKeyAccountRoutingRuntimeCoolingAndIdentityIsolation(t *testing.T) {
	s, _, store, _, key, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	if _, err := s.Exclusions(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	observed, err := s.Observe(context.Background(), accountRuntimeSample(t, s, 1, 41, "real-503", *now))
	if err != nil || !observed.Cooling {
		t.Fatalf("account not cooled: %+v %v", observed, err)
	}
	if after := s.RetryAfter(1); after != 600*time.Second {
		t.Fatalf("retry-after=%s", after)
	}
	if eligible, err := s.Eligible(context.Background(), 1, 41); err != nil || eligible {
		t.Fatalf("cool account admitted: %v %v", eligible, err)
	}
	if eligible, err := s.Eligible(context.Background(), 1, 42); err != nil || !eligible {
		t.Fatalf("other account affected: %v %v", eligible, err)
	}
	if eligible, err := s.Eligible(context.Background(), 2, 41); err != nil || !eligible {
		t.Fatalf("shared account affected in other group: %v %v", eligible, err)
	}
	for _, change := range []string{"site", "key", "model", "endpoint"} {
		runtime := *s.runtime
		copyKey := *key
		request := s.request
		switch change {
		case "site":
			runtime.opts.Site = "other-site"
		case "key":
			copyKey.ID = 8
		case "model":
			request.Model = "another-public-model"
		case "endpoint":
			request.Endpoint = CompositeRouteEndpointResponses
		}
		budget, _ := keyrouting.NewAccountRouteBudget(8, time.Minute)
		isolated, err := runtime.NewSession(context.Background(), &copyKey, request, budget)
		if err != nil {
			t.Fatal(err)
		}
		if eligible, err := isolated.Eligible(context.Background(), 1, 41); err != nil || !eligible {
			t.Fatalf("%s scope leaked cooling: %v %v", change, eligible, err)
		}
	}
	if len(store.getIDs) == 0 {
		t.Fatal("missing health reads")
	}
	*now = now.Add(600 * time.Second)
	if eligible, err := s.Eligible(context.Background(), 1, 41); err != nil || !eligible {
		t.Fatalf("expired cooldown stayed active: %v %v", eligible, err)
	}
}

func TestKeyAccountRoutingRuntimeHealthCacheAndFinalRefresh(t *testing.T) {
	s, _, store, lister, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	if _, err := s.Exclusions(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 2 || lister.calls != 1 {
		t.Fatalf("initial candidates not deduplicated: reads=%d lists=%d", store.getCalls, lister.calls)
	}
	store.override = &keyrouting.AccountHealthSnapshot{PolicyRevision: 10, Cooling: true, CooldownUntil: now.Add(time.Minute)}
	if _, err := s.Exclusions(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 2 || lister.calls != 1 {
		t.Fatal("cached candidate scan repeated I/O")
	}
	if allowed, err := s.Eligible(context.Background(), 1, 41); err != nil || allowed || store.getCalls != 3 {
		t.Fatalf("final admission did not fresh-read: allowed=%v err=%v reads=%d", allowed, err, store.getCalls)
	}
	batch := &accountRuntimeBatchStore{accountRuntimeStoreStub: store}
	s.runtime.store = batch
	s.mu.Lock()
	s.health = make(map[string]keyrouting.AccountHealthSnapshot)
	s.mu.Unlock()
	if _, err := s.Exclusions(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if batch.batchCalls != 1 || store.getCalls != 3 {
		t.Fatal("optional batch store not used")
	}
	batch.short = true
	s.mu.Lock()
	s.health = make(map[string]keyrouting.AccountHealthSnapshot)
	s.mu.Unlock()
	if _, err := s.Exclusions(context.Background(), 1); !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("corrupt batch result not soft-failed: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeSoftHealthAndLocalExclusions(t *testing.T) {
	s, _, store, _, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	if err := s.ExcludeRequestAccount(1, 41); err != nil {
		t.Fatal(err)
	}
	store.getErr = errors.New("redis offline")
	if _, err := s.Exclusions(context.Background(), 1); !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("store read not classified: %v", err)
	}
	copy := s.RequestExclusions(1)
	delete(copy, 41)
	if _, exists := s.RequestExclusions(1)[41]; !exists {
		t.Fatal("local exclusion map leaked")
	}
	ctx := WithKeyAccountRoutingSession(context.Background(), s)
	group := int64(1)
	merged, err := keyAccountRouteSchedulingExclusions(ctx, &group, map[int64]struct{}{90: {}})
	if err != nil || len(merged) != 2 {
		t.Fatalf("degradation lost native/local exclusions: %v %v", merged, err)
	}
	if allowed, err := keyAccountRouteAdmission(ctx, &group, 41); err != nil || allowed {
		t.Fatalf("degradation re-admitted failed local account: %v %v", allowed, err)
	}
	if allowed, err := keyAccountRouteAdmission(ctx, &group, 42); err != nil || !allowed {
		t.Fatalf("optional store outage blocked healthy native account: %v %v", allowed, err)
	}
	store.observeErr = keyrouting.ErrAccountHealthCapacity
	if _, err := s.Observe(context.Background(), accountRuntimeSample(t, s, 1, 42, "capacity", *now)); !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("capacity error not soft: %v", err)
	}
	store.getErr = keyrouting.ErrAccountHealthStaleRevision
	if _, err := s.Eligible(context.Background(), 1, 42); !errors.Is(err, ErrKeyAccountRouteStale) || errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("store revision error failed open: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeDispatchBudgetAndMetadata(t *testing.T) {
	policy := accountRuntimeStatusPolicy()
	policy.Rules[0].Threshold = 5
	s, _, store, _, _, now := accountRuntimeFixture(t, policy)
	budget, err := keyrouting.NewAccountRouteBudget(2, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	s.budget = budget
	metadata, err := s.Metadata(1, 41, "actual-model", "openai")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := s.CanDispatch(context.Background(), metadata); err != nil {
			t.Fatal(err)
		}
	}
	if budget.Dispatched() != 0 || store.getCalls != 0 {
		t.Fatal("advisory budget check dispatched or read health")
	}
	if err := s.TryDispatch(context.Background(), metadata); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observe(context.Background(), accountRuntimeSample(t, s, 1, 41, "attempt-1", *now)); err != nil {
		t.Fatal(err)
	}
	// Native same-account retry in the same group retains its original right.
	if err := s.TryDispatch(context.Background(), metadata); err != nil {
		t.Fatalf("native retry stolen: %v", err)
	}
	if err := s.TryDispatch(context.Background(), metadata); !errors.Is(err, keyrouting.ErrAccountRouteAttemptBudget) || errors.Is(err, ErrKeyAccountSoftHealthUnavailable) {
		t.Fatalf("budget exhaustion was hidden: %v", err)
	}
	newBudget, _ := keyrouting.NewAccountRouteBudget(8, time.Minute)
	s.budget = newBudget
	s.budget.RecordFailure(1, keyAccountRoutingPath(metadata))
	backup, err := s.Metadata(2, 41, "actual-model", "openai")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CanDispatch(context.Background(), backup); !errors.Is(err, keyrouting.ErrAccountRouteRepeatedPath) {
		t.Fatalf("same actual path retried through backup: %v", err)
	}
	// Actual protocol paths, not public entry paths, decide repeat safety.
	backup.UpstreamEndpoint = CompositeRouteEndpointResponses
	if err := s.CanDispatch(context.Background(), backup); err != nil {
		t.Fatalf("distinct actual endpoint incorrectly blocked: %v", err)
	}
	if got := keyAccountRoutingPath(backup).Endpoint; got != CompositeRouteEndpointResponses {
		t.Fatalf("public endpoint shadowed actual endpoint: %s", got)
	}
	bad := metadata
	bad.APIKeyID++
	if err := s.TryDispatch(context.Background(), bad); !errors.Is(err, ErrKeyAccountRouteScope) {
		t.Fatalf("foreign metadata accepted: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeIgnoredSamples(t *testing.T) {
	s, _, store, _, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	for _, change := range []func(*keyrouting.AccountAttemptSample){func(a *keyrouting.AccountAttemptSample) { a.Dispatched = false }, func(a *keyrouting.AccountAttemptSample) { a.Outcome = keyrouting.OutcomeIgnored }, func(a *keyrouting.AccountAttemptSample) { a.CancellationCause = keyrouting.AttemptClientCancelled }} {
		sample := accountRuntimeSample(t, s, 1, 41, "ignored", *now)
		change(&sample)
		if _, err := s.Observe(context.Background(), sample); err != nil {
			t.Fatal(err)
		}
	}
	if store.observeCalls != 0 {
		t.Fatal("non-attempt/client cancellation mutated health")
	}
	bad := accountRuntimeSample(t, s, 1, 41, "", *now)
	if _, err := s.Observe(context.Background(), bad); !errors.Is(err, ErrKeyAccountRouteScope) {
		t.Fatalf("missing attempt identity accepted: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeBeforeDispatch(t *testing.T) {
	s, revision, store, _, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	metadata, err := s.Metadata(1, 41, "actual-model", "openai")
	if err != nil {
		t.Fatal(err)
	}
	store.getErr = errors.New("redis offline")
	if err := s.BeforeDispatch(context.Background(), metadata); err != nil || s.budget.Dispatched() != 1 {
		t.Fatalf("optional health outage blocked dispatch: %v", err)
	}
	if err := s.ExcludeRequestAccount(1, 41); err != nil {
		t.Fatal(err)
	}
	if err := s.BeforeDispatch(context.Background(), metadata); !errors.Is(err, ErrKeyAccountRouteCooling) || s.budget.Dispatched() != 1 {
		t.Fatalf("local exclusion failed open or spent budget: %v", err)
	}
	other, _ := s.Metadata(1, 42, "actual-model", "openai")
	revision.keyRevision++
	if err := s.BeforeDispatch(context.Background(), other); !errors.Is(err, ErrKeyAccountRouteStale) || s.budget.Dispatched() != 1 {
		t.Fatalf("metadata edit failed open or spent budget: %v", err)
	}
	revision.keyRevision--
	store.getErr = nil
	if _, err := s.Observe(context.Background(), accountRuntimeSample(t, s, 1, 42, "cool-before-send", *now)); err != nil {
		t.Fatal(err)
	}
	if err := s.BeforeDispatch(context.Background(), other); !errors.Is(err, ErrKeyAccountRouteCooling) || s.budget.Dispatched() != 1 {
		t.Fatalf("cooldown was not final-admission checked: %v", err)
	}
}

func TestKeyAccountRoutingRuntimeAttemptEvidence(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	base := keyrouting.AccountAttemptSample{Metadata: keyrouting.AccountAttemptMetadata{AttemptID: "a"}, Dispatched: true, Outcome: keyrouting.OutcomeFailure, StartedAt: now.Add(-time.Second), FinishedAt: now}
	tests := []struct {
		name           string
		edit           func(*keyrouting.AccountAttemptSample)
		status         int
		outcome        keyrouting.RouteOutcomeKind
		expired, local bool
		first          *time.Duration
	}{
		{"real503", func(a *keyrouting.AccountAttemptSample) {
			a.StatusSource = keyrouting.AttemptStatusUpstream
			a.StatusCode = 503
			a.UpstreamStatusCode = 503
		}, 503, keyrouting.OutcomeFailure, false, false, nil},
		{"real429", func(a *keyrouting.AccountAttemptSample) {
			a.StatusSource = keyrouting.AttemptStatusUpstream
			a.StatusCode = 429
			a.UpstreamStatusCode = 429
		}, 429, keyrouting.OutcomeRateLimit, false, false, nil},
		{"real524", func(a *keyrouting.AccountAttemptSample) {
			a.StatusSource = keyrouting.AttemptStatusUpstream
			a.StatusCode = 524
			a.UpstreamStatusCode = 524
		}, 524, keyrouting.OutcomeTimeout, false, false, nil},
		{"associatedClient", func(a *keyrouting.AccountAttemptSample) {
			a.StatusSource = keyrouting.AttemptStatusClient
			a.StatusCode = 502
			a.ClientStatusCode = 502
		}, 502, keyrouting.OutcomeFailure, false, false, nil},
		{"syntheticClient", func(a *keyrouting.AccountAttemptSample) {
			a.StatusSource = keyrouting.AttemptStatusClient
			a.StatusCode = 504
			a.ClientStatusCode = 504
			a.ClientStatusSynthetic = true
		}, 0, keyrouting.OutcomeFailure, false, false, nil},
		{"inconsistentStatus", func(a *keyrouting.AccountAttemptSample) {
			a.StatusSource = keyrouting.AttemptStatusUpstream
			a.StatusCode = 503
			a.UpstreamStatusCode = 429
		}, 0, keyrouting.OutcomeFailure, false, false, nil},
		{"localTTFT", func(a *keyrouting.AccountAttemptSample) {
			a.Outcome = keyrouting.OutcomeTimeout
			a.FirstContentExpired = true
			a.CancellationCause = keyrouting.AttemptFirstContentExpiry
			a.StatusSource = keyrouting.AttemptStatusClient
			a.StatusCode = 504
			a.ClientStatusCode = 504
		}, 0, keyrouting.OutcomeFailure, true, true, nil},
		{"TTFTwithReal503", func(a *keyrouting.AccountAttemptSample) {
			a.Outcome = keyrouting.OutcomeTimeout
			a.FirstContentExpired = true
			a.StatusSource = keyrouting.AttemptStatusUpstream
			a.StatusCode = 503
			a.UpstreamStatusCode = 503
		}, 503, keyrouting.OutcomeFailure, true, false, nil},
		{"lateContentSuccess", func(a *keyrouting.AccountAttemptSample) {
			a.Outcome = keyrouting.OutcomeSuccess
			a.FirstContentExpired = true
			a.FirstContentObserved = true
			a.FirstContentElapsed = 9 * time.Second
		}, 0, keyrouting.OutcomeSuccess, false, false, durationAccountRuntime(9 * time.Second)},
		{"lateContentFailure", func(a *keyrouting.AccountAttemptSample) {
			a.Outcome = keyrouting.OutcomeTimeout
			a.FirstContentExpired = true
			a.FirstContentObserved = true
			a.FirstContentElapsed = 9 * time.Second
		}, 0, keyrouting.OutcomeFailure, false, false, durationAccountRuntime(9 * time.Second)},
		{"fastFailureNoTTFT", func(a *keyrouting.AccountAttemptSample) {}, 0, keyrouting.OutcomeFailure, false, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample := base
			tt.edit(&sample)
			got, err := keyAccountRoutingHealthAttempt(sample)
			if err != nil {
				t.Fatal(err)
			}
			if got.StatusCode != tt.status || got.Outcome != tt.outcome || got.FirstContentExpired != tt.expired || got.LocalFirstContentTimeout != tt.local || !reflect.DeepEqual(got.FirstContent, tt.first) {
				t.Fatalf("unexpected mapped evidence: %+v", got)
			}
		})
	}
}

func pointerAccountRuntime(value int64) *int64                  { return &value }
func durationAccountRuntime(value time.Duration) *time.Duration { return &value }
