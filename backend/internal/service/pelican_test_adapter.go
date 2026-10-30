package service

import (
	"context"
	"fmt"
)

// groupScopedPelicanTestAdapter keeps group ownership at the execution
// boundary while reusing the official account test transport. It deliberately
// does not copy provider-specific request code.
type groupScopedPelicanTestAdapter struct {
	accountRepo     AccountRepository
	accountTestSvc  *AccountTestService
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
			return a.accountTestSvc.RunTestBackgroundWithPrompt(ctx, accountID, modelID, prompt)
		}
	}
	return nil, fmt.Errorf("account %d is not assigned to Pelican group %d", accountID, groupID)
}
