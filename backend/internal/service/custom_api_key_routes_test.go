//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type customRoutesGroupRepo struct {
	GroupRepository
	groups map[int64]*Group
}

func (r *customRoutesGroupRepo) GetByID(_ context.Context, id int64) (*Group, error) {
	group := r.groups[id]
	if group == nil {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func newCustomRoutesService() *APIKeyService {
	groups := make(map[int64]*Group)
	for id := int64(1); id <= 6; id++ {
		groups[id] = &Group{ID: id, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard}
	}
	return &APIKeyService{
		groupRepo: &customRoutesGroupRepo{groups: groups},
		userRepo:  &visibilityUserRepo{user: &User{ID: 7}},
	}
}

func TestCustomAPIKeyRoutesValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		primary int64
		backups []int64
		mutate  func(*APIKeyService, *User)
		valid   bool
	}{
		{name: "ordered four backups", primary: 1, backups: []int64{5, 3, 2, 4}, valid: true},
		{name: "legacy unbound", valid: true},
		{name: "legacy subscription", primary: 1, mutate: func(s *APIKeyService, _ *User) {
			s.groupRepo.(*customRoutesGroupRepo).groups[1].SubscriptionType = SubscriptionTypeSubscription
		}, valid: true},
		{name: "too many", primary: 1, backups: []int64{2, 3, 4, 5, 6}},
		{name: "missing primary", backups: []int64{2}},
		{name: "duplicate primary", primary: 1, backups: []int64{1}},
		{name: "duplicate backups", primary: 1, backups: []int64{2, 2}},
		{name: "invalid id", primary: 1, backups: []int64{-1}},
		{name: "missing group", primary: 1, backups: []int64{9}},
		{name: "disabled primary", primary: 1, backups: []int64{2}, mutate: func(s *APIKeyService, _ *User) {
			s.groupRepo.(*customRoutesGroupRepo).groups[1].Status = StatusDisabled
		}},
		{name: "disabled backup", primary: 1, backups: []int64{2}, mutate: func(s *APIKeyService, _ *User) {
			s.groupRepo.(*customRoutesGroupRepo).groups[2].Status = StatusDisabled
		}},
		{name: "subscription backup", primary: 1, backups: []int64{2}, mutate: func(s *APIKeyService, _ *User) {
			s.groupRepo.(*customRoutesGroupRepo).groups[2].SubscriptionType = SubscriptionTypeSubscription
		}},
		{name: "exclusive without access", primary: 1, backups: []int64{2}, mutate: func(s *APIKeyService, _ *User) { s.groupRepo.(*customRoutesGroupRepo).groups[2].IsExclusive = true }},
		{name: "restricted public", primary: 1, backups: []int64{2}, mutate: func(_ *APIKeyService, u *User) { u.RestrictPublicGroups = true; u.AllowedGroups = []int64{1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, owner := newCustomRoutesService(), &User{ID: 7}
			if tc.mutate != nil {
				tc.mutate(svc, owner)
			}
			var primary *int64
			if tc.primary != 0 {
				primary = &tc.primary
			}
			err := svc.validateAPIKeyRoutes(context.Background(), owner, primary, tc.backups)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCustomAPIKeyRoutesClaudeCodeOnlyCreateAndUpdate(t *testing.T) {
	for _, tc := range []struct {
		name         string
		restrictedID int64
		backups      []int64
		allowed      bool
	}{
		{name: "reject primary", restrictedID: 1, backups: []int64{2}},
		{name: "reject backup", restrictedID: 2, backups: []int64{2}},
		{name: "preserve single-group key", restrictedID: 1, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newCustomRoutesService()
			svc.groupRepo.(*customRoutesGroupRepo).groups[tc.restrictedID].ClaudeCodeOnly = true
			primary := int64(1)
			customKey := "sk-claude-only-route-test"
			createRepo := &createLimitAPIKeyRepoStub{apiKeyRepoStub: &apiKeyRepoStub{}}
			svc.apiKeyRepo = createRepo
			_, createErr := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
				Name: "route", GroupID: &primary, BackupGroupIDs: tc.backups, CustomKey: &customKey,
			})
			updateRepo := &updateFieldsAPIKeyRepoStub{key: &APIKey{ID: 1, UserID: 7, Key: customKey, GroupID: &primary, Status: StatusActive}}
			svc.apiKeyRepo = updateRepo
			_, updateErr := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: &primary, BackupGroupIDs: &tc.backups})
			if tc.allowed {
				require.NoError(t, createErr)
				require.NoError(t, updateErr)
				require.Len(t, createRepo.created, 1)
				require.Len(t, updateRepo.updateFields, 1)
			} else {
				require.Equal(t, "API_KEY_ROUTES_CLAUDE_ONLY_UNSUPPORTED", infraerrors.Reason(createErr))
				require.Equal(t, "API_KEY_ROUTES_CLAUDE_ONLY_UNSUPPORTED", infraerrors.Reason(updateErr))
				require.Empty(t, createRepo.created)
				require.Empty(t, updateRepo.updateFields)
			}
		})
	}
}

