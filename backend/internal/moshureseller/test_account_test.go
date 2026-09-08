package moshureseller

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type testAccountAdmin struct {
	service.AdminService
	t               *testing.T
	created         int
	unschedulable   int
	createdGroupIDs []int64
	expectedKey     string
	expectedBaseURL string
}

func (a *testAccountAdmin) CreateAccount(_ context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	a.created++
	a.createdGroupIDs = append([]int64(nil), input.GroupIDs...)
	require.True(a.t, input.SkipDefaultGroupBind)
	expectedKey := a.expectedKey
	if expectedKey == "" {
		expectedKey = "test-key"
	}
	require.Equal(a.t, expectedKey, input.Credentials["api_key"])
	expectedBaseURL := a.expectedBaseURL
	if expectedBaseURL == "" {
		expectedBaseURL = "https://main.example"
	}
	require.Equal(a.t, expectedBaseURL, input.Credentials["base_url"])
	return &service.Account{ID: 42}, nil
}

func (a *testAccountAdmin) SetAccountSchedulable(_ context.Context, id int64, schedulable bool) (*service.Account, error) {
	require.Equal(a.t, int64(42), id)
	require.False(a.t, schedulable)
	a.unschedulable++
	return &service.Account{ID: id, Schedulable: schedulable}, nil
}

func TestEnsureProductTestAccountCreatesUnscheduledAccountWithoutSalesGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT id,remote_product_id").WithArgs(int64(3)).
		WillReturnRows(pricingRows(false, 1, nil, nil))
	mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{
		"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh",
		"expiry", "etag", "version", "catalog_at", "settlement_at", "error",
	}).AddRow("https://main.example", "instance", 1, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "etag", 1, nil, nil, nil))

	admin := &testAccountAdmin{t: t}
	accountID, err := NewService(db, testEncryptor{}, admin).EnsureProductTestAccount(context.Background(), 3)

	require.NoError(t, err)
	require.Equal(t, int64(42), accountID)
	require.Equal(t, 1, admin.created)
	require.Equal(t, 1, admin.unschedulable)
	require.Empty(t, admin.createdGroupIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProductAccountCredentialsExposeAuthorizedModelsToStandardTestDialog(t *testing.T) {
	credentials := productAccountCredentials("key", "https://main.example", []string{"gpt-image-1", " gpt-5.6 "})

	require.Equal(t, map[string]any{
		"gpt-image-1": "gpt-image-1",
		"gpt-5.6":     "gpt-5.6",
	}, credentials["model_mapping"])
}
