package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type keyRoutingReadinessRepo struct {
	AccountRepository
	accounts []Account
	err      error
	reads    int
}

func (r *keyRoutingReadinessRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	r.reads++
	return append([]Account(nil), r.accounts...), r.err
}

func (r *keyRoutingReadinessRepo) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	r.reads++
	return append([]Account(nil), r.accounts...), r.err
}

func (r *keyRoutingReadinessRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, nil
}

func keyRoutingReadyAccount(platform string) Account {
	return Account{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive,
		Schedulable: true, Concurrency: 1, AccountGroups: []AccountGroup{{GroupID: 7}}, GroupIDs: []int64{7}}
}

func TestKeyRoutingReadinessEligibility(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			tests := []struct {
				name        string
				mutate      func(*Account, *Group)
				noAccounts  bool
				profitVeto  bool
				wantModel   bool
				wantAccount bool
			}{
				{name: "ordinary account", wantModel: true, wantAccount: true},
				{name: "empty pool means no accounts", noAccounts: true, wantModel: true},
				{name: "paused account", mutate: func(a *Account, _ *Group) { a.Schedulable = false }, wantModel: true},
				{name: "disabled account", mutate: func(a *Account, _ *Group) { a.Status = "disabled" }, wantModel: true},
				{name: "overload cooling", mutate: func(a *Account, _ *Group) { until := time.Now().Add(time.Hour); a.OverloadUntil = &until }, wantModel: true},
				{name: "temporarily unschedulable", mutate: func(a *Account, _ *Group) { until := time.Now().Add(time.Hour); a.TempUnschedulableUntil = &until }, wantModel: true},
				{name: "unsupported model", mutate: func(a *Account, _ *Group) {
					a.Credentials = map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}
				}},
				{name: "requires privacy", mutate: func(_ *Account, g *Group) { g.RequirePrivacySet = true }, wantModel: true, wantAccount: platform != PlatformOpenAI},
				{name: "profit gate", profitVeto: true, wantModel: true},
				{name: "another group does not leak", mutate: func(a *Account, _ *Group) { a.AccountGroups = []AccountGroup{{GroupID: 8}}; a.GroupIDs = []int64{8} }, wantModel: true},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					account := keyRoutingReadyAccount(platform)
					group := &Group{ID: 7, Platform: platform, Hydrated: true, Status: StatusActive}
					if tt.mutate != nil {
						tt.mutate(&account, group)
					}
					repo := &keyRoutingReadinessRepo{accounts: []Account{account}}
					if tt.noAccounts {
						repo.accounts = nil
					}
					ctx := context.WithValue(context.Background(), ctxkey.Group, group)
					if tt.profitVeto {
						ctx = context.WithValue(ctx, openAIProfitControlGateCtxKey{}, &openAIProfitControlGate{groupID: 7, platform: platform, threshold: 0.5})
					}
					var supports, ready bool
					var err error
					if platform == PlatformOpenAI {
						svc := &OpenAIGatewayService{accountRepo: repo, concurrencyService: &ConcurrencyService{}}
						supports, ready, err = svc.keyRoutingOpenAIReadiness(ctx, group, platform, "text-model", CompositeRouteEndpointResponses)
					} else {
						svc := &GatewayService{accountRepo: repo, concurrencyService: &ConcurrencyService{}}
						supports, ready, err = svc.keyRoutingNativeReadiness(ctx, group, platform, "text-model", CompositeRouteEndpointMessages)
					}
					if err != nil || supports != tt.wantModel || ready != tt.wantAccount {
						t.Fatalf("readiness = (%v, %v, %v), want (%v, %v, nil)", supports, ready, err, tt.wantModel, tt.wantAccount)
					}
					if repo.reads != 1 {
						t.Fatalf("unexpected read/selection activity: %d reads", repo.reads)
					}
				})
			}
		})
	}
}

