package moshureseller

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestRecordSyncDomainSuccessSurvivesCallerCancellation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("UPDATE moshu_reseller_connections SET catalog_last_success_at=").
		WillReturnResult(sqlmock.NewResult(0, 1))

	service := &Service{db: db}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	service.recordSyncDomainSuccess(ctx, "catalog")
	require.NoError(t, mock.ExpectationsWereMet())
}
