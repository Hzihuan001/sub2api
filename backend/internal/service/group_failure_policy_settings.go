package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const groupFailurePolicySettingPrefix = "custom.gateway-resilience.group-failure-policy."

// GroupFailureKind identifies the recoverable failures that a group may override.
type GroupFailureKind string

const (
	GroupFailureOverload    GroupFailureKind = "overload"
	GroupFailureServerError GroupFailureKind = "server_error"
	GroupFailureTimeout     GroupFailureKind = "timeout"
	GroupFailureRateLimit   GroupFailureKind = "rate_limit"
)

// GroupFailureRule counts failures in a rolling window before cooling an account.
type GroupFailureRule struct {
	Threshold       int `json:"threshold"`
	WindowSeconds   int `json:"window_seconds"`
	CooldownSeconds int `json:"cooldown_seconds"`
}

// GroupFailureStatusRule is the status-code based form exposed by the admin
// API. A single rule may cover several status codes, but each code keeps an
// independent rolling counter. The legacy kind based rules below remain
// supported for stored settings and older callers.
type GroupFailureStatusRule struct {
	StatusCodes     []int `json:"status_codes"`
	Threshold       int   `json:"threshold"`
	WindowSeconds   int   `json:"window_seconds"`
	CooldownSeconds int   `json:"cooldown_seconds"`
}

func (r GroupFailureStatusRule) Revision() string {
	codes := append([]int(nil), r.StatusCodes...)
	sort.Ints(codes)
	parts := make([]string, len(codes))
	for i, code := range codes {
		parts[i] = strconv.Itoa(code)
	}
	return GroupFailureRule{Threshold: r.Threshold, WindowSeconds: r.WindowSeconds, CooldownSeconds: r.CooldownSeconds}.Revision() + ":" + strings.Join(parts, ",")
}

// Revision isolates counters when any of the configured rule values changes.
func (r GroupFailureRule) Revision() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d", r.Threshold, r.WindowSeconds, r.CooldownSeconds)))
	return hex.EncodeToString(sum[:8])
}

// GroupFailurePolicy contains only explicit overrides. The public form uses a
// status-code rule array; an empty array restores the system behavior.
type GroupFailurePolicy struct {
	// Rules is the pre-status-code representation. Keep it for source and
	// storage compatibility with the first version of this feature.
	Rules map[GroupFailureKind]GroupFailureRule `json:"-"`
	// StatusRules is serialized as the public "rules" array for new clients.
	StatusRules []GroupFailureStatusRule `json:"-"`
}

