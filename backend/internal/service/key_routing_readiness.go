package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// keyRoutingNativeReadiness observes the same ordinary eligibility gates as
// native scheduling. It never selects/reserves an account, registers a session,
// increments RPM, clears cooldowns, or persists threshold-driven pauses.
func (s *GatewayService) keyRoutingNativeReadiness(ctx context.Context, group *Group, platform, model, endpoint string) (bool, bool, error) {
	if err := keyRoutingReadinessInput(ctx, group, endpoint); err != nil {
		return false, false, err
	}
	if s == nil {
		return false, false, fmt.Errorf("native key routing readiness is unavailable")
	}
	forcePlatform, _ := ctx.Value(ctxkey.ForcePlatform).(string)
	accounts, useMixed, err := keyRoutingReadAccounts(ctx, s.schedulerSnapshot, s.accountRepo, group.ID, platform, forcePlatform != "")
	if err != nil {
		return false, false, err
	}
	ctx = s.withGatewayProfitControlGate(ctx, &group.ID)
	channel, err := keyRoutingReadChannel(ctx, s.channelService, group.ID)
	if err != nil {
		return false, false, err
	}
	if keyRoutingChannelRestricted(channel, group.ID, model, "") {
		return true, false, nil
	}
	supportsModel, platformAccounts := false, 0
	for i := range accounts {
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		account, err := keyRoutingFreshAccount(ctx, s.schedulerSnapshot, &accounts[i])
		if err != nil {
			return false, false, err
		}
		if account == nil || !openAIStickyAccountMatchesGroup(account, &group.ID) || !s.isAccountAllowedForPlatform(account, platform, useMixed) {
			continue
		}
		platformAccounts++
		if !s.isModelSupportedByAccountWithContext(ctx, account, model) {
			continue
		}
		supportsModel = true
		if !account.IsSchedulableForModelWithContext(ctx, model) ||
			(group.RequirePrivacySet && !account.IsPrivacySet()) ||
			!s.isGatewayAccountProfitEligible(ctx, account) ||
			!s.isAccountSchedulableForQuota(account) ||
			keyRoutingThresholdBlocked(ctx, s.rateLimitService, account, model) ||
			(s.rateLimitService != nil && s.rateLimitService.IsAccountOverloadCooling(account.ID, time.Now())) ||
			keyRoutingChannelRestricted(channel, group.ID, model, resolveAccountUpstreamModel(account, model)) {
			continue
		}
		if isKiroDirectModeAccount(account) && s.kiroCooldownStore != nil {
			state, err := s.getKiroCooldownState(ctx, buildKiroAccountKey(account))
			if err != nil {
				return false, false, fmt.Errorf("read key routing kiro cooldown: %w", err)
			}
			if state != nil && state.Active {
				continue
			}
		}
		allowed, err := s.keyRoutingNativeLimits(ctx, account)
		if err != nil {
			return false, false, err
		}
		if !allowed {
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		return true, true, nil
	}
	// Empty/paused pools say nothing about the group's model catalog. Preserve
	// no_schedulable_accounts instead of manufacturing unsupported_model.
	return supportsModel || platformAccounts == 0, false, ctx.Err()
}

// keyRoutingOpenAIReadiness covers stateless text HTTP/SSE. The ordinary text
// Responses path, like Chat and Messages, allows the existing Chat fallback;
// image intent and native compaction are excluded by the entry adapter.
func (s *OpenAIGatewayService) keyRoutingOpenAIReadiness(ctx context.Context, group *Group, platform, model, endpoint string) (bool, bool, error) {
	if err := keyRoutingReadinessInput(ctx, group, endpoint); err != nil {
		return false, false, err
	}
	if s == nil {
		return false, false, fmt.Errorf("openai key routing readiness is unavailable")
	}
	platform = NormalizeOpenAICompatiblePlatform(platform)
	accounts, _, err := keyRoutingReadAccounts(ctx, s.schedulerSnapshot, s.accountRepo, group.ID, platform, false)
	if err != nil {
		return false, false, err
	}
	ctx = s.withOpenAIQuotaAutoPauseContext(ctx)
	ctx = s.withOpenAIProfitControlGate(ctx, &group.ID)
	channel, err := keyRoutingReadChannel(ctx, s.channelService, group.ID)
	if err != nil {
		return false, false, err
	}
	if keyRoutingChannelRestricted(channel, group.ID, model, "") {
		return true, false, nil
	}
	supportsModel, platformAccounts := false, 0
	for i := range accounts {
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		account, err := keyRoutingFreshAccount(ctx, s.schedulerSnapshot, &accounts[i])
		if err != nil {
			return false, false, err
		}
		if account == nil || !openAIStickyAccountMatchesGroup(account, &group.ID) || account.Platform != platform || !account.IsOpenAICompatible() {
			continue
		}
		platformAccounts++
		if !keyRoutingOpenAIModelSupported(ctx, account, model) {
			continue
		}
		supportsModel = true
		if !isOpenAICompatibleAccountEligibleForRequest(ctx, account, platform, model, false, OpenAIEndpointCapabilityChatCompletions) ||
			(group.RequirePrivacySet && !account.IsPrivacySet()) ||
			keyRoutingThresholdBlocked(ctx, s.rateLimitService, account, model) ||
			s.isOpenAIAccountRequestRuntimeBlocked(account, model) ||
			s.isOpenAIProxyStreamQuarantined(ctx, account) ||
			keyRoutingChannelRestricted(channel, group.ID, model, resolveOpenAIAccountUpstreamModelForRequest(account, model, false)) {
			continue
		}
		var parentErr error
		parentHealthy := parentHealthyForShadow(account, func(id int64) *Account {
			if s.accountRepo == nil {
				parentErr = fmt.Errorf("key routing shadow parent repository is unavailable")
				return nil
			}
			parent, err := s.accountRepo.GetByID(ctx, id)
			parentErr = err
			return parent
		})
		if parentErr != nil {
			return false, false, fmt.Errorf("read key routing shadow parent: %w", parentErr)
		}
		if !parentHealthy {
			continue
		}
		if account.IsGrok() {
			upstreamModel := canonicalOpenAIAccountSchedulingModel(account, model)
			if isGrokTeamModelRateLimited(account, upstreamModel, time.Now()) || isGrokModelQuotaBlocked(account.ID, upstreamModel, time.Now()) {
				continue
			}
			// The existing free-tier gate reads usage and maintains its cache;
			// it never reserves capacity or changes account eligibility fields.
			if len(s.filterGrokFreeQuotaAccountsForOpenAI(ctx, []Account{*account})) == 0 {
				continue
			}
		}
		if err := ctx.Err(); err != nil {
			return false, false, err
		}
		return true, true, nil
	}
	return supportsModel || platformAccounts == 0, false, ctx.Err()
}

func keyRoutingReadinessInput(ctx context.Context, group *Group, endpoint string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if group == nil || group.ID <= 0 {
		return fmt.Errorf("key routing readiness requires a group")
	}
	switch endpoint {
	case CompositeRouteEndpointMessages, CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions:
		return nil
	default:
		return fmt.Errorf("key routing readiness does not support endpoint %q", endpoint)
	}
}

// Use the same snapshot/repository reads as listSchedulableAccounts, without
// its ApplyAccountSchedulingThreshold side effect. Key routing always retains
// explicit group isolation, including when the server runs in simple mode.
func keyRoutingReadAccounts(ctx context.Context, snapshot *SchedulerSnapshotService, repo AccountRepository, groupID int64, platform string, forcePlatform bool) ([]Account, bool, error) {
	if snapshot != nil {
		return snapshot.ListSchedulableAccounts(ctx, &groupID, platform, forcePlatform)
	}
	if repo == nil {
		return nil, false, fmt.Errorf("key routing account repository is unavailable")
	}
	useMixed := (platform == PlatformAnthropic || platform == PlatformGemini) && !forcePlatform
	if useMixed {
		accounts, err := repo.ListSchedulableByGroupIDAndPlatforms(ctx, groupID, []string{platform, PlatformAntigravity})
		return accounts, true, err
	}
	accounts, err := repo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, platform)
	return accounts, false, err
}

