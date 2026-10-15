package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// applyGroupHTTPFailure is called only after an upstream failure. The policy
// replaces ordinary transient cooldowns, never a provider's explicit quota,
// model scope, or an administrator's account-specific error rule.
func (s *RateLimitService) applyGroupHTTPFailure(ctx context.Context, account *Account, status int, headers http.Header, body []byte, model string) (bool, time.Time) {
	if s == nil || account == nil || account.IsCustomErrorCodesEnabled() {
		return false, time.Time{}
	}
	if tempUnschedRulesApply(account, status, body, model) && len(matchTempUnschedulableRules(account, status, body)) > 0 {
		return false, time.Time{}
	}
	kind, ok := groupHTTPFailureKind(account, status, headers, body, model)
	if !ok {
		return false, time.Time{}
	}
	return s.ApplyGroupFailurePolicy(ctx, account, kind)
}

func groupHTTPFailureKind(account *Account, status int, headers http.Header, body []byte, model string) (GroupFailureKind, bool) {
	if account == nil || classifyOpenAIGatewayFailure(status, body, "") != openAIGatewayFailureUnknown {
		return "", false
	}
	now := time.Now()
	if reset := parseRetryAfterResetTime(headers, now); reset != nil && reset.After(now) {
		return "", false
	}
	// Bare authorization/permission failures are not transient failure evidence.
	if status == http.StatusUnauthorized || status == http.StatusPaymentRequired || status == http.StatusForbidden {
		return "", false
	}
	if status == http.StatusTooManyRequests && groupRateLimitHasProviderScope(account, headers, body, model, now) {
		return "", false
	}
	if status == 529 || isOpenAIRequestScopedCapacityShed("", body) {
		return GroupFailureOverload, true
	}
	switch status {
	case http.StatusGatewayTimeout, 522, 524:
		return GroupFailureTimeout, true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, 520, 521, 523:
		return GroupFailureServerError, true
	case http.StatusTooManyRequests:
		return GroupFailureRateLimit, true
	default:
		return "", false
	}
}

// Keep the existing provider parsers authoritative. These checks only decide
// whether the generic rule may replace a fallback; they do not persist state.
func groupRateLimitHasProviderScope(account *Account, headers http.Header, body []byte, model string, now time.Time) bool {
	if account.IsShadow() || IsOllamaCloudUsageAccount(account) || account.Platform == PlatformAntigravity ||
		isOpenAIImageRateLimitError(http.StatusTooManyRequests, body) ||
		(isOpenAIOAuthAccount(account) && isCodexSparkModel(model)) {
		return true
	}
	if disposition, _ := classifyOpenAIOAuth429(headers, body); disposition != openAIOAuth429Transient {
		return true
	}
	if openAI429CooldownResetTime(headers, body, now) != nil || calculateAnthropic429ResetTime(headers) != nil {
		return true
	}
	if reset, err := strconv.ParseInt(headers.Get("anthropic-ratelimit-unified-reset"), 10, 64); err == nil && time.Unix(reset, 0).After(now) {
		return true
	}
	if account.Platform == PlatformAnthropic {
		if selectAnthropicFableWindowLimit(headers, now) != nil {
			return true
		}
		fableModel := firstNonEmpty(gjson.GetBytes(body, "error.details.model").String(), model)
		if isAnthropicFableModel(fableModel) && strings.EqualFold(gjson.GetBytes(body, "error.details.error_code").String(), "credits_required") {
			return true
		}
	}
	if account.Platform == PlatformGemini && ParseGeminiRateLimitResetTime(body) != nil {
		return true
	}
	if account.IsCommandCode() && classifyCommandCodeUsageError(body) != commandCodeUsageErrorNone {
		return true
	}
	if account.IsCline() {
		if kind, _ := parseClineError(body); kind != clineErrorNone {
			return true
		}
	}
	return ((account.IsCNProvider() || account.IsOpenCodeGo()) && cnProviderQuotaSnapshotReset(account, now) != nil) ||
		(account.IsCNProvider() && cnProviderResponseIndicatesInsufficientBalance(body))
}

func (s *RateLimitService) applyGroupTransportTimeout(ctx context.Context, account *Account, err error) {
	if s == nil || account == nil || err == nil || errors.Is(err, context.Canceled) || (ctx != nil && ctx.Err() != nil) {
		return
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		s.ApplyGroupFailurePolicy(ctx, account, GroupFailureTimeout)
	}
}
