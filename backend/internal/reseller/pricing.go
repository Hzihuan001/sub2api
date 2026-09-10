package reseller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PricingCatalog struct {
	Schema     int                                        `json:"schema"`
	ResellerID int64                                      `json:"reseller_id"`
	Revision   string                                     `json:"revision"`
	Products   map[int64]*service.ResellerPricingSnapshot `json:"products"`
	Defaults   map[string]*service.LiteLLMModelPricing    `json:"defaults"`
	Fallbacks  map[string]*service.ModelPricing           `json:"fallbacks"`
}

func NewServiceWithPricing(db *sql.DB, cfg *config.Config, apiKeys *service.APIKeyService, admin service.AdminService, channels *service.ChannelService, billing *service.BillingService) *Service {
	s := NewService(db, cfg, apiKeys)
	s.pricingAdmin, s.pricingChannels, s.pricingBilling = admin, channels, billing
	return s
}

// PricingCatalog only exports active, enrolled products of the authenticated
// tenant. No caller-supplied group or account ID can expand its scope.
func (s *Service) PricingCatalog(ctx context.Context, resellerID int64) (*PricingCatalog, error) {
	if s.pricingAdmin == nil || s.pricingChannels == nil || s.pricingBilling == nil {
		return nil, fmt.Errorf("reseller pricing service is unavailable")
	}
	catalog, err := s.Catalog(ctx, resellerID)
	if err != nil {
		return nil, err
	}
	result := &PricingCatalog{Schema: 1, ResellerID: resellerID, Products: make(map[int64]*service.ResellerPricingSnapshot)}
	var absentTokenPrices []string
	for _, product := range catalog.Products {
		group, err := s.pricingAdmin.GetGroup(ctx, product.MoshuGroupID)
		if err != nil {
			return nil, err
		}
		channel, err := s.pricingChannels.GetChannelForGroup(ctx, group.ID)
		if err != nil {
			return nil, err
		}
		snapshot, err := s.pricingBilling.ExportResellerPricing(group, channel, product.CostRateMultiplier)
		if err != nil {
			return nil, err
		}
		result.Products[product.ID] = snapshot
		// The effective default catalog is shared by every product. Transmit it
		// once; each product revision still covers its full reconstructed rules.
		if result.Defaults == nil && result.Fallbacks == nil {
			result.Defaults, result.Fallbacks = snapshot.Defaults, snapshot.Fallbacks
			absentTokenPrices = snapshot.AbsentTokenPrices
		} else {
			// A default-price reload while exporting must not mix versions.
			snapshot.Defaults, snapshot.Fallbacks = result.Defaults, result.Fallbacks
			snapshot.AbsentTokenPrices = absentTokenPrices
			if err := snapshot.SealRevision(); err != nil {
				return nil, err
			}
		}
		snapshot.Defaults, snapshot.Fallbacks = nil, nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	result.Revision = hex.EncodeToString(hash[:])
	return result, nil
}

func (h *Handler) Pricing(c *gin.Context) {
	id, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	prices, err := h.service.PricingCatalog(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	etag := `"` + prices.Revision + `"`
	c.Header("Cache-Control", "private, no-cache")
	c.Header("ETag", etag)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	response.Success(c, prices)
}

// PricingChanges is a bounded authenticated long poll. L1 initiates the
// connection, so no arbitrary callback URLs or externally exposed L1 port is needed.
func (h *Handler) PricingChanges(c *gin.Context) {
	if _, ok := resellerIDFromContext(c); !ok {
		return
	}
	c.Header("Cache-Control", "no-store")
	cursor, changed := service.ResellerPricingChangeCursor()
	if c.Query("after") == cursor {
		timer := time.NewTimer(25 * time.Second)
		defer timer.Stop()
		select {
		case <-c.Request.Context().Done():
			return
		case <-changed:
		case <-timer.C:
		}
		cursor, _ = service.ResellerPricingChangeCursor()
	}
	response.Success(c, gin.H{"cursor": cursor})
}
