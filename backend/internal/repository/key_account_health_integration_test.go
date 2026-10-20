//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountHealthRealRedisIndependentInstances(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	first := NewKeyAccountHealthStore(integrationRedis, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore)
	second := NewKeyAccountHealthStore(integrationRedis, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore)
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	id := keyAccountHealthTestID()
	id.Site = uuid.NewString()
	stateKey, indexKey, siteIndexKey := first.keys(id)
	t.Cleanup(func() {
		require.NoError(t, integrationRedis.Del(context.Background(), stateKey, indexKey, siteIndexKey).Err())
	})
	policy := keyAccountHealthTestPolicy(3)
	start := time.Now().Add(-time.Second)
	results := make(chan error, 12)
	var workers sync.WaitGroup
	// Six real identities, each completion delivered twice to independent
	// clients. Retrying a CAS conflict reuses the exact terminal timestamp/ID.
	for index := 0; index < 12; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			store := first
			if index%2 != 0 {
				store = second
			}
			attempt := keyrouting.AccountHealthAttempt{AttemptID: fmt.Sprintf("real-%d", index/2), Dispatched: true, StartedAt: start, FinishedAt: start.Add(time.Millisecond), Outcome: keyrouting.OutcomeFailure, StatusCode: 503}
			var err error
			for retry := 0; retry < 12; retry++ {
				_, err = store.Observe(ctx, id, policy, attempt)
				if !errors.Is(err, keyrouting.ErrAccountHealthBusy) {
					break
				}
			}
			results <- err
		}(index)
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	a, err := first.Get(ctx, id)
	require.NoError(t, err)
	b, err := second.Get(ctx, id)
	require.NoError(t, err)
	require.True(t, a.Cooling)
	require.Equal(t, a, b)
	var persisted keyrouting.AccountHealthState
	raw, err := integrationRedis.Get(ctx, stateKey).Bytes()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.Len(t, persisted.Seen, 6, "duplicate delivery must not create extra inference samples")
	require.Equal(t, 3, persisted.CooldownCount)

	newer := id
	newer.PolicyRevision++
	require.NoError(t, second.Fence(ctx, newer))
	_, err = first.Observe(ctx, id, policy, keyrouting.AccountHealthAttempt{AttemptID: "late-old-revision", Dispatched: true, StartedAt: start, FinishedAt: time.Now(), Outcome: keyrouting.OutcomeFailure, StatusCode: 503})
	require.ErrorIs(t, err, keyrouting.ErrAccountHealthStaleRevision)
	fresh, err := first.Get(ctx, newer)
	require.NoError(t, err)
	require.False(t, fresh.Cooling, "late old completion must not revive a removed policy")
	require.NoError(t, first.Close())
	require.NoError(t, integrationRedis.Ping(ctx).Err(), "custom cleanup must not close the native Redis owner")
	require.NoError(t, second.Fence(ctx, newer), "another application instance keeps its own pool")
}
