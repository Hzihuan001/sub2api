package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
}

func NewKeyRoutingService(groups GroupRepository, rates UserGroupRateRepository, native *GatewayService, openai *OpenAIGatewayService, composite *CompositeRouteResolver, cfg *config.Config) *KeyRoutingService {
	return &KeyRoutingService{groups: groups, rates: rates, native: native, openai: openai, composite: composite, cfg: cfg}
}

func (s *KeyRoutingService) Select(ctx context.Context, key *APIKey, request KeyRoutingRequest) (*APIKey, keyrouting.Plan, error) {
	if key == nil || key.User == nil || key.GroupID == nil || request.Model == "" {
		return nil, keyrouting.Plan{}, keyrouting.ErrInvalidConfig
	}
	if s == nil || s.groups == nil || (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) {
		return nil, keyrouting.Plan{}, fmt.Errorf("key routing is unavailable in this configuration")
	}
	groups := make(map[int64]*Group, 1+len(key.BackupGroupIDs))
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
		if domain.UsesOpenAIGateway(platform) {
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
		return state, err
	})}
	plan, err := planner.Plan(ctx, keyrouting.Config{PrimaryGroupID: *key.GroupID, BackupGroupIDs: key.BackupGroupIDs}, keyrouting.Request{Model: request.Model, Endpoint: request.Endpoint})
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
