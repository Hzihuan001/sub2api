package moshureseller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type remotePricingCatalog struct {
	Schema     int                                        `json:"schema"`
	ResellerID int64                                      `json:"reseller_id"`
	Revision   string                                     `json:"revision"`
	Products   map[int64]*service.ResellerPricingSnapshot `json:"products"`
	Defaults   map[string]*service.LiteLLMModelPricing    `json:"defaults"`
	Fallbacks  map[string]*service.ModelPricing           `json:"fallbacks"`
}

func (s *Service) restorePricingAfterConfiguration() {
	if !s.pricingEnabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.restorePricingLocked(ctx); err != nil {
		service.SuspendResellerPricing()
		slog.Error("reseller pricing restoration after configuration failed", "error", err)
	}
}

func (s *Service) loadPricing(ctx context.Context, connection *storedConnection, products []Product) (map[int64]*service.ResellerPricingCalculator, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM moshu_pricing_snapshots WHERE id=1 AND base_url=$1::text AND reseller_id=$2::bigint AND instance_id=$3::uuid`, connection.BaseURL, connection.ResellerID, connection.InstanceID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return pendingPricing(products), nil
	}
	if err != nil {
		return nil, err
	}
	var catalog remotePricingCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, err
	}
	return compilePricingCatalog(&catalog, connection, products)
}

func pendingPricing(products []Product) map[int64]*service.ResellerPricingCalculator {
	result := make(map[int64]*service.ResellerPricingCalculator)
	for _, p := range products {
		if p.Authorized && p.LocalGroupID != nil {
			result[*p.LocalGroupID] = nil
		}
	}
	return result
}

func compilePricingCatalog(catalog *remotePricingCatalog, connection *storedConnection, products []Product) (map[int64]*service.ResellerPricingCalculator, error) {
	if catalog == nil || catalog.Schema != 1 || connection == nil || connection.ResellerID == nil || catalog.ResellerID != *connection.ResellerID {
		return nil, fmt.Errorf("pricing tenant or schema mismatch")
	}
	copy := *catalog
	copy.Revision = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	if catalog.Revision != hex.EncodeToString(hash[:]) {
		return nil, fmt.Errorf("pricing catalog revision mismatch")
	}
	result := pendingPricing(products)
	for _, product := range products {
		if !product.Authorized || product.LocalGroupID == nil {
			continue
		}
		snapshot := catalog.Products[product.RemoteProductID]
		if snapshot == nil {
			continue
		}
		if snapshot.Platform != product.Platform {
			return nil, fmt.Errorf("pricing product platform mismatch")
		}
		full := *snapshot
		full.Defaults, full.Fallbacks = catalog.Defaults, catalog.Fallbacks
		calculator, err := service.CompileResellerPricing(&full, *product.LocalGroupID, &config.Config{})
		if err != nil {
			return nil, err
		}
		result[*product.LocalGroupID] = calculator
	}
	return result, nil
}

// restorePricingLocked runs before serving requests and after configuration
// changes. It only uses local storage; upstream outages cannot delay requests.
func (s *Service) restorePricingLocked(ctx context.Context) error {
	connection, err := s.loadConnection(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		service.ReplaceResellerPricing(nil)
		return nil
	}
	if err != nil {
		return err
	}
	products, err := s.listProducts(ctx)
	if err != nil {
		return err
	}
	if connection.Status != "active" && connection.Status != "error" {
		service.ReplaceResellerPricing(pendingPricing(products))
		return nil
	}
	prices, err := s.loadPricing(ctx, connection, products)
	if err != nil {
		service.ReplaceResellerPricing(pendingPricing(products))
		return err
	}
	service.ReplaceResellerPricing(prices)
	return nil
}

// SyncPricing persists and validates a complete generation before publishing it.
// The configuration lock prevents a concurrent reauthorization from installing
// the previous tenant's prices into the new tenant's channels.
func (s *Service) SyncPricing(ctx context.Context) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	connection, token, err := s.authenticatedConnection(ctx)
	if err != nil {
		return err
	}
	products, err := s.listProducts(ctx)
	if err != nil {
		return err
	}
	var revision string
	err = s.db.QueryRowContext(ctx, `SELECT revision FROM moshu_pricing_snapshots WHERE id=1 AND base_url=$1::text AND reseller_id=$2::bigint AND instance_id=$3::uuid`, connection.BaseURL, connection.ResellerID, connection.InstanceID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	etag := ""
	if revision != "" {
		etag = `"` + revision + `"`
	}
	var catalog remotePricingCatalog
	notModified := false
	err = s.client.doJSON(ctx, http.MethodGet, connection.BaseURL+"/api/v1/reseller/v1/pricing", token, etag, nil, &catalog, func(response *http.Response) { notModified = response.StatusCode == http.StatusNotModified })
	if err != nil {
		return err
	}
	if notModified {
		prices, err := s.loadPricing(ctx, connection, products)
		if err == nil {
			service.ReplaceResellerPricing(prices)
			return nil
		}
		// A corrupt local cache must recover even if the upstream revision has
		// not changed. Fetch the full payload instead of looping on HTTP 304.
		if err := s.client.doJSON(ctx, http.MethodGet, connection.BaseURL+"/api/v1/reseller/v1/pricing", token, "", nil, &catalog, nil); err != nil {
			return err
		}
	}
	prices, err := compilePricingCatalog(&catalog, connection, products)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(catalog)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO moshu_pricing_history(revision,payload) VALUES($1::text,$2::jsonb) ON CONFLICT(revision) DO NOTHING`, catalog.Revision, raw)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO moshu_pricing_snapshots(id,base_url,reseller_id,instance_id,revision,payload) VALUES(1,$1::text,$2::bigint,$3::uuid,$4::text,$5::jsonb) ON CONFLICT(id) DO UPDATE SET base_url=EXCLUDED.base_url,reseller_id=EXCLUDED.reseller_id,instance_id=EXCLUDED.instance_id,revision=EXCLUDED.revision,payload=EXCLUDED.payload,updated_at=NOW()`, connection.BaseURL, connection.ResellerID, connection.InstanceID, catalog.Revision, raw)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	service.ReplaceResellerPricing(prices)
	return nil
}

func (s *Service) runPricingSync(ctx context.Context) {
	cursor := ""
	lastSync := time.Time{}
	for ctx.Err() == nil {
		connection, token, err := s.authenticatedConnection(ctx)
		var response struct {
			Cursor string `json:"cursor"`
		}
		if err == nil {
			err = s.client.doJSON(ctx, http.MethodGet, connection.BaseURL+"/api/v1/reseller/v1/pricing/changes?after="+url.QueryEscape(cursor), token, "", nil, &response, nil)
			if err == nil && response.Cursor == "" {
				err = fmt.Errorf("missing pricing change cursor")
			}
		}
		if err == nil && (response.Cursor != cursor || time.Since(lastSync) >= time.Minute) {
			runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			// Refresh effective multiplier/product grants as well as model prices.
			_, err = s.SyncCatalog(runCtx)
			if err == nil {
				err = s.SyncPricing(runCtx)
			}
			cancel()
			if err == nil {
				cursor = response.Cursor
				lastSync = time.Now()
			}
		}
		if err != nil {
			if !errors.Is(err, ErrNotConnected) && ctx.Err() == nil {
				slog.Warn("reseller pricing sync deferred; retaining last valid prices", "error", err)
			}
			// Even a missing/dropped notification endpoint must not disable polling.
			if time.Since(lastSync) >= time.Minute {
				runCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				if s.SyncPricing(runCtx) == nil {
					lastSync = time.Now()
				}
				cancel()
			}
			timer := time.NewTimer(10 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		// Coalesce bursts (or a misbehaving immediate-return long poll) instead
		// of spinning or repeatedly rebuilding the same large price catalog.
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