func TestKeyRoutingReadinessErrorsAndCancellation(t *testing.T) {
	readErr := errors.New("account lookup unavailable")
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		for _, canceled := range []bool{false, true} {
			t.Run(platform+"/"+map[bool]string{false: "read_error", true: "canceled"}[canceled], func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				wantErr, wantReads := readErr, 1
				if canceled {
					cancel()
					wantErr, wantReads = context.Canceled, 0
				}
				repo := &keyRoutingReadinessRepo{err: readErr}
				group := &Group{ID: 7, Platform: platform}
				var supports, ready bool
				var err error
				if platform == PlatformOpenAI {
					supports, ready, err = (&OpenAIGatewayService{accountRepo: repo}).keyRoutingOpenAIReadiness(ctx, group, platform, "text-model", CompositeRouteEndpointChatCompletions)
				} else {
					supports, ready, err = (&GatewayService{accountRepo: repo}).keyRoutingNativeReadiness(ctx, group, platform, "text-model", CompositeRouteEndpointMessages)
				}
				if !errors.Is(err, wantErr) || supports || ready || repo.reads != wantReads {
					t.Fatalf("readiness = (%v, %v, %v), reads=%d; want error %v and %d reads", supports, ready, err, repo.reads, wantErr, wantReads)
				}
			})
		}
	}
}

func TestKeyRoutingReadinessOpenAITextEndpointCapability(t *testing.T) {
	for _, endpoint := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointMessages, CompositeRouteEndpointChatCompletions} {
		t.Run(endpoint, func(t *testing.T) {
			account := keyRoutingReadyAccount(PlatformOpenAI)
			account.Extra = map[string]any{"openai_responses_supported": false}
			repo := &keyRoutingReadinessRepo{accounts: []Account{account}}
			svc := &OpenAIGatewayService{accountRepo: repo}
			group := &Group{ID: 7, Platform: PlatformOpenAI}
			supports, ready, err := svc.keyRoutingOpenAIReadiness(context.Background(), group, PlatformOpenAI, "text-model", endpoint)
			if err != nil || !supports || !ready {
				t.Fatalf("text endpoint must retain chat fallback: (%v, %v, %v)", supports, ready, err)
			}
			account.Credentials = map[string]any{"openai_capabilities": []any{"embeddings"}}
			repo.accounts = []Account{account}
			supports, ready, err = svc.keyRoutingOpenAIReadiness(context.Background(), group, PlatformOpenAI, "text-model", endpoint)
			if err != nil || !supports || ready {
				t.Fatalf("embeddings-only account must not serve text: (%v, %v, %v)", supports, ready, err)
			}
		})
	}
}

func TestKeyRoutingReadinessThresholdDoesNotPersistPause(t *testing.T) {
	account := keyRoutingReadyAccount(PlatformOpenAI)
	account.Credentials = map[string]any{accountSchedulingThresholdCredentialKey: 80}
	account.Extra = map[string]any{"codex_7d_used_percent": 95.0, "codex_7d_reset_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
	repo := &keyRoutingReadinessRepo{accounts: []Account{account}}
	// Unimplemented repository mutations panic through the embedded nil
	// interface, so a future accidental ApplyAccountSchedulingThreshold fails.
	limiter := &RateLimitService{accountRepo: repo, settingService: &SettingService{}}
	svc := &OpenAIGatewayService{accountRepo: repo, rateLimitService: limiter}
	supports, ready, err := svc.keyRoutingOpenAIReadiness(context.Background(), &Group{ID: 7, Platform: PlatformOpenAI}, PlatformOpenAI, "text-model", CompositeRouteEndpointResponses)
	if err != nil || !supports || ready || repo.accounts[0].TempUnschedulableUntil != nil {
		t.Fatalf("threshold must only veto, without mutation: (%v, %v, %v), account=%+v", supports, ready, err, repo.accounts[0])
	}
}

func TestKeyRoutingReadinessKeepsLocalCooldownWithStaleAccount(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		t.Run(platform, func(t *testing.T) {
			account := keyRoutingReadyAccount(platform)
			repo := &keyRoutingReadinessRepo{accounts: []Account{account}}
			limiter := &RateLimitService{accountRepo: repo}
			now := time.Now()
			limiter.overloadFailures.rememberCooldown(account.ID, now, now.Add(time.Hour))
			group := &Group{ID: 7, Platform: platform}
			var supports, ready bool
			var err error
			if platform == PlatformOpenAI {
				supports, ready, err = (&OpenAIGatewayService{accountRepo: repo, rateLimitService: limiter}).keyRoutingOpenAIReadiness(context.Background(), group, platform, "text-model", CompositeRouteEndpointResponses)
			} else {
				supports, ready, err = (&GatewayService{accountRepo: repo, rateLimitService: limiter}).keyRoutingNativeReadiness(context.Background(), group, platform, "text-model", CompositeRouteEndpointMessages)
			}
			if err != nil || !supports || ready || !limiter.IsAccountOverloadCooling(account.ID, now) {
				t.Fatalf("local cooldown must remain effective: (%v, %v, %v)", supports, ready, err)
			}
		})
	}
}

