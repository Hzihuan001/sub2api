//go:build integration

package moshureseller

import (
	"context"
	"database/sql"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	postgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"testing"
	"time"
)

func TestProfitReconciliationHandlesLateLogsAndZeroPrice(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("profit_test"), postgres.WithUsername("test"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, container.Terminate(ctx)) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`CREATE TABLE moshu_request_profit_records(id bigint PRIMARY KEY,request_id text,local_usage_log_id bigint,l1_customer_charge numeric,moshu_actual_cost numeric,gross_profit numeric,updated_at timestamptz);
	CREATE TABLE usage_logs(id bigint,request_id text,actual_cost numeric);
	INSERT INTO moshu_request_profit_records VALUES(1,'raw',NULL,0,1,-1,NOW()),(2,'client',NULL,0,1,-1,NOW()),(3,'local',NULL,0,1,-1,NOW()),(4,'late',NULL,0,1,-1,NOW());
	INSERT INTO usage_logs VALUES(1,'raw',3),(2,'client:client',0),(3,'local:local',0.5);`)
	require.NoError(t, err)
	sut := NewService(db, nil, nil)
	require.NoError(t, sut.reconcilePendingProfits(ctx))
	for id, profit := range map[int]float64{1: 2, 2: -1, 3: -0.5} {
		var usageID int
		var actual float64
		require.NoError(t, db.QueryRow(`SELECT local_usage_log_id,gross_profit FROM moshu_request_profit_records WHERE id=$1`, id).Scan(&usageID, &actual))
		require.Equal(t, id, usageID)
		require.Equal(t, profit, actual)
	}
	var before, after time.Time
	require.NoError(t, db.QueryRow(`SELECT updated_at FROM moshu_request_profit_records WHERE id=1`).Scan(&before))
	_, err = db.Exec(`INSERT INTO usage_logs VALUES(4,'late',4)`)
	require.NoError(t, err)
	require.NoError(t, sut.reconcilePendingProfits(ctx))
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM moshu_request_profit_records WHERE local_usage_log_id IS NULL`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(`SELECT updated_at FROM moshu_request_profit_records WHERE id=1`).Scan(&after))
	require.Equal(t, before, after) // Already reconciled rows are never rewritten.
}
