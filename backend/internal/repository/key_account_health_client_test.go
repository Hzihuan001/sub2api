package repository

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountHealthPrivatePoolOptionsAndClose(t *testing.T) {
	server := miniredis.RunT(t)
	native := redis.NewClient(&redis.Options{Addr: server.Addr(), PoolSize: 64, MinIdleConns: 2, MaxRetries: 2, ReadTimeout: 3 * time.Second})
	t.Cleanup(func() { _ = native.Close() })
	store := NewKeyAccountHealthStore(native, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore)
	t.Cleanup(func() { _ = store.Close() })
	require.NotSame(t, native, store.rdb)
	opts := store.rdb.Options()
	require.True(t, opts.ContextTimeoutEnabled)
	require.Zero(t, opts.MaxRetries, "normalized -1 disables go-redis network retries")
	require.LessOrEqual(t, opts.PoolSize, 16)
	require.LessOrEqual(t, opts.MaxActiveConns, 16)
	require.Zero(t, opts.MinIdleConns)
	require.LessOrEqual(t, opts.MaxConcurrentDials, 4)
	require.Equal(t, 150*time.Millisecond, opts.PoolTimeout)
	require.False(t, native.Options().ContextTimeoutEnabled)
	require.Equal(t, 64, native.Options().PoolSize)
	require.Equal(t, 2, native.Options().MaxRetries)
	require.Equal(t, 3*time.Second, native.Options().ReadTimeout)
	_, err := store.Get(context.Background(), keyAccountHealthTestID())
	require.NoError(t, err)
	require.NoError(t, store.Close())
	require.NoError(t, native.Ping(context.Background()).Err(), "health cleanup must not close the native pool")
	_, err = store.Get(context.Background(), keyAccountHealthTestID())
	require.Error(t, err)
}

func TestKeyAccountHealthPrivatePoolHonorsShortContext(t *testing.T) {
	var dials atomic.Int64
	native := redis.NewClient(&redis.Options{Addr: "synthetic-unresponsive-redis", ReadTimeout: 3 * time.Second, MaxRetries: 3, Dialer: func(context.Context, string, string) (net.Conn, error) {
		dials.Add(1)
		client, peer := net.Pipe()
		t.Cleanup(func() { _ = peer.Close() })
		go func() { _, _ = io.Copy(io.Discard, peer) }()
		return client, nil
	}})
	t.Cleanup(func() { _ = native.Close() })
	store := NewKeyAccountHealthStore(native, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore)
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := store.Get(ctx, keyAccountHealthTestID())
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	require.Less(t, time.Since(start), 300*time.Millisecond, "soft health must not wait native 3-second network timeouts")
	require.EqualValues(t, 1, dials.Load(), "private health client must not retry the failed network operation")
}
