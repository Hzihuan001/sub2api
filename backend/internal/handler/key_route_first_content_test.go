package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func firstContentExpiredContext(t *testing.T) (*gin.Context, *keyrouting.FirstContentGuard) {
	t.Helper()
	guard := keyrouting.NewFirstContentGuard(time.Millisecond)
	ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
	child := guard.Detach(ctx)
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("group deadline did not fire")
	}
	t.Cleanup(guard.Finish)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	return c, guard
}

func TestKeyRouteFirstContentInterruptedOnlyUnsettledExpiredAttempts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result any
		err    error
		want   bool
	}{
		{name: "no result", err: context.Canceled, want: true},
		{name: "zero partial OpenAI", result: &service.OpenAIForwardResult{}, err: context.Canceled, want: true},
		{name: "zero partial Anthropic", result: &service.ForwardResult{}, err: context.Canceled, want: true},
		{name: "complete zero-usage OpenAI", result: &service.OpenAIForwardResult{}},
		{name: "complete zero-usage Anthropic", result: &service.ForwardResult{}},
		{name: "partial input usage", result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 3}}, err: context.Canceled},
		{name: "partial cached usage", result: &service.ForwardResult{Usage: service.ClaudeUsage{CacheReadInputTokens: 3}}, err: context.Canceled},
		{name: "partial output usage", result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{OutputTokens: 3}}, err: context.Canceled},
		{name: "partial image usage", result: &service.OpenAIForwardResult{ImageCount: 1}, err: context.Canceled},
		{name: "partial search usage", result: &service.ForwardResult{SearchCount: 1}, err: context.Canceled},
		{name: "partial credit usage", result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{KiroCredits: 0.1}}, err: context.Canceled},
		{name: "partial storage usage", result: &service.ForwardResult{CacheStorageTokenHours: 0.1}, err: context.Canceled},
		{name: "partial audio usage", result: &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{DurationOrUnits: 0.1}}, err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, guard := firstContentExpiredContext(t)
			if got := keyRouteFirstContentInterrupted(c, tc.result, tc.err); got != tc.want {
				t.Fatalf("interrupted=%v, want %v", got, tc.want)
			}
			if !tc.want && guard.ReplaySafe() {
				t.Fatal("known billable or complete result allowed a second attempt")
			}
			if c.IsAborted() || c.Request.Context().Err() != nil || c.Writer.Written() {
				t.Fatal("return hook mutated the client response, abort state, or inbound cancellation")
			}
		})
	}
}

func TestKeyRouteFirstContentInterruptedLeavesClientCancellationAlone(t *testing.T) {
	c, _ := firstContentExpiredContext(t)
	root, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(root)
	if keyRouteFirstContentInterrupted(c, nil, context.Canceled) {
		t.Fatal("a disconnected client must not initiate backup routing")
	}
	plain, _ := gin.CreateTestContext(httptest.NewRecorder())
	plain.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	if keyRouteFirstContentInterrupted(plain, nil, errors.New("original native failure")) {
		t.Fatal("unguarded requests must keep their native behavior")
	}
}

func TestKeyRouteFirstContentNormalizeErrorOnlyBlocksKnownBillableRetries(t *testing.T) {
	for _, tc := range []struct {
		name    string
		guarded bool
		usage   bool
	}{
		{name: "legacy", usage: true},
		{name: "guarded ordinary error", guarded: true},
		{name: "guarded raw usage with nil partial result", guarded: true, usage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx := context.Background()
			guard := keyrouting.NewFirstContentGuard(0)
			defer guard.Finish()
			if tc.guarded {
				ctx = keyrouting.WithFirstContentGuard(ctx, guard)
			}
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			if tc.usage {
				reader := keyrouting.ProtectUsageReader(ctx, io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"usage\":{\"input_tokens\":7}}}\n\n")))
				_, err := io.ReadAll(reader)
				require.NoError(t, err)
				require.NoError(t, reader.Close())
			}
			original := fmt.Errorf("native parser failed: %w", &service.UpstreamFailoverError{StatusCode: 503, RetryableOnSameAccount: true})
			err := keyRouteFirstContentNormalizeError(c, original)
			require.Equal(t, original.Error(), err.Error())
			var retry *service.UpstreamFailoverError
			if tc.guarded && tc.usage {
				require.False(t, errors.As(err, &retry), "neither same-account nor next-account retry may consume the request twice")
				require.False(t, c.Writer.Written(), "normalization must leave error response and billing to the native handler")
			} else {
				require.Same(t, original, err)
				require.True(t, errors.As(err, &retry))
			}
		})
	}
}
