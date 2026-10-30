package repository

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
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
		"id", "account_id", "group_id", "model_id", "test_kind", "test_prompt", "cron_expression", "enabled", "max_results", "auto_recover",
		"last_run_at", "next_run_at", "lease_token", "lease_expires_at", "active_run_id", "failure_streak", "last_error", "created_at", "updated_at",
	}
	mock.ExpectQuery("SELECT plan").WillReturnRows(sqlmock.NewRows(columns).AddRow(
		int64(7), int64(11), nil, "model", "connection", "prompt", "*/5 * * * *", true, 10, false,
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
