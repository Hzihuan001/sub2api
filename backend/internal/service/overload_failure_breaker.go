package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

const (
	accountOverloadFailureThreshold = 3
	accountOverloadFailureWindow    = 5 * time.Minute
	accountOverloadPersistLease     = 10 * time.Second
	accountOverloadLocalLimit       = 4096
	accountOverloadFailureReason    = "repeated_upstream_overload"
)

// AccountOverloadFailureCache is an optional TempUnschedCache extension. The
// shared counter owns the cooldown deadline and a short persistence lease, so
// concurrent gateways neither extend an active cooldown nor repeatedly write it.
type AccountOverloadFailureCache interface {
	RecordAccountOverloadFailure(context.Context, int64, time.Duration, int, time.Duration) (until time.Time, persistClaim bool, err error)
	CompleteAccountOverloadCooldown(context.Context, int64, time.Time, bool) error
	ClearAccountOverloadFailures(context.Context, int64) error
}

// IsAccountOverloadCooling keeps stale scheduler snapshots from clearing a newly
// observed overload block. It adds no remote lookup to ordinary scheduling.
func (s *RateLimitService) IsAccountOverloadCooling(accountID int64, now time.Time) bool {
	if s == nil || accountID <= 0 {
		return false
	}
	return !s.overloadFailures.coolingUntil(accountID, now).IsZero()
}

// ClearAccountOverload is for explicit account recovery, not ordinary success.
// Database recovery and counter removal are serialized with this process's
// overload observations. The caller still owns clearing other runtime blocks.
func (s *RateLimitService) ClearAccountOverload(ctx context.Context, accountID int64) error {
	if s == nil || accountID <= 0 {
		return nil
	}
	operation := s.accountOverloadOperationLock(accountID)
	operation.Lock()
	defer operation.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if s.accountRepo == nil {
		return errors.New("clear account overload: account repository unavailable")
	}
	if err := s.accountRepo.ClearRateLimit(ctx, accountID); err != nil {
		return err
	}
	s.overloadFailures.mu.Lock()
	delete(s.overloadFailures.entries, accountID)
	s.overloadFailures.mu.Unlock()
	if cache, ok := s.tempUnschedCache.(AccountOverloadFailureCache); ok {
		if err := cache.ClearAccountOverloadFailures(ctx, accountID); err != nil {
			slog.Warn("account_overload_counter_clear_failed", "account_id", accountID, "error", err)
		}
	}
	return nil
}

// RecordAccountOverload records one explicitly classified overload. Successful
// requests do not reset the rolling window. Callers must count each upstream
// failure once; this method deliberately does not infer overload from a status.
func (s *RateLimitService) RecordAccountOverload(ctx context.Context, account *Account) time.Time {
	if s == nil || account == nil || account.ID <= 0 {
		return time.Time{}
	}
	operation := s.accountOverloadOperationLock(account.ID)
	operation.Lock()
	defer operation.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	settingsCtx, cancelSettings := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	settings := s.accountOverloadCooldownSettings(settingsCtx, account.ID)
	cancelSettings()
	if !settings.Enabled {
		return time.Time{}
	}
	now := time.Now()
	cooldown := time.Duration(settings.CooldownMinutes) * time.Minute
	cache, _ := s.tempUnschedCache.(AccountOverloadFailureCache)
	if until, persisted := s.overloadFailures.persistedCooldown(account.ID, now); persisted {
		// Do not feed a newly recovered/empty Redis counter while this process
		// already has a persisted cooldown: that could start a later deadline.
		s.notifyAccountSchedulingBlocked(account, until, accountOverloadFailureReason)
		if cache != nil {
			s.completeAccountOverloadCache(ctx, cache, account.ID, until, true)
		}
		return until
	}
	var until time.Time
	var claim, persisted bool
	if cache != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		var err error
		until, claim, err = cache.RecordAccountOverloadFailure(cacheCtx, account.ID, accountOverloadFailureWindow, accountOverloadFailureThreshold, cooldown)
		cancel()
		if err != nil {
			slog.Warn("account_overload_counter_failed", "account_id", account.ID, "error", err)
			cache = nil
		} else if until.IsZero() {
			// Keep the last local observations for a subsequent Redis outage, but
			// let the shared counter decide when to trip while it is available.
			if s.overloadFailures.coolingUntil(account.ID, now).IsZero() {
				s.overloadFailures.observe(account.ID, now)
				return time.Time{}
			}
			// A local cooldown may have started during an outage, or this Redis
			// response may predate another concurrent caller's trip.
			cache = nil
		} else {
			persisted = s.overloadFailures.rememberCooldown(account.ID, now, until)
		}
	}
	if cache == nil {
		until, claim = s.overloadFailures.record(account.ID, now, cooldown)
	}
	if until.IsZero() || !until.After(now) {
		return time.Time{}
	}
	// The runtime block must precede persistence, including on DB failure. No
	// field on the scheduler's shared account snapshot is mutated here.
	s.notifyAccountSchedulingBlocked(account, until, accountOverloadFailureReason)
	if !claim && !persisted {
		return until
	}
	if claim && !persisted {
		if s.accountRepo == nil {
			slog.Warn("account_overload_persist_failed", "account_id", account.ID, "error", "account repository unavailable")
		} else {
			persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			err := s.accountRepo.SetOverloaded(persistCtx, account.ID, until)
			cancel()
			persisted = err == nil
			if err != nil {
				slog.Warn("account_overload_persist_failed", "account_id", account.ID, "until", until, "error", err)
			} else {
				slog.Warn("account_overload_cooldown_started", "account_id", account.ID, "failure_threshold", accountOverloadFailureThreshold, "window", accountOverloadFailureWindow, "until", until)
			}
		}
		s.overloadFailures.complete(account.ID, until, persisted)
	}
	if cache != nil {
		s.completeAccountOverloadCache(ctx, cache, account.ID, until, persisted)
	}
	return until
}

