package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	ImageTaskStatusProcessing = "processing"
	ImageTaskStatusCompleted  = "completed"
	ImageTaskStatusFailed     = "failed"

	defaultImageTaskTTL              = 24 * time.Hour
	defaultImageTaskExecutionTimeout = 30 * time.Minute
	imageTaskPersistenceTimeout      = 5 * time.Minute
	imageTaskPersistenceAttempts     = 3
)

var (
	ErrImageTaskNotFound              = infraerrors.New(http.StatusNotFound, "IMAGE_TASK_NOT_FOUND", "image task not found")
	ErrImageTaskForbidden             = infraerrors.New(http.StatusForbidden, "IMAGE_TASK_FORBIDDEN", "image task does not belong to this API key")
	ErrImageTaskUnavailable           = infraerrors.New(http.StatusServiceUnavailable, "IMAGE_TASK_UNAVAILABLE", "image task storage is unavailable")
	ErrImageTaskIdempotencyConflict   = infraerrors.New(http.StatusConflict, "IMAGE_TASK_IDEMPOTENCY_CONFLICT", "Idempotency-Key was already used for a different image request")
	ErrImageTaskInvalidIdempotencyKey = infraerrors.New(http.StatusBadRequest, "IMAGE_TASK_INVALID_IDEMPOTENCY_KEY", "Idempotency-Key must contain 1 to 128 ASCII letters, digits, '.', '_', ':', or '-'")
)

// ImageTaskRecord is the private Redis representation of an asynchronous image
// request. Ownership fields are intentionally omitted from the public view.
type ImageTaskRecord struct {
	ID                 string          `json:"id"`
	UserID             int64           `json:"user_id"`
	APIKeyID           int64           `json:"api_key_id"`
	Status             string          `json:"status"`
	HTTPStatus         int             `json:"http_status,omitempty"`
	Result             json.RawMessage `json:"result,omitempty"`
	Error              json.RawMessage `json:"error,omitempty"`
	CreatedAt          int64           `json:"created_at"`
	CompletedAt        *int64          `json:"completed_at,omitempty"`
	ExpiresAt          int64           `json:"expires_at"`
	ExecutionDeadline  int64           `json:"execution_deadline,omitempty"`
	IdempotencyKeyHash string          `json:"idempotency_key_hash,omitempty"`
	RequestFingerprint string          `json:"request_fingerprint,omitempty"`
	ResultUnknown      bool            `json:"result_unknown,omitempty"`
}

// ImageTask is the API-safe task representation returned to callers.
type ImageTask struct {
	ID                string          `json:"id"`
	TaskID            string          `json:"task_id"`
	Object            string          `json:"object"`
	Status            string          `json:"status"`
	HTTPStatus        int             `json:"http_status,omitempty"`
	ImageURL          string          `json:"image_url,omitempty"`
	Result            json.RawMessage `json:"result,omitempty"`
	Error             json.RawMessage `json:"error,omitempty"`
	CreatedAt         int64           `json:"created_at"`
	CompletedAt       *int64          `json:"completed_at,omitempty"`
	ExpiresAt         int64           `json:"expires_at"`
	ExecutionDeadline int64           `json:"execution_deadline,omitempty"`
}

type ImageTaskOwner struct {
	UserID   int64
	APIKeyID int64
}

type ImageTaskStore interface {
	Save(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) error
	Get(ctx context.Context, id string) (*ImageTaskRecord, error)
	// CreateIfAbsent atomically claims an owner-scoped idempotency key and saves
	// its task. A duplicate must never authorize another upstream execution.
	CreateIfAbsent(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) (*ImageTaskRecord, bool, error)
	FindIdempotent(ctx context.Context, keyHash string) (*ImageTaskRecord, error)
	// Finish atomically preserves terminal states, except that a late successful
	// result may replace a result-unknown timeout. It returns the actual state.
	Finish(ctx context.Context, task *ImageTaskRecord, ttl time.Duration) (*ImageTaskRecord, error)
}

