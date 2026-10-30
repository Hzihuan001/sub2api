package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
)

const scheduledTestDefaultMaxWorkers = 10

// ScheduledTestRunnerService periodically scans due test plans and executes them.
type ScheduledTestRunnerService struct {
	planRepo       ScheduledTestPlanRepository
	scheduledSvc   *ScheduledTestService
	accountTestSvc *AccountTestService
	pelicanExecutor PelicanTestExecutor
	rateLimitSvc   *RateLimitService
	cfg            *config.Config

	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewScheduledTestRunnerService creates a new runner.
func NewScheduledTestRunnerService(
	planRepo ScheduledTestPlanRepository,
	scheduledSvc *ScheduledTestService,
	accountTestSvc *AccountTestService,
	rateLimitSvc *RateLimitService,
	cfg *config.Config,
) *ScheduledTestRunnerService {
	var pelicanExecutor PelicanTestExecutor
	if accountTestSvc != nil {
		pelicanExecutor = NewPelicanTestAdapter(accountTestSvc.accountRepo, accountTestSvc)
	}
	return &ScheduledTestRunnerService{
		planRepo:       planRepo,
		scheduledSvc:   scheduledSvc,
		accountTestSvc: accountTestSvc,
		pelicanExecutor: pelicanExecutor,
		rateLimitSvc:   rateLimitSvc,
		cfg:            cfg,
	}
}

// Start begins the cron ticker (every minute).
func (s *ScheduledTestRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		loc := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				loc = parsed
			}
		}

		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(loc))
		_, err := c.AddFunc("* * * * *", func() { s.runScheduled() })
		if err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] not started (invalid schedule): %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] started (tick=every minute)")
	})
}

// Stop gracefully shuts down the cron scheduler.
func (s *ScheduledTestRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cron != nil {
			ctx := s.cron.Stop()
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
				logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] cron stop timed out")
			}
		}
	})
}

func (s *ScheduledTestRunnerService) runScheduled() {
	// Delay 10s so execution lands at ~:10 of each minute instead of :00.
	time.Sleep(10 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	now := time.Now()
	leaseToken := uuid.NewString()
	leaseUntil := now.Add(10 * time.Minute)
	var plans []*ScheduledTestPlan
	var err error
	if claimer, ok := s.planRepo.(ScheduledTestPlanClaimer); ok {
		plans, err = claimer.ClaimDue(ctx, now, leaseUntil, scheduledTestDefaultMaxWorkers*2, leaseToken)
	} else {
		// Compatibility path for in-memory fakes and older callers. The durable
		// repository always implements ClaimDue after migration 9006.
		plans, err = s.planRepo.ListDue(ctx, now)
	}
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] ListDue error: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}

	logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] found %d due plans", len(plans))

	sem := make(chan struct{}, scheduledTestDefaultMaxWorkers)
	var wg sync.WaitGroup

	for _, plan := range plans {
		sem <- struct{}{}
		wg.Add(1)
		go func(p *ScheduledTestPlan) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOnePlan(ctx, p)
		}(plan)
	}

	wg.Wait()
}

func (s *ScheduledTestRunnerService) runOnePlan(ctx context.Context, plan *ScheduledTestPlan) {
	startedAt := time.Now()
	var result *ScheduledTestResult
	var err error
	if plan.TestKind == ScheduledTestKindPelican {
		prompt := plan.TestPrompt
		if prompt == "" {
			prompt = DefaultPelicanTestPrompt
		}
		if plan.GroupID == nil || s.pelicanExecutor == nil {
			err = fmt.Errorf("Pelican test requires a group-scoped executor")
		} else {
			result, err = s.pelicanExecutor.Run(ctx, plan.AccountID, *plan.GroupID, plan.ModelID, prompt)
		}
	} else {
		result, err = s.accountTestSvc.RunTestBackground(ctx, plan.AccountID, plan.ModelID)
	}
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d RunTestBackground error: %v", plan.ID, err)
		finishedAt := time.Now()
		result = &ScheduledTestResult{
			Status: "failed", ErrorMessage: err.Error(),
			StartedAt: startedAt, FinishedAt: finishedAt,
			LatencyMs: finishedAt.Sub(startedAt).Milliseconds(),
		}
	}
	if result == nil {
		finishedAt := time.Now()
		result = &ScheduledTestResult{
			Status: "failed", ErrorMessage: "scheduled test returned no result",
			StartedAt: startedAt, FinishedAt: finishedAt,
			LatencyMs: finishedAt.Sub(startedAt).Milliseconds(),
		}
	}
	if plan.ActiveRunID != "" {
		result.RunID = plan.ActiveRunID
	}

	saveErr := s.scheduledSvc.SaveResult(ctx, plan.ID, plan.MaxResults, result)
	if saveErr != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d SaveResult error: %v", plan.ID, saveErr)
		// A leased run must not be finalized after its result failed to persist.
		// Let the lease expire so another worker can retry the complete operation.
		if plan.LeaseToken != "" && plan.ActiveRunID != "" {
			return
		}
	}

	// Auto-recover account if test succeeded and auto_recover is enabled.
	if result.Status == "success" && plan.AutoRecover {
		s.tryRecoverAccount(ctx, plan.AccountID, plan.ID)
	}

	now := time.Now()
	nextRun, err := computeNextRun(plan.CronExpression, now)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d computeNextRun error: %v", plan.ID, err)
		nextRun = now.Add(5 * time.Minute)
	}
	failureStreak := 0
	lastError := ""
	if result.Status != "success" {
		failureStreak = plan.FailureStreak + 1
		lastError = result.ErrorMessage
		backoff := scheduledTestFailureBackoff(failureStreak)
		if retryAt := now.Add(backoff); nextRun.Before(retryAt) {
			nextRun = retryAt
		}
	}

	if claimer, ok := s.planRepo.(ScheduledTestPlanClaimer); ok && plan.LeaseToken != "" && plan.ActiveRunID != "" {
		if err := claimer.FinalizeRun(ctx, plan.ID, plan.LeaseToken, plan.ActiveRunID, now, nextRun, failureStreak, lastError); err != nil {
			logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d FinalizeRun error: %v", plan.ID, err)
		}
		return
	}
	if err := s.planRepo.UpdateAfterRun(ctx, plan.ID, now, nextRun); err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d UpdateAfterRun error: %v", plan.ID, err)
	}
}

func scheduledTestFailureBackoff(streak int) time.Duration {
	if streak < 1 {
		return time.Minute
	}
	if streak > 4 {
		streak = 4
	}
	return time.Minute * time.Duration(1<<(streak-1))
}

// tryRecoverAccount attempts to recover an account from recoverable runtime state.
func (s *ScheduledTestRunnerService) tryRecoverAccount(ctx context.Context, accountID int64, planID int64) {
	if s.rateLimitSvc == nil {
		return
	}

	recovery, err := s.rateLimitSvc.RecoverAccountAfterSuccessfulTest(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover failed: %v", planID, err)
		return
	}
	if recovery == nil {
		return
	}

	if recovery.ClearedError {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d recovered from error status", planID, accountID)
	}
	if recovery.ClearedRateLimit {
		logger.LegacyPrintf("service.scheduled_test_runner", "[ScheduledTestRunner] plan=%d auto-recover: account=%d cleared rate-limit/runtime state", planID, accountID)
	}
}
