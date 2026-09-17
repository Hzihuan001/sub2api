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

type remotePricingEnvelopeV2 struct {
	Schema                  int             `json:"schema"`
	DigestAlgorithm         string          `json:"digest_algorithm"`
	Digest                  string          `json:"digest"`
	BillingSemanticsVersion int             `json:"billing_semantics_version"`
	RequiredCapabilities    []string        `json:"required_capabilities"`
	Payload                 json.RawMessage `json:"payload"`
}

type compiledPricing struct {
	groups   map[int64]*service.ResellerPricingCalculator
	accounts map[int64]*service.ResellerPricingCalculator
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

func (s *Service) loadPricing(ctx context.Context, connection *storedConnection, products []Product) (*compiledPricing, error) {
	return s.loadPricingSchema(ctx, connection, products, 0)
}

// schema=0 restores the newest locally supported generation. A specific schema
// is used for 304 handling so a v1 response can never publish a stale v2 slot.
func (s *Service) loadPricingSchema(ctx context.Context, connection *storedConnection, products []Product, requestedSchema int) (*compiledPricing, error) {
	var payload, raw []byte
	var schema, semantics int
	var revision string
	var algorithm sql.NullString
	query := `SELECT payload,schema_version,revision,digest_algorithm,raw_payload,billing_semantics_version
		FROM moshu_pricing_snapshots
		WHERE base_url=$1::text AND reseller_id=$2::bigint AND instance_id=$3::uuid`
	args := []any{connection.BaseURL, connection.ResellerID, connection.InstanceID}
	if requestedSchema > 0 {
		query += ` AND id=$4`
		args = append(args, requestedSchema)
	}
	query += ` ORDER BY schema_version DESC,id DESC LIMIT 1`
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&payload, &schema, &revision, &algorithm, &raw, &semantics)
	if errors.Is(err, sql.ErrNoRows) {
		return pendingPricing(products), nil
	}
	if err != nil {
		return nil, err
	}
	trusted := false
	if schema == 2 {
		if algorithm.String != "sha256" || semantics != 1 || len(raw) == 0 {
			return nil, fmt.Errorf("unsupported cached pricing schema")
		}
		hash := sha256.Sum256(raw)
		if hex.EncodeToString(hash[:]) != revision {
			return nil, fmt.Errorf("cached pricing payload digest mismatch")
		}
		payload = raw
		trusted = true
	} else if len(raw) > 0 {
		// Schema 1 remains self-validating through its embedded revision. Prefer
		// the byte-exact response retained for diagnostics and rollback testing.
		payload = raw
	}
	var catalog remotePricingCatalog
	if err := json.Unmarshal(payload, &catalog); err != nil {
		return nil, err
	}
	return s.compilePricingCatalogVersion(ctx, &catalog, connection, products, trusted)
}

func pendingPricing(products []Product) *compiledPricing {
	result := make(map[int64]*service.ResellerPricingCalculator)
	for _, p := range products {
		if p.Authorized && p.LocalGroupID != nil {
			result[*p.LocalGroupID] = nil
		}
	}
	return &compiledPricing{groups: result, accounts: map[int64]*service.ResellerPricingCalculator{}}
}

func (s *Service) compilePricingCatalog(ctx context.Context, catalog *remotePricingCatalog, connection *storedConnection, products []Product) (*compiledPricing, error) {
	return s.compilePricingCatalogVersion(ctx, catalog, connection, products, false)
}

func (s *Service) compilePricingCatalogVersion(ctx context.Context, catalog *remotePricingCatalog, connection *storedConnection, products []Product, trusted bool) (*compiledPricing, error) {
	if trusted {
		if catalog == nil || catalog.Schema != 1 || connection == nil || connection.ResellerID == nil || catalog.ResellerID != *connection.ResellerID {
			return nil, fmt.Errorf("pricing tenant or schema mismatch")
		}
	} else if err := validatePricingCatalog(catalog, connection); err != nil {
		return nil, err
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
		groupIDs := []int64{*product.LocalGroupID}
		if product.LocalAccountID != nil {
			rows, queryErr := s.db.QueryContext(ctx, `SELECT group_id FROM account_groups WHERE account_id=$1 ORDER BY group_id`, *product.LocalAccountID)
			if queryErr != nil {
				return nil, queryErr
			}
			groupIDs = groupIDs[:0]
			for rows.Next() {
				var groupID int64
				if scanErr := rows.Scan(&groupID); scanErr != nil {
					_ = rows.Close()
					return nil, scanErr
				}
				groupIDs = append(groupIDs, groupID)
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				_ = rows.Close()
				return nil, rowsErr
			}
			_ = rows.Close()
		}
		for _, groupID := range groupIDs {
			var calculator *service.ResellerPricingCalculator
			var compileErr error
			if trusted {
				calculator, compileErr = service.CompileValidatedResellerPricing(&full, groupID, &config.Config{})
			} else {
				calculator, compileErr = service.CompileResellerPricing(&full, groupID, &config.Config{})
			}
			if compileErr != nil {
				return nil, compileErr
			}
			if _, exists := result.groups[groupID]; !exists || result.groups[groupID] == nil {
				result.groups[groupID] = calculator
			}
			if product.LocalAccountID != nil {
				result.accounts[*product.LocalAccountID] = calculator
			}
		}
	}
	return result, nil
}

