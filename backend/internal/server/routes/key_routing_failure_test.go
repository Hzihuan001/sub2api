//go:build unit

package routes

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Uses the registered ingress, real scheduler/forwarder and real billing path.
// Only repositories and the paid HTTP upstream are synthetic.
func TestKeyRoutingRegisteredFinalFailureAndCooldown(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		name                         string
		policy                       service.KeyRouteFailurePolicy
		status, failuresBeforeSwitch int
		stream                       bool
	}{
		{"consecutive failures", service.KeyRouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{{Type: keyrouting.FailureRuleConsecutiveFailures, Threshold: 3, CooldownSeconds: 120}}}, 503, 3, false},
		{"upstream timeout", service.KeyRouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{{Type: keyrouting.FailureRuleTimeout, SampleSize: 20, Threshold: 1, CooldownSeconds: 120}}}, 504, 1, false},
		{"SSE final error switches before output", service.KeyRouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{{Type: keyrouting.FailureRuleConsecutiveFailures, Threshold: 1, CooldownSeconds: 120}}}, 503, 1, true},
		{"explicitly disabled", service.KeyRouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{}}, 503, 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			groups := routedGroups{groups: make(map[int64]*service.Group)}
			accounts := routedAccounts{accounts: make(map[int64][]service.Account)}
			for i, platform := range []string{service.PlatformOpenAI, service.PlatformDeepseek} {
				id := int64(101 + i)
				groups.groups[id] = &service.Group{ID: id, Name: fmt.Sprintf("group-%d", id), Status: service.StatusActive, Hydrated: true, Platform: platform, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: float64(i+1) / 10}
				accounts.accounts[id] = []service.Account{{ID: int64(i + 1), Name: "synthetic route account", Platform: platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{id}, Credentials: map[string]any{"api_key": "synthetic-key", "base_url": "https://fixture.example", "model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}, Extra: map[string]any{openai_compat.ExtraKeyResponsesSupported: false}}}
			}
			owner := &service.User{ID: 301, Role: service.RoleUser, Status: service.StatusActive, Balance: 100}
			primary := int64(101)
			key := &service.APIKey{ID: 201, UserID: owner.ID, Key: "synthetic-route-key", Status: service.StatusActive, User: owner, GroupID: &primary, Group: groups.groups[primary], BackupGroupIDs: []int64{102}, KeyRouteFailurePolicy: &scenario.policy}
			keys := &routedKeys{keyBillingRouteAPIKeyRepo{apiKey: key}}
			rates := &routedRates{}
			cfg := &config.Config{RunMode: config.RunModeStandard}
			cfg.Default.RateMultiplier = 1
			cfg.Gateway.MaxBodySize = 1 << 20
			cfg.Gateway.TextMaxBodySize = 1 << 20
			users := routedUsers{user: owner}
			apiKeys := service.NewAPIKeyService(keys, users, groups, nil, rates, nil, cfg)
			cache := service.NewBillingCacheService(nil, users, nil, keys, nil, rates, cfg, nil)
			t.Cleanup(cache.Stop)
			upstream := &routedUpstream{stream: scenario.stream, failures: map[int64]int{1: scenario.status}}
			usage, billing := &routedUsage{}, &routedBilling{}
			concurrency := service.NewConcurrencyService(nil)
			gateway := service.NewOpenAIGatewayService(accounts, usage, billing, users, nil, rates, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, cache, upstream, &service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil)
			routing := service.NewKeyRoutingService(groups, rates, nil, gateway, nil, cfg)
			gatewayHandler := handler.NewOpenAIGatewayHandler(gateway, concurrency, cache, apiKeys, nil, nil, nil, nil, cfg)
			router := gin.New()
			RegisterGatewayRoutes(router, &handler.Handlers{Gateway: &handler.GatewayHandler{}, OpenAIGateway: gatewayHandler}, servermiddleware.ProvideAPIKeyAuthMiddleware(apiKeys, nil, cfg, routing), apiKeys, nil, nil, nil, nil, cfg)
			requests := scenario.failuresBeforeSwitch + 1
			for attempt := 1; attempt <= requests; attempt++ {
				request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(fmt.Sprintf(`{"model":"gpt-5.4","messages":[{"role":"user","content":"synthetic"}],"stream":%t}`, scenario.stream)))
				request.Header.Set("Authorization", "Bearer "+key.Key)
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				if scenario.failuresBeforeSwitch == 0 || attempt < scenario.failuresBeforeSwitch {
					require.GreaterOrEqual(t, recorder.Code, 400, recorder.Body.String())
				} else {
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.Contains(t, recorder.Body.String(), "routed answer")
				}
			}
			if scenario.failuresBeforeSwitch == 0 {
				require.Equal(t, []int64{1}, upstream.accountIDs)
				require.Empty(t, usage.logs)
				require.Empty(t, billing.commands)
				return
			}
			expectedAccounts := make([]int64, scenario.failuresBeforeSwitch)
			for i := range expectedAccounts {
				expectedAccounts[i] = 1
			}
			expectedAccounts = append(expectedAccounts, 2, 2)
			require.Equal(t, expectedAccounts, upstream.accountIDs, "one final failure per request; following request skips the cooled primary")
			require.Len(t, usage.logs, 2)
			require.Len(t, billing.commands, 2, "only successful group settles, never double debit")
			for _, log := range usage.logs {
				require.Equal(t, int64(102), *log.GroupID)
				require.InDelta(t, 0.2, log.RateMultiplier, 1e-9)
			}
			require.Equal(t, int64(101), *key.GroupID)
		})
	}
}
