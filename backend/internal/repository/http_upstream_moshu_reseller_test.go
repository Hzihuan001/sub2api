package repository

import (
	"context"
	"net/http"
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

	external, _ := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://other.example/v1/messages", nil)
	applyMoshuResellerRequestID(external)
	if got := external.Header.Get(moshuResellerRequestIDHeader); got != "" {
		t.Fatalf("external upstream received private reseller header %q", got)
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
