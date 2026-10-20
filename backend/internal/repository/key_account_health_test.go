package repository

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func keyAccountHealthTestID() keyrouting.AccountHealthIdentity {
	return keyrouting.AccountHealthIdentity{Site: "synthetic-site", APIKeyID: 7, GroupID: 11, AccountID: 19, Model: "public-model", Endpoint: "/v1/responses", PolicyRevision: 1}
}

func keyAccountHealthTestPolicy(threshold int) keyrouting.RouteFailurePolicy {
	return keyrouting.RouteFailurePolicy{Rules: []keyrouting.RouteFailureRule{{Type: keyrouting.FailureRuleConsecutiveFailures, Threshold: threshold, CooldownSeconds: 60}}}
}

func keyAccountHealthTestAttempt(id string, now time.Time) keyrouting.AccountHealthAttempt {
	return keyrouting.AccountHealthAttempt{AttemptID: id, Dispatched: true, StartedAt: now.Add(-time.Second), FinishedAt: now, Outcome: keyrouting.OutcomeFailure, StatusCode: 503}
}

func newKeyAccountHealthTestStore(t *testing.T, server *miniredis.Miniredis, opts keyrouting.AccountHealthOptions) (*redis.Client, *keyAccountHealthStore) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: 0})
	t.Cleanup(func() { _ = client.Close() })
	store := NewKeyAccountHealthStore(client, opts).(*keyAccountHealthStore)
	t.Cleanup(func() { _ = store.Close() })
	return store.rdb, store
}

func TestKeyAccountHealthRedisSharedStateDedupFence(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	opts := keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }}
	_, first := newKeyAccountHealthTestStore(t, server, opts)
	_, second := newKeyAccountHealthTestStore(t, server, opts)
	ctx, id, policy := context.Background(), keyAccountHealthTestID(), keyAccountHealthTestPolicy(2)
	result, err := first.Observe(ctx, id, policy, keyAccountHealthTestAttempt("one", now))
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.False(t, result.Cooling)
	result, err = second.Observe(ctx, id, policy, keyAccountHealthTestAttempt("one", now))
	require.NoError(t, err)
	require.True(t, result.Duplicate)
	require.False(t, result.Accepted)
	now = now.Add(time.Second)
	result, err = second.Observe(ctx, id, policy, keyAccountHealthTestAttempt("two", now))
	require.NoError(t, err)
	require.True(t, result.Cooling)
	result, err = first.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, result.Cooling)
	stateKey, indexKey, siteIndexKey := first.keys(id)
	require.Equal(t, 24*time.Hour, server.TTL(stateKey))
	require.Equal(t, 24*time.Hour, server.TTL(indexKey))
	require.Equal(t, 24*time.Hour, server.TTL(siteIndexKey))
	for _, key := range server.Keys() {
		require.True(t, strings.HasPrefix(key, keyAccountHealthNamespace))
		if key != siteIndexKey {
			require.Contains(t, key, "{"+id.KeyScope()+"}")
		}
	}
	newID := id
	newID.PolicyRevision = 2
	require.NoError(t, second.Fence(ctx, newID))
	_, err = first.Observe(ctx, id, policy, keyAccountHealthTestAttempt("late-old", now))
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthStaleRevision)
	result, err = second.Get(ctx, newID)
	require.NoError(t, err)
	require.False(t, result.Cooling)
}

func TestKeyAccountHealthRedisPerKeyQuotaAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }, MaxIdentitiesPerKey: 1})
	id := keyAccountHealthTestID()
	_, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	other := id
	other.Model = "other"
	_, err = store.Get(context.Background(), other)
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCapacity)
	other.APIKeyID++
	_, err = store.Get(context.Background(), other)
	require.NoError(t, err, "quota is per Key, not a global account block")
	server.FastForward(25 * time.Hour)
	now = now.Add(25 * time.Hour)
	other.APIKeyID = id.APIKeyID
	_, err = store.Get(context.Background(), other)
	require.NoError(t, err)
	_, indexKey, _ := store.keys(other)
	items, err := server.ZMembers(indexKey)
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestKeyAccountHealthRedisSiteBucketQuotaIsAtomic(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{
		Clock:                func() time.Time { return now },
		MaxIdentitiesPerSite: 2,
		SiteBucketCount:      1,
		MaxIdentitiesPerKey:  8,
		MaxIdentities:        16,
	})
	first := keyAccountHealthTestID()
	second := first
	second.APIKeyID++
	third := second
	third.APIKeyID++
	for _, id := range []keyrouting.AccountHealthIdentity{first, second} {
		_, err := store.Get(context.Background(), id)
		require.NoError(t, err)
	}
	_, err := store.Get(context.Background(), third)
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCapacity)
	otherSite := third
	otherSite.Site = "other-site"
	_, err = store.Get(context.Background(), otherSite)
	require.NoError(t, err, "site quota must not cross deployment boundaries")
	server.FastForward(25 * time.Hour)
	now = now.Add(25 * time.Hour)
	_, err = store.Get(context.Background(), third)
	require.NoError(t, err, "expired site index entries must be pruned")
	_, _, siteIndex := store.keys(third)
	require.True(t, server.Exists(siteIndex))
}