// ImageStorageResolver reports the currently effective object-storage binding.
// It exists so the async image feature can be switched on and off from the admin
// UI without a restart: the wiring below is fixed at startup, but the answer to
// "is object storage configured right now" is re-read (and cached) per call.
type ImageStorageResolver func() (uploader *ImageResultUploader, enabled bool)

type ImageTaskService struct {
	store            ImageTaskStore
	localStorage     *LocalImageStorage
	uploader         *ImageResultUploader
	enabled          bool
	resolve          ImageStorageResolver
	ttl              time.Duration
	executionTimeout time.Duration
}

func NewImageTaskService(store ImageTaskStore) *ImageTaskService {
	return NewImageTaskServiceWithOptions(store, defaultImageTaskTTL, defaultImageTaskExecutionTimeout)
}

func NewImageTaskServiceWithOptions(store ImageTaskStore, ttl, executionTimeout time.Duration) *ImageTaskService {
	if ttl <= 0 {
		ttl = defaultImageTaskTTL
	}
	if executionTimeout <= 0 {
		executionTimeout = defaultImageTaskExecutionTimeout
	}
	return &ImageTaskService{store: store, ttl: ttl, executionTimeout: executionTimeout}
}

// NewImageTaskServiceWithUploader 构造一个已启用的图片任务服务：结果会先经 uploader
// 转存到对象存储再落 Redis。uploader 为 nil 时不做转存（仅用于测试）。
func NewImageTaskServiceWithUploader(store ImageTaskStore, uploader *ImageResultUploader, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.uploader = uploader
	s.enabled = true
	return s
}

// NewImageTaskServiceWithResolver 构造一个由 resolver 决定启用状态的服务：
// 开关与凭证来自后台设置，保存后立即生效，无需重启。
func NewImageTaskServiceWithResolver(store ImageTaskStore, resolve ImageStorageResolver, ttl, executionTimeout time.Duration) *ImageTaskService {
	s := NewImageTaskServiceWithOptions(store, ttl, executionTimeout)
	s.resolve = resolve
	return s
}

// current 返回当前生效的 uploader 与启用状态。
// 注入了 resolver 时以 resolver 为准（后台设置可热切换），否则回落到构造时固定的值。
func (s *ImageTaskService) current() (*ImageResultUploader, bool) {
	if s == nil {
		return nil, false
	}
	if s.resolve != nil {
		return s.resolve()
	}
	return s.uploader, s.enabled
}

// Enabled 表示异步图片任务功能是否可用（总开关 + 凭证齐全）。
// 关闭时 handler 直接返回 404，不创建任务、不写 Redis。
func (s *ImageTaskService) Enabled() bool {
	if s == nil || s.store == nil {
		return false
	}
	_, enabled := s.current()
	return enabled
}

// Pollable 表示已创建的任务能否被查询。
// 比 Enabled 弱：只要 store 可用即可，从而在功能被关掉后仍能取回进行中的任务结果。
func (s *ImageTaskService) Pollable() bool {
	return s != nil && s.store != nil
}

func (s *ImageTaskService) ExecutionTimeout() time.Duration {
	if s == nil || s.executionTimeout <= 0 {
		return defaultImageTaskExecutionTimeout
	}
	return s.executionTimeout
}

func (s *ImageTaskService) Create(ctx context.Context, owner ImageTaskOwner) (*ImageTask, error) {
	task, _, err := s.CreateIdempotent(ctx, owner, "", "")
	return task, err
}

func ValidateImageTaskIdempotencyKey(key string) error {
	if len(key) < 1 || len(key) > 128 {
		return ErrImageTaskInvalidIdempotencyKey
	}
	for _, ch := range key {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || strings.ContainsRune("._:-", ch) {
			continue
		}
		return ErrImageTaskInvalidIdempotencyKey
	}
	return nil
}

