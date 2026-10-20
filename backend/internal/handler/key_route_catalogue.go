package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const keyRouteCatalogueSource = "custom.key_route_catalogue_source"
const keyRouteCatalogueMaxBytes = 2 << 20

type keyRouteCatalogueSourceReader interface {
	CatalogueCandidates(context.Context, *service.APIKey) ([]service.KeyRouteCatalogueCandidate, error)
}

// writeKeyRouteCatalogue is a metadata-only adapter. Native handlers still own
// each group's representation; this module only narrows and merges them. It
// never selects/reserves an account, changes the key or invokes billing.
func (h *GatewayHandler) writeKeyRouteCatalogue(c *gin.Context) bool {
	key, _ := servermiddleware.GetAPIKeyFromContext(c)
	if key == nil || len(key.BackupGroupIDs) == 0 || !ordinaryKeyRouteCatalogue(c) {
		return false
	}
	if c.Request.Context().Err() != nil {
		return true
	}
	value, _ := c.Get(keyRouteCatalogueSource)
	source, ok := value.(keyRouteCatalogueSourceReader)
	if !ok {
		writeOpenAIModelsError(c, http.StatusServiceUnavailable, "api_error", "Multi-group model discovery is unavailable")
		return true
	}
	candidates, err := source.CatalogueCandidates(c.Request.Context(), key)
	if err != nil || len(candidates) > keyrouting.MaxBackupGroups+1 {
		writeOpenAIModelsError(c, http.StatusServiceUnavailable, "api_error", "Unable to load authorized model catalogues")
		return true
	}
	entries := make([]json.RawMessage, 0)
	seen := make(map[string]struct{})
	total := 0
	for _, candidate := range candidates {
		if c.Request.Context().Err() != nil {
			return true
		}
		if candidate.Group == nil {
			writeOpenAIModelsError(c, http.StatusServiceUnavailable, "api_error", "Invalid model catalogue candidate")
			return true
		}
		allowed := keyRouteCatalogueModelSet(key, candidate)
		if len(allowed) == 0 && !keyRouteCatalogueHasLiveSource(candidate) {
			continue
		}
		body, err := h.captureKeyRouteCatalogue(c, key, candidate.Group)
		if c.Request.Context().Err() != nil {
			return true
		}
		if err != nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Unable to read a configured model catalogue")
			return true
		}
		var envelope struct {
			Data []json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
			writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue")
			return true
		}
		for _, entry := range envelope.Data {
			var identity struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(entry, &identity); err != nil || identity.ID == "" {
				writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Invalid model catalogue entry")
				return true
			}
			if _, ok := allowed[identity.ID]; !ok && !keyRouteCatalogueAllowsLiveModel(key, candidate, identity.ID) {
				continue
			}
			if _, duplicate := seen[identity.ID]; duplicate {
				continue
			}
			total += len(entry)
			if total > keyRouteCatalogueMaxBytes {
				writeOpenAIModelsError(c, http.StatusBadGateway, "upstream_error", "Model catalogue exceeds the supported size")
				return true
			}
			seen[identity.ID] = struct{}{}
			entries = append(entries, entry)
		}
	}
	body, err := json.Marshal(struct {
		Object string            `json:"object"`
		Data   []json.RawMessage `json:"data"`
	}{Object: "list", Data: entries})
	if err != nil {
		writeOpenAIModelsError(c, http.StatusInternalServerError, "api_error", "Failed to encode model catalogue")
		return true
	}
	if c.Request.Context().Err() != nil {
		return true
	}
	if c.Param("model") != "" {
		writeRetrievedModel(c, body)
		return true
	}
	etag := service.CodexModelsManifestETag(body)
	writeOpenAIModelsResponse(c, &service.OpenAIModelsResponse{
		Body: body, ETag: etag,
		NotModified: !keyModelListingRestricted(c) && service.CodexModelsManifestETagMatches(c.GetHeader("If-None-Match"), etag),
	})
	return true
}

func ordinaryKeyRouteCatalogue(c *gin.Context) bool {
	if c == nil || c.Request == nil || c.Request.URL == nil || c.Request.Method != http.MethodGet || c.Query("client_version") != "" {
		return false
	}
	if _, forced := servermiddleware.GetForcePlatformFromContext(c); forced || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") {
		return false
	}
	path := c.Request.URL.Path
	for _, root := range []string{"/v1/models", "/models"} {
		if path == root || (strings.HasPrefix(path, root+"/") && !strings.Contains(strings.TrimPrefix(path, root+"/"), "/") && c.Param("model") != "") {
			return true
		}
	}
	return false
}

