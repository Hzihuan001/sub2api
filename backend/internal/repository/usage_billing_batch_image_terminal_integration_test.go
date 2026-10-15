//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBatchImageBalanceTerminalOperationsExcludeEachOther(t *testing.T) {
	for _, archived := range []bool{false, true} {
		for _, actual := range []float64{0, 0.25} {
			for _, captured := range []bool{false, true} {
				t.Run(fmt.Sprintf("archived=%t/cost=%g/captured=%t", archived, actual, captured), func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					fixture := newBatchImageBalanceTerminalFixture(t)
					fixture.reserve(t, ctx, fixture.batchID, 1)
					fixture.reserve(t, ctx, fixture.otherBatchID, 2)
					fixture.assertBalance(t, ctx, 97, 3)

					apply := fixture.repo.ReleaseBatchImageBalance
					opposite := fixture.repo.CaptureBatchImageBalance
					command := fixture.command(service.BatchImageReleaseRequestID(fixture.batchID), fixture.batchID, 1, 0)
					oppositeCommand := fixture.command(service.BatchImageCaptureRequestID(fixture.batchID), fixture.batchID, 1, actual)
					wantErr := errBatchImageBalanceAlreadyReleased
					wantBalance := 98.0
					if captured {
						apply, opposite = opposite, apply
						command, oppositeCommand = oppositeCommand, command
						wantErr = errBatchImageBalanceAlreadyCaptured
						wantBalance -= actual
					}
					result, err := apply(ctx, command)
					require.NoError(t, err)
					require.True(t, result.Applied)
					fixture.assertBalance(t, ctx, wantBalance, 2)
					if archived {
						// Archived claims must provide the same terminal protection as
						// live claims, including an archived original reservation.
						_, err = integrationDB.ExecContext(ctx, `
							WITH moved AS (
								DELETE FROM usage_billing_dedup WHERE api_key_id = $1
								RETURNING request_id, api_key_id, request_fingerprint, created_at
							)
							INSERT INTO usage_billing_dedup_archive (request_id, api_key_id, request_fingerprint, created_at)
							SELECT request_id, api_key_id, request_fingerprint, created_at FROM moved`, fixture.keyID)
						require.NoError(t, err)
					}
					for repeat := 0; repeat < 2; repeat++ {
						// Simulate replay after the terminal transaction committed but
						// its response or the job status update was lost.
						result, err = apply(ctx, command)
						require.NoError(t, err)
						require.False(t, result.Applied)
						_, err = opposite(ctx, oppositeCommand)
						require.ErrorIs(t, err, wantErr)
						require.NotErrorIs(t, err, service.ErrUsageBillingRequestConflict)
						fixture.assertBalance(t, ctx, wantBalance, 2)
					}
					var failedClaims int
					require.NoError(t, integrationDB.QueryRowContext(ctx,
						`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2`,
						oppositeCommand.RequestID, fixture.keyID).Scan(&failedClaims))
					require.Zero(t, failedClaims, "a rejected operation must not claim success")
					fixture.releaseOther(t, ctx, wantBalance+2)
				})
			}
		}
	}
}

func TestBatchImageBalanceCaptureRequiresItsOwnReservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := newBatchImageBalanceTerminalFixture(t)
	fixture.reserve(t, ctx, fixture.otherBatchID, 2)
	command := fixture.command(service.BatchImageCaptureRequestID(fixture.batchID), fixture.batchID, 1, 0.25)
	_, err := fixture.repo.CaptureBatchImageBalance(ctx, command)
	require.ErrorIs(t, err, errBatchImageBalanceNotReserved)
	fixture.assertBalance(t, ctx, 98, 2)
	// Rejection leaves no capture claim: a subsequent legitimate reservation
	// and capture must still succeed.
	fixture.reserve(t, ctx, fixture.batchID, 1)
	result, err := fixture.repo.CaptureBatchImageBalance(ctx, command)
	require.NoError(t, err)
	require.True(t, result.Applied)
	fixture.assertBalance(t, ctx, 97.75, 2)
	fixture.releaseOther(t, ctx, 99.75)
}

func TestBatchImageBalanceFreeCaptureNeedsNoReservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := newBatchImageBalanceTerminalFixture(t)
	fixture.reserve(t, ctx, fixture.otherBatchID, 2)
	command := fixture.command(service.BatchImageCaptureRequestID(fixture.batchID), fixture.batchID, 0, 0)
	for repeat := 0; repeat < 2; repeat++ {
		result, err := fixture.repo.CaptureBatchImageBalance(ctx, command)
		require.NoError(t, err)
		require.Equal(t, repeat == 0, result.Applied)
		fixture.assertBalance(t, ctx, 98, 2)
	}
	fixture.releaseOther(t, ctx, 100)
}

