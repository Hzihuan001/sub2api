package moshureseller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProtocolClientContractEndpoints keeps the reseller client aligned with
// the versioned main-site protocol.  The fixture deliberately includes fields
// introduced by newer main-site versions; the client must continue to accept
// them while preserving the stable paths, methods, and authentication headers.
func TestProtocolClientContractEndpoints(t *testing.T) {
	var acknowledged []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/reseller/v1/capabilities":
			require.Equal(t, http.MethodGet, r.Method)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"protocol_version":"v2","pricing_schemas":[1,2],"pricing_digest_algorithm":"sha256","billing_semantics_version":1,"settlement_events":true,"future_capability":true}}`))

		case "/api/v1/reseller/v1/catalog":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, `"catalog-1"`, r.Header.Get("If-None-Match"))
			w.Header().Set("ETag", `"catalog-2"`)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"protocol_version":"v2","reseller_id":7,"reseller_name":"L1","catalog_version":12,"generated_at":"2026-09-18T00:00:00Z","products":[{"id":9,"moshu_group_id":4,"product_code":"deepseek","display_name":"DeepSeek","platform":"deepseek","enabled":true,"cost_rate_multiplier":0.3,"price_catalog_version":5,"models":["deepseek-v4.1-flash"],"capabilities":{"streaming":true},"effective_at":"2026-09-18T00:00:00Z","future_product_field":"ignored"}]}}`))

		case "/api/v1/reseller/v1/settlements":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "41", r.URL.Query().Get("after_id"))
			require.Equal(t, "500", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"id":42,"revision":3,"request_source":"monitor","request_id":"req-42","product_id":9,"product_code":"deepseek","standard_cost":0.02,"cost_rate_multiplier":0.3,"actual_cost":0.021,"price_catalog_version":5,"status":"completed"}],"next_cursor":42}}`))

		case "/api/v1/reseller/v1/settlement-events":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "500", r.URL.Query().Get("limit"))
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"items":[{"event_id":43,"revision":4,"settlement":{"id":42,"revision":4,"request_source":"user","request_id":"req-42","product_id":9,"product_code":"deepseek","standard_cost":0.02,"cost_rate_multiplier":0.3,"actual_cost":0.019,"price_catalog_version":6,"status":"completed"}}]}}`))

		case "/api/v1/reseller/v1/settlement-events/ack":
			require.Equal(t, http.MethodPost, r.Method)
			var body struct {
				EventIDs []int64 `json:"event_ids"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			acknowledged = append([]int64(nil), body.EventIDs...)
			_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{}}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := newProtocolClient()
	client.http = server.Client()

	capabilities, err := client.capabilities(t.Context(), server.URL, "access-token")
	require.NoError(t, err)
	require.Equal(t, []int{1, 2}, capabilities.PricingSchemas)
	require.True(t, capabilities.SettlementEvents)

	catalog, etag, notModified, err := client.catalog(t.Context(), server.URL, "access-token", `"catalog-1"`)
	require.NoError(t, err)
	require.False(t, notModified)
	require.Equal(t, `"catalog-2"`, etag)
	require.Len(t, catalog.Products, 1)
	require.Equal(t, "deepseek-v4.1-flash", catalog.Products[0].Models[0])

	settlements, err := client.settlements(t.Context(), server.URL, "access-token", 41)
	require.NoError(t, err)
	require.Equal(t, int64(42), settlements.NextCursor)
	require.Equal(t, int64(3), settlements.Items[0].Revision)

	events, err := client.settlementEvents(t.Context(), server.URL, "access-token")
	require.NoError(t, err)
	require.Len(t, events.Items, 1)
	require.Equal(t, int64(4), events.Items[0].Revision)
	require.NoError(t, client.acknowledgeSettlementEvents(t.Context(), server.URL, "access-token", []int64{43}))
	require.Equal(t, []int64{43}, acknowledged)
}
