package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const groupRoutePolicySettingPrefix = "custom.gateway-resilience.group-failure-policy."

var _ service.KeyRouteRevisionReader = (*apiKeyRepository)(nil)

func (r *apiKeyRepository) refreshAPIKeyRouteMetadata(ctx context.Context, key *service.APIKey) error {
	keys := []service.APIKey{*key}
	if err := r.attachAPIKeyAccountRoutes(ctx, keys); err != nil {
		return err
	}
	key.KeyRouteScope, key.KeyRouteRevision = keys[0].KeyRouteScope, keys[0].KeyRouteRevision
	return nil
}

// The generation is always allocated by the database; callers cannot choose or
// reset it. Updating scope is separate from the strictly-decoded legacy JSON.
func (r *apiKeyRepository) replaceAPIKeyRouteScope(ctx context.Context, key *service.APIKey) error {
	scope := key.KeyRouteScope
	if scope == "" {
		scope = service.KeyRouteScopeGroupV1
	}
	if err := service.ValidateKeyRouteMetadata(scope, 1); err != nil {
		return err
	}
	_, err := clientFromContext(ctx, r.client).ExecContext(ctx, `
		UPDATE custom_api_key_account_routes SET scope = $1 WHERE api_key_id = $2`, scope, key.ID)
	return err
}

func (r *apiKeyRepository) GetKeyRouteRevision(ctx context.Context, keyID int64) (int64, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
		SELECT m.revision FROM api_keys k
		LEFT JOIN custom_api_key_account_routes m ON m.api_key_id = k.id WHERE k.id = $1 AND k.deleted_at IS NULL`, keyID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, err
		}
		return 0, service.ErrAPIKeyNotFound
	}
	var revision sql.NullInt64
	if err := rows.Scan(&revision); err != nil {
		return 0, err
	}
	if !revision.Valid || revision.Int64 <= 0 {
		return 0, fmt.Errorf("invalid API key route revision for key %d", keyID)
	}
	return revision.Int64, rows.Err()
}

func (r *apiKeyRepository) GetGroupRoutePolicySnapshot(ctx context.Context, groupID int64) (string, int64, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
		SELECT s.value, m.revision FROM groups g
		LEFT JOIN custom_group_route_revisions m ON m.group_id = g.id
		LEFT JOIN settings s ON s.key = $1
		WHERE g.id = $2 AND g.deleted_at IS NULL`, groupRoutePolicySettingPrefix+fmt.Sprint(groupID), groupID)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", 0, err
		}
		return "", 0, service.ErrGroupNotFound
	}
	var raw sql.NullString
	var revision sql.NullInt64
	if err := rows.Scan(&raw, &revision); err != nil {
		return "", 0, err
	}
	if !revision.Valid || revision.Int64 <= 0 {
		return "", 0, fmt.Errorf("invalid group route revision for group %d", groupID)
	}
	return raw.String, revision.Int64, rows.Err()
}

// Read every custom routing field in one SQL snapshot. Serially attaching a
// policy and then its newer generation can incorrectly bless stale in-flight
// results after a concurrent edit. A conflicting primary/native snapshot is
// rejected, never combined with a newer custom configuration.
func (r *apiKeyRepository) attachAPIKeyAccountRoutes(ctx context.Context, keys []service.APIKey) error {
	if len(keys) == 0 {
		return nil
	}
	indices := make(map[int64]int, len(keys))
	args, placeholders := make([]any, len(keys)), make([]string, len(keys))
	for i := range keys {
		indices[keys[i].ID], args[i], placeholders[i] = i, keys[i].ID, fmt.Sprintf("$%d", i+1)
		keys[i].BackupGroupIDs = nil
		keys[i].KeyRouteFailurePolicy = nil
		keys[i].KeyModelPolicy = nil
		keys[i].KeyRouteScope = ""
		keys[i].KeyRouteRevision = 0
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
		SELECT k.id, k.user_id, k.group_id, k.status, m.scope, m.revision,
		       r.group_id, p.policy, mp.policy
		FROM api_keys k
		LEFT JOIN custom_api_key_account_routes m ON m.api_key_id = k.id
		LEFT JOIN custom_api_key_routes r ON r.api_key_id = k.id
		LEFT JOIN custom_api_key_route_policies p ON p.api_key_id = k.id
		LEFT JOIN custom_api_key_model_policies mp ON mp.api_key_id = k.id
		WHERE k.deleted_at IS NULL AND k.id IN (`+strings.Join(placeholders, ",")+`)
		ORDER BY k.id, r.position`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := make(map[int64]bool, len(keys))
	for rows.Next() {
		var id, userID int64
		var primary, backup, revision sql.NullInt64
		var scope sql.NullString
		var status string
		var policyRaw, modelRaw []byte
		if err := rows.Scan(&id, &userID, &primary, &status, &scope, &revision, &backup, &policyRaw, &modelRaw); err != nil {
			return err
		}
		i := indices[id]
		key := &keys[i]
		if userID != key.UserID || status != key.Status || primary.Valid != (key.GroupID != nil) || (primary.Valid && primary.Int64 != *key.GroupID) {
			return fmt.Errorf("API key %d changed while reading routing configuration", id)
		}
		if !scope.Valid || !revision.Valid || revision.Int64 <= 0 {
			return fmt.Errorf("missing API key route metadata for key %d", id)
		}
		if err := service.ValidateKeyRouteMetadata(scope.String, revision.Int64); err != nil {
			return err
		}
		key.KeyRouteScope, key.KeyRouteRevision = scope.String, revision.Int64
		if backup.Valid {
			key.BackupGroupIDs = append(key.BackupGroupIDs, backup.Int64)
		}
		if !seen[id] {
			if policyRaw != nil {
				var policy service.KeyRouteFailurePolicy
				if err := json.Unmarshal(policyRaw, &policy); err != nil {
					return fmt.Errorf("decode API key route policy %d: %w", id, err)
				}
				if err := validateStoredAPIKeyRoutePolicy(&policy); err != nil {
					return fmt.Errorf("validate API key route policy %d: %w", id, err)
				}
				key.KeyRouteFailurePolicy = policy.Clone()
			}
			if modelRaw != nil {
				var policy *service.KeyModelPolicy
				if err := json.Unmarshal(modelRaw, &policy); err != nil || policy == nil || !policy.Enabled {
					return fmt.Errorf("invalid stored API key model policy for key %d", id)
				}
				key.KeyModelPolicy = policy.Clone()
			}
			seen[id] = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(seen) != len(indices) {
		return service.ErrAPIKeyNotFound
	}
	return nil
}
