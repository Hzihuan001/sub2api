package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
	"github.com/stretchr/testify/require"
)

func TestImageRelayJournalDispatchClaimSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := newImageRelayJournal(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })
	second, err := newImageRelayJournal(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })
	owner := ImageRelayOwner{UserID: 1, APIKeyID: 2}
	fingerprint := imageRelayRequestFingerprint("/v1/images/generations", "application/json", []byte(`{"prompt":"one"}`))
	type claim struct {
		op  ImageRelayOperation
		new bool
		err error
	}
	const attempts = 12
	claims := make(chan claim, attempts)
	journals := []*imageRelayJournal{first, second}
	for i := range attempts {
		go func() {
			op, created, err := journals[i%len(journals)].Begin(owner, "customer-retry", fingerprint, time.Now())
			claims <- claim{op: op, new: created, err: err}
		}()
	}
	expectedID, err := imageRelayOperationID(owner, "customer-retry")
	require.NoError(t, err)
	dispatches := 0
	for range attempts {
		result := <-claims
		require.NoError(t, result.err)
		require.Equal(t, expectedID, result.op.ID)
		if result.new {
			dispatches++
		}
	}
	require.Equal(t, 1, dispatches)
	require.NoError(t, first.Close())
	require.NoError(t, second.Close())

	reopened, err := newImageRelayJournal(dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = reopened.Close() })
	op, created, err := reopened.BeginWithID(owner, expectedID, fingerprint, time.Now())
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, ImageRelayPending, op.State)
	different := imageRelayRequestFingerprint("/v1/images/edits", "application/json", []byte(`{"prompt":"one"}`))
	_, _, err = reopened.Begin(owner, "customer-retry", different, time.Now())
	require.ErrorIs(t, err, ErrImageRelayOperationConflict)
	otherKey := ImageRelayOwner{UserID: owner.UserID, APIKeyID: owner.APIKeyID + 1}
	_, err = reopened.Lookup(otherKey, expectedID)
	require.ErrorIs(t, err, ErrImageRelayOperationNotFound)
	_, _, err = reopened.BeginWithID(otherKey, expectedID, fingerprint, time.Now())
	require.ErrorIs(t, err, ErrImageRelayOperationNotFound)
	otherOp, created, err := reopened.Begin(otherKey, "customer-retry", fingerprint, time.Now())
	require.NoError(t, err)
	require.True(t, created)
	require.NotEqual(t, expectedID, otherOp.ID)
	require.NoError(t, reopened.MarkFailed(owner, expectedID, time.Now()))
	op, created, err = reopened.Begin(owner, "customer-retry", fingerprint, time.Now())
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, ImageRelayFailed, op.State)
}

