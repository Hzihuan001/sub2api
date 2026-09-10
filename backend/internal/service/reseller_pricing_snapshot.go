package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ResellerPricingSnapshot is a price-only protocol object. It must never contain
// group permissions, routing, account credentials, users or private channel metadata.
// Group and channel prices stay separate to preserve native precedence and tiers.
type ResellerPricingSnapshot struct {
	Schema              int                             `json:"schema"`
	Revision            string                          `json:"revision"`
	Platform            string                          `json:"platform"`
	CostRate            float64                         `json:"cost_rate"`
	Group               ResellerGroupPrices             `json:"group"`
	ChannelPrices       []ChannelModelPricing           `json:"channel_prices"`
	BillingModelSource  string                          `json:"billing_model_source"`
	BillingModelMapping map[string]map[string]string    `json:"billing_model_mapping"`
	Defaults            map[string]*LiteLLMModelPricing `json:"defaults"`
	Fallbacks           map[string]*ModelPricing        `json:"fallbacks"`
	AbsentTokenPrices   []string                        `json:"absent_token_prices,omitempty"`
}

type ResellerGroupPrices struct {
	FreeOpenAIFast               bool                          `json:"free_openai_fast"`
	ModelPricing                 []ChannelModelPricing         `json:"model_pricing"`
	LongContextPricingEnabled    bool                          `json:"long_context_pricing_enabled"`
	ImagePrice1K                 *float64                      `json:"image_price_1k"`
	ImagePrice2K                 *float64                      `json:"image_price_2k"`
	ImagePrice4K                 *float64                      `json:"image_price_4k"`
	VideoPrice480P               *float64                      `json:"video_price_480p"`
	VideoPrice720P               *float64                      `json:"video_price_720p"`
	VideoPrice1080P              *float64                      `json:"video_price_1080p"`
	VideoModelPrices             map[string]map[string]float64 `json:"video_model_prices"`
	WebSearchPricePerCall        *float64                      `json:"web_search_price_per_call"`
	SearchPricePer1k             *float64                      `json:"search_price_per_1k"`
	AudioRealtimePricePerMin     *float64                      `json:"audio_realtime_price_per_min"`
	AudioTTSPricePerMillionChars *float64                      `json:"audio_tts_price_per_million_chars"`
	AudioSTTPricePerHour         *float64                      `json:"audio_stt_price_per_hour"`
	// These affect the upstream charge, not the L1 customer's local retail rate.
	PeakRateEnabled              bool    `json:"peak_rate_enabled"`
	PeakStart                    string  `json:"peak_start"`
	PeakEnd                      string  `json:"peak_end"`
	PeakRateMultiplier           float64 `json:"peak_rate_multiplier"`
	ImageRateIndependent         bool    `json:"image_rate_independent"`
	ImageRateMultiplier          float64 `json:"image_rate_multiplier"`
	VideoRateIndependent         bool    `json:"video_rate_independent"`
	VideoRateMultiplier          float64 `json:"video_rate_multiplier"`
	BatchImageDiscountMultiplier float64 `json:"batch_image_discount_multiplier"`
	BatchImageHoldMultiplier     float64 `json:"batch_image_hold_multiplier"`
}

