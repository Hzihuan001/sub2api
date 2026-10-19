package middleware

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func keyPolicyAPIKey(models ...string) *service.APIKey {
	return &service.APIKey{KeyModelPolicy: &service.KeyModelPolicy{Enabled: true, Models: models}}
}

func TestKeyModelPolicyHTTPAdmissionAllPublicCandidates(t *testing.T) {
	for _, tc := range []struct {
		name, prefix, path, body string
		allowed                  bool
	}{
		{"messages", "/v1", "/v1/messages", `{"model":"allowed"}`, true},
		{"root alias", "", "/responses", `{"model":"blocked"}`, false},
		{"codex alias", "/backend-api/codex", "/backend-api/codex/responses", `{"model":"blocked"}`, false},
		{"chat", "/v1", "/v1/chat/completions", `{"model":"allowed"}`, true},
		{"duplicate", "/v1", "/v1/responses", `{"model":"allowed","model":"blocked"}`, false},
		{"case variant", "/v1", "/v1/responses", `{"model":"allowed","Model":"blocked"}`, false},
		{"null override", "/v1", "/v1/responses", `{"model":"allowed","Model":null}`, false},
		{"empty override", "/v1", "/v1/responses", `{"model":"allowed","model":""}`, false},
		{"number override", "/v1", "/v1/responses", `{"model":"allowed","model":12}`, false},
		{"nested session cannot bypass", "/v1", "/v1/responses", `{"model":"allowed","session":{"model":"blocked"}}`, false},
		{"duplicate allowed", "/v1", "/v1/responses", `{"model":"allowed","model":"allowed"}`, true},
		{"case sensitive value", "/v1", "/v1/responses", `{"model":"Allowed"}`, false},
		{"model absent", "/v1", "/v1/responses", `{"input":"hello"}`, false},
		{"gemini allowed path", "/v1beta", "/v1beta/models/allowed:generateContent", `{}`, true},
		{"gemini blocked path", "/v1beta", "/v1beta/models/blocked:streamGenerateContent", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, calls := newGroupModelAllowlistTestRouter(keyPolicyAPIKey("allowed"), tc.prefix)
			w := doJSON(t, router, http.MethodPost, tc.path, tc.body)
			if tc.allowed {
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Len(t, *calls, 1)
			} else {
				require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
				require.Empty(t, *calls)
				require.Contains(t, w.Body.String(), "API key")
			}
		})
	}
}

func TestKeyModelPolicyMultipartChecksEveryModelAndRestoresBody(t *testing.T) {
	for _, second := range []string{"allowed", "blocked", ""} {
		t.Run("last="+second, func(t *testing.T) {
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			require.NoError(t, form.WriteField("model", "allowed"))
			require.NoError(t, form.WriteField("model", second))
			require.NoError(t, form.WriteField("prompt", "cat"))
			require.NoError(t, form.Close())
			router, calls := newGroupModelAllowlistTestRouter(keyPolicyAPIKey("allowed"), "/v1")
			req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
			req.Header.Set("Content-Type", form.FormDataContentType())
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if second != "allowed" {
				require.Equal(t, http.StatusNotFound, w.Code)
				require.Empty(t, *calls)
			} else {
				require.Equal(t, http.StatusOK, w.Code)
				require.Len(t, *calls, 1)
				restored, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, body.Bytes(), restored)
			}
		})
	}
}

func TestKeyModelPolicyRequiresGroupAndKeyIntersection(t *testing.T) {
	apiKey := allowlistAPIKey(true, "group-only")
	apiKey.KeyModelPolicy = &service.KeyModelPolicy{Enabled: true, Models: []string{"key-only", "group-only"}}
	router, calls := newGroupModelAllowlistTestRouter(apiKey, "/v1")
	require.Equal(t, http.StatusNotFound, doJSON(t, router, http.MethodPost, "/v1/responses", `{"model":"key-only"}`).Code)
	require.Empty(t, *calls)
	require.Equal(t, http.StatusOK, doJSON(t, router, http.MethodPost, "/v1/responses", `{"model":"group-only"}`).Code)
}

func TestKeyModelPolicyUnsupportedSessionsAndExistingResources(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set(string(ContextKeyAPIKey), keyPolicyAPIKey("allowed")) })
	router.Use(GroupModelAllowlist())
	paths := []struct{ method, path string }{
		{http.MethodPost, "/v1/live"}, {http.MethodGet, "/v1/realtime"},
		{http.MethodGet, "/v1/responses"}, {http.MethodGet, "/v1/images/tasks/:task_id"},
		{http.MethodGet, "/v1/images/batches/:id/download"}, {http.MethodPost, "/v1/images/batches/:id/cancel"},
		{http.MethodDelete, "/v1/images/batches/:id"}, {http.MethodPost, "/v1/new-generation"},
	}
	for _, path := range paths {
		router.Handle(path.method, path.path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPost, "/v1/live", http.StatusNotFound},
		{http.MethodGet, "/v1/realtime?model=allowed", http.StatusNotFound},
		{http.MethodGet, "/v1/responses", http.StatusOK},
		{http.MethodGet, "/v1/images/tasks/existing", http.StatusOK},
		{http.MethodGet, "/v1/images/batches/existing/download", http.StatusOK},
		{http.MethodPost, "/v1/images/batches/existing/cancel", http.StatusOK},
		{http.MethodDelete, "/v1/images/batches/existing", http.StatusOK},
		{http.MethodPost, "/v1/new-generation", http.StatusNotFound},
	} {
		w := doJSON(t, router, tc.method, tc.path, `{}`)
		require.Equal(t, tc.status, w.Code, tc.path+": "+w.Body.String())
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/images/batches/existing/cancel", nil))
	require.Equal(t, http.StatusOK, w.Code, "body-free cancellation remains available")
}

func TestKeyModelPolicyDisabledKeepsNoReadFastPath(t *testing.T) {
	apiKey := keyPolicyAPIKey("allowed")
	apiKey.KeyModelPolicy.Enabled = false
	router, calls := newGroupModelAllowlistTestRouter(apiKey, "/v1")
	body := &readTrackingBody{Reader: strings.NewReader(`{"model":"blocked"}`)}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.False(t, body.read)
	require.Len(t, *calls, 1)
}
