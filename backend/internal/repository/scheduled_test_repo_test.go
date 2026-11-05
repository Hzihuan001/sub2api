package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestScanPlanAcceptsNullLeaseMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	createdAt := time.Now().UTC()
	updatedAt := createdAt.Add(time.Second)
	columns := []string{
		"id", "account_id", "group_id", "model_id", "test_kind", "test_prompt", "thinking_effort", "cron_expression", "enabled", "max_results", "auto_recover",
		"last_run_at", "next_run_at", "lease_token", "lease_expires_at", "active_run_id", "failure_streak", "last_error", "created_at", "updated_at",
	}
	mock.ExpectQuery("SELECT plan").WillReturnRows(sqlmock.NewRows(columns).AddRow(
		int64(7), int64(11), nil, "model", "connection", "prompt", "default", "*/5 * * * *", true, 10, false,
		nil, nil, nil, nil, nil, 0, nil, createdAt, updatedAt,
	))

	plan, err := scanPlan(db.QueryRow("SELECT plan"))
	if err != nil {
		t.Fatalf("scanPlan() error = %v", err)
	}
	if plan.LeaseToken != "" || plan.ActiveRunID != "" || plan.LastError != "" {
		t.Fatalf("nullable lease metadata should map to empty strings: %#v", plan)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestCreatePlanPersistsThinkingEffort(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	nextRunAt := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	createdAt := time.Now().UTC()
	columns := []string{
		"id", "account_id", "group_id", "model_id", "test_kind", "test_prompt", "thinking_effort", "cron_expression", "enabled", "max_results", "auto_recover",
		"last_run_at", "next_run_at", "lease_token", "lease_expires_at", "active_run_id", "failure_streak", "last_error", "created_at", "updated_at",
	}
	mock.ExpectQuery("INSERT INTO scheduled_test_plans").
		WithArgs(int64(11), nil, "gpt-6.1-sol", "connection", "prompt", "xhigh", "@hourly", true, 10, false, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows(columns).AddRow(
			int64(7), int64(11), nil, "gpt-6.1-sol", "connection", "prompt", "xhigh", "@hourly", true, 10, false,
			nil, nextRunAt, nil, nil, nil, 0, nil, createdAt, createdAt,
		))

	plan, err := (&scheduledTestPlanRepository{db: db}).Create(context.Background(), &service.ScheduledTestPlan{
		AccountID:      11,
		ModelID:        "gpt-6.1-sol",
		TestKind:       service.ScheduledTestKindConnection,
		TestPrompt:     "prompt",
		ThinkingEffort: "xhigh",
		CronExpression: "@hourly",
		Enabled:        true,
		MaxResults:     10,
		NextRunAt:      &nextRunAt,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if plan.ThinkingEffort != "xhigh" {
		t.Fatalf("ThinkingEffort = %q, want xhigh", plan.ThinkingEffort)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}
