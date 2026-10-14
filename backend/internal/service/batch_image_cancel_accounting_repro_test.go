//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

// Regression reproducer only: use real provider/processor/settlement with a
// deterministic mock upstream and the existing repository/billing test doubles.
// The second upstream response is held until cancellation, so the first image
// is durably checkpointed before cancel and the second is a late success.
func TestAppManagedBatchImageCancelAfterPartialSuccessMustSettleCompletedImages(t *testing.T) {
	ctx := context.Background()
	job := testSettlingBatchImageJob("imgbatch_cancel_accounting_repro")
	job.Provider, job.Model, job.Status = BatchImageProviderAppManaged, "gpt-image-1", BatchImageJobStatusRunning
	job.ItemCount, job.SuccessCount, job.FailCount = 2, 0, 0
	job.ProviderOutputRef = nil
	hold := 0.5
	job.HoldAmount = &hold
	account := &Account{ID: *job.AccountID, Type: AccountTypeAPIKey, Platform: PlatformOpenAI,
		Credentials: map[string]any{"api_key": "synthetic-test-key", "base_url": "https://upstream.invalid"}}
	upstream := &cancelAccountingTestUpstream{secondStarted: make(chan struct{}), releaseSecond: make(chan struct{})}
	defer upstream.release()
	itemRepo := &appManagedTestRepo{items: map[string][]*BatchImageItem{job.BatchID: {
		{CustomID: "one", InputPayload: []byte(`{"custom_id":"one","prompt":"first synthetic image"}`)},
		{CustomID: "two", InputPayload: []byte(`{"custom_id":"two","prompt":"second synthetic image"}`)},
	}}}
	provider := NewAppManagedBatchImageProvider(AppManagedBatchImageProviderOptions{
		HTTPUpstream: upstream, ItemRepo: itemRepo, ResultDir: t.TempDir(), Concurrency: 1,
	}).(*appManagedBatchImageProvider)
	repo := newFakeBatchImageRepository()
	repo.jobs[job.BatchID] = job
	billing := &fakeBatchImageBillingRepo{}
	usageLogs := &openAIRecordUsageLogRepoStub{}
	pipeline := &BatchImagePipelineProcessor{
		ProviderProcessor: &BatchImageProviderProcessor{Repo: repo, ProviderRegistry: NewBatchImageProviderRegistry(provider),
			AccountResolver: &fakeBatchImageAccountResolver{account: account}, BillingRepo: billing},
		SettlementService: &BatchImageSettlementService{Repo: repo, BillingRepo: billing,
			Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usageLogs},
	}
	type outcome struct {
		result BatchImageProcessResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := pipeline.Process(ctx, job.BatchID)
		done <- outcome{result: result, err: err}
	}()
	select {
	case <-upstream.secondStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("second request did not start")
	}
	// Concurrency 1 releases its semaphore only after appending the first result.
	checkpoint, err := os.ReadFile(provider.resultPath(job.BatchID) + ".partial")
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(checkpoint)), "\n")
	require.Len(t, lines, 1)
	first, err := ParseBatchImageResultLine([]byte(lines[0]), 0)
	require.NoError(t, err)
	require.Equal(t, BatchImageParsedStatusSucceeded, first.Status)
	require.Equal(t, 1, first.ImageCount)
	require.NoError(t, provider.Cancel(ctx, job, account))
	upstream.release()
	var processed outcome
	select {
	case processed = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("processing did not finish after releasing the late response")
	}
	require.NoError(t, processed.err)
	if !processed.result.Terminal {
		_, err = pipeline.Process(ctx, job.BatchID)
		require.NoError(t, err)
	}
	checkpoint, err = os.ReadFile(provider.resultPath(job.BatchID) + ".partial")
	if err == nil {
		t.Logf("remaining durable result lines: %d", len(strings.Split(strings.TrimSpace(string(checkpoint)), "\n")))
	}
	released := 0.0
	for _, cmd := range billing.releases {
		released += cmd.HoldAmount
	}
	t.Logf("upstream successes=%d, status=%s, capture calls=%d, release calls=%d, released hold=%.2f, usage records=%d",
		upstream.successes.Load(), job.Status, len(billing.captures), len(billing.releases), released, usageLogs.calls)
	require.Len(t, billing.captures, 1, "successful images must be settled even when cancellation arrives before batch completion")
	require.GreaterOrEqual(t, billing.captures[0].ActualAmount, 0.25, "the durably successful pre-cancel image must not become free")
	require.Empty(t, billing.releases, "do not release the entire hold when successful output exists")
	require.Equal(t, 1, usageLogs.calls)
}

type cancelAccountingTestUpstream struct {
	calls         atomic.Int32
	successes     atomic.Int32
	secondStarted chan struct{}
	releaseSecond chan struct{}
	releaseOnce   sync.Once
}

func (u *cancelAccountingTestUpstream) release() { u.releaseOnce.Do(func() { close(u.releaseSecond) }) }

func (u *cancelAccountingTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	call := u.calls.Add(1)
	if call == 2 {
		close(u.secondStarted)
		select {
		case <-u.releaseSecond:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	u.successes.Add(1)
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
		Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"data":[{"b64_json":"aGVsbG8="}],"test_call":%d}`, call)))}, nil
}

func (u *cancelAccountingTestUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}
