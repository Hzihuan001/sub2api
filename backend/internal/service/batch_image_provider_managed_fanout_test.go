//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type managedPagedTestRepo struct{ *appManagedTestRepo }

func (r *managedPagedTestRepo) ListBatchImageItems(ctx context.Context, id string, filter BatchImageItemFilter) ([]*BatchImageItem, error) {
	items, err := r.appManagedTestRepo.ListBatchImageItems(ctx, id, filter)
	if err != nil {
		return nil, err
	}
	// Match the real repository's normalization, including its legacy trap.
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	if filter.Offset >= len(items) {
		return nil, nil
	}
	return items[filter.Offset:min(len(items), filter.Offset+filter.Limit)], nil
}

func TestAppManagedBatchImageItemsAreFullyPaged(t *testing.T) {
	for _, count := range []int{101, 501} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			id := "imgbatch_paged"
			repo := &managedPagedTestRepo{&appManagedTestRepo{items: map[string][]*BatchImageItem{}}}
			for i := 0; i < count; i++ {
				customID := fmt.Sprintf("image-%d", i)
				repo.items[id] = append(repo.items[id], &BatchImageItem{CustomID: customID,
					InputPayload: []byte(fmt.Sprintf(`{"custom_id":%q,"prompt":"test"}`, customID))})
			}
			items, err := listAllManagedBatchItems(context.Background(), repo, id)
			require.NoError(t, err)
			require.Len(t, items, count)
			upstream := &appManagedTestUpstream{}
			provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{
				HTTPUpstream: upstream, ItemRepo: repo, ResultDir: t.TempDir(), Concurrency: 3,
			})
			account := &Account{ID: 7, Type: AccountTypeAPIKey, Platform: PlatformOpenAI,
				Credentials: map[string]any{"api_key": "test", "base_url": "https://upstream.invalid"}}
			status, err := provider.Get(context.Background(), &BatchImageJob{BatchID: id, Model: "gpt-image-1", ItemCount: count}, account)
			require.NoError(t, err)
			require.Equal(t, BatchProviderStateSucceeded, status.InternalState)
			result, err := os.ReadFile(status.ProviderOutputRef)
			require.NoError(t, err)
			require.Len(t, strings.Split(strings.TrimSpace(string(result)), "\n"), count)
			require.Equal(t, count, upstream.calls)
		})
	}
}

func TestAppManagedBatchImageCancelRetainsUnknownOutcomeAndDoesNotResubmit(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response func() (*http.Response, error)
	}{
		{"transport timeout", func() (*http.Response, error) { return nil, context.DeadlineExceeded }},
		{"connection lost", func() (*http.Response, error) { return nil, errors.New("connection reset after request") }},
		{"response interrupted", func() (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(
				io.MultiReader(strings.NewReader(`{"data":[{"b64_json":"`), managedInterruptedBody{})),
			}, nil
		}},
		{"no confirmed image", func() (*http.Response, error) { return bananaAsyncTestResponse(200, `{}`) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			pipeline, provider, job, account, billing := newManagedCancelPipeline(t, func(*http.Request) (*http.Response, error) {
				calls++
				return tt.response()
			})
			got, err := pipeline.Process(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.False(t, got.Terminal)
			require.NoError(t, provider.Cancel(context.Background(), job, account))
			got, err = pipeline.Process(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.False(t, got.Terminal)
			require.Equal(t, 1, calls)
			require.Empty(t, billing.captures)
			require.Empty(t, billing.releases)
			require.Equal(t, BatchImageJobStatusRunning, job.Status)
		})
	}
}

type managedInterruptedBody struct{}

func (managedInterruptedBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAppManagedBatchImageDefiniteFailureHasNoCharge(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response func() (*http.Response, error)
	}{
		{"upstream rejects", func() (*http.Response, error) {
			return bananaAsyncTestResponse(400, `{"error":{"message":"invalid image request"}}`)
		}},
		{"dns failure", func() (*http.Response, error) {
			return nil, &net.DNSError{Name: "upstream.invalid", Err: "host not found", IsNotFound: true}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			pipeline, provider, job, account, billing := newManagedCancelPipeline(t, func(*http.Request) (*http.Response, error) {
				calls++
				return tt.response()
			})
			_, err := pipeline.Process(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.NoError(t, provider.Cancel(context.Background(), job, account))
			_, err = pipeline.Process(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, BatchImageJobStatusCompleted, job.Status)
			require.Len(t, billing.captures, 1)
			require.Zero(t, billing.captures[0].ActualAmount)
			require.Equal(t, 0, job.SuccessCount)
			require.Equal(t, 1, job.FailCount)
		})
	}
}

func TestAppManagedBatchImageCorruptCheckpointCannotRefundAsUnsubmitted(t *testing.T) {
	pipeline, provider, job, account, billing := newManagedCancelPipeline(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("must not call upstream for a corrupted checkpoint")
		return nil, nil
	})
	require.NoError(t, os.WriteFile(provider.resultPath(job.BatchID)+".partial", []byte(`{"key":"image-0","response":`), 0600))
	require.NoError(t, provider.Cancel(context.Background(), job, account))
	got, err := pipeline.Process(context.Background(), job.BatchID)
	require.NoError(t, err)
	require.False(t, got.Terminal)
	require.Empty(t, billing.captures)
	require.Empty(t, billing.releases)
}

func newManagedCancelPipeline(t *testing.T, handle func(*http.Request) (*http.Response, error)) (*BatchImagePipelineProcessor, *appManagedBatchImageProvider, *BatchImageJob, *Account, *fakeBatchImageBillingRepo) {
	t.Helper()
	job := testSettlingBatchImageJob("imgbatch_cancel_state")
	job.Provider, job.Model, job.Status = BatchImageProviderAppManaged, "gpt-image-1", BatchImageJobStatusRunning
	job.ItemCount, job.SuccessCount, job.FailCount = 1, 0, 0
	job.ProviderOutputRef = nil
	hold := 0.25
	job.HoldAmount = &hold
	account := &Account{ID: *job.AccountID, Type: AccountTypeAPIKey, Platform: PlatformOpenAI,
		Credentials: map[string]any{"api_key": "synthetic-test-key", "base_url": "https://upstream.invalid"}}
	itemRepo := &appManagedTestRepo{items: map[string][]*BatchImageItem{job.BatchID: {
		{CustomID: "one", InputPayload: []byte(`{"custom_id":"one","prompt":"synthetic image"}`)},
	}}}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{
		HTTPUpstream: bananaAsyncTestUpstream{handle: handle}, ItemRepo: itemRepo, ResultDir: t.TempDir(), Concurrency: 1,
	}).(*appManagedBatchImageProvider)
	repo := newFakeBatchImageRepository()
	repo.jobs[job.BatchID] = job
	billing := &fakeBatchImageBillingRepo{}
	pipeline := &BatchImagePipelineProcessor{
		ProviderProcessor: &BatchImageProviderProcessor{Repo: repo, ProviderRegistry: NewBatchImageProviderRegistry(provider),
			AccountResolver: &fakeBatchImageAccountResolver{account: account}, BillingRepo: billing},
		SettlementService: &BatchImageSettlementService{Repo: repo, BillingRepo: billing,
			Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}},
	}
	return pipeline, provider, job, account, billing
}