func TestKeyAccountHealthRedisCorruptUnavailableAndCancelled(t *testing.T) {
	server := miniredis.RunT(t)
	_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{MaxPayloadBytes: 1024})
	id := keyAccountHealthTestID()
	key, _, _ := store.keys(id)
	require.NoError(t, server.Set(key, "not-json"))
	_, err := store.Get(context.Background(), id)
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCorrupt)
	require.NoError(t, server.Set(key, strings.Repeat("x", 1025)))
	_, err = store.Get(context.Background(), id)
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCorrupt)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.Get(ctx, id)
	require.ErrorIs(t, err, context.Canceled)
	_, err = NewKeyAccountHealthStore(nil, keyrouting.AccountHealthOptions{}).Get(context.Background(), id)
	require.Error(t, err)
	server.Close()
	_, err = store.Get(context.Background(), id)
	require.Error(t, err, "a Redis error is not an empty pool or a successful health read")
}

func TestKeyAccountHealthRedisConcurrentDuplicate(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }})
	var wg sync.WaitGroup
	results := make(chan keyrouting.AccountHealthSnapshot, 12)
	errs := make(chan error, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.Observe(context.Background(), keyAccountHealthTestID(), keyAccountHealthTestPolicy(2), keyAccountHealthTestAttempt("same-attempt", now))
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	accepted := 0
	for result := range results {
		if result.Accepted {
			accepted++
		}
		require.False(t, result.Cooling)
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, accepted)
}

type keyAccountHealthProcessHook struct{ before func(redis.Cmder) }

func (h keyAccountHealthProcessHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return next(ctx, network, address)
	}
}
func (h keyAccountHealthProcessHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error { h.before(cmd); return next(ctx, cmd) }
}
func (h keyAccountHealthProcessHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestKeyAccountHealthRedisCASFencesConcurrentOldWriter(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	opts := keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }}
	client, oldStore := newKeyAccountHealthTestStore(t, server, opts)
	_, newStore := newKeyAccountHealthTestStore(t, server, opts)
	id := keyAccountHealthTestID()
	_, err := oldStore.Get(context.Background(), id)
	require.NoError(t, err)
	var once sync.Once
	client.AddHook(keyAccountHealthProcessHook{before: func(cmd redis.Cmder) {
		if cmd.Name() == "evalsha" || cmd.Name() == "eval" {
			once.Do(func() {
				newID := id
				newID.PolicyRevision++
				require.NoError(t, newStore.Fence(context.Background(), newID))
			})
		}
	}})
	_, err = oldStore.Observe(context.Background(), id, keyAccountHealthTestPolicy(1), keyAccountHealthTestAttempt("racing-old", now))
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthStaleRevision)
	newID := id
	newID.PolicyRevision++
	result, err := newStore.Get(context.Background(), newID)
	require.NoError(t, err)
	require.False(t, result.Cooling)
}

func TestKeyAccountHealthRedisCASBoundedRetries(t *testing.T) {
	server := miniredis.RunT(t)
	client, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{})
	id := keyAccountHealthTestID()
	stateKey, _, _ := store.keys(id)
	_, err := store.Get(context.Background(), id)
	require.NoError(t, err)
	mutations := 0
	client.AddHook(keyAccountHealthProcessHook{before: func(cmd redis.Cmder) {
		if cmd.Name() != "evalsha" && cmd.Name() != "eval" {
			return
		}
		raw, readErr := server.Get(stateKey)
		require.NoError(t, readErr)
		var state keyrouting.AccountHealthState
		require.NoError(t, json.Unmarshal([]byte(raw), &state))
		state.UpdatedAt++
		encoded, encodeErr := json.Marshal(&state)
		require.NoError(t, encodeErr)
		require.NoError(t, server.Set(stateKey, string(encoded)))
		mutations++
	}})
	_, err = store.Observe(context.Background(), id, keyAccountHealthTestPolicy(1), keyAccountHealthTestAttempt("race", time.Now()))
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthBusy)
	require.LessOrEqual(t, mutations, keyAccountHealthCASAttempts+1)
}
