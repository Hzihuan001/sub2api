package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/redis/go-redis/v9"
)

const keyAccountHealthNamespace = "moshu:key_account_health:v1:"
const keyAccountHealthCASAttempts = 5
const keyAccountHealthPoolSize = 16

// All three keys use the same {site:bucket} hash tag, including on Redis
// Cluster. The per-Key and per-site-bucket indexes bound arbitrary
// model/endpoint identities without requiring a cross-slot transaction. A
// site-wide upper bound is the sum of the deterministic bucket quotas.
var keyAccountHealthCASScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if (current or '') ~= ARGV[1] then return 0 end
local now = tonumber(ARGV[4])
local ttl = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now)
if not redis.call('ZSCORE', KEYS[2], KEYS[1]) then
  if redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[5]) then return -1 end
end
if not redis.call('ZSCORE', KEYS[3], KEYS[1]) then
  if redis.call('ZCARD', KEYS[3]) >= tonumber(ARGV[6]) then return -2 end
end
redis.call('SET', KEYS[1], ARGV[2], 'PX', ttl)
redis.call('ZADD', KEYS[2], now + ttl, KEYS[1])
redis.call('ZADD', KEYS[3], now + ttl, KEYS[1])
redis.call('PEXPIRE', KEYS[2], ttl)
redis.call('PEXPIRE', KEYS[3], ttl)
return 1
`)

type keyAccountHealthStore struct {
	rdb  *redis.Client
	opts keyrouting.AccountHealthOptions
}

// NewKeyAccountHealthStore keeps every state transition atomic using bounded
// CAS retries. It never writes native account cooldowns, quotas or balances.
// Its one bounded pool is independent of native Redis traffic. Respecting
// context deadlines here is mandatory: go-redis defaults otherwise discard
// them for socket reads. The owner must call the optional Close at shutdown.
func NewKeyAccountHealthStore(rdb *redis.Client, opts keyrouting.AccountHealthOptions) keyrouting.AccountHealthStore {
	if rdb == nil {
		return &keyAccountHealthStore{opts: opts.Normalized()}
	}
	clientOpts := *rdb.Options()
	clientOpts.ContextTimeoutEnabled = true
	clientOpts.MaxRetries = -1
	clientOpts.MinRetryBackoff = -1
	clientOpts.MaxRetryBackoff = -1
	if clientOpts.PoolSize <= 0 || clientOpts.PoolSize > keyAccountHealthPoolSize {
		clientOpts.PoolSize = keyAccountHealthPoolSize
	}
	clientOpts.MaxActiveConns = clientOpts.PoolSize
	clientOpts.MaxConcurrentDials = min(clientOpts.PoolSize, 4)
	clientOpts.MinIdleConns = 0
	clientOpts.MaxIdleConns = clientOpts.PoolSize
	clientOpts.PoolTimeout = keyAccountHealthReadTimeoutLimit
	clientOpts.ReadTimeout = keyAccountHealthWriteTimeoutLimit
	clientOpts.WriteTimeout = keyAccountHealthWriteTimeoutLimit
	clientOpts.DialTimeout = keyAccountHealthReadTimeoutLimit
	clientOpts.PipelineReadBufferSize = 0
	clientOpts.PipelineWriteBufferSize = 0
	return &keyAccountHealthStore{rdb: redis.NewClient(&clientOpts), opts: opts.Normalized()}
}

const (
	keyAccountHealthReadTimeoutLimit  = 150 * time.Millisecond
	keyAccountHealthWriteTimeoutLimit = 250 * time.Millisecond
)

// Close releases only the health pool, never the caller's native client.
func (s *keyAccountHealthStore) Close() error {
	if s == nil || s.rdb == nil {
		return nil
	}
	return s.rdb.Close()
}

func (s *keyAccountHealthStore) Get(ctx context.Context, id keyrouting.AccountHealthIdentity) (keyrouting.AccountHealthSnapshot, error) {
	return s.change(ctx, id, nil, keyrouting.AccountHealthAttempt{})
}

func (s *keyAccountHealthStore) Fence(ctx context.Context, id keyrouting.AccountHealthIdentity) error {
	_, err := s.Get(ctx, id)
	return err
}

func (s *keyAccountHealthStore) Observe(ctx context.Context, id keyrouting.AccountHealthIdentity, policy keyrouting.RouteFailurePolicy, attempt keyrouting.AccountHealthAttempt) (keyrouting.AccountHealthSnapshot, error) {
	return s.change(ctx, id, &policy, attempt)
}

func (s *keyAccountHealthStore) keys(id keyrouting.AccountHealthIdentity) (string, string, string) {
	// The first hash tag is site+bucket so state, the per-key index and the
	// bucket-wide index remain atomic on Redis Cluster. Keep the key scope in
	// the path and a second tag as diagnostic markers for safe inspection.
	bucketTag := id.SiteScope() + ":" + strconv.Itoa(id.SiteBucket(s.opts))
	base := keyAccountHealthNamespace + "{" + bucketTag + "}:" + id.KeyScope() + ":{" + id.KeyScope() + "}:"
	return base + "state:" + id.StorageKey(), base + "index", keyAccountHealthNamespace + "{" + bucketTag + "}:site-index"
}

func (s *keyAccountHealthStore) change(ctx context.Context, id keyrouting.AccountHealthIdentity, policy *keyrouting.RouteFailurePolicy, attempt keyrouting.AccountHealthAttempt) (keyrouting.AccountHealthSnapshot, error) {
	var empty keyrouting.AccountHealthSnapshot
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s == nil || s.rdb == nil {
		return empty, errors.New("key account health Redis unavailable")
	}
	if err := id.Validate(); err != nil {
		return empty, err
	}
	stateKey, indexKey, siteIndexKey := s.keys(id)
	for retry := 0; retry < keyAccountHealthCASAttempts; retry++ {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		raw, err := s.rdb.Get(ctx, stateKey).Bytes()
		if errors.Is(err, redis.Nil) {
			raw, err = nil, nil
		}
		if err != nil {
			return empty, fmt.Errorf("read key account health: %w", err)
		}
		if len(raw) > s.opts.MaxPayloadBytes {
			return empty, keyrouting.ErrAccountHealthCorrupt
		}
		var current *keyrouting.AccountHealthState
		if len(raw) > 0 {
			current = &keyrouting.AccountHealthState{}
			if err := json.Unmarshal(raw, current); err != nil {
				return empty, fmt.Errorf("%w: invalid JSON", keyrouting.ErrAccountHealthCorrupt)
			}
		}
		now := s.opts.Clock()
		var next *keyrouting.AccountHealthState
		var snapshot keyrouting.AccountHealthSnapshot
		if policy == nil {
			next, snapshot, err = keyrouting.PrepareAccountHealth(current, id, now, s.opts)
		} else {
			next, snapshot, err = keyrouting.AdvanceAccountHealth(current, id, *policy, attempt, now, s.opts)
		}
		if err != nil {
			return empty, err
		}
		encoded, err := json.Marshal(next)
		if err != nil || len(encoded) > s.opts.MaxPayloadBytes {
			return empty, keyrouting.ErrAccountHealthCapacity
		}
		result, err := keyAccountHealthCASScript.Run(ctx, s.rdb, []string{stateKey, indexKey, siteIndexKey}, string(raw), string(encoded), strconv.FormatInt(s.opts.IdleTTL.Milliseconds(), 10), strconv.FormatInt(now.UnixMilli(), 10), s.opts.MaxIdentitiesPerKey, s.opts.SiteBucketLimit()).Int()
		if err != nil {
			return empty, fmt.Errorf("commit key account health: %w", err)
		}
		switch result {
		case 1:
			return snapshot, nil
		case -1, -2:
			return empty, keyrouting.ErrAccountHealthCapacity
		case 0: // Another instance committed first. Re-read and re-evaluate.
		default:
			return empty, keyrouting.ErrAccountHealthCorrupt
		}
	}
	return empty, keyrouting.ErrAccountHealthBusy
}

var _ keyrouting.AccountHealthStore = (*keyAccountHealthStore)(nil)
