//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const localImageTestTaskID = "imgtask_0123456789abcdef0123456789abcdef"

func TestLocalImageStoragePersistsCompleteObjectAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	storage := NewLocalImageStorage(dir)
	data := []byte("complete immutable image object")
	key := localImageTestTaskID + "-0.png"
	imageURL, err := storage.Save(context.Background(), key, "image/png", data)
	require.NoError(t, err)
	require.Equal(t, "/v1/images/tasks/"+localImageTestTaskID+"/images/0.png", imageURL)

	restarted := NewLocalImageStorage(dir)
	f, err := restarted.Open(localImageTestTaskID, "0.png")
	require.NoError(t, err)
	actual, readErr := io.ReadAll(f)
	require.NoError(t, f.Close())
	require.NoError(t, readErr)
	require.Equal(t, data, actual)
	// Retrying completion cannot mutate an object during a Range download.
	repeatedURL, err := restarted.Save(context.Background(), key, "image/png", data)
	require.NoError(t, err)
	require.Equal(t, imageURL, repeatedURL)
	_, err = restarted.Save(context.Background(), key, "image/png", bytes.Repeat([]byte("x"), len(data)))
	require.ErrorContains(t, err, "conflicts")
	published, err := os.ReadFile(filepath.Join(dir, key))
	require.NoError(t, err)
	require.Equal(t, data, published)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the complete published object may remain")
	require.Equal(t, key, entries[0].Name())

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = restarted.Save(cancelled, localImageTestTaskID+"-1.png", "image/png", data)
	require.ErrorIs(t, err, context.Canceled)
	_, err = restarted.Open(localImageTestTaskID, "1.png")
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

func TestLocalImageStorageRejectsInvalidPaths(t *testing.T) {
	storage := NewLocalImageStorage(t.TempDir())
	for _, key := range []string{
		"../" + localImageTestTaskID + "-0.png",
		"nested/" + localImageTestTaskID + "-0.png",
		localImageTestTaskID + "-../0.png",
		localImageTestTaskID + "-0.svg",
		"imgtask_short-0.png",
	} {
		_, err := storage.Save(context.Background(), key, "image/png", []byte("image"))
		require.Error(t, err, "key=%q", key)
	}
	for _, input := range [][2]string{
		{"../" + localImageTestTaskID, "0.png"},
		{localImageTestTaskID, "../0.png"},
		{localImageTestTaskID, `..\0.png`},
		{localImageTestTaskID, "/0.png"},
		{localImageTestTaskID, "0.png/extra"},
		{localImageTestTaskID, "0.svg"},
	} {
		f, err := storage.Open(input[0], input[1])
		require.Nil(t, f)
		require.ErrorIs(t, err, ErrImageTaskNotFound, "task=%q file=%q", input[0], input[1])
	}
}

func TestLocalImageStorageCleanupRetainsFreshAndUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	oldNames := []string{localImageTestTaskID + "-0.png", localImageTestTaskID + ".json", ".image-abandoned"}
	old := time.Now().Add(-49 * time.Hour)
	for _, name := range append(append([]string(nil), oldNames...), "unrelated.txt") {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte("old"), 0600))
		require.NoError(t, os.Chtimes(path, old, old))
	}
	freshName := localImageTestTaskID + "-1.png"
	require.NoError(t, os.WriteFile(filepath.Join(dir, freshName), []byte("fresh"), 0600))
	directoryName := localImageTestTaskID + "-2.png"
	require.NoError(t, os.Mkdir(filepath.Join(dir, directoryName), 0700))
	storage := NewLocalImageStorage(dir)
	_, err := storage.Save(context.Background(), localImageTestTaskID+"-3.png", "image/png", []byte("new"))
	require.NoError(t, err)
	for _, name := range oldNames {
		_, err := os.Stat(filepath.Join(dir, name))
		require.ErrorIs(t, err, os.ErrNotExist, "expired object %q", name)
	}
	for _, name := range []string{"unrelated.txt", freshName, directoryName} {
		_, err := os.Stat(filepath.Join(dir, name))
		require.NoError(t, err, "retained entry %q", name)
	}
}

