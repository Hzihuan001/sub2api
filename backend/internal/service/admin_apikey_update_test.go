//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newAdminKeyEditTestService() (*adminServiceImpl, *updateFieldsAPIKeyRepoStub) {
	primary := int64(1)
	native, repo := newUpdateFieldsAPIKeyService(&APIKey{ID: 9, UserID: 7, Key: "test", GroupID: &primary, Status: StatusActive, QuotaUsed: 4})
	native.userRepo = &visibilityUserRepo{user: &User{ID: 7}}
	native.groupRepo = newCustomRoutesService().groupRepo
	return &adminServiceImpl{apiKeyRepo: repo, apiKeyService: native, userRepo: native.userRepo, groupRepo: native.groupRepo}, repo
}

func TestAdminAPIKeyFullEditUsesNativeWriteMaskAndOwner(t *testing.T) {
	svc, repo := newAdminKeyEditTestService()
	name, status, primary := "renamed", StatusDisabled, int64(2)
	backups := []int64{3}
	quota := 99.0
	result, err := svc.AdminUpdateAPIKey(context.Background(), 9, UpdateAPIKeyRequest{
		Name: &name, Status: &status, GroupID: &primary, BackupGroupIDs: &backups,
		Quota: &quota, KeyModelPolicySet: true, KeyRouteFailurePolicySet: true,
	})
	require.NoError(t, err)
	require.Equal(t, int64(7), result.APIKey.UserID)
	require.Equal(t, name, result.APIKey.Name)
	require.Equal(t, []int64{3}, result.APIKey.BackupGroupIDs)
	require.Equal(t, 4.0, result.APIKey.QuotaUsed)
	require.Equal(t, []APIKeyUpdateFields{{Name: true, Status: true, GroupID: true, BackupGroupIDs: true, Quota: true, KeyModelPolicy: true, KeyRouteFailurePolicy: true}}, repo.updateFields)
}

func TestAdminAPIKeyGrantPlanRejectsInvalidRoutesWithoutWriting(t *testing.T) {
	for _, scenario := range []string{"duplicate", "subscription", "inactive", "invalid-ip", "invalid-policy", "grant-without-transaction"} {
		t.Run(scenario, func(t *testing.T) {
			svc, repo := newAdminKeyEditTestService()
			groups := svc.groupRepo.(*customRoutesGroupRepo).groups
			groups[2].IsExclusive = true
			primary := int64(2)
			backups := []int64{3}
			req := UpdateAPIKeyRequest{GroupID: &primary, BackupGroupIDs: &backups}
			switch scenario {
			case "duplicate":
				backups = []int64{2}
				req.BackupGroupIDs = &backups
			case "subscription":
				groups[3].SubscriptionType = SubscriptionTypeSubscription
			case "inactive":
				groups[3].Status = StatusDisabled
			case "invalid-ip":
				patterns := []string{"garbage"}
				req.IPWhitelist = &patterns
			case "invalid-policy":
				req.KeyModelPolicy = &KeyModelPolicy{Enabled: true}
			}
			_, err := svc.AdminUpdateAPIKey(context.Background(), 9, req)
			require.Error(t, err)
			require.Empty(t, repo.updateFields, "invalid edit never writes the key")
			require.Empty(t, svc.userRepo.(*visibilityUserRepo).user.AllowedGroups, "grant planning must not mutate the owner")
		})
	}
}

func TestAdminAPIKeyGrantPlanAllowsAuthorizedStandardGroups(t *testing.T) {
	svc, repo := newAdminKeyEditTestService()
	primary, backups := int64(2), []int64{3}
	svc.groupRepo.(*customRoutesGroupRepo).groups[2].IsExclusive = true
	svc.userRepo.(*visibilityUserRepo).user.AllowedGroups = []int64{2}
	result, err := svc.AdminUpdateAPIKey(context.Background(), 9, UpdateAPIKeyRequest{GroupID: &primary, BackupGroupIDs: &backups})
	require.NoError(t, err)
	require.False(t, result.AutoGrantedGroupAccess)
	require.Len(t, repo.updateFields, 1)
}

func TestAdminAPIKeyFullEditCanUnbindAndClearBackups(t *testing.T) {
	svc, repo := newAdminKeyEditTestService()
	zero, empty, name := int64(0), []int64{}, "unbound"
	result, err := svc.AdminUpdateAPIKey(context.Background(), 9, UpdateAPIKeyRequest{Name: &name, GroupID: &zero, BackupGroupIDs: &empty})
	require.NoError(t, err)
	require.Nil(t, result.APIKey.GroupID)
	require.Empty(t, result.APIKey.BackupGroupIDs)
	require.Equal(t, []APIKeyUpdateFields{{Name: true, GroupID: true, BackupGroupIDs: true}}, repo.updateFields)
}
