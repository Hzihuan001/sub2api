package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func keyCataloguePinnedFixture(t *testing.T, upstream service.HTTPUpstream) (*GatewayHandler, *service.APIKey, *keyCatalogueSourceStub) {
	t.Helper()
	primary := &service.Group{ID: 301, Platform: service.PlatformOpenAI, Status: service.StatusActive,
		CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{1}}}
	backup := &service.Group{ID: 302, Platform: service.PlatformDeepseek, Status: service.StatusActive}
	accounts := []service.Account{newPinnedCodexAccount(1, service.StatusActive, true, false), newPinnedCodexAccount(2, service.StatusActive, true, false)}
	pinned := service.NewOpenAIGatewayService(
		codexModelsFailoverAccountRepo{accounts: accounts},
		nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil, nil, nil, nil, nil,
		upstream,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		backup.ID: {{ID: 3, Platform: service.PlatformDeepseek, Credentials: map[string]any{"model_mapping": map[string]any{
			"deepseek-visible": "private-deepseek", "shared-live": "private-shared",
		}}}},
	}})
	h.openAIGatewayService = pinned
	h.maxAccountSwitches = 3
	key := &service.APIKey{ID: 911, UserID: 61, User: &service.User{ID: 61}, GroupID: &primary.ID, Group: primary, BackupGroupIDs: []int64{backup.ID}}
	source := &keyCatalogueSourceStub{candidates: []service.KeyRouteCatalogueCandidate{
		{Group: primary, DefaultPlatforms: []string{service.PlatformOpenAI}},
		{Group: backup, ModelIDs: []string{"deepseek-visible", "shared-live"}},
	}}
	return h, key, source
}

func TestKeyRouteCataloguePinnedDynamicMetadataAndSourceETag(t *testing.T) {
	upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{1: `{"data":[{"id":"custom-live-text","owned_by":"live-provider","created":123,"provider_feature":{"supported":true}},{"id":"shared-live","owned_by":"primary-provider"}]}`}}
	h, key, source := keyCataloguePinnedFixture(t, upstream)
	single := *key
	single.BackupGroupIDs = nil
	native := requestKeyRouteCatalogue(t, h, &single, nil, "/v1/models", "", "", "")
	require.Equal(t, []string{"custom-live-text", "shared-live"}, ordinaryPinnedModelIDs(t, native))
	require.NotEmpty(t, native.Header().Get("ETag"))
	merged := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", native.Header().Get("ETag"), "")
	entries := keyCatalogueEntries(t, merged)
	require.Equal(t, []string{"custom-live-text", "shared-live", "deepseek-visible"}, ordinaryPinnedModelIDs(t, merged))
	require.Equal(t, "live-provider", entries[0]["owned_by"])
	require.EqualValues(t, 123, entries[0]["created"])
	require.Equal(t, map[string]any{"supported": true}, entries[0]["provider_feature"])
	require.Equal(t, "primary-provider", entries[1]["owned_by"], "primary metadata wins duplicate public IDs")
	require.NotEqual(t, native.Header().Get("ETag"), merged.Header().Get("ETag"), "source ETag cannot short-circuit the merged body")
	require.Equal(t, []int64{1}, upstream.accountIDs(), "native cache is reused without another upstream lookup")
	cached := requestKeyRouteCatalogue(t, h, key, source, "/models", "", merged.Header().Get("ETag"), "")
	require.Equal(t, http.StatusNotModified, cached.Code)
	require.Empty(t, cached.Body.String())
}

