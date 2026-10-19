package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func requestKeyModelDirectory(t *testing.T, handler gin.HandlerFunc, key *service.APIKey, model, etag, forcedPlatform string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Request.Header.Set("If-None-Match", etag)
	if model != "" {
		c.Params = gin.Params{{Key: "model", Value: model}}
	}
	c.Set(string(middleware2.ContextKeyAPIKey), key)
	if forcedPlatform != "" {
		c.Set(string(middleware2.ContextKeyForcePlatform), forcedPlatform)
	}
	handler(c)
	return rec
}

func TestKeyModelListingFilterPreservesEnvelopeAndMetadata(t *testing.T) {
	key := &service.APIKey{KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a", "absent"}}}
	for _, tc := range []struct{ body, want string }{
		{`{"object":"list","next":"cursor","data":[{"id":"model-a","ID":"metadata-not-id","extra":{"context":999}},{"id":"model-b"},{"id":"MODEL-A"}]}`, `{"object":"list","next":"cursor","data":[{"id":"model-a","ID":"metadata-not-id","extra":{"context":999}}]}`},
		{`{"version":"v1","models":[{"slug":"model-a","description":"description","context_window":999},{"slug":"model-b"}]}`, `{"version":"v1","models":[{"slug":"model-a","description":"description","context_window":999}]}`},
		{`{"data":[{"id":"model-b"}],"models":[{"slug":"model-b"}]}`, `{"data":[],"models":[]}`},
	} {
		got, err := filterKeyModelCatalogue(key, []byte(tc.body))
		require.NoError(t, err)
		require.JSONEq(t, tc.want, string(got))
	}
	for _, body := range []string{`{}`, `null`, `{"data":{}}`, `{"data":[{"ID":"model-a"}]}`, `{"models":[{"slug":42}]}`, `{"models":[null]}`} {
		_, err := filterKeyModelCatalogue(key, []byte(body))
		require.Error(t, err, body)
	}
	for _, key := range []*service.APIKey{nil, {}, {KeyModelPolicy: &service.KeyModelPolicy{Enabled: false, Models: []string{"not-selected"}}}} {
		body := []byte("{ \"data\": [{\"id\":\"model-a\"}] }")
		got, err := filterKeyModelCatalogue(key, body)
		require.NoError(t, err)
		require.Equal(t, body, got, "unrestricted representation remains byte-for-byte unchanged")
	}
}

func TestKeyModelListingOrdinaryAndRetrieveNarrowExistingCatalogue(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformGrok, service.PlatformTypeSafe, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			group := &service.Group{ID: 91, Platform: platform, ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"model-a", "model-b"}}}
			accountPlatform := platform
			if platform == service.PlatformComposite {
				accountPlatform = service.PlatformOpenAI
			}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{
				group.ID: {{ID: 1, Platform: accountPlatform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
					Credentials: map[string]any{"model_mapping": map[string]any{"model-a": "source-a", "model-b": "source-b", "group-hidden": "source-hidden"}}}},
			}})
			key := &service.APIKey{GroupID: &group.ID, Group: group, KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a", "group-hidden", "absent"}}}
			list := requestKeyModelDirectory(t, h.Models, key, "", "", "")
			require.Equal(t, http.StatusOK, list.Code, list.Body.String())
			var got gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(list.Body.Bytes(), &got))
			require.Equal(t, []string{"model-a"}, modelIDsForTest(got.Data))
			require.NotContains(t, list.Body.String(), "source-a")
			retrieved := requestKeyModelDirectory(t, h.Models, key, "model-a", list.Header().Get("ETag"), "")
			require.Equal(t, http.StatusOK, retrieved.Code, retrieved.Body.String())
			var envelope struct {
				Data []json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(list.Body.Bytes(), &envelope))
			require.JSONEq(t, string(envelope.Data[0]), retrieved.Body.String())
			for _, hidden := range []string{"model-b", "group-hidden", "absent", "MODEL-A"} {
				rec := requestKeyModelDirectory(t, h.Models, key, hidden, "", "")
				require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), `"code":"model_not_found"`)
			}
			key.KeyModelPolicy.Models = []string{"absent"}
			empty := requestKeyModelDirectory(t, h.Models, key, "", "", "")
			require.Equal(t, http.StatusOK, empty.Code)
			require.Contains(t, empty.Body.String(), `"data":[]`)
		})
	}
}

