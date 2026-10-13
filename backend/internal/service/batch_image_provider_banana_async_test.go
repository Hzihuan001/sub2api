//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type bananaAsyncTestRepo struct {
	mu       sync.Mutex
	items    []*BatchImageItem
	saves    int
	failSave int
	progress map[string]string
}

func (r *bananaAsyncTestRepo) ListBatchImageItems(context.Context, string, BatchImageItemFilter) ([]*BatchImageItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*BatchImageItem, len(r.items))
	for i, item := range r.items {
		copy := *item
		copy.InputPayload = append([]byte(nil), item.InputPayload...)
		items[i] = &copy
	}
	return items, nil
}

func (r *bananaAsyncTestRepo) CompareAndSetBatchImageAsyncState(_ context.Context, _, customID string, previous, next json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
	if r.failSave == r.saves {
		return errors.New("injected state persistence failure")
	}
	for _, item := range r.items {
		if item.CustomID != customID {
			continue
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(item.InputPayload, &raw); err != nil {
			return err
		}
		var oldValue, expectedValue any
		_ = json.Unmarshal(raw["managed_async"], &oldValue)
		_ = json.Unmarshal(previous, &expectedValue)
		if !reflect.DeepEqual(oldValue, expectedValue) || (oldValue == nil && string(raw["managed_async_cancel_requested"]) == "true") {
			return ErrBatchImageIndexStateConflict
		}
		raw["managed_async"] = next
		item.InputPayload, _ = json.Marshal(raw)
		return nil
	}
	return ErrBatchImageIndexStateConflict
}

func (r *bananaAsyncTestRepo) RequestBatchImageAsyncCancel(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(item.InputPayload, &raw); err != nil {
			return err
		}
		raw["managed_async_cancel_requested"] = json.RawMessage("true")
		item.InputPayload, _ = json.Marshal(raw)
	}
	return nil
}

func (r *bananaAsyncTestRepo) UpdateBatchImageItemProgress(_ context.Context, _, customID, status, _, _, _, _ string, _ int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.progress == nil {
		r.progress = make(map[string]string)
	}
	r.progress[customID] = status
	return nil
}

func (*bananaAsyncTestRepo) RefreshBatchImageJobCounts(context.Context, string) error { return nil }

type bananaAsyncTestUpstream struct {
	handle func(*http.Request) (*http.Response, error)
}

func (u bananaAsyncTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.handle(req)
}

func (u bananaAsyncTestUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func bananaAsyncTestResponse(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

const bananaAsyncSuccessResponse = `{"taskID":"task-1","status":"success","resultImages":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Y9Zl1sAAAAASUVORK5CYII="}]}`

func newBananaAsyncFixture(t *testing.T, count int, handler func(*http.Request) (*http.Response, error)) (*appManagedBatchImageProvider, *bananaAsyncTestRepo, *BatchImageJob, *Account, *time.Time) {
	t.Helper()
	repo := &bananaAsyncTestRepo{}
	for i := 0; i < count; i++ {
		customID := fmt.Sprintf("image-%d", i)
		payload, err := json.Marshal(managedBatchImageItemPayload{Item: BatchImageSubmitItem{CustomID: customID, Prompt: "test image", OutputCount: 1}, ImageSize: "2K", AspectRatio: "1:1"})
		require.NoError(t, err)
		repo.items = append(repo.items, &BatchImageItem{CustomID: customID, InputPayload: payload})
	}
	now := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{
		ItemRepo: repo, ResultDir: t.TempDir(), Concurrency: 1, HTTPUpstream: bananaAsyncTestUpstream{handle: handler},
	}).(*appManagedBatchImageProvider)
	provider.bananaNow = func() time.Time { return now }
	job := &BatchImageJob{BatchID: "imgbatch_banana", Model: "gemini-3.1-flash-image", ItemCount: count}
	account := &Account{ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"api_key": "test-secret", "base_url": "https://api.bananarouter.com",
	}}
	return provider, repo, job, account, &now
}

func TestBananaRouterCapabilityIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		name, base, capability string
		want                   bool
	}{
		{"official provider", "https://api.bananarouter.com/v1beta", "", true},
		{"lookalike", "https://api.bananarouter.com.evil.invalid", "", false},
		{"custom Gemini", "https://gemini.example.com", "", false},
		{"Google", "https://generativelanguage.googleapis.com", "", false},
		{"explicit compatible", "https://gateway.example.com", bananaRouterAsyncProtocol, true},
		{"explicit native", "https://api.bananarouter.com", "gemini_native", false},
		{"invalid URL", "not-a-url", bananaRouterAsyncProtocol, false},
		{"invalid scheme", "file://api.bananarouter.com", bananaRouterAsyncProtocol, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: PlatformGemini, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test", "base_url": tc.base}, Extra: map[string]any{"batch_image_protocol": tc.capability}}
			require.Equal(t, tc.want, isBananaRouterGeminiAccount(account))
		})
	}
}

