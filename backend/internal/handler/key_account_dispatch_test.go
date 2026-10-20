package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountDispatchRevisions struct{ revision int64 }

func (r *accountDispatchRevisions) GetKeyRouteRevision(context.Context, int64) (int64, error) {
	return r.revision, nil
}
func (r *accountDispatchRevisions) GetGroupRoutePolicySnapshot(context.Context, int64) (string, int64, error) {
	return "", r.revision, nil
}

type accountDispatchAccounts map[int64][]service.Account

func (a accountDispatchAccounts) ListSchedulableByGroupID(_ context.Context, group int64) ([]service.Account, error) {
	return a[group], nil
}

type accountDispatchStore struct {
	keyrouting.AccountHealthStore
	observations []keyrouting.AccountHealthIdentity
}

func (s *accountDispatchStore) Observe(ctx context.Context, id keyrouting.AccountHealthIdentity, policy keyrouting.RouteFailurePolicy, attempt keyrouting.AccountHealthAttempt) (keyrouting.AccountHealthSnapshot, error) {
	s.observations = append(s.observations, id)
	return s.AccountHealthStore.Observe(ctx, id, policy, attempt)
}

type accountDispatchSelector struct {
	*dispatchTestSelector
	accounts accountDispatchAccounts
	err      error
}

func (s *accountDispatchSelector) AccountGroupHasCandidate(ctx context.Context, _ *service.APIKey, _ service.KeyRoutingRequest, group int64) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	session := service.GetKeyAccountRoutingSession(ctx)
	_, err := session.Exclusions(ctx, group)
	if err != nil {
		return false, err
	}
	for _, account := range s.accounts[group] {
		allowed, err := session.CandidateEligible(ctx, group, account.ID)
		if err != nil {
			return false, err
		}
		if allowed {
			return true, nil
		}
	}
	return false, nil
}
func (s *accountDispatchSelector) AccountFirstContentTimeout(context.Context, *service.APIKey, service.KeyRoutingRequest, int64, int64) (time.Duration, error) {
	return 0, nil
}

type accountDispatchFixture struct {
	c        *gin.Context
	selector *accountDispatchSelector
	session  *service.KeyAccountRoutingSession
	store    *accountDispatchStore
	budget   *keyrouting.AccountRouteBudget
	sends    []int64
	entries  []int64
	closed   int
	cleaned  int
}

func newAccountDispatchFixture(t *testing.T) *accountDispatchFixture {
	t.Helper()
	c, original := newDispatchTestContext(t)
	key, _ := servermiddleware.GetAPIKeyFromContext(c)
	key.KeyRouteScope, key.KeyRouteRevision = service.KeyRouteScopeAccountV2, 10
	key.KeyRouteFailurePolicy = &service.KeyRouteFailurePolicy{Rules: []service.KeyRouteFailureRule{{StatusCodes: []int{503}, Threshold: 1, WindowSeconds: 60, CooldownSeconds: 600}}}
	accounts := accountDispatchAccounts{
		1: {{ID: 41, Platform: service.PlatformOpenAI}, {ID: 42, Platform: service.PlatformOpenAI}},
		2: {{ID: 43, Platform: service.PlatformDeepseek}},
	}
	store := &accountDispatchStore{AccountHealthStore: keyrouting.NewMemoryAccountHealthStore(keyrouting.AccountHealthOptions{})}
	budget, err := keyrouting.NewAccountRouteBudget(10, time.Minute)
	require.NoError(t, err)
	runtime := service.NewKeyAccountRoutingRuntime(store, &accountDispatchRevisions{revision: 10}, accounts, service.KeyAccountRoutingRuntimeOptions{Site: "handler-local"})
	request, _ := keyRouteRequest(c)
	session, err := runtime.NewSession(c.Request.Context(), key, request, budget)
	require.NoError(t, err)
	ctx := service.WithKeyAccountRoutingSession(c.Request.Context(), session)
	ctx = context.WithValue(ctx, ctxkey.Group, key.Group)
	c.Request = c.Request.WithContext(ctx)
	c.Set("custom.key_route_base_context", ctx)
	selector := &accountDispatchSelector{dispatchTestSelector: original, accounts: accounts}
	c.Set(keyRoutingSelectorContextKey, selector)
	return &accountDispatchFixture{c: c, selector: selector, session: session, store: store, budget: budget}
}

