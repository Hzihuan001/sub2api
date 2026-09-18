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

func TestProtocolClientRefreshValidatesRotatedTokenPair(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "missing access token", data: `{"access_token":"","refresh_token":"refresh","expires_in":900}`},
		{name: "missing refresh token", data: `{"access_token":"access","refresh_token":"","expires_in":900}`},
		{name: "missing expiry", data: `{"access_token":"access","refresh_token":"refresh","expires_in":0}`},
		{name: "whitespace token", data: `{"access_token":" access ","refresh_token":"refresh","expires_in":900}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/api/v1/reseller/v1/tokens/refresh", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":` + tt.data + `}`))
			}))
			defer server.Close()

			client := newProtocolClient()
			client.http = server.Client()
			_, _, _, err := client.refresh(context.Background(), server.URL, "refresh-old", "instance")
			require.ErrorIs(t, err, ErrInvalidInput)
		})
	}
}

func TestProtocolClientRefreshReturnsValidatedTokenPair(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/reseller/v1/tokens/refresh", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"access_token":"access-new","refresh_token":"refresh-new","expires_in":900}}`))
	}))
	defer server.Close()

	client := newProtocolClient()
	client.http = server.Client()
	access, refresh, expiresIn, err := client.refresh(context.Background(), server.URL, "refresh-old", "instance")
	require.NoError(t, err)
	require.Equal(t, "access-new", access)
	require.Equal(t, "refresh-new", refresh)
	require.Equal(t, int64(900), expiresIn)
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

func TestProtocolClientPreservesRawResponseData(t *testing.T) {
	raw := []byte(`{"schema": 1, "revision": "exact-bytes"}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(append([]byte(`{"code":0,"data":`), raw...), '}'))
	}))
	defer server.Close()

	client := newProtocolClient()
	var decoded map[string]any
	var retained []byte
	err := client.doJSONWithRawData(context.Background(), http.MethodGet, server.URL, "", "", nil, &decoded, nil, &retained)
	require.NoError(t, err)
	require.Equal(t, raw, retained)
	require.Equal(t, "exact-bytes", decoded["revision"])
}

func TestProtocolClientBalanceUsesScopedResellerEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/reseller/v1/balance", r.URL.Path)
		require.Equal(t, "Bearer reseller-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"balance":88.5,"frozen_balance":3.25,"warning":true}}`))
	}))
	defer server.Close()

	client := newProtocolClient()
	client.http = server.Client()
	result, err := client.balance(context.Background(), server.URL, "reseller-token")
	require.NoError(t, err)
	require.Equal(t, 88.5, result.Balance)
	require.Equal(t, 3.25, result.FrozenBalance)
	require.True(t, result.Warning)
}

func TestProtocolClientDoesNotFollowRedirects(t *testing.T) {
	var followed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			followed = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/final", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	result, err := newProtocolClient().balance(context.Background(), server.URL, "reseller-token")
	require.Error(t, err)
	require.NotNil(t, result)
	require.Zero(t, *result)
	require.False(t, followed, "authenticated protocol requests must not follow redirects")
	var upstreamErr *upstreamRequestError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusTemporaryRedirect, upstreamErr.Status)
}

func TestDisabledRuntimeStopsImmediately(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "false")
	runtime := NewRuntime(nil)
	require.NotPanics(t, runtime.Stop)
	require.NotPanics(t, runtime.Stop)
}
