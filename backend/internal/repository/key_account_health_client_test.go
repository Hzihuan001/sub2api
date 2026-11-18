package repository

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"testing/synctest"
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
	store, ok := NewKeyAccountHealthStore(native, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore)
	require.True(t, ok)
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
	synctest.Test(t, func(t *testing.T) {
		var dials atomic.Int64
		native := redis.NewClient(&redis.Options{Addr: "synthetic-unresponsive-redis", ReadTimeout: 3 * time.Second, MaxRetries: 3, Dialer: func(context.Context, string, string) (net.Conn, error) {
			dials.Add(1)
			client, peer := net.Pipe()
			t.Cleanup(func() { _ = peer.Close() })
			go func() { _, _ = io.Copy(io.Discard, peer) }()
			return client, nil
		}})
		t.Cleanup(func() { _ = native.Close() })
		store, ok := NewKeyAccountHealthStore(native, keyrouting.AccountHealthOptions{}).(*keyAccountHealthStore)
		require.True(t, ok)
		t.Cleanup(func() { _ = store.Close() })
		const timeout = 20 * time.Millisecond
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		start := time.Now()
		_, err := store.Get(ctx, keyAccountHealthTestID())
		// The socket and context timers share a deadline and can fire in either order.
		var timeoutErr net.Error
		require.ErrorAs(t, err, &timeoutErr)
		require.True(t, timeoutErr.Timeout())
		require.Equal(t, timeout, time.Since(start), "socket operations must honor the shorter context deadline")
		require.EqualValues(t, 1, dials.Load(), "private health client must not retry the failed network operation")
	})
}
