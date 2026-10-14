//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImageSettlement_NoDiscountSnapshotV2(t *testing.T) {
	for _, tc := range []struct {
		name          string
		groupRate     float64
		accountRate   float64
		wantActual    float64
		wantUsageRate float64
	}{
		{"two_successful_images_full_price", 1, 1, 0.5, 1},
		{"normal_multipliers_retained", 2, 1.25, 1.25, 2.5},
		{"normal_free_group_retained", 0, 1.25, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeBatchImageRepository()
			job := testSettlingBatchImageJob("imgbatch_v2_" + tc.name)
			job.PricingSnapshotVersion = 2
			job.BaseUnitPrice = 0.25
			job.GroupRateMultiplier = tc.groupRate
			job.AccountRateMultiplier = tc.accountRate
			job.BatchDiscountMultiplier = 1
			job.BillableUnitPrice = 0.25 * tc.groupRate * tc.accountRate
			// The fixture has three requested images: two succeeded, one failed.
			repo.jobs[job.BatchID] = job
			billing := &fakeBatchImageBillingRepo{}
			logs := &openAIRecordUsageLogRepoStub{}
			svc := &BatchImageSettlementService{
				Repo: repo, BillingRepo: billing, UsageLogRepo: logs,
				Pricing: &fakeBatchImagePricingResolver{unitPrice: 99},
			}
			result, err := svc.Settle(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.InDelta(t, tc.wantActual, result.ActualCost, 1e-12)
			require.Len(t, billing.captures, 1)
			require.InDelta(t, tc.wantActual, billing.captures[0].ActualAmount, 1e-12)
			require.Equal(t, 2, logs.lastLog.ImageCount)
			require.InDelta(t, 0.5, logs.lastLog.TotalCost, 1e-12)
			require.InDelta(t, 0.5, logs.lastLog.ImageOutputCost, 1e-12)
			require.InDelta(t, tc.wantActual, logs.lastLog.ActualCost, 1e-12)
			require.InDelta(t, tc.wantUsageRate, logs.lastLog.RateMultiplier, 1e-12)
			require.InDelta(t, logs.lastLog.ActualCost, logs.lastLog.TotalCost*logs.lastLog.RateMultiplier, 1e-12)
			_, err = svc.Settle(context.Background(), job.BatchID)
			require.NoError(t, err)
			require.Len(t, billing.captures, 1, "retry must not charge again")
		})
	}
}

func TestBatchImageSettlement_LegacyDiscountSnapshotNotRepriced(t *testing.T) {
	repo := newFakeBatchImageRepository()
	job := testSettlingBatchImageJob("imgbatch_legacy_discount")
	job.PricingSnapshotVersion = 1
	job.BaseUnitPrice = 0.25
	job.GroupRateMultiplier = 1
	job.AccountRateMultiplier = 1
	job.BatchDiscountMultiplier = 0.5
	job.BillableUnitPrice = 0.125
	repo.jobs[job.BatchID] = job
	billing := &fakeBatchImageBillingRepo{}
	logs := &openAIRecordUsageLogRepoStub{}
	svc := &BatchImageSettlementService{
		Repo: repo, BillingRepo: billing, UsageLogRepo: logs,
		Pricing: &fakeBatchImagePricingResolver{unitPrice: 99},
	}
	_, err := svc.Settle(context.Background(), job.BatchID)
	require.NoError(t, err)
	require.InDelta(t, 0.25, billing.captures[0].ActualAmount, 1e-12)
	require.InDelta(t, 0.25, logs.lastLog.ActualCost, 1e-12)
	require.InDelta(t, 0.25, logs.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.5, logs.lastLog.RateMultiplier, 1e-12)
}
