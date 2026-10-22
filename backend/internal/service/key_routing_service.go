package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// KeyRoutingRequest describes a stateless text request before any upstream
// attempt. HTTP parsing and stateful/media exclusions belong to the ingress
// adapter; priority selection belongs to internal/keyrouting.
type KeyRoutingRequest struct {
	Model    string
	Endpoint string
}

// KeyRoutingHTTPResponseSourceResolver is intentionally narrow: ingress uses
// it only for a Responses previous_response_id continuation. The resolver
// must return the authorized source group and the account that created the
// response; no arbitrary cross-group or cross-account fallback is allowed.
type KeyRoutingHTTPResponseSourceResolver interface {
	SelectHTTPResponseSource(context.Context, *APIKey, KeyRoutingRequest, string) (*APIKey, int64, error)
}

func keyRoutingRequestActive(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(ctxkey.KeyRoutingActive).(bool)
	return active
}

// KeyRoutingService adapts the independent planner to existing group policy
// and schedulers. It does not forward, acquire slots, debit balances, or mutate
// cached API keys. Single-group keys never enter this service.
type KeyRoutingService struct {
	groups    GroupRepository
	rates     UserGroupRateRepository
	native    *GatewayService
	openai    *OpenAIGatewayService
	composite *CompositeRouteResolver
	cfg       *config.Config
	settings  *SettingService
	failures  *keyrouting.FailureTracker
	accounts  *KeyAccountRoutingRuntime
	policyMu  sync.RWMutex
	policy    map[int64]cachedKeyRouteFailurePolicy
}

type cachedKeyRouteFailurePolicy struct {
	value     keyrouting.RouteFailurePolicy
	expiresAt time.Time
}

const keyRouteFailurePolicyCacheTTL = 2 * time.Second

func NewKeyRoutingService(groups GroupRepository, rates UserGroupRateRepository, native *GatewayService, openai *OpenAIGatewayService, composite *CompositeRouteResolver, cfg *config.Config, settings ...*SettingService) *KeyRoutingService {
	var settingService *SettingService
	if len(settings) > 0 {
		settingService = settings[0]
	}
	service := &KeyRoutingService{groups: groups, rates: rates, native: native, openai: openai, composite: composite, cfg: cfg, settings: settingService, failures: keyrouting.NewFailureTracker(), policy: make(map[int64]cachedKeyRouteFailurePolicy)}
	if settingService != nil {
		settingService.SetGroupFailurePolicyChangedListener(service.InvalidateGroupFailurePolicy)
	}
	return service
}

// InvalidateGroupFailurePolicy immediately removes the cached policy and all
// route-local counters for a group after an administrator changes its rules.
// It never changes account scheduling state.
func (s *KeyRoutingService) InvalidateGroupFailurePolicy(groupID int64) {
	if s == nil || groupID <= 0 {
		return
	}
	s.policyMu.Lock()
	delete(s.policy, groupID)
	s.policyMu.Unlock()
	if s.failures != nil {
		s.failures.ClearGroup(groupID)
	}
}

func (s *KeyRoutingService) Select(ctx context.Context, key *APIKey, request KeyRoutingRequest) (*APIKey, keyrouting.Plan, error) {
	return s.selectFrom(ctx, key, request, 0, false)
}

// SelectHTTPResponseSource resolves a previous_response_id in the API key's
// configured group scopes, then performs readiness checks against that group
// only. The returned source account is carried separately so the Responses
// handler can enforce account stickiness after scheduler selection.
func (s *KeyRoutingService) SelectHTTPResponseSource(ctx context.Context, key *APIKey, request KeyRoutingRequest, responseID string) (*APIKey, int64, error) {
	if s == nil || s.openai == nil || key == nil || key.GroupID == nil || key.User == nil {
		return nil, 0, ErrOpenAIHTTPResponseSourceUnavailable
	}
	groupIDs := append([]int64{*key.GroupID}, key.BackupGroupIDs...)
	groupID, accountID, err := s.openai.ResolveOpenAIHTTPResponseSource(ctx, groupIDs, responseID, key.UserID, key.ID)
	if err != nil {
		return nil, 0, err
	}
	source := *key
	source.GroupID = &groupID
	source.Group = nil
	source.BackupGroupIDs = nil
	// A stored response may outlive the source account's schedulable window. We
	// still need the authorized source-group snapshot so the Responses handler
	// can attempt a capability-gated provider replay before falling back to the
	// normal multi-group route. Readiness is checked again by the handler.
	effective, _, selectErr := s.selectFrom(ctx, &source, request, 0, true)
	if selectErr != nil {
		return nil, 0, selectErr
	}
	if effective == nil || effective.GroupID == nil || *effective.GroupID != groupID {
		return nil, 0, ErrOpenAIHTTPResponseSourceUnavailable
	}
	// Continue only with groups after the source group. The source response
	// cannot be submitted to a group that precedes its owner without an explicit
	// provider replay, and once replayed the source group should be preferred
	// before its configured backups.
	routeIDs := append([]int64{derefGroupID(key.GroupID)}, key.BackupGroupIDs...)
	for index, candidate := range routeIDs {
		if candidate == groupID {
			effective.BackupGroupIDs = append([]int64(nil), routeIDs[index+1:]...)
			break
		}
	}
	return effective, accountID, nil
}

