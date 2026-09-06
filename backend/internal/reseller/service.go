package reseller

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
	rotationOverlap = 10 * time.Minute
)

var (
	ErrDisabled          = errors.New("reseller protocol is disabled")
	ErrUnauthorized      = errors.New("invalid reseller credentials")
	ErrForbidden         = errors.New("reseller access denied")
	ErrNotFound          = errors.New("reseller resource not found")
	ErrEnrollmentInvalid = errors.New("enrollment code is invalid or expired")
	ErrDuplicateRequest  = errors.New("reseller request id was already used")
	ErrInvalidInput      = errors.New("invalid reseller request")
)

type Service struct {
	db       *sql.DB
	cfg      *config.Config
	apiKeys  *service.APIKeyService
	now      func() time.Time
	randRead func([]byte) (int, error)
}

func NewService(db *sql.DB, cfg *config.Config, apiKeys *service.APIKeyService) *Service {
	return &Service{db: db, cfg: cfg, apiKeys: apiKeys, now: time.Now, randRead: rand.Read}
}

func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MOSHU_RESELLER_SERVER_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Service) CreateTenant(ctx context.Context, userID int64, name string, allowedCIDRs []string) (*Tenant, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return nil, fmt.Errorf("%w: invalid tenant name", ErrInvalidInput)
	}
	if err := validateCIDRs(allowedCIDRs); err != nil {
		return nil, err
	}
	rawCIDRs, _ := json.Marshal(normalizeStrings(allowedCIDRs))
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO reseller_tenants (user_id, name, allowed_cidrs)
		SELECT $1, $2, $3::jsonb FROM users WHERE id = $1 AND deleted_at IS NULL
		RETURNING id, user_id, name, status, protocol_version, instance_id::text,
		          allowed_cidrs, created_at, updated_at`, userID, name, rawCIDRs)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: user does not exist", ErrInvalidInput)
	}
	return tenant, err
}

func (s *Service) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, status, protocol_version, instance_id::text,
		       allowed_cidrs, created_at, updated_at
		FROM reseller_tenants ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Tenant, 0)
	for rows.Next() {
		tenant, scanErr := scanTenant(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, *tenant)
	}
	return result, rows.Err()
}

func (s *Service) UpdateTenant(ctx context.Context, id int64, status string, allowedCIDRs []string) (*Tenant, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "active" && status != "suspended" && status != "disabled" {
		return nil, fmt.Errorf("%w: invalid tenant status", ErrInvalidInput)
	}
	if err := validateCIDRs(allowedCIDRs); err != nil {
		return nil, err
	}
	rawCIDRs, _ := json.Marshal(normalizeStrings(allowedCIDRs))
	row := s.db.QueryRowContext(ctx, `
		UPDATE reseller_tenants SET status=$2, allowed_cidrs=$3::jsonb, updated_at=NOW()
		WHERE id=$1
		RETURNING id, user_id, name, status, protocol_version, instance_id::text,
		          allowed_cidrs, created_at, updated_at`, id, status, rawCIDRs)
	tenant, err := scanTenant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return tenant, err
}

func (s *Service) UpsertProduct(ctx context.Context, resellerID, groupID int64, productCode, displayName string, enabled bool) (*Product, error) {
	productCode = strings.ToLower(strings.TrimSpace(productCode))
	displayName = strings.TrimSpace(displayName)
	if productCode == "" || len(productCode) > 80 || displayName == "" || len(displayName) > 120 {
		return nil, fmt.Errorf("%w: invalid product fields", ErrInvalidInput)
	}
	var platform string
	var costRate float64
	var modelsRaw []byte
	err := s.db.QueryRowContext(ctx, `
		SELECT platform, rate_multiplier, COALESCE(models_list_config, '{}'::jsonb)
		FROM groups WHERE id=$1 AND deleted_at IS NULL`, groupID).Scan(&platform, &costRate, &modelsRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: group does not exist", ErrInvalidInput)
	}
	if err != nil {
		return nil, err
	}
	models := extractModelsSnapshot(modelsRaw)
	modelsJSON, _ := json.Marshal(models)
	capabilitiesJSON, _ := json.Marshal(map[string]any{"platform": platform})
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO reseller_products (
			reseller_id, moshu_group_id, product_code, display_name, platform,
			enabled, cost_rate_multiplier, model_snapshot, capabilities
		) SELECT $1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb
		  WHERE EXISTS (SELECT 1 FROM reseller_tenants WHERE id=$1)
		ON CONFLICT (reseller_id, product_code) DO UPDATE SET
			moshu_group_id=EXCLUDED.moshu_group_id,
			display_name=EXCLUDED.display_name,
			platform=EXCLUDED.platform,
			enabled=EXCLUDED.enabled,
			cost_rate_multiplier=EXCLUDED.cost_rate_multiplier,
			model_snapshot=EXCLUDED.model_snapshot,
			capabilities=EXCLUDED.capabilities,
			price_catalog_version=reseller_products.price_catalog_version + 1,
			effective_at=NOW(), updated_at=NOW()
		RETURNING id, reseller_id, moshu_group_id, product_code, display_name,
		          platform, enabled, cost_rate_multiplier, price_catalog_version,
		          model_snapshot, capabilities, effective_at,
		          EXISTS(SELECT 1 FROM reseller_credentials rc WHERE rc.product_id=reseller_products.id AND rc.status='active'),
		          created_at, updated_at`, resellerID, groupID, productCode, displayName, platform, enabled, costRate, modelsJSON, capabilitiesJSON)
	product, err := scanProduct(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return product, err
}