// Refresh from the individual account snapshot without forcing per-candidate
// SQL. The real scheduler still owns its final database eligibility check.
// Snapshot/fallback failures stop routing rather than triggering a backup.
func keyRoutingFreshAccount(ctx context.Context, snapshot *SchedulerSnapshotService, account *Account) (*Account, error) {
	if snapshot == nil {
		return account, nil
	}
	return snapshot.GetAccount(ctx, account.ID)
}

func keyRoutingOpenAIModelSupported(ctx context.Context, account *Account, model string) bool {
	if source, ok := CompositeRouteSourceFromContext(ctx); ok && source == CompositeRouteSourceAccount {
		if publicModel, ok := RequestedPublicModelFromContext(ctx); ok && !explicitModelMappingClaims(*account, publicModel) {
			return false
		}
	}
	return account.IsModelSupported(model)
}

func keyRoutingThresholdBlocked(ctx context.Context, limiter *RateLimitService, account *Account, model string) bool {
	if limiter == nil || limiter.settingService == nil || limiter.accountRepo == nil || account == nil || !account.IsActive() || !account.Schedulable {
		return false
	}
	now := time.Now().UTC()
	thresholds := limiter.settingService.GetAccountSchedulingThresholds(ctx)
	decision := EvaluateAccountSchedulingThreshold(account, thresholds, now)
	if decision.ShouldPause && decision.Until != nil && decision.Until.After(now) {
		return true
	}
	if account.Platform == PlatformAnthropic && isAnthropicFableModel(account.GetMappedModel(model)) {
		decision = evaluateAnthropicFableSchedulingThreshold(account, thresholds, now)
		return decision.ShouldPause && decision.Until != nil && decision.Until.After(now)
	}
	return false
}

