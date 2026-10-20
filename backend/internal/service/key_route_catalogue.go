package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

// KeyRouteCatalogueCandidate is a read-only source for the ordinary model
// catalogue. ModelIDs are concrete account-mapping public IDs, not declarations
// from a group or key allowlist. DefaultPlatforms permits the presentation layer
// to add its existing native defaults only when a real, schedulable unmapped
// account supports that platform. It is never a signal to use a catch-all default.
type KeyRouteCatalogueCandidate struct {
	Group            *Group
	ModelIDs         []string
	DefaultPlatforms []string
}

// CatalogueCandidates returns authorized main/backup groups in configured order.
// It deliberately does not select a route, acquire scheduler slots, load pricing,
// change account state, or observe/update fault-rule counters. Forced-platform
// and provider-native catalogue endpoints must retain their existing handlers.
// The caller renders provider metadata and intersects it with these sources,
// then applies both group and key allowlists to the final catalogue.
func (s *KeyRoutingService) CatalogueCandidates(ctx context.Context, key *APIKey) ([]KeyRouteCatalogueCandidate, error) {
	if ctx == nil || key == nil || key.User == nil || key.UserID <= 0 || key.User.ID != key.UserID || key.GroupID == nil {
		return nil, fmt.Errorf("%w: catalogue requires an authenticated key owner and primary group", keyrouting.ErrInvalidConfig)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.groups == nil || s.native == nil || s.native.accountRepo == nil || (s.cfg != nil && s.cfg.RunMode == config.RunModeSimple) {
		return nil, fmt.Errorf("key route catalogue is unavailable in this configuration")
	}
	ids, err := keyRouteCatalogueGroupIDs(*key.GroupID, key.BackupGroupIDs)
	if err != nil {
		return nil, err
	}
	result := make([]KeyRouteCatalogueCandidate, 0, len(ids))
	for index, id := range ids {
		group, err := s.groups.GetByIDLite(ctx, id)
		if errors.Is(err, ErrGroupNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("load key catalogue group: %w", err)
		}
		if group == nil || group.ID != id {
			return nil, fmt.Errorf("key catalogue group lookup returned an invalid snapshot")
		}
		// Native routing stops at an unauthorized/subscription/Claude-only
		// group rather than skipping to a later one. Preserve that reachable
		// prefix in discovery as well, including when the blocking group is
		// inactive: those gates precede active-state checks in Select.
		if group.ClaudeCodeOnly || group.SubscriptionType != SubscriptionTypeStandard || !key.User.CanBindGroup(id, group.IsExclusive) {
			break
		}
		if !group.IsActive() {
			continue
		}
		// A scoped repository read preserves group isolation and distinguishes a
		// storage failure from an empty/mapping-free source. GetAvailableModels
		// intentionally has a legacy nil/fallback contract and is not used here.
		accounts, err := s.native.accountRepo.ListSchedulableByGroupID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("load key catalogue accounts: %w", err)
		}
		candidate := keyRouteCatalogueSource(group, accounts, index > 0)
		if len(candidate.ModelIDs) == 0 && len(candidate.DefaultPlatforms) == 0 {
			continue
		}
		result = append(result, candidate)
	}
	return result, nil
}

func keyRouteCatalogueGroupIDs(primary int64, backups []int64) ([]int64, error) {
	if primary <= 0 || len(backups) > keyrouting.MaxBackupGroups {
		return nil, fmt.Errorf("%w: invalid catalogue group configuration", keyrouting.ErrInvalidConfig)
	}
	ids := append([]int64{primary}, backups...)
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("%w: catalogue group IDs must be positive", keyrouting.ErrInvalidConfig)
		}
		if _, ok := seen[id]; ok {
			return nil, fmt.Errorf("%w: duplicate catalogue group ID", keyrouting.ErrInvalidConfig)
		}
		seen[id] = struct{}{}
	}
	return ids, nil
}

