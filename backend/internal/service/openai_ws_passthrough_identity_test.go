package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSPassthroughIdentityRetainsCompletedIDsWithoutEviction(t *testing.T) {
	state := &openAIWSPassthroughResponseIdentity{}
	for turn := 0; turn < openAIWSPassthroughIdentityLimit; turn++ {
		require.NoError(t, state.beginAttempt())
		state.observe(coderws.MessageText, []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":"r%d"}}`, turn)))
		require.True(t, state.shouldObserveLastFrame())
	}
	require.Len(t, state.responses, openAIWSPassthroughIdentityLimit)
	require.Error(t, state.beginAttempt(), "close the connection rather than forget a completed identity")
	state.observe(coderws.MessageText, []byte(`{"type":"response.failed","response":{"id":"r0"}}`))
	require.False(t, state.shouldObserveLastFrame())
	require.Nil(t, state.failureEffects())
}

func TestOpenAIWSPassthroughIdentityUsesResponseIDWithoutCreated(t *testing.T) {
	state := &openAIWSPassthroughResponseIdentity{}
	require.NoError(t, state.beginAttempt())
	state.observe(coderws.MessageText, []byte(`{"type":"error","response_id":"r1","error":{"code":"server_is_overloaded"}}`))
	first := state.failureEffects()
	require.NotNil(t, first)
	first.record(true)
	state.observe(coderws.MessageText, []byte(`{"type":"response.failed","response":{"id":"r1"}}`))
	require.Same(t, first, state.failureEffects())
	require.False(t, state.failureEffects().shouldObserve())
	require.NoError(t, state.beginAttempt())
	state.observe(coderws.MessageText, []byte(`{"type":"response.failed","response_id":"r2"}`))
	require.True(t, state.failureEffects().shouldObserve())
	require.NotSame(t, first, state.failureEffects())
}

func TestOpenAIWSPassthroughIdentityRejectsNewSendAfterAmbiguousIdleFrame(t *testing.T) {
	inner := newStagedPassthroughConn()
	identity := &openAIWSPassthroughResponseIdentity{}
	conn := &openAIWSPassthroughFirstOutputFrameConn{inner: inner, identity: identity}
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create"}`)))
	requirePassthroughUpstreamWrite(t, inner, time.Second)
	inner.Send(`{"type":"response.completed","response":{"id":"r1"}}`)
	_, _, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	inner.Send(`{"type":"error","error":{"code":"server_is_overloaded"}}`)
	_, payload, err := conn.ReadFrame(context.Background())
	require.NoError(t, err, "the ambiguous frame must still reach the downstream writer")
	require.Contains(t, string(payload), `"type":"error"`)
	require.False(t, conn.ShouldObserveLastFrame())
	// The client writer can run before the reader's next call reports the
	// pending reconnect. It must not send a new request on this connection.
	err = conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create"}`))
	require.Error(t, err)
	require.Same(t, identity.readError, err)
	select {
	case payload := <-inner.writes:
		t.Fatalf("new request sent after ambiguous response: %s", payload)
	default:
	}
}

func TestOpenAIWSPassthroughIdentityFailedWriteDoesNotObserveFailure(t *testing.T) {
	inner := newStagedPassthroughConn()
	require.NoError(t, inner.Close())
	identity := &openAIWSPassthroughResponseIdentity{}
	conn := &openAIWSPassthroughFirstOutputFrameConn{
		inner: inner, identity: identity,
		resolveDeadline: func([]byte) openAIWSPassthroughFirstOutputDeadline {
			return openAIWSPassthroughFirstOutputDeadline{timeout: time.Second}
		},
	}
	err := conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create","metadata":{"custom":"unchanged"}}`))
	require.Error(t, err)
	require.Equal(t, uint64(1), identity.sequence, "identity is installed before the send to cover immediate replies")
	require.False(t, identity.current.effects.observed, "a failed send must not apply failure/account effects")
	require.Empty(t, identity.responses)
}

func TestOpenAIWSPassthroughIdentityBinaryCreatePreservesOpaqueDeadline(t *testing.T) {
	inner := newStagedPassthroughConn()
	identity := &openAIWSPassthroughResponseIdentity{}
	conn := &openAIWSPassthroughFirstOutputFrameConn{
		inner: inner, identity: identity,
		resolveDeadline: func([]byte) openAIWSPassthroughFirstOutputDeadline {
			return openAIWSPassthroughFirstOutputDeadline{timeout: time.Second}
		},
	}
	payload := []byte(`{"type":"response.create","metadata":{"custom":"unchanged"}}`)
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageBinary, payload))
	require.Equal(t, payload, requirePassthroughUpstreamWrite(t, inner, time.Second))
	require.Equal(t, uint64(1), identity.sequence)
	require.False(t, conn.deadlineState().armed, "binary create must not introduce text semantic-output deadlines")
}

