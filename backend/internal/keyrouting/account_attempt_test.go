package keyrouting

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func accountAttemptTestMetadata() AccountAttemptMetadata {
	return AccountAttemptMetadata{Site: "local-main", APIKeyID: 10, GroupID: 20, AccountID: 30,
		Model: "public-model", UpstreamModel: "mapped-model", Platform: "openai", Endpoint: "responses", PolicyRevision: 42}
}

func TestAccountAttemptFrozenIdentityAndIndependentDispatch(t *testing.T) {
	metadata := accountAttemptTestMetadata()
	a := NewAccountAttempt(metadata, time.Hour)
	defer a.Finish(AccountAttemptResult{Outcome: OutcomeIgnored})
	metadata.AccountID = 999
	if a.Metadata().AccountID != 30 || a.Metadata().PolicyRevision != 42 {
		t.Fatal("attempt metadata did not remain a value snapshot")
	}
	if _, err := uuid.Parse(a.Metadata().AttemptID); err != nil {
		t.Fatalf("missing unique attempt UUID: %v", err)
	}
	if a.Guard() == nil || !a.Guard().started.IsZero() || a.Guard().timer != nil {
		t.Fatal("constructing an attempt must not start the deadline")
	}
	type rootValue struct{}
	root := context.WithValue(context.Background(), rootValue{}, "kept")
	old := NewFirstContentGuard(time.Hour)
	defer old.Finish()
	root = WithFirstContentGuard(root, old)
	bound := WithAccountAttempt(root, a)
	if GetAccountAttempt(bound) != a || GetFirstContentGuard(bound) != a.Guard() || !a.Guard().started.IsZero() {
		t.Fatal("binding must install a fresh guard without dispatch")
	}
	child := a.Dispatch(bound)
	if child.Value(rootValue{}) != "kept" || GetAccountAttempt(child) != a || GetFirstContentGuard(child) != a.Guard() {
		t.Fatal("dispatch lost immutable request values or its shared safety latch")
	}
	if a.Dispatch(bound) != child || a.Guard().started.IsZero() || a.Guard().timer == nil {
		t.Fatal("dispatch must start once and return the same child")
	}
	a.Guard().expire()
	b := NewAccountAttempt(accountAttemptTestMetadata(), time.Hour)
	defer b.Finish(AccountAttemptResult{Outcome: OutcomeIgnored})
	other := b.Dispatch(root)
	if a.Metadata().AttemptID == b.Metadata().AttemptID || a.Guard() == b.Guard() || other.Err() != nil || b.Guard().Expired() || root.Err() != nil {
		t.Fatal("a new account must not inherit a preceding attempt's timer or cancellation")
	}
}

func TestAccountAttemptUndispatchedAndPreCancelledAreNotSamples(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		a := NewAccountAttempt(accountAttemptTestMetadata(), time.Hour)
		root, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
			if a.Dispatch(root).Err() == nil {
				t.Fatal("a pre-cancelled client must not dispatch")
			}
		}
		sample := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 503})
		a.MarkCleanupComplete()
		if sample.Dispatched || sample.Outcome != OutcomeIgnored || sample.StatusSource != AttemptStatusNone || sample.StatusCode != 0 || sample.Elapsed != 0 || a.CanStartNext() {
			t.Fatalf("admission/cancellation manufactured an inference sample: %+v", sample)
		}
		if a.Dispatch(root).Err() == nil {
			t.Fatal("a finished undispatched attempt cannot start later")
		}
		cancel()
	}
}

func TestAccountAttemptFinishOnceAndCleanupBarrier(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
	child := a.Dispatch(context.Background())
	a.MarkCleanupComplete() // Deliberately premature; it must not count.
	first := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 503, ClientStatusCode: 502})
	second := a.Finish(AccountAttemptResult{Outcome: OutcomeSuccess, UpstreamStatusCode: 200, UsageKnown: true})
	if !reflect.DeepEqual(first, second) || first.Outcome != OutcomeFailure || !first.Dispatched || first.StatusCode != 503 || first.StatusSource != AttemptStatusUpstream {
		t.Fatalf("terminal result was not immutable: first=%+v second=%+v", first, second)
	}
	if first.StartedAt.IsZero() || first.FinishedAt.Before(first.StartedAt) || first.Elapsed < 0 || child.Err() == nil || a.CanStartNext() {
		t.Fatal("finish must freeze timing and close its child without bypassing cleanup")
	}
	a.MarkCleanupComplete()
	if !a.CanStartNext() {
		t.Fatal("an uncommitted, unbillable completed failure should allow the next candidate after cleanup")
	}
	first.Metadata.AccountID = 999
	if a.Finish(AccountAttemptResult{}).Metadata.AccountID != 30 {
		t.Fatal("the returned sample shared mutable metadata")
	}
}

