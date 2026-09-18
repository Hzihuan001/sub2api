package reseller

import (
	"context"
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
