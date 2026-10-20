package kiro

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func buildTypedExceptionFrame(t *testing.T, kind string, payload []byte) []byte {
	t.Helper()
	headers := bytes.NewBuffer(nil)
	for _, header := range [][2]string{{":message-type", "exception"}, {":exception-type", kind}, {":content-type", "application/json"}} {
		_ = headers.WriteByte(byte(len(header[0])))
		_, _ = headers.WriteString(header[0])
		_ = headers.WriteByte(7)
		require.NoError(t, binary.Write(headers, binary.BigEndian, uint16(len(header[1]))))
		_, _ = headers.WriteString(header[1])
	}
	frame := bytes.NewBuffer(nil)
	require.NoError(t, binary.Write(frame, binary.BigEndian, uint32(16+headers.Len()+len(payload))))
	require.NoError(t, binary.Write(frame, binary.BigEndian, uint32(headers.Len())))
	require.NoError(t, binary.Write(frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
	_, _ = frame.Write(headers.Bytes())
	_, _ = frame.Write(payload)
	require.NoError(t, binary.Write(frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
	return frame.Bytes()
}

func TestKiroObservedTypedExceptionNeverBuildsEstimatedSuccess(t *testing.T) {
	frame := buildTypedExceptionFrame(t, "ServiceUnavailableException", []byte(`{"message":"busy"}`))
	var observed []EventStreamObservation
	request := KiroRequestContext{EstimatedInputTokens: 100, CacheEmulationUsage: &Usage{InputTokens: 90, CacheReadInputTokens: 10}, ObserveEvent: func(event EventStreamObservation) { observed = append(observed, event) }}
	result, err := ParseNonStreamingEventStreamWithContext(bytes.NewReader(frame), "claude-sonnet-4.5", request)
	var failure *EventStreamFailure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, 503, failure.StatusCode())
	require.NotNil(t, result)
	require.Empty(t, result.ResponseBody)
	require.Equal(t, Usage{}, result.Usage)
	require.Len(t, observed, 1)
	require.Same(t, failure, observed[0].Failure)

	// The old parser's opt-out path is unchanged, including estimated usage.
	native, nativeErr := ParseNonStreamingEventStreamWithContext(bytes.NewReader(frame), "claude-sonnet-4.5", KiroRequestContext{EstimatedInputTokens: 100})
	require.NoError(t, nativeErr)
	require.Equal(t, 100, native.Usage.InputTokens)

	var stream bytes.Buffer
	streamResult, streamErr := StreamEventStreamAsAnthropicWithContext(context.Background(), bytes.NewReader(frame), &stream, "claude-sonnet-4.5", 100, request)
	require.ErrorAs(t, streamErr, &failure)
	require.NotNil(t, streamResult)
	require.Equal(t, Usage{}, streamResult.Usage)
	require.Empty(t, stream.String(), "no synthetic message_start/message_stop or estimated usage on an immediate exception")
}

func TestKiroObservedTypedExceptionKeepsActualPartialUsage(t *testing.T) {
	var frames bytes.Buffer
	_, _ = frames.Write(buildEventStreamFrame(t, "assistantResponseEvent", map[string]any{"content": "partial output"}))
	_, _ = frames.Write(buildEventStreamFrame(t, "messageMetadataEvent", map[string]any{"tokenUsage": map[string]any{"uncachedInputTokens": 7, "outputTokens": 3, "totalTokens": 10}}))
	_, _ = frames.Write(buildEventStreamFrame(t, "meteringEvent", map[string]any{"usage": 0.04}))
	_, _ = frames.Write(buildTypedExceptionFrame(t, "InternalServerException", []byte(`{"message":"generation failed"}`)))
	var observed EventStreamObservation
	request := KiroRequestContext{EstimatedInputTokens: 100, CacheEmulationUsage: &Usage{InputTokens: 90, CacheReadInputTokens: 10}, ObserveEvent: func(event EventStreamObservation) { observed = event }}
	result, err := ParseNonStreamingEventStreamWithContext(bytes.NewReader(frames.Bytes()), "claude-sonnet-4.5", request)
	var failure *EventStreamFailure
	require.ErrorAs(t, err, &failure)
	require.Equal(t, Usage{InputTokens: 7, OutputTokens: 3, TotalTokens: 10, KiroCredits: 0.04}, result.Usage)
	require.Equal(t, result.Usage, observed.Usage)
	require.Empty(t, result.ResponseBody)
	var stream bytes.Buffer
	streamResult, streamErr := StreamEventStreamAsAnthropicWithContext(context.Background(), bytes.NewReader(frames.Bytes()), &stream, "claude-sonnet-4.5", 100, request)
	require.ErrorAs(t, streamErr, &failure)
	require.Equal(t, result.Usage, streamResult.Usage)
	require.Contains(t, stream.String(), "partial output")
	require.NotContains(t, stream.String(), "message_stop")
}

func TestKiroObservedOpaqueResponsesDoNotGiveTypedProof(t *testing.T) {
	for name, frame := range map[string][]byte{
		"event name only": buildEventStreamFrame(t, "ServiceUnavailableException", map[string]any{"message": "busy"}),
		"bad JSON":        buildTypedExceptionFrame(t, "ServiceUnavailableException", []byte(`{"message":`)),
		"empty body":      buildTypedExceptionFrame(t, "ServiceUnavailableException", nil),
		"empty message":   buildTypedExceptionFrame(t, "ServiceUnavailableException", []byte(`{}`)),
		"trailing JSON":   buildTypedExceptionFrame(t, "ServiceUnavailableException", []byte(`{"message":"busy"}{}`)),
		"no frames":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			proof := false
			request := KiroRequestContext{EstimatedInputTokens: 100, ObserveEvent: func(event EventStreamObservation) { proof = proof || event.Failure != nil }}
			result, err := ParseNonStreamingEventStreamWithContext(bytes.NewReader(frame), "claude-sonnet-4.5", request)
			require.Error(t, err)
			var failure *EventStreamFailure
			require.False(t, errors.As(err, &failure))
			require.False(t, proof)
			require.NotNil(t, result)
			require.Zero(t, result.Usage.InputTokens)
			var stream bytes.Buffer
			streamResult, streamErr := StreamEventStreamAsAnthropicWithContext(context.Background(), bytes.NewReader(frame), &stream, "claude-sonnet-4.5", 100, request)
			require.Error(t, streamErr)
			require.False(t, errors.As(streamErr, &failure))
			require.NotNil(t, streamResult)
			require.False(t, proof)
			require.False(t, strings.Contains(stream.String(), "message_stop"))
		})
	}
}
