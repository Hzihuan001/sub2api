package handler

import (
	"encoding/json"
	"errors"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func keyModelListingRestricted(c *gin.Context) bool {
	key, _ := middleware2.GetAPIKeyFromContext(c)
	return key.KeyModelPolicyEnabled()
}

// sourceModelCatalogueETag only validates an unchanged source representation.
// A restricted key validates the final, narrowed catalogue instead; a source 304
// would otherwise leave no body on which to enforce the key policy.
func sourceModelCatalogueETag(c *gin.Context) string {
	if keyModelListingRestricted(c) || c.Param("model") != "" {
		return ""
	}
	return c.GetHeader("If-None-Match")
}

// filterKeyModelCatalogue only removes entries from the already selected group
// and provider catalogue. It never treats the key allowlist as a model source,
// resolves aliases, or changes metadata on retained entries.
func filterKeyModelCatalogue(key *service.APIKey, body []byte) ([]byte, error) {
	if !key.KeyModelPolicyEnabled() {
		return body, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	found := false
	for _, collection := range []struct{ field, id string }{{"data", "id"}, {"models", "slug"}} {
		raw, exists := envelope[collection.field]
		if !exists {
			continue
		}
		found = true
		var entries []json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
		filtered := make([]json.RawMessage, 0, len(entries))
		for _, entry := range entries {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(entry, &fields); err != nil {
				return nil, err
			}
			var id string
			if err := json.Unmarshal(fields[collection.id], &id); err != nil || id == "" {
				return nil, errors.New("invalid model catalogue ID")
			}
			if key.AllowsPublicModel(id) {
				filtered = append(filtered, entry)
			}
		}
		encoded, err := json.Marshal(filtered)
		if err != nil {
			return nil, err
		}
		envelope[collection.field] = encoded
	}
	if !found {
		return nil, errors.New("missing model catalogue collection")
	}
	return json.Marshal(envelope)
}

func keyFilteredModelResponse(c *gin.Context, source *service.OpenAIModelsResponse) (*service.OpenAIModelsResponse, error) {
	key, _ := middleware2.GetAPIKeyFromContext(c)
	if !key.KeyModelPolicyEnabled() {
		return source, nil
	}
	if source == nil || source.NotModified {
		return nil, errors.New("missing model catalogue body")
	}
	body, err := filterKeyModelCatalogue(key, source.Body)
	if err != nil {
		return nil, err
	}
	// Source responses may be shared by discovery caches. Do not mutate them.
	filtered := *source
	filtered.Body = body
	filtered.ETag = service.CodexModelsManifestETag(body)
	filtered.NotModified = false
	if c.Param("model") == "" && service.CodexModelsManifestETagMatches(c.GetHeader("If-None-Match"), filtered.ETag) {
		filtered.Body = nil
		filtered.NotModified = true
	}
	return &filtered, nil
}