func TestBatchImageBalanceConcurrentCaptureAndReleaseHaveOneWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := newBatchImageBalanceTerminalFixture(t)
	fixture.reserve(t, ctx, fixture.batchID, 1)
	fixture.reserve(t, ctx, fixture.otherBatchID, 2)
	type outcome struct {
		capture bool
		applied bool
		err     error
	}
	const calls = 12
	start := make(chan struct{})
	results := make(chan outcome, calls)
	for i := 0; i < calls; i++ {
		go func(capture bool) {
			<-start
			apply := fixture.repo.ReleaseBatchImageBalance
			command := fixture.command(service.BatchImageReleaseRequestID(fixture.batchID), fixture.batchID, 1, 0)
			if capture {
				apply = fixture.repo.CaptureBatchImageBalance
				command = fixture.command(service.BatchImageCaptureRequestID(fixture.batchID), fixture.batchID, 1, 0.25)
			}
			result, err := apply(ctx, command)
			results <- outcome{capture: capture, applied: result != nil && result.Applied, err: err}
		}(i%2 == 0)
	}
	close(start)
	var outcomes []outcome
	var captureWon bool
	appliedCount := 0
	for i := 0; i < calls; i++ {
		result := <-results
		outcomes = append(outcomes, result)
		if result.applied {
			appliedCount++
			captureWon = result.capture
		}
	}
	require.Equal(t, 1, appliedCount, "only one terminal operation may mutate the balance")
	for _, result := range outcomes {
		if result.capture == captureWon {
			require.NoError(t, result.err, "same-operation deliveries must remain idempotent")
		} else if captureWon {
			require.ErrorIs(t, result.err, errBatchImageBalanceAlreadyCaptured)
		} else {
			require.ErrorIs(t, result.err, errBatchImageBalanceAlreadyReleased)
		}
	}
	wantBalance := 98.0
	if captureWon {
		wantBalance -= 0.25
	}
	fixture.assertBalance(t, ctx, wantBalance, 2)
	fixture.releaseOther(t, ctx, wantBalance+2)
}

type batchImageBalanceTerminalFixture struct {
	repo         service.UsageBillingRepository
	userID       int64
	keyID        int64
	batchID      string
	otherBatchID string
}

func newBatchImageBalanceTerminalFixture(t *testing.T) *batchImageBalanceTerminalFixture {
	t.Helper()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email: "batch-terminal-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 100,
	})
	key := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID, Key: "sk-batch-terminal-" + uuid.NewString(), Name: "batch terminal accounting",
	})
	return &batchImageBalanceTerminalFixture{
		repo: NewUsageBillingRepository(client, integrationDB), userID: user.ID, keyID: key.ID,
		batchID: "batch-terminal-" + uuid.NewString(), otherBatchID: "batch-other-" + uuid.NewString(),
	}
}

func (f *batchImageBalanceTerminalFixture) command(requestID, batchID string, hold, actual float64) *service.BatchImageBalanceHoldCommand {
	return &service.BatchImageBalanceHoldCommand{
		RequestID: requestID, BatchID: batchID, UserID: f.userID, APIKeyID: f.keyID, HoldAmount: hold, ActualAmount: actual,
	}
}

func (f *batchImageBalanceTerminalFixture) reserve(t *testing.T, ctx context.Context, batchID string, amount float64) {
	t.Helper()
	result, err := f.repo.ReserveBatchImageBalance(ctx, f.command(service.BatchImageHoldRequestID(batchID), batchID, amount, 0))
	require.NoError(t, err)
	require.True(t, result.Applied)
}

func (f *batchImageBalanceTerminalFixture) assertBalance(t *testing.T, ctx context.Context, wantBalance, wantFrozen float64) {
	t.Helper()
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance, frozen_balance FROM users WHERE id = $1`, f.userID).Scan(&balance, &frozen))
	require.InDelta(t, wantBalance, balance, 1e-9)
	require.InDelta(t, wantFrozen, frozen, 1e-9)
}

func (f *batchImageBalanceTerminalFixture) releaseOther(t *testing.T, ctx context.Context, wantBalance float64) {
	t.Helper()
	result, err := f.repo.ReleaseBatchImageBalance(ctx, f.command(service.BatchImageReleaseRequestID(f.otherBatchID), f.otherBatchID, 2, 0))
	require.NoError(t, err)
	require.True(t, result.Applied)
	f.assertBalance(t, ctx, wantBalance, 0)
}
