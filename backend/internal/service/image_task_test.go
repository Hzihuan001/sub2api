package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskMemoryStore struct {
	mu             sync.Mutex
	task           *ImageTaskRecord
	ttl            time.Duration
	saveErr        error
	getErr         error
	finishFailures int
	finishCalls    int
}

func (s *imageTaskMemoryStore) Save(_ context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *task
	s.task = &copy
	s.ttl = ttl
	return nil
}

func (s *imageTaskMemoryStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func (s *imageTaskMemoryStore) CreateIfAbsent(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) (*ImageTaskRecord, bool, error) {
	if err := s.Save(ctx, task, ttl); err != nil {
		return nil, false, err
	}
	copy := *task
	return &copy, true, nil
}

func (s *imageTaskMemoryStore) Finish(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) (*ImageTaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.finishCalls++
	if s.finishFailures > 0 {
		s.finishFailures--
		return nil, errors.New("temporary Redis error")
	}
	if s.saveErr != nil {
		return nil, s.saveErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	if s.task.Status == ImageTaskStatusProcessing || (s.task.Status == ImageTaskStatusFailed && s.task.ResultUnknown && task.Status == ImageTaskStatusCompleted) {
		copy := *task
		s.task, s.ttl = &copy, ttl
	}
	copy := *s.task
	return &copy, nil
}

func (s *imageTaskMemoryStore) FindIdempotent(_ context.Context, keyHash string) (*ImageTaskRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.task == nil || s.task.IdempotencyKeyHash != keyHash {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func TestImageTaskServiceLifecycleAndOwnership(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, created.Status)
	require.Equal(t, created.ID, created.TaskID)
	require.Equal(t, "image.generation.task", created.Object)
	require.Equal(t, time.Hour, store.ttl)
	require.Equal(t, owner.UserID, store.task.UserID)
	require.Equal(t, owner.APIKeyID, store.task.APIKeyID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://example.test/image.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	completed, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Equal(t, http.StatusOK, completed.HTTPStatus)
	require.Equal(t, "https://example.test/image.png", completed.ImageURL)
	require.JSONEq(t, string(result), string(completed.Result))
	require.NotNil(t, completed.CompletedAt)
}

func TestImageTaskServiceInvalidResultBecomesFailed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)

	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, json.RawMessage(`not-json`)))
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Equal(t, http.StatusBadGateway, got.HTTPStatus)
	require.Contains(t, string(got.Error), "non-JSON")
}

func TestImageTaskServiceMapsStoreFailures(t *testing.T) {
	store := &imageTaskMemoryStore{saveErr: errors.New("redis down")}
	svc := NewImageTaskService(store)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

func TestImageTaskServicePersistenceSurvivesCancellationAndRetries(t *testing.T) {
	for _, complete := range []bool{true, false} {
		t.Run(fmt.Sprintf("complete=%v", complete), func(t *testing.T) {
			store := &imageTaskMemoryStore{}
			svc := NewImageTaskService(store)
			owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
			task, err := svc.Create(context.Background(), owner)
			require.NoError(t, err)
			store.finishFailures = 2
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if complete {
				err = svc.Complete(ctx, task.ID, 200, json.RawMessage(`{"data":[{"url":"https://example.test/result.png"}]}`))
			} else {
				err = svc.Fail(ctx, task.ID, 502, imageTaskErrorJSON("upstream_error", "provider failed"))
			}
			require.NoError(t, err)
			require.Equal(t, 3, store.finishCalls)
			got, err := svc.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			if complete {
				require.Equal(t, ImageTaskStatusCompleted, got.Status)
			} else {
				require.Equal(t, ImageTaskStatusFailed, got.Status)
			}
		})
	}
}

func TestImageTaskServiceExpiredProcessingBecomesUnknownAndLateResultRecovers(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) {
			store := &imageTaskMemoryStore{}
			svc := NewImageTaskService(store)
			owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
			task, err := svc.Create(context.Background(), owner)
			require.NoError(t, err)
			if legacy {
				store.task.ExecutionDeadline = 0
				store.task.CreatedAt = time.Now().Add(-defaultImageTaskExecutionTimeout - imageTaskPersistenceTimeout - time.Minute).Unix()
			} else {
				store.task.ExecutionDeadline = time.Now().Add(-imageTaskPersistenceTimeout - time.Minute).Unix()
			}
			_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 99, APIKeyID: 2}, task.ID)
			require.ErrorIs(t, err, ErrImageTaskNotFound)
			require.Equal(t, ImageTaskStatusProcessing, store.task.Status, "another owner cannot expire the task")
			got, err := svc.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			require.Equal(t, ImageTaskStatusFailed, got.Status)
			require.Contains(t, string(got.Error), "image_task_result_unknown")
			require.NotZero(t, got.ExecutionDeadline)
			require.NoError(t, svc.Complete(context.Background(), task.ID, 200, json.RawMessage(`{"data":[{"url":"https://example.test/late.png"}]}`)))
			got, err = svc.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			require.Equal(t, ImageTaskStatusCompleted, got.Status)
			require.Contains(t, string(got.Result), "late.png")
			require.NoError(t, svc.Fail(context.Background(), task.ID, 500, imageTaskErrorJSON("api_error", "late failure")))
			got, err = svc.Get(context.Background(), owner, task.ID)
			require.NoError(t, err)
			require.Equal(t, ImageTaskStatusCompleted, got.Status)
		})
	}
}