// UnmarshalJSON accepts both the original kind map and the new status-code
// array. This lets an upgraded binary read settings written by an older one.
func (p *GroupFailurePolicy) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields) != 1 {
		return errors.New("policy must contain only the rules field")
	}
	rawRules, ok := fields["rules"]
	rawRules = bytes.TrimSpace(rawRules)
	if !ok || len(rawRules) == 0 || string(rawRules) == "null" {
		return errors.New("rules must be an object or array")
	}
	if rawRules[0] == '[' {
		var rules []GroupFailureStatusRule
		decoder := json.NewDecoder(strings.NewReader(string(rawRules)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&rules); err != nil {
			return err
		}
		p.Rules = nil
		p.StatusRules = rules
		return nil
	}
	var rules map[GroupFailureKind]GroupFailureRule
	decoder := json.NewDecoder(strings.NewReader(string(rawRules)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return err
	}
	p.Rules = rules
	p.StatusRules = nil
	return nil
}

func (p GroupFailurePolicy) MarshalJSON() ([]byte, error) {
	if p.StatusRules != nil {
		return json.Marshal(struct {
			Rules []GroupFailureStatusRule `json:"rules"`
		}{Rules: p.StatusRules})
	}
	return json.Marshal(struct {
		Rules map[GroupFailureKind]GroupFailureRule `json:"rules"`
	}{Rules: p.Rules})
}

// Public returns the status-code representation used by the admin API. Older
// saved policies are expanded to their canonical status-code sets while the
// internal legacy map remains available to the runtime and old callers.
func (p GroupFailurePolicy) Public() *GroupFailurePolicy {
	if p.StatusRules != nil {
		out := &GroupFailurePolicy{StatusRules: make([]GroupFailureStatusRule, len(p.StatusRules))}
		for i, rule := range p.StatusRules {
			out.StatusRules[i] = rule
			out.StatusRules[i].StatusCodes = append([]int(nil), rule.StatusCodes...)
		}
		return out
	}
	order := []GroupFailureKind{GroupFailureOverload, GroupFailureServerError, GroupFailureTimeout, GroupFailureRateLimit}
	out := &GroupFailurePolicy{StatusRules: make([]GroupFailureStatusRule, 0, len(p.Rules))}
	for _, kind := range order {
		rule, ok := p.Rules[kind]
		if !ok {
			continue
		}
		out.StatusRules = append(out.StatusRules, GroupFailureStatusRule{
			StatusCodes: append([]int(nil), canonicalStatusCodes(kind)...),
			Threshold:   rule.Threshold, WindowSeconds: rule.WindowSeconds, CooldownSeconds: rule.CooldownSeconds,
		})
	}
	return out
}

func canonicalStatusCodes(kind GroupFailureKind) []int {
	switch kind {
	case GroupFailureOverload:
		return []int{529}
	case GroupFailureServerError:
		return []int{500, 502, 503, 520, 521, 523}
	case GroupFailureTimeout:
		return []int{504, 522, 524}
	case GroupFailureRateLimit:
		return []int{429}
	default:
		return nil
	}
}

func validateGroupFailurePolicy(policy *GroupFailurePolicy) error {
	invalid := func(message string) error {
		return infraerrors.BadRequest("GROUP_FAILURE_POLICY_INVALID", message)
	}
	if policy == nil || (policy.Rules == nil && policy.StatusRules == nil) {
		return invalid("rules must be an array (or a legacy object); use [] to restore defaults")
	}
	if policy.StatusRules != nil {
		seen := make(map[int]struct{})
		for i, rule := range policy.StatusRules {
			if len(rule.StatusCodes) == 0 {
				return invalid(fmt.Sprintf("rules[%d] status_codes must not be empty", i))
			}
			for _, code := range rule.StatusCodes {
				if code < 100 || code > 599 {
					return invalid(fmt.Sprintf("rules[%d] status code must be between 100 and 599", i))
				}
				if _, ok := seen[code]; ok {
					return invalid(fmt.Sprintf("status code %d is assigned to more than one rule", code))
				}
				seen[code] = struct{}{}
			}
			if err := validateGroupFailureRule(fmt.Sprintf("rules[%d]", i), GroupFailureRule{Threshold: rule.Threshold, WindowSeconds: rule.WindowSeconds, CooldownSeconds: rule.CooldownSeconds}); err != nil {
				return err
			}
		}
		return nil
	}
	for kind, rule := range policy.Rules {
		switch kind {
		case GroupFailureOverload, GroupFailureServerError, GroupFailureTimeout, GroupFailureRateLimit:
		default:
			return invalid(fmt.Sprintf("unknown failure kind %q", kind))
		}
		if err := validateGroupFailureRule(string(kind), rule); err != nil {
			return err
		}
	}
	return nil
}

func validateGroupFailureRule(name string, rule GroupFailureRule) error {
	if rule.Threshold < 1 || rule.Threshold > 100 {
		return infraerrors.BadRequest("GROUP_FAILURE_POLICY_INVALID", fmt.Sprintf("%s threshold must be between 1 and 100", name))
	}
	if rule.WindowSeconds < 1 || rule.WindowSeconds > 3600 {
		return infraerrors.BadRequest("GROUP_FAILURE_POLICY_INVALID", fmt.Sprintf("%s window_seconds must be between 1 and 3600", name))
	}
	if rule.CooldownSeconds < 1 || rule.CooldownSeconds > 7200 {
		return infraerrors.BadRequest("GROUP_FAILURE_POLICY_INVALID", fmt.Sprintf("%s cooldown_seconds must be between 1 and 7200", name))
	}
	return nil
}

func (s *SettingService) validateGroupFailurePolicyGroup(ctx context.Context, groupID int64) error {
	if groupID <= 0 {
		return infraerrors.BadRequest("GROUP_FAILURE_POLICY_INVALID", "group ID must be a positive integer")
	}
	if s == nil || s.settingRepo == nil || s.defaultSubGroupReader == nil {
		return errors.New("group failure policy settings are unavailable")
	}
	group, err := s.defaultSubGroupReader.GetByID(ctx, groupID)
	if err != nil {
		return fmt.Errorf("get group for failure policy: %w", err)
	}
	if group == nil || group.ID != groupID {
		return ErrGroupNotFound
	}
	return nil
}

// GetGroupFailurePolicy reads overrides on the failure path or in the admin
// API. Callers must not invoke it on the normal request scheduling path.
func (s *SettingService) GetGroupFailurePolicy(ctx context.Context, groupID int64) (*GroupFailurePolicy, error) {
	if err := s.validateGroupFailurePolicyGroup(ctx, groupID); err != nil {
		return nil, err
	}
	raw, err := s.settingRepo.GetValue(ctx, groupFailurePolicySettingPrefix+strconv.FormatInt(groupID, 10))
	if errors.Is(err, ErrSettingNotFound) {
		return &GroupFailurePolicy{Rules: map[GroupFailureKind]GroupFailureRule{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load group failure policy: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var policy *GroupFailurePolicy
	if err := decoder.Decode(&policy); err != nil {
		return nil, fmt.Errorf("decode stored group failure policy: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("stored group failure policy must contain one JSON object")
	}
	if err := validateGroupFailurePolicy(policy); err != nil {
		// Corrupt stored configuration is an internal error, not invalid client input.
		return nil, fmt.Errorf("invalid stored group failure policy: %v", err)
	}
	return policy, nil
}

// UpdateGroupFailurePolicy replaces the complete set of overrides for a group.
func (s *SettingService) UpdateGroupFailurePolicy(ctx context.Context, groupID int64, policy *GroupFailurePolicy) (*GroupFailurePolicy, error) {
	if err := s.validateGroupFailurePolicyGroup(ctx, groupID); err != nil {
		return nil, err
	}
	if err := validateGroupFailurePolicy(policy); err != nil {
		return nil, err
	}
	copyPolicy := &GroupFailurePolicy{}
	if policy.StatusRules != nil {
		copyPolicy.StatusRules = make([]GroupFailureStatusRule, len(policy.StatusRules))
		for i, rule := range policy.StatusRules {
			copyPolicy.StatusRules[i] = rule
			copyPolicy.StatusRules[i].StatusCodes = append([]int(nil), rule.StatusCodes...)
		}
	} else {
		copyPolicy.Rules = make(map[GroupFailureKind]GroupFailureRule, len(policy.Rules))
		for kind, rule := range policy.Rules {
			copyPolicy.Rules[kind] = rule
		}
	}
	data, err := json.Marshal(copyPolicy)
	if err != nil {
		return nil, fmt.Errorf("encode group failure policy: %w", err)
	}
	if err := s.settingRepo.Set(ctx, groupFailurePolicySettingPrefix+strconv.FormatInt(groupID, 10), string(data)); err != nil {
		return nil, fmt.Errorf("save group failure policy: %w", err)
	}
	return copyPolicy, nil
}
