//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestScheduledTestPlanClaimDueLeaseAndFinalize(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: fmt.Sprintf("scheduled-lease-%d", suffix),
		Platform: service.PlatformOpenAI,
		Type: service.AccountTypeAPIKey,
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduled_test_plans WHERE account_id = $1", account.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM accounts WHERE id = $1", account.ID)
	})

	now := time.Now().UTC()
	var planID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_plans (account_id, model_id, test_kind, cron_expression, enabled, max_results, next_run_at)
		VALUES ($1, 'test-model', 'pelican', '*/5 * * * *', true, 5, $2)
		RETURNING id
	`, account.ID, now.Add(-time.Minute)).Scan(&planID))

	repo := &scheduledTestPlanRepository{db: integrationDB}
	leaseUntil := now.Add(10 * time.Minute)
	first, err := repo.ClaimDue(ctx, now, leaseUntil, 10, "worker-a")
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, planID, first[0].ID)
	require.Equal(t, "worker-a", first[0].LeaseToken)

	second, err := repo.ClaimDue(ctx, now, leaseUntil, 10, "worker-b")
	require.NoError(t, err)
	require.Empty(t, second, "a leased plan must not be claimed by a second worker")

	err = repo.FinalizeRun(ctx, planID, "wrong-worker", first[0].ActiveRunID, now, now.Add(5*time.Minute), 1, "failed")
	require.Error(t, err, "stale lease owner must not finalize")
	require.NoError(t, repo.FinalizeRun(ctx, planID, first[0].LeaseToken, first[0].ActiveRunID, now, now.Add(5*time.Minute), 1, "failed"))
	err = repo.FinalizeRun(ctx, planID, first[0].LeaseToken, first[0].ActiveRunID, now, now.Add(5*time.Minute), 1, "failed")
	require.Error(t, err, "a finalized run must not be finalized twice")

	resultRepo := &scheduledTestResultRepository{db: integrationDB}
	firstResult, err := resultRepo.Create(ctx, &service.ScheduledTestResult{
		PlanID: planID, RunID: first[0].ActiveRunID, Status: "failed",
		ErrorMessage: "upstream overload", StartedAt: now, FinishedAt: now,
	})
	require.NoError(t, err)
	secondResult, err := resultRepo.Create(ctx, &service.ScheduledTestResult{
		PlanID: planID, RunID: first[0].ActiveRunID, Status: "failed",
		ErrorMessage: "duplicate", StartedAt: now, FinishedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, firstResult.ID, secondResult.ID, "same run_id must be idempotent")
}
