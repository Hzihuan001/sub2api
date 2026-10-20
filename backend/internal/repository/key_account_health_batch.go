package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/redis/go-redis/v9"
)

const (
	keyAccountHealthBatchSize          = 32
	keyAccountHealthBatchMaxIdentities = 1024
	keyAccountHealthBatchWrongType     = "KEY_ACCOUNT_HEALTH_CORRUPT_TYPE"
)

// Redis MGET returns nil both for missing keys and non-string keys. Inspect
// nil entries in the same read-only script so corrupt types cannot silently
// masquerade as healthy missing identities. Every key has the same hash tag.
var keyAccountHealthBatchReadScript = redis.NewScript(`
local values = redis.call('MGET', unpack(KEYS))
for index, value in ipairs(values) do
  if value == false then
    local kind = redis.call('TYPE', KEYS[index])
    if type(kind) == 'table' then kind = kind.ok end
    if kind ~= 'none' then
      return redis.error_reply('KEY_ACCOUNT_HEALTH_CORRUPT_TYPE')
    end
  end
end
return values
`)

// GetMany provides ordered, read-only candidate hints for one site+Key scope.
// It does not create empty state, consume identity quota, renew TTLs or persist
// a newer revision fence. PrepareAccountHealth still validates each payload
// and rejects lower revisions. A newer revision is only a provisional healthy
// snapshot: callers must revalidate authoritative metadata and use the atomic
// Get at final account admission. Observe/Fence retain their CAS guarantees.
// Batches are individually atomic, not a transaction spanning the whole pool;
// any failed/partial read returns no snapshots and must not mean pool-empty.
func (s *keyAccountHealthStore) GetMany(ctx context.Context, ids []keyrouting.AccountHealthIdentity) ([]keyrouting.AccountHealthSnapshot, error) {
	if ctx == nil {
		return nil, errors.New("key account health batch requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(ids) > keyAccountHealthBatchMaxIdentities {
		return nil, keyrouting.ErrAccountHealthCapacity
	}
	if len(ids) == 0 {
		return []keyrouting.AccountHealthSnapshot{}, nil
	}
	if s == nil || s.rdb == nil {
		return nil, errors.New("key account health Redis unavailable")
	}
	normalized := make([]keyrouting.AccountHealthIdentity, len(ids))
	keys := make([]string, len(ids))
	var scope string
	for index, id := range ids {
		id = id.Normalized()
		if err := id.Validate(); err != nil {
			return nil, err
		}
		if index == 0 {
			scope = id.KeyScope()
		} else if id.KeyScope() != scope {
			return nil, fmt.Errorf("%w: batch spans multiple site or Key scopes", keyrouting.ErrAccountHealthIdentity)
		}
		normalized[index] = id
		keys[index], _, _ = s.keys(id)
	}
	results := make([]keyrouting.AccountHealthSnapshot, len(ids))
	now := s.opts.Clock()
	for start := 0; start < len(keys); start += keyAccountHealthBatchSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		end := start + keyAccountHealthBatchSize
		if end > len(keys) {
			end = len(keys)
		}
		values, err := keyAccountHealthBatchReadScript.Run(ctx, s.rdb, keys[start:end]).Slice()
		if err != nil {
			if strings.Contains(err.Error(), keyAccountHealthBatchWrongType) {
				return nil, fmt.Errorf("%w: non-string state", keyrouting.ErrAccountHealthCorrupt)
			}
			return nil, fmt.Errorf("read key account health batch: %w", err)
		}
		if len(values) != end-start {
			return nil, keyrouting.ErrAccountHealthCorrupt
		}
		for offset, value := range values {
			var current *keyrouting.AccountHealthState
			if value != nil {
				var raw []byte
				switch data := value.(type) {
				case string:
					raw = []byte(data)
				case []byte:
					raw = data
				default:
					return nil, keyrouting.ErrAccountHealthCorrupt
				}
				if len(raw) == 0 || len(raw) > s.opts.MaxPayloadBytes {
					return nil, keyrouting.ErrAccountHealthCorrupt
				}
				current = &keyrouting.AccountHealthState{}
				if err := json.Unmarshal(raw, current); err != nil {
					return nil, fmt.Errorf("%w: invalid JSON", keyrouting.ErrAccountHealthCorrupt)
				}
			}
			_, snapshot, err := keyrouting.PrepareAccountHealth(current, normalized[start+offset], now, s.opts)
			if err != nil {
				return nil, err
			}
			results[start+offset] = snapshot
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
