package service

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// resellerKeyFromMonitorHeaders is only a fallback for direct request-builder
// callers. Normal monitor paths pass the selected api key explicitly so a
// custom header cannot change whether the private protocol is enabled.
func resellerKeyFromMonitorHeaders(headers http.Header) string {
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
		if len(value) >= len("Bearer ") && strings.EqualFold(value[:len("Bearer ")], "Bearer ") {
			value = strings.TrimSpace(value[len("Bearer "):])
		}
		if isMonitorResellerAPIKey(value) {
			return value
		}
	}
	return ""
}

func isMonitorResellerAPIKey(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), "sk-rs_")
}

func deleteMonitorResellerHeaders(headers http.Header) {
	for name := range headers {
		if strings.EqualFold(name, monitorResellerRequestIDHeader) || strings.EqualFold(name, monitorRequestSourceHeader) {
			delete(headers, name)
		}
	}
}

func setMonitorResellerHeader(headers http.Header, name, value string) {
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			delete(headers, existing)
		}
	}
	headers.Set(name, value)
}

// applyMonitorResellerHeaders adds the private protocol only for station
// credentials. Every probe gets a fresh UUID and caller-provided values cannot
// override it. Ordinary provider requests have any accidental private headers
// removed before they leave the station.
func applyMonitorResellerHeaders(headers http.Header, apiKey string) {
	if headers == nil {
		return
	}
	if !isMonitorResellerAPIKey(apiKey) {
		deleteMonitorResellerHeaders(headers)
		return
	}
	setMonitorResellerHeader(headers, monitorResellerRequestIDHeader, uuid.NewString())
	setMonitorResellerHeader(headers, monitorRequestSourceHeader, monitorRequestSource)
}