func (s *BillingService) ExportResellerPricing(group *Group, channel *Channel, costRate float64) (*ResellerPricingSnapshot, error) {
	if group == nil || math.IsNaN(costRate) || math.IsInf(costRate, 0) || costRate < 0 {
		return nil, fmt.Errorf("invalid reseller pricing source")
	}
	result := &ResellerPricingSnapshot{
		Schema: 1, Platform: group.Platform, CostRate: costRate,
		Group: ResellerGroupPrices{
			FreeOpenAIFast: group.FreeOpenAIFast,
			ModelPricing:   group.ModelPricing, LongContextPricingEnabled: group.LongContextPricingEnabled,
			ImagePrice1K: group.ImagePrice1K, ImagePrice2K: group.ImagePrice2K, ImagePrice4K: group.ImagePrice4K,
			VideoPrice480P: group.VideoPrice480P, VideoPrice720P: group.VideoPrice720P, VideoPrice1080P: group.VideoPrice1080P,
			VideoModelPrices: group.VideoModelPrices, WebSearchPricePerCall: group.WebSearchPricePerCall,
			SearchPricePer1k: group.SearchPricePer1k, AudioRealtimePricePerMin: group.AudioRealtimePricePerMin,
			AudioTTSPricePerMillionChars: group.AudioTTSPricePerMillionChars, AudioSTTPricePerHour: group.AudioSTTPricePerHour,
			PeakRateEnabled: group.PeakRateEnabled, PeakStart: group.PeakStart, PeakEnd: group.PeakEnd, PeakRateMultiplier: group.PeakRateMultiplier,
			ImageRateIndependent: group.ImageRateIndependent, ImageRateMultiplier: group.ImageRateMultiplier,
			VideoRateIndependent: group.VideoRateIndependent, VideoRateMultiplier: group.VideoRateMultiplier,
			BatchImageDiscountMultiplier: group.BatchImageDiscountMultiplier, BatchImageHoldMultiplier: group.BatchImageHoldMultiplier,
		},
		Fallbacks: s.fallbackPrices,
	}
	if channel != nil {
		result.BillingModelSource = channel.BillingModelSource
		result.BillingModelMapping = make(map[string]map[string]string)
		for platform, mapping := range channel.ModelMapping {
			if isPlatformPricingMatch(group.Platform, platform) {
				result.BillingModelMapping[platform] = mapping
			}
		}
		for _, price := range channel.ModelPricing {
			if isPlatformPricingMatch(group.Platform, price.Platform) {
				result.ChannelPrices = append(result.ChannelPrices, price)
			}
		}
	}
	// Serialize under the price cache read lock to detach every nested pointer/map.
	if s.pricingService != nil {
		s.pricingService.mu.RLock()
		defer s.pricingService.mu.RUnlock()
		result.Defaults = s.pricingService.pricingData
		for name, price := range result.Defaults {
			if price != nil && price.TokenPricingAbsent {
				result.AbsentTokenPrices = append(result.AbsentTokenPrices, name)
			}
		}
		sort.Strings(result.AbsentTokenPrices)
	}
	detached, err := cloneResellerPricing(result)
	if err != nil {
		return nil, err
	}
	// Storage IDs and timestamps are not price inputs and must not churn revisions.
	for i := range detached.Group.ModelPricing {
		stripResellerPriceMetadata(&detached.Group.ModelPricing[i])
	}
	for i := range detached.ChannelPrices {
		stripResellerPriceMetadata(&detached.ChannelPrices[i])
	}
	detached.Revision, err = detached.contentRevision()
	return detached, err
}

func stripResellerPriceMetadata(price *ChannelModelPricing) {
	price.ID = 0
	price.ChannelID = 0
	price.CreatedAt = time.Time{}
	price.UpdatedAt = time.Time{}
	for i := range price.Intervals {
		price.Intervals[i].ID = 0
		price.Intervals[i].PricingID = 0
		price.Intervals[i].CreatedAt = time.Time{}
		price.Intervals[i].UpdatedAt = time.Time{}
	}
}