func keyRouteCatalogueModelSet(key *service.APIKey, candidate service.KeyRouteCatalogueCandidate) map[string]struct{} {
	models := append([]string(nil), candidate.ModelIDs...)
	for _, platform := range candidate.DefaultPlatforms {
		models = append(models, defaultModelIDsForPlatform(platform)...)
	}
	allowed := make(map[string]struct{}, len(models))
	for _, model := range models {
		if model == "" || !key.AllowsPublicModel(model) || (candidate.Group.ModelAllowlistEnabled() && !candidate.Group.ModelAllowlist.Allows(model)) {
			continue
		}
		if key.GroupID == nil || (candidate.Group.ID != *key.GroupID && !service.KeyRouteCatalogueBackupModelAllowed(model)) {
			continue
		}
		allowed[model] = struct{}{}
	}
	return allowed
}

// A configured pinned provider is itself a live source. With a fresh healthy
// unmapped account, its verified response may contain new models absent from
// the bundled defaults. Never generalize this exception to static fallbacks.
func keyRouteCatalogueAllowsLiveModel(key *service.APIKey, candidate service.KeyRouteCatalogueCandidate, model string) bool {
	group := candidate.Group
	if !keyRouteCatalogueHasLiveSource(candidate) ||
		!key.AllowsPublicModel(model) || (group.ModelAllowlistEnabled() && !group.ModelAllowlist.Allows(model)) ||
		(key.GroupID != nil && group.ID != *key.GroupID && !service.KeyRouteCatalogueBackupModelAllowed(model)) {
		return false
	}
	return true
}

func keyRouteCatalogueHasLiveSource(candidate service.KeyRouteCatalogueCandidate) bool {
	if candidate.Group.Platform != service.PlatformOpenAI || !candidate.Group.CodexModelsManifestConfig.Enabled {
		return false
	}
	for _, platform := range candidate.DefaultPlatforms {
		if platform == service.PlatformOpenAI {
			return true
		}
	}
	return false
}

func (h *GatewayHandler) captureKeyRouteCatalogue(parent *gin.Context, key *service.APIKey, group *service.Group) ([]byte, error) {
	child := parent.Copy()
	writer := &keyRouteCatalogueWriter{header: make(http.Header), status: http.StatusOK, size: -1}
	child.Writer = writer
	child.Params = nil // Read the full native list, even for /models/:model.
	ctx := context.WithValue(parent.Request.Context(), ctxkey.Group, group)
	child.Request = parent.Request.Clone(ctx)
	child.Request.Header.Del("If-None-Match") // No source 304 may hide its body.
	snapshot := *key
	snapshot.BackupGroupIDs = nil // No recursion and no routing side effects.
	groupSnapshot := *group
	snapshot.GroupID, snapshot.Group = &groupSnapshot.ID, &groupSnapshot
	child.Set(string(servermiddleware.ContextKeyAPIKey), &snapshot)
	h.Models(child)
	if writer.err != nil {
		return nil, writer.err
	}
	if writer.Status() != http.StatusOK {
		return nil, errors.New("native model catalogue failed")
	}
	return writer.body.Bytes(), nil
}

// A bounded, disconnected Gin writer prevents headers, errors or flushes from
// any native catalogue from reaching the actual response before validation.
type keyRouteCatalogueWriter struct {
	header http.Header
	body   bytes.Buffer
	status int
	size   int
	err    error
}

func (w *keyRouteCatalogueWriter) Header() http.Header { return w.header }
func (w *keyRouteCatalogueWriter) WriteHeader(status int) {
	if !w.Written() {
		w.status = status
	}
}
func (w *keyRouteCatalogueWriter) WriteHeaderNow() {
	if !w.Written() {
		w.size = 0
	}
}
func (w *keyRouteCatalogueWriter) Write(body []byte) (int, error) {
	w.WriteHeaderNow()
	if len(body) > keyRouteCatalogueMaxBytes-w.body.Len() {
		w.err = io.ErrShortBuffer
		return 0, w.err
	}
	n, err := w.body.Write(body)
	w.size += n
	return n, err
}
func (w *keyRouteCatalogueWriter) WriteString(body string) (int, error) { return w.Write([]byte(body)) }
func (w *keyRouteCatalogueWriter) Status() int                          { return w.status }
func (w *keyRouteCatalogueWriter) Size() int                            { return w.size }
func (w *keyRouteCatalogueWriter) Written() bool                        { return w.size >= 0 }
func (w *keyRouteCatalogueWriter) Flush()                               { w.WriteHeaderNow() }
func (w *keyRouteCatalogueWriter) CloseNotify() <-chan bool             { return nil }
func (w *keyRouteCatalogueWriter) Pusher() http.Pusher                  { return nil }
func (w *keyRouteCatalogueWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("model catalogue cannot hijack the connection")
}

var _ gin.ResponseWriter = (*keyRouteCatalogueWriter)(nil)
