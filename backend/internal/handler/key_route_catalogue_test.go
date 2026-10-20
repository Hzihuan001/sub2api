package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type keyCatalogueSourceStub struct {
	candidates []service.KeyRouteCatalogueCandidate
	err        error
	calls      int
}

func (s *keyCatalogueSourceStub) CatalogueCandidates(context.Context, *service.APIKey) ([]service.KeyRouteCatalogueCandidate, error) {
	s.calls++
	return s.candidates, s.err
}

func requestKeyRouteCatalogue(t *testing.T, h *GatewayHandler, key *service.APIKey, source *keyCatalogueSourceStub, path, model, etag, forced string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(r)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	c.Request.Header.Set("If-None-Match", etag)
	if model != "" {
		c.Params = gin.Params{{Key: "model", Value: model}}
	}
	c.Set(string(servermiddleware.ContextKeyAPIKey), key)
	if source != nil {
		c.Set(keyRouteCatalogueSource, source)
	}
	if forced != "" {
		c.Set(string(servermiddleware.ContextKeyForcePlatform), forced)
	}
	h.Models(c)
	require.Equal(t, etag, c.Request.Header.Get("If-None-Match"), "native capture must not mutate request headers")
	require.Same(t, key, c.MustGet(string(servermiddleware.ContextKeyAPIKey)))
	return r
}

func keyCatalogueTestFixture(t *testing.T) (*GatewayHandler, *service.APIKey, *keyCatalogueSourceStub) {
	t.Helper()
	primary := &service.Group{ID: 201, Platform: service.PlatformOpenAI, Status: service.StatusActive}
	backup := &service.Group{ID: 202, Platform: service.PlatformDeepseek, Status: service.StatusActive}
	third := &service.Group{ID: 203, Platform: service.PlatformKimi, Status: service.StatusActive}
	modelMap := func(ids ...string) map[string]any {
		mapping := make(map[string]any)
		for _, id := range ids {
			mapping[id] = "private-upstream-" + id
		}
		return map[string]any{"model_mapping": mapping}
	}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
		primary.ID: {{ID: 1, Platform: primary.Platform, Credentials: modelMap("gpt-4o-mini", "shared-model")}},
		backup.ID:  {{ID: 2, Platform: backup.Platform, Credentials: modelMap("deepseek-v4.1-flash", "shared-model")}},
		third.ID:   {{ID: 3, Platform: third.Platform, Credentials: modelMap("kimi-test")}},
	}})
	key := &service.APIKey{ID: 901, User: &service.User{ID: 51}, GroupID: &primary.ID, Group: primary, BackupGroupIDs: []int64{backup.ID, third.ID}}
	source := &keyCatalogueSourceStub{candidates: []service.KeyRouteCatalogueCandidate{
		{Group: primary, ModelIDs: []string{"gpt-4o-mini", "shared-model"}},
		{Group: backup, ModelIDs: []string{"deepseek-v4.1-flash", "shared-model"}},
		{Group: third, ModelIDs: []string{"kimi-test"}},
	}}
	return h, key, source
}

func keyCatalogueEntries(t *testing.T, r *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, r.Code, r.Body.String())
	var body struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &body))
	require.Equal(t, "list", body.Object)
	return body.Data
}

func TestKeyRouteCatalogueUnionPriorityMetadataAndDetails(t *testing.T) {
	h, key, source := keyCatalogueTestFixture(t)
	before, err := json.Marshal(key)
	require.NoError(t, err)
	r := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "old-source-tag", "")
	entries := keyCatalogueEntries(t, r)
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry["id"].(string))
		detail := requestKeyRouteCatalogue(t, h, key, source, "/v1/models/"+entry["id"].(string), entry["id"].(string), r.Header().Get("ETag"), "")
		require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
		encoded, err := json.Marshal(entry)
		require.NoError(t, err)
		require.JSONEq(t, string(encoded), detail.Body.String())
	}
	require.Equal(t, []string{"gpt-4o-mini", "shared-model", "deepseek-v4.1-flash", "kimi-test"}, ids)
	require.Equal(t, "openai", entries[1]["owned_by"], "duplicate ID keeps primary metadata")
	require.NotContains(t, r.Body.String(), "private-upstream-")
	after, err := json.Marshal(key)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	missing := requestKeyRouteCatalogue(t, h, key, source, "/models/missing", "missing", "", "")
	require.Equal(t, http.StatusNotFound, missing.Code)
	require.Contains(t, missing.Body.String(), "model_not_found")
}