func (s *RateLimitService) completeAccountOverloadCache(ctx context.Context, cache AccountOverloadFailureCache, accountID int64, until time.Time, persisted bool) {
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := cache.CompleteAccountOverloadCooldown(ackCtx, accountID, until, persisted); err != nil {
		// The shared lease expires, and the next observation can compensate.
		// The local persisted flag prevents a second DB write after a lost ACK.
		slog.Warn("account_overload_counter_complete_failed", "account_id", accountID, "error", err)
	}
}

func (s *RateLimitService) accountOverloadCooldownSettings(ctx context.Context, accountID int64) *OverloadCooldownSettings {
	if s.settingService != nil {
		settings, err := s.settingService.GetOverloadCooldownSettings(ctx)
		if err == nil && settings != nil {
			return settings
		}
		if err != nil {
			slog.Warn("account_overload_settings_failed", "account_id", accountID, "error", err)
		}
	}
	minutes := 10
	if s.cfg != nil && s.cfg.RateLimit.OverloadCooldownMinutes > 0 {
		minutes = min(s.cfg.RateLimit.OverloadCooldownMinutes, 120)
	}
	return &OverloadCooldownSettings{Enabled: true, CooldownMinutes: minutes}
}

type accountOverloadLocalEntry struct {
	failures   [accountOverloadFailureThreshold]time.Time
	count      int
	until      time.Time
	leaseUntil time.Time
	persisted  bool
	touched    time.Time
}

// This bounded fallback has no timer goroutine. Only overload signals touch it.
type accountOverloadLocalState struct {
	mu      sync.Mutex
	entries map[int64]*accountOverloadLocalEntry
	// Only overload observations and explicit recovery use these locks. Normal
	// scheduling never waits for their database/Redis work, and unrelated shards
	// proceed independently.
	operations [64]sync.Mutex
}

func (s *RateLimitService) accountOverloadOperationLock(accountID int64) *sync.Mutex {
	return &s.overloadFailures.operations[uint64(accountID)%uint64(len(s.overloadFailures.operations))]
}

func (s *accountOverloadLocalState) coolingUntil(id int64, now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[id]; entry != nil && entry.until.After(now) {
		return entry.until
	}
	return time.Time{}
}

func (s *accountOverloadLocalState) persistedCooldown(id int64, now time.Time) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[id]; entry != nil && entry.persisted && entry.until.After(now) {
		return entry.until, true
	}
	return time.Time{}, false
}

func (s *accountOverloadLocalState) entry(id int64, now time.Time) *accountOverloadLocalEntry {
	if s.entries == nil {
		s.entries = make(map[int64]*accountOverloadLocalEntry)
	}
	if entry := s.entries[id]; entry != nil {
		if !entry.until.IsZero() && !entry.until.After(now) {
			*entry = accountOverloadLocalEntry{}
		}
		entry.touched = now
		return entry
	}
	if len(s.entries) >= accountOverloadLocalLimit {
		var oldestID int64
		var oldest time.Time
		for key, entry := range s.entries {
			if entry.until.After(now) {
				continue // Never evict an active local cooldown.
			}
			if oldestID == 0 || entry.touched.Before(oldest) {
				oldestID, oldest = key, entry.touched
			}
		}
		if oldestID == 0 {
			return nil
		}
		delete(s.entries, oldestID)
	}
	entry := &accountOverloadLocalEntry{touched: now}
	s.entries[id] = entry
	return entry
}

func (e *accountOverloadLocalEntry) addFailure(now time.Time) {
	count := 0
	for _, at := range e.failures[:e.count] {
		if at.After(now.Add(-accountOverloadFailureWindow)) {
			e.failures[count] = at
			count++
		}
	}
	if count >= len(e.failures) {
		copy(e.failures[:], e.failures[1:])
		count--
	}
	e.failures[count] = now
	e.count = count + 1
}

func (s *accountOverloadLocalState) observe(id int64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entry(id, now); entry != nil && !entry.until.After(now) {
		entry.addFailure(now)
	}
}

func (s *accountOverloadLocalState) record(id int64, now time.Time, cooldown time.Duration) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entry(id, now)
	if entry == nil {
		return time.Time{}, false
	}
	if !entry.until.After(now) {
		entry.addFailure(now)
		if entry.count < accountOverloadFailureThreshold {
			return time.Time{}, false
		}
		entry.until = now.Add(cooldown).Truncate(time.Millisecond)
		entry.count = 0
	}
	claim := !entry.persisted && !entry.leaseUntil.After(now)
	if claim {
		entry.leaseUntil = now.Add(accountOverloadPersistLease)
	}
	return entry.until, claim
}

func (s *accountOverloadLocalState) rememberCooldown(id int64, now, until time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.entry(id, now)
	if entry == nil {
		return false
	}
	if !entry.until.Equal(until) {
		entry.until, entry.leaseUntil = until, now.Add(accountOverloadPersistLease)
		entry.count, entry.persisted = 0, false
	}
	return entry.persisted
}

func (s *accountOverloadLocalState) complete(id int64, until time.Time, persisted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[id]; entry != nil && entry.until.Equal(until) {
		entry.persisted = entry.persisted || persisted
		entry.leaseUntil = time.Time{}
	}
}
