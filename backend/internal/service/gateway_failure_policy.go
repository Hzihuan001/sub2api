package service

import (
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

// openAIGatewayFailureClass describes the provider's error, independently of
// account selection, cooldowns, and whether downstream output has already begun.
// Unknown leaves those decisions to the caller's existing status/retry policy.
type openAIGatewayFailureClass uint8

const (
	openAIGatewayFailureUnknown openAIGatewayFailureClass = iota
	openAIGatewayFailureRequest
	openAIGatewayFailureCredential
	openAIGatewayFailureQuota
	openAIGatewayFailureModelUnavailable
)

// classifyOpenAIGatewayFailure uses only explicit error fields. In particular,
// quoted request data must never turn a schema error into a credential failure.
// A specific content/context/parameter rejection wins over retryable HTTP status
// codes; a generic invalid_request_error wrapper does not hide a concrete
// credential, quota, missing-model, or transient provider error.
func classifyOpenAIGatewayFailure(statusCode int, body []byte, message string) openAIGatewayFailureClass {
	if hit, _, _ := detectOpenAICyberPolicy(body); hit {
		return openAIGatewayFailureRequest
	}
	if isOpenAIContextWindowError(message, body) {
		return openAIGatewayFailureRequest
	}

	signals := openAIGatewayFailureSignals(body)
	for _, signal := range signals {
		if isOpenAISpecificRequestFailureSignal(signal) {
			return openAIGatewayFailureRequest
		}
	}
	for _, text := range openAIGatewayFailureMessages(body, message) {
		if isOpenAIRequestRejectionMessage(text) {
			return openAIGatewayFailureRequest
		}
	}

	for _, signal := range signals {
		if isOpenAIUpstreamAccessStateCode(signal) || isOpenAICredentialFailureSignal(signal) {
			return openAIGatewayFailureCredential
		}
	}
	for _, signal := range signals {
		if isOpenAIHardQuotaFailureSignal(signal) {
			return openAIGatewayFailureQuota
		}
	}
	for _, signal := range signals {
		if signal == "model_not_found" {
			return openAIGatewayFailureModelUnavailable
		}
	}
	// Preserve the existing compatible-provider missing-model messages without
	// adding model aliases, permissions, or generic endpoint-404 fallbacks.
	if isOpenAICompatibleModelNotFoundBody(body) {
		return openAIGatewayFailureModelUnavailable
	}

	for _, signal := range signals {
		switch signal {
		case "permission_error", "permission_denied", "forbidden", "access_denied":
			// An explicit network access failure is not a content rejection.
			// Preserve the existing HTTP account/proxy policy, without treating
			// arbitrary denial text or a streaming status as credential evidence.
			if statusCode == http.StatusForbidden && strings.EqualFold(gjson.GetBytes(body, "error.details.reason").String(), "ip_blocked") {
				return openAIGatewayFailureUnknown
			}
			return openAIGatewayFailureRequest
		}
	}
	// Some providers report transient failures and account-specific serialized
	// body limits inside invalid_request_error. Preserve those existing paths;
	// the specific deterministic request checks above still take precedence.
	if statusCode == http.StatusTooManyRequests ||
		isOpenAIRequestBodyTooLargeError(statusCode, message, body) ||
		isOpenAITransientProcessingError(http.StatusBadRequest, message, body) ||
		openAIGatewayHasTransientFailureSignal(body, signals) {
		return openAIGatewayFailureUnknown
	}
	for _, signal := range signals {
		switch signal {
		case "invalid_request", "invalid_request_error", "bad_request", "bad_request_error", "validation_error":
			// Some compatible providers wrap credential/payment/permission
			// failures in invalid_request_error. A generic wrapper alone must
			// not erase those established status policies. Concrete safety,
			// content, context and parameter errors already returned above.
			if openAIGatewayHasAuthStatus(statusCode, body) ||
				(statusCode == http.StatusBadRequest && openAIGatewayHasLegacyAccountStateMessage(body)) {
				return openAIGatewayFailureUnknown
			}
			return openAIGatewayFailureRequest
		}
	}
	return openAIGatewayFailureUnknown
}

func openAIGatewayHasAuthStatus(statusCode int, body []byte) bool {
	isAuthStatus := func(status int) bool {
		return status == http.StatusUnauthorized || status == http.StatusPaymentRequired || status == http.StatusForbidden
	}
	if isAuthStatus(statusCode) {
		return true
	}
	for _, path := range []string{"error.status_code", "error.status", "response.error.status_code", "response.error.status", "status_code", "status"} {
		if isAuthStatus(int(gjson.GetBytes(body, path).Int())) {
			return true
		}
	}
	return false
}

// Keep the existing HTTP-400 account-state policy reachable for its two legacy
// provider messages. This does not promote free-form text to Credential, does
// not scan echoed request fields, and does not apply to errors inside HTTP 200.
func openAIGatewayHasLegacyAccountStateMessage(body []byte) bool {
	if !gjson.ValidBytes(body) {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "error.message").String()))
	for _, prefix := range []string{
		"organization has been disabled", "your organization has been disabled", "this organization has been disabled",
		"identity verification is required",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

// isOpenAIRequestScopedFailure is the shared no-retry/no-account-penalty guard
// for HTTP and stream error payloads. It does not classify a bare HTTP status.
func isOpenAIRequestScopedFailure(body []byte, message string) bool {
	return classifyOpenAIGatewayFailure(0, body, message) == openAIGatewayFailureRequest
}

func openAIGatewayFailureSignals(body []byte) []string {
	if !gjson.ValidBytes(body) {
		return nil
	}
	signals := make([]string, 0, 8)
	for _, path := range []string{
		"error.code", "response.error.code", "detail.code", "code",
		"error.type", "response.error.type", "detail.type", "type",
	} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String {
			signals = append(signals, strings.ToLower(strings.TrimSpace(value.String())))
		}
	}
	return signals
}

