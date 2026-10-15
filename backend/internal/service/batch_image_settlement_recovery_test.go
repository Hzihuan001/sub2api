//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// The old implementation switched from capture to full release after 5 errors.
// This is a regression boundary, not a runtime limit on settlement recovery.
const batchImageFormerSettlementRetryLimit = 5

func TestBatchImageSettlementRecoveryPreservesSuccessfulHoldPastRetryLimit(t *testing.T) {
	ctx := context.Background()
	repo := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("imgbatch_success_retry_recovery")
	repo.jobs[job.BatchID] = job
	billing := &fakeBatchImageBillingRepo{captureErr: errors.New("temporary billing outage")}
	usage := &batchSettlementRecoveryUsageRepo{}
	svc := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usage}
	pipeline := &BatchImagePipelineProcessor{ProviderProcessor: &BatchImageProviderProcessor{Repo: repo}, SettlementService: svc}

	for i := 0; i < batchImageFormerSettlementRetryLimit+2; i++ {
		result, err := pipeline.Process(ctx, job.BatchID)
		require.NoError(t, err)
		require.False(t, result.Terminal, "temporary settlement failures cannot make generated images free")
		require.Equal(t, BatchImageJobStatusSettling, job.Status)
		require.Empty(t, billing.releases)
	}
	billing.captureErr = nil
	result, err := pipeline.Process(ctx, job.BatchID)
	require.NoError(t, err)
	require.True(t, result.Terminal)
	require.Equal(t, BatchImageJobStatusCompleted, job.Status)
	require.Equal(t, 0.5, *job.ActualCost)
	require.Len(t, billing.seen, 1)
	require.Len(t, usage.rows, 1)
	require.Empty(t, billing.releases)
}

func TestBatchImageSettlementRecoveryWaitsForUsageBeforeCompletion(t *testing.T) {
	for _, successes := range []int{0, 2} {
		t.Run(fmt.Sprint(successes), func(t *testing.T) {
			ctx := context.Background()
			repo := newFakeBatchImageRepository()
			job := testSettlingBatchImageJob("imgbatch_usage_retry_recovery")
			job.SuccessCount, job.FailCount = successes, job.ItemCount-successes
			repo.jobs[job.BatchID] = job
			billing := &fakeBatchImageBillingRepo{}
			usage := &batchSettlementRecoveryUsageRepo{createErr: errors.New("usage storage unavailable")}
			svc := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usage}

			for i := 0; i < batchImageFormerSettlementRetryLimit+2; i++ {
				_, err := svc.Settle(ctx, job.BatchID)
				require.ErrorIs(t, err, ErrBatchImageSettlementBillingFailed)
				require.Equal(t, BatchImageJobStatusSettling, job.Status)
				require.Equal(t, successes, job.SuccessCount)
				require.Equal(t, "SETTLEMENT_USAGE_LOG_FAILED", batchImageDerefString(job.LastErrorCode))
				require.Empty(t, billing.releases)
			}
			require.Len(t, billing.seen, 1, "capture is applied once while usage persistence retries")
			// Even a later error while replaying the already-applied capture
			// must not discard the pending usage write for a zero-cost batch.
			billing.captureErr = errors.New("temporary capture replay failure")
			_, err := svc.Settle(ctx, job.BatchID)
			require.ErrorIs(t, err, ErrBatchImageSettlementBillingFailed)
			require.Equal(t, BatchImageJobStatusSettling, job.Status)
			require.Empty(t, billing.releases)
			billing.captureErr = nil
			usage.createErr = nil
			// Recovery is driven by persisted state, not by the service instance.
			svc = &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usage}
			_, err = svc.Settle(ctx, job.BatchID)
			require.NoError(t, err)
			require.Equal(t, BatchImageJobStatusCompleted, job.Status)
			require.Equal(t, float64(successes)*0.25, *job.ActualCost)
			require.Len(t, usage.rows, 1)
			require.Len(t, billing.seen, 1)
		})
	}
}

