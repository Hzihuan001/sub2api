package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const imageTaskKeyPrefix = "image_task:"
const imageTaskIdempotencyKeyPrefix = "image_task_idempotency:"

var createImageTaskScript = redis.NewScript(`
if KEYS[2] ~= '' then
  local existing = redis.call('GET', KEYS[2])
  if existing then
    local data = redis.call('GET', ARGV[3] .. existing)
    if not data then return redis.error_reply('idempotent image task is unavailable') end
    local task = cjson.decode(data)
    if task.request_fingerprint ~= ARGV[4] then return {-1, data} end
    return {0, data}
  end
end
if redis.call('EXISTS', KEYS[1]) == 1 then return redis.error_reply('image task id collision') end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
if KEYS[2] ~= '' then redis.call('SET', KEYS[2], ARGV[5], 'PX', ARGV[2]) end
return {1, ARGV[1]}
`)

var finishImageTaskScript = redis.NewScript(`
local data = redis.call('GET', KEYS[1])
if not data then return nil end
local old = cjson.decode(data)
local next = cjson.decode(ARGV[1])
if old.status ~= 'processing' and not (old.status == 'failed' and old.result_unknown == true and next.status == 'completed') then
  return data
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
if KEYS[2] ~= '' and redis.call('GET', KEYS[2]) == next.id then
  redis.call('PEXPIRE', KEYS[2], ARGV[2])
end
return ARGV[1]
`)

type imageTaskStore struct {
	rdb *redis.Client
}

func NewImageTaskStore(rdb *redis.Client) service.ImageTaskStore {
	return &imageTaskStore{rdb: rdb}
}

func (s *imageTaskStore) Save(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) error {
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, imageTaskKey(task.ID), data, ttl).Err()
}

func (s *imageTaskStore) Get(ctx context.Context, id string) (*service.ImageTaskRecord, error) {
	data, err := s.rdb.Get(ctx, imageTaskKey(id)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, service.ErrImageTaskNotFound
		}
		return nil, err
	}
	var task service.ImageTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *imageTaskStore) CreateIfAbsent(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) (*service.ImageTaskRecord, bool, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, false, err
	}
	claim := imageTaskIdempotencyKey(task)
	result, err := createImageTaskScript.Run(ctx, s.rdb, []string{imageTaskKey(task.ID), claim}, data, ttl.Milliseconds(), imageTaskKeyPrefix, task.RequestFingerprint, task.ID).Slice()
	if err != nil {
		return nil, false, err
	}
	if len(result) != 2 {
		return nil, false, errors.New("image task idempotency script returned an invalid result")
	}
	status, ok := result[0].(int64)
	if !ok {
		return nil, false, errors.New("image task idempotency script returned an invalid status")
	}
	dataString, ok := result[1].(string)
	if !ok {
		return nil, false, errors.New("image task idempotency script returned invalid task data")
	}
	if status == -1 {
		return nil, false, service.ErrImageTaskIdempotencyConflict
	}
	var stored service.ImageTaskRecord
	if err := json.Unmarshal([]byte(dataString), &stored); err != nil {
		return nil, false, err
	}
	if status != 0 && status != 1 {
		return nil, false, errors.New("image task idempotency script returned an unknown status")
	}
	return &stored, status == 1, nil
}

func (s *imageTaskStore) Finish(ctx context.Context, task *service.ImageTaskRecord, ttl time.Duration) (*service.ImageTaskRecord, error) {
	data, err := json.Marshal(task)
	if err != nil {
		return nil, err
	}
	result, err := finishImageTaskScript.Run(ctx, s.rdb, []string{imageTaskKey(task.ID), imageTaskIdempotencyKey(task)}, data, ttl.Milliseconds()).Text()
	if err == redis.Nil {
		return nil, service.ErrImageTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	var stored service.ImageTaskRecord
	if err := json.Unmarshal([]byte(result), &stored); err != nil {
		return nil, err
	}
	return &stored, nil
}

func (s *imageTaskStore) FindIdempotent(ctx context.Context, keyHash string) (*service.ImageTaskRecord, error) {
	id, err := s.rdb.Get(ctx, imageTaskIdempotencyKeyPrefix+keyHash).Result()
	if err == redis.Nil {
		return nil, service.ErrImageTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	task, err := s.Get(ctx, id)
	// A surviving claim with a missing task must fail closed: never authorize a
	// second generation merely because task retention/state was interrupted.
	if err == service.ErrImageTaskNotFound {
		return nil, service.ErrImageTaskUnavailable
	}
	return task, err
}

func imageTaskIdempotencyKey(task *service.ImageTaskRecord) string {
	if task.IdempotencyKeyHash == "" {
		return ""
	}
	return imageTaskIdempotencyKeyPrefix + task.IdempotencyKeyHash
}

func imageTaskKey(id string) string {
	return imageTaskKeyPrefix + strings.TrimSpace(id)
}