func (s *Service) ListProducts(ctx context.Context, resellerID int64, enabledOnly bool) ([]Product, error) {
	query := `
		SELECT rp.id, rp.reseller_id, rp.moshu_group_id, rp.product_code,
		       rp.display_name, rp.platform, rp.enabled, rp.cost_rate_multiplier,
		       rp.price_catalog_version, rp.model_snapshot, rp.capabilities,
		       rp.effective_at,
		       EXISTS(SELECT 1 FROM reseller_credentials rc WHERE rc.product_id=rp.id AND rc.status='active'),
		       rp.created_at, rp.updated_at
		FROM reseller_products rp WHERE rp.reseller_id=$1`
	if enabledOnly {
		query += ` AND rp.enabled=TRUE`
	}
	query += ` ORDER BY rp.product_code, rp.id`
	rows, err := s.db.QueryContext(ctx, query, resellerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	products := make([]Product, 0)
	for rows.Next() {
		product, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		products = append(products, *product)
	}
	return products, rows.Err()
}

func (s *Service) CreateEnrollment(ctx context.Context, resellerID, createdBy int64, productIDs []int64, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		ttl = 30 * time.Minute
	}
	productIDs = normalizeIDs(productIDs)
	if len(productIDs) == 0 {
		return "", time.Time{}, fmt.Errorf("%w: at least one product is required", ErrInvalidInput)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM reseller_products
		WHERE reseller_id=$1 AND enabled=TRUE AND id = ANY($2::bigint[])`, resellerID, pgInt64Array(productIDs)).Scan(&count); err != nil {
		return "", time.Time{}, err
	}
	if count != len(productIDs) {
		return "", time.Time{}, fmt.Errorf("%w: product scope contains unavailable products", ErrInvalidInput)
	}
	code, err := s.randomToken("mre_", 32)
	if err != nil {
		return "", time.Time{}, err
	}
	hash := sha256.Sum256([]byte(code))
	expiresAt := s.now().UTC().Add(ttl)
	rawProducts, _ := json.Marshal(productIDs)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO reseller_enrollment_codes
		(reseller_id, code_hash, product_ids, expires_at, created_by)
		VALUES ($1,$2,$3::jsonb,$4,$5)`, resellerID, hash[:], rawProducts, expiresAt, nullablePositive(createdBy))
	return code, expiresAt, err
}

