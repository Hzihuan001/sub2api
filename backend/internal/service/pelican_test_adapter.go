package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// groupScopedPelicanTestAdapter keeps group ownership at the execution
// boundary while reusing the official account test transport. It deliberately
// does not copy provider-specific request code.
type groupScopedPelicanTestAdapter struct {
	accountRepo    AccountRepository
	accountTestSvc *AccountTestService
}

func NewPelicanTestAdapter(accountRepo AccountRepository, accountTestSvc *AccountTestService) PelicanTestExecutor {
	return &groupScopedPelicanTestAdapter{accountRepo: accountRepo, accountTestSvc: accountTestSvc}
}

func (a *groupScopedPelicanTestAdapter) Run(ctx context.Context, accountID, groupID int64, modelID, prompt, thinkingEffort string) (*ScheduledTestResult, error) {
	if a == nil || a.accountRepo == nil || a.accountTestSvc == nil {
		return nil, fmt.Errorf("pelican test adapter is unavailable")
	}
	if accountID <= 0 || groupID <= 0 {
		return nil, fmt.Errorf("pelican tests require a valid account and group")
	}
	accounts, err := a.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("validate Pelican account group: %w", err)
	}
	for _, account := range accounts {
		if account.ID == accountID {
			return runPelicanTestWithTransientRetry(ctx, func(attemptCtx context.Context) (*ScheduledTestResult, error) {
				return a.accountTestSvc.RunTestBackgroundWithPrompt(attemptCtx, accountID, modelID, prompt, AccountTestOptions{ThinkingEffort: thinkingEffort})
			})
		}
	}
	return nil, fmt.Errorf("account %d is not assigned to Pelican group %d", accountID, groupID)
}

// runPelicanTestWithTransientRetry retries only a failed test with no response
// content when a response read failed transiently. The upstream may have
// completed and billed an attempt even though no body reached us, so this uses
// a small fixed attempt budget and never retries after response content exists.
func runPelicanTestWithTransientRetry(
	ctx context.Context,
	run func(context.Context) (*ScheduledTestResult, error),
) (*ScheduledTestResult, error) {
	const maxAttempts = 3
	retryDelays := [...]time.Duration{250 * time.Millisecond, 750 * time.Millisecond}
	var earliestStartedAt time.Time

	for attempt := 1; ; attempt++ {
		result, runErr := run(ctx)
		if result != nil && !result.StartedAt.IsZero() && (earliestStartedAt.IsZero() || result.StartedAt.Before(earliestStartedAt)) {
			earliestStartedAt = result.StartedAt
		}
		if ctx.Err() != nil || attempt >= maxAttempts || !isRetryablePelicanTransportFailure(result, runErr) {
			if result != nil && !earliestStartedAt.IsZero() {
				result.StartedAt = earliestStartedAt
				if !result.FinishedAt.IsZero() {
					result.LatencyMs = result.FinishedAt.Sub(result.StartedAt).Milliseconds()
				}
			}
			return result, runErr
		}

		timer := time.NewTimer(retryDelays[attempt-1])
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, runErr
		case <-timer.C:
		}
	}
}

func isRetryablePelicanTransportFailure(result *ScheduledTestResult, err error) bool {
	if result != nil && (result.Status == "success" || strings.TrimSpace(result.ResponseText) != "") {
		return false
	}
	message := ""
	if err != nil {
		message += err.Error()
	}
	if result != nil {
		message += " " + result.ErrorMessage
	}
	message = strings.ToLower(message)
	for _, marker := range []string{"api returned ", "http ", "status code", "status: "} {
		if strings.Contains(message, marker) {
			return false
		}
	}
	if strings.Contains(message, "stream ended before response.completed") {
		return true
	}
	// Only response-read failures are retried. A generic request/transport
	// error does not reveal whether the request was already accepted upstream.
	if !strings.Contains(message, "stream read error:") && !strings.Contains(message, "failed to read response:") {
		return false
	}
	for _, marker := range []string{
		"connection reset by peer",
		"connection reset",
		"unexpected eof",
		"server closed idle connection",
		"broken pipe",
		"tls handshake timeout",
		"i/o timeout",
		"stream ended before response.completed",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
