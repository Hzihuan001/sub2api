package moshureseller

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type catalogAdmin struct {
	service.AdminService
	t                            *testing.T
	group                        service.Group
	account                      service.Account
	groupUpdates, accountUpdates int
	lastGroupUpdate              *service.UpdateGroupInput
	failAccount                  bool
}

func (a *catalogAdmin) GetGroup(context.Context, int64) (*service.Group, error) { return &a.group, nil }
func (a *catalogAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	return &a.account, nil
}
func (a *catalogAdmin) UpdateGroup(_ context.Context, _ int64, input *service.UpdateGroupInput) (*service.Group, error) {
	a.lastGroupUpdate = input
	if input.Platform != "" {
		a.group.Platform = input.Platform
	}
	if input.ModelAllowlist != nil {
		a.group.ModelAllowlist = *input.ModelAllowlist
	}
	a.group.Status = input.Status
	a.groupUpdates++
	return &a.group, nil
}
func (a *catalogAdmin) UpdateAccount(_ context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	if a.failAccount {
		return nil, errors.New("retry cost update")
	}
	require.Equal(a.t, "keep", input.Credentials["api_key"])
	require.Equal(a.t, "preserved", input.Credentials["custom"])
	_, hasMapping := input.Credentials["model_mapping"]
	require.False(a.t, hasMapping)
	require.Equal(a.t, true, input.Extra["openai_passthrough"])
	require.Equal(a.t, "preserved", input.Extra["custom"])
	require.Equal(a.t, []string{}, input.Extra[service.MoshuResellerModelSnapshotExtraKey])
	require.Equal(a.t, a.account.Status, input.Status)
	require.Equal(a.t, a.account.GroupIDs, *input.GroupIDs)
	a.account.RateMultiplier = input.RateMultiplier
	a.account.Credentials = input.Credentials
	a.account.Extra = input.Extra
	a.account.Status = input.Status
	a.account.GroupIDs = *input.GroupIDs
	a.accountUpdates++
	return &a.account, nil
}

func TestApplyCatalogPreservesSalesAndRetriesOnlyChangedFields(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	rate := 1.0
	admin := &catalogAdmin{t: t, failAccount: true,
		group:   service.Group{ID: 10, Name: "My sales name", Platform: "openai", Status: "disabled", RateMultiplier: 0, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"old-model"}}},
		account: service.Account{ID: 20, Name: "Keep account", Platform: "openai", Type: "apikey", Status: "active", GroupIDs: []int64{10}, Credentials: map[string]any{"api_key": "keep", "custom": "preserved", "model_mapping": map[string]any{"old-model": "old-model"}}, Extra: map[string]any{"custom": "preserved"}, RateMultiplier: &rate},
	}
	sut := NewService(db, nil, admin)
	for run := 0; run < 3; run++ {
		mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(true, 0, 10, 20))
		err := sut.applyCatalogConfiguration(context.Background())
		if run == 0 {
			require.ErrorContains(t, err, "retry cost update")
			admin.failAccount = false
		} else {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 0, admin.groupUpdates)
	require.Equal(t, 1, admin.accountUpdates)
	require.Equal(t, 0.0, admin.group.RateMultiplier)
	require.Equal(t, "My sales name", admin.group.Name)
	require.Equal(t, "disabled", admin.group.Status)
	require.True(t, admin.group.ModelAllowlist.Enabled)
	require.Equal(t, []string{"old-model"}, admin.group.ModelAllowlist.Models)
	require.Equal(t, 5.0, *admin.account.RateMultiplier)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestWithProductModelSnapshot(t *testing.T) {
	extra, changed := withProductModelSnapshot(map[string]any{"custom": "preserved"}, []string{" kimi-k3 ", "kimi-k2.6"})
	require.True(t, changed)
	require.Equal(t, "preserved", extra["custom"])
	require.Equal(t, []string{"kimi-k3", "kimi-k2.6"}, extra[service.MoshuResellerModelSnapshotExtraKey])

	unchanged, changed := withProductModelSnapshot(extra, []string{"kimi-k3", "kimi-k2.6"})
	require.False(t, changed)
	require.Equal(t, extra, unchanged)
}

func TestApplyCatalogResetsWhitelistWhenPlatformChanges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	admin := &catalogAdmin{t: t, group: service.Group{
		ID: 10, Platform: service.PlatformAnthropic, Status: service.StatusActive,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"claude-sonnet-4"}},
	}}
	mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(sqlmock.NewRows([]string{
		"id", "remote", "code", "name", "platform", "group", "authorized", "selected", "cost", "sales", "version", "models", "capabilities", "credential", "local_group", "local_account", "effective", "cost_override",
	}).AddRow(3, 7, "deepseek", "DeepSeek", service.PlatformDeepseek, 9, true, true, 0.3, 1.0, 1,
		[]byte(`["deepseek-v4.1-flash"]`), []byte(`{}`), "test-key", 10, nil, time.Now(), nil))

	err = NewService(db, nil, admin).applyCatalogConfiguration(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, admin.groupUpdates)
	require.NotNil(t, admin.lastGroupUpdate)
	require.Equal(t, service.PlatformDeepseek, admin.lastGroupUpdate.Platform)
	require.NotNil(t, admin.lastGroupUpdate.ModelAllowlist)
	require.False(t, admin.lastGroupUpdate.ModelAllowlist.Enabled)
	require.Empty(t, admin.lastGroupUpdate.ModelAllowlist.Models)
	require.NoError(t, mock.ExpectationsWereMet())
}
