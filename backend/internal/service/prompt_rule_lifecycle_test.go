package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/stretchr/testify/require"
)

type lifecyclePromptRuleRepo struct {
	PromptRuleRepository
	list func(context.Context) ([]*model.PromptRule, error)
}

func (r *lifecyclePromptRuleRepo) List(ctx context.Context) ([]*model.PromptRule, error) {
	if r.list == nil {
		return nil, nil
	}
	return r.list(ctx)
}

type lifecyclePromptRuleCache struct {
	get       func(context.Context) ([]*model.PromptRule, bool)
	set       func(context.Context, []*model.PromptRule) error
	subscribe func(context.Context, func())
	starts    atomic.Int32
}

func (c *lifecyclePromptRuleCache) Get(ctx context.Context) ([]*model.PromptRule, bool) {
	if c.get != nil {
		return c.get(ctx)
	}
	return nil, false
}

func (c *lifecyclePromptRuleCache) Set(ctx context.Context, rules []*model.PromptRule) error {
	if c.set != nil {
		return c.set(ctx, rules)
	}
	return nil
}

func (c *lifecyclePromptRuleCache) Invalidate(context.Context) error   { return nil }
func (c *lifecyclePromptRuleCache) NotifyUpdate(context.Context) error { return nil }

func (c *lifecyclePromptRuleCache) SubscribeUpdates(ctx context.Context, handler func()) {
	c.starts.Add(1)
	if c.subscribe != nil {
		c.subscribe(ctx, handler)
		return
	}
	<-ctx.Done()
}

func cleanupPromptRuleLifecycle(t *testing.T, svc *PromptRuleService) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, svc.Shutdown(ctx))
	})
}

func TestPromptRuleConstructorLoadsSynchronouslyWithoutSubscription(t *testing.T) {
	for _, test := range []struct {
		name       string
		dbFails    bool
		cacheHit   bool
		setFails   bool
		wantID     int64
		wantEvents []string
	}{
		{name: "database wins over cache", cacheHit: true, wantID: 1, wantEvents: []string{"list", "set"}},
		{name: "cache write failure retains database rules", setFails: true, wantID: 1, wantEvents: []string{"list", "set"}},
		{name: "database failure falls back to cache", dbFails: true, cacheHit: true, wantID: 2, wantEvents: []string{"list", "get"}},
		{name: "cache miss retries database without changing failure policy", dbFails: true, wantEvents: []string{"list", "get", "list"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var events []string
			repo := &lifecyclePromptRuleRepo{list: func(context.Context) ([]*model.PromptRule, error) {
				events = append(events, "list")
				if test.dbFails {
					return nil, errors.New("database unavailable")
				}
				return []*model.PromptRule{{ID: 1}}, nil
			}}
			cache := &lifecyclePromptRuleCache{
				get: func(context.Context) ([]*model.PromptRule, bool) {
					events = append(events, "get")
					return []*model.PromptRule{{ID: 2}}, test.cacheHit
				},
				set: func(context.Context, []*model.PromptRule) error {
					events = append(events, "set")
					if test.setFails {
						return errors.New("cache unavailable")
					}
					return nil
				},
			}
			svc := NewPromptRuleService(repo, cache)
			require.Equal(t, test.wantEvents, events)
			require.Zero(t, cache.starts.Load())
			require.Nil(t, svc.subscription)
			if test.wantID == 0 {
				// Do not call getCachedRules: its existing lazy retry is separate.
				require.Nil(t, svc.localCache)
			} else {
				require.Len(t, svc.localCache, 1)
				require.Equal(t, test.wantID, svc.localCache[0].ID)
			}
		})
	}
}