func keyRoutingReadChannel(ctx context.Context, channels *ChannelService, groupID int64) (*channelLookup, error) {
	if channels == nil {
		return nil, nil
	}
	return channels.lookupGroupChannel(ctx, groupID)
}

func keyRoutingChannelRestricted(channel *channelLookup, groupID int64, model, upstreamModel string) bool {
	if channel == nil {
		return false
	}
	mapping := resolveMapping(channel, groupID, model)
	billingModel := billingModelForRestriction(mapping.BillingModelSource, model, mapping.MappedModel)
	if mapping.BillingModelSource == BillingModelSourceUpstream {
		billingModel = upstreamModel
	}
	return billingModel != "" && checkRestricted(channel, groupID, billingModel)
}

// Read the same non-sticky window/RPM limits without populating caches or
// swallowing dependency errors. Concurrency and session slots are deliberately
// absent: a temporarily full account remains eligible for normal queuing.
func (s *GatewayService) keyRoutingNativeLimits(ctx context.Context, account *Account) (bool, error) {
	if !account.IsAnthropicOAuthOrSetupToken() {
		return true, nil
	}
	if account.GetWindowCostLimit() > 0 {
		cost, hit := windowCostFromPrefetchContext(ctx, account.ID)
		if !hit && s.sessionLimitCache != nil {
			var err error
			cost, hit, err = s.sessionLimitCache.GetWindowCost(ctx, account.ID)
			if err != nil {
				return false, fmt.Errorf("read key routing window cost: %w", err)
			}
		}
		if !hit {
			if s.usageLogRepo == nil {
				return false, fmt.Errorf("key routing window cost repository is unavailable")
			}
			stats, err := s.usageLogRepo.GetAccountWindowStats(ctx, account.ID, account.GetCurrentWindowStartTime())
			if err != nil {
				return false, fmt.Errorf("read key routing window cost: %w", err)
			}
			if stats != nil {
				cost = stats.StandardCost
			}
		}
		if account.CheckWindowCostSchedulability(cost) != WindowCostSchedulable {
			return false, nil
		}
	}
	if account.GetBaseRPM() > 0 {
		rpm, hit := rpmFromPrefetchContext(ctx, account.ID)
		if !hit && s.rpmCache != nil {
			var err error
			rpm, err = s.rpmCache.GetRPM(ctx, account.ID)
			if err != nil {
				return false, fmt.Errorf("read key routing rpm: %w", err)
			}
		}
		if account.CheckRPMSchedulability(rpm) != WindowCostSchedulable {
			return false, nil
		}
	}
	return true, ctx.Err()
}
