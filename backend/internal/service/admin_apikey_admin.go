package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// AdminCreateAPIKey creates a key for a target user while reusing the regular
// API-key creation path.  The initial create is deliberately unbound; the
// existing admin group-binding path then applies the same active-group,
// subscription, exclusive-group and cache rules used when an administrator
// changes a key's group in the user-management UI.
func (s *adminServiceImpl) AdminCreateAPIKey(ctx context.Context, userID int64, req CreateAPIKeyRequest) (*APIKey, error) {
	if s.apiKeyService == nil {
		return nil, infraerrors.InternalServer("API_KEY_SERVICE_UNAVAILABLE", "api key service is not configured")
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "user id must be positive")
	}

	requestedGroupID := req.GroupID
	req.GroupID = nil
	key, err := s.apiKeyService.Create(ctx, userID, req)
	if err != nil {
		return nil, err
	}
	if requestedGroupID == nil || *requestedGroupID == 0 {
		return key, nil
	}

	result, err := s.AdminUpdateAPIKeyGroupID(ctx, key.ID, requestedGroupID)
	if err != nil {
		// Creation succeeded but the requested binding did not.  Do not leave a
		// surprising unbound key behind when the form reports failure.
		_ = s.apiKeyService.Delete(ctx, key.ID, userID)
		return nil, err
	}
	return result.APIKey, nil
}

// AdminDeleteAPIKey deletes a key only when it belongs to the target user.
// The final delete is delegated to APIKeyService so soft-delete, audit and
// authentication-cache invalidation remain identical to self-service delete.
func (s *adminServiceImpl) AdminDeleteAPIKey(ctx context.Context, keyID, userID int64) error {
	if s.apiKeyService == nil {
		return infraerrors.InternalServer("API_KEY_SERVICE_UNAVAILABLE", "api key service is not configured")
	}
	key, err := s.apiKeyRepo.GetByID(ctx, keyID)
	if err != nil {
		return err
	}
	if key.UserID != userID {
		return ErrAPIKeyNotFound
	}
	return s.apiKeyService.Delete(ctx, keyID, userID)
}
