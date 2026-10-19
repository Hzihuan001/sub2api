package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *apiKeyRepository) replaceAPIKeyModelPolicy(ctx context.Context, keyID int64, policy *service.KeyModelPolicy) error {
	normalized, err := service.NormalizeKeyModelPolicy(policy)
	if err != nil {
		return err
	}
	client := clientFromContext(ctx, r.client)
	if _, err := client.ExecContext(ctx, "DELETE FROM custom_api_key_model_policies WHERE api_key_id = $1", keyID); err != nil {
		return err
	}
	if normalized == nil {
		return nil
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	_, err = client.ExecContext(ctx, "INSERT INTO custom_api_key_model_policies (api_key_id, policy) VALUES ($1, $2)", keyID, raw)
	return err
}

// Every key read, including auth projection and user/admin lists, shares this
// batched attachment seam. Missing/corrupt policies must never silently widen
// access; unlike optional presentation metadata a failed read is an error.
func (r *apiKeyRepository) attachAPIKeyModelPolicies(ctx context.Context, keys []service.APIKey) error {
	if len(keys) == 0 {
		return nil
	}
	indices := make(map[int64]int, len(keys))
	args, placeholders := make([]any, len(keys)), make([]string, len(keys))
	for i := range keys {
		indices[keys[i].ID], args[i], placeholders[i] = i, keys[i].ID, fmt.Sprintf("$%d", i+1)
		keys[i].KeyModelPolicy = nil
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, "SELECT api_key_id, policy FROM custom_api_key_model_policies WHERE api_key_id IN ("+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var keyID int64
		var raw []byte
		if err := rows.Scan(&keyID, &raw); err != nil {
			return err
		}
		var policy *service.KeyModelPolicy
		if err := json.Unmarshal(raw, &policy); err != nil || policy == nil || !policy.Enabled {
			return fmt.Errorf("invalid stored API key model policy for key %d", keyID)
		}
		if i, ok := indices[keyID]; ok {
			keys[i].KeyModelPolicy = policy.Clone()
		}
	}
	return rows.Err()
}
