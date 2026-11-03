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

func (a *groupScopedPelicanTestAdapter) Run(ctx context.Context, accountID, groupID int64, modelID, prompt string) (*ScheduledTestResult, error) {
	if a == nil || a.accountRepo == nil || a.accountTestSvc == nil {
		return nil, fmt.Errorf("Pelican test adapter is unavailable")
	}
	if accountID <= 0 || groupID <= 0 {
		return nil, fmt.Errorf("Pelican tests require a valid account and group")
	}
	accounts, err := a.accountRepo.ListByGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("validate Pelican account group: %w", err)
	}
	for _, account := range accounts {
		if account.ID == accountID {
			return runPelicanTestWithTransientRetry(ctx, func(attemptCtx context.Context) (*ScheduledTestResult, error) {
				return a.accountTestSvc.RunTestBackgroundWithPrompt(attemptCtx, accountID, modelID, prompt)
			})
		}
	}
	return nil, fmt.Errorf("account %d is not assigned to Pelican group %d", accountID, groupID)
}

// runPelicanTestWithTransientRetry retries only a failed test with no response
// content when a response read failed transiently. The upstream may have
// completed and billed the first attempt even though no body reached us, so
// this intentionally permits at most one duplicate upstream charge.
func runPelicanTestWithTransientRetry(
	ctx context.Context,
	run func(context.Context) (*ScheduledTestResult, error),
) (*ScheduledTestResult, error) {
	first, firstErr := run(ctx)
	if ctx.Err() != nil || !isRetryablePelicanTransportFailure(first, firstErr) {
		return first, firstErr
	}
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return first, firstErr
	case <-timer.C:
	}

	second, secondErr := run(ctx)
	if second == nil {
		return second, secondErr
	}
	if first != nil {
		if !first.StartedAt.IsZero() && (second.StartedAt.IsZero() || first.StartedAt.Before(second.StartedAt)) {
			second.StartedAt = first.StartedAt
		}
		if !second.FinishedAt.IsZero() && !second.StartedAt.IsZero() {
			second.LatencyMs = second.FinishedAt.Sub(second.StartedAt).Milliseconds()
		}
	}
	return second, secondErr
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
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}
