package moshureseller

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

var (
	ErrDisabled     = errors.New("Moshu reseller client is disabled")
	ErrNotConnected = errors.New("Moshu reseller is not connected")
	ErrNotFound     = errors.New("Moshu product not found")
	ErrInvalidInput = errors.New("invalid Moshu reseller request")
)

type Service struct {
	db        *sql.DB
	encryptor service.SecretEncryptor
	admin     service.AdminService
	client    *protocolClient
	now       func() time.Time
	authMu    sync.Mutex
	configMu  sync.Mutex
}

func NewService(db *sql.DB, encryptor service.SecretEncryptor, admin service.AdminService) *Service {
	return &Service{db: db, encryptor: encryptor, admin: admin, client: newProtocolClient(), now: time.Now}
}

func Enabled() bool { return envEnabled("MOSHU_RESELLER_CLIENT_ENABLED") }

type storedConnection struct {
	Connection
	AccessCiphertext  string
	RefreshCiphertext string
	AccessExpiresAt   *time.Time
	CatalogETag       string
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	return s.status(ctx, true)
}

func (s *Service) status(ctx context.Context, reconcile bool) (*Status, error) {
	result := &Status{Enabled: Enabled(), Products: []Product{}}
	connection, err := s.loadConnection(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Connection = &connection.Connection
	result.Connected = connection.Status == "active" || connection.Status == "error"
	if reconcile && s.admin != nil {
		if err := s.reconcileSelectedProducts(ctx); err != nil {
			return nil, err
		}
	}
	products, err := s.listProducts(ctx)
	if err != nil {
		return nil, err
	}
	result.Products = products
	return result, nil
}

func (s *Service) reconcileSelectedProducts(ctx context.Context) error {
	products, err := s.listProducts(ctx)
	if err != nil {
		return err
	}
	for i := range products {
		product := &products[i]
		if !product.Selected {
			continue
		}
		missing := product.LocalGroupID == nil || product.LocalAccountID == nil
		if !missing {
			_, groupErr := s.admin.GetGroup(ctx, *product.LocalGroupID)
			if groupErr != nil && !errors.Is(groupErr, service.ErrGroupNotFound) {
				return groupErr
			}
			missing = errors.Is(groupErr, service.ErrGroupNotFound)
		}
		if !missing {
			_, accountErr := s.admin.GetAccount(ctx, *product.LocalAccountID)
			if accountErr != nil && !errors.Is(accountErr, service.ErrAccountNotFound) {
				return accountErr
			}
			missing = errors.Is(accountErr, service.ErrAccountNotFound)
		}
		if missing {
			if _, err := s.deactivateProduct(ctx, product); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) Enroll(ctx context.Context, rawBaseURL, enrollmentCode string) (*Status, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	// Enrollment replaces the refresh token; do not race the settlement worker.
	s.authMu.Lock()
	defer s.authMu.Unlock()
	if !Enabled() {
		return nil, ErrDisabled
	}
	baseURL := strings.TrimSpace(rawBaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("MOSHU_RESELLER_URL"))
	}
	baseURL, err := validateBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	instanceID := uuid.NewString()
	var existingResellerID *int64
	if existing, loadErr := s.loadConnection(ctx); loadErr == nil && existing.InstanceID != "" {
		if existing.BaseURL != baseURL {
			return nil, fmt.Errorf("%w: reconnect to the existing main site; changing sites requires a separate migration", ErrInvalidInput)
		}
		existingResellerID = existing.ResellerID
		instanceID = existing.InstanceID
	} else if loadErr != nil && !errors.Is(loadErr, sql.ErrNoRows) {
		return nil, loadErr
	}
	exchange, err := s.client.exchange(ctx, baseURL, strings.TrimSpace(enrollmentCode), instanceID)
	if err != nil {
		return nil, err
	}
	if exchange.Tenant.ProtocolVersion != "v1" || exchange.Catalog.ProtocolVersion != "v1" {
		return nil, fmt.Errorf("%w: unsupported Moshu reseller protocol", ErrInvalidInput)
	}
	if existingResellerID != nil && *existingResellerID != exchange.Tenant.ID {
		return nil, fmt.Errorf("%w: cannot replace the connected reseller identity", ErrInvalidInput)
	}
	accessCiphertext, err := s.encryptor.Encrypt(exchange.AccessToken)
	if err != nil {
		return nil, err
	}
	refreshCiphertext, err := s.encryptor.Encrypt(exchange.RefreshToken)
	if err != nil {
		return nil, err
	}
	credentials := make(map[int64]string, len(exchange.Credentials))
	for _, credential := range exchange.Credentials {
		ciphertext, encErr := s.encryptor.Encrypt(credential.APIKey)
		if encErr != nil {
			return nil, encErr
		}
		credentials[credential.ProductID] = ciphertext
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	expiresAt := s.now().UTC().Add(time.Duration(exchange.ExpiresIn) * time.Second)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO moshu_reseller_connections
		(id,base_url,instance_id,reseller_id,reseller_name,protocol_version,status,
		 access_token_ciphertext,refresh_token_ciphertext,access_token_expires_at,
		 catalog_version,last_catalog_sync_at,last_error,updated_at)
		VALUES (1,$1,$2,$3,$4,$5,'active',$6,$7,$8,$9,NOW(),NULL,NOW())
		ON CONFLICT (id) DO UPDATE SET
		 base_url=EXCLUDED.base_url,instance_id=EXCLUDED.instance_id,
		 reseller_id=EXCLUDED.reseller_id,reseller_name=EXCLUDED.reseller_name,
		 protocol_version=EXCLUDED.protocol_version,status='active',
		 access_token_ciphertext=EXCLUDED.access_token_ciphertext,
		 refresh_token_ciphertext=EXCLUDED.refresh_token_ciphertext,
		 access_token_expires_at=EXCLUDED.access_token_expires_at,
		 catalog_version=EXCLUDED.catalog_version,catalog_etag=NULL,last_catalog_sync_at=NOW(),
		 last_error=NULL,updated_at=NOW()`, baseURL, instanceID, exchange.Tenant.ID,
		exchange.Tenant.Name, exchange.Tenant.ProtocolVersion, accessCiphertext,
		refreshCiphertext, expiresAt, exchange.Catalog.CatalogVersion)
	if err != nil {
		return nil, err
	}
	if err := persistCatalogTx(ctx, tx, exchange.Catalog, credentials); err != nil {
		return nil, err
	}
	for _, credential := range exchange.Credentials {
		if err := syncAccountCredentialTx(ctx, tx, credential.ProductID, credential.APIKey); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.status(ctx, false)
}

func (s *Service) SyncCatalog(ctx context.Context) (_ *Status, syncErr error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	connection, token, err := s.authenticatedConnection(ctx)
	if err != nil {
		return nil, err
	}
	var runID int64
	defer func() {
		if syncErr != nil {
			s.recordSyncFailure(ctx, runID, syncErr)
		}
	}()
	_ = s.db.QueryRowContext(ctx, `INSERT INTO moshu_catalog_sync_runs(status,from_version) VALUES('running',$1) RETURNING id`, connection.CatalogVersion).Scan(&runID)
	catalog, etag, notModified, err := s.client.catalog(ctx, connection.BaseURL, token, connection.CatalogETag)
	if err != nil {
		return nil, err
	}
	if notModified {
		// Retry local revocation even if the remote catalog has not changed.
		if err := s.disableRevokedProducts(ctx); err != nil {
			return nil, err
		}
		if err := s.applyCatalogConfiguration(ctx); err != nil {
			return nil, err
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE moshu_reseller_connections SET status='active',last_catalog_sync_at=NOW(),last_error=NULL,updated_at=NOW() WHERE id=1`)
		_, _ = s.db.ExecContext(ctx, `UPDATE moshu_catalog_sync_runs SET status='succeeded',to_version=$2,completed_at=NOW() WHERE id=$1`, runID, connection.CatalogVersion)
		return s.status(ctx, false)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := persistCatalogTx(ctx, tx, *catalog, nil); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE moshu_products SET authorized=FALSE,updated_at=NOW() WHERE remote_product_id <> ALL($1::bigint[]) AND authorized=TRUE`, pgInt64Array(remoteProductIDs(catalog.Products)))
	if err != nil {
		return nil, err
	}
	revoked, _ := result.RowsAffected()
	_, err = tx.ExecContext(ctx, `UPDATE moshu_reseller_connections SET status='active',catalog_etag=$1,catalog_version=$2,last_catalog_sync_at=NOW(),last_error=NULL,updated_at=NOW() WHERE id=1`, etag, catalog.CatalogVersion)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE moshu_catalog_sync_runs SET status='succeeded',to_version=$2,products_seen=$3,products_revoked=$4,completed_at=NOW() WHERE id=$1`, runID, catalog.CatalogVersion, len(catalog.Products), revoked)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.disableRevokedProducts(ctx); err != nil {
		return nil, err
	}
	if err := s.applyCatalogConfiguration(ctx); err != nil {
		return nil, err
	}
	return s.status(ctx, false)
}

func (s *Service) ConfigureProduct(ctx context.Context, id int64, selected bool, salesName string, salesMultiplier float64) (*Product, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	product, credentialCiphertext, err := s.loadProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	if !selected {
		return s.deactivateProduct(ctx, product)
	}
	if !product.Authorized {
		return nil, fmt.Errorf("%w: product authorization was revoked", ErrInvalidInput)
	}
	if math.IsNaN(salesMultiplier) || math.IsInf(salesMultiplier, 0) || salesMultiplier < 0 {
		return nil, fmt.Errorf("%w: invalid sales multiplier", ErrInvalidInput)
	}
	if strings.TrimSpace(salesName) == "" {
		salesName = product.DisplayName
	}
	if credentialCiphertext == "" {
		if _, err := s.rotateCredential(ctx, id); err != nil {
			return nil, err
		}
		product, credentialCiphertext, err = s.loadProduct(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	plainKey, err := s.encryptor.Decrypt(credentialCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt product credential: %w", err)
	}
	groupID, err := s.ensureGroup(ctx, *product, salesName, salesMultiplier)
	if err != nil {
		return nil, err
	}
	accountID, err := s.ensureAccount(ctx, *product, groupID, plainKey)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE moshu_products SET selected=TRUE,sales_rate_multiplier=$2,
		local_group_id=$3,local_account_id=$4,updated_at=NOW() WHERE id=$1`, id, salesMultiplier, groupID, accountID)
	if err != nil {
		return nil, err
	}
	updated, _, err := s.loadProduct(ctx, id)
	return updated, err
}

// deactivateProduct stops routing while tolerating resources that an
// administrator already removed from the normal group/account pages. Missing
// pointers are cleared so a later re-enable can safely recreate them.
func (s *Service) deactivateProduct(ctx context.Context, product *Product) (*Product, error) {
	clearGroup := false
	clearAccount := false
	if product.LocalGroupID != nil {
		disabled := service.StatusDisabled
		if _, err := s.admin.UpdateGroup(ctx, *product.LocalGroupID, &service.UpdateGroupInput{Platform: product.Platform, Status: disabled}); err != nil {
			if !errors.Is(err, service.ErrGroupNotFound) {
				return nil, err
			}
			clearGroup = true
		}
	}
	if product.LocalAccountID != nil {
		if _, err := s.admin.SetAccountSchedulable(ctx, *product.LocalAccountID, false); err != nil {
			if !errors.Is(err, service.ErrAccountNotFound) {
				return nil, err
			}
			clearAccount = true
		}
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE moshu_products SET selected=FALSE,
		local_group_id=CASE WHEN $2 THEN NULL ELSE local_group_id END,
		local_account_id=CASE WHEN $3 THEN NULL ELSE local_account_id END,
		updated_at=NOW() WHERE id=$1`, product.ID, clearGroup, clearAccount)
	if err != nil {
		return nil, err
	}
	product.Selected = false
	if clearGroup {
		product.LocalGroupID = nil
	}
	if clearAccount {
		product.LocalAccountID = nil
	}
	return product, nil
}

func (s *Service) RotateCredential(ctx context.Context, id int64) (*Product, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	return s.rotateCredential(ctx, id)
}

func (s *Service) rotateCredential(ctx context.Context, id int64) (*Product, error) {
	product, _, err := s.loadProduct(ctx, id)
	if err != nil {
		return nil, err
	}
	connection, token, err := s.authenticatedConnection(ctx)
	if err != nil {
		return nil, err
	}
	credential, err := s.client.rotate(ctx, connection.BaseURL, token, product.RemoteProductID)
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.encryptor.Encrypt(credential.APIKey)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE moshu_products SET credential_ciphertext=$2,credential_received_at=NOW(),updated_at=NOW() WHERE id=$1`, id, ciphertext); err != nil {
		return nil, err
	}
	if err = syncAccountCredentialTx(ctx, tx, product.RemoteProductID, credential.APIKey); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	updated, _, err := s.loadProduct(ctx, id)
	return updated, err
}

func (s *Service) SyncSettlements(ctx context.Context) (synced int, syncErr error) {
	defer func() {
		if syncErr != nil {
			s.recordSyncFailure(ctx, 0, syncErr)
		}
	}()
	connection, token, err := s.authenticatedConnection(ctx)
	if err != nil {
		return 0, err
	}
	var cursor int64
	if err := s.db.QueryRowContext(ctx, `SELECT last_remote_id FROM moshu_settlement_cursors WHERE id=1`).Scan(&cursor); err != nil {
		return 0, err
	}
	total := 0
	for {
		page, fetchErr := s.client.settlements(ctx, connection.BaseURL, token, cursor)
		if fetchErr != nil {
			return total, fetchErr
		}
		for _, settlement := range page.Items {
			if err := s.persistSettlement(ctx, settlement); err != nil {
				return total, err
			}
			if settlement.ID > cursor {
				cursor = settlement.ID
			}
			total++
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE moshu_settlement_cursors SET last_remote_id=$1,updated_at=NOW() WHERE id=1`, cursor); err != nil {
			return total, err
		}
		if page.NextCursor == 0 || len(page.Items) == 0 {
			break
		}
		cursor = page.NextCursor
	}
	if err := s.reconcilePendingProfits(ctx); err != nil {
		return total, fmt.Errorf("reconcile pending profits: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE moshu_reseller_connections SET status='active',last_settlement_sync_at=NOW(),last_error=NULL,updated_at=NOW() WHERE id=1`); err != nil {
		return total, err
	}
	return total, nil
}

func (s *Service) ListProfits(ctx context.Context, page, pageSize int) ([]ProfitRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	var total int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM moshu_request_profit_records`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id,request_id::text,product_code,requested_model,moshu_actual_cost,
		l1_customer_charge,gross_profit,settlement_status,remote_completed_at,created_at
		FROM moshu_request_profit_records ORDER BY id DESC LIMIT $1 OFFSET $2`, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]ProfitRecord, 0, pageSize)
	for rows.Next() {
		var item ProfitRecord
		var requested sql.NullString
		var completed sql.NullTime
		if err := rows.Scan(&item.ID, &item.RequestID, &item.ProductCode, &requested,
			&item.MoshuActualCost, &item.L1CustomerCharge, &item.GrossProfit,
			&item.SettlementStatus, &completed, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if requested.Valid {
			item.RequestedModel = &requested.String
		}
		if completed.Valid {
			item.RemoteCompletedAt = &completed.Time
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Service) authenticatedConnection(ctx context.Context) (*storedConnection, string, error) {
	if !Enabled() {
		return nil, "", ErrDisabled
	}
	connection, err := s.loadConnection(ctx)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && connection.Status != "active" && connection.Status != "error") {
		return nil, "", ErrNotConnected
	}
	if err != nil {
		return nil, "", err
	}
	if connection.AccessExpiresAt == nil || connection.AccessExpiresAt.Before(s.now().UTC().Add(time.Minute)) {
		// Catalog and settlement jobs can fire at the same instant. Refresh tokens
		// rotate on every use, so serialize refreshes and re-read state after taking
		// the lock to prevent the second job from replaying an already revoked token.
		s.authMu.Lock()
		defer s.authMu.Unlock()
		connection, err = s.loadConnection(ctx)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && connection.Status != "active" && connection.Status != "error") {
			return nil, "", ErrNotConnected
		}
		if err != nil {
			return nil, "", err
		}
		if connection.AccessExpiresAt != nil && !connection.AccessExpiresAt.Before(s.now().UTC().Add(time.Minute)) {
			access, decryptErr := s.encryptor.Decrypt(connection.AccessCiphertext)
			return connection, access, decryptErr
		}
		refresh, decErr := s.encryptor.Decrypt(connection.RefreshCiphertext)
		if decErr != nil {
			return nil, "", decErr
		}
		access, rotatedRefresh, expiresIn, refreshErr := s.client.refresh(ctx, connection.BaseURL, refresh, connection.InstanceID)
		if refreshErr != nil {
			return nil, "", refreshErr
		}
		accessCipher, encErr := s.encryptor.Encrypt(access)
		if encErr != nil {
			return nil, "", encErr
		}
		refreshCipher, encErr := s.encryptor.Encrypt(rotatedRefresh)
		if encErr != nil {
			return nil, "", encErr
		}
		expiresAt := s.now().UTC().Add(time.Duration(expiresIn) * time.Second)
		_, err = s.db.ExecContext(ctx, `UPDATE moshu_reseller_connections SET access_token_ciphertext=$1,refresh_token_ciphertext=$2,access_token_expires_at=$3,updated_at=NOW() WHERE id=1`, accessCipher, refreshCipher, expiresAt)
		if err != nil {
			return nil, "", err
		}
		connection.AccessCiphertext, connection.RefreshCiphertext, connection.AccessExpiresAt = accessCipher, refreshCipher, &expiresAt
		return connection, access, nil
	}
	access, err := s.encryptor.Decrypt(connection.AccessCiphertext)
	return connection, access, err
}