type keyRoutingReadinessWindowCache struct {
	SessionLimitCache
	cost float64
	err  error
}

func (c *keyRoutingReadinessWindowCache) GetWindowCost(context.Context, int64) (float64, bool, error) {
	return c.cost, true, c.err
}

type keyRoutingReadinessRPMCache struct {
	RPMCache
	rpm int
	err error
}

func (c *keyRoutingReadinessRPMCache) GetRPM(context.Context, int64) (int, error) {
	return c.rpm, c.err
}

func TestKeyRoutingReadinessNativeLimitsReadOnly(t *testing.T) {
	readErr := errors.New("runtime cache unavailable")
	for _, tt := range []struct {
		name    string
		cost    float64
		rpm     int
		costErr error
		rpmErr  error
		want    bool
		wantErr error
	}{
		{name: "limits allow", cost: 1, rpm: 1, want: true},
		{name: "window exhausted", cost: 100, rpm: 1},
		{name: "rpm exhausted", cost: 1, rpm: 100},
		{name: "window read failure", costErr: readErr, wantErr: readErr},
		{name: "rpm read failure", cost: 1, rpmErr: readErr, wantErr: readErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			account := keyRoutingReadyAccount(PlatformAnthropic)
			account.Type = AccountTypeOAuth
			account.Extra = map[string]any{"window_cost_limit": 10.0, "base_rpm": 10, "max_sessions": 1}
			svc := &GatewayService{
				sessionLimitCache:  &keyRoutingReadinessWindowCache{cost: tt.cost, err: tt.costErr},
				rpmCache:           &keyRoutingReadinessRPMCache{rpm: tt.rpm, err: tt.rpmErr},
				concurrencyService: &ConcurrencyService{},
			}
			got, err := svc.keyRoutingNativeLimits(context.Background(), &account)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("limits = (%v, %v), want (%v, %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestKeyRoutingReadinessFreshAccountErrorsRemainErrors(t *testing.T) {
	readErr := errors.New("fresh account lookup failed")
	repo := &keyRoutingReadinessRepo{err: readErr}
	account := keyRoutingReadyAccount(PlatformOpenAI)
	got, err := keyRoutingFreshAccount(context.Background(), &SchedulerSnapshotService{accountRepo: repo}, &account)
	if got != nil || !errors.Is(err, readErr) {
		t.Fatalf("fresh read = (%v, %v), want original read error", got, err)
	}
}

func TestKeyRoutingReadinessFreshSnapshotAvoidsDatabaseRead(t *testing.T) {
	account := keyRoutingReadyAccount(PlatformOpenAI)
	fresh := account
	fresh.Schedulable = false
	repo := &keyRoutingReadinessRepo{err: errors.New("must not query database on snapshot hit")}
	cache := &openAISnapshotCacheStub{accountsByID: map[int64]*Account{account.ID: &fresh}}
	got, err := keyRoutingFreshAccount(context.Background(), &SchedulerSnapshotService{cache: cache, accountRepo: repo}, &account)
	if err != nil || got == nil || got.Schedulable || repo.reads != 0 {
		t.Fatalf("fresh snapshot must win without SQL: got=%+v error=%v reads=%d", got, err, repo.reads)
	}
}