func TestKeyRouteCatalogueFiltersAndFinalETag(t *testing.T) {
	h, key, source := keyCatalogueTestFixture(t)
	first := requestKeyRouteCatalogue(t, h, key, source, "/models", "", "", "")
	tag := first.Header().Get("ETag")
	require.NotEmpty(t, tag)
	unchanged := requestKeyRouteCatalogue(t, h, key, source, "/models", "", tag, "")
	require.Equal(t, http.StatusNotModified, unchanged.Code)
	require.Empty(t, unchanged.Body.String())
	key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"deepseek-v4.1-flash", "not-in-any-group"}}
	filtered := requestKeyRouteCatalogue(t, h, key, source, "/models", "", tag, "")
	entries := keyCatalogueEntries(t, filtered)
	require.Len(t, entries, 1)
	require.Equal(t, "deepseek-v4.1-flash", entries[0]["id"])
	require.NotEqual(t, tag, filtered.Header().Get("ETag"))
	cached := requestKeyRouteCatalogue(t, h, key, source, "/models", "", filtered.Header().Get("ETag"), "")
	require.Equal(t, http.StatusNotModified, cached.Code)
	source.candidates[1].Group.ModelAllowlist = service.GroupModelAllowlist{Enabled: true, Models: []string{"shared-model"}}
	revoked := requestKeyRouteCatalogue(t, h, key, source, "/models", "", filtered.Header().Get("ETag"), "")
	require.Empty(t, keyCatalogueEntries(t, revoked))
	require.NotEqual(t, filtered.Header().Get("ETag"), revoked.Header().Get("ETag"))
}

func TestKeyRouteCatalogueEmptyPoolsNoSourceOrNativeFailure(t *testing.T) {
	h, key, source := keyCatalogueTestFixture(t)
	source.candidates = source.candidates[1:]
	backupOnly := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Len(t, keyCatalogueEntries(t, backupOnly), 3)
	source.candidates = nil
	empty := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Empty(t, keyCatalogueEntries(t, empty))
	require.NotContains(t, empty.Body.String(), "claude")
	source.err = errors.New("private database diagnostics")
	failed := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Equal(t, http.StatusServiceUnavailable, failed.Code)
	require.NotContains(t, failed.Body.String(), "private")
	missing := requestKeyRouteCatalogue(t, h, key, nil, "/v1/models", "", "", "")
	require.Equal(t, http.StatusServiceUnavailable, missing.Code)
	source.err = nil
	key.Group.CodexModelsManifestConfig.Enabled = true
	source.candidates = []service.KeyRouteCatalogueCandidate{{Group: key.Group, ModelIDs: []string{"gpt-4o-mini"}}}
	nativeFailure := requestKeyRouteCatalogue(t, h, key, source, "/v1/models", "", "", "")
	require.Equal(t, http.StatusBadGateway, nativeFailure.Code)
	require.NotContains(t, nativeFailure.Body.String(), `"data"`)
}

func TestKeyRouteCatalogueSingleKeyAndSpecialProtocolsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, path, forced string
		single             bool
	}{
		{"single", "/v1/models", "", true},
		{"codex", "/v1/models?client_version=1", "", false},
		{"forced", "/v1/models", service.PlatformGemini, false},
		{"native", "/antigravity/v1/models", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, key, source := keyCatalogueTestFixture(t)
			if tc.single {
				key.BackupGroupIDs = nil
			}
			r := requestKeyRouteCatalogue(t, h, key, source, tc.path, "", "", tc.forced)
			require.Equal(t, http.StatusOK, r.Code)
			require.Zero(t, source.calls)
			require.NotContains(t, r.Body.String(), "deepseek-v4.1-flash")
		})
	}
}

func TestKeyRouteCatalogueWriterBoundAndCancellation(t *testing.T) {
	w := &keyRouteCatalogueWriter{header: make(http.Header), status: http.StatusOK, size: -1}
	w.Header().Set("X-Private", "captured-only")
	_, err := w.WriteString(strings.Repeat("x", keyRouteCatalogueMaxBytes+1))
	require.Error(t, err)
	require.Empty(t, w.body.String())
	require.NotNil(t, w.err)
	_, _, err = w.Hijack()
	require.Error(t, err)
	h, key, source := keyCatalogueTestFixture(t)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil).WithContext(ctx)
	c.Set(string(servermiddleware.ContextKeyAPIKey), key)
	c.Set(keyRouteCatalogueSource, source)
	h.Models(c)
	require.Zero(t, source.calls)
	require.Empty(t, rec.Body.String())
}