func (s *Service) loadConnection(ctx context.Context) (*storedConnection, error) {
	var result storedConnection
	var resellerID sql.NullInt64
	var lastCatalog, lastSettlement, accessExpires sql.NullTime
	var resellerName, protocol, accessCipher, refreshCipher, etag, lastError sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT base_url,instance_id::text,reseller_id,reseller_name,protocol_version,status,
		access_token_ciphertext,refresh_token_ciphertext,access_token_expires_at,
		catalog_etag,catalog_version,last_catalog_sync_at,last_settlement_sync_at,last_error
		FROM moshu_reseller_connections WHERE id=1`).Scan(
		&result.BaseURL, &result.InstanceID, &resellerID, &resellerName, &protocol, &result.Status,
		&accessCipher, &refreshCipher, &accessExpires, &etag, &result.CatalogVersion,
		&lastCatalog, &lastSettlement, &lastError)
	if err != nil {
		return nil, err
	}
	if resellerID.Valid {
		result.ResellerID = &resellerID.Int64
	}
	result.ResellerName, result.ProtocolVersion = resellerName.String, protocol.String
	result.AccessCiphertext, result.RefreshCiphertext, result.CatalogETag, result.LastError = accessCipher.String, refreshCipher.String, etag.String, lastError.String
	if accessExpires.Valid {
		result.AccessExpiresAt = &accessExpires.Time
	}
	if lastCatalog.Valid {
		result.LastCatalogSyncAt = &lastCatalog.Time
	}
	if lastSettlement.Valid {
		result.LastSettlementSyncAt = &lastSettlement.Time
	}
	return &result, nil
}

func (s *Service) listProducts(ctx context.Context) ([]Product, error) {
	rows, err := s.db.QueryContext(ctx, productSelect+` ORDER BY platform,product_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Product, 0)
	for rows.Next() {
		item, _, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Service) loadProduct(ctx context.Context, id int64) (*Product, string, error) {
	item, credential, err := scanProduct(s.db.QueryRowContext(ctx, productSelect+` WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return item, credential, err
}

const productSelect = `SELECT id,remote_product_id,product_code,display_name,platform,moshu_group_id,
	authorized,selected,cost_rate_multiplier,sales_rate_multiplier,price_catalog_version,
	model_snapshot,capabilities,credential_ciphertext,local_group_id,local_account_id,effective_at FROM moshu_products`

type rowScanner interface{ Scan(...any) error }

func scanProduct(row rowScanner) (*Product, string, error) {
	var product Product
	var sales sql.NullFloat64
	var models, capabilities []byte
	var credential sql.NullString
	var groupID, accountID sql.NullInt64
	err := row.Scan(&product.ID, &product.RemoteProductID, &product.ProductCode, &product.DisplayName,
		&product.Platform, &product.MoshuGroupID, &product.Authorized, &product.Selected,
		&product.CostRateMultiplier, &sales, &product.CatalogVersion, &models, &capabilities,
		&credential, &groupID, &accountID, &product.EffectiveAt)
	if err != nil {
		return nil, "", err
	}
	if sales.Valid {
		product.SalesRateMultiplier = &sales.Float64
	}
	if groupID.Valid {
		product.LocalGroupID = &groupID.Int64
	}
	if accountID.Valid {
		product.LocalAccountID = &accountID.Int64
	}
	product.CredentialAvailable = credential.Valid && credential.String != ""
	_ = json.Unmarshal(models, &product.Models)
	_ = json.Unmarshal(capabilities, &product.Capabilities)
	if product.Models == nil {
		product.Models = []string{}
	}
	if product.Capabilities == nil {
		product.Capabilities = map[string]any{}
	}
	return &product, credential.String, nil
}

func persistCatalogTx(ctx context.Context, tx *sql.Tx, catalog RemoteCatalog, credentials map[int64]string) error {
	for _, product := range catalog.Products {
		models, _ := json.Marshal(product.Models)
		capabilities, _ := json.Marshal(product.Capabilities)
		credential := ""
		if credentials != nil {
			credential = credentials[product.ID]
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO moshu_products
			(remote_product_id,product_code,display_name,platform,moshu_group_id,authorized,
			 cost_rate_multiplier,price_catalog_version,model_snapshot,capabilities,
			 credential_ciphertext,credential_received_at,effective_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10::jsonb,NULLIF($11,''),
			 CASE WHEN $11='' THEN NULL ELSE NOW() END,$12)
			ON CONFLICT(remote_product_id) DO UPDATE SET
			 product_code=EXCLUDED.product_code,display_name=EXCLUDED.display_name,
			 platform=EXCLUDED.platform,moshu_group_id=EXCLUDED.moshu_group_id,
			 authorized=EXCLUDED.authorized,cost_rate_multiplier=EXCLUDED.cost_rate_multiplier,
			 price_catalog_version=EXCLUDED.price_catalog_version,
			 model_snapshot=EXCLUDED.model_snapshot,capabilities=EXCLUDED.capabilities,
			 credential_ciphertext=COALESCE(EXCLUDED.credential_ciphertext,moshu_products.credential_ciphertext),
			 credential_received_at=CASE WHEN EXCLUDED.credential_ciphertext IS NULL THEN moshu_products.credential_received_at ELSE NOW() END,
			 effective_at=EXCLUDED.effective_at,updated_at=NOW()`, product.ID, product.ProductCode,
			product.DisplayName, product.Platform, product.MoshuGroupID, product.Enabled,
			product.CostRateMultiplier, product.PriceCatalogVersion, models, capabilities,
			credential, product.EffectiveAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ensureGroup(ctx context.Context, product Product, name string, salesMultiplier float64) (int64, error) {
	models := service.GroupModelsListConfig{Enabled: len(product.Models) > 0, Models: append([]string(nil), product.Models...)}
	createGroup := func() (int64, error) {
		group, err := s.admin.CreateGroup(service.WithResellerResourceProduct(ctx, product.ID), &service.CreateGroupInput{
			Name: name, Description: "Moshu reseller product: " + product.ProductCode,
			Platform: product.Platform, RateMultiplier: salesMultiplier, AllowZeroRateMultiplier: true,
			ModelsListConfig: models,
		})
		if err != nil {
			return 0, err
		}
		return group.ID, nil
	}
	if product.LocalGroupID == nil {
		return createGroup()
	}
	status := service.StatusActive
	group, err := s.admin.UpdateGroup(ctx, *product.LocalGroupID, &service.UpdateGroupInput{
		Name: name, Platform: product.Platform, RateMultiplier: &salesMultiplier, AllowZeroRateMultiplier: true,
		Status: status, ModelsListConfig: &models,
	})
	if err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			if _, clearErr := s.db.ExecContext(ctx, `UPDATE moshu_products SET local_group_id=NULL,selected=FALSE,updated_at=NOW() WHERE id=$1 AND local_group_id=$2`, product.ID, *product.LocalGroupID); clearErr != nil {
				return 0, clearErr
			}
			return createGroup()
		}
		return 0, err
	}
	return group.ID, nil
}

func (s *Service) ensureAccount(ctx context.Context, product Product, groupID int64, apiKey string) (int64, error) {
	connection, err := s.loadConnection(ctx)
	if err != nil {
		return 0, err
	}
	credentials := map[string]any{"api_key": apiKey, "base_url": connection.BaseURL}
	extra := map[string]any{
		"moshu_reseller_managed": true, "moshu_product_code": product.ProductCode,
		"moshu_remote_product_id": product.RemoteProductID, "moshu_cost_read_only": true,
	}
	groupIDs := []int64{groupID}
	rate := product.CostRateMultiplier
	if product.LocalAccountID == nil {
		return s.createProductAccount(ctx, product, groupID, credentials, extra, groupIDs, rate)
	}
	account, err := s.admin.GetAccount(ctx, *product.LocalAccountID)
	if err != nil {
		if errors.Is(err, service.ErrAccountNotFound) {
			if _, clearErr := s.db.ExecContext(ctx, `UPDATE moshu_products SET local_account_id=NULL,selected=FALSE,updated_at=NOW() WHERE id=$1 AND local_account_id=$2`, product.ID, *product.LocalAccountID); clearErr != nil {
				return 0, clearErr
			}
			return s.createProductAccount(ctx, product, groupID, credentials, extra, groupIDs, rate)
		}
		return 0, err
	}
	extra = mergeMap(account.Extra, extra)
	updated, err := s.admin.UpdateAccount(ctx, account.ID, &service.UpdateAccountInput{
		Name: account.Name, Type: account.Type, Credentials: credentials, Extra: extra,
		RateMultiplier: &rate, Status: service.StatusActive, GroupIDs: &groupIDs, SkipMixedChannelCheck: true,
	})
	if err != nil {
		return 0, err
	}
	if _, err := s.admin.SetAccountSchedulable(ctx, updated.ID, true); err != nil {
		return 0, err
	}
	return updated.ID, nil
}

func (s *Service) createProductAccount(ctx context.Context, product Product, groupID int64, credentials, extra map[string]any, groupIDs []int64, rate float64) (int64, error) {
	account, err := s.admin.CreateAccount(service.WithResellerResourceProduct(ctx, product.ID), &service.CreateAccountInput{
		Name: "Moshu - " + product.DisplayName, Platform: product.Platform, Type: service.AccountTypeAPIKey,
		Credentials: credentials, Extra: extra, Concurrency: 100, Priority: 50,
		RateMultiplier: &rate, GroupIDs: groupIDs, SkipDefaultGroupBind: true, SkipMixedChannelCheck: true,
	})
	if err != nil {
		return 0, err
	}
	return account.ID, nil
}

func (s *Service) disableRevokedProducts(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT local_group_id,local_account_id,platform FROM moshu_products WHERE authorized=FALSE AND selected=TRUE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type target struct {
		groupID, accountID sql.NullInt64
		platform           string
	}
	var targets []target
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.groupID, &item.accountID, &item.platform); err != nil {
			return err
		}
		targets = append(targets, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range targets {
		if item.groupID.Valid {
			if _, err := s.admin.UpdateGroup(ctx, item.groupID.Int64, &service.UpdateGroupInput{Platform: item.platform, Status: service.StatusDisabled}); err != nil {
				if !errors.Is(err, service.ErrGroupNotFound) {
					return err
				}
			}
		}
		if item.accountID.Valid {
			if _, err := s.admin.SetAccountSchedulable(ctx, item.accountID.Int64, false); err != nil {
				if !errors.Is(err, service.ErrAccountNotFound) {
					return err
				}
			}
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE moshu_products SET selected=FALSE,updated_at=NOW() WHERE authorized=FALSE AND selected=TRUE`)
	return err
}

func (s *Service) persistSettlement(ctx context.Context, settlement RemoteSettlement) error {
	var localGroupID, usageLogID sql.NullInt64
	var salesMultiplier, customerCharge sql.NullFloat64
	err := s.db.QueryRowContext(ctx, `
		SELECT mp.local_group_id,mp.sales_rate_multiplier,ul.id,ul.actual_cost
		FROM moshu_products mp
		LEFT JOIN usage_logs ul ON ul.request_id IN ($2,'client:' || $2,'local:' || $2)
		WHERE mp.remote_product_id=$1 ORDER BY ul.id DESC NULLS LAST LIMIT 1`, settlement.ProductID, settlement.RequestID).
		Scan(&localGroupID, &salesMultiplier, &usageLogID, &customerCharge)
	if err != nil {
		return err
	}
	charge := customerCharge.Float64
	grossProfit := charge - settlement.ActualCost
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO moshu_request_profit_records
		(remote_settlement_id,request_id,remote_product_id,product_code,local_group_id,
		 local_usage_log_id,requested_model,upstream_model,service_tier,standard_cost,
		 moshu_cost_rate_multiplier,moshu_actual_cost,l1_sales_rate_multiplier,
		 l1_customer_charge,gross_profit,price_catalog_version,settlement_status,remote_completed_at)
		VALUES($1,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
		ON CONFLICT(remote_settlement_id) DO UPDATE SET
		 local_group_id=EXCLUDED.local_group_id,local_usage_log_id=EXCLUDED.local_usage_log_id,
		 l1_sales_rate_multiplier=EXCLUDED.l1_sales_rate_multiplier,
		 l1_customer_charge=EXCLUDED.l1_customer_charge,gross_profit=EXCLUDED.gross_profit,
		 settlement_status=EXCLUDED.settlement_status,remote_completed_at=EXCLUDED.remote_completed_at,
		 updated_at=NOW()`, settlement.ID, settlement.RequestID, settlement.ProductID,
		settlement.ProductCode, nullableInt64(localGroupID), nullableInt64(usageLogID),
		settlement.RequestedModel, settlement.UpstreamModel, settlement.ServiceTier,
		settlement.StandardCost, settlement.CostRateMultiplier, settlement.ActualCost,
		salesMultiplier.Float64, charge, grossProfit, settlement.PriceCatalogVersion,
		settlement.Status, settlement.CompletedAt)
	return err
}

func (s *Service) reconcilePendingProfits(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		WITH matches AS (
		 SELECT pending.id,ul.id AS usage_id,ul.actual_cost
		 FROM moshu_request_profit_records pending
		 JOIN LATERAL (
		  SELECT id,actual_cost FROM usage_logs
		  WHERE request_id IN (pending.request_id::text,'client:' || pending.request_id::text,'local:' || pending.request_id::text)
		  ORDER BY id DESC LIMIT 1
		 ) ul ON TRUE
		 WHERE pending.local_usage_log_id IS NULL
		 ORDER BY pending.id LIMIT 1000
		)
		UPDATE moshu_request_profit_records pr SET
		 local_usage_log_id=matches.usage_id,l1_customer_charge=matches.actual_cost,
		 gross_profit=matches.actual_cost-pr.moshu_actual_cost,updated_at=NOW()
		FROM matches WHERE pr.id=matches.id AND pr.local_usage_log_id IS NULL`)
	return err
}

func (s *Service) recordSyncFailure(ctx context.Context, runID int64, err error) {
	message := truncate(err.Error(), 1000)
	_, _ = s.db.ExecContext(ctx, `UPDATE moshu_reseller_connections SET status='error',last_error=$1,updated_at=NOW() WHERE id=1`, message)
	if runID > 0 {
		_, _ = s.db.ExecContext(ctx, `UPDATE moshu_catalog_sync_runs SET status='failed',error_message=$2,completed_at=NOW() WHERE id=$1`, runID, message)
	}
}

func remoteProductIDs(products []RemoteProduct) []int64 {
	ids := make([]int64, 0, len(products))
	for _, product := range products {
		ids = append(ids, product.ID)
	}
	return ids
}

func pgInt64Array(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func mergeMap(base, updates map[string]any) map[string]any {
	result := cloneMap(base)
	for key, value := range updates {
		result[key] = value
	}
	return result
}

func nullableInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
