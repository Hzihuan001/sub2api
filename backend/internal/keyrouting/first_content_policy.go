package keyrouting

import "time"

// FirstContentTimeout returns only an explicitly configured active TTFT limit.
// No hidden system timeout is added to inherited or explicitly disabled rules.
func (p RouteFailurePolicy) FirstContentTimeout() time.Duration {
	for _, rule := range p.Rules {
		if rule.ruleType() == FailureRuleTTFT && validRouteFailureRule(rule) {
			return time.Duration(rule.FirstContentSeconds) * time.Second
		}
	}
	return 0
}

// RecordFirstContent is independent from final failure/SLA outcomes. A slow
// successful last-resort request can cool the next request without replaying
// the success; an actively interrupted group records one slow sample only.
func (t *FailureTracker) RecordFirstContent(identity RouteFailureIdentity, elapsed time.Duration, policy RouteFailurePolicy) RouteFailureResult {
	result := RouteFailureResult{RuleIndex: -1}
	if t == nil || elapsed < 0 {
		return result
	}
	identity = normalizeIdentity(identity)
	now := t.nowTime()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked()
	t.touchIdentityLocked(identity, now)
	t.pruneIdentityLocked(identity, policyKeys(identity, policy), now)
	if cooling := t.coolingResultLocked(identity, policy, now); cooling.Triggered {
		return cooling
	}
	for index, rule := range policy.Rules {
		if rule.ruleType() != FailureRuleTTFT || !validRouteFailureRule(rule) {
			continue
		}
		key := routeFailureKey{identity: identity, rule: routeFailureRuleKey(rule)}
		if !t.ensureCounterLocked(key, now) {
			continue
		}
		outcome := OutcomeSuccess
		if elapsed >= time.Duration(rule.FirstContentSeconds)*time.Second {
			outcome = OutcomeTimeout
		}
		samples := append(t.outcomes[key], outcome)
		if len(samples) > rule.SampleSize {
			samples = samples[len(samples)-rule.SampleSize:]
		}
		count := 0
		for _, sample := range samples {
			if sample == OutcomeTimeout {
				count++
			}
		}
		if count < rule.Threshold {
			t.outcomes[key] = samples
			continue
		}
		until := now.Add(time.Duration(rule.CooldownSeconds) * time.Second)
		t.cooldowns[key] = until
		delete(t.outcomes, key)
		result = RouteFailureResult{Triggered: true, RuleIndex: index, Count: count, CooldownUntil: until}
	}
	return result
}
