package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

// --- Plan Repository ---

type scheduledTestPlanRepository struct {
	db *sql.DB
}

func NewScheduledTestPlanRepository(db *sql.DB) service.ScheduledTestPlanRepository {
	return &scheduledTestPlanRepository{db: db}
}

func (r *scheduledTestPlanRepository) Create(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	if err := r.validateScheduledTestPlan(ctx, plan); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_plans (account_id, group_id, model_id, test_kind, test_prompt, cron_expression, enabled, max_results, auto_recover, next_run_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, account_id, group_id, model_id, test_kind, test_prompt, cron_expression, enabled, max_results, auto_recover, last_run_at, next_run_at, lease_token, lease_expires_at, active_run_id, failure_streak, last_error, created_at, updated_at
	`, plan.AccountID, plan.GroupID, plan.ModelID, normalizeScheduledTestKind(plan.TestKind), plan.TestPrompt, plan.CronExpression, plan.Enabled, plan.MaxResults, plan.AutoRecover, plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) GetByID(ctx context.Context, id int64) (*service.ScheduledTestPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, account_id, group_id, model_id, test_kind, test_prompt, cron_expression, enabled, max_results, auto_recover, last_run_at, next_run_at, lease_token, lease_expires_at, active_run_id, failure_streak, last_error, created_at, updated_at
		FROM scheduled_test_plans WHERE id = $1
	`, id)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) ListByAccountID(ctx context.Context, accountID int64) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, group_id, model_id, test_kind, test_prompt, cron_expression, enabled, max_results, auto_recover, last_run_at, next_run_at, lease_token, lease_expires_at, active_run_id, failure_streak, last_error, created_at, updated_at
		FROM scheduled_test_plans WHERE account_id = $1
		ORDER BY created_at DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) ListDue(ctx context.Context, now time.Time) ([]*service.ScheduledTestPlan, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, account_id, group_id, model_id, test_kind, test_prompt, cron_expression, enabled, max_results, auto_recover, last_run_at, next_run_at, lease_token, lease_expires_at, active_run_id, failure_streak, last_error, created_at, updated_at
		FROM scheduled_test_plans
		WHERE enabled = true AND next_run_at <= $1
		ORDER BY next_run_at ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

// ClaimDue atomically leases due plans so multiple workers cannot execute the
// same scheduled test concurrently. The lease token is shared by one scan;
// active_run_id remains unique per plan because it includes the plan ID.
func (r *scheduledTestPlanRepository) ClaimDue(ctx context.Context, now, leaseUntil time.Time, limit int, leaseToken string) ([]*service.ScheduledTestPlan, error) {
	if limit <= 0 {
		limit = 100
	}
	if leaseToken == "" {
		leaseToken = uuid.NewString()
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH due AS (
			SELECT id
			FROM scheduled_test_plans
			WHERE enabled = true
			  AND next_run_at <= $1
			  AND (lease_expires_at IS NULL OR lease_expires_at <= $1)
			ORDER BY next_run_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT $4
		)
		UPDATE scheduled_test_plans p
		SET lease_token = $2,
		    lease_expires_at = $3,
		    active_run_id = $2 || ':' || p.id::text,
		    updated_at = NOW()
		FROM due
		WHERE p.id = due.id
		RETURNING p.id, p.account_id, p.group_id, p.model_id, p.test_kind, p.test_prompt,
		          p.cron_expression, p.enabled, p.max_results, p.auto_recover,
		          p.last_run_at, p.next_run_at, p.lease_token, p.lease_expires_at,
		          p.active_run_id, p.failure_streak, p.last_error, p.created_at, p.updated_at
	`, now, leaseToken, leaseUntil, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPlans(rows)
}

func (r *scheduledTestPlanRepository) Update(ctx context.Context, plan *service.ScheduledTestPlan) (*service.ScheduledTestPlan, error) {
	if err := r.validateScheduledTestPlan(ctx, plan); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		UPDATE scheduled_test_plans
		SET group_id = $2, model_id = $3, test_kind = $4, test_prompt = $5, cron_expression = $6, enabled = $7, max_results = $8, auto_recover = $9, next_run_at = $10, updated_at = NOW()
		WHERE id = $1
		RETURNING id, account_id, group_id, model_id, test_kind, test_prompt, cron_expression, enabled, max_results, auto_recover, last_run_at, next_run_at, lease_token, lease_expires_at, active_run_id, failure_streak, last_error, created_at, updated_at
	`, plan.ID, plan.GroupID, plan.ModelID, normalizeScheduledTestKind(plan.TestKind), plan.TestPrompt, plan.CronExpression, plan.Enabled, plan.MaxResults, plan.AutoRecover, plan.NextRunAt)
	return scanPlan(row)
}

func (r *scheduledTestPlanRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM scheduled_test_plans WHERE id = $1`, id)
	return err
}

func (r *scheduledTestPlanRepository) UpdateAfterRun(ctx context.Context, id int64, lastRunAt time.Time, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_test_plans SET last_run_at = $2, next_run_at = $3, updated_at = NOW() WHERE id = $1
	`, id, lastRunAt, nextRunAt)
	return err
}

