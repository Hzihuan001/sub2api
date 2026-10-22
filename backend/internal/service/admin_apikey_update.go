package service

import (
	"context"
	"errors"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
)

// AdminUpdateAPIKey keeps one native edit/validation/write-mask path for both
// editors. Standard-group grants and the key edit commit together; subscription
// entitlement and multi-group eligibility are never bypassed.
func (s *adminServiceImpl) AdminUpdateAPIKey(ctx context.Context, keyID int64, req UpdateAPIKeyRequest) (*AdminUpdateAPIKeyGroupIDResult, error) {
	if s.apiKeyService == nil {
		return nil, infraerrors.InternalServer("API_KEY_SERVICE_UNAVAILABLE", "api key service is not configured")
	}
	if err := validateUpdateAPIKeyRequest(req); err != nil {
		return nil, err
	}
	for _, patterns := range []*[]string{req.IPWhitelist, req.IPBlacklist} {
		if patterns != nil {
			if invalid := ip.ValidateIPPatterns(*patterns); len(invalid) > 0 {
				return nil, fmt.Errorf("%w: %v", ErrInvalidIPPattern, invalid)
			}
		}
	}
	key, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	grants, err := s.planAdminAPIKeyGroupGrants(ctx, key, req)
	if err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		updated, err := s.apiKeyService.Update(ctx, keyID, key.UserID, req)
		if err != nil {
			return nil, err
		}
		return &AdminUpdateAPIKeyGroupIDResult{APIKey: updated}, nil
	}
	if s.entClient == nil {
		return nil, infraerrors.InternalServer("API_KEY_TRANSACTION_UNAVAILABLE", "atomic API key group authorization is not configured")
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin API key edit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	for _, group := range grants {
		if err := s.userRepo.AddGroupToAllowedGroups(txCtx, key.UserID, group.ID); err != nil {
			return nil, fmt.Errorf("grant API key group: %w", err)
		}
	}
	updated, err := s.apiKeyService.Update(txCtx, keyID, key.UserID, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit API key edit: %w", err)
	}
	// Native Update may invalidate before the outer transaction commits. Clear
	// again afterwards, including other owner keys whose allowed groups changed.
	s.apiKeyService.InvalidateAuthCacheByUserID(ctx, key.UserID)
	if req.ResetRateLimitUsage != nil && *req.ResetRateLimitUsage && s.apiKeyService.rateLimitCacheInvalid != nil {
		_ = s.apiKeyService.rateLimitCacheInvalid.InvalidateAPIKeyRateLimit(ctx, keyID)
	}
	firstID := grants[0].ID
	return &AdminUpdateAPIKeyGroupIDResult{
		APIKey: updated, AutoGrantedGroupAccess: true,
		GrantedGroupID: &firstID, GrantedGroupName: grants[0].Name,
	}, nil
}

// Plan permissions in memory first, so invalid routes never create grants.
func (s *adminServiceImpl) planAdminAPIKeyGroupGrants(ctx context.Context, key *APIKey, req UpdateAPIKeyRequest) ([]*Group, error) {
	if req.GroupID == nil && req.BackupGroupIDs == nil {
		return nil, nil
	}
	primary, backups := key.GroupID, key.BackupGroupIDs
	if req.GroupID != nil {
		primary = req.GroupID
	}
	if req.BackupGroupIDs != nil {
		backups = *req.BackupGroupIDs
	}
	user, err := s.userRepo.GetByID(ctx, key.UserID)
	if err != nil {
		return nil, err
	}
	owner := *user
	owner.AllowedGroups = append([]int64(nil), user.AllowedGroups...)
	ids := append([]int64(nil), backups...)
	if primary != nil {
		if *primary < 0 {
			return nil, infraerrors.BadRequest("INVALID_GROUP_ID", "group_id must be non-negative")
		}
		if *primary > 0 {
			ids = append([]int64{*primary}, ids...)
		}
	}
	var grants []*Group
	for _, id := range ids {
		group, err := s.groupRepo.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if group == nil || group.Status != StatusActive {
			return nil, infraerrors.BadRequest("GROUP_NOT_ACTIVE", "target group is not active")
		}
		if group.IsSubscriptionType() {
			if s.userSubRepo == nil {
				return nil, infraerrors.BadRequest("SUBSCRIPTION_REQUIRED", "user does not have an active subscription for this group")
			}
			if _, err := s.userSubRepo.GetActiveByUserIDAndGroupID(ctx, key.UserID, id); err != nil {
				if errors.Is(err, ErrSubscriptionNotFound) {
					return nil, infraerrors.BadRequest("SUBSCRIPTION_REQUIRED", "user does not have an active subscription for this group")
				}
				return nil, err
			}
		} else if !owner.CanBindGroup(id, group.IsExclusive) {
			owner.AllowedGroups = append(owner.AllowedGroups, id)
			grants = append(grants, group)
		}
	}
	if err := s.apiKeyService.validateAPIKeyRoutes(ctx, &owner, primary, backups); err != nil {
		return nil, err
	}
	return grants, nil
}
