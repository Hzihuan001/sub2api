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

func TestProductAccountCredentialsUsePassthroughMode(t *testing.T) {
	credentials := productAccountCredentials("key", "https://main.example", []string{"gpt-image-1", " gpt-5.6 "})

	require.Equal(t, "key", credentials["api_key"])
	require.Equal(t, "https://main.example", credentials["base_url"])
	_, hasMapping := credentials["model_mapping"]
	require.False(t, hasMapping)
}

func TestWithPassthroughExtraUsesPlatformSpecificSwitch(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		accountType string
		key         string
		changed     bool
	}{
		{name: "openai", platform: service.PlatformOpenAI, accountType: service.AccountTypeAPIKey, key: "openai_passthrough", changed: true},
		{name: "anthropic api key", platform: service.PlatformAnthropic, accountType: service.AccountTypeAPIKey, key: "anthropic_passthrough", changed: true},
		{name: "anthropic oauth unsupported", platform: service.PlatformAnthropic, accountType: service.AccountTypeOAuth},
		{name: "other platform", platform: service.PlatformGemini, accountType: service.AccountTypeAPIKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extra, changed := withPassthroughExtra(map[string]any{"keep": "value"}, tt.platform, tt.accountType)
			require.Equal(t, tt.changed, changed)
			require.Equal(t, "value", extra["keep"])
			if tt.key != "" {
				require.Equal(t, true, extra[tt.key])
			}
		})
	}
}
