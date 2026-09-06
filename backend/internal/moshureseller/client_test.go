package moshureseller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBaseURLRequiresHTTPSForPublicHosts(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_ALLOW_HTTP", "false")

	_, err := validateBaseURL("http://moshu.example/path?secret=value#fragment")
	require.ErrorIs(t, err, ErrInvalidInput)

	got, err := validateBaseURL("https://moshu.example/path?secret=value#fragment")
	require.NoError(t, err)
	require.Equal(t, "https://moshu.example/path", got)
}

func TestValidateBaseURLAllowsLocalHTTPWithoutOverride(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_ALLOW_HTTP", "false")
	got, err := validateBaseURL("http://127.0.0.1:18080/")
	require.NoError(t, err)
	require.Equal(t, "http://127.0.0.1:18080", got)
}

func TestProtocolClientExchangeUsesEnrollmentEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/reseller/v1/enrollments/exchange", r.URL.Path)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"tenant":{"id":7,"name":"L1","protocol_version":"v1"},"access_token":"access","refresh_token":"refresh","expires_in":900,"catalog":{"protocol_version":"v1","reseller_id":7,"reseller_name":"L1","catalog_version":3,"generated_at":"2026-09-07T00:00:00Z","products":[]},"credentials":[]}}`))
	}))
	defer server.Close()

	client := newProtocolClient()
	client.http = server.Client()
	result, err := client.exchange(context.Background(), server.URL, "one-time-code", "00000000-0000-0000-0000-000000000001")
	require.NoError(t, err)
	require.Equal(t, int64(7), result.Tenant.ID)
	require.Equal(t, "access", result.AccessToken)
	require.Equal(t, int64(3), result.Catalog.CatalogVersion)
}

func TestProtocolClientCatalogHonorsETagAndNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		require.Equal(t, `W/"3"`, r.Header.Get("If-None-Match"))
		w.Header().Set("ETag", `W/"3"`)
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	client := newProtocolClient()
	client.http = server.Client()
	_, etag, notModified, err := client.catalog(context.Background(), server.URL, "token", `W/"3"`)
	require.NoError(t, err)
	require.True(t, notModified)
	require.Equal(t, `W/"3"`, etag)
}

func TestDisabledRuntimeStopsImmediately(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "false")
	runtime := NewRuntime(nil)
	require.NotPanics(t, runtime.Stop)
	require.NotPanics(t, runtime.Stop)
}
