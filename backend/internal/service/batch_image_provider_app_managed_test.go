//go:build unit

package service

import (
	"context"
	"encoding/base64"
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
	responseBody    string
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
	responseBody := u.responseBody
	if responseBody == "" {
		responseBody = `{"data":[{"b64_json":"aGVsbG8="}]}`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(
		strings.NewReader(responseBody)), Request: req}, nil
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

func TestManagedImageSizeSupportsTiersAndCustomDimensions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "1K", input: "1K", want: "1024x1024"},
		{name: "2K", input: "2K", want: "2048x2048"},
		{name: "4K", input: "4K", want: "3840x2160"},
		{name: "custom", input: "2160X3840", want: "2160x3840"},
		{name: "legacy fallback", input: "", want: "1024x1024"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, managedImageSize(tt.input))
		})
	}
}

func TestDecodeManagedImageDataDetectsImageMimeType(t *testing.T) {
	tests := []struct {
		name       string
		bytes      []byte
		dataURL    bool
		wantMime   string
		wantBase64 string
	}{
		{
			name:       "jpeg plain base64",
			bytes:      []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01},
			wantMime:   "image/jpeg",
			wantBase64: "/9j/4AAQSkZJRgAB",
		},
		{
			name:       "png data URL",
			bytes:      []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'},
			dataURL:    true,
			wantMime:   "image/png",
			wantBase64: "iVBORw0KGgo=",
		},
		{
			name:       "webp plain base64",
			bytes:      []byte{'R', 'I', 'F', 'F', 0x08, 0x00, 0x00, 0x00, 'W', 'E', 'B', 'P', 'V', 'P', '8', ' '},
			wantMime:   "image/webp",
			wantBase64: "UklGRggAAABXRUJQVlA4IA==",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := base64.StdEncoding.EncodeToString(tt.bytes)
			if tt.dataURL {
				input = "data:image/jpeg;base64," + input
			}
			got, mime, ok := decodeManagedImageData(input, "image/png")
			require.True(t, ok)
			require.Equal(t, tt.wantMime, mime)
			require.Equal(t, tt.wantBase64, got)
		})
	}
}

func TestAppManagedBatchImageProviderDetectsB64ImageMimeType(t *testing.T) {
	dir := t.TempDir()
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01}
	upstream := &appManagedTestUpstream{responseBody: `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(jpeg) + `"}]}`}
	jobID := "imgbatch_app_managed_jpeg"
	repo := &appManagedTestRepo{items: map[string][]*BatchImageItem{jobID: {{CustomID: "one", InputPayload: []byte(`{"custom_id":"one","prompt":"draw a cat"}`)}}}}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{HTTPUpstream: upstream, ItemRepo: repo, ResultDir: dir})
	account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://upstream.invalid"}}
	job := &BatchImageJob{BatchID: jobID, Model: "gpt-image-1", ItemCount: 1}
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
	result, err := os.ReadFile(filepath.Join(dir, jobID+".jsonl"))
	require.NoError(t, err)
	var line struct {
		Response struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						InlineData struct {
							MimeType string `json:"mimeType"`
						} `json:"inlineData"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		} `json:"response"`
	}
	require.NoError(t, json.Unmarshal(result, &line))
	require.Equal(t, "image/jpeg", line.Response.Candidates[0].Content.Parts[0].InlineData.MimeType)
}

func TestBananaRouterHelpersRecognizeCustomGeminiGateway(t *testing.T) {
	banana := &Account{Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "base_url": "https://api.bananarouter.com",
	}}
	official := &Account{Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-key", "base_url": "https://generativelanguage.googleapis.com",
	}}
	require.True(t, isBananaRouterGeminiAccount(banana))
	require.False(t, isBananaRouterGeminiAccount(official))
	require.Equal(t, "https://api.bananarouter.com/v1beta/models/demo:asyncGenerateContent", buildBananaRouterEndpointURL("https://api.bananarouter.com", "/v1beta/models/demo:asyncGenerateContent"))
	require.Equal(t, "https://api.bananarouter.com/v1beta/models/demo:asyncGenerateContent", buildBananaRouterEndpointURL("https://api.bananarouter.com/v1beta", "/v1beta/models/demo:asyncGenerateContent"))
	require.Equal(t, "https://api.bananarouter.com/v1/async-tasks/task-123", buildBananaRouterEndpointURL("https://api.bananarouter.com/v1beta", "/v1/async-tasks/task-123"))
	require.Equal(t, "4K", bananaImageSize("4k"))
}

func TestBananaRouterAsyncResponseParsesTaskAndImages(t *testing.T) {
	var response bananaRouterAsyncResponse
	require.NoError(t, json.Unmarshal([]byte(`{"taskID":"task-123","status":"completed","resultImages":[{"url":"https://cdn.example/image.jpg","mimeType":"image/jpeg"}]}`), &response))
	require.Equal(t, "task-123", response.TaskID)
	require.Equal(t, "completed", response.Status)
	require.Len(t, bananaRouterResponseImages(response.Raw), 1)
}
