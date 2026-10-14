package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPromptRuleCacheSubscriptionBlocksThroughCallbackAndCloses(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	subscriber := NewPromptRuleCache(client)
	publisher := NewPromptRuleCache(client)
	require.NoError(t, subscriber.Set(context.Background(), []*model.PromptRule{{ID: 1}}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	received := make(chan []*model.PromptRule, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	done := make(chan struct{})
	go func() {
		defer close(done)
		subscriber.SubscribeUpdates(ctx, func() {
			rules, _ := subscriber.Get(ctx)
			received <- rules
			<-release
		})
	}()
	require.Eventually(t, func() bool {
		return server.PubSubNumSub(promptRulePubSubKey)[promptRulePubSubKey] == 1
	}, time.Second, time.Millisecond)
	select {
	case <-done:
		t.Fatal("subscription runner returned while active")
	default:
	}
	require.NoError(t, publisher.Set(context.Background(), []*model.PromptRule{{ID: 2}}))
	require.NoError(t, publisher.NotifyUpdate(context.Background()))
	select {
	case rules := <-received:
		require.Len(t, rules, 1)
		require.Equal(t, int64(2), rules[0].ID, "notification must invalidate the subscriber's local cache")
	case <-time.After(time.Second):
		t.Fatal("subscription did not receive refresh notification")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("subscription runner returned before its callback completed")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscription did not finish after cancellation")
	}
	require.Eventually(t, func() bool {
		return server.PubSubNumSub(promptRulePubSubKey)[promptRulePubSubKey] == 0
	}, time.Second, time.Millisecond, "subscription connection must be closed on return")
}

func TestPromptRuleCacheSubscriptionRejectsCanceledContextBeforeConnecting(t *testing.T) {
	// A canceled subscription must return without touching even a nil client.
	cache := NewPromptRuleCache(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cache.SubscribeUpdates(ctx, func() { t.Error("canceled subscription invoked its callback") })
}