func TestAccountAttemptStatusProvenance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		result    AccountAttemptResult
		expire    bool
		wantCode  int
		wantCause AccountAttemptStatusSource
	}{
		{name: "real upstream wins", result: AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 503, ClientStatusCode: 502}, wantCode: 503, wantCause: AttemptStatusUpstream},
		{name: "real client fallback", result: AccountAttemptResult{Outcome: OutcomeFailure, ClientStatusCode: 502}, wantCode: 502, wantCause: AttemptStatusClient},
		{name: "synthetic client excluded", result: AccountAttemptResult{Outcome: OutcomeFailure, ClientStatusCode: 504, ClientStatusSynthetic: true}, wantCause: AttemptStatusNone},
		{name: "invalid HTTP excluded", result: AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 900, ClientStatusCode: -1}, wantCause: AttemptStatusNone},
		{name: "local deadline never invents 504", result: AccountAttemptResult{Outcome: OutcomeFailure, ClientStatusCode: 504}, expire: true, wantCause: AttemptStatusNone},
		{name: "real upstream timeout retained", result: AccountAttemptResult{Outcome: OutcomeTimeout, UpstreamStatusCode: 504}, wantCode: 504, wantCause: AttemptStatusUpstream},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAccountAttempt(accountAttemptTestMetadata(), time.Hour)
			a.Dispatch(context.Background())
			if tc.expire {
				a.Guard().expire()
			}
			got := a.Finish(tc.result)
			if got.StatusCode != tc.wantCode || got.StatusSource != tc.wantCause {
				t.Fatalf("wrong status provenance: %+v", got)
			}
			if tc.expire && (got.Outcome != OutcomeTimeout || got.CancellationCause != AttemptFirstContentExpiry) {
				t.Fatalf("Key deadline lost its distinct semantic outcome: %+v", got)
			}
		})
	}
}

func TestAccountAttemptSafetyAlwaysOnWithoutTTFT(t *testing.T) {
	for _, tc := range []struct {
		name string
		mark func(*AccountAttempt)
		want AccountAttemptReplayVeto
	}{
		{name: "usage", mark: (*AccountAttempt).ObserveUsage, want: AttemptVetoUsage},
		{name: "side effect", mark: (*AccountAttempt).ObserveSideEffect, want: AttemptVetoSideEffect},
		{name: "content", mark: func(a *AccountAttempt) { a.ObserveFirstContent() }, want: AttemptVetoFirstContent},
		{name: "unknown protocol", mark: func(a *AccountAttempt) { a.PreventReplay(AttemptVetoUnknownProtocol) }, want: AttemptVetoUnknownProtocol},
		{name: "native guard", mark: func(a *AccountAttempt) { a.Guard().PreventReplay() }, want: AttemptVetoNativeGuard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
			a.Dispatch(context.Background())
			tc.mark(a)
			got := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 503})
			a.MarkCleanupComplete()
			if !a.ReplayBlocked() || a.CanStartNext() || got.ReplayVeto&tc.want == 0 {
				t.Fatalf("safety was disabled with TTFT=0: %+v", got)
			}
		})
	}
}

func TestAccountAttemptRawUsageReaderSharesSafetyLatch(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
	ctx := a.Dispatch(context.Background())
	reader := ProtectUsageReader(ctx, io.NopCloser(strings.NewReader("data: {\"usage\":{\"input_tokens\":\"3\"}}\n\n")))
	defer reader.Close()
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}
	got := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure})
	a.MarkCleanupComplete()
	if !got.UsageKnown || got.ReplayVeto&AttemptVetoUsage == 0 || !a.ReplayBlocked() || a.CanStartNext() {
		t.Fatalf("raw usage was disconnected from attempt replay protection: %+v", got)
	}
}

func TestAccountAttemptVerifiedFailureIsMonotonicProofNotReplayPermission(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
	if a.MarkVerifiedFailure() || a.VerifiedFailure() {
		t.Fatal("an undispatched attempt cannot carry native parser proof")
	}
	a.Dispatch(context.Background())
	if !a.MarkVerifiedFailure() || !a.VerifiedFailure() {
		t.Fatal("dispatched parser proof was not retained")
	}
	a.ObserveUsage()
	sample := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 200})
	a.MarkCleanupComplete()
	if !sample.VerifiedFailure || !sample.UsageKnown || a.CanStartNext() {
		t.Fatalf("parser proof overrode irrevocable usage: %+v", sample)
	}
	if a.MarkVerifiedFailure() {
		t.Fatal("frozen samples cannot be reopened by a late parser callback")
	}
}