func (s *ImageTaskService) CreateIdempotent(ctx context.Context, owner ImageTaskOwner, key, fingerprint string) (*ImageTask, bool, error) {
	if s == nil || s.store == nil {
		return nil, false, ErrImageTaskUnavailable
	}
	keyHash := ""
	if key != "" {
		if err := ValidateImageTaskIdempotencyKey(key); err != nil {
			return nil, false, err
		}
		keyHash = imageTaskIdempotencyHash(owner, key)
	}
	now := time.Now().UTC()
	task := &ImageTaskRecord{
		ID:                 "imgtask_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		UserID:             owner.UserID,
		APIKeyID:           owner.APIKeyID,
		Status:             ImageTaskStatusProcessing,
		CreatedAt:          now.Unix(),
		ExpiresAt:          now.Add(s.ttl).Unix(),
		ExecutionDeadline:  now.Add(s.ExecutionTimeout()).Unix(),
		IdempotencyKeyHash: keyHash,
		RequestFingerprint: fingerprint,
	}
	stored, created, err := s.store.CreateIfAbsent(ctx, task, s.ttl)
	if err != nil {
		if errors.Is(err, ErrImageTaskIdempotencyConflict) {
			return nil, false, err
		}
		return nil, false, ErrImageTaskUnavailable.WithCause(err)
	}
	return imageTaskToPublic(stored), created, nil
}

func imageTaskIdempotencyHash(owner ImageTaskOwner, key string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", owner.UserID, owner.APIKeyID, key)))
	return hex.EncodeToString(sum[:])
}

func (s *ImageTaskService) FindIdempotent(ctx context.Context, owner ImageTaskOwner, key, fingerprint string) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	if err := ValidateImageTaskIdempotencyKey(key); err != nil {
		return nil, err
	}
	task, err := s.store.FindIdempotent(ctx, imageTaskIdempotencyHash(owner, key))
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, err
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.RequestFingerprint != fingerprint {
		return nil, ErrImageTaskIdempotencyConflict
	}
	return s.Get(ctx, owner, task.ID)
}

func (s *ImageTaskService) Get(ctx context.Context, owner ImageTaskOwner, id string) (*ImageTask, error) {
	if s == nil || s.store == nil {
		return nil, ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, strings.TrimSpace(id))
	if s.localStorage != nil && (err != nil || task.Status == ImageTaskStatusProcessing || task.ResultUnknown) {
		// A compact checkpoint survives Redis metadata outages/process restarts
		// after the full image bytes have already been saved locally.
		if saved, loadErr := s.localStorage.LoadResult(strings.TrimSpace(id)); loadErr == nil {
			task, err = saved, nil
		}
	}
	if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return nil, ErrImageTaskNotFound
		}
		return nil, ErrImageTaskUnavailable.WithCause(err)
	}
	if task.UserID != owner.UserID || task.APIKeyID != owner.APIKeyID {
		// Do not reveal whether a random task ID exists for another caller.
		return nil, ErrImageTaskNotFound
	}
	deadline := task.ExecutionDeadline
	if deadline == 0 {
		deadline = time.Unix(task.CreatedAt, 0).Add(s.ExecutionTimeout()).Unix()
		task.ExecutionDeadline = deadline
	}
	if task.Status == ImageTaskStatusProcessing && time.Now().After(time.Unix(deadline, 0).Add(imageTaskPersistenceTimeout)) {
		now := time.Now().UTC().Unix()
		task.Status, task.HTTPStatus, task.CompletedAt, task.ResultUnknown = ImageTaskStatusFailed, http.StatusGatewayTimeout, &now, true
		task.Error = imageTaskResultUnknownJSON()
		task.ExpiresAt = time.Now().Add(s.ttl).Unix()
		task, err = s.store.Finish(ctx, task, s.ttl)
		if err != nil {
			return nil, ErrImageTaskUnavailable.WithCause(err)
		}
	}
	return imageTaskToPublic(task), nil
}

