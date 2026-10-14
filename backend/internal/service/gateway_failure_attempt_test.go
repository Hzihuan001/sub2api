package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayFailureReplayDecisionPreservesMetadata(t *testing.T) {
	for _, tt := range []struct {
		name, body, message string
		status              int
		retry               bool
	}{
		{"unknown", `{}`, "", 502, false},
		{"pool retry", `{}`, "", 503, true},
		{"credential", `{"error":{"code":"invalid_api_key"}}`, "", 401, true},
		{"access state", `{"error":{"code":"account_suspended"}}`, "", 403, true},
		{"hard quota", `{"error":{"code":"insufficient_quota"}}`, "", 429, true},
		{"missing model", `{"error":{"code":"model_not_found"}}`, "", 404, true},
		{"body limit", `{"error":{"message":"Request payload is too large"}}`, "Request payload is too large", 413, true},
		{"context limit", `{"error":{"code":"context_length_exceeded"}}`, "", 413, false},
		{"capacity", `{"error":{"code":"server_is_overloaded","message":"server overloaded"}}`, "server overloaded", 503, false},
		{"capacity timeout", `{"error":{"code":"server_is_overloaded","message":"server overloaded"}}`, "server overloaded", 504, true},
		{"edge timeout", `{}`, "", 524, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(tt.body)
			facts := openAIFailureReplayFacts{
				class:                 classifyOpenAIGatewayFailure(tt.status, body, tt.message),
				statusCode:            tt.status,
				requestScopedCapacity: isOpenAIRequestScopedCapacityShed(tt.message, body),
				requestBodyTooLarge:   isOpenAIRequestBodyTooLargeError(tt.status, tt.message, body),
				accessStateFailure:    isOpenAIHTTPUpstreamAccessStateError(tt.status, tt.message, body),
				capacityClientMessage: openAICapacityShedClientMessage(tt.message, body),
			}
			state := openAIFailureReplayState{sameAccountRetryAllowed: tt.retry}
			decision := decideOpenAIFailureReplay(facts, state)
			legacy := newOpenAIUpstreamFailoverError(tt.status, nil, body, tt.message, tt.retry)
			require.Equal(t, legacy.RetryableOnSameAccount, decision.retryableOnSameAccount)
			require.Equal(t, legacy.RequestScopedTransient, decision.requestScopedTransient)
			require.Equal(t, legacy.NextAccountAction, decision.nextAccountAction)
			require.Equal(t, legacy.Stage, decision.stage)
			require.Equal(t, legacy.Scope, decision.scope)
			require.Equal(t, legacy.Reason, decision.reason)
			require.Equal(t, legacy.ClientStatusCode, decision.clientStatusCode)
			require.Equal(t, legacy.ClientMessage, decision.clientMessage)
			require.Equal(t, decision, decideOpenAIFailureReplay(facts, state))
		})
	}
}

func TestGatewayFailureReplayDecisionAccountBlockVetoPreservesCapacity(t *testing.T) {
	facts := openAIFailureReplayFacts{statusCode: http.StatusServiceUnavailable,
		requestScopedCapacity: true, capacityClientMessage: "server overloaded"}
	decision := decideOpenAIFailureReplay(facts, openAIFailureReplayState{accountDisabled: true})
	require.False(t, decision.retryableOnSameAccount)
	require.True(t, decision.requestScopedTransient)
	require.Equal(t, http.StatusServiceUnavailable, decision.clientStatusCode)
	require.Equal(t, "server overloaded", decision.clientMessage)
}

func TestGatewayFailureAttemptEffectsObserveOneFailurePerAttempt(t *testing.T) {
	var effects openAIFailureAttemptEffects
	require.True(t, effects.shouldObserve())
	effects.record(false)
	require.True(t, effects.shouldObserve(), "a request rejection did not observe account state")
	effects.record(true)
	require.False(t, effects.shouldObserve(), "paired error and response.failed observe once")
	effects.record(false)
	require.False(t, effects.shouldObserve(), "a later non-account frame cannot erase observation")
	effects = openAIFailureAttemptEffects{}
	require.True(t, effects.shouldObserve(), "the next actual attempt must be counted")
	effects.record(true)
	var otherAttempt openAIFailureAttemptEffects
	require.True(t, otherAttempt.shouldObserve(), "another request with identical provider bytes is independent")
}
