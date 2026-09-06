package repository

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
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

func TestApplyMoshuResellerRequestIDDisabled(t *testing.T) {
	t.Setenv("MOSHU_RESELLER_CLIENT_ENABLED", "false")
	t.Setenv("MOSHU_RESELLER_URL", "https://moshu.example")
	req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), ctxkey.ClientRequestID, "request-uuid"), http.MethodPost, "https://moshu.example/v1/messages", nil)
	applyMoshuResellerRequestID(req)
	if got := req.Header.Get(moshuResellerRequestIDHeader); got != "" {
		t.Fatalf("disabled client added header %q", got)
	}
}
