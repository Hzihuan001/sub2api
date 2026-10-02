//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type appManagedTestRepo struct{ items map[string][]*BatchImageItem }

func (r *appManagedTestRepo) ListBatchImageItems(_ context.Context, id string, _ BatchImageItemFilter) ([]*BatchImageItem, error) {
	return r.items[id], nil
}

type appManagedTestUpstream struct {
	mu              sync.Mutex
	calls           int
	idempotencyKeys []string
	lastURL         string
	lastContentType string
	lastBody        []byte
}

func (u *appManagedTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	u.mu.Lock()
	u.calls++
	u.idempotencyKeys = append(u.idempotencyKeys, req.Header.Get("Idempotency-Key"))
	u.lastURL = req.URL.String()
	u.lastContentType = req.Header.Get("Content-Type")
	u.lastBody = body
	u.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(
		strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="}]}`)), Request: req}, nil
}

func TestAppManagedBatchImageProviderSendsGPTImageReferenceAsEdit(t *testing.T) {
	dir := t.TempDir()
	upstream := &appManagedTestUpstream{}
	jobID := "imgbatch_app_managed_gpt_reference"
	payload, err := json.Marshal(managedBatchImageItemPayload{
		Item: BatchImageSubmitItem{
			CustomID: "reference",
			Prompt:   "make this image warmer",
			ReferenceImages: []BatchImageReferenceInput{{
				ID:       "reference.png",
				MimeType: "image/png",
				Data:     []byte("PNG_BYTES"),
			}},
		},
		ImageSize: "2K",
	})
	require.NoError(t, err)
	repo := &appManagedTestRepo{items: map[string][]*BatchImageItem{
		jobID: {{CustomID: "reference", InputPayload: payload}},
	}}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{
		HTTPUpstream: upstream,
		ItemRepo:     repo,
		ResultDir:    dir,
	})
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Credentials: map[string]any{
		"api_key":  "test-key",
		"base_url": "https://upstream.invalid",
	}}
	job := &BatchImageJob{BatchID: jobID, Model: "gpt-image-1", ItemCount: 1}
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)

	upstream.mu.Lock()
	defer upstream.mu.Unlock()
	require.Equal(t, "https://upstream.invalid/v1/images/edits", upstream.lastURL)
	require.Contains(t, upstream.lastContentType, "multipart/form-data")
	require.Contains(t, string(upstream.lastBody), "name=\"image[]\"")
	require.Contains(t, string(upstream.lastBody), "PNG_BYTES")
}

func (u *appManagedTestUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestAppManagedBatchImageProviderFanoutAndIdempotentCompletion(t *testing.T) {
	dir := t.TempDir()
	upstream := &appManagedTestUpstream{}
	jobID := "imgbatch_app_managed_test"
	payload := []byte(`{"custom_id":"one","prompt":"draw a cat"}`)
	repo := &appManagedTestRepo{items: map[string][]*BatchImageItem{jobID: {{CustomID: "one", InputPayload: payload}}}}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{HTTPUpstream: upstream, ItemRepo: repo, ResultDir: dir, Concurrency: 2})
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://upstream.invalid"}}
	job := &BatchImageJob{BatchID: jobID, Model: "gpt-image-1", ItemCount: 1}
	created, err := provider.Submit(context.Background(), job, account, BatchImageInput{})
	require.NoError(t, err)
	require.Equal(t, "managed:"+jobID, created.ProviderJobName)
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
	require.FileExists(t, filepath.Join(dir, jobID+".jsonl"))
	// A retry after the worker has acknowledged the result must not call the
	// upstream again and therefore cannot double-charge the user.
	_, err = provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	upstream.mu.Lock()
	require.Equal(t, 1, upstream.calls)
	require.Len(t, upstream.idempotencyKeys, 1)
	require.NotEmpty(t, upstream.idempotencyKeys[0])
	upstream.mu.Unlock()
	require.NoError(t, provider.Cleanup(context.Background(), job, account, CleanupTargetOutput))
	_, err = os.Stat(filepath.Join(dir, jobID+".jsonl"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAppManagedBatchImageProviderDoesNotSilentlyDropFileReference(t *testing.T) {
	dir := t.TempDir()
	upstream := &appManagedTestUpstream{}
	jobID := "imgbatch_app_managed_reference"
	payload, err := json.Marshal(managedBatchImageItemPayload{
		Item: BatchImageSubmitItem{
			CustomID: "reference",
			Prompt:   "edit this image",
			ReferenceImages: []BatchImageReferenceInput{{
				MimeType: "image/png",
				FileURI:  "gs://bucket/reference.png",
			}},
		},
		ImageSize: "1K",
	})
	require.NoError(t, err)
	repo := &appManagedTestRepo{items: map[string][]*BatchImageItem{
		jobID: {{CustomID: "reference", InputPayload: payload}},
	}}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{
		HTTPUpstream: upstream,
		ItemRepo:     repo,
		ResultDir:    dir,
	})
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Credentials: map[string]any{
		"api_key":  "test-key",
		"base_url": "https://upstream.invalid",
	}}
	job := &BatchImageJob{BatchID: jobID, Model: "gpt-image-1", ItemCount: 1}
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
	require.Equal(t, 0, upstream.calls)
	result, err := os.ReadFile(filepath.Join(dir, jobID+".jsonl"))
	require.NoError(t, err)
	require.Contains(t, string(result), "REFERENCE_UNAVAILABLE")
}
