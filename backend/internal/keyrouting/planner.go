// Package keyrouting selects one API key group before a request is forwarded.
// Planning is read-only: it does not reserve an account, retry an upstream
// request, or perform billing. A successful plan is a point-in-time decision,
// not a guarantee that capacity will remain available when scheduling begins.
package keyrouting

import (
	"context"
	"errors"
	"fmt"
)

const MaxBackupGroups = 4

var (
	ErrInvalidConfig         = errors.New("invalid key routing configuration")
	ErrNotAuthorized         = errors.New("key routing group is not authorized")
	ErrModelUnavailable      = errors.New("model or endpoint is unavailable in the configured groups")
	ErrNoSchedulableAccounts = errors.New("no schedulable accounts in the configured groups")
)

// Config keeps the key's existing primary group and explicitly ordered backups.
// All IDs must be positive and distinct, including the primary group.
type Config struct {
	PrimaryGroupID int64
	BackupGroupIDs []int64
}

// Request describes the capabilities required by the current request. The
// adapter is responsible for normalizing and validating model and endpoint.
type Request struct {
	Model    string
	Endpoint string
}

// CandidateState is a read-only observation of a group's current eligibility.
// Evaluators may stop reading as soon as an earlier prerequisite is false;
// later fields are ignored in authorization, enabled, model, endpoint order.
type CandidateState struct {
	Authorized             bool
	Enabled                bool
	SupportsModel          bool
	SupportsEndpoint       bool
	HasSchedulableAccounts bool
}

// Evaluator reads eligibility without forwarding, reserving capacity, or
// charging the user. Read failures must be returned as errors, never encoded
// as HasSchedulableAccounts=false. Implementations must honor ctx cancellation.
type Evaluator interface {
	Evaluate(ctx context.Context, groupID int64, request Request) (CandidateState, error)
}

// EvaluatorFunc adapts a read-only function to Evaluator.
type EvaluatorFunc func(context.Context, int64, Request) (CandidateState, error)

func (f EvaluatorFunc) Evaluate(ctx context.Context, groupID int64, request Request) (CandidateState, error) {
	return f(ctx, groupID, request)
}

type Decision string

const (
	DecisionEligible              Decision = "eligible"
	DecisionUnsupportedModel      Decision = "unsupported_model"
	DecisionUnsupportedEndpoint   Decision = "unsupported_endpoint"
	DecisionNoSchedulableAccounts Decision = "no_schedulable_accounts"
	DecisionNotAuthorized         Decision = "not_authorized"
	DecisionDisabled              Decision = "disabled"
)

type Reason string

const (
	ReasonPrimary               Reason = "primary"
	ReasonModelUnavailable      Reason = "model_unavailable"
	ReasonNoSchedulableAccounts Reason = "no_schedulable_accounts"
)

// Attempt records each evaluated group in order for operational tracing. On a
// read error or cancellation during evaluation, Decision is empty and Err is
// populated: a failed read is not an eligibility decision.
type Attempt struct {
	GroupID  int64
	Decision Decision
	Err      error
}

// Plan contains a selection only on success. Attempts are retained on failure.
// Reason explains why the primary group was selected or skipped; it is empty
// if no group was selected.
type Plan struct {
	SelectedGroupID int64
	Reason          Reason
	Attempts        []Attempt
}

// Planner has no mutable routing state. Evaluator implementations determine
// whether the same Planner may safely be used by concurrent requests.
type Planner struct {
	Evaluator Evaluator
}

// Plan checks the primary group, then backups in their configured order. It
// stops at the first eligible group. Revoked authorization and read failures
// fail closed immediately, even when later groups might otherwise be usable.
// Disabled groups may fall through, just like groups without available accounts.
func (p Planner) Plan(ctx context.Context, config Config, request Request) (Plan, error) {
	var plan Plan
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	groupIDs, err := candidateIDs(config)
	if err != nil {
		return plan, err
	}
	if p.Evaluator == nil {
		return plan, fmt.Errorf("%w: evaluator is required", ErrInvalidConfig)
	}

	primaryReason := ReasonPrimary
	hasUnavailableCapacity := false
	for index, groupID := range groupIDs {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		state, evaluationErr := p.Evaluator.Evaluate(ctx, groupID, request)
		// An evaluator may finish just as cancellation arrives. Never return a
		// successful selection, or start another evaluation, after observing it.
		if err := ctx.Err(); err != nil {
			plan.Attempts = append(plan.Attempts, Attempt{GroupID: groupID, Err: err})
			return plan, err
		}
		if evaluationErr != nil {
			plan.Attempts = append(plan.Attempts, Attempt{GroupID: groupID, Err: evaluationErr})
			return plan, fmt.Errorf("evaluate key routing group %d: %w", groupID, evaluationErr)
		}

		decision := decide(state)
		plan.Attempts = append(plan.Attempts, Attempt{GroupID: groupID, Decision: decision})
		switch decision {
		case DecisionEligible:
			plan.SelectedGroupID = groupID
			plan.Reason = primaryReason
			return plan, nil
		case DecisionNotAuthorized:
			return plan, fmt.Errorf("%w: group %d", ErrNotAuthorized, groupID)
		case DecisionDisabled, DecisionNoSchedulableAccounts:
			hasUnavailableCapacity = true
			if index == 0 {
				primaryReason = ReasonNoSchedulableAccounts
			}
		case DecisionUnsupportedModel, DecisionUnsupportedEndpoint:
			if index == 0 {
				primaryReason = ReasonModelUnavailable
			}
		}
	}
	if hasUnavailableCapacity {
		return plan, ErrNoSchedulableAccounts
	}
	return plan, ErrModelUnavailable
}

func decide(state CandidateState) Decision {
	switch {
	case !state.Authorized:
		return DecisionNotAuthorized
	case !state.Enabled:
		return DecisionDisabled
	case !state.SupportsModel:
		return DecisionUnsupportedModel
	case !state.SupportsEndpoint:
		return DecisionUnsupportedEndpoint
	case !state.HasSchedulableAccounts:
		return DecisionNoSchedulableAccounts
	default:
		return DecisionEligible
	}
}

func candidateIDs(config Config) ([]int64, error) {
	if config.PrimaryGroupID <= 0 {
		return nil, fmt.Errorf("%w: primary group ID must be positive", ErrInvalidConfig)
	}
	if len(config.BackupGroupIDs) > MaxBackupGroups {
		return nil, fmt.Errorf("%w: at most %d backup groups are allowed", ErrInvalidConfig, MaxBackupGroups)
	}
	groupIDs := make([]int64, 1, 1+len(config.BackupGroupIDs))
	groupIDs[0] = config.PrimaryGroupID
	seen := map[int64]struct{}{config.PrimaryGroupID: {}}
	for _, groupID := range config.BackupGroupIDs {
		if groupID <= 0 {
			return nil, fmt.Errorf("%w: backup group ID must be positive", ErrInvalidConfig)
		}
		if _, exists := seen[groupID]; exists {
			return nil, fmt.Errorf("%w: duplicate group ID %d", ErrInvalidConfig, groupID)
		}
		seen[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}
	return groupIDs, nil
}
