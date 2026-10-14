package service

import (
	"bytes"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// openAIGatewayFailureDecisionKind identifies the transport envelope being
// classified.  The payload classifier is shared, while each envelope keeps
// its established semantic-status rules (HTTP, response.failed, or error).
//
// This type intentionally contains no account, repository, clock, or network
// state.  Callers supply the small account-derived booleans they already own
// and apply the returned verdict/effects in their transport adapter.
type openAIGatewayFailureDecisionKind uint8

const (
	openAIGatewayFailureHTTP openAIGatewayFailureDecisionKind = iota
	openAIGatewayFailurePassthrough
	openAIGatewayFailureResponseFailed
	openAIGatewayFailureErrorEvent
)

// openAIGatewayFailureDecisionInput is the pure input to the failure policy.
// Body is treated as immutable by the classifier; no caller-owned bytes are
// changed.  AllowModelFailover and PoolRetryable are account-selection facts
// computed by the adapter, not repository lookups performed here.
type openAIGatewayFailureDecisionInput struct {
	kind               openAIGatewayFailureDecisionKind
	statusCode         int
	body               []byte
	message            string
	allowModelFailover bool
	poolRetryable      bool
}

// openAIGatewayFailureDecision is deliberately a value object.  It only
// describes the policy result; persistence, cooldowns, retries, response
// rendering, and billing remain the responsibility of existing adapters.
type openAIGatewayFailureDecision struct {
	failureClass   openAIGatewayFailureClass
	semanticStatus int
	shouldFailover bool
}

func decideOpenAIGatewayFailure(input openAIGatewayFailureDecisionInput) openAIGatewayFailureDecision {
	classificationStatus := input.statusCode
	if input.kind == openAIGatewayFailureResponseFailed || input.kind == openAIGatewayFailureErrorEvent {
		classificationStatus = 0
	}
	decision := openAIGatewayFailureDecision{
		failureClass:   classifyOpenAIGatewayFailure(classificationStatus, input.body, input.message),
		semanticStatus: input.statusCode,
	}

	switch input.kind {
	case openAIGatewayFailureResponseFailed:
		decision.semanticStatus = openAIStreamFailureStatusForClass(input.body, input.message, decision.failureClass)
		decision.shouldFailover = shouldFailoverOpenAIResponseFailedDecision(input, decision)
	case openAIGatewayFailureErrorEvent:
		decision.semanticStatus = openAIStreamSemanticStatusForClass(input.body, input.message, decision.failureClass)
		decision.shouldFailover = shouldFailoverOpenAIErrorEventDecision(input, decision)
	default:
		decision.shouldFailover = shouldFailoverOpenAIHTTPDecision(input, decision)
	}
	return decision
}

// Callers with an existing classification reuse it instead of parsing the same
// provider error for another classification in each transport helper.
func openAIStreamSemanticStatusForClass(payload []byte, message string, class openAIGatewayFailureClass) int {
	switch class {
	case openAIGatewayFailureRequest:
		for _, signal := range openAIGatewayFailureSignals(payload) {
			switch signal {
			case "permission_error", "permission_denied", "forbidden", "access_denied":
				return http.StatusForbidden
			}
		}
		return http.StatusBadRequest
	case openAIGatewayFailureQuota:
		return http.StatusPaymentRequired
	case openAIGatewayFailureCredential:
		if isOpenAIUpstreamAccessStateError(message, payload) {
			return http.StatusForbidden
		}
		return http.StatusUnauthorized
	case openAIGatewayFailureModelUnavailable:
		return http.StatusNotFound
	}
	code := openAIStreamFailedEventErrorCode(payload)
	errType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "response.error.type").String()))
	if errType == "" {
		errType = strings.ToLower(strings.TrimSpace(gjson.GetBytes(payload, "error.type").String()))
	}
	combined := strings.TrimSpace(errType + " " + code + " " + strings.ToLower(strings.TrimSpace(message)))
	for _, path := range openAIStreamErrorStatusPaths {
		if status := int(gjson.GetBytes(payload, path).Int()); status == http.StatusUnauthorized ||
			status == http.StatusPaymentRequired || status == http.StatusForbidden || status == http.StatusTooManyRequests || (status >= 500 && status <= 599) {
			return status
		}
	}
	switch {
	case strings.Contains(combined, "rate_limit"):
		return http.StatusTooManyRequests
	case strings.Contains(errType, "invalid_request"):
		return http.StatusBadRequest
	case strings.Contains(combined, "authentication") || strings.Contains(combined, "unauthorized") || strings.Contains(combined, "invalid_api_key"):
		return http.StatusUnauthorized
	case strings.Contains(combined, "permission") || strings.Contains(combined, "forbidden") || strings.Contains(combined, "access denied"):
		return http.StatusForbidden
	case isOpenAIUpstreamAccessStateError(message, payload):
		return http.StatusForbidden
	case isOpenAIUpstreamCapacityShedEvent(payload):
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

func openAIStreamFailureStatusForClass(payload []byte, message string, class openAIGatewayFailureClass) int {
	if len(bytes.TrimSpace(payload)) == 0 || !gjson.ValidBytes(payload) {
		return http.StatusBadGateway
	}
	semanticStatus := openAIStreamSemanticStatusForClass(payload, message, class)
	switch semanticStatus {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests, http.StatusNotFound, 529:
		return semanticStatus
	case http.StatusServiceUnavailable:
		if isOpenAIUpstreamCapacityShedEvent(payload) {
			return semanticStatus
		}
	}
	if semanticStatus >= 500 && semanticStatus <= 599 {
		return semanticStatus
	}
	return http.StatusBadGateway
}

func shouldFailoverOpenAIHTTPDecision(input openAIGatewayFailureDecisionInput, decision openAIGatewayFailureDecision) bool {
	switch decision.failureClass {
	case openAIGatewayFailureRequest:
		return false
	case openAIGatewayFailureCredential, openAIGatewayFailureQuota:
		return true
	case openAIGatewayFailureModelUnavailable:
		if input.allowModelFailover || input.kind == openAIGatewayFailurePassthrough {
			return input.allowModelFailover
		}
	}
	if isOpenAIRequestBodyTooLargeError(input.statusCode, input.message, input.body) {
		return true
	}
	if isOpenAIHTTPUpstreamAccessStateError(input.statusCode, input.message, input.body) {
		return true
	}
	if input.poolRetryable {
		return true
	}
	return shouldFailoverUpstreamError(input.statusCode) ||
		isOpenAITransientProcessingError(input.statusCode, input.message, input.body)
}

func shouldFailoverOpenAIResponseFailedDecision(input openAIGatewayFailureDecisionInput, decision openAIGatewayFailureDecision) bool {
	switch decision.failureClass {
	case openAIGatewayFailureRequest:
		return false
	case openAIGatewayFailureCredential, openAIGatewayFailureQuota, openAIGatewayFailureModelUnavailable:
		return true
	}
	if isOpenAIUpstreamAccessStateError(input.message, input.body) {
		return true
	}
	if decision.semanticStatus == http.StatusForbidden {
		return openAIStream403AccountFailure(input.body, input.message)
	}
	// response.failed is transported over HTTP 200.  Its embedded semantic
	// 429 must enter the same retry path as a regular HTTP 429.
	if decision.semanticStatus == http.StatusTooManyRequests {
		return true
	}
	if isOpenAITransientProcessingError(http.StatusBadRequest, input.message, input.body) {
		return true
	}
	combined := openAIStreamFailureDecisionText(input.body, input.message, true)
	if combined == "" {
		return true
	}
	for _, marker := range []string{
		"invalid_request", "content_policy", "policy", "safety",
		"high-risk cyber", "not allowed", "violat",
	} {
		if strings.Contains(combined, marker) {
			return false
		}
	}
	return true
}

func shouldFailoverOpenAIErrorEventDecision(input openAIGatewayFailureDecisionInput, decision openAIGatewayFailureDecision) bool {
	switch decision.failureClass {
	case openAIGatewayFailureRequest:
		return false
	case openAIGatewayFailureCredential, openAIGatewayFailureQuota, openAIGatewayFailureModelUnavailable:
		return true
	}
	if isOpenAIUpstreamAccessStateError(input.message, input.body) {
		return true
	}
	switch decision.semanticStatus {
	case http.StatusForbidden:
		return openAIStream403AccountFailure(input.body, input.message)
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusTooManyRequests, 529:
		return true
	}
	for _, path := range openAIStreamErrorStatusPaths {
		if status := int(gjson.GetBytes(input.body, path).Int()); status >= 500 && status <= 599 {
			return true
		}
	}
	for _, signal := range openAIGatewayFailureSignals(input.body) {
		switch signal {
		case "server_error", "internal_error", "internal_server_error", "upstream_error":
			return true
		}
	}
	if isOpenAITransientProcessingError(http.StatusBadRequest, input.message, input.body) {
		return true
	}
	combined := openAIStreamFailureDecisionText(input.body, input.message, false)
	return strings.Contains(combined, "temporary") ||
		strings.Contains(combined, "try again") ||
		strings.Contains(combined, "please retry")
}

// openAIStreamFailureDecisionText returns the exact normalized text used by
// the two existing stream adapters.  Keeping this extraction in the pure
// decision layer prevents each transport from growing another parser while
// preserving their intentionally different code/type versus message inputs.
func openAIStreamFailureDecisionText(body []byte, message string, responseFailed bool) string {
	if responseFailed {
		code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "response.error.code").String()))
		if code == "" {
			code = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.code").String()))
		}
		errType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "response.error.type").String()))
		if errType == "" {
			errType = strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.type").String()))
		}
		return strings.ToLower(strings.TrimSpace(message + " " + code + " " + errType))
	}
	return strings.ToLower(strings.TrimSpace(message + " " +
		gjson.GetBytes(body, "error.message").String() + " " +
		gjson.GetBytes(body, "response.error.message").String()))
}
