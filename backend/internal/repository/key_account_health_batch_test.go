package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountHealthBatchMissingReadOnlyAndCooling(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }, MaxIdentitiesPerKey: 1})
	id := keyAccountHealthTestID()
	other := id
	other.AccountID++
	results, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id, other})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.False(t, results[0].Cooling)
	require.Equal(t, id.PolicyRevision, results[1].PolicyRevision)
	require.Empty(t, server.Keys(), "candidate reads must not create blank state/index or consume quota")
	_, err = store.Observe(context.Background(), id, keyAccountHealthTestPolicy(1), keyAccountHealthTestAttempt("cool", now))
	require.NoError(t, err)
	stateKey, indexKey, siteIndexKey := store.keys(id)
	before, _ := server.Get(stateKey)
	server.FastForward(time.Minute)
	stateTTL, indexTTL := server.TTL(stateKey), server.TTL(indexKey)
	results, err = store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{other, id, id})
	require.NoError(t, err)
	require.Len(t, results, 3)
	require.False(t, results[0].Cooling)
	require.True(t, results[1].Cooling)
	require.Equal(t, results[1], results[2])
	require.Equal(t, stateTTL, server.TTL(stateKey))
	require.Equal(t, indexTTL, server.TTL(indexKey))
	require.Equal(t, indexTTL, server.TTL(siteIndexKey))
	after, _ := server.Get(stateKey)
	require.Equal(t, before, after)
	_, err = store.Observe(context.Background(), other, keyAccountHealthTestPolicy(1), keyAccountHealthTestAttempt("other", now))
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCapacity, "missing batch snapshot did not reserve a quota slot")
}

func TestKeyAccountHealthBatchRevisionIsProvisional(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{Clock: func() time.Time { return now }})
	id := keyAccountHealthTestID()
	_, err := store.Observe(context.Background(), id, keyAccountHealthTestPolicy(1), keyAccountHealthTestAttempt("old", now))
	require.NoError(t, err)
	newID := id
	newID.PolicyRevision++
	snapshots, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{newID})
	require.NoError(t, err)
	require.False(t, snapshots[0].Cooling)
	require.Equal(t, newID.PolicyRevision, snapshots[0].PolicyRevision)
	// Read-only scanning must not claim to have persisted a revision fence.
	old, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id})
	require.NoError(t, err)
	require.True(t, old[0].Cooling)
	require.NoError(t, store.Fence(context.Background(), newID))
	snapshots, err = store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id})
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthStaleRevision)
	require.Nil(t, snapshots)
	now = now.Add(61 * time.Second)
	snapshots, err = store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{newID})
	require.NoError(t, err)
	require.False(t, snapshots[0].Cooling)
}

func TestKeyAccountHealthBatchRejectsCorruptionAndPartialResults(t *testing.T) {
	for _, bad := range []string{"invalid-json", "", strings.Repeat("x", 1025), `{"schema":9}`, `{"schema":1,"identity_key":"wrong","policy_revision":1}`} {
		t.Run(fmt.Sprintf("payload-%d-%s", len(bad), bad[:min(8, len(bad))]), func(t *testing.T) {
			server := miniredis.RunT(t)
			_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{MaxPayloadBytes: 1024})
			id := keyAccountHealthTestID()
			other := id
			other.AccountID++
			badKey, _, _ := store.keys(other)
			require.NoError(t, server.Set(badKey, bad))
			results, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id, other})
			require.ErrorIs(t, err, keyrouting.ErrAccountHealthCorrupt)
			require.Nil(t, results, "one corrupt account cannot turn the rest into an all-healthy/all-cooling snapshot")
		})
	}
	t.Run("wrong-redis-type", func(t *testing.T) {
		server := miniredis.RunT(t)
		client, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{})
		id := keyAccountHealthTestID()
		key, _, _ := store.keys(id)
		require.NoError(t, client.HSet(context.Background(), key, "wrong", "type").Err())
		results, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id})
		require.ErrorIs(t, err, keyrouting.ErrAccountHealthCorrupt)
		require.Nil(t, results)
	})
	t.Run("wrong-current-identity", func(t *testing.T) {
		server := miniredis.RunT(t)
		_, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{})
		id := keyAccountHealthTestID()
		other := id
		other.Model = "other"
		state, _, err := keyrouting.PrepareAccountHealth(nil, other, time.Now(), keyrouting.AccountHealthOptions{})
		require.NoError(t, err)
		raw, err := json.Marshal(state)
		require.NoError(t, err)
		key, _, _ := store.keys(id)
		require.NoError(t, server.Set(key, string(raw)))
		results, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id})
		require.ErrorIs(t, err, keyrouting.ErrAccountHealthCorrupt)
		require.Nil(t, results)
	})
}