func (s *Service) ExchangeEnrollment(ctx context.Context, code, instanceID, remoteIP string) (*EnrollmentExchangeResult, error) {
	if !Enabled() {
		return nil, ErrDisabled
	}
	instanceUUID, err := uuid.Parse(strings.TrimSpace(instanceID))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid instance_id", ErrInvalidInput)
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(code)))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var enrollmentID, resellerID int64
	var productRaw []byte
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `
		SELECT id, reseller_id, product_ids, expires_at
		FROM reseller_enrollment_codes
		WHERE code_hash=$1 AND used_at IS NULL AND expires_at > NOW()
		FOR UPDATE`, hash[:]).Scan(&enrollmentID, &resellerID, &productRaw, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrEnrollmentInvalid
	}
	if err != nil {
		return nil, err
	}
	tenant, err := getTenantTx(ctx, tx, resellerID, true)
	if err != nil || tenant.Status != "active" || !ipAllowed(remoteIP, tenant.AllowedCIDRs) {
		return nil, ErrForbidden
	}
	if tenant.InstanceID != nil && *tenant.InstanceID != instanceUUID.String() {
		return nil, fmt.Errorf("%w: tenant is already bound to another instance", ErrInvalidInput)
	}

	var productIDs []int64
	if err := json.Unmarshal(productRaw, &productIDs); err != nil || len(productIDs) == 0 {
		return nil, ErrEnrollmentInvalid
	}
	products, err := listProductsTx(ctx, tx, resellerID, productIDs)
	if err != nil || len(products) != len(normalizeIDs(productIDs)) {
		return nil, ErrEnrollmentInvalid
	}

	credentials := make([]IssuedCredential, 0, len(products))
	for _, product := range products {
		issued, issueErr := s.rotateCredentialTx(ctx, tx, *tenant, product, rotationOverlap)
		if issueErr != nil {
			return nil, issueErr
		}
		credentials = append(credentials, issued)
	}

	if _, err = tx.ExecContext(ctx, `UPDATE reseller_tenants SET instance_id=$2, updated_at=NOW() WHERE id=$1`, resellerID, instanceUUID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reseller_enrollment_codes SET used_at=NOW(), used_instance_id=$2 WHERE id=$1`, enrollmentID, instanceUUID); err != nil {
		return nil, err
	}
	refresh, refreshID, refreshHash, err := s.newRefreshToken()
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO reseller_refresh_tokens (id,reseller_id,instance_id,token_hash,expires_at)
		VALUES ($1,$2,$3,$4,$5)`, refreshID, resellerID, instanceUUID, refreshHash, s.now().UTC().Add(refreshTokenTTL)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	tenant.InstanceID = stringPtr(instanceUUID.String())
	access, err := s.signAccessToken(resellerID, instanceUUID.String())
	if err != nil {
		return nil, err
	}
	catalog := buildCatalog(*tenant, products, s.now().UTC())
	return &EnrollmentExchangeResult{
		Tenant: *tenant, AccessToken: access, RefreshToken: refresh,
		ExpiresIn: int64(accessTokenTTL.Seconds()), Catalog: catalog, Credentials: credentials,
	}, nil
}

