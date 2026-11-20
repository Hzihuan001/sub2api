package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
)

// settleImageRelayUsage captures the fully priced event before applying it.
// A retry always uses the stored amount and request fingerprint.
func (s *OpenAIGatewayService) settleImageRelayUsage(ctx context.Context, usage *UsageLog, params *postUsageBillingParams, op ImageRelayOperation) error {
	runtime := s.imageRelayRuntime()
	if runtime == nil || runtime.journal == nil || s.usageBillingRepo == nil || s.usageLogRepo == nil {
		return ErrImageRelayOperationState
	}
	stored, err := runtime.journal.Lookup(op.Owner, op.ID)
	if err != nil {
		return err
	}
	if stored.Settlement == nil {
		if usage == nil || params == nil {
			return ErrImageRelayOperationInvalid
		}
		command := buildUsageBillingCommand("image-relay:"+op.ID, usage, params)
		if command == nil {
			return ErrImageRelayOperationInvalid
		}
		err := runtime.journal.PrepareSettlement(op.Owner, op.ID, ImageRelaySettlement{Command: *command, Usage: *usage}, time.Now())
		if err != nil {
			return err
		}
	}
	return s.applyStoredImageRelaySettlement(ctx, runtime, op, params)
}

func (s *OpenAIGatewayService) finishImageRelaySettlement(ctx context.Context, runtime *imageRelayRuntime, op ImageRelayOperation) error {
	return s.applyStoredImageRelaySettlement(ctx, runtime, op, nil)
}

func (s *OpenAIGatewayService) applyStoredImageRelaySettlement(ctx context.Context, runtime *imageRelayRuntime, operation ImageRelayOperation, liveParams *postUsageBillingParams) error {
	if runtime == nil || runtime.journal == nil || runtime.results == nil || s.usageBillingRepo == nil || s.usageLogRepo == nil {
		return ErrImageRelayOperationState
	}
	op, err := runtime.journal.Lookup(operation.Owner, operation.ID)
	if err != nil {
		return err
	}
	if op.Usable() {
		return nil
	}
	if op.State != ImageRelayResultReady || op.Result == nil || op.Settlement == nil {
		return ErrImageRelayOperationState
	}
	applyCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	result, err := runtime.results.Open(applyCtx, op.ID, op.Owner.APIKeyID)
	if err != nil {
		return err
	}
	defer result.Close()
	if result.Metadata.BodyBytes != op.Result.BodyBytes || result.Metadata.BodySHA256 != op.Result.BodySHA256 ||
		result.Metadata.ContentType != op.Result.ContentType {
		return imagerelay.ErrSpoolIntegrity
	}
	command := op.Settlement.Command
	applied, err := s.usageBillingRepo.Apply(applyCtx, &command)
	if err != nil {
		return err
	}
	if applied == nil {
		return ErrImageRelayOperationState
	}
	if liveParams != nil && applied.Applied {
		deps := s.billingDeps()
		if applied.APIKeyQuotaExhausted {
			if invalidator, ok := liveParams.APIKeyService.(apiKeyAuthCacheInvalidator); ok && liveParams.APIKey != nil && liveParams.APIKey.Key != "" {
				invalidator.InvalidateAuthCacheByKey(applyCtx, liveParams.APIKey.Key)
			}
		}
		finalizePostUsageBilling(applyCtx, liveParams, deps, applied)
	} else if s.billingCacheService != nil {
		// A restart has no live cache snapshots. Invalidate from the stored IDs
		// instead of decrementing the cached balance a second time.
		if command.BalanceCost > 0 {
			_ = s.billingCacheService.InvalidateUserBalance(applyCtx, command.UserID)
		}
		if command.SubscriptionCost > 0 && op.Settlement.Usage.GroupID != nil {
			_ = s.billingCacheService.InvalidateSubscription(applyCtx, command.UserID, *op.Settlement.Usage.GroupID)
		}
		if command.APIKeyRateLimitCost > 0 {
			_ = s.billingCacheService.InvalidateAPIKeyRateLimit(applyCtx, command.APIKeyID)
		}
	}

	// The permanent usage log uses the same dedup key. A successful no-op
	// after an earlier commit is sufficient for both repositories.
	usage := op.Settlement.Usage
	if _, err := s.usageLogRepo.Create(applyCtx, &usage); err != nil {
		return err
	}
	return runtime.journal.MarkSettled(op.Owner, op.ID, time.Now())
}

// RecoverImageRelaySettlements retries prepared billing commands. Calling it
// never dispatches an image request. Pending operations without a known result
// remain available for reconciliation.
func (s *OpenAIGatewayService) RecoverImageRelaySettlements(ctx context.Context, limit int) (int, error) {
	runtime := s.imageRelayRuntime()
	if runtime == nil || runtime.journal == nil {
		return 0, nil
	}
	pending, err := runtime.journal.PendingSettlements(limit)
	if err != nil {
		return 0, err
	}
	var failures error
	completed := 0
	for _, op := range pending {
		if err := ctx.Err(); err != nil {
			return completed, errors.Join(failures, err)
		}
		if err := s.finishImageRelaySettlement(ctx, runtime, op); err != nil {
			failures = errors.Join(failures, err, runtime.journal.deferSettlement(op, time.Now()))
			continue
		}
		completed++
	}
	return completed, failures
}
