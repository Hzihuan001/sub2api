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
	// A known old acknowledgement cannot bind the new send before its own
	// response ID arrives. Old progress/usage/error frames remain raw too.
	for _, late := range []string{
		`{"type":"response.created","response":{"id":"r1"}}`,
		`{"type":"response.output_text.delta","response_id":"r2","delta":"late","usage":{"input_tokens":99,"output_tokens":99}}`,
		`{"type":"error","response_id":"r1","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`,
	} {
		h.sendAndRead(t, late)
	}
	h.sendAndRead(t, `{"type":"response.created","response":{"id":"r3"}}`)
	// A duplicate terminal for r1 is provably old. It must not count as a
	// third overload, invoke billing completion twice, or finish r3's lifecycle.
	late := gatewayFailureIdentityFailedFrame(1)
	h.upstream.Send(late)
	h.upstream.Send(`{"type":"response.completed","response":{"id":"r3","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	payload, err := readPassthroughLifecycleFrame(t, h.client, time.Second)
	require.NoError(t, err)
	require.Equal(t, late, string(payload), "known late frames must remain wire-compatible")
	payload, err = readPassthroughLifecycleFrame(t, h.client, time.Second)
	require.NoError(t, err)
	require.Equal(t, "response.completed", gjson.GetBytes(payload, "type").String())
	got := h.nextCompleted(t)
	require.Equal(t, "r3", got.requestID, "the prior response must not receive a second turn-complete callback")
	require.False(t, got.cooling, "a delayed old response must not be counted against the active attempt")
	require.Equal(t, 1, got.inputTokens)
	require.Equal(t, 1, got.outputTokens)
	select {
	case duplicate := <-h.completed:
		t.Fatalf("duplicate completion after three requests: %+v", duplicate)
	default:
	}
}

func TestGatewayFailureAttemptPassthroughLateTerminalDoesNotDisarmNextTurnTimeout(t *testing.T) {
	h := newGatewayFailureIdentityHarness(t)
	h.nextRequest(t, 1)
	h.sendAndRead(t, `{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	require.Equal(t, "r1", h.nextCompleted(t).requestID)
	h.nextRequest(t, 2)
	h.sendAndRead(t, `{"type":"response.created","response":{"id":"r2"}}`)
	late := gatewayFailureIdentityFailedFrame(1)
	h.upstream.Send(late)
	payload, err := readPassthroughLifecycleFrame(t, h.client, time.Second)
	require.NoError(t, err)
	require.Equal(t, late, string(payload))
	_, err = readPassthroughLifecycleFrame(t, h.client, 3*time.Second)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusGoingAway, closeErr.Code)
	require.Equal(t, "upstream produced no semantic output; please reconnect", closeErr.Reason)
	select {
	case got := <-h.completed:
		t.Fatalf("late terminal incorrectly completed an active turn: %+v", got)
	default:
	}
}

func TestGatewayFailureAttemptPassthroughLateTerminalDoesNotAllowOverlap(t *testing.T) {
	h := newGatewayFailureIdentityHarness(t)
	h.nextRequest(t, 1)
	h.sendAndRead(t, `{"type":"response.completed","response":{"id":"r1","usage":{"input_tokens":1,"output_tokens":1}}}`)
	h.nextCompleted(t)
	h.nextRequest(t, 2)
	h.sendAndRead(t, `{"type":"response.created","response":{"id":"r2"}}`)
	h.sendAndRead(t, gatewayFailureIdentityFailedFrame(1))
	ctx, cancel := context.WithTimeout(h.ctx, time.Second)
	err := h.client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`))
	cancel()
	require.NoError(t, err)
	_, err = readPassthroughLifecycleFrame(t, h.client, time.Second)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusPolicyViolation, closeErr.Code)
	select {
	case payload := <-h.upstream.writes:
		t.Fatalf("an old terminal allowed an overlapping upstream create: %s", payload)
	default:
	}
}

func TestGatewayFailureAttemptPassthroughDelayedCreatedDoesNotCompleteFailureTwice(t *testing.T) {
	for _, firstAnonymous := range []bool{false, true} {
		t.Run(fmt.Sprintf("first_anonymous=%t", firstAnonymous), func(t *testing.T) {
			testGatewayFailureAttemptPassthroughDelayedCreated(t, firstAnonymous)
		})
	}
}

func testGatewayFailureAttemptPassthroughDelayedCreated(t *testing.T, firstAnonymous bool) {
	t.Helper()
	h := newGatewayFailureIdentityHarness(t)
	for turn := 1; turn <= 3; turn++ {
		h.nextRequest(t, turn)
		errorFrame := fmt.Sprintf(`{"type":"error","response_id":"r%d","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`, turn)
		if turn == 1 && firstAnonymous {
			errorFrame = `{"type":"error","id":"event_1","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`
		}
		h.sendAndRead(t, errorFrame)
		h.sendAndRead(t, fmt.Sprintf(`{"type":"response.created","response":{"id":"r%d"}}`, turn))
		h.sendAndRead(t, gatewayFailureIdentityFailedFrame(turn))
		got := h.nextCompleted(t)
		require.Equal(t, fmt.Sprintf("r%d", turn), got.requestID)
		require.Equal(t, turn == 3, got.cooling)
		require.Equal(t, 1, got.inputTokens, "only the authoritative failure usage must be settled")
		require.Equal(t, 1, got.outputTokens)
		select {
		case duplicate := <-h.completed:
			t.Fatalf("same-attempt acknowledgement settled the error twice: %+v", duplicate)
		default:
		}
	}
}

