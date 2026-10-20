package keyrouting

import (
	"context"
	"sync"
	"time"
)

// MemoryAccountHealthStore is deterministic under an injected clock. It is
// process-local; production replicas must use the shared Redis adapter.
type MemoryAccountHealthStore struct {
	mu      sync.Mutex
	opts    AccountHealthOptions
	entries map[string]memoryAccountHealthEntry
}

type memoryAccountHealthEntry struct {
	state    *AccountHealthState
	keyScope string
	siteScope string
	expires  time.Time
}

func NewMemoryAccountHealthStore(opts AccountHealthOptions) *MemoryAccountHealthStore {
	return &MemoryAccountHealthStore{opts: opts.Normalized(), entries: make(map[string]memoryAccountHealthEntry)}
}

func (s *MemoryAccountHealthStore) Get(ctx context.Context, id AccountHealthIdentity) (AccountHealthSnapshot, error) {
	return s.change(ctx, id, nil, AccountHealthAttempt{})
}

func (s *MemoryAccountHealthStore) Fence(ctx context.Context, id AccountHealthIdentity) error {
	_, err := s.Get(ctx, id)
	return err
}

func (s *MemoryAccountHealthStore) Observe(ctx context.Context, id AccountHealthIdentity, policy RouteFailurePolicy, attempt AccountHealthAttempt) (AccountHealthSnapshot, error) {
	return s.change(ctx, id, &policy, attempt)
}

func (s *MemoryAccountHealthStore) change(ctx context.Context, id AccountHealthIdentity, policy *RouteFailurePolicy, attempt AccountHealthAttempt) (AccountHealthSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return AccountHealthSnapshot{}, err
	}
	if err := id.Validate(); err != nil {
		return AccountHealthSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AccountHealthSnapshot{}, err
	}
	now := s.opts.Clock()
	key, scope := id.StorageKey(), id.KeyScope()
	for key, entry := range s.entries {
		if !now.Before(entry.expires) {
			delete(s.entries, key)
		}
	}
	entry, exists := s.entries[key]
	if !exists {
		if len(s.entries) >= s.opts.MaxIdentities {
			return AccountHealthSnapshot{}, ErrAccountHealthCapacity
		}
		count, siteCount := 0, 0
		for _, existing := range s.entries {
			if existing.keyScope == scope {
				count++
			}
			if existing.siteScope == id.SiteScope() {
				siteCount++
			}
		}
		if count >= s.opts.MaxIdentitiesPerKey {
			return AccountHealthSnapshot{}, ErrAccountHealthCapacity
		}
		if siteCount >= s.opts.MaxIdentitiesPerSite {
			return AccountHealthSnapshot{}, ErrAccountHealthCapacity
		}
	}
	var next *AccountHealthState
	var result AccountHealthSnapshot
	var err error
	if policy == nil {
		next, result, err = PrepareAccountHealth(entry.state, id, now, s.opts)
	} else {
		next, result, err = AdvanceAccountHealth(entry.state, id, *policy, attempt, now, s.opts)
	}
	if err != nil {
		return result, err
	}
	s.entries[key] = memoryAccountHealthEntry{state: next, keyScope: scope, siteScope: id.SiteScope(), expires: now.Add(s.opts.IdleTTL)}
	return result, nil
}

var _ AccountHealthStore = (*MemoryAccountHealthStore)(nil)
