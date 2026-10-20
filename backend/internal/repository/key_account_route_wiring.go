package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// GetKeyRouteSiteID reads the one stable, non-secret deployment namespace.
// Missing migrations are a startup/configuration error, never an excuse to
// silently share cooldowns using a site display name.
func (r *apiKeyRepository) GetKeyRouteSiteID(ctx context.Context) (string, error) {
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx, `SELECT site_id FROM custom_key_route_site WHERE id = 1`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", errors.New("missing key account routing site namespace")
	}
	var site string
	if err := rows.Scan(&site); err != nil {
		return "", err
	}
	id, err := uuid.Parse(site)
	if err != nil || id == uuid.Nil {
		return "", errors.New("invalid key account routing site namespace")
	}
	return id.String(), rows.Err()
}

func ProvideKeyAccountHealthStore(rdb *redis.Client) (keyrouting.AccountHealthStore, func()) {
	store := NewKeyAccountHealthStore(rdb, keyrouting.AccountHealthOptions{})
	return store, func() {
		if closer, ok := store.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
}