func TestKeyRouteCataloguePinnedRestrictedPoliciesAndFinalETag(t *testing.T) {
	upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{1: `{"data":[{"id":"custom-live-text","owned_by":"live-provider"},{"id":"custom-blocked-text"},{"id":"shared-live"}]}`}}
	h, key, source := keyCataloguePinnedFixture(t, upstream)
	key.Group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"custom-live-text", "shared-live"}}
	key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"custom-live-text", "deepseek-visible", "invented-live-text", "custom-blocked-text"}}
	filtered := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Equal(t, []string{"custom-live-text", "deepseek-visible"}, ordinaryPinnedModelIDs(t, filtered))
	require.NotContains(t, filtered.Body.String(), "invented-live-text", "a key allowlist is not a source of upstream models")
	require.NotContains(t, filtered.Body.String(), "custom-blocked-text", "dynamic discovery cannot bypass the group allowlist")
	tag := filtered.Header().Get("ETag")
	require.NotEmpty(t, tag)
	cached := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", tag, "")
	require.Equal(t, http.StatusNotModified, cached.Code)
	key.KeyModelPolicy.Models = []string{"deepseek-visible"}
	changed := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", tag, "")
	require.Equal(t, []string{"deepseek-visible"}, ordinaryPinnedModelIDs(t, changed))
	require.NotEqual(t, tag, changed.Header().Get("ETag"))
}

func TestKeyRouteCataloguePinnedDynamicIDsRequireFreshUnmappedEvidence(t *testing.T) {
	upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{1: `{"data":[{"id":"gpt-4o-mini"},{"id":"custom-live-text"}]}`}}
	h, key, source := keyCataloguePinnedFixture(t, upstream)
	source.candidates[0].DefaultPlatforms = nil
	source.candidates[0].ModelIDs = []string{"gpt-4o-mini"}
	merged := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Equal(t, []string{"gpt-4o-mini", "deepseek-visible", "shared-live"}, ordinaryPinnedModelIDs(t, merged))
	require.NotContains(t, merged.Body.String(), "custom-live-text", "pinned mode alone must not weaken the current account-source intersection")
}

func TestKeyRouteCataloguePinnedBackupDynamicModelsExcludeMedia(t *testing.T) {
	upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{
		1: `{"data":[{"id":"custom-live-text","owned_by":"primary"}]}`,
		2: `{"data":[{"id":"backup-live-text","owned_by":"backup"},{"id":"gpt-image-2"},{"id":"gemini-nano-banana-2.1"},{"id":"grok-imagine-video"}]}`,
	}}
	h, key, source := keyCataloguePinnedFixture(t, upstream)
	backup := source.candidates[1].Group
	backup.Platform = service.PlatformOpenAI
	backup.CodexModelsManifestConfig = service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{2}}
	source.candidates[1].ModelIDs = nil
	source.candidates[1].DefaultPlatforms = []string{service.PlatformOpenAI}
	merged := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Equal(t, []string{"custom-live-text", "backup-live-text"}, ordinaryPinnedModelIDs(t, merged))
	require.Equal(t, []int64{1, 2}, upstream.accountIDs())
}

type keyCatalogueCancelPinnedHTTPUpstream struct {
	service.HTTPUpstream
	base   *codexModelsPinnedHTTPUpstream
	cancel context.CancelFunc
	done   chan struct{}
}

func (u *keyCatalogueCancelPinnedHTTPUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	defer close(u.done)
	u.cancel()
	return u.base.Do(req, proxy, accountID, concurrency)
}

func TestKeyRouteCataloguePinnedCancellationDuringNativeCaptureWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	upstream := &keyCatalogueCancelPinnedHTTPUpstream{
		base:   &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{1: `{"data":[{"id":"custom-live-text"}]}`}},
		cancel: cancel, done: make(chan struct{}),
	}
	h, key, source := keyCataloguePinnedFixture(t, upstream)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil).WithContext(ctx)
	c.Set(string(servermiddleware.ContextKeyAPIKey), key)
	c.Set(keyRouteCatalogueSource, source)
	h.Models(c)
	select {
	case <-upstream.done:
	case <-time.After(time.Second):
		t.Fatal("synthetic pinned discovery did not execute")
	}
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.False(t, c.Writer.Written(), "cancellation during native discovery must not be translated into a synthetic 502")
	require.Empty(t, recorder.Body.String())
}