func TestBatchImageSettlementRecoveryRetriesCompletionWithoutDuplicateCaptureOrUsage(t *testing.T) {
	ctx := context.Background()
	base := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("imgbatch_mark_settled_retry")
	base.jobs[job.BatchID] = job
	repo := &batchSettlementRecoveryRepo{fakeBatchImageRepository: base, markErr: errors.New("temporary completion write failure")}
	billing := &fakeBatchImageBillingRepo{}
	usage := &batchSettlementRecoveryUsageRepo{}
	svc := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usage}

	_, err := svc.Settle(ctx, job.BatchID)
	require.Error(t, err)
	require.Equal(t, BatchImageJobStatusSettling, job.Status)
	require.Len(t, usage.rows, 1, "usage must already be durable before the completion write")
	repo.markErr = nil
	svc = &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usage}
	_, err = svc.Settle(ctx, job.BatchID)
	require.NoError(t, err)
	require.Equal(t, BatchImageJobStatusCompleted, job.Status)
	require.Len(t, billing.captures, 2)
	require.Len(t, billing.seen, 1)
	require.Equal(t, 2, usage.createCalls)
	require.Len(t, usage.rows, 1)
	require.Empty(t, billing.releases)
	_, err = svc.Settle(ctx, job.BatchID)
	require.NoError(t, err)
	require.Len(t, billing.captures, 2)
	require.Equal(t, 2, usage.createCalls)
}

func TestBatchImageSettlementRecoveryCompletionCommitBeforeLostResponseKeepsUsage(t *testing.T) {
	ctx := context.Background()
	base := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("imgbatch_completion_response_lost")
	base.jobs[job.BatchID] = job
	repo := &batchSettlementRecoveryRepo{fakeBatchImageRepository: base, markAfterCommitErr: errors.New("completion response lost")}
	billing := &fakeBatchImageBillingRepo{}
	usage := &batchSettlementRecoveryUsageRepo{}
	svc := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 0.25}, UsageLogRepo: usage}

	_, err := svc.Settle(ctx, job.BatchID)
	require.Error(t, err)
	require.Equal(t, BatchImageJobStatusCompleted, job.Status)
	require.Len(t, usage.rows, 1)
	result, err := svc.Settle(ctx, job.BatchID)
	require.NoError(t, err)
	require.True(t, result.AlreadySettled)
	require.Len(t, billing.captures, 1)
	require.Equal(t, 1, usage.createCalls)
}

func TestBatchImageSettlementRecoveryDoesNotRewritePreviouslyCompletedJob(t *testing.T) {
	repo := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("imgbatch_historical_completed")
	job.Status = BatchImageJobStatusCompleted
	job.ActualCost = new(float64)
	*job.ActualCost = 0.37
	repo.jobs[job.BatchID] = job
	billing := &fakeBatchImageBillingRepo{}
	usage := &batchSettlementRecoveryUsageRepo{}
	svc := &BatchImageSettlementService{Repo: repo, BillingRepo: billing, Pricing: &fakeBatchImagePricingResolver{unitPrice: 99}, UsageLogRepo: usage}

	result, err := svc.Settle(context.Background(), job.BatchID)
	require.NoError(t, err)
	require.True(t, result.AlreadySettled)
	require.Equal(t, 0.37, result.ActualCost)
	require.Empty(t, billing.captures)
	require.Empty(t, billing.releases)
	require.Zero(t, usage.createCalls)
	require.Empty(t, repo.transitions[job.BatchID])
}

type batchSettlementRecoveryRepo struct {
	*fakeBatchImageRepository
	markErr            error
	markAfterCommitErr error
}

func (r *batchSettlementRecoveryRepo) MarkBatchImageJobSettled(ctx context.Context, params MarkBatchImageJobSettledParams) error {
	if r.markErr != nil {
		return r.markErr
	}
	if err := r.fakeBatchImageRepository.MarkBatchImageJobSettled(ctx, params); err != nil {
		return err
	}
	return r.markAfterCommitErr
}

type batchSettlementRecoveryUsageRepo struct {
	UsageLogRepository
	createErr   error
	createCalls int
	rows        map[string]*UsageLog
}

func (r *batchSettlementRecoveryUsageRepo) Create(_ context.Context, log *UsageLog) (bool, error) {
	r.createCalls++
	if r.createErr != nil {
		return false, r.createErr
	}
	if r.rows == nil {
		r.rows = make(map[string]*UsageLog)
	}
	key := fmt.Sprintf("%d:%s", log.APIKeyID, log.RequestID)
	if _, ok := r.rows[key]; ok {
		return false, nil
	}
	copy := *log
	r.rows[key] = &copy
	return true, nil
}