type accountDispatchBody struct {
	io.Reader
	closed *int
}

func (b *accountDispatchBody) Close() error { *b.closed++; return nil }

func (f *accountDispatchFixture) do(t *testing.T, ctx context.Context, account *service.Account, status int, body string, closeBody bool) error {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://controlled.invalid/v1/chat/completions", strings.NewReader(`{"model":"actual-model"}`))
	require.NoError(t, err)
	resp, err := service.KeyAccountInferenceDoFunc(req, account, "actual-model", func(*http.Request) (*http.Response, error) {
		f.sends = append(f.sends, account.ID)
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: &accountDispatchBody{Reader: strings.NewReader(body), closed: &f.closed}}, nil
	})
	if err != nil {
		return err
	}
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	if closeBody {
		require.NoError(t, resp.Body.Close())
	}
	if status >= 400 {
		return &service.UpstreamFailoverError{StatusCode: status}
	}
	return nil
}

func (f *accountDispatchFixture) choose(t *testing.T, group int64, fs *FailoverState) *service.Account {
	t.Helper()
	_, err := f.session.Exclusions(f.c.Request.Context(), group)
	require.NoError(t, err)
	for _, account := range f.selector.accounts[group] {
		if _, failed := fs.FailedAccountIDs[account.ID]; failed {
			continue
		}
		allowed, err := f.session.CandidateEligible(f.c.Request.Context(), group, account.ID)
		require.NoError(t, err)
		if allowed {
			return &account
		}
	}
	return nil
}

func (f *accountDispatchFixture) run(t *testing.T, limit int, statuses map[int64]int) {
	t.Helper()
	KeyRouteDispatch(func(c *gin.Context) {
		key, _ := servermiddleware.GetAPIKeyFromContext(c)
		group := *key.GroupID
		f.entries = append(f.entries, group)
		defer func() { f.cleaned++ }()
		fs := keyAccountFailoverState(c, limit, false)
		for {
			account := f.choose(t, group, fs)
			if account == nil {
				if !tryKeyAccountGroupExhausted(c) {
					writeKeyAccountUnavailable(c, "no candidate")
				}
				return
			}
			ctx := keyAccountForwardContext(c, c.Request.Context(), account)
			status := statuses[account.ID]
			if status == 0 {
				status = 200
			}
			err := f.do(t, ctx, account, status, `{"ok":true}`, true)
			if keyRouteFirstContentInterrupted(c, nil, err) {
				return
			}
			err = keyRouteFirstContentNormalizeError(c, err)
			if err == nil {
				c.JSON(http.StatusOK, gin.H{"account": account.ID})
				return
			}
			var failure *service.UpstreamFailoverError
			if errors.As(err, &failure) && fs.HandleFailoverError(c.Request.Context(), nil, account.ID, account.Platform, 0, failure) == FailoverContinue {
				continue
			}
			if !tryFinalKeyRouteFailover(c, failure) {
				writeKeyAccountUnavailable(c, "retry exhausted")
			}
			return
		}
	})(f.c)
}

func TestKeyAccountDispatchKeepsHealthySameGroupBeforeBackup(t *testing.T) {
	f := newAccountDispatchFixture(t)
	f.run(t, 4, map[int64]int{41: 503})
	require.Equal(t, []int64{41, 42}, f.sends)
	require.Equal(t, []int64{1}, f.entries)
	require.Empty(t, f.selector.switched)
	require.Len(t, f.store.observations, 2)
	require.Empty(t, f.selector.statuses, "old group counters must not run alongside account samples")
	require.Equal(t, 2, f.budget.Dispatched())
	require.Equal(t, 2, f.closed)
}