func TestKeyAccountHealthBatchScopeBoundsAndCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	client, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{})
	id := keyAccountHealthTestID()
	var commands atomic.Int64
	client.AddHook(keyAccountHealthProcessHook{before: func(redis.Cmder) { commands.Add(1) }})
	for _, change := range []func(*keyrouting.AccountHealthIdentity){func(i *keyrouting.AccountHealthIdentity) { i.APIKeyID++ }, func(i *keyrouting.AccountHealthIdentity) { i.Site = "other-site" }, func(i *keyrouting.AccountHealthIdentity) { i.AccountID = 0 }} {
		other := id
		change(&other)
		snapshots, err := store.GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id, other})
		require.ErrorIs(t, err, keyrouting.ErrAccountHealthIdentity)
		require.Nil(t, snapshots)
	}
	tooMany := make([]keyrouting.AccountHealthIdentity, keyAccountHealthBatchMaxIdentities+1)
	_, err := store.GetMany(context.Background(), tooMany)
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCapacity)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = store.GetMany(ctx, []keyrouting.AccountHealthIdentity{id})
	require.ErrorIs(t, err, context.Canceled)
	_, err = store.GetMany(nil, []keyrouting.AccountHealthIdentity{id})
	require.Error(t, err)
	results, err := store.GetMany(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, results)
	require.Zero(t, commands.Load(), "invalid or empty batch must not issue Redis reads")
	_, err = NewKeyAccountHealthStore(nil, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore).GetMany(context.Background(), []keyrouting.AccountHealthIdentity{id})
	require.Error(t, err)
}

func TestKeyAccountHealthBatchBoundedReadCommandsAndLaterFailure(t *testing.T) {
	server := miniredis.RunT(t)
	client, store := newKeyAccountHealthTestStore(t, server, keyrouting.AccountHealthOptions{})
	ids := make([]keyrouting.AccountHealthIdentity, 2*keyAccountHealthBatchSize+1)
	for index := range ids {
		ids[index] = keyAccountHealthTestID()
		ids[index].AccountID += int64(index)
	}
	// Warm only the script cache; the test counts reads, not handshake setup.
	_, err := store.GetMany(context.Background(), ids[:1])
	require.NoError(t, err)
	var evals atomic.Int64
	client.AddHook(keyAccountHealthProcessHook{before: func(cmd redis.Cmder) {
		if cmd.Name() == "evalsha" || cmd.Name() == "eval" {
			evals.Add(1)
		}
	}})
	results, err := store.GetMany(context.Background(), ids)
	require.NoError(t, err)
	require.Len(t, results, len(ids))
	require.EqualValues(t, 3, evals.Load())
	require.Empty(t, server.Keys())
	lastKey, _, _ := store.keys(ids[len(ids)-1])
	require.NoError(t, server.Set(lastKey, "broken"))
	results, err = store.GetMany(context.Background(), ids)
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthCorrupt)
	require.Nil(t, results, "later-batch failure must not leak earlier healthy results")
	server.Close()
	_, err = store.GetMany(context.Background(), ids)
	require.Error(t, err)
	require.False(t, errors.Is(err, keyrouting.ErrAccountHealthIdentity))
}