func TestAccountAttemptExpiryUsageAndContentRacesNeverReplay(t *testing.T) {
	for _, content := range []bool{false, true} {
		for i := 0; i < 100; i++ {
			a := NewAccountAttempt(accountAttemptTestMetadata(), time.Hour)
			a.Dispatch(context.Background())
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); <-start; a.Guard().expire() }()
			go func() {
				defer wg.Done()
				<-start
				if content {
					a.ObserveFirstContent()
				} else {
					a.ObserveUsage()
				}
			}()
			close(start)
			wg.Wait()
			got := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure})
			a.MarkCleanupComplete()
			if !a.ReplayBlocked() || a.CanStartNext() || got.ReplayVeto == 0 {
				t.Fatalf("deadline race permitted duplicate inference: %+v", got)
			}
		}
	}
}

func TestAccountAttemptLateSuccessfulResponseWinsDeadline(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), time.Hour)
	child := a.Dispatch(context.Background())
	a.Guard().expire()
	if child.Err() == nil || !a.Guard().Expired() {
		t.Fatal("expected the local deadline to cancel only its inference child")
	}
	got := a.Finish(AccountAttemptResult{Outcome: OutcomeSuccess, CompleteResponse: true})
	a.MarkCleanupComplete()
	if got.Outcome != OutcomeSuccess || !got.FirstContentExpired || got.ReplayVeto&AttemptVetoCompleteResponse == 0 || a.CanStartNext() {
		t.Fatalf("a late complete response was discarded or replayed: %+v", got)
	}
}

func TestAccountAttemptClientCancellationDoesNotCancelUsageDrain(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), time.Hour)
	parent, cancel := context.WithCancel(context.Background())
	child := a.Dispatch(parent)
	cancel()
	if child.Err() != nil || a.CancellationCause() != ErrAccountAttemptClientCancelled {
		t.Fatal("client cancellation must veto replay without discarding native usage drain")
	}
	got := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 503})
	a.MarkCleanupComplete()
	if got.Outcome != OutcomeIgnored || got.StatusCode != 0 || got.CancellationCause != AttemptClientCancelled || got.ReplayVeto&AttemptVetoClientCancelled == 0 || a.CanStartNext() {
		t.Fatalf("client-aborted inference became an account failure/replay: %+v", got)
	}
}

func TestAccountAttemptLateSafetyObservationAndRootCancellationVeto(t *testing.T) {
	for _, late := range []string{"usage", "content", "cancel"} {
		t.Run(late, func(t *testing.T) {
			a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			a.Dispatch(parent)
			first := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 503})
			a.MarkCleanupComplete()
			if !a.CanStartNext() {
				t.Fatal("expected a safe failed attempt before the late veto")
			}
			switch late {
			case "usage":
				a.ObserveUsage()
			case "content":
				a.ObserveFirstContent()
			case "cancel":
				cancel()
			}
			if a.CanStartNext() || !reflect.DeepEqual(first, a.Finish(AccountAttemptResult{Outcome: OutcomeSuccess})) {
				t.Fatal("late veto either permitted replay or rewrote an immutable event")
			}
		})
	}
}

func TestAccountAttemptConcurrentFinishProducesOneImmutableSample(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
	a.Dispatch(context.Background())
	const callers = 32
	results := make(chan AccountAttemptSample, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results <- a.Finish(AccountAttemptResult{Outcome: OutcomeFailure, UpstreamStatusCode: 500 + index})
		}(i)
	}
	wg.Wait()
	close(results)
	want := a.Finish(AccountAttemptResult{})
	for got := range results {
		if !reflect.DeepEqual(got, want) {
			t.Fatal("duplicate terminal callbacks produced different events")
		}
	}
}

func TestAccountAttemptRealTimerStartsOnlyAtDispatch(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), 15*time.Millisecond)
	if a.guard.timer != nil || !a.guard.started.IsZero() {
		t.Fatal("metadata preparation started a timeout")
	}
	child := a.Dispatch(context.Background())
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("attempt deadline did not cancel its upstream child")
	}
	if a.CancellationCause() != ErrAccountAttemptFirstContent {
		t.Fatal("local first-content expiry was confused with client cancellation")
	}
	got := a.Finish(AccountAttemptResult{Outcome: OutcomeFailure})
	if got.Outcome != OutcomeTimeout || !got.FirstContentExpired || got.Elapsed < 0 {
		t.Fatalf("invalid timeout sample: %+v", got)
	}
}

func TestAccountAttemptPrematureContentIsNotNegativeTTFT(t *testing.T) {
	a := NewAccountAttempt(accountAttemptTestMetadata(), 0)
	if a.ObserveFirstContent() {
		t.Fatal("content outside a real dispatch cannot produce a TTFT sample")
	}
	a.Dispatch(context.Background())
	got := a.Finish(AccountAttemptResult{Outcome: OutcomeSuccess})
	if got.FirstContentObserved || got.FirstContentElapsed != 0 || got.Elapsed < 0 {
		t.Fatalf("pre-dispatch observation manufactured latency: %+v", got)
	}
}
