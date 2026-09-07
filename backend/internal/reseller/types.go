package reseller

import "time"

const (
	ProtocolVersion = "v1"
	apiKeyPrefix    = "sk-rs_"
)

type Tenant struct {
	ID              int64           `json:"id"`
	UserID          int64           `json:"user_id"`
	BillingAccount  *BillingAccount `json:"billing_account,omitempty"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	ProtocolVersion string          `json:"protocol_version"`
	InstanceID      *string         `json:"instance_id,omitempty"`
	AllowedCIDRs    []string        `json:"allowed_cidrs"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// BillingAccount is the ordinary Moshu user that owns all reseller product keys.
// Balance and usage continue to use the existing user billing ledger.
type BillingAccount struct {
	Email         string  `json:"email"`
	Role          string  `json:"role"`
	Status        string  `json:"status"`
	Balance       float64 `json:"balance"`
	FrozenBalance float64 `json:"frozen_balance"`
}

type Product struct {
	ID                   int64          `json:"id"`
	ResellerID           int64          `json:"reseller_id"`
	MoshuGroupID         int64          `json:"moshu_group_id"`
	ProductCode          string         `json:"product_code"`
	DisplayName          string         `json:"display_name"`
	Platform             string         `json:"platform"`
	Enabled              bool           `json:"enabled"`
	CostRateMultiplier   float64        `json:"cost_rate_multiplier"`
	PriceCatalogVersion  int64          `json:"price_catalog_version"`
	Models               []string       `json:"models"`
	Capabilities         map[string]any `json:"capabilities"`
	EffectiveAt          time.Time      `json:"effective_at"`
	CredentialConfigured bool           `json:"credential_configured"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

type IssuedCredential struct {
	ProductID   int64      `json:"product_id"`
	ProductCode string     `json:"product_code"`
	APIKey      string     `json:"api_key"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type Catalog struct {
	ProtocolVersion string    `json:"protocol_version"`
	ResellerID      int64     `json:"reseller_id"`
	ResellerName    string    `json:"reseller_name"`
	CatalogVersion  int64     `json:"catalog_version"`
	GeneratedAt     time.Time `json:"generated_at"`
	Products        []Product `json:"products"`
}

type Settlement struct {
	ID                  int64      `json:"id"`
	RequestID           string     `json:"request_id"`
	UpstreamRequestID   *string    `json:"upstream_request_id,omitempty"`
	ProductID           int64      `json:"product_id"`
	ProductCode         string     `json:"product_code"`
	DisplayName         string     `json:"display_name"`
	MoshuGroupID        int64      `json:"moshu_group_id"`
	RequestedModel      *string    `json:"requested_model,omitempty"`
	UpstreamModel       *string    `json:"upstream_model,omitempty"`
	ServiceTier         *string    `json:"service_tier,omitempty"`
	InputTokens         int64      `json:"input_tokens"`
	OutputTokens        int64      `json:"output_tokens"`
	CacheCreationTokens int64      `json:"cache_creation_tokens"`
	CacheReadTokens     int64      `json:"cache_read_tokens"`
	ImageCount          int        `json:"image_count"`
	VideoCount          int        `json:"video_count"`
	StandardCost        float64    `json:"standard_cost"`
	CostRateMultiplier  float64    `json:"cost_rate_multiplier"`
	ActualCost          float64    `json:"actual_cost"`
	PriceCatalogVersion int64      `json:"price_catalog_version"`
	Status              string     `json:"status"`
	ErrorType           *string    `json:"error_type,omitempty"`
	StartedAt           time.Time  `json:"started_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

type AccessClaims struct {
	ResellerID int64
	InstanceID string
	ExpiresAt  time.Time
}

type EnrollmentExchangeResult struct {
	Tenant       Tenant             `json:"tenant"`
	AccessToken  string             `json:"access_token"`
	RefreshToken string             `json:"refresh_token"`
	ExpiresIn    int64              `json:"expires_in"`
	Catalog      Catalog            `json:"catalog"`
	Credentials  []IssuedCredential `json:"credentials"`
}

type Balance struct {
	Balance       float64 `json:"balance"`
	FrozenBalance float64 `json:"frozen_balance"`
	Warning       bool    `json:"warning"`
}

type ListSettlementsResult struct {
	Items      []Settlement `json:"items"`
	NextCursor int64        `json:"next_cursor,omitempty"`
}
