package repository

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/google/uuid"
)

func TestApplyMoshuResellerRequestIDScopesHeaderToConfiguredOrigin(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	requestID := uuid.NewString()

	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxkey.ClientRequestID, requestID), http.MethodPost, "https://moshu.example/v1/messages", nil)
	applyMoshuResellerRequestID(req)
	if got := req.Header.Get(moshuResellerRequestIDHeader); got != requestID {
		t.Fatalf("header = %q, want %q", got, requestID)
	}
	if got := req.Header.Get(moshuResellerRequestSourceHeader); got != "user" {
		t.Fatalf("source = %q, want user", got)
	}

	external, _ := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://other.example/v1/messages", nil)
	applyMoshuResellerRequestID(external)
	if got := external.Header.Get(moshuResellerRequestIDHeader); got != "" {
		t.Fatalf("external upstream received private reseller header %q", got)
	}
}

func TestApplyMoshuResellerRequestIDUsesExplicitAccountTestSource(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	t.Setenv("MOSHU_RESELLER_URL", "https://main.example")
	ctx := context.WithValue(context.Background(), ctxkey.ResellerRequestSource, "account_test")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://main.example/v1/chat/completions", nil)
	applyMoshuResellerRequestID(req)
	if got := req.Header.Get(moshuResellerRequestSourceHeader); got != "account_test" {
		t.Fatalf("source = %q, want account_test", got)
	}
}

func TestApplyMoshuResellerRequestIDMonitorUsesValidFallbackUUID(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	requestID := uuid.NewString()
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "channel-monitor:deepseek:16136")
	ctx = context.WithValue(ctx, ctxkey.RequestID, requestID)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://moshu.example/v1/models", nil)

	applyMoshuResellerRequestID(req)

	if got := req.Header.Get(moshuResellerRequestIDHeader); got != requestID {
		t.Fatalf("header = %q, want fallback UUID %q", got, requestID)
	}
	if got := req.Header.Get(moshuResellerRequestSourceHeader); got != "monitor" {
		t.Fatalf("source = %q, want monitor", got)
	}
}

func TestApplyMoshuResellerRequestIDMonitorGeneratesUUIDWhenContextIDsInvalid(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "channel-monitor:deepseek:16136")
	ctx = context.WithValue(ctx, ctxkey.RequestID, "monitor-request")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://moshu.example/v1/models", nil)

	applyMoshuResellerRequestID(req)

	if _, err := uuid.Parse(req.Header.Get(moshuResellerRequestIDHeader)); err != nil {
		t.Fatalf("header is not a UUID: %v", err)
	}
	if got := req.Header.Get(moshuResellerRequestSourceHeader); got != "monitor" {
		t.Fatalf("source = %q, want monitor", got)
	}
}

func TestApplyMoshuResellerRequestIDProbeGetsUniqueUUID(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	var ids []string
	for range 2 {
		req, _ := http.NewRequest(http.MethodPost, "https://moshu.example/v1/responses", nil)
		applyMoshuResellerRequestID(req)
		id := req.Header.Get(moshuResellerRequestIDHeader)
		if _, err := uuid.Parse(id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if ids[0] == ids[1] {
		t.Fatal("independent probes reused a reservation")
	}
	req, _ := http.NewRequest(http.MethodPost, "http://moshu.example/v1/responses", nil)
	applyMoshuResellerRequestID(req)
	if req.Header.Get(moshuResellerRequestIDHeader) != "" {
		t.Fatal("scheme mismatch leaked reseller header")
	}
}

func TestApplyMoshuResellerRequestIDDisabled(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "false")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxkey.ClientRequestID, uuid.NewString()), http.MethodPost, "https://moshu.example/v1/messages", nil)
	applyMoshuResellerRequestID(req)
	if got := req.Header.Get(moshuResellerRequestIDHeader); got != "" {
		t.Fatalf("disabled client added header %q", got)
	}
}

func TestApplyMoshuResellerRequestIDRemovesNonCanonicalCallerHeaders(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "true")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, uuid.NewString())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://moshu.example/v1/chat/completions", nil)
	// Deliberately bypass Header.Set: this is how a custom account header can
	// arrive with a non-canonical map key and evade a canonical-only replace.
	req.Header["x-reseller-request-id"] = []string{"not-a-uuid"}
	req.Header["x-reseller-request-source"] = []string{"user"}
	applyMoshuResellerRequestID(req)
	nonCanonicalID := strings.ToLower(moshuResellerRequestIDHeader)
	nonCanonicalSource := strings.ToLower(moshuResellerRequestSourceHeader)
	if values := req.Header[nonCanonicalID]; len(values) != 0 {
		t.Fatalf("non-canonical request ID survived: %#v", values)
	}
	if values := req.Header[nonCanonicalSource]; len(values) != 0 {
		t.Fatalf("non-canonical request source survived: %#v", values)
	}
	if _, err := uuid.Parse(req.Header.Get(moshuResellerRequestIDHeader)); err != nil {
		t.Fatalf("request ID is not a UUID: %v", err)
	}
	if got := req.Header.Get(moshuResellerRequestSourceHeader); got != "user" {
		t.Fatalf("source = %q, want user", got)
	}
}
