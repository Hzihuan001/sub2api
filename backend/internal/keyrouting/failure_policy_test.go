package keyrouting

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRouteFailurePolicyStrictJSON(t *testing.T) {
	for _, input := range []string{
		`{"rules":[]}`,
		`{"rules":[{"status_codes":[502,503],"threshold":3,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"type":"status_code","status_codes":[529],"threshold":1,"window_seconds":1,"cooldown_seconds":1}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":3,"cooldown_seconds":120},{"type":"timeout","threshold":3,"sample_size":20,"cooldown_seconds":120},{"type":"rate_limit","threshold":3,"sample_size":20,"cooldown_seconds":120}]}`,
	} {
		t.Run(input, func(t *testing.T) {
			var policy RouteFailurePolicy
			if err := json.Unmarshal([]byte(input), &policy); err != nil {
				t.Fatalf("valid policy rejected: %v", err)
			}
			if err := policy.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, input := range []string{
		`null`, `{}`, `[]`, `{"rules":null}`, `{"rules":{}}`, `{"rules":[],"unknown":true}`,
		`{"rules":[],"rules":[]}`, `{"rules":[null]}`,
		`{"rules":[{"type":"ttft","threshold":3,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":3,"cooldown_seconds":120,"unknown":1}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":3,"cooldown_seconds":120,"window_seconds":0}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":3,"cooldown_seconds":120,"status_codes":[]}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":3,"cooldown_seconds":120,"sample_size":0}]}`,
		`{"rules":[{"type":"timeout","threshold":3,"sample_size":20,"cooldown_seconds":120,"window_seconds":0}]}`,
		`{"rules":[{"type":"timeout","threshold":21,"sample_size":20,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"timeout","threshold":1,"sample_size":101,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"timeout","threshold":1,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":0,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":101,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":1,"cooldown_seconds":7201}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":1,"cooldown_seconds":0}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":1,"cooldown_seconds":1},{"type":"consecutive_failures","threshold":2,"cooldown_seconds":2}]}`,
		`{"rules":[{"status_codes":[502,502],"threshold":3,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"status_codes":[502],"threshold":3,"window_seconds":60,"cooldown_seconds":600},{"status_codes":[502],"threshold":1,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"status_codes":[99],"threshold":3,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"status_codes":[600],"threshold":3,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"status_codes":[],"threshold":3,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"status_codes":[502],"threshold":3,"window_seconds":3601,"cooldown_seconds":600}]}`,
		`{"rules":[{"status_codes":[502],"threshold":3,"window_seconds":60,"cooldown_seconds":600,"sample_size":0}]}`,
		`{"rules":[{"type":null,"status_codes":[502],"threshold":3,"window_seconds":60,"cooldown_seconds":600}]}`,
		`{"rules":[{"type":"consecutive_failures","threshold":1,"threshold":2,"cooldown_seconds":120}]}`,
	} {
		t.Run("invalid "+input, func(t *testing.T) {
			var policy RouteFailurePolicy
			if err := json.Unmarshal([]byte(input), &policy); err == nil {
				t.Fatal("invalid or meaningless fields must be rejected")
			}
		})
	}
}

func TestRouteFailurePolicyRuleLimitAndClone(t *testing.T) {
	policy := &RouteFailurePolicy{Rules: []RouteFailureRule{}}
	for i := 0; i < MaxFailureRules; i++ {
		policy.Rules = append(policy.Rules, RouteFailureRule{StatusCodes: []int{500 + i}, Threshold: 1, WindowSeconds: 1, CooldownSeconds: 1})
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := policy.Public()
	clone.Rules[0].StatusCodes[0] = 400
	if policy.Rules[0].StatusCodes[0] != 500 {
		t.Fatal("public policy must be a deep copy")
	}
	policy.Rules = append(policy.Rules, RouteFailureRule{StatusCodes: []int{550}, Threshold: 1, WindowSeconds: 1, CooldownSeconds: 1})
	if err := policy.Validate(); err == nil {
		t.Fatal("more than 20 rules must be rejected")
	}
	var inherited *RouteFailurePolicy
	if inherited.Clone() != nil || inherited.Public() != nil || inherited.Validate() != nil {
		t.Fatal("nil policy must preserve inheritance")
	}
	disabled := (&RouteFailurePolicy{Rules: []RouteFailureRule{}}).Clone()
	if disabled.Rules == nil {
		t.Fatal("explicit [] must remain distinct from nil")
	}
	var missing RouteFailurePolicy
	if missing.Validate() == nil {
		t.Fatal("non-nil policy must include rules")
	}
}

func TestRouteFailurePolicySemanticJSONDoesNotLeakStatusFields(t *testing.T) {
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleTimeout, Threshold: 3, SampleSize: 20, CooldownSeconds: 120}}}
	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "status_codes") || strings.Contains(string(data), "window_seconds") {
		t.Fatalf("semantic policy must omit meaningless fields: %s", data)
	}
	var roundtrip RouteFailurePolicy
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.Rules[0].SampleSize != 20 {
		t.Fatal("sample_size lost in roundtrip")
	}
}

func TestRouteFailurePolicySLASuccessRateJSON(t *testing.T) {
	for _, input := range []string{
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":20,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":100,"sample_size":1,"cooldown_seconds":1}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":1,"sample_size":100,"cooldown_seconds":7200}]}`,
	} {
		t.Run(input, func(t *testing.T) {
			var policy RouteFailurePolicy
			if err := json.Unmarshal([]byte(input), &policy); err != nil {
				t.Fatalf("valid SLA policy rejected: %v", err)
			}
			data, err := json.Marshal(policy)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != input {
				t.Fatalf("SLA policy changed on roundtrip: %s", data)
			}
			if err := policy.Clone().Validate(); err != nil {
				t.Fatalf("cloned SLA policy invalid: %v", err)
			}
		})
	}
	for _, input := range []string{
		`{"rules":[{"type":"sla_success_rate","threshold":90,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":0,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":101,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":0,"sample_size":20,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":101,"sample_size":20,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":20,"cooldown_seconds":0}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":20,"cooldown_seconds":120,"status_codes":[]}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":20,"cooldown_seconds":120,"window_seconds":0}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":null,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":20,"cooldown_seconds":120,"success_rate_percent":90}]}`,
		`{"rules":[{"type":"sla_success_rate","threshold":90,"sample_size":20,"cooldown_seconds":120},{"type":"sla_success_rate","threshold":80,"sample_size":10,"cooldown_seconds":120}]}`,
	} {
		t.Run("invalid "+input, func(t *testing.T) {
			var policy RouteFailurePolicy
			if err := json.Unmarshal([]byte(input), &policy); err == nil {
				t.Fatal("invalid SLA policy must be rejected")
			}
		})
	}
}
