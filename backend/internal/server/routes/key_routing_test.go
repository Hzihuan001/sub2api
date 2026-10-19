//go:build unit

package routes

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routedGroups struct {
	service.GroupRepository
	groups map[int64]*service.Group
}

func (r routedGroups) GetByIDLite(_ context.Context, id int64) (*service.Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, service.ErrGroupNotFound
	}
	return group, nil
}

type routedAccounts struct {
	service.AccountRepository
	accounts map[int64][]service.Account
}

func (r routedAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for _, accounts := range r.accounts {
		for _, account := range accounts {
			if account.ID == id {
				return &account, nil
			}
		}
	}
	return nil, service.ErrAccountNotFound
}
func (r routedAccounts) ListSchedulableByGroupIDAndPlatform(_ context.Context, id int64, _ string) ([]service.Account, error) {
	return append([]service.Account(nil), r.accounts[id]...), nil
}
func (r routedAccounts) ListModelAvailabilityCandidates(_ context.Context, id *int64, _ []string, _ bool) ([]service.Account, error) {
	return append([]service.Account(nil), r.accounts[*id]...), nil
}

type routedUsers struct {
	service.UserRepository
	user *service.User
}

func (r routedUsers) GetByID(context.Context, int64) (*service.User, error) { return r.user, nil }

type routedRates struct {
	service.UserGroupRateRepository
	override *float64
	groups   []int64
}

func (r *routedRates) GetByUserAndGroup(_ context.Context, _ int64, groupID int64) (*float64, error) {
	r.groups = append(r.groups, groupID)
	if groupID == 102 {
		return r.override, nil
	}
	return nil, nil
}
func (*routedRates) GetRPMOverrideByUserAndGroup(context.Context, int64, int64) (*int, error) {
	return nil, nil
}

type routedKeys struct{ keyBillingRouteAPIKeyRepo }

func (*routedKeys) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type routedBilling struct {
	service.UsageBillingRepository
	commands []*service.UsageBillingCommand
}

type routedUsage struct {
	service.UsageLogRepository
	logs []*service.UsageLog
}

func (r *routedUsage) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.logs = append(r.logs, log)
	return true, nil
}

func (b *routedBilling) Apply(_ context.Context, command *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	copy := *command
	b.commands = append(b.commands, &copy)
	balance := 100.0 - command.BalanceCost
	return &service.UsageBillingApplyResult{Applied: true, NewBalance: &balance}, nil
}

type routedUpstream struct {
	service.HTTPUpstream
	stream, interrupted bool
	accountIDs          []int64
	failures            map[int64]int
}

