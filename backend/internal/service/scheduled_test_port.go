package service

import (
	"context"
	"errors"
	"time"
)

// ScheduledTestPlan represents a scheduled test plan domain model.
type ScheduledTestPlan struct {
	ID             int64      `json:"id"`
	AccountID      int64      `json:"account_id"`
	GroupID        *int64     `json:"group_id,omitempty"`
	ModelID        string     `json:"model_id"`
	TestKind       string     `json:"test_kind"`
	TestPrompt     string     `json:"test_prompt,omitempty"`
	CronExpression string     `json:"cron_expression"`
	Enabled        bool       `json:"enabled"`
	MaxResults     int        `json:"max_results"`
	AutoRecover    bool       `json:"auto_recover"`
	LastRunAt      *time.Time `json:"last_run_at"`
	NextRunAt      *time.Time `json:"next_run_at"`
	LeaseToken     string     `json:"-"`
	LeaseExpiresAt *time.Time `json:"-"`
	ActiveRunID    string     `json:"-"`
	FailureStreak  int        `json:"-"`
	LastError      string     `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

const (
	ScheduledTestKindConnection = "connection"
	ScheduledTestKindPelican   = "pelican"
	DefaultPelicanTestPrompt   = "Create a self-contained HTML file with a 2D SVG animation of a pelican riding a bicycle."
)

// ScheduledTestResult represents a single test execution result.
type ScheduledTestResult struct {
	ID           int64     `json:"id"`
	PlanID       int64     `json:"plan_id"`
	Status       string    `json:"status"`
	ResponseText string    `json:"response_text"`
	ErrorMessage string    `json:"error_message"`
	LatencyMs    int64     `json:"latency_ms"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	CreatedAt    time.Time `json:"created_at"`
	RunID        string    `json:"run_id,omitempty"`
}

// PelicanShowcaseRecord is the flat read model used by the public group
// showcase. It lets the repository fetch plans and results in one bounded SQL
// query instead of walking group -> account -> plan -> result.
type PelicanShowcaseRecord struct {
	GroupID     int64
	AccountID   int64
	AccountName string
	PlanID      int64
	ModelID     string
	Result      ScheduledTestResult
}

var ErrPelicanShowcaseReaderUnavailable = errors.New("pelican showcase bulk reader unavailable")

type PelicanShowcaseReader interface {
	ListLatestPelicanByGroups(ctx context.Context, groupIDs []int64, limitPerGroup int) ([]PelicanShowcaseRecord, error)
}

// PelicanTestExecutor is the stable boundary for group-scoped showcase tests.
// Ordinary scheduled connection tests continue to use AccountTestService
// directly; only Pelican plans cross this adapter.
type PelicanTestExecutor interface {
	Run(ctx context.Context, accountID, groupID int64, modelID, prompt string) (*ScheduledTestResult, error)
}

// ScheduledTestPlanClaimer is implemented by the durable repository. Keeping
// it separate from the legacy repository interface lets existing fakes and
// ordinary scheduled tests remain source-compatible while workers gain leases.
type ScheduledTestPlanClaimer interface {
	ClaimDue(ctx context.Context, now, leaseUntil time.Time, limit int, leaseToken string) ([]*ScheduledTestPlan, error)
	FinalizeRun(ctx context.Context, planID int64, leaseToken, runID string, lastRunAt, nextRunAt time.Time, failureStreak int, lastError string) error
}

// ScheduledTestPlanRepository defines the data access interface for test plans.
type ScheduledTestPlanRepository interface {
	Create(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	GetByID(ctx context.Context, id int64) (*ScheduledTestPlan, error)
	ListByAccountID(ctx context.Context, accountID int64) ([]*ScheduledTestPlan, error)
	ListDue(ctx context.Context, now time.Time) ([]*ScheduledTestPlan, error)
	Update(ctx context.Context, plan *ScheduledTestPlan) (*ScheduledTestPlan, error)
	Delete(ctx context.Context, id int64) error
	UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error
}

// ScheduledTestResultRepository defines the data access interface for test results.
type ScheduledTestResultRepository interface {
	Create(ctx context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error)
	ListByPlanID(ctx context.Context, planID int64, limit int) ([]*ScheduledTestResult, error)
	PruneOldResults(ctx context.Context, planID int64, keepCount int) error
}
