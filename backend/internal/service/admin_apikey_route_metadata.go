package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// Scope-aware routing edits reuse owner validation and the regular atomic
// repository write. The historical admin binding/autogrant path is unchanged
// when scope is omitted; conversion never grants a new group permission.
func (s *adminServiceImpl) AdminUpdateAPIKeyRouting(ctx context.Context, keyID int64, primary *int64, backups *[]int64, scope *string) (*AdminUpdateAPIKeyGroupIDResult, error) {
	if scope == nil {
		return s.AdminUpdateAPIKeyRoutes(ctx, keyID, primary, backups)
	}
	if err := ValidateKeyRouteWriteMetadata(scope, nil); err != nil {
		return nil, err
	}
	if s.apiKeyService == nil {
		return nil, infraerrors.InternalServer("API_KEY_SERVICE_UNAVAILABLE", "api key service is not configured")
	}
	key, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	updated, err := s.apiKeyService.Update(ctx, keyID, key.UserID, UpdateAPIKeyRequest{
		GroupID: primary, BackupGroupIDs: backups, KeyRouteScope: scope,
	})
	if err != nil {
		return nil, err
	}
	return &AdminUpdateAPIKeyGroupIDResult{APIKey: updated}, nil
}
