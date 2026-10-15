package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// Revision isolates counters when any of the configured rule values changes.
func (r GroupFailureRule) Revision() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%d", r.Threshold, r.WindowSeconds, r.CooldownSeconds)))
	return hex.EncodeToString(sum[:8])
}

// GroupFailurePolicy contains only explicit overrides. An absent kind retains
// existing system behavior; an empty rules object restores it for every kind.
type GroupFailurePolicy struct {
	Rules map[GroupFailureKind]GroupFailureRule `json:"rules"`
}

func validateGroupFailurePolicy(policy *GroupFailurePolicy) error {
	invalid := func(message string) error {
		return infraerrors.BadRequest("GROUP_FAILURE_POLICY_INVALID", message)
	}
	if policy == nil || policy.Rules == nil {
		return invalid("rules must be an object; use an empty object to restore defaults")
	}
	for kind, rule := range policy.Rules {
		switch kind {
		case GroupFailureOverload, GroupFailureServerError, GroupFailureTimeout, GroupFailureRateLimit:
		default:
			return invalid(fmt.Sprintf("unknown failure kind %q", kind))
		}
		if rule.Threshold < 1 || rule.Threshold > 100 {
			return invalid(fmt.Sprintf("%s threshold must be between 1 and 100", kind))
		}
		if rule.WindowSeconds < 1 || rule.WindowSeconds > 3600 {
			return invalid(fmt.Sprintf("%s window_seconds must be between 1 and 3600", kind))
		}
		if rule.CooldownSeconds < 1 || rule.CooldownSeconds > 7200 {
			return invalid(fmt.Sprintf("%s cooldown_seconds must be between 1 and 7200", kind))
		}
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
	copyPolicy := &GroupFailurePolicy{Rules: make(map[GroupFailureKind]GroupFailureRule, len(policy.Rules))}
	for kind, rule := range policy.Rules {
		copyPolicy.Rules[kind] = rule
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