func TestKeyModelListingForcedProviderStillApplies(t *testing.T) {
	group := &service.Group{ID: 92, Platform: service.PlatformComposite}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{group.ID: {
		{ID: 1, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"openai-only": "source"}}},
		{ID: 2, Platform: service.PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-only": "source"}}},
	}}})
	key := &service.APIKey{GroupID: &group.ID, Group: group, KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"openai-only", "gemini-only"}}}
	got := requestKeyModelDirectory(t, h.Models, key, "", "", service.PlatformGemini)
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	require.Contains(t, got.Body.String(), "gemini-only")
	require.NotContains(t, got.Body.String(), "openai-only")
}

func TestKeyModelListingStaticFallbackOnlyNarrows(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic, service.PlatformGemini, service.PlatformGrok, service.PlatformTypeSafe, service.PlatformComposite} {
		t.Run(platform, func(t *testing.T) {
			group := &service.Group{ID: 96, Platform: platform}
			h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{}})
			key := &service.APIKey{GroupID: &group.ID, Group: group}
			unrestricted := requestKeyModelDirectory(t, h.Models, key, "", "", "")
			require.Equal(t, http.StatusOK, unrestricted.Code, unrestricted.Body.String())
			var catalogue struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(unrestricted.Body.Bytes(), &catalogue))
			require.NotEmpty(t, catalogue.Data)
			model := catalogue.Data[0].ID
			key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{model, "never-in-source"}}
			filtered := requestKeyModelDirectory(t, h.Models, key, "", "", "")
			require.Equal(t, http.StatusOK, filtered.Code, filtered.Body.String())
			var got gatewayModelsResponseForTest
			require.NoError(t, json.Unmarshal(filtered.Body.Bytes(), &got))
			require.Equal(t, []string{model}, modelIDsForTest(got.Data))
			require.Equal(t, http.StatusNotFound, requestKeyModelDirectory(t, h.Models, key, "never-in-source", "", "").Code)
		})
	}
}

func TestKeyModelListingGeneratedCodexUsesFilteredETag(t *testing.T) {
	group := &service.Group{ID: 93, Platform: service.PlatformDeepseek}
	h := newGatewayModelsHandlerForTest(&gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{group.ID: {
		{ID: 1, Platform: group.Platform, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"model_mapping": map[string]any{"deepseek-model-a": "a", "deepseek-model-b": "b"}}},
	}}})
	key := &service.APIKey{GroupID: &group.ID, Group: group}
	unrestricted := requestKeyModelDirectory(t, h.CodexModels, key, "", "", "")
	require.Equal(t, http.StatusOK, unrestricted.Code, unrestricted.Body.String())
	key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"deepseek-model-a", "absent"}}
	first := requestKeyModelDirectory(t, h.CodexModels, key, "", unrestricted.Header().Get("ETag"), "")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Equal(t, []string{"deepseek-model-a"}, codexHandlerManifestSlugs(t, first))
	require.NotEqual(t, unrestricted.Header().Get("ETag"), first.Header().Get("ETag"))
	second := requestKeyModelDirectory(t, h.CodexModels, key, "", first.Header().Get("ETag"), "")
	require.Equal(t, http.StatusNotModified, second.Code, second.Body.String())
	require.Empty(t, second.Body.String())
}

