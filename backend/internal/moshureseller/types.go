package moshureseller

import "time"

type Connection struct {
	BaseURL              string     `json:"base_url"`
	InstanceID           string     `json:"instance_id"`
	ResellerID           *int64     `json:"reseller_id,omitempty"`
	ResellerName         string     `json:"reseller_name"`
	ProtocolVersion      string     `json:"protocol_version"`
	Status               string     `json:"status"`
	CatalogVersion       int64      `json:"catalog_version"`
	LastCatalogSyncAt    *time.Time `json:"last_catalog_sync_at,omitempty"`
	LastSettlementSyncAt *time.Time `json:"last_settlement_sync_at,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
}

type Product struct {
	ID                  int64          `json:"id"`
	RemoteProductID     int64          `json:"remote_product_id"`
	ProductCode         string         `json:"product_code"`
	DisplayName         string         `json:"display_name"`
	Platform            string         `json:"platform"`
	MoshuGroupID        int64          `json:"moshu_group_id"`
	Authorized          bool           `json:"authorized"`
	Selected            bool           `json:"selected"`
	CostRateMultiplier  float64        `json:"cost_rate_multiplier"`
	SalesRateMultiplier *float64       `json:"sales_rate_multiplier,omitempty"`
	CatalogVersion      int64          `json:"price_catalog_version"`
	Models              []string       `json:"models"`
	Capabilities        map[string]any `json:"capabilities"`
	CredentialAvailable bool           `json:"credential_available"`
	LocalGroupID        *int64         `json:"local_group_id,omitempty"`
	LocalAccountID      *int64         `json:"local_account_id,omitempty"`
	Capacity            int            `json:"capacity"`
	EffectiveAt         time.Time      `json:"effective_at"`
}

type Status struct {
	Enabled    bool        `json:"enabled"`
	Connected  bool        `json:"connected"`
	Connection *Connection `json:"connection,omitempty"`
	Products   []Product   `json:"products"`
}

// Balance is the wallet state of the billing account bound to this reseller.
// It intentionally contains no account identity or credential fields.
type Balance struct {
	Balance       float64 `json:"balance"`
	FrozenBalance float64 `json:"frozen_balance"`
	Warning       bool    `json:"warning"`
}

type RemoteTenant struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	ProtocolVersion string `json:"protocol_version"`
}

type RemoteProduct struct {
	ID                  int64          `json:"id"`
	MoshuGroupID        int64          `json:"moshu_group_id"`
	ProductCode         string         `json:"product_code"`
	DisplayName         string         `json:"display_name"`
	Platform            string         `json:"platform"`
	Enabled             bool           `json:"enabled"`
	CostRateMultiplier  float64        `json:"cost_rate_multiplier"`
	PriceCatalogVersion int64          `json:"price_catalog_version"`
	Models              []string       `json:"models"`
	Capabilities        map[string]any `json:"capabilities"`
	EffectiveAt         time.Time      `json:"effective_at"`
}

type RemoteCatalog struct {
	ProtocolVersion string          `json:"protocol_version"`
	ResellerID      int64           `json:"reseller_id"`
	ResellerName    string          `json:"reseller_name"`
	CatalogVersion  int64           `json:"catalog_version"`
	GeneratedAt     time.Time       `json:"generated_at"`
	Products        []RemoteProduct `json:"products"`
}

type RemoteCredential struct {
	ProductID   int64  `json:"product_id"`
	ProductCode string `json:"product_code"`
	APIKey      string `json:"api_key"`
}

type EnrollmentExchange struct {
	Tenant       RemoteTenant       `json:"tenant"`
	AccessToken  string             `json:"access_token"`
	RefreshToken string             `json:"refresh_token"`
	ExpiresIn    int64              `json:"expires_in"`
	Catalog      RemoteCatalog      `json:"catalog"`
	Credentials  []RemoteCredential `json:"credentials"`
}

type RemoteSettlement struct {
	ID                  int64      `json:"id"`
	RequestID           string     `json:"request_id"`
	ProductID           int64      `json:"product_id"`
	ProductCode         string     `json:"product_code"`
	RequestedModel      *string    `json:"requested_model"`
	UpstreamModel       *string    `json:"upstream_model"`
	ServiceTier         *string    `json:"service_tier"`
	StandardCost        float64    `json:"standard_cost"`
	CostRateMultiplier  float64    `json:"cost_rate_multiplier"`
	ActualCost          float64    `json:"actual_cost"`
	PriceCatalogVersion int64      `json:"price_catalog_version"`
	Status              string     `json:"status"`
	CompletedAt         *time.Time `json:"completed_at"`
}

type RemoteSettlementPage struct {
	Items      []RemoteSettlement `json:"items"`
	NextCursor int64              `json:"next_cursor"`
}

type ProfitRecord struct {
	ID                int64      `json:"id"`
	RequestID         string     `json:"request_id"`
	ProductCode       string     `json:"product_code"`
	RequestedModel    *string    `json:"requested_model,omitempty"`
	MoshuActualCost   float64    `json:"moshu_actual_cost"`
	L1CustomerCharge  float64    `json:"l1_customer_charge"`
	GrossProfit       float64    `json:"gross_profit"`
	SettlementStatus  string     `json:"settlement_status"`
	RemoteCompletedAt *time.Time `json:"remote_completed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}
