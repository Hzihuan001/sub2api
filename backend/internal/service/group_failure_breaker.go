package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// Counters are per group/account/error/rule revision, but cooldown is account
// wide, intentionally using the existing durable overload field and scheduler.
// Only failed upstream attempts call this adapter; successful requests and
// account selection never load settings or add a remote query.
type AccountGroupFailureCache interface {
	RecordAccountGroupFailure(context.Context, int64, int64, string, string, time.Duration, int, time.Duration) (time.Time, bool, error)
	ClearAccountGroupFailures(context.Context, int64) error
}

// accountOverloadOnlyClearer is implemented by the production account
// repository. It deliberately remains optional so the broad AccountRepository
// test doubles and older integrations do not need a breaking interface change.
type accountOverloadOnlyClearer interface {
	ClearOverload(context.Context, int64) error
}

// ResetGroupFailurePolicy clears stale state left by a policy whose last rule
// was deleted. Group failure pauses are stored in the account-wide overload
// field, so the reset intentionally clears the account's overload cooldown as
// well as the rolling counters. It does not clear rate-limit or model-limit
// fields. The scheduler snapshot is refreshed by the repository and the
// runtime blocker is released in the same operation.
func (s *RateLimitService) ResetGroupFailurePolicy(ctx context.Context, groupID int64) error {
	if s == nil || s.accountRepo == nil || groupID <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	accounts, err := s.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return fmt.Errorf("list accounts for group failure policy reset: %w", err)
	}
	var firstErr error
	for i := range accounts {
		accountID := accounts[i].ID
		if accountID <= 0 {
			continue
		}
		operation := s.accountOverloadOperationLock(accountID)
		operation.Lock()

		if clearErr := s.clearAccountGroupFailures(ctx, accountID); clearErr != nil && firstErr == nil {
			firstErr = clearErr
		}
		s.overloadFailures.mu.Lock()
		delete(s.overloadFailures.entries, accountID)
		s.overloadFailures.mu.Unlock()

		if clearer, ok := s.accountRepo.(accountOverloadOnlyClearer); ok {
			if clearErr := clearer.ClearOverload(ctx, accountID); clearErr != nil && firstErr == nil {
				firstErr = clearErr
			}
		}
		if s.runtimeBlocker != nil {
			s.runtimeBlocker.ClearAccountSchedulingBlock(accountID)
		}
		operation.Unlock()
	}
	return firstErr
}

