package repository

import (
	"context"
	"encoding/json"

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
