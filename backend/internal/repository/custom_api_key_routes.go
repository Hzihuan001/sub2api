package repository

import (
	"context"
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
	if len(keys) == 0 {
		return nil
	}
	// Match the list with one query and preserve the configured position order.
	indices := make(map[int64]int, len(keys))
	args := make([]any, len(keys))
	placeholders := make([]string, len(keys))
	for i := range keys {
		indices[keys[i].ID] = i
		keys[i].BackupGroupIDs = nil
		args[i] = keys[i].ID
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx,
		"SELECT api_key_id, group_id FROM custom_api_key_routes WHERE api_key_id IN ("+strings.Join(placeholders, ",")+") ORDER BY api_key_id, position", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var keyID, groupID int64
		if err := rows.Scan(&keyID, &groupID); err != nil {
			return err
		}
		if i, ok := indices[keyID]; ok {
			keys[i].BackupGroupIDs = append(keys[i].BackupGroupIDs, groupID)
		}
	}
	return rows.Err()
}
