package service

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// These headers are part of the private reseller hop between a station and
// the main gateway.  They are deliberately set at the last possible point in
// each outbound request builder: a station API key is not a normal upstream
// credential and the main gateway uses the request id for reservation and
// settlement idempotency.
const (
	resellerRequestIDHeader     = "X-Reseller-Request-ID"
	resellerRequestSourceHeader = "X-Reseller-Request-Source"

	resellerRequestSourceUser        = "user"
	resellerRequestSourceMonitor     = "monitor"
	resellerRequestSourceAccountTest = "account_test"
)

// isResellerAPIKey reports whether key is a station-issued credential.  Keep
// this check intentionally narrow: adding reseller headers to an ordinary
// provider key can break strict third-party OpenAI-compatible gateways.
func isResellerAPIKey(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), "sk-rs_")
}

// resellerKeyFromHeaders extracts the credential already selected by a
// provider adapter.  It is a fallback for request-construction tests and for
// any future adapter that calls newMonitorRequest directly; normal monitor
// paths pass the original apiKey explicitly so custom headers cannot alter the
// decision.
func resellerKeyFromHeaders(headers http.Header) string {
	if headers == nil {
		return ""
	}
	for name, values := range headers {
		if !strings.EqualFold(name, "Authorization") && !strings.EqualFold(name, "X-Api-Key") {
			continue
		}
		if len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if strings.HasPrefix(strings.ToLower(value), "bearer ") {
			value = strings.TrimSpace(value[len("Bearer "):])
		}
		if isResellerAPIKey(value) {
			return value
		}
	}
	return ""
}

// isResellerManagedAccount accepts both the explicit marker written by the
// reseller catalogue sync and the key prefix used by older station records.
// The prefix fallback is important during a rolling upgrade where an account
// can be restored from a pre-marker snapshot.
func isResellerManagedAccount(account *Account) bool {
	if account == nil {
		return false
	}
	if account.Extra != nil {
		for _, key := range []string{"moshu_reseller_managed", "moshu_reseller_passthrough"} {
			switch value := account.Extra[key].(type) {
			case bool:
				if value {
					return true
				}
			case string:
				if strings.EqualFold(strings.TrimSpace(value), "true") {
					return true
				}
			}
		}
	}
	return isResellerAPIKey(account.GetCredential("api_key"))
}

func normalizeResellerRequestSource(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case resellerRequestSourceMonitor:
		return resellerRequestSourceMonitor
	case resellerRequestSourceAccountTest:
		return resellerRequestSourceAccountTest
	default:
		return resellerRequestSourceUser
	}
}

// setMandatoryResellerHeader replaces every casing variant before setting the
// canonical wire key.  Header.Set alone leaves a manually inserted lower-case
// map key in place; net/http would then serialize both values and the custom
// value could win at the main gateway.
func setMandatoryResellerHeader(headers http.Header, name, value string) {
	if headers == nil {
		return
	}
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			delete(headers, existing)
		}
	}
	headers.Set(name, value)
}

// applyResellerCredentialHeaders adds a fresh request id for one outbound
// reseller attempt.  Calling it more than once is safe and intentionally
// generates a new UUID each time (for example, on a retry or per monitor
// model).  apiKey is passed separately so user-supplied extra headers cannot
// trick the decision into forwarding private reseller headers.
func applyResellerCredentialHeaders(headers http.Header, apiKey, source string) {
	if headers == nil {
		return
	}
	if !isResellerAPIKey(apiKey) {
		// Private reseller headers are never valid on a normal provider hop.
		// Remove caller/template supplied casing variants instead of leaving
		// them to be serialized to a strict third-party gateway.
		for existing := range headers {
			if strings.EqualFold(existing, resellerRequestIDHeader) || strings.EqualFold(existing, resellerRequestSourceHeader) {
				delete(headers, existing)
			}
		}
		return
	}
	setMandatoryResellerHeader(headers, resellerRequestIDHeader, uuid.NewString())
	setMandatoryResellerHeader(headers, resellerRequestSourceHeader, normalizeResellerRequestSource(source))
}

// applyResellerAccountHeaders is the normal gateway counterpart.  It is used
// after authentication and custom-header overrides have been applied, so the
// generated UUID can never be replaced by account configuration.
func applyResellerAccountHeaders(headers http.Header, account *Account, source string) {
	if headers == nil || !isResellerManagedAccount(account) {
		return
	}
	apiKey := account.GetCredential("api_key")
	if isResellerAPIKey(apiKey) {
		applyResellerCredentialHeaders(headers, apiKey, source)
		return
	}
	// A legacy account may have an explicit marker but a redacted/rotated key
	// that is not available in memory. Marked accounts must still receive the
	// protocol headers. Replace any stale/custom values as well: the marker is
	// trusted account metadata, while request headers are caller-controlled.
	setMandatoryResellerHeader(headers, resellerRequestIDHeader, uuid.NewString())
	setMandatoryResellerHeader(headers, resellerRequestSourceHeader, normalizeResellerRequestSource(source))
}