func (s *ImageTaskService) Complete(ctx context.Context, id string, statusCode int, result json.RawMessage) error {
	uploader, _ := s.current()
	if s != nil && s.resolve != nil && uploader == nil {
		return s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("image_task_result_storage_failed", "image result storage is unavailable; do not automatically generate again"))
	}
	return s.complete(ctx, id, statusCode, result, uploader, nil)
}

type ImageTaskCompletionBinding struct{ uploader *ImageResultUploader }

// CaptureCompletionBinding reads the mutable storage configuration once. A
// production resolver must supply storage before a new task can be accepted.
func (s *ImageTaskService) CaptureCompletionBinding() (*ImageTaskCompletionBinding, bool) {
	uploader, enabled := s.current()
	if !enabled || (s.resolve != nil && uploader == nil) {
		return nil, false
	}
	return &ImageTaskCompletionBinding{uploader: uploader}, true
}

// CompletionWriter captures the accepted task's storage binding. Turning the
// feature off after acceptance must not put raw base64 into Redis or strand it.
func (s *ImageTaskService) CompletionWriter(task *ImageTask, owner ImageTaskOwner, idempotencyKey, fingerprint string, binding *ImageTaskCompletionBinding) func(context.Context, string, int, json.RawMessage) error {
	uploader := binding.uploader
	seed := &ImageTaskRecord{ID: task.ID, UserID: owner.UserID, APIKeyID: owner.APIKeyID, Status: ImageTaskStatusProcessing, CreatedAt: task.CreatedAt, ExpiresAt: task.ExpiresAt, ExecutionDeadline: task.ExecutionDeadline}
	seed.RequestFingerprint = fingerprint
	if idempotencyKey != "" {
		seed.IdempotencyKeyHash = imageTaskIdempotencyHash(owner, idempotencyKey)
	}
	return func(ctx context.Context, id string, status int, result json.RawMessage) error {
		return s.complete(ctx, id, status, result, uploader, seed)
	}
}

func (s *ImageTaskService) complete(ctx context.Context, id string, statusCode int, result json.RawMessage, uploader *ImageResultUploader, seed *ImageTaskRecord) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageTaskPersistenceTimeout)
	defer cancel()
	if seed == nil && s != nil && s.store != nil && s.localStorage != nil {
		seed, _ = s.store.Get(persistCtx, id)
	}
	if !json.Valid(result) {
		return s.finishWithRetry(persistCtx, id, ImageTaskStatusFailed, http.StatusBadGateway, nil, imageTaskErrorJSON("api_error", "upstream returned a non-JSON image response"))
	}
	if uploader != nil {
		var rewritten json.RawMessage
		err := retryImageTaskPersistence(persistCtx, func() error {
			var err error
			rewritten, err = uploader.Rewrite(persistCtx, id, result)
			return err
		})
		if err != nil {
			// 转存失败不回退存 base64，避免大 blob 撑爆 Redis：直接把任务标记为失败。
			logger.L().Error("image_task.offload_failed", zap.String("task_id", id), zap.Error(err))
			// Use a fresh bounded budget to make the storage failure visible even
			// when an upload consumed the entire result-persistence budget.
			return s.Fail(ctx, id, http.StatusBadGateway, imageTaskErrorJSON("image_task_result_storage_failed", "image was generated but its result could not be saved; do not automatically generate again"))
		}
		result = rewritten
	}
	return s.finishWithRetry(persistCtx, id, ImageTaskStatusCompleted, statusCode, result, nil, seed)
}

func (s *ImageTaskService) Fail(ctx context.Context, id string, statusCode int, taskErr json.RawMessage) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), imageTaskPersistenceTimeout)
	defer cancel()
	if !json.Valid(taskErr) {
		taskErr = imageTaskErrorJSON("api_error", "image generation failed")
	}
	return s.finishWithRetry(persistCtx, id, ImageTaskStatusFailed, statusCode, nil, taskErr)
}