// SelectNext evaluates only groups after currentGroupID. It is used by the
// gateway after a pre-first-byte upstream failure; the original key snapshot
// remains untouched and the planner still applies the configured priority.
func (s *KeyRoutingService) SelectNext(ctx context.Context, key *APIKey, request KeyRoutingRequest, currentGroupID int64) (*APIKey, keyrouting.Plan, error) {
	start := -1
	if key != nil && key.GroupID != nil {
		ids := append([]int64{*key.GroupID}, key.BackupGroupIDs...)
		for index, id := range ids {
			if id == currentGroupID {
				start = index + 1
				break
			}
		}
	}
	if start < 0 {
		return nil, keyrouting.Plan{}, fmt.Errorf("%w: current group is outside the configured route", keyrouting.ErrInvalidConfig)
	}
	return s.selectFrom(ctx, key, request, start, false)
}

// RecordFailure records a configured group rule without touching account or
// scheduler state. It returns whether the current group should be skipped for
// subsequent attempts and requests.
func (s *KeyRoutingService) RecordFailure(ctx context.Context, key *APIKey, request KeyRoutingRequest, groupID, statusCode int64) (keyrouting.RouteFailureResult, error) {
	result := keyrouting.RouteFailureResult{RuleIndex: -1}
	if key != nil && key.KeyRouteScope == KeyRouteScopeAccountV2 {
		return result, nil // Each real dispatch is observed by the account session.
	}
	if s == nil || s.failures == nil || key == nil || key.ID <= 0 || groupID <= 0 || statusCode < 100 || statusCode > 599 || (s.settings == nil && key.KeyRouteFailurePolicy == nil) {
		return result, nil
	}
	routePolicy, err := s.loadRouteFailurePolicy(ctx, key, groupID)
	if err != nil {
		return result, err
	}
	return s.failures.Record(keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: groupID, Model: request.Model, Endpoint: request.Endpoint}, int(statusCode), routePolicy), nil
}

// RecordOutcome observes a completed attempt without changing account state.
// Semantic outcomes intentionally share the same key/group/model/endpoint
// identity as exact-status rules, while the independent tracker owns counters.
func (s *KeyRoutingService) RecordOutcome(ctx context.Context, key *APIKey, request KeyRoutingRequest, groupID int64, kind keyrouting.RouteOutcomeKind) (keyrouting.RouteFailureResult, error) {
	result := keyrouting.RouteFailureResult{RuleIndex: -1}
	if key != nil && key.KeyRouteScope == KeyRouteScopeAccountV2 {
		return result, nil
	}
	if s == nil || s.failures == nil || key == nil || key.ID <= 0 || groupID <= 0 || (s.settings == nil && key.KeyRouteFailurePolicy == nil) {
		return result, nil
	}
	policy, err := s.loadRouteFailurePolicy(ctx, key, groupID)
	if err != nil {
		return result, err
	}
	return s.failures.RecordOutcome(keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: groupID, Model: request.Model, Endpoint: request.Endpoint}, kind, policy), nil
}

