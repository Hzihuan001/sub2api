package dto

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// APIKeyUpdateRequest is shared by self-service and user-management editors.
// Keep omitted, explicit null and empty policy objects distinct on both paths.
type APIKeyUpdateRequest struct {
	Name                  string          `json:"name"`
	GroupID               *int64          `json:"group_id"`
	BackupGroupIDs        *[]int64        `json:"backup_group_ids"`
	KeyRouteScope         *string         `json:"key_route_scope"`
	KeyRouteRevision      json.RawMessage `json:"key_route_revision"`
	KeyRouteFailurePolicy json.RawMessage `json:"key_route_failure_policy"`
	KeyModelPolicy        json.RawMessage `json:"key_model_policy"`
	Status                string          `json:"status" binding:"omitempty,oneof=active inactive"`
	IPWhitelist           *[]string       `json:"ip_whitelist"`
	IPBlacklist           *[]string       `json:"ip_blacklist"`
	Quota                 *float64        `json:"quota"`
	ExpiresAt             *string         `json:"expires_at"`
	ResetQuota            *bool           `json:"reset_quota"`
	RateLimit5h           *float64        `json:"rate_limit_5h"`
	RateLimit1d           *float64        `json:"rate_limit_1d"`
	RateLimit7d           *float64        `json:"rate_limit_7d"`
	ResetRateLimitUsage   *bool           `json:"reset_rate_limit_usage"`
}

func (req APIKeyUpdateRequest) ToServiceRequest() (service.UpdateAPIKeyRequest, error) {
	out := service.UpdateAPIKeyRequest{
		GroupID: req.GroupID, BackupGroupIDs: req.BackupGroupIDs,
		KeyRouteScope: req.KeyRouteScope, KeyRouteRevision: req.KeyRouteRevision,
		KeyRouteFailurePolicySet: req.KeyRouteFailurePolicy != nil,
		KeyModelPolicySet:        req.KeyModelPolicy != nil,
		IPWhitelist:              req.IPWhitelist, IPBlacklist: req.IPBlacklist,
		Quota: req.Quota, ResetQuota: req.ResetQuota,
		RateLimit5h: req.RateLimit5h, RateLimit1d: req.RateLimit1d, RateLimit7d: req.RateLimit7d,
		ResetRateLimitUsage: req.ResetRateLimitUsage,
	}
	if req.Name != "" {
		out.Name = &req.Name
	}
	if req.Status != "" {
		out.Status = &req.Status
	}
	if len(req.KeyRouteFailurePolicy) > 0 && string(req.KeyRouteFailurePolicy) != "null" {
		if err := json.Unmarshal(req.KeyRouteFailurePolicy, &out.KeyRouteFailurePolicy); err != nil {
			return out, fmt.Errorf("invalid key_route_failure_policy: %w", err)
		}
	}
	if len(req.KeyModelPolicy) > 0 && string(req.KeyModelPolicy) != "null" {
		if err := json.Unmarshal(req.KeyModelPolicy, &out.KeyModelPolicy); err != nil {
			return out, fmt.Errorf("invalid key_model_policy: %w", err)
		}
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			out.ClearExpiration = true
		} else {
			parsed, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				return out, fmt.Errorf("invalid expires_at format: %w", err)
			}
			out.ExpiresAt = &parsed
		}
	}
	return out, nil
}

// HasFullEditFields preserves the historical group-only admin binding path.
func (req APIKeyUpdateRequest) HasFullEditFields() bool {
	return req.Name != "" || req.Status != "" || req.KeyRouteFailurePolicy != nil ||
		req.KeyModelPolicy != nil || req.IPWhitelist != nil || req.IPBlacklist != nil ||
		req.Quota != nil || req.ExpiresAt != nil || req.ResetQuota != nil ||
		req.RateLimit5h != nil || req.RateLimit1d != nil || req.RateLimit7d != nil
}
