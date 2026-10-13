//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestBatchImageRepository_AsyncStateCompareAndSet(t *testing.T) {
	ctx := context.Background()
	repo := newBatchImageRepositoryWithSQL(testTx(t))
	batchID := batchImageTestID(t, "async-cas")
	createAsyncStateTestJob(t, repo, batchID, service.BatchImageJobStatusRunning)
	payload := json.RawMessage(`{"item":{"custom_id":"line-1","prompt":"keep prompt","reference_images":[{"data":"reference"}]},"image_size":"2K","aspect_ratio":"1:1","response_mime_type":"image/png","extra":{"keep":true}}`)
	createAsyncStateTestItem(t, repo, batchID, "line-1", payload)
	first := json.RawMessage(`{"phase":"submitting","idempotency_key":"stable-key"}`)
	next := json.RawMessage(`{"phase":"submitted","task_id":"task-1","idempotency_key":"stable-key"}`)

	require.NoError(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "line-1", nil, first))
	require.ErrorIs(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "line-1", nil, next), service.ErrBatchImageIndexStateConflict)
	// JSONB equality must ignore property ordering and whitespace.
	require.NoError(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "line-1", json.RawMessage(`{ "idempotency_key": "stable-key", "phase": "submitting" }`), next))
	require.ErrorIs(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "line-1", first, first), service.ErrBatchImageIndexStateConflict)
	require.ErrorIs(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "missing", nil, first), service.ErrBatchImageIndexStateConflict)

	stored := readAsyncStateTestPayload(t, repo, batchID, "line-1")
	require.JSONEq(t, string(next), string(stored["managed_async"]))
	delete(stored, "managed_async")
	preserved, err := json.Marshal(stored)
	require.NoError(t, err)
	require.JSONEq(t, string(payload), string(preserved))
}

func TestBatchImageRepository_AsyncCancelPreventsNewSubmissionAndPreservesExistingTask(t *testing.T) {
	ctx := context.Background()
	repo := newBatchImageRepositoryWithSQL(testTx(t))
	batchID := batchImageTestID(t, "async-cancel")
	otherBatchID := batchImageTestID(t, "async-unrelated")
	for _, id := range []string{batchID, otherBatchID} {
		createAsyncStateTestJob(t, repo, id, service.BatchImageJobStatusRunning)
		createAsyncStateTestItem(t, repo, id, "pending", json.RawMessage(`{"item":{"prompt":"keep prompt"},"image_size":"2K"}`))
	}
	createAsyncStateTestItem(t, repo, batchID, "legacy-null", nil)
	createAsyncStateTestItem(t, repo, batchID, "in-flight", json.RawMessage(`{"item":{"prompt":"submitted prompt"}}`))
	first := json.RawMessage(`{"task_id":"task-1","phase":"submitted"}`)
	final := json.RawMessage(`{"task_id":"task-1","phase":"completed"}`)
	require.NoError(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "in-flight", nil, first))

	require.NoError(t, repo.RequestBatchImageAsyncCancel(ctx, batchID))
	require.NoError(t, repo.RequestBatchImageAsyncCancel(ctx, batchID))
	for _, customID := range []string{"pending", "legacy-null"} {
		require.ErrorIs(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, customID, nil, first), service.ErrBatchImageIndexStateConflict)
		stored := readAsyncStateTestPayload(t, repo, batchID, customID)
		require.JSONEq(t, "true", string(stored["managed_async_cancel_requested"]))
		require.NotContains(t, stored, "managed_async")
	}
	before := readAsyncStateTestPayload(t, repo, batchID, "in-flight")
	require.JSONEq(t, string(first), string(before["managed_async"]))
	require.NoError(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "in-flight", first, final))
	stored := readAsyncStateTestPayload(t, repo, batchID, "in-flight")
	require.JSONEq(t, string(final), string(stored["managed_async"]))
	require.JSONEq(t, "true", string(stored["managed_async_cancel_requested"]))
	require.JSONEq(t, `{"prompt":"submitted prompt"}`, string(stored["item"]))
	other := readAsyncStateTestPayload(t, repo, otherBatchID, "pending")
	require.NotContains(t, other, "managed_async_cancel_requested")
	require.NotContains(t, other, "managed_async")
	require.NoError(t, repo.CompareAndSetBatchImageAsyncState(ctx, otherBatchID, "pending", nil, first))
	require.ErrorIs(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "pending", first, final), service.ErrBatchImageIndexStateConflict)
}

func TestBatchImageRepository_AsyncStateDoesNotUpdateTerminalJobs(t *testing.T) {
	for _, status := range []string{
		service.BatchImageJobStatusCompleted,
		service.BatchImageJobStatusFailed,
		service.BatchImageJobStatusCancelled,
		service.BatchImageJobStatusOutputDeleted,
	} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			repo := newBatchImageRepositoryWithSQL(testTx(t))
			batchID := batchImageTestID(t, "async-terminal")
			createAsyncStateTestJob(t, repo, batchID, status)
			payload := json.RawMessage(`{"item":{"prompt":"keep"},"managed_async":{"task_id":"task-1"}}`)
			createAsyncStateTestItem(t, repo, batchID, "line-1", payload)
			require.ErrorIs(t, repo.CompareAndSetBatchImageAsyncState(ctx, batchID, "line-1", json.RawMessage(`{"task_id":"task-1"}`), json.RawMessage(`{"task_id":"task-2"}`)), service.ErrBatchImageIndexStateConflict)
			require.NoError(t, repo.RequestBatchImageAsyncCancel(ctx, batchID))
			stored, err := json.Marshal(readAsyncStateTestPayload(t, repo, batchID, "line-1"))
			require.NoError(t, err)
			require.JSONEq(t, string(payload), string(stored))
		})
	}
}

func createAsyncStateTestJob(t *testing.T, repo *batchImageRepository, batchID, status string) {
	t.Helper()
	_, err := repo.CreateBatchImageJob(context.Background(), service.CreateBatchImageJobParams{
		BatchID: batchID, UserID: 1001, Provider: service.BatchImageProviderAppManaged,
		Model: "gemini-2.5-flash-image", Status: status, ItemCount: 3,
	})
	require.NoError(t, err)
}

func createAsyncStateTestItem(t *testing.T, repo *batchImageRepository, batchID, customID string, payload json.RawMessage) {
	t.Helper()
	_, err := repo.CreateBatchImageItem(context.Background(), service.CreateBatchImageItemParams{
		JobID: batchID, CustomID: customID, Status: service.BatchImageItemStatusPending, InputPayload: payload,
	})
	require.NoError(t, err)
}

func readAsyncStateTestPayload(t *testing.T, repo *batchImageRepository, batchID, customID string) map[string]json.RawMessage {
	t.Helper()
	var payload []byte
	err := repo.sql.QueryRowContext(context.Background(), `SELECT input_payload FROM batch_image_items WHERE job_id = $1 AND custom_id = $2`, batchID, customID).Scan(&payload)
	require.NoError(t, err)
	var result map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(payload, &result))
	return result
}
