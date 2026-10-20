package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Keep the extension table outside the upstream Ent schema. Ent's underlying
// executor participates in the same transaction as the primary API key row.
func (r *apiKeyRepository) withAPIKeyRoutesTx(ctx context.Context, fn func(context.Context) error) error {
	if dbent.TxFromContext(ctx) != nil {
		return fn(ctx)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return fn(ctx) // repository already uses a transaction-scoped client
	}
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(dbent.NewTxContext(ctx, tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *apiKeyRepository) replaceAPIKeyRoutes(ctx context.Context, keyID int64, groups []int64) error {
	client := clientFromContext(ctx, r.client)
	if _, err := client.ExecContext(ctx, "DELETE FROM custom_api_key_routes WHERE api_key_id = $1", keyID); err != nil {
		return err
	}
	for i, groupID := range groups {
		if _, err := client.ExecContext(ctx, "INSERT INTO custom_api_key_routes (api_key_id, position, group_id) VALUES ($1, $2, $3)", keyID, i+1, groupID); err != nil {
			return err
		}
	}
	return r.checkAPIKeyRoutePrimary(ctx, keyID)
}

func (r *apiKeyRepository) replaceAPIKeyRoutePolicy(ctx context.Context, keyID int64, policy *service.KeyRouteFailurePolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	client := clientFromContext(ctx, r.client)
	if _, err := client.ExecContext(ctx, "DELETE FROM custom_api_key_route_policies WHERE api_key_id = $1", keyID); err != nil {
		if policy == nil && isMissingAPIKeyRoutePolicyTable(err) {
			return nil
		}
		return err
	}
	if policy == nil {
		return nil
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, `INSERT INTO custom_api_key_route_policies (api_key_id, policy) VALUES ($1, $2)`, keyID, raw)
	return err
}

func isMissingAPIKeyRoutePolicyTable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "custom_api_key_route_policies") && (strings.Contains(message, "no such table") || strings.Contains(message, "does not exist"))
}

// Updating the primary row acquires its write lock before touching routes.
// Checking the resulting pair inside that transaction also rejects a racing
// primary change that would duplicate a concurrently configured backup.
func (r *apiKeyRepository) checkAPIKeyRoutePrimary(ctx context.Context, keyID int64) error {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `
		SELECT 1 FROM custom_api_key_routes r JOIN api_keys k ON k.id = r.api_key_id
		WHERE k.id = $1 AND (k.group_id IS NULL OR k.group_id = r.group_id) LIMIT 1`, keyID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return errors.New("API key routes require a primary distinct from every backup")
	}
	return rows.Err()
}

func (r *apiKeyRepository) apiKeyWithRoutes(ctx context.Context, entity *dbent.APIKey) (*service.APIKey, error) {
	keys := []service.APIKey{*apiKeyEntityToService(entity)}
	if err := r.attachAPIKeyRoutes(ctx, keys); err != nil {
		return nil, err
	}
	return &keys[0], nil
}

func (r *apiKeyRepository) attachAPIKeyRoutes(ctx context.Context, keys []service.APIKey) error {
	return r.attachAPIKeyAccountRoutes(ctx, keys)
}

func (r *apiKeyRepository) attachAPIKeyRoutePolicies(ctx context.Context, keys []service.APIKey) error {
	if len(keys) == 0 {
		return nil
	}
	indices := make(map[int64]int, len(keys))
	args := make([]any, len(keys))
	placeholders := make([]string, len(keys))
	for i := range keys {
		indices[keys[i].ID] = i
		args[i] = keys[i].ID
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		keys[i].KeyRouteFailurePolicy = nil
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx,
		"SELECT api_key_id, policy FROM custom_api_key_route_policies WHERE api_key_id IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		if isMissingAPIKeyRoutePolicyTable(err) {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var keyID int64
		var raw []byte
		if err := rows.Scan(&keyID, &raw); err != nil {
			return err
		}
		var policy service.KeyRouteFailurePolicy
		if err := json.Unmarshal(raw, &policy); err != nil {
			return fmt.Errorf("decode api key route policy %d: %w", keyID, err)
		}
		if err := validateStoredAPIKeyRoutePolicy(&policy); err != nil {
			return fmt.Errorf("validate api key route policy %d: %w", keyID, err)
		}
		if i, ok := indices[keyID]; ok {
			keys[i].KeyRouteFailurePolicy = cloneStoredAPIKeyRoutePolicy(&policy)
		}
	}
	return rows.Err()
}

func validateStoredAPIKeyRoutePolicy(policy *service.KeyRouteFailurePolicy) error {
	if policy == nil {
		return sql.ErrNoRows
	}
	return policy.Validate()
}

func cloneStoredAPIKeyRoutePolicy(policy *service.KeyRouteFailurePolicy) *service.KeyRouteFailurePolicy {
	return policy.Clone()
}