func TestImageTaskServiceDeadlineAllowsPersistenceGrace(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskService(store)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	task, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	store.task.ExecutionDeadline = time.Now().Add(-time.Minute).Unix()
	got, err := svc.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, got.Status)
	require.Zero(t, store.finishCalls)
}

func TestImageTaskIdempotencyKeyValidation(t *testing.T) {
	for _, key := range []string{"a", "task-123_abc.1:2", strings.Repeat("a", 128)} {
		require.NoError(t, ValidateImageTaskIdempotencyKey(key))
	}
	for _, key := range []string{"", " leading", "trailing ", "a/b", "你好", strings.Repeat("a", 129)} {
		require.ErrorIs(t, ValidateImageTaskIdempotencyKey(key), ErrImageTaskInvalidIdempotencyKey)
	}
}

func TestImageTaskServiceCheckpointSurvivesRedisOutageAndRestart(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	local := NewLocalImageStorage(t.TempDir())
	svc.SetLocalStorage(local)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	task, _, err := svc.CreateIdempotent(context.Background(), owner, "checkpoint-test", "fingerprint")
	require.NoError(t, err)
	binding, ok := svc.CaptureCompletionBinding()
	require.True(t, ok)
	complete := svc.CompletionWriter(task, owner, "checkpoint-test", "fingerprint", binding)
	store.getErr, store.saveErr = errors.New("Redis unavailable"), errors.New("Redis unavailable")
	result := json.RawMessage(`{"data":[{"url":"https://example.test/saved.png"}]}`)
	require.ErrorIs(t, complete(context.Background(), task.ID, http.StatusOK, result), ErrImageTaskUnavailable)

	// A fresh service can authenticate and return a completed local checkpoint
	// even if every Redis metadata write failed after image storage succeeded.
	restarted := NewImageTaskService(store)
	restarted.SetLocalStorage(local)
	got, err := restarted.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, got.Status)
	require.JSONEq(t, string(result), string(got.Result))
	_, err = restarted.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 99}, task.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
	saved, err := local.LoadResult(task.ID)
	require.NoError(t, err)
	require.Equal(t, imageTaskIdempotencyHash(owner, "checkpoint-test"), saved.IdempotencyKeyHash)
	require.Equal(t, "fingerprint", saved.RequestFingerprint)
}

func TestImageTaskServiceAcceptedStorageBindingSurvivesDisable(t *testing.T) {
	store := &imageTaskMemoryStore{}
	storage := &fakeImageStorage{}
	uploader := NewImageResultUploader(storage, "images/", 0, nil)
	enabled := true
	svc := NewImageTaskServiceWithResolver(store, func() (*ImageResultUploader, bool) {
		if !enabled {
			return nil, false
		}
		return uploader, true
	}, time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 1, APIKeyID: 2}
	binding, ok := svc.CaptureCompletionBinding()
	require.True(t, ok)
	task, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	complete := svc.CompletionWriter(task, owner, "", "", binding)
	enabled = false
	result := json.RawMessage(fmt.Sprintf(`{"data":[{"b64_json":"%s"}]}`, base64.StdEncoding.EncodeToString(pngBytes)))
	require.NoError(t, complete(context.Background(), task.ID, http.StatusOK, result))
	got, err := svc.Get(context.Background(), owner, task.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, got.Status)
	require.Len(t, storage.saved, 1)
	require.Contains(t, string(got.Result), "https://cdn.test/")
	require.NotContains(t, string(got.Result), "b64_json")
	require.NotContains(t, string(got.Result), base64.StdEncoding.EncodeToString(pngBytes))
	_, ok = svc.CaptureCompletionBinding()
	require.False(t, ok, "new submissions are disabled while the accepted task retains its binding")
}
