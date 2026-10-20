//go:build unit

package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type catalogueIngressSelector struct {
	calls int
}

func (s *catalogueIngressSelector) Select(context.Context, *service.APIKey, service.KeyRoutingRequest) (*service.APIKey, keyrouting.Plan, error) {
	s.calls++
	return nil, keyrouting.Plan{}, nil
}

func TestKeyRouteCatalogueIngressScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, method, path string
		force, websocket   bool
		noBackup, noSource bool
		wantSource         bool
	}{
		{name: "ordinary list", method: http.MethodGet, path: "/v1/models", wantSource: true},
		{name: "root list alias", method: http.MethodGet, path: "/models", wantSource: true},
		{name: "ordinary detail", method: http.MethodGet, path: "/v1/models/backup-model", wantSource: true},
		{name: "root detail alias", method: http.MethodGet, path: "/models/backup-model", wantSource: true},
		{name: "empty client version is ordinary", method: http.MethodGet, path: "/models?client_version=", wantSource: true},
		{name: "Codex list manifest", method: http.MethodGet, path: "/v1/models?client_version=0.147.0"},
		{name: "Codex root manifest", method: http.MethodGet, path: "/models?client_version=0.147.0"},
		{name: "client version detail conservatively excluded", method: http.MethodGet, path: "/v1/models/backup-model?client_version=0.147.0"},
		{name: "forced provider list", method: http.MethodGet, path: "/v1/models", force: true},
		{name: "forced provider detail", method: http.MethodGet, path: "/models/backup-model", force: true},
		{name: "native Gemini list", method: http.MethodGet, path: "/v1beta/models"},
		{name: "native Gemini detail", method: http.MethodGet, path: "/v1beta/models/gemini-model"},
		{name: "Antigravity list", method: http.MethodGet, path: "/antigravity/models"},
		{name: "Antigravity v1 list", method: http.MethodGet, path: "/antigravity/v1/models"},
		{name: "Antigravity native detail", method: http.MethodGet, path: "/antigravity/v1beta/models/gemini-model"},
		{name: "direct Codex manifest", method: http.MethodGet, path: "/backend-api/codex/models"},
		{name: "media model metadata", method: http.MethodGet, path: "/v1/images/batches/models"},
		{name: "resource metadata", method: http.MethodGet, path: "/v1/responses/response-id"},
		{name: "not a single model detail", method: http.MethodGet, path: "/v1/models/model/nested"},
		{name: "empty detail", method: http.MethodGet, path: "/v1/models/"},
		{name: "POST", method: http.MethodPost, path: "/v1/models"},
		{name: "HEAD", method: http.MethodHead, path: "/v1/models"},
		{name: "WebSocket upgrade", method: http.MethodGet, path: "/v1/models", websocket: true},
		{name: "single group", method: http.MethodGet, path: "/v1/models", noBackup: true},
		{name: "source unavailable", method: http.MethodGet, path: "/v1/models", noSource: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			const body = `{"model":"must-not-be-read"}`
			c.Request = httptest.NewRequest(tc.method, tc.path, strings.NewReader(body))
			if tc.force {
				c.Set(string(ContextKeyForcePlatform), service.PlatformAntigravity)
			}
			if tc.websocket {
				c.Request.Header.Set("Connection", "Upgrade")
				c.Request.Header.Set("Upgrade", "websocket")
			}
			group := &service.Group{ID: 1, Platform: service.PlatformOpenAI}
			key := &service.APIKey{ID: 10, GroupID: &group.ID, Group: group, BackupGroupIDs: []int64{2, 3}}
			if tc.noBackup {
				key.BackupGroupIDs = nil
			}
			originalBackups := append([]int64(nil), key.BackupGroupIDs...)
			source := &catalogueIngressSelector{}
			var selector APIKeyRouteSelector = source
			if tc.noSource {
				selector = nil
			}
			got, ok := selectAPIKeyGroupRoute(c, key, selector)
			require.True(t, ok)
			require.Same(t, key, got)
			require.Same(t, group, key.Group)
			require.EqualValues(t, 1, *key.GroupID)
			require.Equal(t, originalBackups, key.BackupGroupIDs)
			require.Zero(t, source.calls, "metadata must never resolve a route")
			value, exists := c.Get("custom.key_route_catalogue_source")
			require.Equal(t, tc.wantSource, exists)
			if tc.wantSource {
				require.Same(t, source, value)
			}
			_, hasPlan := c.Get("custom.key_route")
			require.False(t, hasPlan)
			require.Nil(t, c.Request.Context().Value(ctxkey.KeyRoutingActive))
			require.False(t, c.IsAborted())
			remaining, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(remaining), "metadata ingress must not consume the body")
		})
	}
}
