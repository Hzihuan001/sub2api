package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunPelicanTestWithTransientRetryRetriesEmptyTransportResetOnce(t *testing.T) {
	started := time.Now()
	calls := 0
	result, err := runPelicanTestWithTransientRetry(context.Background(), func(context.Context) (*ScheduledTestResult, error) {
		calls++
		if calls == 1 {
			return &ScheduledTestResult{
				Status: "failed", ErrorMessage: "Stream read error: read tcp: connection reset by peer",
				StartedAt: started, FinishedAt: started.Add(time.Second), LatencyMs: 1000,
			}, nil
		}
		return &ScheduledTestResult{
			Status: "success", ResponseText: "<svg></svg>",
			StartedAt: started.Add(2 * time.Second), FinishedAt: started.Add(3 * time.Second), LatencyMs: 1000,
		}, nil
	})

	if err != nil {
		t.Fatalf("retry returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("attempts = %d, want 2", calls)
	}
	if result == nil || result.Status != "success" || result.ResponseText != "<svg></svg>" {
		t.Fatalf("retry result = %#v, want successful SVG result", result)
	}
	if result.LatencyMs != 3000 {
		t.Fatalf("combined latency = %dms, want 3000ms", result.LatencyMs)
	}
}

func TestRunPelicanTestWithTransientRetryDoesNotRetryPartialOutput(t *testing.T) {
	calls := 0
	partial := &ScheduledTestResult{
		Status: "failed", ResponseText: "partial SVG", ErrorMessage: "connection reset by peer",
	}
	result, err := runPelicanTestWithTransientRetry(context.Background(), func(context.Context) (*ScheduledTestResult, error) {
		calls++
		return partial, nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 || result != partial {
		t.Fatalf("partial response was retried: calls=%d result=%p", calls, result)
	}
}

func TestRunPelicanTestWithTransientRetryDoesNotRetryExplicitHTTPFailure(t *testing.T) {
	calls := 0
	result, err := runPelicanTestWithTransientRetry(context.Background(), func(context.Context) (*ScheduledTestResult, error) {
		calls++
		return &ScheduledTestResult{Status: "failed", ErrorMessage: "HTTP 503: connection reset by peer"}, nil
	})

	if err != nil || calls != 1 || result == nil || result.Status != "failed" {
		t.Fatalf("explicit HTTP error should be returned once: calls=%d result=%#v err=%v", calls, result, err)
	}
}

func TestRunPelicanTestWithTransientRetryDoesNotRetryDeterministicFailure(t *testing.T) {
	calls := 0
	wantErr := errors.New("invalid request body")
	_, err := runPelicanTestWithTransientRetry(context.Background(), func(context.Context) (*ScheduledTestResult, error) {
		calls++
		return nil, wantErr
	})
	if calls != 1 || !errors.Is(err, wantErr) {
		t.Fatalf("deterministic error retry result: calls=%d err=%v", calls, err)
	}
}

func TestIsRetryablePelicanTransportFailure(t *testing.T) {
	cases := []struct {
		name   string
		result *ScheduledTestResult
		err    error
		want   bool
	}{
		{name: "reset", result: &ScheduledTestResult{Status: "failed", ErrorMessage: "Stream read error: connection reset by peer"}, want: true},
		{name: "unexpected eof", result: &ScheduledTestResult{Status: "failed", ErrorMessage: "Failed to read response: unexpected EOF"}, want: true},
		{name: "timeout", err: errors.New("Failed to read response: i/o timeout"), want: true},
		{name: "partial response", result: &ScheduledTestResult{Status: "failed", ResponseText: "some content", ErrorMessage: "connection reset"}, want: false},
		{name: "http failure", result: &ScheduledTestResult{Status: "failed", ErrorMessage: "HTTP 502: upstream unavailable connection reset"}, want: false},
		{name: "overload", result: &ScheduledTestResult{Status: "failed", ErrorMessage: "upstream overload"}, want: false},
		{name: "cancelled", err: context.Canceled, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryablePelicanTransportFailure(tc.result, tc.err); got != tc.want {
				t.Fatalf("retryable = %v, want %v (%q)", got, tc.want, strings.TrimSpace(fmtFailure(tc.result, tc.err)))
			}
		})
	}
}

func fmtFailure(result *ScheduledTestResult, err error) string {
	if err != nil {
		return err.Error()
	}
	if result != nil {
		return result.ErrorMessage
	}
	return ""
}
