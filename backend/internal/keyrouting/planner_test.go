package keyrouting

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPlannerPlan(t *testing.T) {
	eligible := CandidateState{true, true, true, true, true}
	noModel := CandidateState{true, true, false, true, true}
	noEndpoint := CandidateState{true, true, true, false, true}
	noAccounts := CandidateState{true, true, true, true, false}
	disabled := CandidateState{Authorized: true}
	notAuthorized := CandidateState{}
	request := Request{Model: "text-model", Endpoint: "/v1/responses"}
	tests := []struct {
		name       string
		config     Config
		states     map[int64]CandidateState
		wantGroup  int64
		wantReason Reason
		wantCalls  []int64
		wantSteps  []Decision
		wantErr    error
	}{
		{
			name:   "default primary only",
			config: Config{PrimaryGroupID: 10}, states: map[int64]CandidateState{10: eligible},
			wantGroup: 10, wantReason: ReasonPrimary,
			wantCalls: []int64{10}, wantSteps: []Decision{DecisionEligible},
		},
		{
			name:   "eligible primary stops before backups",
			config: Config{10, []int64{30, 20}}, states: map[int64]CandidateState{10: eligible},
			wantGroup: 10, wantReason: ReasonPrimary,
			wantCalls: []int64{10}, wantSteps: []Decision{DecisionEligible},
		},
		{
			name:   "primary does not support model",
			config: Config{10, []int64{30, 20}}, states: map[int64]CandidateState{10: noModel, 30: eligible},
			wantGroup: 30, wantReason: ReasonModelUnavailable,
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionUnsupportedModel, DecisionEligible},
		},
		{
			name:   "endpoint mismatch falls through",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: noEndpoint, 30: eligible},
			wantGroup: 30, wantReason: ReasonModelUnavailable,
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionUnsupportedEndpoint, DecisionEligible},
		},
		{
			name:   "all accounts paused on primary",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: noAccounts, 30: eligible},
			wantGroup: 30, wantReason: ReasonNoSchedulableAccounts,
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionNoSchedulableAccounts, DecisionEligible},
		},
		{
			name:   "disabled primary is skipped",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: disabled, 30: eligible},
			wantGroup: 30, wantReason: ReasonNoSchedulableAccounts,
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionDisabled, DecisionEligible},
		},
		{
			name:      "all four backups retain configured order",
			config:    Config{10, []int64{50, 30, 40, 20}},
			states:    map[int64]CandidateState{10: noModel, 50: noEndpoint, 30: disabled, 40: noAccounts, 20: eligible},
			wantGroup: 20, wantReason: ReasonModelUnavailable,
			wantCalls: []int64{10, 50, 30, 40, 20},
			wantSteps: []Decision{DecisionUnsupportedModel, DecisionUnsupportedEndpoint, DecisionDisabled, DecisionNoSchedulableAccounts, DecisionEligible},
		},
		{
			name:   "primary and all backups lack model",
			config: Config{10, []int64{30, 20}}, states: map[int64]CandidateState{10: noModel, 30: noModel, 20: noModel},
			wantCalls: []int64{10, 30, 20}, wantSteps: []Decision{DecisionUnsupportedModel, DecisionUnsupportedModel, DecisionUnsupportedModel},
			wantErr: ErrModelUnavailable,
		},
		{
			name:   "all endpoints unavailable",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: noEndpoint, 30: noEndpoint},
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionUnsupportedEndpoint, DecisionUnsupportedEndpoint},
			wantErr: ErrModelUnavailable,
		},
		{
			name:   "all accounts paused in every group",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: noAccounts, 30: noAccounts},
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionNoSchedulableAccounts, DecisionNoSchedulableAccounts},
			wantErr: ErrNoSchedulableAccounts,
		},
		{
			name:   "all groups disabled",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: disabled, 30: disabled},
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionDisabled, DecisionDisabled},
			wantErr: ErrNoSchedulableAccounts,
		},
		{
			name:   "unsupported backups preserve a capacity failure",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: noAccounts, 30: noModel},
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionNoSchedulableAccounts, DecisionUnsupportedModel},
			wantErr: ErrNoSchedulableAccounts,
		},
		{
			name:   "backup supports model but has no capacity",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: noModel, 30: noAccounts},
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionUnsupportedModel, DecisionNoSchedulableAccounts},
			wantErr: ErrNoSchedulableAccounts,
		},
		{
			name:   "primary authorization revoked fails closed",
			config: Config{10, []int64{30}}, states: map[int64]CandidateState{10: notAuthorized, 30: eligible},
			wantCalls: []int64{10}, wantSteps: []Decision{DecisionNotAuthorized}, wantErr: ErrNotAuthorized,
		},
		{
			name:   "backup authorization revoked fails closed",
			config: Config{10, []int64{30, 20}}, states: map[int64]CandidateState{10: noModel, 30: notAuthorized, 20: eligible},
			wantCalls: []int64{10, 30}, wantSteps: []Decision{DecisionUnsupportedModel, DecisionNotAuthorized}, wantErr: ErrNotAuthorized,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []int64
			planner := Planner{Evaluator: EvaluatorFunc(func(ctx context.Context, groupID int64, gotRequest Request) (CandidateState, error) {
				if ctx == nil || gotRequest != request {
					t.Fatalf("evaluation lost request context: ctx=%v request=%+v", ctx, gotRequest)
				}
				calls = append(calls, groupID)
				state, ok := tt.states[groupID]
				if !ok {
					t.Fatalf("unexpected evaluation of group %d", groupID)
				}
				return state, nil
			})}
			plan, err := planner.Plan(context.Background(), tt.config, request)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Plan error = %v, want %v", err, tt.wantErr)
			}
			if plan.SelectedGroupID != tt.wantGroup || plan.Reason != tt.wantReason {
				t.Fatalf("selection = (%d, %q), want (%d, %q)", plan.SelectedGroupID, plan.Reason, tt.wantGroup, tt.wantReason)
			}
			if !reflect.DeepEqual(calls, tt.wantCalls) {
				t.Fatalf("evaluations = %v, want %v", calls, tt.wantCalls)
			}
			if len(plan.Attempts) != len(tt.wantSteps) {
				t.Fatalf("attempts = %+v, want decisions %v", plan.Attempts, tt.wantSteps)
			}
			for i, attempt := range plan.Attempts {
				if attempt.GroupID != tt.wantCalls[i] || attempt.Decision != tt.wantSteps[i] || attempt.Err != nil {
					t.Errorf("attempt %d = %+v, want group %d decision %q and no error", i, attempt, tt.wantCalls[i], tt.wantSteps[i])
				}
			}
		})
	}
}

