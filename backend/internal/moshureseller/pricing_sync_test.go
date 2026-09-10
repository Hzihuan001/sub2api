package moshureseller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func pricingFixture(t *testing.T) (*remotePricingCatalog, *storedConnection, []Product) {
	t.Helper()
	billing := service.NewBillingService(&config.Config{}, nil)
	snapshot, err := billing.ExportResellerPricing(&service.Group{Platform: "openai"}, nil, 0.15)
	require.NoError(t, err)
	catalog := &remotePricingCatalog{Schema: 1, ResellerID: 3, Products: map[int64]*service.ResellerPricingSnapshot{10: snapshot}, Defaults: snapshot.Defaults, Fallbacks: snapshot.Fallbacks}
	snapshot.Defaults, snapshot.Fallbacks = nil, nil
	sealTestCatalog(t, catalog)
	id, gid := int64(3), int64(7)
	return catalog, &storedConnection{Connection: Connection{ResellerID: &id}}, []Product{{RemoteProductID: 10, LocalGroupID: &gid, Platform: "openai", Authorized: true}}
}

func sealTestCatalog(t *testing.T, catalog *remotePricingCatalog) {
	t.Helper()
	catalog.Revision = ""
	raw, err := json.Marshal(catalog)
	require.NoError(t, err)
	hash := sha256.Sum256(raw)
	catalog.Revision = hex.EncodeToString(hash[:])
}

func TestCompilePricingCatalogRestoresAndScopesProducts(t *testing.T) {
	catalog, connection, products := pricingFixture(t)
	compiled, err := compilePricingCatalog(catalog, connection, products)
	require.NoError(t, err)
	require.Equal(t, 0.15, compiled[7].CostRate())
	raw, err := json.Marshal(catalog)
	require.NoError(t, err)
	var restored remotePricingCatalog
	require.NoError(t, json.Unmarshal(raw, &restored))
	again, err := compilePricingCatalog(&restored, connection, products)
	require.NoError(t, err)
	require.Equal(t, compiled[7].Revision(), again[7].Revision())
	products[0].Authorized = false
	revoked, err := compilePricingCatalog(catalog, connection, products)
	require.NoError(t, err)
	require.Empty(t, revoked)
}

func TestCompilePricingCatalogRejectsWrongTenantOrCorruption(t *testing.T) {
	catalog, connection, products := pricingFixture(t)
	other := int64(99)
	connection.ResellerID = &other
	_, err := compilePricingCatalog(catalog, connection, products)
	require.ErrorContains(t, err, "tenant")
	connection.ResellerID = &catalog.ResellerID
	catalog.Products[10].CostRate = 0
	_, err = compilePricingCatalog(catalog, connection, products)
	require.ErrorContains(t, err, "revision")
	sealTestCatalog(t, catalog)
	_, err = compilePricingCatalog(catalog, connection, products)
	require.ErrorContains(t, err, "revision")
}

func TestSyncPricingPublishesOnlyAfterDurableCommit(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		t.Run(fmtBool(failWrite), func(t *testing.T) {
			t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
			defer service.ReplaceResellerPricing(nil)
			catalog, _, _ := pricingFixture(t)
			// Match the existing product fixture: remote ID 7, local group 10.
			catalog.Products[7] = catalog.Products[10]
			delete(catalog.Products, 10)
			sealTestCatalog(t, catalog)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer access", r.Header.Get("Authorization"))
				require.Equal(t, "/api/v1/reseller/v1/pricing", r.URL.Path)
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": catalog}))
			}))
			defer server.Close()
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(server.URL, "00000000-0000-0000-0000-000000000001", 3, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "", 1, nil, nil, nil))
			mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(true, 1.7, 10, 20))
			mock.ExpectQuery("SELECT revision").WillReturnError(sql.ErrNoRows)
			mock.ExpectBegin()
			write := mock.ExpectExec("INSERT INTO moshu_pricing_history")
			if failWrite {
				write.WillReturnError(sql.ErrConnDone)
				mock.ExpectRollback()
			} else {
				write.WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("INSERT INTO moshu_pricing_snapshots").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			}
			service.ReplaceResellerPricing(map[int64]*service.ResellerPricingCalculator{10: nil})
			sut := NewService(db, testEncryptor{}, nil)
			err = sut.SyncPricing(context.Background())
			key, pinErr := service.PinResellerPricing(&service.APIKey{Group: &service.Group{ID: 10}})
			if failWrite {
				require.Error(t, err)
				require.Error(t, pinErr)
			} else {
				require.NoError(t, err)
				require.NoError(t, pinErr)
				require.NotNil(t, key)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func fmtBool(value bool) string {
	if value {
		return "failed_write"
	}
	return "committed"
}

func TestSyncPricingOutageKeepsLastValidGeneration(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	defer service.ReplaceResellerPricing(nil)
	catalog, connection, products := pricingFixture(t)
	compiled, err := compilePricingCatalog(catalog, connection, products)
	require.NoError(t, err)
	service.ReplaceResellerPricing(compiled)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery("SELECT base_url").WillReturnRows(sqlmock.NewRows([]string{"base", "instance", "reseller", "name", "protocol", "status", "access", "refresh", "expiry", "etag", "version", "catalog_at", "settlement_at", "error"}).AddRow(server.URL, "00000000-0000-0000-0000-000000000001", 3, "L1", "v1", "active", "access", "refresh", time.Now().Add(time.Hour), "", 1, nil, nil, nil))
	mock.ExpectQuery("SELECT id,remote_product_id").WillReturnRows(pricingRows(true, 1.7, 7, 20))
	mock.ExpectQuery("SELECT revision").WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(catalog.Revision))
	require.Error(t, NewService(db, testEncryptor{}, nil).SyncPricing(context.Background()))
	key, err := service.PinResellerPricing(&service.APIKey{Group: &service.Group{ID: 7}})
	require.NoError(t, err)
	require.NotNil(t, key)
	require.NoError(t, mock.ExpectationsWereMet())
}