func validatePricingCatalog(catalog *remotePricingCatalog, connection *storedConnection) error {
	if catalog == nil || catalog.Schema != 1 || connection == nil || connection.ResellerID == nil || catalog.ResellerID != *connection.ResellerID {
		return fmt.Errorf("pricing tenant or schema mismatch")
	}
	copy := *catalog
	copy.Revision = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	if catalog.Revision != hex.EncodeToString(hash[:]) {
		return fmt.Errorf("pricing catalog revision mismatch")
	}
	return nil
}

// compilePricingCatalog keeps the pure schema-1 compiler available for
// compatibility tests and offline validation. Runtime publishing uses the
// service method above so one upstream account can price every linked group.
func compilePricingCatalog(catalog *remotePricingCatalog, connection *storedConnection, products []Product) (map[int64]*service.ResellerPricingCalculator, error) {
	if err := validatePricingCatalog(catalog, connection); err != nil {
		return nil, err
	}
	result := make(map[int64]*service.ResellerPricingCalculator)
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
		calculator, compileErr := service.CompileResellerPricing(&full, *product.LocalGroupID, &config.Config{})
		if compileErr != nil {
			return nil, compileErr
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
		pending := pendingPricing(products)
		service.ReplaceResellerPricingWithAccounts(pending.groups, pending.accounts)
		return nil
	}
	prices, err := s.loadPricing(ctx, connection, products)
	if err != nil {
		pending := pendingPricing(products)
		service.ReplaceResellerPricingWithAccounts(pending.groups, pending.accounts)
		return err
	}
	service.ReplaceResellerPricingWithAccounts(prices.groups, prices.accounts)
	return nil
}