func TestGatewayFailureAttemptPassthroughAnonymousLaterErrorReconnectsWithoutGuessing(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		for name, anonymous := range map[string]string{
			"error":     `{"type":"error","id":"event_1","error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}`,
			"failed":    `{"type":"response.failed","response":{"usage":{"input_tokens":99,"output_tokens":99},"error":{"code":"server_is_overloaded","message":"Our servers are currently overloaded. Please try again later."}}}`,
			"completed": `{"type":"response.completed","response":{"usage":{"input_tokens":99,"output_tokens":99}}}`,
		} {
			t.Run(fmt.Sprintf("%s/acknowledged=%t", name, acknowledged), func(t *testing.T) {
				testGatewayFailureAttemptPassthroughAnonymousLaterFrame(t, anonymous, acknowledged)
			})
		}
	}
}

func testGatewayFailureAttemptPassthroughAnonymousLaterFrame(t *testing.T, anonymous string, acknowledged bool) {
	t.Helper()
	h := newGatewayFailureIdentityHarness(t)
	for turn := 1; turn <= 2; turn++ {
		h.nextRequest(t, turn)
		h.sendAndRead(t, gatewayFailureIdentityFailedFrame(turn))
		require.False(t, h.nextCompleted(t).cooling)
	}
	h.nextRequest(t, 3)
	if acknowledged {
		h.sendAndRead(t, `{"type":"response.created","response":{"id":"r3"}}`)
	}
	// This could be a late r1 error or an r3 failure. In particular, error.id
	// identifies the error, not the response, and must not invent an owner.
	h.upstream.Send(anonymous)
	payload, err := readPassthroughLifecycleFrame(t, h.client, time.Second)
	require.NoError(t, err)
	require.Equal(t, anonymous, string(payload))
	_, err = readPassthroughLifecycleFrame(t, h.client, time.Second)
	var closeErr coderws.CloseError
	require.ErrorAs(t, err, &closeErr)
	require.Equal(t, coderws.StatusTryAgainLater, closeErr.Code)
	require.Equal(t, "upstream response identity is ambiguous; please reconnect", closeErr.Reason)
	require.False(t, h.isCooling(), "an anonymous old frame must not count as the third account failure")
	select {
	case got := <-h.completed:
		t.Fatalf("an anonymous error must not replay an old completion: %+v", got)
	default:
	}
	select {
	case payload := <-h.upstream.writes:
		t.Fatalf("ambiguous response must not automatically replay upstream: %s", payload)
	default:
	}
}

type gatewayFailureIdentityCompletion struct {
	requestID    string
	cooling      bool
	inputTokens  int
	outputTokens int
}

type gatewayFailureIdentityHarness struct {
	ctx       context.Context
	upstream  *stagedPassthroughConn
	client    *coderws.Conn
	completed chan gatewayFailureIdentityCompletion
	isCooling func() bool
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
		isCooling: func() bool { return limiter.IsAccountOverloadCooling(account.ID, time.Now()) },
	}
	hooks := &OpenAIWSIngressHooks{AfterTurn: func(_ int, result *OpenAIForwardResult, err error) {
		if err == nil && result != nil {
			h.completed <- gatewayFailureIdentityCompletion{
				requestID:    result.RequestID,
				cooling:      h.isCooling(),
				inputTokens:  result.Usage.InputTokens,
				outputTokens: result.Usage.OutputTokens,
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
	actual, err := readPassthroughLifecycleFrame(t, h.client, time.Second)
	require.NoError(t, err)
	require.Equal(t, payload, string(actual), "identity observation must preserve raw frames")
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
