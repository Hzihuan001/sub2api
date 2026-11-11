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
	dir         string
	mu          sync.Mutex
	lastCleanup time.Time
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
	s.cleanup()
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
	s.cleanup()
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

// Results remain queryable for 24 hours after completion. A 48-hour file
// retention window leaves room for completion persistence retries. Cleanup is
// throttled and runs only against strictly named files in this private folder.
func (s *LocalImageStorage) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.lastCleanup) < time.Hour {
		return
	}
	s.lastCleanup = time.Now()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-48 * time.Hour)
	for _, entry := range entries {
		manifest := strings.HasSuffix(entry.Name(), ".json") && localImageTaskPattern.MatchString(strings.TrimSuffix(entry.Name(), ".json"))
		if entry.IsDir() || (!manifest && !localImageNamePattern.MatchString(entry.Name()) && !strings.HasPrefix(entry.Name(), ".image-")) {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(s.dir, entry.Name()))
		}
	}
}

func (s *ImageTaskService) SetLocalStorage(storage *LocalImageStorage) { s.localStorage = storage }

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
