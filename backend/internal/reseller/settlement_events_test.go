package reseller

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestAcknowledgeSettlementEventsRejectsUnknownEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM reseller_settlement_events").
		WithArgs(int64(7), "{11,12}").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err = (&Service{db: db}).AcknowledgeSettlementEvents(context.Background(), 7, []int64{11, 12})
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListSettlementEventsUsesImmutableSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	snapshot, err := json.Marshal(Settlement{
		ID: 42, Revision: 1, RequestSource: "monitor", RequestID: "00000000-0000-0000-0000-000000000042",
		ProductID: 7, ProductCode: "deepseek", DisplayName: "DeepSeek", Status: "completed",
	})
	require.NoError(t, err)
	mock.ExpectExec("DELETE FROM reseller_settlement_events").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT e.id,e.revision,e.settlement_snapshot").
		WithArgs(int64(7), 500).
		WillReturnRows(sqlmock.NewRows([]string{"id", "revision", "settlement_snapshot"}).
			AddRow(int64(11), int64(1), snapshot))

	page, err := (&Service{db: db}).ListSettlementEvents(context.Background(), 7, 500)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, int64(1), page.Items[0].Revision)
	require.Equal(t, "deepseek", page.Items[0].Settlement.ProductCode)
	require.Equal(t, "monitor", page.Items[0].Settlement.RequestSource)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAcknowledgeSettlementEventsIsIdempotentForKnownEvents(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM reseller_settlement_events").
		WithArgs(int64(7), "{11}").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec("UPDATE reseller_settlement_events SET acknowledged_at").
		WithArgs(int64(7), "{11}").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, (&Service{db: db}).AcknowledgeSettlementEvents(context.Background(), 7, []int64{11, 11}))
	require.NoError(t, mock.ExpectationsWereMet())
}