// PrepareRouteContext resolves the concrete destination for an already
// selected request-local key. Callers provide the clean authentication context
// before re-entering the native handler, which rebuilds its own pricing state.
// A concrete group's platform is authoritative even for an ambiguous alias.
func (s *KeyRoutingService) PrepareRouteContext(ctx context.Context, key *APIKey, request KeyRoutingRequest) (context.Context, string, error) {
	if ctx == nil || key == nil || key.GroupID == nil || key.Group == nil || key.Group.ID != *key.GroupID || strings.TrimSpace(request.Model) == "" {
		return ctx, "", keyrouting.ErrInvalidConfig
	}
	if key.Group.Platform != PlatformComposite {
		return WithResolvedTargetPlatform(ctx, key.Group.Platform), request.Model, nil
	}
	if s == nil || s.composite == nil {
		return ctx, "", fmt.Errorf("composite route resolver is unavailable")
	}
	decision, err := s.composite.Resolve(ctx, *key.GroupID, request.Model, request.Endpoint)
	if err != nil {
		return ctx, "", err
	}
	if !decision.Matched {
		return ctx, "", keyrouting.ErrModelUnavailable
	}
	model := strings.TrimSpace(decision.UpstreamModel)
	if model == "" {
		model = request.Model
	}
	return WithCompositeRouteDecision(ctx, decision), model, nil
}