// SyncPricing persists and validates a complete generation before publishing it.
// The configuration lock prevents a concurrent reauthorization from installing
// the previous tenant's prices into the new tenant's channels.
func (s *Service) SyncPricing(ctx context.Context) (syncErr error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	connection, token, err := s.authenticatedConnection(ctx)
	if err != nil {
		s.recordSyncDomainFailure(context.Background(), "auth", err)
		return err
	}
	s.recordSyncDomainSuccess(ctx, "auth")
	defer func() {
		if syncErr != nil {
			s.recordSyncDomainFailure(context.Background(), "pricing", syncErr)
		}
	}()
	products, err := s.listProducts(ctx)
	if err != nil {
		return err
	}
	useSchema2 := false
	capabilities, capabilityErr := s.client.capabilities(ctx, connection.BaseURL, token)
	if capabilityErr == nil {
		for _, schema := range capabilities.PricingSchemas {
			useSchema2 = useSchema2 || schema == 2
		}
		if !useSchema2 || capabilities.PricingDigestAlgorithm != "sha256" || capabilities.BillingSemanticsVersion != 1 {
			return fmt.Errorf("main site does not provide compatible pricing semantics")
		}
	} else {
		var upstreamErr *upstreamRequestError
		if !errors.As(capabilityErr, &upstreamErr) || upstreamErr.Status != http.StatusNotFound {
			return capabilityErr
		}
	}
	storedSchema := 1
	if useSchema2 {
		storedSchema = 2
	}
	var revision string
	err = s.db.QueryRowContext(ctx, `SELECT revision FROM moshu_pricing_snapshots WHERE id=$1 AND base_url=$2::text AND reseller_id=$3::bigint AND instance_id=$4::uuid`, storedSchema, connection.BaseURL, connection.ResellerID, connection.InstanceID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	etag := ""
	if revision != "" {
		if useSchema2 {
			etag = `"v2-` + revision + `"`
		} else {
			etag = `"v1-` + revision + `"`
		}
	}
	var catalog remotePricingCatalog
	var envelope remotePricingEnvelopeV2
	var responseRawData []byte
	notModified := false
	endpoint := connection.BaseURL + "/api/v1/reseller/v1/pricing"
	var destination any = &catalog
	if useSchema2 {
		endpoint += "?schema=2"
		destination = &envelope
	}
	err = s.client.doJSONWithRawData(ctx, http.MethodGet, endpoint, token, etag, nil, destination, func(response *http.Response) { notModified = response.StatusCode == http.StatusNotModified }, &responseRawData)
	if err != nil {
		return err
	}
	if notModified {
		prices, err := s.loadPricingSchema(ctx, connection, products, storedSchema)
		if err == nil {
			service.ReplaceResellerPricingWithAccounts(prices.groups, prices.accounts)
			s.recordSyncDomainSuccess(ctx, "pricing")
			return nil
		}
		// A corrupt local cache must recover even if the upstream revision has
		// not changed. Fetch the full payload instead of looping on HTTP 304.
		if err := s.client.doJSONWithRawData(ctx, http.MethodGet, endpoint, token, "", nil, destination, nil, &responseRawData); err != nil {
			return err
		}
	}
	semantics := 1
	algorithm := ""
	var rawPayload []byte
	if useSchema2 {
		if err := validatePricingEnvelopeV2(&envelope); err != nil {
			return err
		}
		if err := json.Unmarshal(envelope.Payload, &catalog); err != nil {
			return fmt.Errorf("invalid schema-2 pricing payload: %w", err)
		}
		storedSchema, semantics, algorithm = 2, envelope.BillingSemanticsVersion, envelope.DigestAlgorithm
		revision, rawPayload = envelope.Digest, append([]byte(nil), envelope.Payload...)
	} else {
		revision = catalog.Revision
		rawPayload = append([]byte(nil), responseRawData...)
	}
	prices, err := s.compilePricingCatalogVersion(ctx, &catalog, connection, products, useSchema2)
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
	_, err = tx.ExecContext(ctx, `INSERT INTO moshu_pricing_history(revision,payload,schema_version,digest_algorithm,raw_payload,billing_semantics_version) VALUES($1::text,$2::jsonb,$3,$4,$5,$6) ON CONFLICT(revision) DO NOTHING`, revision, raw, storedSchema, nullableString(algorithm), rawPayload, semantics)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO moshu_pricing_snapshots(id,base_url,reseller_id,instance_id,revision,payload,schema_version,digest_algorithm,raw_payload,billing_semantics_version) VALUES($1,$2::text,$3::bigint,$4::uuid,$5::text,$6::jsonb,$7,$8,$9,$10) ON CONFLICT(id) DO UPDATE SET base_url=EXCLUDED.base_url,reseller_id=EXCLUDED.reseller_id,instance_id=EXCLUDED.instance_id,revision=EXCLUDED.revision,payload=EXCLUDED.payload,schema_version=EXCLUDED.schema_version,digest_algorithm=EXCLUDED.digest_algorithm,raw_payload=EXCLUDED.raw_payload,billing_semantics_version=EXCLUDED.billing_semantics_version,updated_at=NOW()`, storedSchema, connection.BaseURL, connection.ResellerID, connection.InstanceID, revision, raw, storedSchema, nullableString(algorithm), rawPayload, semantics)
	if err != nil {
		return err
	}
	if useSchema2 {
		// The v2 payload is the byte-exact schema-1 catalog wrapped with stronger
		// integrity metadata. Retain it in slot 1 so rollback binaries can start
		// and bill without requiring the main site to be online.
		_, err = tx.ExecContext(ctx, `INSERT INTO moshu_pricing_history(revision,payload,schema_version,digest_algorithm,raw_payload,billing_semantics_version) VALUES($1::text,$2::jsonb,1,NULL,$3,1) ON CONFLICT(revision) DO NOTHING`, catalog.Revision, raw, rawPayload)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO moshu_pricing_snapshots(id,base_url,reseller_id,instance_id,revision,payload,schema_version,digest_algorithm,raw_payload,billing_semantics_version) VALUES(1,$1::text,$2::bigint,$3::uuid,$4::text,$5::jsonb,1,NULL,$6,1) ON CONFLICT(id) DO UPDATE SET base_url=EXCLUDED.base_url,reseller_id=EXCLUDED.reseller_id,instance_id=EXCLUDED.instance_id,revision=EXCLUDED.revision,payload=EXCLUDED.payload,schema_version=1,digest_algorithm=NULL,raw_payload=EXCLUDED.raw_payload,billing_semantics_version=1,updated_at=NOW()`, connection.BaseURL, connection.ResellerID, connection.InstanceID, catalog.Revision, raw, rawPayload)
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	service.ReplaceResellerPricingWithAccounts(prices.groups, prices.accounts)
	s.recordSyncDomainSuccess(ctx, "pricing")
	return nil
}

func validatePricingEnvelopeV2(envelope *remotePricingEnvelopeV2) error {
	if envelope == nil || envelope.Schema != 2 || envelope.DigestAlgorithm != "sha256" || envelope.BillingSemanticsVersion != 1 || len(envelope.Payload) == 0 {
		return fmt.Errorf("unsupported schema-2 pricing envelope")
	}
	known := map[string]bool{"token_pricing": true, "cache_pricing": true, "media_pricing": true, "service_tier_pricing": true}
	for _, capability := range envelope.RequiredCapabilities {
		if !known[capability] {
			return fmt.Errorf("unsupported pricing capability %q", capability)
		}
	}
	hash := sha256.Sum256(envelope.Payload)
	if hex.EncodeToString(hash[:]) != envelope.Digest {
		return fmt.Errorf("schema-2 pricing payload digest mismatch")
	}
	return nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
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