func TestPlannerInvalidConfigDoesNotEvaluate(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{"missing primary", Config{}},
		{"negative primary", Config{PrimaryGroupID: -1}},
		{"too many backups", Config{1, []int64{2, 3, 4, 5, 6}}},
		{"missing backup", Config{1, []int64{0}}},
		{"negative backup", Config{1, []int64{-2}}},
		{"primary repeated as backup", Config{1, []int64{2, 1}}},
		{"duplicate backups", Config{1, []int64{2, 3, 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planner := Planner{Evaluator: EvaluatorFunc(func(context.Context, int64, Request) (CandidateState, error) {
				t.Fatal("invalid configuration must not evaluate any candidate")
				return CandidateState{}, nil
			})}
			plan, err := planner.Plan(context.Background(), tt.config, Request{})
			if !errors.Is(err, ErrInvalidConfig) || plan.SelectedGroupID != 0 || len(plan.Attempts) != 0 {
				t.Fatalf("Plan = (%+v, %v), want no attempts and ErrInvalidConfig", plan, err)
			}
		})
	}

	t.Run("missing evaluator", func(t *testing.T) {
		plan, err := (Planner{}).Plan(context.Background(), Config{PrimaryGroupID: 1}, Request{})
		if !errors.Is(err, ErrInvalidConfig) || plan.SelectedGroupID != 0 || len(plan.Attempts) != 0 {
			t.Fatalf("Plan = (%+v, %v), want no attempts and ErrInvalidConfig", plan, err)
		}
	})
}

func TestPlannerEvaluationErrorFailsClosed(t *testing.T) {
	readErr := errors.New("candidate lookup failed")
	tests := []struct {
		name    string
		failAt  int64
		wantIDs []int64
	}{
		{"primary read error", 1, []int64{1}},
		{"backup read error", 2, []int64{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []int64
			planner := Planner{Evaluator: EvaluatorFunc(func(_ context.Context, groupID int64, _ Request) (CandidateState, error) {
				calls = append(calls, groupID)
				if groupID == tt.failAt {
					return CandidateState{true, true, true, true, true}, readErr
				}
				return CandidateState{true, true, false, false, false}, nil
			})}
			plan, err := planner.Plan(context.Background(), Config{1, []int64{2, 3}}, Request{})
			if !errors.Is(err, readErr) || errors.Is(err, ErrNoSchedulableAccounts) || errors.Is(err, ErrModelUnavailable) {
				t.Fatalf("Plan error = %v, want preserved read error", err)
			}
			if plan.SelectedGroupID != 0 || plan.Reason != "" || !reflect.DeepEqual(calls, tt.wantIDs) || len(plan.Attempts) != len(calls) {
				t.Fatalf("unexpected selection or continuation after read error: plan=%+v calls=%v", plan, calls)
			}
			failedAttempt := plan.Attempts[len(plan.Attempts)-1]
			if failedAttempt.GroupID != tt.failAt || failedAttempt.Decision != "" || !errors.Is(failedAttempt.Err, readErr) {
				t.Fatalf("failed attempt lost the read error: %+v", failedAttempt)
			}
		})
	}
}

func TestPlannerCancellationStopsEvaluation(t *testing.T) {
	tests := []struct {
		name         string
		cancelBefore bool
		state        CandidateState
		wantCalls    int
	}{
		{"already canceled", true, CandidateState{}, 0},
		{"canceled while primary would be eligible", false, CandidateState{true, true, true, true, true}, 1},
		{"canceled before moving to backup", false, CandidateState{true, true, false, false, false}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.cancelBefore {
				cancel()
			}
			calls := 0
			planner := Planner{Evaluator: EvaluatorFunc(func(context.Context, int64, Request) (CandidateState, error) {
				calls++
				cancel()
				return tt.state, nil
			})}
			plan, err := planner.Plan(ctx, Config{1, []int64{2}}, Request{})
			if !errors.Is(err, context.Canceled) || plan.SelectedGroupID != 0 || plan.Reason != "" || calls != tt.wantCalls || len(plan.Attempts) != tt.wantCalls {
				t.Fatalf("Plan = (%+v, %v), calls=%d; want canceled with %d calls", plan, err, calls, tt.wantCalls)
			}
			if len(plan.Attempts) > 0 {
				attempt := plan.Attempts[0]
				if attempt.GroupID != 1 || attempt.Decision != "" || !errors.Is(attempt.Err, context.Canceled) {
					t.Fatalf("attempt must retain evaluation cancellation: %+v", attempt)
				}
			}
		})
	}
}
