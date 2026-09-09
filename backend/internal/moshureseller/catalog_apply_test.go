package moshureseller

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

type catalogAdmin struct {
	service.AdminService
	t                            *testing.T
	group                        service.Group
	account                      service.Account
	groupUpdates, accountUpdates int
	failAccount                  bool
}

func (a *catalogAdmin) GetGroup(context.Context, int64) (*service.Group, error) { return &a.group, nil }
func (a *catalogAdmin) GetAccount(context.Context, int64) (*service.Account, error) {
	return &a.account, nil
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