// FinalizeRun clears a lease only when the worker still owns the plan and the
// run ID matches. A stale worker therefore cannot overwrite a newer result.
func (r *scheduledTestPlanRepository) FinalizeRun(ctx context.Context, planID int64, leaseToken, runID string, lastRunAt, nextRunAt time.Time, failureStreak int, lastError string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE scheduled_test_plans
		SET last_run_at = $3,
		    next_run_at = $4,
		    lease_token = NULL,
		    lease_expires_at = NULL,
		    active_run_id = NULL,
		    failure_streak = $5,
		    last_error = $6,
		    updated_at = NOW()
		WHERE id = $1 AND lease_token = $2 AND active_run_id = $7
	`, planID, leaseToken, lastRunAt, nextRunAt, failureStreak, lastError, runID)
	if err != nil {
		return err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return fmt.Errorf("scheduled test lease lost for plan %d", planID)
	}
	return nil
}

// --- Result Repository ---

type scheduledTestResultRepository struct {
	db *sql.DB
}

func NewScheduledTestResultRepository(db *sql.DB) service.ScheduledTestResultRepository {
	return &scheduledTestResultRepository{db: db}
}

func (r *scheduledTestResultRepository) Create(ctx context.Context, result *service.ScheduledTestResult) (*service.ScheduledTestResult, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO scheduled_test_results (plan_id, status, response_text, error_message, latency_ms, started_at, finished_at, run_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		ON CONFLICT (plan_id, run_id) WHERE run_id <> '' DO NOTHING
		RETURNING id, plan_id, status, response_text, error_message, latency_ms, started_at, finished_at, run_id, created_at
	`, result.PlanID, result.Status, result.ResponseText, result.ErrorMessage, result.LatencyMs, result.StartedAt, result.FinishedAt, result.RunID)

	out := &service.ScheduledTestResult{}
	if err := row.Scan(
		&out.ID, &out.PlanID, &out.Status, &out.ResponseText, &out.ErrorMessage,
		&out.LatencyMs, &out.StartedAt, &out.FinishedAt, &out.RunID, &out.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows && result.RunID != "" {
			existing := r.db.QueryRowContext(ctx, `
				SELECT id, plan_id, status, response_text, error_message, latency_ms, started_at, finished_at, run_id, created_at
				FROM scheduled_test_results WHERE plan_id = $1 AND run_id = $2
			`, result.PlanID, result.RunID)
			out := &service.ScheduledTestResult{}
			if lookupErr := existing.Scan(
				&out.ID, &out.PlanID, &out.Status, &out.ResponseText, &out.ErrorMessage,
				&out.LatencyMs, &out.StartedAt, &out.FinishedAt, &out.RunID, &out.CreatedAt,
			); lookupErr == nil {
				return out, nil
			}
		}
		return nil, err
	}
	return out, nil
}

func (r *scheduledTestResultRepository) ListByPlanID(ctx context.Context, planID int64, limit int) ([]*service.ScheduledTestResult, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, plan_id, status, response_text, error_message, latency_ms, started_at, finished_at, run_id, created_at
		FROM scheduled_test_results
		WHERE plan_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, planID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []*service.ScheduledTestResult
	for rows.Next() {
		r := &service.ScheduledTestResult{}
		if err := rows.Scan(
			&r.ID, &r.PlanID, &r.Status, &r.ResponseText, &r.ErrorMessage,
			&r.LatencyMs, &r.StartedAt, &r.FinishedAt, &r.RunID, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func (r *scheduledTestResultRepository) PruneOldResults(ctx context.Context, planID int64, keepCount int) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM scheduled_test_results
		WHERE id IN (
			SELECT id FROM (
				SELECT id, ROW_NUMBER() OVER (PARTITION BY plan_id ORDER BY created_at DESC) AS rn
				FROM scheduled_test_results
				WHERE plan_id = $1
			) ranked
			WHERE rn > $2
		)
	`, planID, keepCount)
	return err
}

// --- scan helpers ---

type scannable interface {
	Scan(dest ...any) error
}

func scanPlan(row scannable) (*service.ScheduledTestPlan, error) {
	p := &service.ScheduledTestPlan{}
	if err := row.Scan(
		&p.ID, &p.AccountID, &p.GroupID, &p.ModelID, &p.TestKind, &p.TestPrompt, &p.CronExpression, &p.Enabled, &p.MaxResults, &p.AutoRecover,
		&p.LastRunAt, &p.NextRunAt, &p.LeaseToken, &p.LeaseExpiresAt, &p.ActiveRunID, &p.FailureStreak, &p.LastError, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return p, nil
}

func normalizeScheduledTestKind(kind string) string {
	if kind == service.ScheduledTestKindPelican {
		return service.ScheduledTestKindPelican
	}
	return service.ScheduledTestKindConnection
}

func (r *scheduledTestPlanRepository) validateScheduledTestPlan(ctx context.Context, plan *service.ScheduledTestPlan) error {
	if plan == nil {
		return fmt.Errorf("scheduled test plan is nil")
	}
	plan.TestKind = normalizeScheduledTestKind(plan.TestKind)
	if plan.TestKind == service.ScheduledTestKindPelican && plan.GroupID == nil {
		return fmt.Errorf("group_id is required for Pelican tests")
	}
	if plan.GroupID != nil {
		var belongs bool
		if err := r.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM account_groups
				WHERE account_id = $1 AND group_id = $2
			)`, plan.AccountID, *plan.GroupID).Scan(&belongs); err != nil {
			return err
		}
		if !belongs {
			return fmt.Errorf("group_id %d is not assigned to account %d", *plan.GroupID, plan.AccountID)
		}
	}
	return nil
}

func scanPlans(rows *sql.Rows) ([]*service.ScheduledTestPlan, error) {
	var plans []*service.ScheduledTestPlan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		plans = append(plans, p)
	}
	return plans, rows.Err()
}
