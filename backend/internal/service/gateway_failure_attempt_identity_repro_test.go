package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// These are intentionally end-to-end contract reproductions, not tests of an
// isolated counter. They drive the actual passthrough relay, account executor,
// and turn-complete callback through missing/late upstream acknowledgement.
func TestGatewayFailureAttemptPassthroughMustCountNextTurnWithoutCreated(t *testing.T) {
	h := newGatewayFailureIdentityHarness(t)
	for turn := 1; turn <= 3; turn++ {
		h.nextRequest(t, turn)
		if turn == 1 {
			h.sendAndRead(t, `{"type":"response.created","response":{"id":"r1"}}`)
		}
		h.sendAndRead(t, gatewayFailureIdentityFailedFrame(turn))
		got := h.nextCompleted(t)
		require.Equal(t, fmt.Sprintf("r%d", turn), got.requestID)
		require.Equal(t, turn == 3, got.cooling,
			"each real upstream send must count even when response.created is omitted")
	}
}

func TestGatewayFailureAttemptPassthroughMustIgnoreKnownLateTerminal(t *testing.T) {
	h := newGatewayFailureIdentityHarness(t)
	for turn := 1; turn <= 2; turn++ {
		h.nextRequest(t, turn)
		h.sendAndRead(t, fmt.Sprintf(`{"type":"response.created","response":{"id":"r%d"}}`, turn))
		h.sendAndRead(t, gatewayFailureIdentityFailedFrame(turn))
		got := h.nextCompleted(t)
		require.Equal(t, fmt.Sprintf("r%d", turn), got.requestID)
		require.False(t, got.cooling)
	}
	h.nextRequest(t, 3)
	h.sendAndRead(t, `{"type":"response.created","response":{"id":"r3"}}`)
	// A duplicate terminal for r1 is provably old. It must not count as a
	// third overload, invoke billing completion twice, or finish r3's lifecycle.
	h.upstream.Send(gatewayFailureIdentityFailedFrame(1))
	h.upstream.Send(`{"type":"response.completed","response":{"id":"r3","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	for {
		payload, err := readPassthroughLifecycleFrame(t, h.client, time.Second)
		require.NoError(t, err)
		if gjson.GetBytes(payload, "type").String() == "response.completed" {
			break
		}
	}
	got := h.nextCompleted(t)
	require.Equal(t, "r3", got.requestID, "the prior response must not receive a second turn-complete callback")
	require.False(t, got.cooling, "a delayed old response must not be counted against the active attempt")
	select {
	case duplicate := <-h.completed:
		t.Fatalf("duplicate completion after three requests: %+v", duplicate)
	default:
	}
}

type gatewayFailureIdentityCompletion struct {
	requestID string
	cooling   bool
}

type gatewayFailureIdentityHarness struct {
	ctx       context.Context
	upstream  *stagedPassthroughConn
	client    *coderws.Conn
	completed chan gatewayFailureIdentityCompletion
}

func newGatewayFailureIdentityHarness(t *testing.T) *gatewayFailureIdentityHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, cancel := context.WithCancelCause(context.Background())
	upstream := newStagedPassthroughConn()
	cfg := passthroughLifecycleConfig()
	svc := newPassthroughLifecycleService(cfg, upstream)
	account := passthroughLifecycleAccount()
	repo := &openAIWSIngressOverloadRepo{}
	limiter := NewRateLimitService(repo, nil, cfg, nil, nil)
	svc.accountRepo, svc.rateLimitService = repo, limiter
	limiter.SetAccountRuntimeBlocker(svc)
	h := &gatewayFailureIdentityHarness{
		ctx: ctx, upstream: upstream, completed: make(chan gatewayFailureIdentityCompletion, 16),
	}
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
		if err == nil && result != nil {
			h.completed <- gatewayFailureIdentityCompletion{
				requestID: result.RequestID,
				cooling:   limiter.IsAccountOverloadCooling(account.ID, time.Now()),
			}
		}
	}}
	server, serverErr := startPassthroughHookRecordingServer(t, ctx, svc, account, hooks)
	h.client = dialPassthroughLifecycleClient(t, server)
	t.Cleanup(func() {
		_ = h.client.CloseNow()
		_ = upstream.Close()
		cancel(context.Canceled)
		select {
		case <-serverErr:
		case <-time.After(3 * time.Second):
			t.Error("passthrough relay did not stop after test cleanup")
		}
		server.Close()
	})
	return h
}

func (h *gatewayFailureIdentityHarness) nextRequest(t *testing.T, turn int) {
	t.Helper()
	if turn > 1 {
		writeCtx, cancel := context.WithTimeout(h.ctx, time.Second)
		err := h.client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
		cancel()
		require.NoError(t, err)
	}
	payload := requirePassthroughUpstreamWrite(t, h.upstream, time.Second)
	require.Equal(t, "response.create", gjson.GetBytes(payload, "type").String())
}

func (h *gatewayFailureIdentityHarness) sendAndRead(t *testing.T, payload string) {
	t.Helper()
	h.upstream.Send(payload)
	_, err := readPassthroughLifecycleFrame(t, h.client, time.Second)
	require.NoError(t, err)
}

func (h *gatewayFailureIdentityHarness) nextCompleted(t *testing.T) gatewayFailureIdentityCompletion {
	t.Helper()
	select {
	case got := <-h.completed:
		return got
	case <-time.After(time.Second):
		t.Fatal("expected terminal completion callback")
		return gatewayFailureIdentityCompletion{}
	}
}

func gatewayFailureIdentityFailedFrame(turn int) string {
	return fmt.Sprintf(`{"type":"response.failed","response":{"id":"r%d","model":"gpt-5.1","status":"failed","usage":{"input_tokens":1,"output_tokens":1},"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`, turn)
}