func (s *RateLimitService) ApplyGroupFailurePolicy(ctx context.Context, account *Account, kind GroupFailureKind) (bool, time.Time) {
	if s == nil || s.settingService == nil || ctx == nil || account == nil || account.ID <= 0 {
		return false, time.Time{}
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if group == nil || group.ID <= 0 {
		return false, time.Time{}
	}
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	policy, err := s.settingService.GetGroupFailurePolicy(loadCtx, group.ID)
	cancel()
	if err != nil {
		slog.Warn("group_failure_policy_load_failed", "group_id", group.ID, "error", err)
		return false, time.Time{} // A failed/absent policy must not create a system pause.
	}
	if policy == nil {
		return false, time.Time{}
	}
	rule, configured := policy.Rules[kind]
	// A status-code policy must never fall back to the semantic kind. Doing so
	// would make 502 and 503 share a counter even when only one was configured.
	if policy.StatusRules != nil {
		return false, time.Time{}
	}
	if !configured {
		return false, time.Time{}
	}
	return s.applyGroupFailureRule(ctx, account, group.ID, kind, rule)
}

// ApplyGroupFailurePolicyStatus applies a rule selected by the exact
// upstream HTTP status. Each status code gets its own counter, even when
// several codes are listed on the same rule.
func (s *RateLimitService) ApplyGroupFailurePolicyStatus(ctx context.Context, account *Account, status int) (bool, time.Time) {
	if s == nil || s.settingService == nil || ctx == nil || account == nil || account.ID <= 0 || status < 100 || status > 599 {
		return false, time.Time{}
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if group == nil || group.ID <= 0 {
		return false, time.Time{}
	}
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	policy, err := s.settingService.GetGroupFailurePolicy(loadCtx, group.ID)
	cancel()
	if err != nil {
		slog.Warn("group_failure_policy_load_failed", "group_id", group.ID, "error", err)
		return false, time.Time{}
	}
	if policy == nil {
		return false, time.Time{}
	}
	for _, candidate := range policy.StatusRules {
		for _, code := range candidate.StatusCodes {
			if code == status {
				rule := GroupFailureRule{Threshold: candidate.Threshold, WindowSeconds: candidate.WindowSeconds, CooldownSeconds: candidate.CooldownSeconds}
				return s.applyGroupFailureRule(ctx, account, group.ID, GroupFailureKind("status_"+strconv.Itoa(status)), rule)
			}
		}
	}
	return false, time.Time{}
}

func (s *RateLimitService) applyGroupFailureRule(ctx context.Context, account *Account, groupID int64, kind GroupFailureKind, rule GroupFailureRule) (bool, time.Time) {
	if s == nil || account == nil || groupID <= 0 {
		return false, time.Time{}
	}
	operation := s.accountOverloadOperationLock(account.ID)
	operation.Lock()
	defer operation.Unlock()
	now := time.Now()
	var err error
	cooldown := time.Duration(rule.CooldownSeconds) * time.Second
	window := time.Duration(rule.WindowSeconds) * time.Second
	key := groupFailureLocalKey{account.ID, groupID, kind, rule.Revision()}
	cache, _ := s.tempUnschedCache.(AccountGroupFailureCache)
	ack, _ := s.tempUnschedCache.(AccountOverloadFailureCache)
	until := s.overloadFailures.coolingUntil(account.ID, now)
	claim, persisted := false, false
	if !until.IsZero() {
		_, persisted = s.overloadFailures.persistedCooldown(account.ID, now)
		if !persisted {
			until, claim = s.overloadFailures.record(account.ID, now, cooldown)
		}
	} else {
		if cache != nil {
			cacheCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			until, claim, err = cache.RecordAccountGroupFailure(cacheCtx, account.ID, groupID, string(kind), key.revision, window, rule.Threshold, cooldown)
			stop()
			if err != nil {
				slog.Warn("group_failure_counter_failed", "account_id", account.ID, "group_id", groupID, "error", err)
				cache = nil
			}
		}
		if cache == nil {
			if !s.groupFailures.observe(key, now, window, rule.Threshold) {
				return true, time.Time{}
			}
			until, claim = now.Add(cooldown).Truncate(time.Millisecond), true
		} else if until.IsZero() {
			s.groupFailures.observe(key, now, window, rule.Threshold)
			return true, time.Time{}
		}
		persisted = s.overloadFailures.rememberCooldown(account.ID, now, until)
		if cache == nil && s.overloadFailures.coolingUntil(account.ID, now).IsZero() {
			// Match the existing bounded fallback: without Redis or room to retain
			// a fixed deadline, do not persist a new, endlessly extendable cooldown.
			slog.Warn("group_failure_local_capacity_reached", "account_id", account.ID)
			return true, time.Time{}
		}
		s.groupFailures.clear(account.ID)
	}
	s.notifyAccountSchedulingBlocked(account, until, "group_failure_"+string(kind))
	if claim && !persisted {
		if s.accountRepo != nil {
			persistCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			err = s.accountRepo.SetOverloaded(persistCtx, account.ID, until)
			stop()
			persisted = err == nil
			if err != nil {
				slog.Warn("group_failure_cooldown_persist_failed", "account_id", account.ID, "error", err)
			} else {
				slog.Info("group_failure_cooldown_started", "account_id", account.ID, "group_id", groupID, "kind", kind,
					"threshold", rule.Threshold, "window_seconds", rule.WindowSeconds, "until", until)
			}
		}
		s.overloadFailures.complete(account.ID, until, persisted)
	}
	if ack != nil && (claim || persisted) {
		s.completeAccountOverloadCache(ctx, ack, account.ID, until, persisted)
	}
	return true, until
}

// Caller holds the same account operation lock used by observations.
func (s *RateLimitService) clearAccountGroupFailures(ctx context.Context, accountID int64) error {
	if cache, ok := s.tempUnschedCache.(AccountGroupFailureCache); ok {
		clearCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := cache.ClearAccountGroupFailures(clearCtx, accountID); err != nil {
			return fmt.Errorf("clear group failure counters: %w", err)
		}
	}
	s.groupFailures.clear(accountID)
	return nil
}

type groupFailureLocalKey struct {
	accountID int64
	groupID   int64
	kind      GroupFailureKind
	revision  string
}

type groupFailureLocalEntry struct {
	failures []time.Time
	expires  time.Time
}

type groupFailureLocalState struct {
	mu      sync.Mutex
	entries map[groupFailureLocalKey]*groupFailureLocalEntry
}

func (s *groupFailureLocalState) observe(key groupFailureLocalKey, now time.Time, window time.Duration, threshold int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = make(map[groupFailureLocalKey]*groupFailureLocalEntry)
	}
	entry := s.entries[key]
	if entry == nil {
		if len(s.entries) >= accountOverloadLocalLimit {
			for k, e := range s.entries {
				if !e.expires.After(now) {
					delete(s.entries, k)
				}
			}
			if len(s.entries) >= accountOverloadLocalLimit {
				// Bounded fallback: do not invent a threshold hit or evict active counts.
				return false
			}
		}
		entry = &groupFailureLocalEntry{}
		s.entries[key] = entry
	}
	kept := entry.failures[:0]
	for _, at := range entry.failures {
		if at.After(now.Add(-window)) {
			kept = append(kept, at)
		}
	}
	// Redis is authoritative while healthy. Its mirror may already be at the
	// threshold after an outage, so retain the latest bounded observations until
	// an actual cooldown or manual recovery clears this account's state.
	if len(kept) >= threshold {
		kept = kept[len(kept)-threshold+1:]
	}
	entry.failures = append(kept, now)
	entry.expires = now.Add(window)
	return len(entry.failures) >= threshold
}

func (s *groupFailureLocalState) clear(accountID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.entries {
		if key.accountID == accountID {
			delete(s.entries, key)
		}
	}
}
