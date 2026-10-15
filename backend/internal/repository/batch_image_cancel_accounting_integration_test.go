//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestBatchImageCancellation_PostgresAccountingRecovery(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		lateFailure    bool
		captureLost    bool
		usageLost      bool
		completionLost bool
		allFailed      bool
		priorFailures  int
		unrelatedHold  float64
	}{
		{name: "late_success"},
		{name: "explicit_generation_failure", lateFailure: true},
		{name: "capture_committed_response_lost", captureLost: true},
		{name: "usage_committed_response_lost", usageLost: true},
		{name: "completion_committed_response_lost", completionLost: true},
		{name: "zero_success_capture_lost_at_former_retry_limit", allFailed: true, captureLost: true, priorFailures: 4, unrelatedHold: 0.75},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			client := testEntClient(t)
			user := mustCreateUser(t, client, &service.User{
				Email: "batch-cancel-" + uuid.NewString() + "@example.com", PasswordHash: "hash", Balance: 100,
			})
			key := mustCreateApiKey(t, client, &service.APIKey{
				UserID: user.ID, Key: "sk-batch-cancel-" + uuid.NewString(), Name: "synthetic cancellation",
			})
			var calls atomic.Int32
			secondStarted, releaseSecond := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseSecond) }) }
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/v1/images/generations" {
					http.Error(w, "unexpected image endpoint", http.StatusBadRequest)
					return
				}
				call := calls.Add(1)
				if call == 2 {
					close(secondStarted)
					select {
					case <-releaseSecond:
					case <-r.Context().Done():
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if scenario.allFailed || (call == 2 && scenario.lateFailure) {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprint(w, `{"error":{"message":"explicit synthetic generation failure"}}`)
					return
				}
				_, _ = fmt.Fprint(w, `{"data":[{"b64_json":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a1ZkAAAAASUVORK5CYII="}]}`)
			}))
			// Always release a blocked in-flight request before closing the server.
			// This keeps failure cleanup deterministic when the test fails before the
			// processor drains the second request.
			defer func() {
				release()
				upstream.Close()
			}()
			account := mustCreateAccount(t, client, &service.Account{
				Name: "batch-cancel-" + uuid.NewString(), Type: service.AccountTypeAPIKey, Platform: service.PlatformOpenAI,
				Credentials: map[string]any{"api_key": "synthetic-upstream-key", "base_url": upstream.URL},
			})
			baseRepo := NewBatchImageRepository(integrationDB)
			repo := &cancelRecoveryJobRepository{BatchImageRepository: baseRepo, loseCompletion: scenario.completionLost}
			billing := &cancelRecoveryBillingRepository{
				UsageBillingRepository: NewUsageBillingRepository(client, integrationDB), loseCapture: scenario.captureLost,
			}
			usage := &cancelRecoveryUsageRepository{
				UsageLogRepository: NewUsageLogRepository(client, integrationDB), loseCreate: scenario.usageLost,
			}
			// Use the production ID generator: its compact ID keeps the stable
			// capture request ID within the usage table's 64-character contract.
			batchID, err := service.NewBatchImageID()
			require.NoError(t, err)
			require.LessOrEqual(t, len(service.BatchImageCaptureRequestID(batchID)), 64)
			providerName := "managed:" + batchID
			hold := 0.75
			job, err := repo.CreateBatchImageJob(ctx, service.CreateBatchImageJobParams{
				BatchID: batchID, UserID: user.ID, APIKeyID: &key.ID, AccountID: &account.ID,
				Provider: service.BatchImageProviderAppManaged, Model: "gpt-image-1",
				Status: service.BatchImageJobStatusRunning, ProviderJobName: &providerName,
				ItemCount: 3, EstimatedCost: hold, HoldAmount: &hold, BaseUnitPrice: 0.25,
				GroupRateMultiplier: 1, AccountRateMultiplier: 1, BatchDiscountMultiplier: 1,
				HoldMultiplier: 1, BillableUnitPrice: 0.25, HoldUnitPrice: 0.25, PricingSnapshotVersion: 2,
			})
			require.NoError(t, err)
			for _, id := range []string{"one", "two", "three"} {
				_, err := repo.CreateBatchImageItem(ctx, service.CreateBatchImageItemParams{
					JobID: batchID, CustomID: id, Status: service.BatchImageItemStatusPending,
					InputPayload: []byte(fmt.Sprintf(`{"item":{"custom_id":%q,"prompt":"synthetic test"},"image_size":"2K"}`, id)),
				})
				require.NoError(t, err)
			}
			_, err = billing.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
				RequestID: service.BatchImageHoldRequestID(batchID), UserID: user.ID,
				APIKeyID: key.ID, BatchID: batchID, HoldAmount: hold,
			})
			require.NoError(t, err)
			if scenario.unrelatedHold > 0 {
				otherBatchID, idErr := service.NewBatchImageID()
				require.NoError(t, idErr)
				_, err = billing.ReserveBatchImageBalance(ctx, &service.BatchImageBalanceHoldCommand{
					RequestID: service.BatchImageHoldRequestID(otherBatchID), UserID: user.ID,
					APIKeyID: key.ID, BatchID: otherBatchID, HoldAmount: scenario.unrelatedHold,
				})
				require.NoError(t, err)
			}
			assertCancelRecoveryBalance(t, user.ID, 100-hold-scenario.unrelatedHold, hold+scenario.unrelatedHold)
			provider := service.NewAppManagedBatchImageProvider(service.AppManagedBatchImageProviderOptions{
				ItemRepo: baseRepo, ResultDir: t.TempDir(), Concurrency: 1,
			})
			newPipeline := func() *service.BatchImagePipelineProcessor {
				return &service.BatchImagePipelineProcessor{
					ProviderProcessor: &service.BatchImageProviderProcessor{
						Repo: repo, ProviderRegistry: service.NewBatchImageProviderRegistry(provider),
						AccountResolver: &service.BatchImageAccountRepositoryResolver{Repo: NewAccountRepository(client, integrationDB, nil)}, BillingRepo: billing,
					},
					SettlementService: &service.BatchImageSettlementService{
						Repo: repo, BillingRepo: billing, UsageLogRepo: usage, Pricing: cancelRecoveryPricing{},
					},
				}
			}
			done := make(chan error, 1)
			go func() {
				_, err := newPipeline().Process(ctx, batchID)
				done <- err
			}()
			select {
			case <-secondStarted:
			case <-ctx.Done():
				t.Fatal("second image request did not start")
			}
			require.NoError(t, provider.Cancel(ctx, job, account))
			release()
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("provider did not drain its in-flight request after cancellation")
			}
			for i := 0; i < scenario.priorFailures; i++ {
				_, err = repo.SetBatchImageJobSettlementFailed(ctx, batchID, "SETTLEMENT_BILLING_FAILED", "synthetic earlier outage")
				require.NoError(t, err)
			}
			// A new processor instance reuses durable repositories at each delivery.
			// Lost responses deliberately leave the preceding database commit intact.
			for attempt := 0; attempt < 4; attempt++ {
				processResult, processErr := newPipeline().Process(ctx, batchID)
				if processErr != nil {
					require.True(t, scenario.completionLost, "unexpected pipeline error: %v", processErr)
					require.EqualError(t, processErr, "synthetic completion response lost after commit")
				}
				job, err = repo.GetBatchImageJobByBatchID(ctx, batchID)
				require.NoError(t, err)
				t.Logf("settlement attempt=%d status=%s retry=%d result=%+v err=%v", attempt, job.Status, job.RetryCount, processResult, processErr)
				if job.LastErrorCode != nil {
					t.Logf("settlement code=%s", *job.LastErrorCode)
				}
				if job.LastErrorMessage != nil {
					t.Logf("settlement detail=%s", *job.LastErrorMessage)
				}
				if job.Status == service.BatchImageJobStatusCompleted {
					break
				}
				require.Equal(t, service.BatchImageJobStatusSettling, job.Status)
				if scenario.allFailed && scenario.captureLost && attempt == 0 {
					// The zero-cost capture already returned this batch's hold.
					// Losing its response must not release another batch's funds.
					assertCancelRecoveryBalance(t, user.ID, 100-scenario.unrelatedHold, scenario.unrelatedHold)
				}
			}
			wantSuccess := 2
			if scenario.lateFailure {
				wantSuccess = 1
			}
			if scenario.allFailed {
				wantSuccess = 0
			}
			wantCost := float64(wantSuccess) * 0.25
			require.Equal(t, service.BatchImageJobStatusCompleted, job.Status)
			require.Equal(t, wantSuccess, job.SuccessCount)
			require.Equal(t, 3-wantSuccess, job.FailCount)
			require.NotNil(t, job.ActualCost)
			require.InDelta(t, wantCost, *job.ActualCost, 1e-9)
			require.EqualValues(t, 2, calls.Load(), "cancellation must stop the waiting item")
			require.False(t, billing.loseCapture, "capture response-loss injection must have been consumed")
			require.False(t, usage.loseCreate, "usage response-loss injection must have been consumed")
			require.False(t, repo.loseCompletion, "completion response-loss injection must have been consumed")
			assertCancelRecoveryBalance(t, user.ID, 100-wantCost-scenario.unrelatedHold, scenario.unrelatedHold)
			for repeat := 0; repeat < 2; repeat++ {
				require.NoError(t, provider.Cancel(ctx, job, account))
				_, err = newPipeline().Process(ctx, batchID)
				require.NoError(t, err)
			}
			assertCancelRecoveryBalance(t, user.ID, 100-wantCost-scenario.unrelatedHold, scenario.unrelatedHold)
			var rows, images, captureEntries, releaseEntries int
			var loggedCost float64
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT COUNT(*), COALESCE(SUM(image_count),0), COALESCE(SUM(actual_cost),0) FROM usage_logs WHERE request_id=$1 AND api_key_id=$2`,
				service.BatchImageCaptureRequestID(batchID), key.ID).Scan(&rows, &images, &loggedCost))
			require.Equal(t, 1, rows)
			require.Equal(t, wantSuccess, images)
			require.InDelta(t, wantCost, loggedCost, 1e-9)
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`,
				service.BatchImageCaptureRequestID(batchID), key.ID).Scan(&captureEntries))
			require.Equal(t, 1, captureEntries)
			require.NoError(t, integrationDB.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2`,
				service.BatchImageReleaseRequestID(batchID), key.ID).Scan(&releaseEntries))
			require.Zero(t, releaseEntries, "successful output must not take the full-refund path")
		})
	}
}

func assertCancelRecoveryBalance(t *testing.T, userID int64, balance, frozen float64) {
	t.Helper()
	var actualBalance, actualFrozen float64
	require.NoError(t, integrationDB.QueryRow("SELECT balance, frozen_balance FROM users WHERE id=$1", userID).Scan(&actualBalance, &actualFrozen))
	require.InDelta(t, balance, actualBalance, 1e-9)
	require.InDelta(t, frozen, actualFrozen, 1e-9)
}

type cancelRecoveryPricing struct{}

func (cancelRecoveryPricing) BatchImageUnitPrice(context.Context, *service.BatchImageJob) (float64, error) {
	return 0.25, nil
}

type cancelRecoveryBillingRepository struct {
	service.UsageBillingRepository
	loseCapture bool
}

func (r *cancelRecoveryBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	result, err := r.UsageBillingRepository.CaptureBatchImageBalance(ctx, cmd)
	if err == nil && r.loseCapture {
		r.loseCapture = false
		return nil, errors.New("synthetic capture response lost after commit")
	}
	return result, err
}

type cancelRecoveryUsageRepository struct {
	service.UsageLogRepository
	loseCreate bool
}

func (r *cancelRecoveryUsageRepository) Create(ctx context.Context, log *service.UsageLog) (bool, error) {
	inserted, err := r.UsageLogRepository.Create(ctx, log)
	if err == nil && r.loseCreate {
		r.loseCreate = false
		return false, errors.New("synthetic usage response lost after commit")
	}
	return inserted, err
}

type cancelRecoveryJobRepository struct {
	service.BatchImageRepository
	loseCompletion bool
}

func (r *cancelRecoveryJobRepository) MarkBatchImageJobSettled(ctx context.Context, params service.MarkBatchImageJobSettledParams) error {
	err := r.BatchImageRepository.MarkBatchImageJobSettled(ctx, params)
	if err == nil && r.loseCompletion {
		r.loseCompletion = false
		return errors.New("synthetic completion response lost after commit")
	}
	return err
}