func TestOpenAIWSPassthroughIdentityLateProgressKeepsActiveDeadline(t *testing.T) {
	inner := newStagedPassthroughConn()
	identity := &openAIWSPassthroughResponseIdentity{}
	conn := &openAIWSPassthroughFirstOutputFrameConn{
		inner: inner, identity: identity, activeReadTimeout: time.Second,
		resolveDeadline: func([]byte) openAIWSPassthroughFirstOutputDeadline {
			return openAIWSPassthroughFirstOutputDeadline{timeout: time.Second}
		},
	}
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create"}`)))
	requirePassthroughUpstreamWrite(t, inner, time.Second)
	inner.Send(`{"type":"response.completed","response":{"id":"r1"}}`)
	_, _, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create"}`)))
	requirePassthroughUpstreamWrite(t, inner, time.Second)
	inner.Send(`{"type":"response.output_text.delta","response_id":"r2","delta":"new"}`)
	_, _, err = conn.ReadFrame(context.Background())
	require.NoError(t, err)
	before := conn.deadlineState()
	require.Equal(t, openAIWSPassthroughDeadlinePhaseActiveRead, before.deadline.phase)
	late := `{"type":"response.output_text.delta","response_id":"r1","delta":"late"}`
	inner.Send(late)
	_, payload, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	require.Equal(t, late, string(payload))
	require.False(t, conn.ShouldObserveLastFrame())
	require.Equal(t, before, conn.deadlineState(), "old progress must not refresh the active response's deadline")
}

func TestOpenAIWSPassthroughIdentityTerminalObservationCannotDisarmNextSend(t *testing.T) {
	inner := newStagedPassthroughConn()
	identity := &openAIWSPassthroughResponseIdentity{}
	conn := &openAIWSPassthroughFirstOutputFrameConn{
		inner: inner, identity: identity,
		resolveDeadline: func([]byte) openAIWSPassthroughFirstOutputDeadline {
			return openAIWSPassthroughFirstOutputDeadline{timeout: 30 * time.Millisecond}
		},
	}
	request := []byte(`{"type":"response.create"}`)
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, request))
	requirePassthroughUpstreamWrite(t, inner, time.Second)
	terminal := []byte(`{"type":"response.completed","response":{"id":"r1"}}`)
	// Deterministically pause ReadFrame between its identity observation and
	// deadline observation, then let the independent writer send the next turn.
	identity.observe(coderws.MessageText, terminal)
	require.True(t, conn.ShouldObserveLastFrame())
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, request))
	requirePassthroughUpstreamWrite(t, inner, time.Second)
	nextDeadline := conn.deadlineState()
	require.True(t, nextDeadline.armed)
	conn.observeUpstreamActivity(coderws.MessageText, terminal)
	require.Equal(t, nextDeadline, conn.deadlineState(), "the prior terminal must not disarm a newer send's deadline")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := conn.ReadFrame(ctx)
	require.ErrorIs(t, err, errOpenAIWSPassthroughFirstOutputTimeout, "the next send must still have a bounded first-output wait")
}
