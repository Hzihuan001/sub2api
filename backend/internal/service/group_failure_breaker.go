package service

import (
	"context"
	"fmt"
	"log/slog"
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
		return false, time.Time{} // No valid override: retain existing protection.
	}
	if policy == nil {
		return false, time.Time{}
	}
	rule, configured := policy.Rules[kind]
	if !configured {
		return false, time.Time{}
	}
	operation := s.accountOverloadOperationLock(account.ID)
	operation.Lock()
	defer operation.Unlock()
	now := time.Now()
	cooldown := time.Duration(rule.CooldownSeconds) * time.Second
	window := time.Duration(rule.WindowSeconds) * time.Second
	key := groupFailureLocalKey{account.ID, group.ID, kind, rule.Revision()}
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
			until, claim, err = cache.RecordAccountGroupFailure(cacheCtx, account.ID, group.ID, string(kind), key.revision, window, rule.Threshold, cooldown)
			stop()
			if err != nil {
				slog.Warn("group_failure_counter_failed", "account_id", account.ID, "group_id", group.ID, "error", err)
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
				slog.Info("group_failure_cooldown_started", "account_id", account.ID, "group_id", group.ID, "kind", kind,
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