func (s *Service) RefreshAccessToken(ctx context.Context, refreshToken, instanceID, remoteIP string) (string, string, int64, error) {
	instanceUUID, err := uuid.Parse(strings.TrimSpace(instanceID))
	if err != nil {
		return "", "", 0, ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(refreshToken)))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", 0, err
	}
	defer func() { _ = tx.Rollback() }()
	var tokenID uuid.UUID
	var resellerID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id,reseller_id FROM reseller_refresh_tokens
		WHERE token_hash=$1 AND instance_id=$2 AND revoked_at IS NULL AND expires_at > NOW()
		FOR UPDATE`, hash[:], instanceUUID).Scan(&tokenID, &resellerID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, ErrUnauthorized
	}
	if err != nil {
		return "", "", 0, err
	}
	tenant, err := getTenantTx(ctx, tx, resellerID, false)
	if err != nil || tenant.Status != "active" || tenant.InstanceID == nil || *tenant.InstanceID != instanceUUID.String() || !ipAllowed(remoteIP, tenant.AllowedCIDRs) {
		return "", "", 0, ErrForbidden
	}
	newRefresh, newID, newHash, err := s.newRefreshToken()
	if err != nil {
		return "", "", 0, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reseller_refresh_tokens SET revoked_at=NOW(),last_used_at=NOW() WHERE id=$1`, tokenID); err != nil {
		return "", "", 0, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO reseller_refresh_tokens (id,reseller_id,instance_id,token_hash,expires_at) VALUES ($1,$2,$3,$4,$5)`, newID, resellerID, instanceUUID, newHash, s.now().UTC().Add(refreshTokenTTL)); err != nil {
		return "", "", 0, err
	}
	if err = tx.Commit(); err != nil {
		return "", "", 0, err
	}
	access, err := s.signAccessToken(resellerID, instanceUUID.String())
	return access, newRefresh, int64(accessTokenTTL.Seconds()), err
}

func (s *Service) ValidateAccessToken(ctx context.Context, rawToken, remoteIP string) (*AccessClaims, error) {
	if !Enabled() || s.cfg == nil || strings.TrimSpace(s.cfg.JWT.Secret) == "" {
		return nil, ErrUnauthorized
	}
	parsed, err := jwt.Parse(strings.TrimSpace(rawToken), func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthorized
		}
		return s.jwtKey(), nil
	}, jwt.WithIssuer("sub2api-reseller-v1"), jwt.WithAudience("sub2api-reseller-api"), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return nil, ErrUnauthorized
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || claims["typ"] != "reseller_access" {
		return nil, ErrUnauthorized
	}
	resellerID, err := numericClaim(claims["reseller_id"])
	if err != nil {
		return nil, ErrUnauthorized
	}
	instanceID, _ := claims["instance_id"].(string)
	if _, err := uuid.Parse(instanceID); err != nil {
		return nil, ErrUnauthorized
	}
	tenant, err := s.getTenant(ctx, resellerID)
	if err != nil || tenant.Status != "active" || tenant.InstanceID == nil || *tenant.InstanceID != instanceID || !ipAllowed(remoteIP, tenant.AllowedCIDRs) {
		return nil, ErrForbidden
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return nil, ErrUnauthorized
	}
	return &AccessClaims{ResellerID: resellerID, InstanceID: instanceID, ExpiresAt: exp.Time}, nil
}

func (s *Service) Catalog(ctx context.Context, resellerID int64) (*Catalog, error) {
	tenant, err := s.getTenant(ctx, resellerID)
	if err != nil {
		return nil, err
	}
	products, err := s.ListProducts(ctx, resellerID, true)
	if err != nil {
		return nil, err
	}
	catalog := buildCatalog(*tenant, products, s.now().UTC())
	return &catalog, nil
}

func (s *Service) RotateCredential(ctx context.Context, resellerID, productID int64) (*IssuedCredential, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	tenant, err := getTenantTx(ctx, tx, resellerID, true)
	if err != nil || tenant.Status != "active" {
		return nil, ErrForbidden
	}
	products, err := listProductsTx(ctx, tx, resellerID, []int64{productID})
	if err != nil || len(products) != 1 || !products[0].Enabled {
		return nil, ErrNotFound
	}
	issued, err := s.rotateCredentialTx(ctx, tx, *tenant, products[0], rotationOverlap)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &issued, nil
}

func (s *Service) ListSettlements(ctx context.Context, resellerID, afterID int64, limit int) (*ListSettlementsResult, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, settlementSelect+`
		WHERE rs.reseller_id=$1 AND rs.id > $2
		ORDER BY rs.id ASC LIMIT $3`, resellerID, afterID, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Settlement, 0, limit)
	for rows.Next() {
		item, scanErr := scanSettlement(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, *item)
	}
	result := &ListSettlementsResult{Items: items}
	if len(items) > limit {
		result.NextCursor = items[limit-1].ID
		result.Items = items[:limit]
	}
	return result, rows.Err()
}

func (s *Service) GetSettlement(ctx context.Context, resellerID int64, requestID string) (*Settlement, error) {
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, ErrNotFound
	}
	item, err := scanSettlement(s.db.QueryRowContext(ctx, settlementSelect+` WHERE rs.reseller_id=$1 AND rs.request_id=$2::uuid`, resellerID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Service) Balance(ctx context.Context, resellerID int64) (*Balance, error) {
	var result Balance
	err := s.db.QueryRowContext(ctx, `
		SELECT u.balance,u.frozen_balance,
		       (u.balance-u.frozen_balance) <= COALESCE(u.balance_notify_threshold,0)
		FROM reseller_tenants rt JOIN users u ON u.id=rt.user_id
		WHERE rt.id=$1`, resellerID).Scan(&result.Balance, &result.FrozenBalance, &result.Warning)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &result, err
}

func (s *Service) BeginGatewayRequest(c *gin.Context) error {
	value, exists := c.Get("api_key")
	if !exists {
		return nil
	}
	apiKey, ok := value.(*service.APIKey)
	if !ok || apiKey == nil || !strings.HasPrefix(apiKey.Key, apiKeyPrefix) {
		return nil
	}
	if !Enabled() {
		return ErrDisabled
	}
	requestID := strings.TrimSpace(c.GetHeader("X-Reseller-Request-ID"))
	requestUUID, err := uuid.Parse(requestID)
	if err != nil {
		return fmt.Errorf("%w: X-Reseller-Request-ID must be a UUID", ErrInvalidInput)
	}
	var resellerID, productID, groupID, catalogVersion int64
	var costRateMultiplier float64
	var allowedCIDRsRaw []byte
	err = s.db.QueryRowContext(c.Request.Context(), `
		SELECT rc.reseller_id,rc.product_id,rp.moshu_group_id,rp.price_catalog_version,
		       rp.cost_rate_multiplier,rt.allowed_cidrs
		FROM reseller_credentials rc
		JOIN reseller_products rp ON rp.id=rc.product_id
		JOIN reseller_tenants rt ON rt.id=rc.reseller_id
		WHERE rc.api_key_id=$1 AND rc.status IN ('active','retiring')
		  AND (rc.expires_at IS NULL OR rc.expires_at>NOW())
		  AND (rc.status<>'retiring' OR rc.overlap_until IS NULL OR rc.overlap_until>NOW())
		  AND rp.enabled=TRUE AND rt.status='active'`, apiKey.ID).Scan(
		&resellerID, &productID, &groupID, &catalogVersion, &costRateMultiplier, &allowedCIDRsRaw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	var allowedCIDRs []string
	if json.Unmarshal(allowedCIDRsRaw, &allowedCIDRs) != nil || !ipAllowed(c.ClientIP(), allowedCIDRs) {
		return ErrForbidden
	}
	ctx := context.WithValue(c.Request.Context(), ctxkey.ClientRequestID, requestUUID.String())
	c.Request = c.Request.WithContext(ctx)
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO reseller_request_reservations
		(request_id,reseller_id,product_id,api_key_id,moshu_group_id,
		 price_catalog_version,cost_rate_multiplier)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (reseller_id,request_id) DO NOTHING`, requestUUID, resellerID,
		productID, apiKey.ID, groupID, catalogVersion, costRateMultiplier)
	if err != nil {
		return err
	}
	if inserted, rowsErr := result.RowsAffected(); rowsErr != nil || inserted != 1 {
		if rowsErr != nil {
			return rowsErr
		}
		return ErrDuplicateRequest
	}
	c.Set("reseller_id", resellerID)
	c.Set("reseller_request_id", requestUUID.String())
	c.Set("reseller_product_id", productID)
	c.Set("reseller_group_id", groupID)
	c.Set("reseller_catalog_version", catalogVersion)
	c.Set("reseller_cost_rate_multiplier", costRateMultiplier)
	return nil
}

