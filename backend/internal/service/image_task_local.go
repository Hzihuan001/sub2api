package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var localImageNamePattern = regexp.MustCompile(`^(imgtask_[a-f0-9]{32})-([0-9]+\.(?:png|jpg|webp|gif|avif|bin))$`)
var localImageTaskPattern = regexp.MustCompile(`^imgtask_[a-f0-9]{32}$`)

// LocalImageStorage keeps generated objects on the application's persistent
// data volume. URLs are always served through task ownership authentication.
type LocalImageStorage struct {
	dir        string
	mu         sync.Mutex
	workerMu   sync.Mutex
	cancel     context.CancelFunc
	workerDone chan struct{}
}

func NewLocalImageStorage(dir string) *LocalImageStorage {
	return &LocalImageStorage{dir: filepath.Clean(dir)}
}

func (s *LocalImageStorage) Save(ctx context.Context, key, _ string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	match := localImageNamePattern.FindStringSubmatch(key)
	if match == nil {
		return "", errors.New("invalid local image key")
	}
	if len(data) == 0 || int64(len(data)) > defaultImageMaxDownloadBytes {
		return "", errors.New("local image size out of bounds")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target := filepath.Join(s.dir, key)
	imageURL := "/v1/images/tasks/" + match[1] + "/images/" + match[2]
	// Retries may publish the same object again, but Range readers must never
	// observe different bytes under an already-published image URL.
	if info, err := os.Lstat(target); err == nil {
		if !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
			return "", errors.New("completed image object conflicts")
		}
		existing, readErr := os.ReadFile(target)
		if readErr != nil {
			return "", readErr
		}
		if !bytes.Equal(existing, data) {
			return "", errors.New("completed image object conflicts")
		}
		return imageURL, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	f, err := os.CreateTemp(s.dir, ".image-*")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(tmp) }()
	if _, err = f.Write(data); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	if err = os.Rename(tmp, target); err != nil {
		return "", err
	}
	return imageURL, nil
}

func (s *LocalImageStorage) Open(taskID, filename string) (*os.File, error) {
	key := taskID + "-" + filename
	match := localImageNamePattern.FindStringSubmatch(key)
	if match == nil || match[1] != taskID || match[2] != filename {
		return nil, ErrImageTaskNotFound
	}
	path := filepath.Join(s.dir, key)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrImageTaskNotFound
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrImageTaskNotFound
	}
	return f, nil
}

// SaveResult checkpoints only compact completed results. This allows a later
// authenticated poll to recover an already-generated image if Redis is briefly
// unavailable while completion is being recorded.
func (s *LocalImageStorage) SaveResult(task *ImageTaskRecord) error {
	if task == nil || !localImageTaskPattern.MatchString(task.ID) || task.Status != ImageTaskStatusCompleted {
		return errors.New("invalid completed image task")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, err := s.LoadResult(task.ID); err == nil {
		if existing.UserID != task.UserID || existing.APIKeyID != task.APIKeyID || string(existing.Result) != string(task.Result) {
			return errors.New("completed image task checkpoint conflicts")
		}
		return nil
	}
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("image task checkpoint is not compact")
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.dir, ".image-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(tmp) }()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, task.ID+".json"))
}

func (s *LocalImageStorage) LoadResult(id string) (*ImageTaskRecord, error) {
	if !localImageTaskPattern.MatchString(id) {
		return nil, ErrImageTaskNotFound
	}
	path := filepath.Join(s.dir, id+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, ErrImageTaskNotFound
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrImageTaskNotFound
	}
	var task ImageTaskRecord
	if json.Unmarshal(data, &task) != nil || task.ID != id || task.Status != ImageTaskStatusCompleted || task.ExpiresAt <= time.Now().Unix() {
		return nil, ErrImageTaskNotFound
	}
	return &task, nil
}

const (
	imageTaskLocalFileRetention = defaultImageTaskTTL
	imageTaskCleanupInterval    = time.Hour
)

// StartCleanupWorker removes expired local task results at startup and hourly.
func (s *LocalImageStorage) StartCleanupWorker(interval time.Duration) {
	if s == nil {
		return
	}
	if interval <= 0 {
		interval = imageTaskCleanupInterval
	}
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.workerDone = make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		s.cleanup()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanup()
			}
		}
	}(s.workerDone)
}

func (s *LocalImageStorage) StopCleanupWorker() {
	if s == nil {
		return
	}
	s.workerMu.Lock()
	defer s.workerMu.Unlock()
	if s.cancel == nil {
		return
	}
	s.cancel()
	<-s.workerDone
	s.cancel = nil
	s.workerDone = nil
}

// cleanup scans only task result files and temporary files in this private folder.
func (s *LocalImageStorage) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	now := time.Now()
	cutoff := now.Add(-imageTaskLocalFileRetention)
	expiredTasks := make(map[string]struct{})
	activeTasks := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		taskID := strings.TrimSuffix(entry.Name(), ".json")
		if !localImageTaskPattern.MatchString(taskID) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(s.dir, entry.Name()))
		var task ImageTaskRecord
		if readErr == nil && json.Unmarshal(data, &task) == nil && task.ID == taskID && task.ExpiresAt > now.Unix() {
			activeTasks[taskID] = struct{}{}
			continue
		}
		if readErr != nil || info.ModTime().Before(cutoff) || task.ExpiresAt <= now.Unix() {
			expiredTasks[taskID] = struct{}{}
			_ = os.Remove(filepath.Join(s.dir, entry.Name()))
		}
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := localImageNamePattern.FindStringSubmatch(entry.Name())
		taskExpired := match != nil
		taskActive := false
		if taskExpired {
			_, taskExpired = expiredTasks[match[1]]
			_, taskActive = activeTasks[match[1]]
		}
		isTemporary := strings.HasPrefix(entry.Name(), ".image-")
		if match == nil && !isTemporary {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.Mode().IsRegular() && (taskExpired || (!taskActive && info.ModTime().Before(cutoff))) {
			_ = os.Remove(filepath.Join(s.dir, entry.Name()))
		}
	}
}

func (s *ImageTaskService) SetLocalStorage(storage *LocalImageStorage) { s.localStorage = storage }

func (s *ImageTaskService) Stop() {
	if s != nil && s.localStorage != nil {
		s.localStorage.StopCleanupWorker()
	}
}

func (s *ImageTaskService) OpenLocalImage(ctx context.Context, owner ImageTaskOwner, id, filename string) (*os.File, error) {
	task, err := s.Get(ctx, owner, id)
	if err != nil {
		return nil, err
	}
	if s.localStorage == nil || task.Status != ImageTaskStatusCompleted {
		return nil, ErrImageTaskNotFound
	}
	var body struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(task.Result, &body); err != nil {
		return nil, ErrImageTaskNotFound
	}
	want := fmt.Sprintf("/v1/images/tasks/%s/images/%s", id, filename)
	for _, item := range body.Data {
		if item.URL == want {
			return s.localStorage.Open(id, filename)
		}
	}
	return nil, ErrImageTaskNotFound
}
