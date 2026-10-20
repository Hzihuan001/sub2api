package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountAttemptInstallPreservesInboundAndRestoresScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	root, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(root)
	old := keyrouting.NewFirstContentGuard(0)
	defer old.Finish()
	c.Request = c.Request.WithContext(keyrouting.WithFirstContentGuard(root, old))
	parent := c.Request.Context()
	a := keyrouting.NewAccountAttempt(keyrouting.AccountAttemptMetadata{APIKeyID: 1, GroupID: 2, AccountID: 3}, 0)
	restore := installKeyAccountAttempt(c, a)
	require.Same(t, a, keyAccountAttemptFromContext(c))
	require.Same(t, a.Guard(), keyrouting.GetFirstContentGuard(c.Request.Context()))
	child := a.Dispatch(c.Request.Context())
	cancel()
	require.Error(t, c.Request.Context().Err(), "inbound request must retain client cancellation")
	require.NoError(t, child.Err(), "native billing drain retains detached context ownership")
	sample := finishKeyAccountAttempt(c, a, nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UpstreamStatusCode: 503})
	require.Equal(t, keyrouting.OutcomeIgnored, sample.Outcome)
	restore()
	restore()
	require.Same(t, parent, c.Request.Context())
	require.Same(t, old, keyrouting.GetFirstContentGuard(c.Request.Context()))
}

func TestKeyAccountAttemptStaleRestoreCannotRemoveNextAttempt(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	parent := c.Request.Context()
	first := keyrouting.NewAccountAttempt(keyrouting.AccountAttemptMetadata{}, 0)
	second := keyrouting.NewAccountAttempt(keyrouting.AccountAttemptMetadata{}, 0)
	restoreFirst := installKeyAccountAttempt(c, first)
	restoreSecond := installKeyAccountAttempt(c, second)
	restoreFirst()
	require.Same(t, second, keyAccountAttemptFromContext(c))
	restoreSecond()
	require.Equal(t, parent, c.Request.Context(), "a closed preceding scope must not be resurrected")
	first.Finish(keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeIgnored})
	second.Finish(keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeIgnored})
}

func TestKeyAccountAttemptNativeUsageVocabularyAndCleanupOwnership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result any
	}{
		{name: "input tokens", result: &service.OpenAIForwardResult{Usage: service.OpenAIUsage{InputTokens: 3}}},
		{name: "cached tokens", result: &service.ForwardResult{Usage: service.ClaudeUsage{CacheReadInputTokens: 3}}},
		{name: "image", result: &service.OpenAIForwardResult{ImageCount: 1}},
		{name: "storage", result: &service.ForwardResult{CacheStorageTokenHours: 0.1}},
		{name: "audio", result: &service.OpenAIForwardResult{AudioUsage: &service.AudioUsage{DurationOrUnits: 0.1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			a := keyrouting.NewAccountAttempt(keyrouting.AccountAttemptMetadata{}, 0)
			restore := installKeyAccountAttempt(c, a)
			defer restore()
			a.Dispatch(c.Request.Context())
			sample := finishKeyAccountAttempt(c, a, tc.result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.True(t, sample.UsageKnown)
			require.False(t, a.CanStartNext())
			a.MarkCleanupComplete()
			require.False(t, a.CanStartNext(), "known billing always wins over account/group retry")
			require.False(t, c.Writer.Written(), "the adapter must not manufacture a response or usage record")
		})
	}
}

func TestKeyAccountAttemptCommittedResponseVetoesReplay(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	a := keyrouting.NewAccountAttempt(keyrouting.AccountAttemptMetadata{}, 0)
	restore := installKeyAccountAttempt(c, a)
	defer restore()
	a.Dispatch(c.Request.Context())
	c.String(200, "already returned")
	sample := finishKeyAccountAttempt(c, a, nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	a.MarkCleanupComplete()
	require.True(t, sample.ResponseCommitted)
	require.False(t, a.CanStartNext())
}