func (s *Service) FinishGatewayRequest(c *gin.Context) {
	resellerValue, ok := c.Get("reseller_id")
	if !ok {
		return
	}
	resellerID, ok := resellerValue.(int64)
	requestID, _ := c.Get("reseller_request_id")
	productID, productOK := int64ContextValue(c, "reseller_product_id")
	groupID, groupOK := int64ContextValue(c, "reseller_group_id")
	catalogVersion, versionOK := int64ContextValue(c, "reseller_catalog_version")
	costRate, rateOK := c.Get("reseller_cost_rate_multiplier")
	costRateMultiplier, costRateOK := costRate.(float64)
	if !ok || !productOK || !groupOK || !versionOK || !rateOK || !costRateOK ||
		resellerID <= 0 || requestID == nil || c.Writer.Status() < 400 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `
		INSERT INTO reseller_request_settlements
		(request_id,reseller_id,product_id,moshu_group_id,cost_rate_multiplier,
		 price_catalog_version,status,error_type,completed_at)
		VALUES ($1::uuid,$2,$3,$4,$5,$6,'failed',$7,NOW())
		ON CONFLICT (reseller_id,request_id) DO UPDATE SET
		 status='failed',error_type=EXCLUDED.error_type,completed_at=NOW(),updated_at=NOW()
		WHERE reseller_request_settlements.status<>'completed'`, requestID, resellerID,
		productID, groupID, costRateMultiplier, catalogVersion, "http_"+strconv.Itoa(c.Writer.Status()))
}

