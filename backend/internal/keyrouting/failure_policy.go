package keyrouting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	FailureRuleStatusCode          = "status_code"
	FailureRuleConsecutiveFailures = "consecutive_failures"
	FailureRuleTimeout             = "timeout"
	FailureRuleRateLimit           = "rate_limit"
	FailureRuleSLASuccessRate      = "sla_success_rate"
	FailureRuleTTFT                = "ttft"
	MaxFailureRules                = 20
)

// RouteFailureRule is independent from administrator account suspension rules.
// An omitted type retains the original per-status rolling-window behavior.
type RouteFailureRule struct {
	Type        string `json:"type,omitempty"`
	StatusCodes []int  `json:"status_codes,omitempty"`
	// Threshold is a failure count, except for sla_success_rate where it is
	// the minimum success percentage (a strictly lower rate triggers).
	Threshold           int `json:"threshold"`
	WindowSeconds       int `json:"window_seconds,omitempty"`
	SampleSize          int `json:"sample_size,omitempty"`
	FirstContentSeconds int `json:"first_content_seconds,omitempty"`
	CooldownSeconds     int `json:"cooldown_seconds"`
}

// RouteFailurePolicy is a per-key override. Nil means inherit the selected
// group's status rules; a non-nil policy containing [] explicitly disables it.
type RouteFailurePolicy struct {
	Rules []RouteFailureRule `json:"rules"`
}

func (r RouteFailureRule) ruleType() string {
	if r.Type == "" {
		return FailureRuleStatusCode
	}
	return r.Type
}

// Validate rejects unknown types and fields whose semantics do not apply to
// that type. JSON decoding additionally rejects their explicit zero values.
func (p *RouteFailurePolicy) Validate() error {
	if p == nil {
		return nil
	}
	if p.Rules == nil {
		return errors.New("rules must be an array; use [] to disable the key rules")
	}
	if len(p.Rules) > MaxFailureRules {
		return fmt.Errorf("rules must contain at most %d entries", MaxFailureRules)
	}
	seenTypes := make(map[string]struct{})
	seenCodes := make(map[int]struct{})
	for i, rule := range p.Rules {
		if err := validateRouteFailureRule(rule); err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
		kind := rule.ruleType()
		if kind != FailureRuleStatusCode {
			if _, exists := seenTypes[kind]; exists {
				return fmt.Errorf("rule type %q may only appear once", kind)
			}
			seenTypes[kind] = struct{}{}
		}
		for _, status := range rule.StatusCodes {
			if _, exists := seenCodes[status]; exists {
				return fmt.Errorf("status code %d is assigned more than once", status)
			}
			seenCodes[status] = struct{}{}
		}
	}
	return nil
}

func validateRouteFailureRule(rule RouteFailureRule) error {
	if rule.ruleType() != FailureRuleTTFT && rule.FirstContentSeconds != 0 {
		return errors.New("first_content_seconds only applies to ttft rules")
	}
	if rule.Threshold < 1 || rule.Threshold > 100 {
		return errors.New("threshold must be between 1 and 100")
	}
	if rule.CooldownSeconds < 1 || rule.CooldownSeconds > 7200 {
		return errors.New("cooldown_seconds must be between 1 and 7200")
	}
	switch rule.ruleType() {
	case FailureRuleStatusCode:
		if len(rule.StatusCodes) == 0 {
			return errors.New("status_codes must not be empty")
		}
		for _, status := range rule.StatusCodes {
			if status < 100 || status > 599 {
				return errors.New("status codes must be between 100 and 599")
			}
		}
		if rule.WindowSeconds < 1 || rule.WindowSeconds > 3600 {
			return errors.New("window_seconds must be between 1 and 3600")
		}
		if rule.SampleSize != 0 {
			return errors.New("sample_size does not apply to status_code rules")
		}
	case FailureRuleConsecutiveFailures:
		if len(rule.StatusCodes) > 0 || rule.WindowSeconds != 0 || rule.SampleSize != 0 {
			return errors.New("only threshold and cooldown_seconds apply to consecutive_failures rules")
		}
	case FailureRuleTimeout, FailureRuleRateLimit, FailureRuleTTFT:
		if len(rule.StatusCodes) > 0 || rule.WindowSeconds != 0 {
			return errors.New("status_codes and window_seconds do not apply to sample rules")
		}
		if rule.SampleSize < 1 || rule.SampleSize > 100 {
			return errors.New("sample_size must be between 1 and 100")
		}
		if rule.Threshold > rule.SampleSize {
			return errors.New("threshold must not exceed sample_size")
		}
		if rule.ruleType() == FailureRuleTTFT && (rule.FirstContentSeconds < 5 || rule.FirstContentSeconds > 3600) {
			return errors.New("first_content_seconds must be between 5 and 3600")
		}
	case FailureRuleSLASuccessRate:
		if len(rule.StatusCodes) > 0 || rule.WindowSeconds != 0 {
			return errors.New("status_codes and window_seconds do not apply to sla_success_rate rules")
		}
		if rule.SampleSize < 1 || rule.SampleSize > 100 {
			return errors.New("sample_size must be between 1 and 100")
		}
	default:
		return fmt.Errorf("unknown rule type %q", rule.Type)
	}
	return nil
}

// Clone and Public never share mutable status-code slices with cached auth
// snapshots or handler requests. Empty [] remains distinct from nil.
func (p *RouteFailurePolicy) Clone() *RouteFailurePolicy {
	if p == nil {
		return nil
	}
	out := &RouteFailurePolicy{}
	if p.Rules != nil {
		out.Rules = make([]RouteFailureRule, len(p.Rules))
		for i, rule := range p.Rules {
			out.Rules[i] = rule
			out.Rules[i].StatusCodes = append([]int(nil), rule.StatusCodes...)
		}
	}
	return out
}

func (p *RouteFailurePolicy) Public() *RouteFailurePolicy { return p.Clone() }

func (p *RouteFailurePolicy) UnmarshalJSON(data []byte) error {
	fields, err := failurePolicyObject(data)
	if err != nil {
		return err
	}
	if len(fields) != 1 || fields["rules"] == nil {
		return errors.New("policy must contain only the rules field")
	}
	var decoded RouteFailurePolicy
	if err := json.Unmarshal(fields["rules"], &decoded.Rules); err != nil {
		return err
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

func (r *RouteFailureRule) UnmarshalJSON(data []byte) error {
	fields, err := failurePolicyObject(data)
	if err != nil {
		return err
	}
	type plainRule RouteFailureRule
	var decoded plainRule
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	rule := RouteFailureRule(decoded)
	allowed := map[string]bool{"type": true, "threshold": true, "cooldown_seconds": true}
	switch rule.ruleType() {
	case FailureRuleStatusCode:
		allowed["status_codes"], allowed["window_seconds"] = true, true
	case FailureRuleTimeout, FailureRuleRateLimit, FailureRuleSLASuccessRate, FailureRuleTTFT:
		allowed["sample_size"] = true
		if rule.ruleType() == FailureRuleTTFT {
			allowed["first_content_seconds"] = true
		}
	}
	for name, raw := range fields {
		if !allowed[name] {
			return fmt.Errorf("field %q does not apply to %s rules", name, rule.ruleType())
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("field %q must not be null", name)
		}
	}
	if err := validateRouteFailureRule(rule); err != nil {
		return err
	}
	*r = rule
	return nil
}

// Decode an object without silently accepting duplicate JSON field names.
func failurePolicyObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, errors.New("expected an object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, errors.New("expected a field name")
		}
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("duplicate field %q", name)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		fields[name] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected content after object")
	}
	return fields, nil
}