func (u *routedUpstream) Do(req *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.accountIDs = append(u.accountIDs, accountID)
	_, _ = io.Copy(io.Discard, req.Body)
	if status := u.failures[accountID]; status > 0 {
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"server_error","message":"synthetic upstream unavailable"}}`))}, nil
	}
	header := http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"routed-upstream"}}
	var body io.ReadCloser = io.NopCloser(strings.NewReader(`{"id":"chatcmpl-route","object":"chat.completion","model":"gpt-5.4","choices":[{"index":0,"message":{"role":"assistant","content":"routed answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`))
	if u.stream {
		header.Set("Content-Type", "text/event-stream")
		prefix := "data: {\"id\":\"chatcmpl-route\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"routed answer\"},\"finish_reason\":null}]}\n\n"
		if u.interrupted {
			body = &routedInterruptedBody{Reader: strings.NewReader(prefix)}
		} else {
			body = io.NopCloser(strings.NewReader(prefix + "data: {\"id\":\"chatcmpl-route\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-5.4\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}\n\ndata: [DONE]\n\n"))
		}
	}
	return &http.Response{StatusCode: http.StatusOK, Header: header, Body: body}, nil
}

type routedInterruptedBody struct{ *strings.Reader }

func (r *routedInterruptedBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}
func (*routedInterruptedBody) Close() error { return nil }

func TestKeyRoutingRegisteredGatewayForwardAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name                                                   string
		unsupported, allCooling, stream, interrupted, userRate bool
	}{
		{name: "unsupported primary HTTP", unsupported: true},
		{name: "cooling primary SSE", stream: true},
		{name: "selected group user rate", unsupported: true, userRate: true},
		{name: "all groups cooling sends nothing", allCooling: true},
		{name: "partial SSE never crosses groups", stream: true, interrupted: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			groups := routedGroups{groups: map[int64]*service.Group{}}
			accounts := routedAccounts{accounts: map[int64][]service.Account{}}
			cooldown := time.Now().Add(time.Hour)
			for i, id := range []int64{101, 102, 103} {
				groups.groups[id] = &service.Group{ID: id, Status: service.StatusActive, Hydrated: true, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 0.5}
				account := service.Account{ID: int64(i + 1), Name: "route account", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{id}, Credentials: map[string]any{"api_key": "synthetic-key", "base_url": "https://fixture.example", "model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}, Extra: map[string]any{openai_compat.ExtraKeyResponsesSupported: false}}
				if id == 101 || scenario.allCooling {
					account.TempUnschedulableUntil = &cooldown
				}
				if id == 101 && scenario.unsupported {
					account.TempUnschedulableUntil = nil
					account.Credentials["model_mapping"] = map[string]any{"other-model": "other-model"}
				}
				accounts.accounts[id] = []service.Account{account}
			}
			groups.groups[101].RateMultiplier = 9
			owner := &service.User{ID: 301, Role: service.RoleUser, Status: service.StatusActive, Balance: 100}
			primary := int64(101)
			key := &service.APIKey{ID: 201, UserID: owner.ID, Key: "routed-key", Status: service.StatusActive, User: owner, GroupID: &primary, Group: groups.groups[primary], BackupGroupIDs: []int64{102, 103}}
			keys := &routedKeys{keyBillingRouteAPIKeyRepo{apiKey: key}}
			rates := &routedRates{}
			expectedRate := 0.5
			if scenario.userRate {
				expectedRate = 0.25
				rates.override = &expectedRate
			}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxBodySize = 1 << 20
			cfg.Gateway.TextMaxBodySize = 1 << 20
			users := routedUsers{user: owner}
			apiKeys := service.NewAPIKeyService(keys, users, groups, nil, rates, nil, cfg)
			cache := service.NewBillingCacheService(nil, users, nil, keys, nil, rates, cfg, nil)
			t.Cleanup(cache.Stop)
			upstream := &routedUpstream{stream: scenario.stream, interrupted: scenario.interrupted}
			usage, billing := &routedUsage{}, &routedBilling{}
			concurrency := service.NewConcurrencyService(nil)
			gateway := service.NewOpenAIGatewayService(accounts, usage, billing, users, nil, rates, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, cache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			routing := service.NewKeyRoutingService(groups, rates, nil, gateway, nil, cfg)
			gatewayHandler := handler.NewOpenAIGatewayHandler(gateway, concurrency, cache, apiKeys, nil, nil, nil, nil, cfg)
			router := gin.New()
			RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: gatewayHandler}, servermiddleware.ProvideAPIKeyAuthMiddleware(apiKeys, nil, cfg, routing), apiKeys, nil, nil, nil, nil, cfg)
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":%t}`, scenario.stream)))
			request.Header.Set("Authorization", "Bearer "+key.Key)
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if scenario.allCooling {
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
				require.Empty(t, upstream.accountIDs)
				require.Empty(t, billing.commands)
				require.Empty(t, usage.logs)
				return
			}
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), "routed answer")
			require.Equal(t, []int64{2}, upstream.accountIDs, "forward once inside the selected backup; never replay into the third group")
			require.Equal(t, int64(101), *key.GroupID, "request routing must not mutate the persisted/authenticated key")
			if scenario.interrupted {
				require.LessOrEqual(t, len(billing.commands), 1)
				return
			}
			require.Len(t, usage.logs, 1)
			log := usage.logs[0]
			require.Equal(t, int64(102), *log.GroupID)
			require.Equal(t, key.ID, log.APIKeyID)
			require.Equal(t, 100, log.InputTokens)
			require.Equal(t, 20, log.OutputTokens)
			require.Positive(t, log.TotalCost)
			require.InDelta(t, expectedRate, log.RateMultiplier, 1e-9)
			require.InDelta(t, log.TotalCost*expectedRate, log.ActualCost, 1e-8)
			require.Len(t, billing.commands, 1, "real RecordUsage must settle exactly once")
			require.Equal(t, key.ID, billing.commands[0].APIKeyID)
			require.InDelta(t, log.ActualCost, billing.commands[0].BalanceCost, 1e-8)
			require.Contains(t, rates.groups, int64(102), "resolve the selected group's owner-specific rate")
		})
	}
}