func TestKeyAccountDispatchExhaustsSameGroupBeforeBackup(t *testing.T) {
	f := newAccountDispatchFixture(t)
	f.run(t, 4, map[int64]int{41: 503, 42: 503})
	require.Equal(t, []int64{41, 42, 43}, f.sends)
	require.Equal(t, []int64{1, 2}, f.entries)
	require.Equal(t, []int64{1}, f.selector.switched)
	require.Equal(t, 2, f.cleaned)
	require.Len(t, f.store.observations, 3)
	require.Equal(t, "backup", f.c.GetString("custom.key_route_switch_group"))
}

func TestKeyAccountDispatchApprovedNativeBudgetBackupDoesNotFailHealthyCandidate(t *testing.T) {
	f := newAccountDispatchFixture(t)
	f.run(t, 0, map[int64]int{41: 503})
	require.Equal(t, []int64{41, 43}, f.sends)
	require.Equal(t, []int64{1}, f.selector.switched)
	require.Equal(t, http.StatusOK, f.c.Writer.Status())
	require.Len(t, f.store.observations, 2)
	_, excluded := f.session.RequestExclusions(1)[42]
	require.False(t, excluded, "untried healthy B must not be failed by the approved budget exception")
	allowed, err := f.session.CandidateEligible(f.c.Request.Context(), 1, 42)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestKeyAccountDispatchApprovedBudgetBackupStillHonorsWholeRequestBudget(t *testing.T) {
	f := newAccountDispatchFixture(t)
	// Consume the shared budget with independent prior mapped paths. Group
	// switching cannot replenish this request's remaining wire permissions.
	for i := 0; i < 9; i++ {
		require.NoError(t, f.budget.TryDispatch(1, keyrouting.AccountRouteFingerprint{AccountID: int64(100 + i), UpstreamModel: "actual-model", Platform: "openai", Endpoint: "/v1/chat/completions"}))
	}
	f.run(t, 0, map[int64]int{41: 503})
	require.Equal(t, []int64{41}, f.sends)
	require.Empty(t, f.selector.switched)
	require.Equal(t, http.StatusServiceUnavailable, f.c.Writer.Status())
	require.Len(t, f.store.observations, 1)
}

func TestKeyAccountDispatchFreshAdmissionReselectsWithoutSampleOrSwitch(t *testing.T) {
	f := newAccountDispatchFixture(t)
	entries := 0
	KeyRouteDispatch(func(c *gin.Context) {
		entries++
		fs := keyAccountFailoverState(c, 3, false)
		if entries == 1 {
			defer func() { f.cleaned++ }()
			account := &f.selector.accounts[1][0]
			ctx := keyAccountForwardContext(c, c.Request.Context(), account)
			metadata, err := f.session.Metadata(1, account.ID, "actual-model", account.Platform)
			require.NoError(t, err)
			metadata.AttemptID = "other-inflight-attempt"
			now := time.Now()
			_, err = f.session.Observe(ctx, keyrouting.AccountAttemptSample{Metadata: metadata, Dispatched: true, Outcome: keyrouting.OutcomeFailure, UpstreamStatusCode: 503, StatusCode: 503, StatusSource: keyrouting.AttemptStatusUpstream, StartedAt: now.Add(-time.Second), FinishedAt: now})
			require.NoError(t, err)
			err = f.do(t, ctx, account, 200, `{"ok":true}`, true)
			require.ErrorIs(t, err, service.ErrKeyAccountRouteCooling)
			require.True(t, keyRouteFirstContentInterrupted(c, nil, err))
			return
		}
		require.Equal(t, 1, f.cleaned, "all previous handler defers must finish before reentry")
		require.Zero(t, fs.SwitchCount, "an unsent admission denial is not a failed native attempt")
		account := f.choose(t, 1, fs)
		require.Equal(t, int64(42), account.ID)
		ctx := keyAccountForwardContext(c, c.Request.Context(), account)
		err := f.do(t, ctx, account, 200, `{"ok":true}`, true)
		require.NoError(t, err)
		require.False(t, keyRouteFirstContentInterrupted(c, nil, err))
		c.JSON(200, gin.H{"ok": true})
	})(f.c)
	require.Equal(t, 2, entries)
	require.Equal(t, []int64{42}, f.sends)
	require.Len(t, f.store.observations, 2, "only other real request and B; denied A creates no sample")
	require.Equal(t, 1, f.budget.Dispatched())
	require.Empty(t, f.selector.switched)
}

func TestKeyAccountDispatchInternalRetryAdmissionKeepsNativeSwitchBudget(t *testing.T) {
	f := newAccountDispatchFixture(t)
	entries := 0
	KeyRouteDispatch(func(c *gin.Context) {
		entries++
		fs := keyAccountFailoverState(c, 3, false)
		if entries == 1 {
			account := &f.selector.accounts[1][0]
			ctx := keyAccountForwardContext(c, c.Request.Context(), account)
			require.Error(t, f.do(t, ctx, account, 503, `{"error":{"type":"overloaded_error"}}`, true))
			err := f.do(t, ctx, account, 200, `{"ok":true}`, true)
			require.ErrorIs(t, err, service.ErrKeyAccountRouteCooling)
			require.True(t, keyRouteFirstContentInterrupted(c, nil, err))
			return
		}
		require.Equal(t, 1, fs.SwitchCount, "A was actually sent before its internal retry was denied")
		_, failed := fs.FailedAccountIDs[41]
		require.True(t, failed)
		account := f.choose(t, 1, fs)
		require.Equal(t, int64(42), account.ID)
		ctx := keyAccountForwardContext(c, c.Request.Context(), account)
		err := f.do(t, ctx, account, 200, `{"ok":true}`, true)
		require.NoError(t, err)
		require.False(t, keyRouteFirstContentInterrupted(c, nil, err))
		c.JSON(200, gin.H{"ok": true})
	})(f.c)
	require.Equal(t, []int64{41, 42}, f.sends)
	require.Len(t, f.store.observations, 2)
	require.Equal(t, 2, f.budget.Dispatched())
}

func TestKeyAccountDispatchUsageAndCleanupVetoWithoutTTFT(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		closeBody bool
	}{
		{"known usage", 503, `{"usage":{"input_tokens":3}}`, true},
		{"unknown success", 200, `{"opaque":"unrecognized"}`, true},
		{"body not closed", 503, `{"error":"overloaded"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAccountDispatchFixture(t)
			c := f.c
			c.Set("custom.key_route_dispatch", true)
			keyAccountFailoverState(c, 3, false)
			account := &f.selector.accounts[1][0]
			ctx := keyAccountForwardContext(c, c.Request.Context(), account)
			err := f.do(t, ctx, account, tc.status, tc.body, tc.closeBody)
			if err == nil {
				err = &service.UpstreamFailoverError{StatusCode: 502}
			}
			require.False(t, keyRouteFirstContentInterrupted(c, nil, err))
			require.True(t, keyAccountForwardReplayBlocked(c))
			require.False(t, tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: 503}))
			require.Empty(t, f.selector.switched)
			require.Equal(t, []int64{41}, f.sends)
		})
	}
}

func TestKeyAccountDispatchNative503ResetAndRetryDoNotReviveCooling(t *testing.T) {
	f := newAccountDispatchFixture(t)
	c := f.c
	fs := keyAccountFailoverState(c, 3, false)
	account := &f.selector.accounts[1][0]
	ctx := keyAccountForwardContext(c, c.Request.Context(), account)
	err := f.do(t, ctx, account, 503, `{"error":"overloaded"}`, true)
	require.False(t, keyRouteFirstContentInterrupted(c, nil, err))
	require.False(t, keyAccountSameAccountRetryAllowed(c.Request.Context(), account.ID))
	failure := &service.UpstreamFailoverError{StatusCode: 503, RetryableOnSameAccount: true}
	require.Equal(t, FailoverContinue, fs.HandleFailoverError(c.Request.Context(), nil, account.ID, account.Platform, 3, failure))
	require.Empty(t, fs.SameAccountRetryCount, "known cooling must skip the native same-account delay")
	require.Equal(t, FailoverExhausted, fs.HandleSelectionExhausted(c.Request.Context()))
	_, excluded := f.session.RequestExclusions(1)[account.ID]
	require.True(t, excluded)
	_, retained := fs.FailedAccountIDs[account.ID]
	require.True(t, retained)
	require.True(t, keyAccountSameAccountRetryAllowed(context.Background(), account.ID), "no session retains exact native retry policy")
}

func TestKeyAccountDispatchClientCancelNeverStartsBackup(t *testing.T) {
	f := newAccountDispatchFixture(t)
	ctx, cancel := context.WithCancel(f.c.Request.Context())
	f.c.Request = f.c.Request.WithContext(ctx)
	f.c.Set("custom.key_route_base_context", ctx)
	KeyRouteDispatch(func(c *gin.Context) {
		fs := keyAccountFailoverState(c, 0, false)
		account := f.choose(t, 1, fs)
		wireCtx := keyAccountForwardContext(c, c.Request.Context(), account)
		err := f.do(t, wireCtx, account, 503, `{"error":"overloaded"}`, true)
		cancel()
		require.False(t, keyRouteFirstContentInterrupted(c, nil, err))
		require.False(t, tryFinalKeyRouteFailover(c, &service.UpstreamFailoverError{StatusCode: 503}))
	})(f.c)
	require.Equal(t, []int64{41}, f.sends)
	require.Empty(t, f.selector.switched)
	require.Equal(t, 1, f.budget.Dispatched())
}

func TestKeyAccountDispatchReentryKeepsOnlyExistingCausalBackupLabel(t *testing.T) {
	for _, label := range []string{"", "切换备用分组：测试备用"} {
		f := newAccountDispatchFixture(t)
		key, _ := servermiddleware.GetAPIKeyFromContext(f.c)
		request, _ := keyRouteRequest(f.c)
		f.c.Set("custom.key_route_switch_group", label)
		require.NoError(t, resetKeyAccountGroupContext(f.c, key, request, f.selector))
		require.Equal(t, label, service.OpsRouteSwitchLabel(f.c.Request.Context()), "historical cooling without a causal error must not fabricate a label")
	}
}

func TestKeyAccountDispatchUnavailableReportsObservedCooldown(t *testing.T) {
	f := newAccountDispatchFixture(t)
	_, err := f.session.Exclusions(f.c.Request.Context(), 1)
	require.NoError(t, err)
	metadata, err := f.session.Metadata(1, 41, "actual-model", service.PlatformOpenAI)
	require.NoError(t, err)
	metadata.AttemptID = "earlier-real-dispatch"
	now := time.Now()
	_, err = f.session.Observe(f.c.Request.Context(), keyrouting.AccountAttemptSample{Metadata: metadata, Dispatched: true, Outcome: keyrouting.OutcomeFailure, StatusCode: 503, UpstreamStatusCode: 503, StatusSource: keyrouting.AttemptStatusUpstream, StartedAt: now.Add(-time.Second), FinishedAt: now})
	require.NoError(t, err)
	writeKeyAccountUnavailable(f.c, "No eligible account remains")
	require.Equal(t, 503, f.c.Writer.Status())
	require.Equal(t, "600", f.c.Writer.Header().Get("Retry-After"))
	require.Empty(t, f.sends, "retry advice cannot generate a paid probe")
	require.Zero(t, f.budget.Dispatched())
}
