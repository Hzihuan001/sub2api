package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

type accountRuntimeBoundedBatchStore struct {
	*accountRuntimeStoreStub
	contexts []context.Context
	failLast bool
	waitLast bool
	until    time.Time
}

func (s *accountRuntimeBoundedBatchStore) GetMany(ctx context.Context, ids []keyrouting.AccountHealthIdentity) ([]keyrouting.AccountHealthSnapshot, error) {
	s.contexts = append(s.contexts, ctx)
	// Match the repository's public batch bound, independently of the runtime.
	if len(ids) > 1024 {
		return nil, keyrouting.ErrAccountHealthCapacity
	}
	if len(ids) > 0 && ids[len(ids)-1].AccountID == 1025 {
		if s.waitLast {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if s.failLast {
			return nil, errors.New("second batch read failed")
		}
	}
	snapshots := make([]keyrouting.AccountHealthSnapshot, len(ids))
	for i, id := range ids {
		snapshots[i] = keyrouting.AccountHealthSnapshot{PolicyRevision: id.PolicyRevision}
		if id.AccountID == 1 || id.AccountID == 1025 {
			snapshots[i].Cooling = true
			snapshots[i].CooldownUntil = s.until
		}
	}
	return snapshots, nil
}

func TestKeyAccountRoutingRuntimeBatchOver1024(t *testing.T) {
	for _, mode := range []string{"success", "later failure", "shared deadline"} {
		t.Run(mode, func(t *testing.T) {
			session, _, store, lister, _, now := accountRuntimeFixture(t, accountRuntimeStatusPolicy())
			lister.accounts[1] = make([]Account, 1025)
			for i := range lister.accounts[1] {
				lister.accounts[1][i].ID = int64(i + 1)
			}
			batch := &accountRuntimeBoundedBatchStore{
				accountRuntimeStoreStub: store,
				failLast:                mode == "later failure",
				waitLast:                mode == "shared deadline",
				until:                   now.Add(time.Minute),
			}
			session.runtime.store = batch
			session.runtime.opts.HealthReadTimeout = time.Second
			if batch.waitLast {
				session.runtime.opts.HealthReadTimeout = 20 * time.Millisecond
			}
			if err := session.ExcludeRequestAccount(1, 2); err != nil {
				t.Fatal(err)
			}
			excluded, err := session.Exclusions(context.Background(), 1)
			if len(batch.contexts) != 2 || batch.contexts[0] != batch.contexts[1] {
				t.Fatalf("large pool must use bounded calls sharing one deadline: contexts=%v", batch.contexts)
			}
			if _, ok := batch.contexts[0].Deadline(); !ok {
				t.Fatal("batch reads have no deadline")
			}
			if mode != "success" {
				if !errors.Is(err, ErrKeyAccountSoftHealthUnavailable) || excluded != nil || len(session.health) != 0 {
					t.Fatalf("later error published partial health: excluded=%v cached=%d err=%v", excluded, len(session.health), err)
				}
				if batch.waitLast && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("shared read deadline was lost: %v", err)
				}
				groupID := int64(1)
				ctx := WithKeyAccountRoutingSession(context.Background(), session)
				merged, mergeErr := keyAccountRouteSchedulingExclusions(ctx, &groupID, map[int64]struct{}{3: {}})
				if mergeErr != nil || len(merged) != 2 {
					t.Fatalf("soft failure lost native/local exclusions: merged=%v err=%v", merged, mergeErr)
				}
				if _, ok := merged[2]; !ok {
					t.Fatalf("soft failure lost request-local exclusion: %v", merged)
				}
				return
			}
			if err != nil || len(excluded) != 3 || len(session.health) != 1025 {
				t.Fatalf("large pool failed to load all health: excluded=%v cached=%d err=%v", excluded, len(session.health), err)
			}
			for _, id := range []int64{1, 2, 1025} {
				if _, ok := excluded[id]; !ok {
					t.Fatalf("cooled/local account %d was not excluded", id)
				}
			}
			for _, id := range []int64{1, 3, 1025} {
				allowed, hintErr := session.CandidateEligible(context.Background(), 1, id)
				if hintErr != nil || allowed != (id == 3) {
					t.Fatalf("wrong cached eligibility for account %d: allowed=%v err=%v", id, allowed, hintErr)
				}
			}
			if len(batch.contexts) != 2 || store.getCalls != 0 || lister.calls != 1 {
				t.Fatalf("candidate hints repeated I/O: batches=%d gets=%d lists=%d", len(batch.contexts), store.getCalls, lister.calls)
			}
		})
	}
}