func TestBananaAsyncPersistsTaskAndRestartsWithoutResubmit(t *testing.T) {
	posts, gets := 0, 0
	provider, repo, job, account, now := newBananaAsyncFixture(t, 1, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			posts++
			body, _ := io.ReadAll(req.Body)
			require.Contains(t, string(body), `"imageSize":"2K"`)
			require.Contains(t, req.URL.Path, ":asyncGenerateContent")
			require.NotEmpty(t, req.Header.Get("Idempotency-Key"))
			return bananaAsyncTestResponse(202, `{"taskID":"task-1","status":"queued"}`)
		}
		gets++
		require.Equal(t, "/v1/async-tasks/task-1", req.URL.Path)
		return bananaAsyncTestResponse(200, bananaAsyncSuccessResponse)
	})
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateRunning, status.InternalState)
	in, err := decodeBananaRouterItem(repo.items[0])
	require.NoError(t, err)
	require.Equal(t, "task-1", in.state.TaskID)
	require.Equal(t, "2K", in.payload.ImageSize)
	require.NotContains(t, string(repo.items[0].InputPayload), "test-secret")
	// A different result directory also proves recovery is database-backed.
	provider.resultDir = t.TempDir()
	*now = now.Add(6 * time.Second)
	status, err = provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, gets)
	require.Equal(t, BatchImageItemStatusSuccess, repo.progress["image-0"])
	_, err = provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, 1, gets)
}

func TestBananaAsyncUncertainSubmissionReusesOriginalIdempotency(t *testing.T) {
	var keys []string
	provider, repo, job, account, now := newBananaAsyncFixture(t, 1, func(req *http.Request) (*http.Response, error) {
		keys = append(keys, req.Header.Get("Idempotency-Key"))
		if len(keys) == 1 {
			return nil, errors.New("response lost after accepting task")
		}
		return bananaAsyncTestResponse(202, `{"taskID":"task-1","status":"queued"}`)
	})
	_, err := provider.Get(context.Background(), job, account)
	require.Error(t, err)
	started, err := decodeBananaRouterItem(repo.items[0])
	require.NoError(t, err)
	require.Empty(t, started.state.TaskID)
	*now = now.Add(6 * time.Second)
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateRunning, status.InternalState)
	require.Len(t, keys, 2)
	require.Equal(t, keys[0], keys[1])
	recovered, err := decodeBananaRouterItem(repo.items[0])
	require.NoError(t, err)
	require.Equal(t, started.state.StartedAt, recovered.state.StartedAt)
	require.Equal(t, "task-1", recovered.state.TaskID)
}

func TestBananaAsyncPersistenceFailureNeverSilentlyAdvances(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			posts := 0
			var keys []string
			provider, repo, job, account, now := newBananaAsyncFixture(t, 1, func(req *http.Request) (*http.Response, error) {
				posts++
				keys = append(keys, req.Header.Get("Idempotency-Key"))
				return bananaAsyncTestResponse(202, `{"taskID":"task-1","status":"queued"}`)
			})
			repo.failSave = failAt
			_, err := provider.Get(context.Background(), job, account)
			require.ErrorContains(t, err, "persistence failure")
			require.Equal(t, failAt-1, posts)
			*now = now.Add(6 * time.Second)
			_, err = provider.Get(context.Background(), job, account)
			require.NoError(t, err)
			if failAt == 2 {
				require.Equal(t, keys[0], keys[1])
			}
		})
	}
}

func TestBananaAsyncTemporaryPollFailureKeepsTask(t *testing.T) {
	for _, code := range []int{429, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			posts, gets := 0, 0
			provider, repo, job, account, now := newBananaAsyncFixture(t, 1, func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost {
					posts++
					return bananaAsyncTestResponse(202, `{"taskID":"task-1","status":"queued"}`)
				}
				gets++
				if gets == 1 {
					return bananaAsyncTestResponse(code, `{"error":"temporary"}`)
				}
				return bananaAsyncTestResponse(200, bananaAsyncSuccessResponse)
			})
			_, err := provider.Get(context.Background(), job, account)
			require.NoError(t, err)
			*now = now.Add(6 * time.Second)
			_, err = provider.Get(context.Background(), job, account)
			require.Error(t, err)
			require.Empty(t, repo.progress)
			*now = now.Add(6 * time.Second)
			status, err := provider.Get(context.Background(), job, account)
			require.NoError(t, err)
			require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
			require.Equal(t, 1, posts)
		})
	}
}

