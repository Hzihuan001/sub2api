package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestImageTaskStoreRoundTripAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:        "imgtask_123",
		UserID:    7,
		APIKeyID:  9,
		Status:    service.ImageTaskStatusProcessing,
		CreatedAt: 100,
		ExpiresAt: 200,
	}

	require.NoError(t, store.Save(context.Background(), task, 24*time.Hour))
	got, err := store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, got)
	require.Equal(t, 24*time.Hour, mr.TTL(imageTaskKey(task.ID)))
}

func TestImageTaskStoreIdempotencyAcrossServiceInstances(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	// Distinct service/store objects share only Redis, never a process mutex.
	services := []*service.ImageTaskService{
		service.NewImageTaskService(NewImageTaskStore(rdb)),
		service.NewImageTaskService(NewImageTaskStore(rdb)),
	}
	owner := service.ImageTaskOwner{UserID: 7, APIKeyID: 9}
	var createdCount atomic.Int32
	ids := make(chan string, 32)
	errs := make(chan error, 32)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			task, created, err := services[index%2].CreateIdempotent(context.Background(), owner, "image-123", "fingerprint-a")
			if err != nil {
				errs <- err
				return
			}
			if created {
				createdCount.Add(1)
			}
			ids <- task.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, createdCount.Load())
	id := ""
	for actual := range ids {
		if id == "" {
			id = actual
		}
		require.Equal(t, id, actual)
	}
	_, _, err := services[0].CreateIdempotent(context.Background(), owner, "image-123", "fingerprint-b")
	require.ErrorIs(t, err, service.ErrImageTaskIdempotencyConflict)
	for _, differentOwner := range []service.ImageTaskOwner{{UserID: 7, APIKeyID: 10}, {UserID: 8, APIKeyID: 9}} {
		task, created, err := services[0].CreateIdempotent(context.Background(), differentOwner, "image-123", "fingerprint-b")
		require.NoError(t, err)
		require.True(t, created)
		require.NotEqual(t, id, task.ID)
	}
}

func TestImageTaskStoreFinishCASAndLateUnknownRecovery(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	ctx := context.Background()
	for _, initialTerminal := range []string{service.ImageTaskStatusCompleted, service.ImageTaskStatusFailed} {
		t.Run(initialTerminal, func(t *testing.T) {
			task := &service.ImageTaskRecord{ID: fmt.Sprintf("imgtask_%s", initialTerminal), Status: service.ImageTaskStatusProcessing, IdempotencyKeyHash: initialTerminal, RequestFingerprint: "fp"}
			_, _, err := store.CreateIfAbsent(ctx, task, time.Hour)
			require.NoError(t, err)
			staleTimeout := *task
			staleTimeout.Status, staleTimeout.ResultUnknown = service.ImageTaskStatusFailed, true
			task.Status = initialTerminal
			task.Result = json.RawMessage(`{"data":[{"url":"https://example.test/original.png"}]}`)
			_, err = store.Finish(ctx, task, 24*time.Hour)
			require.NoError(t, err)
			actual, err := store.Finish(ctx, &staleTimeout, 24*time.Hour)
			require.NoError(t, err)
			require.Equal(t, initialTerminal, actual.Status)
			require.False(t, actual.ResultUnknown)
			task.Status = service.ImageTaskStatusCompleted
			task.Result = json.RawMessage(`{"data":[{"url":"https://example.test/replacement.png"}]}`)
			actual, err = store.Finish(ctx, task, 24*time.Hour)
			require.NoError(t, err)
			require.Equal(t, initialTerminal, actual.Status)
			require.Contains(t, string(actual.Result), "original.png")
		})
	}
	task := &service.ImageTaskRecord{ID: "imgtask_late", Status: service.ImageTaskStatusProcessing, IdempotencyKeyHash: "late", RequestFingerprint: "fp"}
	_, _, err := store.CreateIfAbsent(ctx, task, time.Hour)
	require.NoError(t, err)
	mr.FastForward(30 * time.Minute)
	task.Status, task.ResultUnknown = service.ImageTaskStatusFailed, true
	_, err = store.Finish(ctx, task, 24*time.Hour)
	require.NoError(t, err)
	task.Status, task.ResultUnknown, task.Result = service.ImageTaskStatusCompleted, false, json.RawMessage(`{"data":[{"url":"https://example.test/late.png"}]}`)
	got, err := store.Finish(ctx, task, 24*time.Hour)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusCompleted, got.Status)
	require.Equal(t, 24*time.Hour, mr.TTL(imageTaskKey(task.ID)))
	require.Equal(t, 24*time.Hour, mr.TTL(imageTaskIdempotencyKey(task)))
	_, created, err := store.CreateIfAbsent(ctx, &service.ImageTaskRecord{ID: "imgtask_duplicate", Status: service.ImageTaskStatusProcessing, IdempotencyKeyHash: "late", RequestFingerprint: "fp"}, 24*time.Hour)
	require.NoError(t, err)
	require.False(t, created)
}

func TestImageTaskStoreMissingClaimTargetNeverRecreates(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{ID: "imgtask_original", IdempotencyKeyHash: "claim", RequestFingerprint: "fp"}
	_, _, err := store.CreateIfAbsent(context.Background(), task, time.Hour)
	require.NoError(t, err)
	mr.Del(imageTaskKey(task.ID))
	_, err = store.FindIdempotent(context.Background(), "claim")
	require.ErrorIs(t, err, service.ErrImageTaskUnavailable)
	task.ID = "imgtask_new"
	_, _, err = store.CreateIfAbsent(context.Background(), task, time.Hour)
	require.Error(t, err)
	require.False(t, mr.Exists(imageTaskKey(task.ID)))
}

func TestImageTaskStoreMissing(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)

	_, err := store.Get(context.Background(), "imgtask_missing")
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
}
