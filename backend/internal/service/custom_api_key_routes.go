package service

import (
	"context"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const MaxAPIKeyBackupGroups = 4

// validateAPIKeyRoutes leaves legacy single-group keys unchanged. Multi-group
// routes require an active standard primary and active standard backups that
// the owner can already use; configuring a route never grants group access.
func (s *APIKeyService) validateAPIKeyRoutes(ctx context.Context, user *User, primary *int64, backups []int64) error {
	if len(backups) == 0 {
		return nil
	}
	if len(backups) > MaxAPIKeyBackupGroups {
		return infraerrors.BadRequest("API_KEY_ROUTES_LIMIT", "an API key supports at most four backup groups")
	}
	if primary == nil || *primary <= 0 {
		return infraerrors.BadRequest("API_KEY_ROUTES_PRIMARY_REQUIRED", "backup groups require a primary group")
	}
	seen := make(map[int64]bool, len(backups)+1)
	for _, id := range append([]int64{*primary}, backups...) {
		if id <= 0 || seen[id] {
			return infraerrors.BadRequest("API_KEY_ROUTES_INVALID", "primary and backup group IDs must be positive and unique")
		}
		seen[id] = true
		group, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return fmt.Errorf("get route group: %w", err)
		}
		if group == nil || group.Status != StatusActive {
			return infraerrors.BadRequest("GROUP_NOT_ACTIVE", "all API key route groups must be active")
		}
		if group.SubscriptionType != SubscriptionTypeStandard {
			return infraerrors.BadRequest("API_KEY_ROUTES_STANDARD_ONLY", "API key backup routes support standard groups only")
		}
		if group.ClaudeCodeOnly {
			return infraerrors.BadRequest("API_KEY_ROUTES_CLAUDE_ONLY_UNSUPPORTED", "Claude Code-only groups are not supported by multi-group API keys")
		}
		if user == nil || !user.CanBindGroup(group.ID, group.IsExclusive) {
			return ErrGroupNotAllowed
		}
	}
	return nil
}

// AdminUpdateAPIKeyRoutes uses the owner permissions for multi-group keys while
// preserving the legacy administrator single-group binding behavior.
func (s *adminServiceImpl) AdminUpdateAPIKeyRoutes(ctx context.Context, keyID int64, primary *int64, backups *[]int64) (*AdminUpdateAPIKeyGroupIDResult, error) {
	key, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	effectiveBackups := key.BackupGroupIDs
	if backups != nil {
		effectiveBackups = *backups
	}
	if len(effectiveBackups) == 0 {
		return s.adminUpdateAPIKeyGroupID(ctx, keyID, primary, backups)
	}
	if s.apiKeyService == nil {
		return nil, infraerrors.InternalServer("API_KEY_SERVICE_UNAVAILABLE", "api key service is not configured")
	}
	updated, err := s.apiKeyService.Update(ctx, keyID, key.UserID, UpdateAPIKeyRequest{GroupID: primary, BackupGroupIDs: backups})
	if err != nil {
		return nil, err
	}
	return &AdminUpdateAPIKeyGroupIDResult{APIKey: updated}, nil
}
