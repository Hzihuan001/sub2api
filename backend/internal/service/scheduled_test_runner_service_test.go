package service

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

type scheduledRunnerPlanRepoStub struct {
	updated bool
	lastRun time.Time
	nextRun time.Time
}

func (r *scheduledRunnerPlanRepoStub) Create(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	return nil, errors.New("not implemented")
}
func (r *scheduledRunnerPlanRepoStub) GetByID(context.Context, int64) (*ScheduledTestPlan, error) {
	return nil, errors.New("not implemented")
}
func (r *scheduledRunnerPlanRepoStub) ListByAccountID(context.Context, int64) ([]*ScheduledTestPlan, error) {
	return nil, errors.New("not implemented")
}
func (r *scheduledRunnerPlanRepoStub) ListDue(context.Context, time.Time) ([]*ScheduledTestPlan, error) {
	return nil, errors.New("not implemented")
}
func (r *scheduledRunnerPlanRepoStub) Update(context.Context, *ScheduledTestPlan) (*ScheduledTestPlan, error) {
	return nil, errors.New("not implemented")
}
func (r *scheduledRunnerPlanRepoStub) Delete(context.Context, int64) error { return nil }
func (r *scheduledRunnerPlanRepoStub) UpdateAfterRun(_ context.Context, _ int64, lastRunAt, nextRunAt time.Time) error {
	r.updated = true
	r.lastRun = lastRunAt
	r.nextRun = nextRunAt
	return nil
}

type scheduledRunnerResultRepoStub struct {
	results        []*ScheduledTestResult
	createErr      error
	createDeadline time.Time
}

func (r *scheduledRunnerResultRepoStub) Create(ctx context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	r.createDeadline, _ = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.createErr != nil {
		return nil, r.createErr
	}
	r.results = append(r.results, result)
	return result, nil
}
func (r *scheduledRunnerResultRepoStub) ListByPlanID(context.Context, int64, int) ([]*ScheduledTestResult, error) {
	return r.results, nil
}
func (r *scheduledRunnerResultRepoStub) PruneOldResults(ctx context.Context, _ int64, _ int) error {
	return ctx.Err()
}

type scheduledRunnerClaimerStub struct {
	scheduledRunnerPlanRepoStub
	finalize func(context.Context, int64, string, string, time.Time, time.Time, int, string) error
}

func (*scheduledRunnerClaimerStub) ClaimDue(context.Context, time.Time, time.Time, int, string) ([]*ScheduledTestPlan, error) {
	return nil, errors.New("not implemented")
}

func (r *scheduledRunnerClaimerStub) FinalizeRun(ctx context.Context, id int64, leaseToken, runID string, lastRun, nextRun time.Time, failureStreak int, lastError string) error {
	return r.finalize(ctx, id, leaseToken, runID, lastRun, nextRun, failureStreak, lastError)
}

type scheduledRunnerPelicanExecutorFunc func(context.Context) (*ScheduledTestResult, error)

func (f scheduledRunnerPelicanExecutorFunc) Run(ctx context.Context, _, _ int64, _, _, _ string) (*ScheduledTestResult, error) {
	return f(ctx)
}

type scheduledRunnerPelicanExecutorStub struct{}

func (scheduledRunnerPelicanExecutorStub) Run(_ context.Context, _, _ int64, _, _, _ string) (*ScheduledTestResult, error) {
	now := time.Now()
	return &ScheduledTestResult{
		Status: "failed", ErrorMessage: "upstream overload", StartedAt: now,
		FinishedAt: now, LatencyMs: 1,
	}, nil
}

type scheduledRunnerPelicanResponseStub struct{ response string }

func (s scheduledRunnerPelicanResponseStub) Run(context.Context, int64, int64, string, string, string) (*ScheduledTestResult, error) {
	now := time.Now()
	return &ScheduledTestResult{Status: "success", ResponseText: s.response, StartedAt: now, FinishedAt: now}, nil
}

func TestScheduledTestRunnerPersistsPelicanFailureAndAdvancesSchedule(t *testing.T) {
	planRepo := &scheduledRunnerPlanRepoStub{}
	resultRepo := &scheduledRunnerResultRepoStub{}
	runner := &ScheduledTestRunnerService{
		planRepo:        planRepo,
		scheduledSvc:    NewScheduledTestService(planRepo, resultRepo),
		pelicanExecutor: scheduledRunnerPelicanExecutorStub{},
	}
	plan := &ScheduledTestPlan{
		ID: 11, AccountID: 22, GroupID: i64ptr(33), ModelID: "gpt-6.1-sol",
		TestKind: ScheduledTestKindPelican, CronExpression: "* * * * *", Enabled: true,
		MaxResults: 5,
	}

	runner.runOnePlan(context.Background(), plan)

	if len(resultRepo.results) != 1 {
		t.Fatalf("saved results = %d, want 1", len(resultRepo.results))
	}
	if resultRepo.results[0].ErrorMessage != "upstream overload" {
		t.Fatalf("saved error = %q", resultRepo.results[0].ErrorMessage)
	}
	if !planRepo.updated {
		t.Fatal("failed Pelican run did not advance its schedule")
	}
	if !planRepo.nextRun.After(planRepo.lastRun) {
		t.Fatalf("next run %s is not after last run %s", planRepo.nextRun, planRepo.lastRun)
	}
}