func TestKeyModelListingOpenAICodexAllDiscoveryBranches(t *testing.T) {
	for _, mode := range []string{"ordinary", "pinned", "fallback", "configured"} {
		t.Run(mode, func(t *testing.T) {
			account := newPinnedCodexAccount(1, service.StatusActive, true, false)
			if mode == "configured" {
				account.Credentials["model_mapping"] = map[string]any{"model-a": "source-a", "model-b": "source-b"}
			}
			upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{1: `{"models":[{"slug":"model-a"},{"slug":"model-b"}]}`}}
			h := newPinnedCodexTestHandler([]service.Account{account}, upstream, 3)
			group := &service.Group{ID: 94, Platform: service.PlatformOpenAI}
			if mode == "pinned" || mode == "fallback" {
				group.CodexModelsManifestConfig = service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{1}}
			}
			if mode == "fallback" {
				group.CodexModelsManifestConfig.AccountIDs = []int64{999}
				group.CodexModelsManifestConfig.FallbackToScheduler = true
			}
			key := &service.APIKey{GroupID: &group.ID, Group: group}
			unrestricted := requestKeyModelDirectory(t, h.CodexModels, key, "", "", "")
			require.Equal(t, http.StatusOK, unrestricted.Code, unrestricted.Body.String())
			key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a", "absent"}}
			first := requestKeyModelDirectory(t, h.CodexModels, key, "", unrestricted.Header().Get("ETag"), "")
			require.Equal(t, http.StatusOK, first.Code, first.Body.String())
			require.Equal(t, []string{"model-a"}, codexHandlerManifestSlugs(t, first))
			second := requestKeyModelDirectory(t, h.CodexModels, key, "", first.Header().Get("ETag"), "")
			require.Equal(t, http.StatusNotModified, second.Code, second.Body.String())
			require.Empty(t, second.Body.String())
			key.KeyModelPolicy.Models = []string{"model-b"}
			changed := requestKeyModelDirectory(t, h.CodexModels, key, "", first.Header().Get("ETag"), "")
			require.Equal(t, http.StatusOK, changed.Code, changed.Body.String())
			require.Equal(t, []string{"model-b"}, codexHandlerManifestSlugs(t, changed))
		})
	}
}

func TestKeyModelListingPinnedOrdinaryAndSingle(t *testing.T) {
	upstream := &codexModelsPinnedHTTPUpstream{bodies: map[int64]string{1: `{"data":[{"id":"model-a","owned_by":"owner","extra":{"context":999}},{"id":"model-b"}]}`}}
	codex := newPinnedCodexTestHandler([]service.Account{newPinnedCodexAccount(1, service.StatusActive, true, false)}, upstream, 3)
	h := &GatewayHandler{openAIGatewayService: codex.gatewayService, maxAccountSwitches: 3}
	group := &service.Group{ID: 95, Platform: service.PlatformOpenAI, CodexModelsManifestConfig: service.GroupCodexModelsManifestConfig{Enabled: true, AccountIDs: []int64{1}}}
	key := &service.APIKey{GroupID: &group.ID, Group: group}
	unrestricted := requestKeyModelDirectory(t, h.Models, key, "", "", "")
	require.Equal(t, http.StatusOK, unrestricted.Code, unrestricted.Body.String())
	key.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a"}}
	first := requestKeyModelDirectory(t, h.Models, key, "", unrestricted.Header().Get("ETag"), "")
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	require.Contains(t, first.Body.String(), `"owned_by":"owner"`)
	require.Contains(t, first.Body.String(), `"context":999`)
	require.NotContains(t, first.Body.String(), "model-b")
	second := requestKeyModelDirectory(t, h.Models, key, "", first.Header().Get("ETag"), "")
	require.Equal(t, http.StatusNotModified, second.Code, second.Body.String())
	allowed := requestKeyModelDirectory(t, h.Models, key, "model-a", first.Header().Get("ETag"), "")
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	require.Empty(t, allowed.Header().Get("ETag"))
	denied := requestKeyModelDirectory(t, h.Models, key, "model-b", first.Header().Get("ETag"), "")
	require.Equal(t, http.StatusNotFound, denied.Code, denied.Body.String())
}

func TestKeyModelListingResponseDoesNotMutateSourceAndRejectsMissingBody(t *testing.T) {
	key := &service.APIKey{KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: []string{"model-a"}}}
	source := &service.OpenAIModelsResponse{Body: []byte(`{"models":[{"slug":"model-a"},{"slug":"model-b"}]}`), ETag: "source-etag"}
	original := string(source.Body)
	got := requestKeyModelDirectory(t, func(c *gin.Context) { writeOpenAIModelsResponse(c, source) }, key, "", "", "")
	require.Equal(t, http.StatusOK, got.Code)
	require.Equal(t, original, string(source.Body))
	require.Equal(t, "source-etag", source.ETag)
	require.False(t, source.NotModified)
	missing := requestKeyModelDirectory(t, func(c *gin.Context) { writeOpenAIModelsResponse(c, &service.OpenAIModelsResponse{NotModified: true}) }, key, "", "source-etag", "")
	require.Equal(t, http.StatusBadGateway, missing.Code, missing.Body.String())
}
