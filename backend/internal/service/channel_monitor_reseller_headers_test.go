package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var resellerMonitorChallengeRE = regexp.MustCompile(`Q: (\d+) ([+-]) (\d+) = \?\nA:$`)

// Monitoring is also used to exercise reseller-backed accounts.  The main
// gateway reserves a request before forwarding it, so every probe must carry a
// fresh protocol request id.  Keep this assertion at the request-construction
// boundary: it catches regressions even when a new provider adapter is added.
func TestNewMonitorRequestUsesFreshResellerRequestID(t *testing.T) {
	ctx := context.Background()
	custom := map[string]string{
		"Authorization":             "Bearer sk-rs_station-key",
		"X-Reseller-Request-ID":     "not-a-uuid",
		"X-Reseller-Request-Source": "user",
	}

	first, err := newMonitorRequest(ctx, "http://127.0.0.1/v1/chat/completions", []byte(`{"stream":true}`), custom, "sk-rs_station-key")
	if err != nil {
		t.Fatal(err)
	}
	second, err := newMonitorRequest(ctx, "http://127.0.0.1/v1/chat/completions", []byte(`{"stream":true}`), custom, "sk-rs_station-key")
	if err != nil {
		t.Fatal(err)
	}

	firstID, err := uuid.Parse(first.Header.Get("X-Reseller-Request-ID"))
	if err != nil {
		t.Fatalf("monitor request id must be a UUID, got %q: %v", first.Header.Get("X-Reseller-Request-ID"), err)
	}
	secondID, err := uuid.Parse(second.Header.Get("X-Reseller-Request-ID"))
	if err != nil {
		t.Fatalf("second monitor request id must be a UUID, got %q: %v", second.Header.Get("X-Reseller-Request-ID"), err)
	}
	if firstID == secondID {
		t.Fatalf("each monitor probe must use a distinct request id: %s", firstID)
	}
	if got := first.Header.Get("X-Reseller-Request-Source"); got != "monitor" {
		t.Fatalf("monitor source = %q, want monitor", got)
	}
	if got := second.Header.Get("X-Reseller-Request-Source"); got != "monitor" {
		t.Fatalf("second monitor source = %q, want monitor", got)
	}
	if got := first.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("streaming monitor should request SSE, got %q", got)
	}
}

func TestNewMonitorRequestDoesNotAllowCustomResellerHeadersToOverride(t *testing.T) {
	req, err := newMonitorRequest(context.Background(), "http://127.0.0.1/v1/models", []byte(`{}`), map[string]string{
		"Authorization":             "Bearer sk-rs_station-key",
		"x-reseller-request-id":     "bad-id",
		"x-reseller-request-source": "account_test",
	}, "sk-rs_station-key")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Reseller-Request-Source"); got != "monitor" {
		t.Fatalf("custom source overrode monitor source: %q", got)
	}
	if _, err := uuid.Parse(req.Header.Get("X-Reseller-Request-ID")); err != nil {
		t.Fatalf("custom request id overrode generated UUID %q: %v", req.Header.Get("X-Reseller-Request-ID"), err)
	}
	if req.Method != http.MethodPost {
		t.Fatalf("monitor method = %s, want POST", req.Method)
	}
}

func TestNewMonitorRequestDoesNotLeakResellerHeadersToOrdinaryProvider(t *testing.T) {
	req, err := newMonitorRequest(context.Background(), "http://127.0.0.1/v1/models", []byte(`{}`), map[string]string{
		"Authorization":             "Bearer sk-provider-key",
		"x-reseller-request-id":     "not-a-uuid",
		"x-reseller-request-source": "monitor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Reseller-Request-ID"); got != "" {
		t.Fatalf("ordinary provider received reseller request id %q", got)
	}
	if got := req.Header.Get("X-Reseller-Request-Source"); got != "" {
		t.Fatalf("ordinary provider received reseller request source %q", got)
	}
}

func TestRunCheckForModelResellerMonitorProtocolIntegration(t *testing.T) {
	var received http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Clone()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		messages, _ := body["messages"].([]any)
		prompt := ""
		if len(messages) > 0 {
			if message, ok := messages[0].(map[string]any); ok {
				prompt, _ = message["content"].(string)
			}
		}
		match := resellerMonitorChallengeRE.FindStringSubmatch(prompt)
		if len(match) != 4 {
			http.Error(w, "challenge missing", http.StatusBadRequest)
			return
		}
		left, _ := strconv.Atoi(match[1])
		right, _ := strconv.Atoi(match[3])
		answer := left + right
		if match[2] == "-" {
			answer = left - right
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", `{"choices":[{"delta":{"content":"`+strconv.Itoa(answer)+`"}}]}`)
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	previousClient := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(func() { monitorHTTPClient = previousClient })

	result := runCheckForModel(context.Background(), MonitorProviderDeepseek, server.URL, "sk-rs_integration-key", "deepseek-v4.1-flash", nil)
	if result.Status != MonitorStatusOperational {
		t.Fatalf("reseller monitor should pass through UUID protocol, got status=%s message=%q", result.Status, result.Message)
	}
	if _, err := uuid.Parse(received.Get(resellerRequestIDHeader)); err != nil {
		t.Fatalf("monitor request id = %q, want UUID: %v", received.Get(resellerRequestIDHeader), err)
	}
	if got := received.Get(resellerRequestSourceHeader); got != resellerRequestSourceMonitor {
		t.Fatalf("monitor source = %q, want %q", got, resellerRequestSourceMonitor)
	}
	if strings.Contains(strings.ToLower(received.Get(resellerRequestIDHeader)), "integration") {
		t.Fatal("request id must not be caller-controlled")
	}
}