func TestScheduledTestRunnerMarksDefaultPelicanToolRefusalAsFailed(t *testing.T) {
	planRepo := &scheduledRunnerPlanRepoStub{}
	resultRepo := &scheduledRunnerResultRepoStub{}
	runner := &ScheduledTestRunnerService{
		planRepo:        planRepo,
		scheduledSvc:    NewScheduledTestService(planRepo, resultRepo),
		pelicanExecutor: scheduledRunnerPelicanResponseStub{response: "I cannot create files in this environment."},
	}
	plan := &ScheduledTestPlan{
		ID: 12, AccountID: 23, GroupID: i64ptr(34), ModelID: "gpt-6-astra",
		TestKind: ScheduledTestKindPelican, TestPrompt: PreviousDefaultPelicanTestPrompt,
		CronExpression: "* * * * *", Enabled: true, MaxResults: 5,
	}

	runner.runOnePlan(context.Background(), plan)

	if len(resultRepo.results) != 1 {
		t.Fatalf("saved results = %d, want 1", len(resultRepo.results))
	}
	result := resultRepo.results[0]
	if result.Status != "failed" || result.ErrorMessage != invalidPelicanArtifactError {
		t.Fatalf("saved result = %#v, want failed artifact validation", result)
	}
	if result.ResponseText != "I cannot create files in this environment." {
		t.Fatalf("response was not retained for diagnosis: %q", result.ResponseText)
	}
}

func TestScheduledTestRunnerFinalizesTimedOutRun(t *testing.T) {
	for _, tc := range []struct {
		name          string
		leaseDuration time.Duration
		createErr     error
		wantFinalized bool
	}{
		{name: "failed result persisted after execution deadline", leaseDuration: 10 * time.Minute, wantFinalized: true},
		{name: "persistence stops at lease expiry", leaseDuration: 5*time.Minute + 5*time.Second, wantFinalized: true},
		{name: "failed persistence keeps lease", leaseDuration: 10 * time.Minute, createErr: errors.New("database unavailable")},
		{name: "expired lease cannot persist", leaseDuration: 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				const executionTimeout = 5 * time.Minute
				leaseExpiry := time.Now().Add(tc.leaseDuration)
				plan := &ScheduledTestPlan{
					ID: 13, AccountID: 24, GroupID: i64ptr(35), ModelID: "gpt-6.1-sol",
					TestKind: ScheduledTestKindPelican, CronExpression: "* * * * *", Enabled: true,
					MaxResults: 5, LeaseToken: "lease", LeaseExpiresAt: &leaseExpiry, ActiveRunID: "lease:13",
					FailureStreak: 2,
				}
				resultRepo := &scheduledRunnerResultRepoStub{createErr: tc.createErr}
				finalized := false
				planRepo := &scheduledRunnerClaimerStub{finalize: func(ctx context.Context, id int64, leaseToken, runID string, lastRun, nextRun time.Time, streak int, lastError string) error {
					if err := ctx.Err(); err != nil {
						return err
					}
					if len(resultRepo.results) != 1 || id != plan.ID || leaseToken != plan.LeaseToken || runID != plan.ActiveRunID {
						t.Fatal("finalization did not match a persisted run and its lease")
					}
					if streak != 3 || lastError != context.DeadlineExceeded.Error() || nextRun.Sub(lastRun) != 4*time.Minute {
						t.Fatalf("unexpected failure update: streak=%d, error=%q, retry in %s", streak, lastError, nextRun.Sub(lastRun))
					}
					finalized = true
					return nil
				}}
				attempts := 0
				runner := &ScheduledTestRunnerService{
					planRepo:     planRepo,
					scheduledSvc: NewScheduledTestService(planRepo, resultRepo),
					pelicanExecutor: scheduledRunnerPelicanExecutorFunc(func(ctx context.Context) (*ScheduledTestResult, error) {
						attempts++
						<-ctx.Done()
						return nil, ctx.Err()
					}),
				}
				ctx, cancel := context.WithTimeout(context.Background(), executionTimeout)
				defer cancel()
				runner.runOnePlan(ctx, plan)

				if attempts != 1 || finalized != tc.wantFinalized || planRepo.updated {
					t.Fatalf("attempts = %d, finalized = %v, legacy update = %v; want 1, %v, false", attempts, finalized, planRepo.updated, tc.wantFinalized)
				}
				if resultRepo.createDeadline.IsZero() || resultRepo.createDeadline.After(leaseExpiry) || resultRepo.createDeadline.Sub(time.Now()) > 30*time.Second {
					t.Fatalf("persistence deadline %s exceeds its budget or lease %s", resultRepo.createDeadline, leaseExpiry)
				}
				if tc.wantFinalized {
					result := resultRepo.results[0]
					if result.Status != "failed" || result.RunID != plan.ActiveRunID || result.ErrorMessage != context.DeadlineExceeded.Error() {
						t.Fatalf("unexpected saved result: %+v", result)
					}
				} else if len(resultRepo.results) != 0 {
					t.Fatal("saved a result without a valid persistence window")
				}
			})
		})
	}
}

func i64ptr(value int64) *int64 { return &value }