func TestLocalImageTaskCheckpointSurvivesStoreReconstructionAndChecksOwner(t *testing.T) {
	dir := t.TempDir()
	storage := NewLocalImageStorage(dir)
	data := []byte("saved generation result")
	imageURL, err := storage.Save(context.Background(), localImageTestTaskID+"-0.png", "image/png", data)
	require.NoError(t, err)
	result, err := json.Marshal(map[string]any{"data": []map[string]string{{"url": imageURL}}})
	require.NoError(t, err)
	now := time.Now()
	task := &ImageTaskRecord{
		ID: localImageTestTaskID, UserID: 7, APIKeyID: 9, Status: ImageTaskStatusCompleted,
		Result: result, CreatedAt: now.Unix(), ExpiresAt: now.Add(24 * time.Hour).Unix(),
	}
	require.NoError(t, storage.SaveResult(task))
	restarted := NewLocalImageStorage(dir)
	loaded, err := restarted.LoadResult(task.ID)
	require.NoError(t, err)
	require.Equal(t, task.ID, loaded.ID)
	require.JSONEq(t, string(result), string(loaded.Result))

	// A local completed checkpoint must remain usable during a Redis outage.
	svc := NewImageTaskService(&imageTaskMemoryStore{getErr: errors.New("Redis unavailable")})
	svc.SetLocalStorage(restarted)
	require.Equal(t, 24*time.Hour, svc.ttl)
	f, err := svc.OpenLocalImage(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 9}, task.ID, "0.png")
	require.NoError(t, err)
	actual, readErr := io.ReadAll(f)
	require.NoError(t, f.Close())
	require.NoError(t, readErr)
	require.Equal(t, data, actual)
	for _, owner := range []ImageTaskOwner{{UserID: 8, APIKeyID: 9}, {UserID: 7, APIKeyID: 10}} {
		f, err := svc.OpenLocalImage(context.Background(), owner, task.ID, "0.png")
		require.Nil(t, f)
		require.ErrorIs(t, err, ErrImageTaskNotFound)
	}
	for _, id := range []string{"../" + task.ID, "imgtask_ffffffffffffffffffffffffffffffff"} {
		_, err := restarted.LoadResult(id)
		require.ErrorIs(t, err, ErrImageTaskNotFound)
	}
}

func TestLocalImageTaskCheckpointRejectsExpiredOrMismatchedRecords(t *testing.T) {
	storage := NewLocalImageStorage(t.TempDir())
	task := &ImageTaskRecord{ID: localImageTestTaskID, Status: ImageTaskStatusCompleted, ExpiresAt: time.Now().Add(-time.Hour).Unix()}
	require.NoError(t, storage.SaveResult(task))
	_, err := storage.LoadResult(task.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
	data, err := json.Marshal(&ImageTaskRecord{ID: "imgtask_ffffffffffffffffffffffffffffffff", Status: ImageTaskStatusCompleted, ExpiresAt: time.Now().Add(time.Hour).Unix()})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(storage.dir, task.ID+".json"), data, 0600))
	_, err = storage.LoadResult(task.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)
}

type localImageSettingsErrorRepo struct {
	*stubSettingRepo
	err error
}

func (r localImageSettingsErrorRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.stubSettingRepo.GetValue(ctx, key)
}

func TestImageStorageLocalFallbackHonorsExplicitSettingsAndReadErrors(t *testing.T) {
	local := NewImageResultUploader(NewLocalImageStorage(t.TempDir()), "", 0, nil)
	for _, test := range []struct {
		name    string
		raw     string
		err     error
		enabled bool
	}{
		{name: "never configured", enabled: true},
		{name: "setting not found", err: ErrSettingNotFound, enabled: true},
		{name: "administrator disabled", raw: `{"enabled":false}`},
		{name: "configured but incomplete", raw: `{"enabled":true}`},
		{name: "invalid stored settings", raw: `invalid JSON`},
		{name: "database unavailable", err: errors.New("database unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newStubSettingRepo()
			require.NoError(t, repo.Set(context.Background(), settingKeyImageStorageConfig, test.raw))
			settings := NewImageStorageSettingService(localImageSettingsErrorRepo{stubSettingRepo: repo, err: test.err}, nil, nil, nil, config.ImageStorageConfig{})
			uploader, enabled := settings.ResolverWithLocalFallback(local)()
			require.Equal(t, test.enabled, enabled)
			if enabled {
				require.Same(t, local, uploader)
			} else {
				require.Nil(t, uploader)
			}
		})
	}
}
