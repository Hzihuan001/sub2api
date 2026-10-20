package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestKeyAccountRouteRetryAfterUsesEarliestObservedConfiguredGroup(t *testing.T) {
	s, _, store, _, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
	ctx := WithKeyAccountRoutingSession(context.Background(), s)
	require.Zero(t, KeyAccountRouteRetryAfter(ctx))
	_, err := s.Exclusions(ctx, 1)
	require.NoError(t, err)
	_, err = s.Observe(ctx, accountRuntimeSample(t, s, 1, 41, "primary-cold", *now))
	require.NoError(t, err)
	*now = now.Add(50 * time.Second)
	_, err = s.Exclusions(ctx, 2)
	require.NoError(t, err)
	_, err = s.Observe(ctx, accountRuntimeSample(t, s, 2, 43, "backup-cold", *now))
	require.NoError(t, err)
	reads, writes := store.getCalls, store.observeCalls
	require.Equal(t, 550*time.Second, KeyAccountRouteRetryAfter(ctx))
	require.Equal(t, reads, store.getCalls, "header advice must not read remote health")
	require.Equal(t, writes, store.observeCalls, "header advice must not create a sample")
	*now = now.Add(551 * time.Second)
	require.Equal(t, 49*time.Second, KeyAccountRouteRetryAfter(ctx))
	*now = now.Add(49 * time.Second)
	require.Zero(t, KeyAccountRouteRetryAfter(ctx))
	require.Zero(t, KeyAccountRouteRetryAfter(context.Background()))
	require.Zero(t, KeyAccountRouteRetryAfter(nil))
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.Zero(t, KeyAccountRouteRetryAfter(cancelled))
}
