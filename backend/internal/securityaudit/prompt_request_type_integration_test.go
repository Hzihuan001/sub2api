package securityaudit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Exercise real PostgreSQL writes/reads: an SQL-string assertion cannot catch
// missing columns, misplaced bind parameters or lost asynchronous job metadata.
func TestPromptAuditRequestTypeDatabaseRoundTrip(t *testing.T) {
	db := openPromptAuditIntegrationDB(t)
	repo := NewPostgreSQLRepository(db)
	ctx := context.Background()
	for _, mode := range []string{"capture_only", "blocking", "async_audit"} {
		for _, kind := range []string{"sync", "stream", "ws_v2", "live", "cyber"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				snapshot := integrationSnapshot(mode + "-" + kind)
				snapshot.RequestType = kind
				snapshot.FullPrompt = "synthetic prompt " + kind
				var event *Event
				var err error
				switch mode {
				case "capture_only":
					event, err = repo.CaptureOnly(ctx, snapshot, 1)
				case "blocking":
					event, err = repo.RecordBlocking(ctx, snapshot, 1, integrationResult(EventCritical), true)
				case "async_audit":
					job, createErr := repo.CreateStagingWithCapacity(ctx, snapshot, 1, 3, 10)
					require.NoError(t, createErr)
					require.Equal(t, kind, job.Snapshot.RequestType)
					require.NoError(t, repo.PublishQueued(ctx, job.ID))
					claimed, ok, claimErr := repo.ClaimNextJob(ctx, time.Now().Add(time.Second))
					require.NoError(t, claimErr)
					require.True(t, ok)
					require.Equal(t, kind, claimed.Snapshot.RequestType)
					event, err = repo.Complete(ctx, claimed, integrationResult(EventCritical), true)
				}
				require.NoError(t, err)
				require.Equal(t, kind, event.Snapshot.RequestType)
				detail, err := repo.GetEvent(ctx, event.ID)
				require.NoError(t, err)
				require.Equal(t, kind, detail.Snapshot.RequestType)
			})
		}
	}
	// Historical/unknown types stay included unfiltered but never match a type.
	_, err := repo.CaptureOnly(ctx, integrationSnapshot("legacy"), 1)
	require.NoError(t, err)
	all, err := repo.ListEvents(ctx, EventFilter{}, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(16), all.Total)
	filter := EventFilter{RequestType: "stream"}
	page, err := repo.ListEvents(ctx, filter, 1, 100)
	require.NoError(t, err)
	require.Equal(t, int64(3), page.Total)
	for _, event := range page.Items {
		require.Equal(t, "stream", event.Snapshot.RequestType)
		require.Empty(t, event.Snapshot.FullPrompt)
	}
	exported, err := repo.StreamEvents(ctx, filter, 100, func(event *Event) error {
		require.Equal(t, "stream", event.Snapshot.RequestType)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), exported)
	start, end := time.Unix(0, 0), time.Now().Add(time.Minute)
	filter.StartAt, filter.EndAt = &start, &end
	preview, err := repo.PreviewDelete(ctx, filter)
	require.NoError(t, err)
	require.Equal(t, int64(3), preview.MatchedCount)
	deleted, err := repo.DeleteEventsByFilter(ctx, filter, preview.SnapshotMaxID, 100)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted.DeletedEvents)
	remaining, err := repo.CountEvents(ctx, EventFilter{})
	require.NoError(t, err)
	require.Equal(t, int64(13), remaining)
}
