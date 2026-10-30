package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scheduledRunnerPlanRepoStub struct {
	updated   bool
	lastRun   time.Time
	nextRun   time.Time
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
	results []*ScheduledTestResult
}

func (r *scheduledRunnerResultRepoStub) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	r.results = append(r.results, result)
	return result, nil
}
func (r *scheduledRunnerResultRepoStub) ListByPlanID(context.Context, int64, int) ([]*ScheduledTestResult, error) {
	return r.results, nil
}
func (r *scheduledRunnerResultRepoStub) PruneOldResults(context.Context, int64, int) error { return nil }

type scheduledRunnerPelicanExecutorStub struct{}

func (scheduledRunnerPelicanExecutorStub) Run(_ context.Context, _, _ int64, _, _ string) (*ScheduledTestResult, error) {
	now := time.Now()
	return &ScheduledTestResult{
		Status: "failed", ErrorMessage: "upstream overload", StartedAt: now,
		FinishedAt: now, LatencyMs: 1,
	}, nil
}

func TestScheduledTestRunnerPersistsPelicanFailureAndAdvancesSchedule(t *testing.T) {
	planRepo := &scheduledRunnerPlanRepoStub{}
	resultRepo := &scheduledRunnerResultRepoStub{}
	runner := &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   NewScheduledTestService(planRepo, resultRepo),
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

func i64ptr(value int64) *int64 { return &value }