func TestBananaAsyncCancellationDrainsSubmittedAndIndexesSuccess(t *testing.T) {
	posts, gets := 0, 0
	provider, repo, job, account, now := newBananaAsyncFixture(t, 3, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost {
			posts++
			return bananaAsyncTestResponse(202, `{"taskID":"task-1","status":"queued"}`)
		}
		gets++
		return bananaAsyncTestResponse(200, bananaAsyncSuccessResponse)
	})
	_, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, 1, posts)
	require.NoError(t, provider.Cancel(context.Background(), job, account))
	provider.resultDir = t.TempDir()
	*now = now.Add(6 * time.Second)
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	// Succeeded here means the result file is ready for normal indexing and
	// settlement, NOT immediate cancelled/hold release in the processor.
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, gets)
	require.Equal(t, BatchImageItemStatusSuccess, repo.progress["image-0"])
	require.Equal(t, BatchImageItemStatusFailed, repo.progress["image-1"])
	require.Equal(t, BatchImageItemStatusFailed, repo.progress["image-2"])
	result, err := os.ReadFile(status.ProviderOutputRef)
	require.NoError(t, err)
	require.Len(t, strings.Split(strings.TrimSpace(string(result)), "\n"), 3)
	require.Contains(t, string(result), "BATCH_ITEM_CANCELLED")
	// Cancelling again must not destroy the indexed output.
	require.NoError(t, provider.Cancel(context.Background(), job, account))
	require.FileExists(t, status.ProviderOutputRef)

	indexRepo := newFakeBatchImageRepository()
	indexJob := testSettlingBatchImageJob(job.BatchID)
	indexJob.Provider = BatchImageProviderAppManaged
	indexJob.Model = job.Model
	indexJob.AccountID = &account.ID
	indexJob.Status = BatchImageJobStatusRunning
	indexJob.ProviderOutputRef = nil
	indexRepo.jobs[job.BatchID] = indexJob
	billing := &fakeBatchImageBillingRepo{}
	processor := &BatchImageProviderProcessor{
		Repo: indexRepo, ProviderRegistry: NewBatchImageProviderRegistry(provider),
		AccountResolver: &fakeBatchImageAccountResolver{account: account}, BillingRepo: billing,
	}
	processed, err := processor.Process(context.Background(), job.BatchID)
	require.NoError(t, err)
	require.False(t, processed.Terminal)
	require.Equal(t, BatchImageJobStatusSettling, indexJob.Status)
	require.Equal(t, 1, indexJob.SuccessCount)
	require.Equal(t, 2, indexJob.FailCount)
	require.Empty(t, billing.releases)
	settlement := &BatchImageSettlementService{
		Repo: indexRepo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25},
	}
	settled, err := settlement.Settle(context.Background(), job.BatchID)
	require.NoError(t, err)
	require.Equal(t, 0.25, settled.ActualCost)
	require.Len(t, billing.captures, 1)
	require.Empty(t, billing.releases)
}

func TestBananaAsyncCannotResubmitAfterIdempotencyWindowOrCredentialChange(t *testing.T) {
	for _, mode := range []string{"expired", "credential", "base-url"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			provider, _, job, account, now := newBananaAsyncFixture(t, 1, func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("response lost")
			})
			_, err := provider.Get(context.Background(), job, account)
			require.Error(t, err)
			*now = now.Add(6 * time.Second)
			switch mode {
			case "expired":
				*now = now.Add(24 * time.Hour)
			case "credential":
				account.Credentials["api_key"] = "changed-key"
			case "base-url":
				account.Credentials["base_url"] = "https://other.invalid"
				account.Extra = map[string]any{"batch_image_protocol": bananaRouterAsyncProtocol}
			}
			_, err = provider.Get(context.Background(), job, account)
			require.Error(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestBananaAsyncDownloadFailureRetriesSavedResult(t *testing.T) {
	downloads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads++
		if downloads == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("image-bytes"))
	}))
	defer server.Close()
	calls := 0
	provider, _, job, account, now := newBananaAsyncFixture(t, 1, func(*http.Request) (*http.Response, error) {
		calls++
		return bananaAsyncTestResponse(200, fmt.Sprintf(`{"taskID":"task-1","status":"success","resultImages":[{"url":%q}]}`, server.URL))
	})
	_, err := provider.Get(context.Background(), job, account)
	require.ErrorContains(t, err, "download incomplete")
	*now = now.Add(6 * time.Second)
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
	require.Equal(t, 1, calls)
	require.Equal(t, 2, downloads)
}

func TestBananaAsyncHonorsPollIntervalAndFailureMessage(t *testing.T) {
	calls := 0
	provider, _, job, account, now := newBananaAsyncFixture(t, 1, func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return bananaAsyncTestResponse(202, `{"taskID":"task-1","status":"queued"}`)
		}
		return bananaAsyncTestResponse(200, `{"taskID":"task-1","status":"failed","statusMessage":"upstream rejected image"}`)
	})
	_, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	_, err = provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	*now = now.Add(6 * time.Second)
	status, err := provider.Get(context.Background(), job, account)
	require.NoError(t, err)
	result, err := os.ReadFile(status.ProviderOutputRef)
	require.NoError(t, err)
	require.Contains(t, string(result), "upstream rejected image")
}