func keyRouteCatalogueSource(group *Group, accounts []Account, backup bool) KeyRouteCatalogueCandidate {
	// Do not expose the repository's mutable allowlist slice to presentation
	// adapters that need a request-local group context.
	snapshot := *group
	snapshot.ModelAllowlist.Models = append([]string(nil), group.ModelAllowlist.Models...)
	candidate := KeyRouteCatalogueCandidate{Group: &snapshot}
	models, defaults := make(map[string]struct{}), make(map[string]struct{})
	for i := range accounts {
		// GetModelMapping maintains an account-local cache; use a value copy so
		// catalogue reads do not change repository/scheduler account snapshots.
		account := accounts[i]
		if !account.IsSchedulable() || !isConcreteRequestPlatform(account.Platform) {
			continue
		}
		if group.Platform != PlatformComposite && account.Platform != group.Platform && !mixedListingAccountAllowed(group.Platform, &account) {
			continue
		}
		if backup && !keyRouteCatalogueTextPlatformAllowed(group, account.Platform) {
			continue
		}
		mapping := account.GetModelMapping()
		if account.IsOpenAIPassthroughEnabled() {
			// Native passthrough ignores stale model_mapping, so discovery must
			// not turn those leftover mapping keys into promised public models.
			mapping = nil
		}
		if len(mapping) == 0 && keyRouteCatalogueHasNativeDefaults(account.Platform) && (group.Platform == PlatformComposite || account.Platform == group.Platform) {
			defaults[account.Platform] = struct{}{}
		}
		for model, target := range mapping {
			if model != strings.TrimSpace(model) || !keyRouteCatalogueConcreteID(model) || strings.TrimSpace(target) == "" || (backup && (!KeyRouteCatalogueBackupModelAllowed(model) || !KeyRouteCatalogueBackupModelAllowed(target))) {
				continue
			}
			if group.Platform != PlatformComposite && account.Platform != group.Platform && !mixedListingModelAllowed(group.Platform, model) {
				continue
			}
			if group.ModelAllowlist.Allows(model) {
				models[model] = struct{}{}
			}
		}
		// A configured group ID may concretize an actual account wildcard, but
		// it cannot declare capability on its own. Preserve this native listing
		// behavior without publishing wildcard patterns or treating an unmapped
		// account as evidence that every allowlist entry exists upstream.
		if len(mapping) > 0 && group.ModelAllowlistEnabled() {
			for _, model := range group.ModelAllowlist.Models {
				model = strings.TrimSpace(model)
				if !keyRouteCatalogueConcreteID(model) {
					continue
				}
				target, matched := account.ResolveMappedModel(model)
				if !matched || strings.TrimSpace(target) == "" || (backup && (!KeyRouteCatalogueBackupModelAllowed(model) || !KeyRouteCatalogueBackupModelAllowed(target))) {
					continue
				}
				if group.Platform != PlatformComposite && account.Platform != group.Platform && !mixedListingModelAllowed(group.Platform, model) {
					continue
				}
				models[model] = struct{}{}
			}
		}
	}
	// Composite route-only aliases are intentionally not inferred from route
	// configuration: an enabled alias does not establish a healthy target account.
	// Existing account-backed mappings/defaults remain valid conservative sources.
	for model := range models {
		candidate.ModelIDs = append(candidate.ModelIDs, model)
	}
	for platform := range defaults {
		candidate.DefaultPlatforms = append(candidate.DefaultPlatforms, platform)
	}
	sort.Strings(candidate.ModelIDs)
	sort.Strings(candidate.DefaultPlatforms)
	return candidate
}

func keyRouteCatalogueConcreteID(model string) bool {
	// Native account/group matching uses * only. Context suffixes such as
	// [1m] and literal question marks are concrete IDs, not shell globs.
	return model != "" && !strings.Contains(model, "*")
}

func keyRouteCatalogueTextPlatformAllowed(group *Group, platform string) bool {
	for _, endpoint := range []string{CompositeRouteEndpointResponses, CompositeRouteEndpointChatCompletions, CompositeRouteEndpointMessages} {
		if keyRoutingEndpointAllowed(group, platform, endpoint) {
			return true
		}
	}
	return false
}

// KeyRouteCatalogueBackupModelAllowed narrows discovered backup sources to the
// existing stateless text route surface. Presentation uses the same check when
// expanding native defaults, so a backup-only media ID is not advertised as
// callable merely because its provider has a healthy text account.
func KeyRouteCatalogueBackupModelAllowed(model string) bool {
	model = strings.TrimSpace(model)
	return keyRouteCatalogueConcreteID(model) &&
		!IsExplicitImageGenerationIntent(openAIResponsesEndpoint, model, nil) &&
		!isImageGenerationModel(model) && !xai.IsGrokImagineModel(model)
}

// Keep this explicit: the legacy presentation helper's unknown-platform branch
// falls back to Claude models. In particular CN providers without a native
// static catalogue must contribute concrete mappings, not invented defaults.
func keyRouteCatalogueHasNativeDefaults(platform string) bool {
	switch platform {
	case PlatformOpenAI, PlatformAnthropic, PlatformGemini, PlatformAntigravity, PlatformKiro, PlatformGrok, PlatformOpenCodeGo, PlatformTypeSafe:
		return true
	default:
		return false
	}
}