func openAIGatewayFailureMessages(body []byte, message string) []string {
	messages := []string{message}
	if !gjson.ValidBytes(body) {
		return append(messages, string(body))
	}
	for _, path := range []string{"error.message", "response.error.message", "detail.message", "message"} {
		value := gjson.GetBytes(body, path)
		if value.Type == gjson.String {
			messages = append(messages, value.String())
		}
	}
	return messages
}

func isOpenAISpecificRequestFailureSignal(signal string) bool {
	switch signal {
	case "cyber_policy", "content_policy", "content_policy_violation", "content_filter", "content_filter_error",
		"safety", "safety_error", "safety_violation", "request_blocked", "content_moderation_blocked",
		"responsible_ai_policy_violation", "responsibleaipolicyviolation",
		"context_length_exceeded", "context_too_large",
		"invalid_function_parameters", "invalid_function_schema", "invalid_schema", "invalid_json",
		"invalid_parameter", "invalid_parameters", "invalid_argument", "invalid_arguments", "invalid_value", "invalid_type",
		"missing_required_parameter", "missing_parameter", "unknown_parameter", "unsupported_parameter",
		"unrecognized_request_argument", "unsupported_value", "invalid_tool", "invalid_tool_choice",
		"invalid_response_format", "invalid_image", "invalid_image_url", "unsupported_content_type",
		"invalid_payload", "malformed_request":
		return true
	default:
		return false
	}
}

func isOpenAIRequestRejectionMessage(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	for _, marker := range []string{
		"content policy", "content_policy", "content filter", "content_filter",
		"safety policy", "safety violation", "safety system", "high-risk cyber",
		"blocked by policy", "blocked by our policy", "violates our policy",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	for _, prefix := range []string{
		"invalid schema", "invalid parameter", "invalid value for", "unknown parameter",
		"unsupported parameter", "missing required parameter", "unrecognized request argument",
		"instructions are required",
	} {
		if strings.HasPrefix(message, prefix) {
			return true
		}
	}
	return false
}

func isOpenAICredentialFailureSignal(signal string) bool {
	switch signal {
	case "invalid_api_key", "api_key_disabled", "api_key_expired", "unauthorized", "unauthorized_error",
		"authentication_error", "authentication_failed", "invalid_token", "access_token_invalid",
		"token_expired", "access_token_expired", "token_revoked", "token_invalidated",
		"invalid_credentials", "credential_invalid":
		return true
	default:
		return false
	}
}

// Usage-window exhaustion (usage_limit_reached) is intentionally absent: its
// reset time and transient retry policy remain owned by the rate-limit service.
func isOpenAIHardQuotaFailureSignal(signal string) bool {
	switch signal {
	case "insufficient_quota", "billing_hard_limit_reached", "billing_hard_limit_exceeded",
		"billing_not_active", "insufficient_balance", "insufficient_funds", "insufficient_credits",
		"credits_exhausted", "credit_balance_exhausted", "payment_required":
		return true
	default:
		return false
	}
}

func openAIGatewayHasTransientFailureSignal(body []byte, signals []string) bool {
	for _, signal := range signals {
		switch signal {
		case "rate_limit_exceeded", "rate_limit_error", "rate_limit", "usage_limit_reached", "too_many_requests",
			"server_is_overloaded", "slow_down", "server_error", "internal_error", "internal_server_error":
			return true
		}
	}
	for _, path := range []string{"error.status_code", "error.status", "response.error.status_code", "response.error.status"} {
		if gjson.GetBytes(body, path).Int() == http.StatusTooManyRequests {
			return true
		}
	}
	return false
}