func (s *KeyRoutingService) selectFrom(ctx context.Context, key *APIKey, request KeyRoutingRequest, start int, allowUnavailableSource bool) (*APIKey, keyrouting.Plan, error) {
	if key == nil || key.User == nil || key.GroupID == nil || request.Model == "" {
		return nil, keyrouting.Plan{}, keyrouting.ErrInvalidConfig
	}
	if s == nil || s.groups == nil || (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) {
		return nil, keyrouting.Plan{}, fmt.Errorf("key routing is unavailable in this configuration")
	}
	groups := make(map[int64]*Group, 1+len(key.BackupGroupIDs))
	// A cooled candidate may be used only as a last-resort request-local
	// fallback. Remember it only after native readiness succeeds, so ignoring
	// route cooldown never bypasses permission, model, account or quota gates.
	var lastCoolingGroupID int64
	ignoreRouteCooldown := false
	planner := keyrouting.Planner{Evaluator: keyrouting.EvaluatorFunc(func(ctx context.Context, groupID int64, req keyrouting.Request) (keyrouting.CandidateState, error) {
		state := keyrouting.CandidateState{Authorized: true}
		group, err := s.groups.GetByIDLite(ctx, groupID)
		if errors.Is(err, ErrGroupNotFound) {
			return state, nil
		}
		if err != nil {
			return state, err
		}
		if group == nil {
			return state, fmt.Errorf("group %d lookup returned no result", groupID)
		}
		groups[groupID] = group
		// Claude client identity is established later by the native handler.
		// Its internal fallback cannot be safely preflighted at the auth seam.
		if group.ClaudeCodeOnly {
			return state, fmt.Errorf("%w: Claude-client-only groups require a single-group key", keyrouting.ErrInvalidConfig)
		}
		state.Authorized = !group.IsSubscriptionType() && key.User.CanBindGroup(group.ID, group.IsExclusive)
		state.Enabled = group.IsActive()
		if !state.Authorized || !state.Enabled {
			return state, nil
		}
		state.SupportsModel = !group.ModelAllowlistEnabled() || group.ModelAllowlist.Allows(req.Model)
		if !state.SupportsModel {
			return state, nil
		}
		candidateCtx := context.WithValue(ctx, ctxkey.Group, group)
		candidateCtx = context.WithValue(candidateCtx, ctxkey.UserID, key.UserID)
		platform, model := group.Platform, req.Model
		if platform == PlatformComposite {
			if s.composite == nil {
				return state, fmt.Errorf("composite route resolver is unavailable")
			}
			decision, err := s.composite.Resolve(candidateCtx, groupID, model, req.Endpoint)
			if err != nil {
				return state, err
			}
			if !decision.Matched {
				state.SupportsModel = false
				return state, nil
			}
			candidateCtx = WithCompositeRouteDecision(candidateCtx, decision)
			platform, model = decision.TargetPlatform, decision.UpstreamModel
		}
		state.SupportsEndpoint = keyRoutingEndpointAllowed(group, platform, req.Endpoint)
		if !state.SupportsEndpoint {
			return state, nil
		}
		if allowUnavailableSource {
			// The response owner has already proven that this group produced the
			// stored response. Do not require a currently schedulable account here;
			// a provider retrieval adapter may still be able to read the response
			// and replay it on another account. Authorization, active state,
			// endpoint and model allowlist checks above remain mandatory.
			state.SupportsModel = true
			state.HasSchedulableAccounts = true
		} else if domain.UsesOpenAIGateway(platform) {
			if s.openai == nil {
				return state, fmt.Errorf("OpenAI-compatible scheduler is unavailable")
			}
			model, err = s.keyRoutingOpenAISchedulingModel(candidateCtx, group, platform, model, req.Endpoint)
			if err != nil {
				return state, err
			}
			candidateCtx, _ = s.openai.WithOpenAIRequestPricingContext(candidateCtx, &groupID)
			state.SupportsModel, state.HasSchedulableAccounts, err = s.openai.keyRoutingOpenAIReadiness(candidateCtx, group, platform, model, req.Endpoint)
		} else {
			if s.native == nil {
				return state, fmt.Errorf("native scheduler is unavailable")
			}
			candidateCtx, _ = WithGatewayTokenRequestPricing(candidateCtx)
			candidateCtx = s.native.withGatewayProfitControlGate(candidateCtx, &groupID)
			state.SupportsModel, state.HasSchedulableAccounts, err = s.native.keyRoutingNativeReadiness(candidateCtx, group, platform, model, req.Endpoint)
		}
		if !allowUnavailableSource && key.KeyRouteScope != KeyRouteScopeAccountV2 && err == nil && state.SupportsModel && state.HasSchedulableAccounts && !ignoreRouteCooldown && s.failures != nil && (s.settings != nil || key.KeyRouteFailurePolicy != nil) {
			if policy, policyErr := s.loadRouteFailurePolicy(ctx, key, groupID); policyErr == nil {
				if cooling, _ := s.failures.IsCooling(keyrouting.RouteFailureIdentity{APIKeyID: key.ID, GroupID: groupID, Model: req.Model, Endpoint: req.Endpoint}, policy); cooling {
					lastCoolingGroupID = groupID
					state.HasSchedulableAccounts = false
				}
			}
		}
		return state, err
	})}
	plan, err := planner.PlanFrom(ctx, keyrouting.Config{PrimaryGroupID: *key.GroupID, BackupGroupIDs: key.BackupGroupIDs}, keyrouting.Request{Model: request.Model, Endpoint: request.Endpoint}, start)
	if errors.Is(err, keyrouting.ErrNoSchedulableAccounts) && lastCoolingGroupID > 0 {
		// Retry only the last otherwise-eligible candidate from this traversal.
		// SelectNext's cursor therefore cannot wrap to a previously attempted
		// group. The cooldown itself is retained for all subsequent requests.
		ignoreRouteCooldown = true
		fallback, fallbackErr := planner.Plan(ctx, keyrouting.Config{PrimaryGroupID: lastCoolingGroupID}, keyrouting.Request{Model: request.Model, Endpoint: request.Endpoint})
		plan.Attempts = append(plan.Attempts, fallback.Attempts...)
		if fallbackErr != nil {
			return nil, plan, fallbackErr
		}
		plan.SelectedGroupID = fallback.SelectedGroupID
		plan.Reason = keyrouting.ReasonNoSchedulableAccounts
		err = nil
	}
	if err != nil {
		return nil, plan, err
	}
	group := groups[plan.SelectedGroupID]
	if group == nil {
		return nil, plan, fmt.Errorf("selected route has no group snapshot")
	}
	// Group and User are request-local. In particular the primary group's RPM
	// override must never leak into the backup's billing/rate-limit checks.
	effective := *key
	groupSnapshot := *group
	userSnapshot := *key.User
	id := group.ID
	effective.GroupID, effective.Group, effective.User = &id, &groupSnapshot, &userSnapshot
	effective.BackupGroupIDs = append([]int64(nil), key.BackupGroupIDs...)
	if id != *key.GroupID {
		userSnapshot.UserGroupRPMOverride = nil
		if s.rates != nil {
			override, err := s.rates.GetRPMOverrideByUserAndGroup(ctx, key.UserID, id)
			if err != nil {
				return nil, plan, fmt.Errorf("load selected group RPM policy: %w", err)
			}
			if override != nil {
				value := *override
				userSnapshot.UserGroupRPMOverride = &value
			}
		}
	}
	return &effective, plan, nil
}

