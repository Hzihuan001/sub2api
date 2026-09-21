package moshureseller

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// syncProductAdmin records the fields that catalog reconciliation sends to the
// account service. Embedding AdminService keeps this focused on the operations
// used by syncExistingProductAccount.
type syncProductAdmin struct {
	service.AdminService
	updated     *service.UpdateAccountInput
	accountID   int64
	schedulable bool
	scheduleID  int64
}

func (a *syncProductAdmin) UpdateAccount(_ context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	a.accountID = id
	a.updated = input
	return &service.Account{ID: id, Platform: input.Platform}, nil
}

func (a *syncProductAdmin) SetAccountSchedulable(_ context.Context, id int64, schedulable bool) (*service.Account, error) {
	a.scheduleID = id
	a.schedulable = schedulable
	return &service.Account{ID: id, Schedulable: schedulable}, nil
}

func TestSyncExistingProductAccountUpdatesPlatformAndClearsModelMapping(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{
		"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh",
		"expiry", "etag", "version", "catalog_at", "settlement_at", "error",
	}).AddRow("https://main.example", "instance", 1, "L1", "v1", "active", "access", "refresh",
		time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))
	mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{
		"id", "remote", "code", "name", "platform", "group", "authorized", "selected", "cost", "sales", "version",
		"models", "capabilities", "credential", "local_group", "local_account", "effective", "cost_override",
	}).AddRow(3, 7, "deepseek", "DeepSeek", service.PlatformDeepseek, 9, true, true, 0.3, 1.0, 1,
		[]byte(`["deepseek-v4.1-flash"]`), []byte(`{}`), "new-key", 10, 20, time.Now(), nil))

	localGroupID := int64(10)
	admin := &syncProductAdmin{}
	sut := NewService(db, testEncryptor{}, admin)
	legacy := &service.Account{
		ID:          20,
		Name:        "legacy Claude account",
		Platform:    service.PlatformAnthropic,
		Type:        service.AccountTypeAPIKey,
		Status:      service.StatusActive,
		GroupIDs:    []int64{10},
		Concurrency: 12,
		Credentials: map[string]any{"api_key": "old-key", "model_mapping": map[string]any{"claude": "claude"}},
		Extra:       map[string]any{"old": "value"},
	}
	product := Product{
		ID:                 3,
		ProductCode:        "deepseek",
		DisplayName:        "DeepSeek",
		Platform:           service.PlatformDeepseek,
		Selected:           true,
		Models:             []string{"deepseek-v4.1-flash"},
		LocalGroupID:       &localGroupID,
		CostRateMultiplier: 0.3,
	}

	err = sut.syncExistingProductAccount(context.Background(), product, legacy)
	require.NoError(t, err)
	require.Equal(t, int64(20), admin.accountID)
	require.Equal(t, int64(20), admin.scheduleID)
	require.True(t, admin.schedulable)
	require.NotNil(t, admin.updated)
	require.Equal(t, service.PlatformDeepseek, admin.updated.Platform)
	require.Equal(t, "new-key", admin.updated.Credentials["api_key"])
	_, hasMapping := admin.updated.Credentials["model_mapping"]
	require.False(t, hasMapping, "reseller passthrough sync must remove stale model mapping")
	require.Equal(t, true, admin.updated.Extra[service.MoshuResellerPassthroughExtraKey])
	require.Equal(t, []string{"deepseek-v4.1-flash"}, admin.updated.Extra[service.MoshuResellerModelSnapshotExtraKey])
	require.Equal(t, "value", admin.updated.Extra["old"])
	require.Equal(t, 0.3, *admin.updated.RateMultiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}