func TestPromptRuleLifecycleIsIdempotentAndRefreshesNotifications(t *testing.T) {
	entered := make(chan context.Context, 2)
	updates := make(chan struct{}, 1)
	refreshed := make(chan struct{}, 1)
	cache := &lifecyclePromptRuleCache{
		get: func(context.Context) ([]*model.PromptRule, bool) {
			return []*model.PromptRule{{ID: 2}}, true
		},
		subscribe: func(ctx context.Context, handler func()) {
			entered <- ctx
			for {
				select {
				case <-ctx.Done():
					return
				case <-updates:
					handler()
					refreshed <- struct{}{}
				}
			}
		},
	}
	svc := NewPromptRuleService(&lifecyclePromptRuleRepo{}, cache)
	cleanupPromptRuleLifecycle(t, svc)

	var starters sync.WaitGroup
	startErrors := make(chan error, 16)
	for range 16 {
		starters.Add(1)
		go func() {
			defer starters.Done()
			startErrors <- svc.Start(context.Background())
		}()
	}
	starters.Wait()
	close(startErrors)
	for err := range startErrors {
		require.NoError(t, err)
	}
	var runCtx context.Context
	select {
	case runCtx = <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription did not start")
	}
	require.EqualValues(t, 1, cache.starts.Load())
	updates <- struct{}{}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("notification did not refresh rules")
	}
	require.Equal(t, int64(2), svc.getCachedRules()[0].ID)
	require.NoError(t, svc.Shutdown(context.Background()))
	require.ErrorIs(t, runCtx.Err(), context.Canceled)
	require.NoError(t, svc.Shutdown(context.Background()))

	require.NoError(t, svc.Start(context.Background()))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription did not restart after completed shutdown")
	}
	require.EqualValues(t, 2, cache.starts.Load())
}

func TestPromptRuleShutdownTimeoutKeepsInFlightCallbackOwned(t *testing.T) {
	entered := make(chan struct{}, 2)
	updates := make(chan struct{}, 1)
	refreshContext := make(chan context.Context, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	cache := &lifecyclePromptRuleCache{
		get: func(ctx context.Context) ([]*model.PromptRule, bool) {
			refreshContext <- ctx
			<-release // Simulate an in-flight callback finishing after cancellation.
			return nil, true
		},
		subscribe: func(ctx context.Context, handler func()) {
			entered <- struct{}{}
			for {
				select {
				case <-ctx.Done():
					return
				case <-updates:
					handler()
				}
			}
		},
	}
	svc := NewPromptRuleService(&lifecyclePromptRuleRepo{}, cache)
	cleanupPromptRuleLifecycle(t, svc)
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	require.NoError(t, svc.Start(context.Background()))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription did not start")
	}
	updates <- struct{}{}
	var callbackCtx context.Context
	select {
	case callbackCtx = <-refreshContext:
	case <-time.After(time.Second):
		t.Fatal("notification callback did not start")
	}
	run := svc.subscription
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, svc.Shutdown(shutdownCtx), context.DeadlineExceeded)
	require.ErrorIs(t, callbackCtx.Err(), context.Canceled, "refresh must use the subscription context")
	require.Same(t, run, svc.subscription, "a timed-out shutdown must retain ownership")
	require.Error(t, svc.Start(context.Background()), "cannot replace a still-stopping run")
	require.EqualValues(t, 1, cache.starts.Load())
	select {
	case <-run.done:
		t.Fatal("subscription finished before its callback")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	finishCtx, finishCancel := context.WithTimeout(context.Background(), time.Second)
	defer finishCancel()
	require.NoError(t, svc.Shutdown(finishCtx))
	require.Nil(t, svc.subscription)
	require.NoError(t, svc.Start(context.Background()))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("subscription did not restart after callback finished")
	}
	require.EqualValues(t, 2, cache.starts.Load())
}

func TestPromptRuleLifecycleCancellationAndInvalidInputs(t *testing.T) {
	var missing *PromptRuleService
	require.Error(t, missing.Start(context.Background()))
	require.Error(t, missing.Shutdown(context.Background()))
	svc := NewPromptRuleService(&lifecyclePromptRuleRepo{}, nil)
	require.NoError(t, svc.Start(context.Background()), "nil cache is a valid configuration")
	require.NoError(t, svc.Shutdown(context.Background()))
	require.Error(t, svc.Start(nil))
	require.Error(t, svc.Shutdown(nil))

	cache := &lifecyclePromptRuleCache{}
	svc = NewPromptRuleService(&lifecyclePromptRuleRepo{}, cache)
	cleanupPromptRuleLifecycle(t, svc)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, svc.Start(ctx), context.Canceled)
	require.Zero(t, cache.starts.Load())
	require.Nil(t, svc.subscription)

	ctx, cancel = context.WithCancel(context.Background())
	require.NoError(t, svc.Start(ctx))
	run := svc.subscription
	cancel()
	select {
	case <-run.done:
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not stop subscription")
	}
	// Completion takes precedence over an expired shutdown deadline.
	require.NoError(t, svc.Shutdown(ctx))
}
