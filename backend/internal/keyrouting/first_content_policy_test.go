package keyrouting

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFirstContentPolicyProtocol(t *testing.T) {
	for _, raw := range []string{
		`{"rules":[{"type":"ttft","threshold":3,"sample_size":20,"first_content_seconds":15,"cooldown_seconds":120}]}`,
	} {
		var policy RouteFailurePolicy
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			t.Fatal(err)
		}
		if policy.FirstContentTimeout() != 15*time.Second {
			t.Fatal(policy)
		}
	}
	for _, raw := range []string{
		`{"rules":[{"type":"ttft","threshold":3,"sample_size":20,"first_content_seconds":4,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"ttft","threshold":3,"sample_size":20,"cooldown_seconds":120}]}`,
		`{"rules":[{"type":"timeout","threshold":3,"sample_size":20,"first_content_seconds":0,"cooldown_seconds":120}]}`,
	} {
		var policy RouteFailurePolicy
		if err := json.Unmarshal([]byte(raw), &policy); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestFirstContentSamplesAreIndependentAndRevised(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	tracker := NewFailureTracker(func() time.Time { return now })
	identity := RouteFailureIdentity{APIKeyID: 1, GroupID: 1, Model: "model", Endpoint: "chat"}
	policy := RouteFailurePolicy{Rules: []RouteFailureRule{{Type: FailureRuleTTFT, Threshold: 2, SampleSize: 3, FirstContentSeconds: 5, CooldownSeconds: 120}}}
	if tracker.RecordOutcome(identity, OutcomeTimeout, policy).Triggered {
		t.Fatal("outcomes must not count TTFT")
	}
	if tracker.RecordFirstContent(identity, 5*time.Second, policy).Triggered {
		t.Fatal("one sample")
	}
	if tracker.RecordFirstContent(identity, time.Second, policy).Triggered {
		t.Fatal("fast sample")
	}
	if !tracker.RecordFirstContent(identity, 6*time.Second, policy).Triggered {
		t.Fatal("two slow samples")
	}
	other := identity
	other.Model = "other"
	if cooling, _ := tracker.IsCooling(other, policy); cooling {
		t.Fatal("model isolation")
	}
	policy.Rules[0].FirstContentSeconds = 15
	if cooling, _ := tracker.IsCooling(identity, policy); cooling {
		t.Fatal("policy revision")
	}
	if tracker.RecordFirstContent(identity, 6*time.Second, policy).Triggered {
		t.Fatal("new threshold")
	}
}