func (s *KeyRoutingService) loadRouteFailurePolicy(ctx context.Context, key *APIKey, groupID int64) (keyrouting.RouteFailurePolicy, error) {
	policy := keyrouting.RouteFailurePolicy{}
	// A non-nil key policy is an explicit override. An empty rules array
	// therefore disables group rules for this key instead of falling back.
	if key != nil && key.KeyRouteFailurePolicy != nil {
		return cloneKeyRouteFailurePolicy(*key.KeyRouteFailurePolicy.Public()), nil
	}
	if s == nil || s.settings == nil {
		return policy, nil
	}
	now := time.Now()
	s.policyMu.RLock()
	cached, ok := s.policy[groupID]
	s.policyMu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return cloneKeyRouteFailurePolicy(cached.value), nil
	}
	groupPolicy, err := s.settings.GetGroupFailurePolicy(ctx, groupID)
	if err != nil {
		return policy, err
	}
	if groupPolicy == nil {
		return policy, nil
	}
	for _, rule := range groupPolicy.Public().StatusRules {
		policy.Rules = append(policy.Rules, keyrouting.RouteFailureRule{
			StatusCodes: append([]int(nil), rule.StatusCodes...), Threshold: rule.Threshold,
			WindowSeconds: rule.WindowSeconds, CooldownSeconds: rule.CooldownSeconds,
		})
	}
	s.policyMu.Lock()
	if s.policy == nil {
		s.policy = make(map[int64]cachedKeyRouteFailurePolicy)
	}
	s.policy[groupID] = cachedKeyRouteFailurePolicy{value: cloneKeyRouteFailurePolicy(policy), expiresAt: now.Add(keyRouteFailurePolicyCacheTTL)}
	s.policyMu.Unlock()
	return policy, nil
}

func cloneKeyRouteFailurePolicy(policy keyrouting.RouteFailurePolicy) keyrouting.RouteFailurePolicy {
	clone := keyrouting.RouteFailurePolicy{Rules: make([]keyrouting.RouteFailureRule, len(policy.Rules))}
	for i, rule := range policy.Rules {
		clone.Rules[i] = rule
		clone.Rules[i].StatusCodes = append([]int(nil), rule.StatusCodes...)
	}
	return clone
}

// Match the model passed to SelectAccountWithSchedulerForCapability by each
// text handler. The public model was already checked against the group allowlist
// before composite, dispatch, or channel rewrites are considered here.
func (s *KeyRoutingService) keyRoutingOpenAISchedulingModel(ctx context.Context, group *Group, platform, model, endpoint string) (string, error) {
	if endpoint == CompositeRouteEndpointMessages {
		// Messages schedules with its group dispatch model, then applies channel
		// forwarding mappings after selection. Composite Grok/CN targets bypass
		// the parent's OpenAI-only dispatch defaults, just like the handler.
		if group.Platform != PlatformComposite || (platform != PlatformGrok && !IsMultiProtocolAPIKeyProvider(platform)) {
			if mapped := strings.TrimSpace(group.ResolveMessagesDispatchModel(model)); mapped != "" {
				return mapped, nil
			}
		}
		return NormalizeOpenAICompatRequestedModel(model), nil
	}
	if endpoint != CompositeRouteEndpointResponses && endpoint != CompositeRouteEndpointChatCompletions {
		return model, nil
	}
	// ResolveChannelMapping's public convenience wrapper intentionally fails
	// open. Use the same lookup and mapping implementation while retaining its
	// read error, so routing cannot turn a failed read into a backup decision.
	channel, err := keyRoutingReadChannel(ctx, s.openai.channelService, group.ID)
	if err != nil {
		return "", fmt.Errorf("read key routing channel mapping: %w", err)
	}
	if channel != nil {
		mapping := resolveMapping(channel, group.ID, model)
		if mapping.Mapped {
			if mapped := strings.TrimSpace(mapping.MappedModel); mapped != "" {
				return mapped, nil
			}
		}
	}
	return model, nil
}

func keyRoutingEndpointAllowed(group *Group, platform, endpoint string) bool {
	if endpoint != CompositeRouteEndpointChatCompletions && endpoint != CompositeRouteEndpointResponses && endpoint != CompositeRouteEndpointMessages {
		return false
	}
	if domain.UsesOpenAIGateway(platform) {
		return endpoint != CompositeRouteEndpointMessages || platform == PlatformGrok || IsMultiProtocolAPIKeyProvider(platform) || group.AllowMessagesDispatch
	}
	switch platform {
	case PlatformAnthropic, PlatformAntigravity, PlatformGemini, PlatformKiro:
		return true
	default:
		return false
	}
}
