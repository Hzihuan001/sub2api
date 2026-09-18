package service

import (
	"context"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func requireMoshuResellerGuardError(t *testing.T, err error) {
	t.Helper()
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "MOSHU_RESELLER_MANAGED_ACCOUNT", appErr.Reason)
}

type moshuResellerGuardRepo struct {
	*upstreamBillingProbeAccountRepo
}

func (r *moshuResellerGuardRepo) ListShadowsByParent(context.Context, int64) ([]*Account, error) {
	return nil, nil
}

func (r *moshuResellerGuardRepo) BindGroups(context.Context, int64, []int64) error { return nil }

func (r *moshuResellerGuardRepo) Delete(_ context.Context, id int64) error {
	delete(r.accounts, id)
	return nil
}

func newMoshuResellerGuardRepo() *moshuResellerGuardRepo {
	return &moshuResellerGuardRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		7: {
			ID: 7, Name: "managed", Platform: PlatformDeepseek, Type: AccountTypeAPIKey,
			Credentials: map[string]any{
				"api_key": "remote-key", "base_url": "https://parent.example/v1",
				"model_mapping": map[string]any{"deepseek-v4": "deepseek-v4"},
			},
			Extra:  map[string]any{"moshu_reseller_managed": true, "openai_passthrough": true},
			Status: StatusActive,
		},
	}}}
}

func TestUpdateAccountRejectsProtectedMoshuResellerFields(t *testing.T) {
	repo := newMoshuResellerGuardRepo()
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{
		Credentials: map[string]any{"model_mapping": map[string]any{"deepseek-v4": "other"}},
	})
	requireMoshuResellerGuardError(t, err)

	_, err = svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{Platform: PlatformAnthropic})
	requireMoshuResellerGuardError(t, err)

	groupIDs := []int64{9}
	_, err = svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{GroupIDs: &groupIDs})
	requireMoshuResellerGuardError(t, err)

	err = svc.UpdateAccountExtra(context.Background(), 7, map[string]any{MoshuResellerModelSnapshotExtraKey: []string{"other"}})
	requireMoshuResellerGuardError(t, err)

	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{7}, Credentials: map[string]any{"base_url": "https://attacker.example/v1"},
	})
	requireMoshuResellerGuardError(t, err)

	err = svc.DeleteAccount(context.Background(), 7)
	requireMoshuResellerGuardError(t, err)
	_, existsErr := repo.GetByID(context.Background(), 7)
	require.NoError(t, existsErr, "protected account must remain after rejected delete")
}

func TestUpdateAccountAllowsUnrelatedManagedEditAndInternalSync(t *testing.T) {
	repo := newMoshuResellerGuardRepo()
	svc := &adminServiceImpl{accountRepo: repo}

	updated, err := svc.UpdateAccount(context.Background(), 7, &UpdateAccountInput{Name: "renamed"})
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.Name)

	updated, err = svc.UpdateAccount(WithMoshuResellerSync(context.Background()), 7, &UpdateAccountInput{
		Credentials: map[string]any{"api_key": "rotated", "base_url": "https://parent.example/v2"},
	})
	require.NoError(t, err)
	require.Equal(t, "rotated", updated.Credentials["api_key"])
	require.Equal(t, "https://parent.example/v2", updated.Credentials["base_url"])
}