func TestCustomAPIKeyRoutesUpdatePreserveClearAndValidatePrimary(t *testing.T) {
	primary := int64(1)
	key := &APIKey{ID: 1, UserID: 7, Key: "key", GroupID: &primary, BackupGroupIDs: []int64{2, 3}, Status: StatusActive}
	svc, repo := newUpdateFieldsAPIKeyService(key)
	name := "renamed"
	got, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Name: &name})
	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, got.BackupGroupIDs)
	require.False(t, repo.updateFields[0].BackupGroupIDs)
	empty := []int64{}
	got, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{BackupGroupIDs: &empty})
	require.NoError(t, err)
	require.Empty(t, got.BackupGroupIDs)
	require.Equal(t, APIKeyUpdateFields{BackupGroupIDs: true}, repo.updateFields[1])
	configured := newCustomRoutesService()
	svc.groupRepo, svc.userRepo = configured.groupRepo, configured.userRepo
	duplicate := int64(2)
	_, err = svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{GroupID: &duplicate})
	require.Error(t, err)
	require.Len(t, repo.updateFields, 2, "invalid changes must not be persisted")
}

func TestCustomAPIKeyRoutesUpdateJSONContract(t *testing.T) {
	for _, input := range []string{`{}`, `{"backup_group_ids":null}`} {
		var req UpdateAPIKeyRequest
		require.NoError(t, json.Unmarshal([]byte(input), &req))
		require.Nil(t, req.BackupGroupIDs)
	}
	var req UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{"backup_group_ids":[]}`), &req))
	require.NotNil(t, req.BackupGroupIDs)
	require.Empty(t, *req.BackupGroupIDs)
}

func TestCustomAPIKeyRoutesAuthSnapshotCopiesBothDirections(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 7, BackupGroupIDs: []int64{3, 2}, User: &User{ID: 7}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	key.BackupGroupIDs[0] = 99
	require.Equal(t, []int64{3, 2}, snapshot.BackupGroupIDs)
	first := svc.snapshotToAPIKey("key", snapshot)
	first.BackupGroupIDs[0] = 88
	require.Equal(t, []int64{3, 2}, svc.snapshotToAPIKey("key", snapshot).BackupGroupIDs)
	_, ok, err := svc.applyAuthCacheEntry("key", &APIKeyAuthCacheEntry{Snapshot: &APIKeyAuthSnapshot{Version: 24}})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCustomAPIKeyRoutesAdminPermissionAndLegacyClear(t *testing.T) {
	primary := int64(1)
	key := &APIKey{ID: 1, UserID: 7, Key: "key", GroupID: &primary, BackupGroupIDs: []int64{2}, Status: StatusActive}
	repo := &apiKeyRepoStubForGroupUpdate{key: key}
	svc := newCustomRoutesService()
	svc.apiKeyRepo = repo
	groups := svc.groupRepo.(*customRoutesGroupRepo)
	groups.groups[3].IsExclusive = true
	grants := &userRepoStubForGroupUpdate{}
	admin := &adminServiceImpl{apiKeyRepo: repo, apiKeyService: svc, groupRepo: groups, userRepo: grants}
	newPrimary := int64(3)
	_, err := admin.AdminUpdateAPIKeyGroupID(context.Background(), key.ID, &newPrimary)
	require.ErrorIs(t, err, ErrGroupNotAllowed)
	require.False(t, grants.addGroupCalled, "multi-group admin edits must not grant group access")
	require.Nil(t, repo.updated)
	// Clearing routes makes this an ordinary single-group admin binding again.
	empty := []int64{}
	result, err := admin.AdminUpdateAPIKeyRoutes(context.Background(), key.ID, &newPrimary, &empty)
	require.NoError(t, err)
	require.True(t, result.AutoGrantedGroupAccess)
	require.True(t, grants.addGroupCalled)
	require.Empty(t, result.APIKey.BackupGroupIDs)
	require.Equal(t, newPrimary, *result.APIKey.GroupID)
}