func cloneResellerPricing(source *ResellerPricingSnapshot) (*ResellerPricingSnapshot, error) {
	raw, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var result ResellerPricingSnapshot
	if err = json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *ResellerPricingSnapshot) contentRevision() (string, error) {
	copy := *s
	copy.Revision = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

// SealRevision is only for producing a detached protocol payload, never for
// validating an untrusted received snapshot (CompileResellerPricing does that).
func (s *ResellerPricingSnapshot) SealRevision() error {
	var err error
	s.Revision, err = s.contentRevision()
	return err
}

// ResellerPricingCalculator owns detached, immutable rule data. Building it is
// background work; evaluating it never fetches pricing or channels over the network.
type ResellerPricingCalculator struct {
	snapshot *ResellerPricingSnapshot
	billing  *BillingService
	resolver *ModelPricingResolver
	groupID  int64
}

func CompileResellerPricing(source *ResellerPricingSnapshot, localGroupID int64, cfg *config.Config) (*ResellerPricingCalculator, error) {
	if source == nil || source.Schema != 1 || localGroupID <= 0 {
		return nil, fmt.Errorf("unsupported reseller pricing snapshot")
	}
	detached, err := cloneResellerPricing(source)
	if err != nil {
		return nil, err
	}
	revision, err := detached.contentRevision()
	if err != nil || revision != detached.Revision || len(revision) != 64 {
		return nil, fmt.Errorf("reseller pricing revision mismatch")
	}
	if math.IsNaN(detached.CostRate) || math.IsInf(detached.CostRate, 0) || detached.CostRate < 0 {
		return nil, fmt.Errorf("invalid reseller cost rate")
	}
	pricing := &PricingService{cfg: cfg, pricingData: detached.Defaults}
	for _, name := range detached.AbsentTokenPrices {
		if price := pricing.pricingData[name]; price != nil {
			price.TokenPricingAbsent = true
		}
	}
	billing := &BillingService{cfg: cfg, pricingService: pricing, fallbackPrices: detached.Fallbacks}
	channel := Channel{ID: 1, Status: "active", GroupIDs: []int64{localGroupID}, ModelPricing: detached.ChannelPrices,
		BillingModelSource: detached.BillingModelSource, ModelMapping: detached.BillingModelMapping}
	channels := &ChannelService{frozenPricing: true}
	channels.cache.Store(populateChannelCache([]Channel{channel}, map[int64]string{localGroupID: detached.Platform}))
	return &ResellerPricingCalculator{snapshot: detached, billing: billing,
		resolver: NewModelPricingResolver(channels, billing), groupID: localGroupID}, nil
}

func (s *ResellerPricingCalculator) Revision() string  { return s.snapshot.Revision }
func (s *ResellerPricingCalculator) CostRate() float64 { return s.snapshot.CostRate }

// ApplyPriceFields builds a cost-only group, retaining local identity/routing.
// The returned object is request-owned; shared API key/group cache entries are untouched.
func (s *ResellerPricingCalculator) ApplyPriceFields(local *Group) *Group {
	result := Group{}
	if local != nil {
		result = *local
	}
	p := s.snapshot.Group
	result.ModelPricing = make([]ChannelModelPricing, len(p.ModelPricing))
	for i := range p.ModelPricing {
		result.ModelPricing[i] = p.ModelPricing[i].Clone()
	}
	result.LongContextPricingEnabled = p.LongContextPricingEnabled
	result.ImagePrice1K, result.ImagePrice2K, result.ImagePrice4K = cloneResellerFloat(p.ImagePrice1K), cloneResellerFloat(p.ImagePrice2K), cloneResellerFloat(p.ImagePrice4K)
	result.VideoPrice480P, result.VideoPrice720P, result.VideoPrice1080P = cloneResellerFloat(p.VideoPrice480P), cloneResellerFloat(p.VideoPrice720P), cloneResellerFloat(p.VideoPrice1080P)
	result.VideoModelPrices = make(map[string]map[string]float64, len(p.VideoModelPrices))
	for model, prices := range p.VideoModelPrices {
		result.VideoModelPrices[model] = make(map[string]float64, len(prices))
		for resolution, price := range prices {
			result.VideoModelPrices[model][resolution] = price
		}
	}
	result.WebSearchPricePerCall = cloneResellerFloat(p.WebSearchPricePerCall)
	result.SearchPricePer1k = cloneResellerFloat(p.SearchPricePer1k)
	result.AudioRealtimePricePerMin = cloneResellerFloat(p.AudioRealtimePricePerMin)
	result.AudioTTSPricePerMillionChars = cloneResellerFloat(p.AudioTTSPricePerMillionChars)
	result.AudioSTTPricePerHour = cloneResellerFloat(p.AudioSTTPricePerHour)
	return &result
}

func cloneResellerFloat(source *float64) *float64 {
	if source == nil {
		return nil
	}
	value := *source
	return &value
}

// Retail uses local explicit rules, with the complete upstream price ladder as
// fallback. Neither resolver writes into the local group or channel cache.
func (s *ResellerPricingCalculator) retailResolver(local *ModelPricingResolver) *ModelPricingResolver {
	var channels *ChannelService
	if local != nil {
		channels = local.channelService
	}
	return &ModelPricingResolver{channelService: channels, billingService: s.billing, resellerFallback: s}
}

func (s *ResellerPricingCalculator) CalculateRetail(input CostInput) (*CostBreakdown, error) {
	input.Resolver = s.retailResolver(input.Resolver)
	input.Resolved = nil
	if input.GroupID == nil && input.Group != nil {
		input.GroupID = &input.Group.ID
	}
	if input.PricingAt.IsZero() && input.Group != nil {
		input.PricingAt = input.Group.resellerPricingAt
	}
	return s.billing.CalculateCostUnified(input)
}

func (s *ResellerPricingCalculator) Calculate(input CostInput) (*CostBreakdown, error) {
	// The main site's free-fast policy changes its customer-facing base cost.
	// This policy is for upstream cost only, never local retail overrides.
	if s.snapshot.Group.FreeOpenAIFast && groupSupportsOpenAIFast(s.snapshot.Platform) {
		tier := normalizeBillingServiceTier(input.ServiceTier)
		if tier == "priority" || tier == "fast" {
			input.ServiceTier = ""
		}
	}
	if input.Ctx == nil {
		input.Ctx = context.Background()
	}
	input.Group = s.ApplyPriceFields(input.Group)
	if input.PricingAt.IsZero() {
		input.PricingAt = input.Group.resellerPricingAt
	}
	input.GroupID = &s.groupID
	input.Resolver = s.resolver
	// Never accept a result resolved against a different price version.
	input.Resolved = nil
	return s.billing.CalculateCostUnified(input)
}