func int64ContextValue(c *gin.Context, key string) (int64, bool) {
	value, exists := c.Get(key)
	result, ok := value.(int64)
	return result, exists && ok && result > 0
}

func (s *Service) rotateCredentialTx(ctx context.Context, tx *sql.Tx, tenant Tenant, product Product, overlap time.Duration) (IssuedCredential, error) {
	// api_keys.key is VARCHAR(64); keep the dedicated prefix while staying below it.
	key, err := s.randomToken(apiKeyPrefix, 28)
	if err != nil {
		return IssuedCredential{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		UPDATE reseller_credentials SET status='retiring',overlap_until=$2,rotated_at=NOW(),updated_at=NOW()
		WHERE product_id=$1 AND status='active'`, product.ID, s.now().UTC().Add(overlap)); err != nil {
		return IssuedCredential{}, err
	}
	var apiKeyID int64
	name := "reseller:" + tenant.Name + ":" + product.ProductCode
	err = tx.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id,key,name,group_id,status,created_at,updated_at)
		VALUES ($1,$2,$3,$4,'active',NOW(),NOW()) RETURNING id`, tenant.UserID, key, name, product.MoshuGroupID).Scan(&apiKeyID)
	if err != nil {
		return IssuedCredential{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO reseller_credentials (reseller_id,product_id,api_key_id,status,rotated_at)
		VALUES ($1,$2,$3,'active',NOW())`, tenant.ID, product.ID, apiKeyID); err != nil {
		return IssuedCredential{}, err
	}
	return IssuedCredential{ProductID: product.ID, ProductCode: product.ProductCode, APIKey: key}, nil
}

func (s *Service) signAccessToken(resellerID int64, instanceID string) (string, error) {
	now := s.now().UTC()
	claims := jwt.MapClaims{
		"iss": "sub2api-reseller-v1", "aud": "sub2api-reseller-api",
		"sub": strconv.FormatInt(resellerID, 10), "typ": "reseller_access",
		"reseller_id": resellerID, "instance_id": instanceID,
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(accessTokenTTL).Unix(),
		"jti": uuid.NewString(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtKey())
}

func (s *Service) jwtKey() []byte {
	secret := ""
	if s.cfg != nil {
		secret = s.cfg.JWT.Secret
	}
	digest := sha256.Sum256([]byte("sub2api:reseller:v1\x00" + secret))
	return digest[:]
}

func (s *Service) newRefreshToken() (string, uuid.UUID, []byte, error) {
	raw, err := s.randomToken("mrr_", 48)
	if err != nil {
		return "", uuid.Nil, nil, err
	}
	digest := sha256.Sum256([]byte(raw))
	return raw, uuid.New(), digest[:], nil
}

func (s *Service) randomToken(prefix string, bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := s.randRead(buffer); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func (s *Service) getTenant(ctx context.Context, id int64) (*Tenant, error) {
	tenant, err := scanTenant(s.db.QueryRowContext(ctx, `
		SELECT id,user_id,name,status,protocol_version,instance_id::text,
		       allowed_cidrs,created_at,updated_at FROM reseller_tenants WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return tenant, err
}

type scanner interface{ Scan(...any) error }

func scanTenant(row scanner) (*Tenant, error) {
	var tenant Tenant
	var instance sql.NullString
	var rawCIDRs []byte
	if err := row.Scan(&tenant.ID, &tenant.UserID, &tenant.Name, &tenant.Status,
		&tenant.ProtocolVersion, &instance, &rawCIDRs, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
		return nil, err
	}
	if instance.Valid {
		tenant.InstanceID = &instance.String
	}
	_ = json.Unmarshal(rawCIDRs, &tenant.AllowedCIDRs)
	if tenant.AllowedCIDRs == nil {
		tenant.AllowedCIDRs = []string{}
	}
	return &tenant, nil
}

func scanProduct(row scanner) (*Product, error) {
	var product Product
	var modelsRaw, capabilitiesRaw []byte
	if err := row.Scan(&product.ID, &product.ResellerID, &product.MoshuGroupID,
		&product.ProductCode, &product.DisplayName, &product.Platform, &product.Enabled,
		&product.CostRateMultiplier, &product.PriceCatalogVersion, &modelsRaw,
		&capabilitiesRaw, &product.EffectiveAt, &product.CredentialConfigured,
		&product.CreatedAt, &product.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(modelsRaw, &product.Models)
	_ = json.Unmarshal(capabilitiesRaw, &product.Capabilities)
	if product.Models == nil {
		product.Models = []string{}
	}
	if product.Capabilities == nil {
		product.Capabilities = map[string]any{}
	}
	return &product, nil
}

func getTenantTx(ctx context.Context, tx *sql.Tx, id int64, lock bool) (*Tenant, error) {
	query := `SELECT id,user_id,name,status,protocol_version,instance_id::text,allowed_cidrs,created_at,updated_at FROM reseller_tenants WHERE id=$1`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanTenant(tx.QueryRowContext(ctx, query, id))
}

func listProductsTx(ctx context.Context, tx *sql.Tx, resellerID int64, ids []int64) ([]Product, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT rp.id,rp.reseller_id,rp.moshu_group_id,rp.product_code,rp.display_name,
		       rp.platform,rp.enabled,rp.cost_rate_multiplier,rp.price_catalog_version,
		       rp.model_snapshot,rp.capabilities,rp.effective_at,
		       EXISTS(SELECT 1 FROM reseller_credentials rc WHERE rc.product_id=rp.id AND rc.status='active'),
		       rp.created_at,rp.updated_at
		FROM reseller_products rp
		WHERE rp.reseller_id=$1 AND rp.id=ANY($2::bigint[]) AND rp.enabled=TRUE
		ORDER BY rp.product_code FOR UPDATE`, resellerID, pgInt64Array(normalizeIDs(ids)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	products := make([]Product, 0, len(ids))
	for rows.Next() {
		product, scanErr := scanProduct(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		products = append(products, *product)
	}
	return products, rows.Err()
}

func buildCatalog(tenant Tenant, products []Product, now time.Time) Catalog {
	var version int64
	for _, product := range products {
		if product.PriceCatalogVersion > version {
			version = product.PriceCatalogVersion
		}
	}
	return Catalog{ProtocolVersion: ProtocolVersion, ResellerID: tenant.ID, ResellerName: tenant.Name, CatalogVersion: version, GeneratedAt: now, Products: products}
}

const settlementSelect = `
	SELECT rs.id,rs.request_id::text,rs.upstream_request_id,rs.product_id,
	       rp.product_code,rp.display_name,rs.moshu_group_id,rs.requested_model,
	       rs.upstream_model,rs.service_tier,rs.input_tokens,rs.output_tokens,
	       rs.cache_creation_tokens,rs.cache_read_tokens,rs.image_count,
	       rs.video_count,rs.standard_cost,rs.cost_rate_multiplier,rs.actual_cost,
	       rs.price_catalog_version,rs.status,rs.error_type,rs.started_at,
	       rs.completed_at,rs.created_at
	FROM reseller_request_settlements rs
	JOIN reseller_products rp ON rp.id=rs.product_id `

func scanSettlement(row scanner) (*Settlement, error) {
	var item Settlement
	var upstreamID, requestedModel, upstreamModel, tier, errorType sql.NullString
	var completedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.RequestID, &upstreamID, &item.ProductID,
		&item.ProductCode, &item.DisplayName, &item.MoshuGroupID, &requestedModel,
		&upstreamModel, &tier, &item.InputTokens, &item.OutputTokens,
		&item.CacheCreationTokens, &item.CacheReadTokens, &item.ImageCount,
		&item.VideoCount, &item.StandardCost, &item.CostRateMultiplier,
		&item.ActualCost, &item.PriceCatalogVersion, &item.Status, &errorType,
		&item.StartedAt, &completedAt, &item.CreatedAt); err != nil {
		return nil, err
	}
	item.UpstreamRequestID = nullStringPtr(upstreamID)
	item.RequestedModel = nullStringPtr(requestedModel)
	item.UpstreamModel = nullStringPtr(upstreamModel)
	item.ServiceTier = nullStringPtr(tier)
	item.ErrorType = nullStringPtr(errorType)
	if completedAt.Valid {
		item.CompletedAt = &completedAt.Time
	}
	return &item, nil
}

func extractModelsSnapshot(raw []byte) []string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return []string{}
	}
	seen := map[string]struct{}{}
	result := make([]string, 0)
	var walk func(any)
	walk = func(current any) {
		switch v := current.(type) {
		case string:
			v = strings.TrimSpace(v)
			if v != "" {
				if _, ok := seen[v]; !ok {
					seen[v] = struct{}{}
					result = append(result, v)
				}
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for key, item := range v {
				if strings.Contains(strings.ToLower(key), "model") {
					walk(item)
				}
			}
		}
	}
	walk(value)
	return result
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func pgInt64Array(values []int64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(value, 10))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func validateCIDRs(values []string) error {
	for _, value := range normalizeStrings(values) {
		if net.ParseIP(value) != nil {
			continue
		}
		if _, _, err := net.ParseCIDR(value); err != nil {
			return fmt.Errorf("%w: invalid CIDR %q", ErrInvalidInput, value)
		}
	}
	return nil
}

func ipAllowed(remoteIP string, values []string) bool {
	if len(values) == 0 {
		return true
	}
	ip := net.ParseIP(strings.TrimSpace(remoteIP))
	if ip == nil {
		return false
	}
	for _, value := range values {
		if allowed := net.ParseIP(value); allowed != nil && allowed.Equal(ip) {
			return true
		}
		if _, block, err := net.ParseCIDR(value); err == nil && block.Contains(ip) {
			return true
		}
	}
	return false
}

func numericClaim(value any) (int64, error) {
	switch v := value.(type) {
	case float64:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, ErrUnauthorized
	}
}

func nullablePositive(value int64) any {
	if value > 0 {
		return value
	}
	return nil
}
func stringPtr(value string) *string { return &value }
func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