func (s *ImageTaskService) finishWithRetry(ctx context.Context, id, status string, statusCode int, result, taskErr json.RawMessage, seed ...*ImageTaskRecord) error {
	err := retryImageTaskPersistence(ctx, func() error { return s.finish(ctx, id, status, statusCode, result, taskErr, seed...) })
	if err != nil {
		logger.L().Error("image_task.result_persistence_exhausted", zap.String("task_id", id), zap.String("status", status), zap.Error(err))
	}
	return err
}

func retryImageTaskPersistence(ctx context.Context, save func() error) error {
	var err error
	for attempt := 0; attempt < imageTaskPersistenceAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = save(); err == nil || errors.Is(err, ErrImageTaskNotFound) {
			return err
		}
		if attempt+1 < imageTaskPersistenceAttempts {
			timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return err
}

func (s *ImageTaskService) finish(ctx context.Context, id, status string, statusCode int, result, taskErr json.RawMessage, seed ...*ImageTaskRecord) error {
	if s == nil || s.store == nil {
		return ErrImageTaskUnavailable
	}
	task, err := s.store.Get(ctx, id)
	if err != nil && status == ImageTaskStatusCompleted && s.localStorage != nil && len(seed) > 0 && seed[0] != nil && seed[0].ID == id {
		copy := *seed[0]
		task = &copy
	} else if err != nil {
		if errors.Is(err, ErrImageTaskNotFound) {
			return ErrImageTaskNotFound
		}
		return ErrImageTaskUnavailable.WithCause(err)
	}
	if task.Status == ImageTaskStatusCompleted || (task.Status == ImageTaskStatusFailed && !task.ResultUnknown) {
		return nil
	}
	now := time.Now().UTC()
	completedAt := now.Unix()
	task.Status = status
	task.HTTPStatus = statusCode
	task.Result = result
	task.Error = taskErr
	var taskError struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(taskErr, &taskError)
	task.ResultUnknown = status == ImageTaskStatusFailed && taskError.Code == "image_task_result_unknown"
	task.CompletedAt = &completedAt
	task.ExpiresAt = now.Add(s.ttl).Unix()
	if status == ImageTaskStatusCompleted && s.localStorage != nil {
		if err := s.localStorage.SaveResult(task); err != nil {
			return ErrImageTaskUnavailable.WithCause(err)
		}
	}
	if _, err := s.store.Finish(ctx, task, s.ttl); err != nil {
		return ErrImageTaskUnavailable.WithCause(err)
	}
	return nil
}

func imageTaskToPublic(task *ImageTaskRecord) *ImageTask {
	if task == nil {
		return nil
	}
	return &ImageTask{
		ID:                task.ID,
		TaskID:            task.ID,
		Object:            "image.generation.task",
		Status:            task.Status,
		HTTPStatus:        task.HTTPStatus,
		ImageURL:          firstImageTaskURL(task.Result),
		Result:            task.Result,
		Error:             task.Error,
		CreatedAt:         task.CreatedAt,
		CompletedAt:       task.CompletedAt,
		ExpiresAt:         task.ExpiresAt,
		ExecutionDeadline: task.ExecutionDeadline,
	}
}

func imageTaskResultUnknownJSON() json.RawMessage {
	data, _ := json.Marshal(map[string]string{"type": "timeout_error", "code": "image_task_result_unknown", "message": "image task execution deadline passed without a saved result; the outcome is unknown, do not automatically generate again"})
	return data
}

func firstImageTaskURL(result json.RawMessage) string {
	if len(result) == 0 || !json.Valid(result) {
		return ""
	}
	var response struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(result, &response) != nil || len(response.Data) == 0 {
		return ""
	}
	return strings.TrimSpace(response.Data[0].URL)
}

func imageTaskErrorJSON(errorType, message string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"type": errorType, "message": message})
	return data
}