func TestImageRelaySettlementReplaysStoredTermsAfterLogFailure(t *testing.T) {
	dir := t.TempDir()
	open := func() *imageRelayRuntime {
		journal, err := newImageRelayJournal(dir)
		require.NoError(t, err)
		t.Cleanup(func() { _ = journal.Close() })
		results, err := imagerelay.NewResultStore(filepath.Join(dir, "results"), 4, 1<<20, 0, ImageRelayResultRetention)
		require.NoError(t, err)
		t.Cleanup(func() { _ = results.Close() })
		return &imageRelayRuntime{journal: journal, results: results}
	}
	r := open()
	now := time.Now()
	owner := ImageRelayOwner{UserID: 3, APIKeyID: 4}
	fingerprint := imageRelayRequestFingerprint("/v1/images/generations", "application/json", []byte(`{"prompt":"one"}`))
	op, created, err := r.journal.Begin(owner, "retry", fingerprint, now)
	require.NoError(t, err)
	require.True(t, created)
	body := []byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`)
	metadata := imagerelay.ResultMetadata{Status: 200, BodyBytes: int64(len(body)),
		BodySHA256: imagerelay.SHA256Hex(body), ContentType: "application/json"}
	reservation, err := r.results.Reserve(op.ID, owner.APIKeyID, int64(len(body)))
	require.NoError(t, err)
	require.NoError(t, reservation.Save(context.Background(), bytes.NewReader(body), metadata))
	require.NoError(t, reservation.Close())
	result := ImageRelayStoredResult{BodyBytes: metadata.BodyBytes, BodySHA256: metadata.BodySHA256, ContentType: metadata.ContentType}
	require.NoError(t, r.journal.MarkResult(owner, op.ID, result, now))
	requestID := "image-relay:" + op.ID
	settlement := ImageRelaySettlement{
		Command: UsageBillingCommand{RequestID: requestID, UserID: owner.UserID, APIKeyID: owner.APIKeyID, AccountID: 5,
			ImageCount: 1, Model: "gpt-image-2", BalanceCost: 0.04},
		Usage: UsageLog{RequestID: requestID, UserID: owner.UserID, APIKeyID: owner.APIKeyID, AccountID: 5,
			ImageCount: 1, ActualCost: 0.04, APIKey: &APIKey{Key: "secret-not-for-journal"}},
	}
	require.ErrorIs(t, r.journal.MarkSettled(owner, op.ID, now), ErrImageRelayOperationState)
	require.NoError(t, r.journal.PrepareSettlement(owner, op.ID, settlement, now))
	require.NoError(t, r.journal.PrepareSettlement(owner, op.ID, settlement, now))
	conflict := settlement
	conflict.Command.BalanceCost = 0.09
	require.ErrorIs(t, r.journal.PrepareSettlement(owner, op.ID, conflict, now), ErrImageRelayOperationState)

	usageFailure := errors.New("simulated usage log outage")
	billing := &imageRelayTestBilling{}
	logs := &imageRelayTestUsageLogs{failure: usageFailure}
	s := &OpenAIGatewayService{usageBillingRepo: billing, usageLogRepo: logs}
	require.ErrorIs(t, s.finishImageRelaySettlement(context.Background(), r, op), usageFailure)
	require.Equal(t, 1, billing.applied)
	require.NoError(t, r.results.Close())
	require.NoError(t, r.journal.Close())

	r = open()
	recoverable, err := r.journal.PendingSettlements(8)
	require.NoError(t, err)
	require.Len(t, recoverable, 1)
	require.False(t, recoverable[0].Usable())
	require.Nil(t, recoverable[0].Settlement.Usage.APIKey)
	require.Equal(t, 0.04, recoverable[0].Settlement.Command.BalanceCost)
	logs.failure = nil
	require.NoError(t, s.finishImageRelaySettlement(context.Background(), r, recoverable[0]))
	require.Equal(t, 1, billing.applied)
	require.Equal(t, 2, billing.attempts)
	require.Equal(t, requestID, logs.inserted.RequestID)
	settled, err := r.journal.Lookup(owner, op.ID)
	require.NoError(t, err)
	require.True(t, settled.Usable())
	require.NoError(t, s.finishImageRelaySettlement(context.Background(), r, settled))
	require.Equal(t, 2, billing.attempts)

	expired, err := r.journal.Prune(settled.UpdatedAt.Add(ImageRelayResultRetention))
	require.NoError(t, err)
	require.Equal(t, []string{op.ID}, expired)
	tombstone, created, err := r.journal.Begin(owner, "retry", fingerprint, time.Now())
	require.NoError(t, err)
	require.False(t, created)
	require.False(t, tombstone.Usable())
	require.Equal(t, ImageRelayExpired, tombstone.State)
	require.Nil(t, tombstone.Result)
	require.Nil(t, tombstone.Settlement)
}

type imageRelayTestBilling struct {
	UsageBillingRepository
	command           UsageBillingCommand
	applied, attempts int
}

func (b *imageRelayTestBilling) Apply(_ context.Context, cmd *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	b.attempts++
	if b.applied > 0 {
		if b.command != *cmd {
			return nil, ErrUsageBillingRequestConflict
		}
		return &UsageBillingApplyResult{}, nil
	}
	b.applied++
	b.command = *cmd
	return &UsageBillingApplyResult{Applied: true}, nil
}

type imageRelayTestUsageLogs struct {
	UsageLogRepository
	failure  error
	inserted UsageLog
}

func (l *imageRelayTestUsageLogs) Create(_ context.Context, usage *UsageLog) (bool, error) {
	if l.failure != nil {
		return false, l.failure
	}
	l.inserted = *usage
	return true, nil
}
