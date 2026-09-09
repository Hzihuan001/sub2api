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

	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxkey.ClientRequestID, "request-uuid"), http.MethodPost, "https://moshu.example/v1/messages", nil)
	applyMoshuResellerRequestID(req)
	if got := req.Header.Get(moshuResellerRequestIDHeader); got != "request-uuid" {
		t.Fatalf("header = %q, want request-uuid", got)
	}

	external, _ := http.NewRequestWithContext(req.Context(), http.MethodPost, "https://other.example/v1/messages", nil)
	applyMoshuResellerRequestID(external)
	if got := external.Header.Get(moshuResellerRequestIDHeader); got != "" {
		t.Fatalf("external upstream received private reseller header %q", got)
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
	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxkey.ClientRequestID, "request-uuid"), http.MethodPost, "https://moshu.example/v1/messages", nil)
	applyMoshuResellerRequestID(req)
	if got := req.Header.Get(moshuResellerRequestIDHeader); got != "" {
		t.Fatalf("disabled client added header %q", got)
	}
}
