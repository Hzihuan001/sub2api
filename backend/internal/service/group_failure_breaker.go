package service

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AccountGroupFailureCache is retained only to clear state written by older
// releases. New group failure rules affect key-local routing, never accounts.
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

// ApplyGroupFailurePolicy is a compatibility seam for existing native error
// hooks. Group rules are now interpreted only by KeyRoutingService; they must
// not write account-wide overload state, including for single-group keys.
// Returning unhandled preserves the caller's native credential/quota rules.
func (s *RateLimitService) ApplyGroupFailurePolicy(_ context.Context, _ *Account, _ GroupFailureKind) (bool, time.Time) {
	return false, time.Time{}
}

// ApplyGroupFailurePolicyStatus retains the narrow legacy call contract while
// preventing an HTTP routing rule from globally pausing an upstream account.
func (s *RateLimitService) ApplyGroupFailurePolicyStatus(_ context.Context, _ *Account, _ int) (bool, time.Time) {
	return false, time.Time{}
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
