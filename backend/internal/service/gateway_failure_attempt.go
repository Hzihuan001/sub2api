package service

import "net/http"

// openAIFailureReplayFacts contains normalized failure data only. Transport
// adapters extract it before asking for retry metadata; this function never
// reads a clock, account, cache, repository, or mutable service state.
type openAIFailureReplayFacts struct {
	class                 openAIGatewayFailureClass
	statusCode            int
	requestScopedCapacity bool
	requestBodyTooLarge   bool
	accessStateFailure    bool
	capacityClientMessage string
}

type openAIFailureReplayState struct {
	sameAccountRetryAllowed bool
	accountDisabled         bool
}

type openAIFailureReplayDecision struct {
	retryableOnSameAccount bool
	requestScopedTransient bool
	nextAccountAction      NextAccountAction
	stage                  GatewayFailureStage
	scope                  GatewayFailureScope
	reason                 GatewayFailureReason
	clientStatusCode       int
	clientMessage          string
}

// decideOpenAIFailureReplay preserves the .5 replay/client policy independently
// from how an adapter executes cooldown, credential refresh, or retry delay.
// An execution-time account block vetoes replay, but does not erase the typed
// error or rewrite the provider payload.
func decideOpenAIFailureReplay(facts openAIFailureReplayFacts, state openAIFailureReplayState) openAIFailureReplayDecision {
	decision := openAIFailureReplayDecision{
		retryableOnSameAccount: state.sameAccountRetryAllowed || facts.requestScopedCapacity,
		requestScopedTransient: facts.requestScopedCapacity,
	}
	if facts.class == openAIGatewayFailureCredential || facts.class == openAIGatewayFailureQuota ||
		facts.class == openAIGatewayFailureModelUnavailable || facts.statusCode == http.StatusGatewayTimeout ||
		(facts.statusCode >= 520 && facts.statusCode <= 524) {
		decision.retryableOnSameAccount = false
		decision.requestScopedTransient = false
		decision.nextAccountAction = NextAccountRetry
	}
	if facts.class == openAIGatewayFailureCredential {
		decision.stage = GatewayFailureStageAccountAuth
		decision.scope = GatewayFailureScopeAccount
		decision.reason = GatewayFailureReason("openai_upstream_credential")
		decision.clientStatusCode = http.StatusBadGateway
		decision.clientMessage = openAIUpstreamAccessUnavailableClientMessage
	}
	if facts.requestBodyTooLarge {
		decision.retryableOnSameAccount = false
		decision.requestScopedTransient = false
		decision.scope = GatewayFailureScopeAccount
		decision.reason = openAIRequestBodyTooLargeReason
		decision.nextAccountAction = NextAccountRetry
		decision.clientStatusCode = http.StatusRequestEntityTooLarge
		decision.clientMessage = OpenAIRequestBodyTooLargeClientMessage
	}
	if facts.accessStateFailure {
		decision.retryableOnSameAccount = false
		decision.requestScopedTransient = false
		decision.stage = GatewayFailureStageAccountAuth
		decision.scope = GatewayFailureScopeAccount
		decision.reason = OpenAIUpstreamAccessStateReason
		decision.nextAccountAction = NextAccountRetry
		decision.clientStatusCode = http.StatusBadGateway
		decision.clientMessage = openAIUpstreamAccessUnavailableClientMessage
	} else if facts.requestScopedCapacity {
		decision.clientStatusCode = http.StatusServiceUnavailable
		decision.clientMessage = facts.capacityClientMessage
	}
	if state.accountDisabled {
		decision.retryableOnSameAccount = false
	}
	return decision
}

// openAIFailureAttemptEffects belongs to a transport-owned observation scope.
// The pooled WS and HTTP bridge scope is one real upstream send/read loop;
// passthrough supplies the corresponding write-owned scope after correlating
// the response identity and excluding frames from already completed turns.
// An error frame followed by response.failed observes account state only once
// within that scope; the next scope starts with a new value. Observed is
// deliberately different from "account disabled": the first overload below
// its threshold is already counted even though it did not disable the account.
//
// The transport's serialized reader owns this value. It is not a cross-request
// deduper and intentionally has no global key, timer, or shared synchronization.
type openAIFailureAttemptEffects struct {
	observed bool
}

func (s *openAIFailureAttemptEffects) shouldObserve() bool { return s != nil && !s.observed }

func (s *openAIFailureAttemptEffects) record(observed bool) {
	s.observed = s.observed || observed
}
