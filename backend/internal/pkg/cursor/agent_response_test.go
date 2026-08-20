package cursor

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

// nest builds a chain of length-delimited fields from the inside out, so a test
// fixture reads in the same order as the byte path it stands for:
// nest([]byte("x"), 1, 4, 1) is f1{f4{f1: "x"}}.
func nest(leaf []byte, path ...int) []byte {
	out := leaf
	for i := len(path) - 1; i >= 0; i-- {
		var w Writer
		w.WriteBytes(path[i], out)
		out = w.Bytes()
	}
	return out
}

func stringField(field int, value string) []byte {
	var w Writer
	w.WriteString(field, value)
	return w.Bytes()
}

func varintField(field int, value int64) []byte {
	var w Writer
	w.WriteInt64(field, value)
	return w.Bytes()
}

// Each case pins one documented byte path through AgentServerMessage. These are
// the paths the whole response side rests on, so they are asserted from raw
// bytes rather than through the encoder.
func TestParseAgentServerMessageBytePaths(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		want    AgentEvent
	}{
		{
			name:    "text delta 1.1.1",
			payload: nest(stringField(1, "hello"), 1, 1),
			want:    AgentEvent{Type: AgentEventText, Text: "hello"},
		},
		{
			name:    "thinking delta 1.4.1",
			payload: nest(stringField(1, "pondering"), 1, 4),
			want:    AgentEvent{Type: AgentEventThinking, Text: "pondering"},
		},
		{
			name:    "thinking completed 1.5.1",
			payload: nest(varintField(1, 1200), 1, 5),
			want:    AgentEvent{Type: AgentEventThinkingEnd, Usage: &AgentUsage{ThinkingDurationMs: 1200}},
		},
		{
			name:    "tool call started 1.2",
			payload: nest(stringField(1, "call-7"), 1, 2),
			want:    AgentEvent{Type: AgentEventToolCallStarted, ToolCall: &AgentToolCall{ID: "call-7"}},
		},
		{
			name:    "token delta 1.8.1",
			payload: nest(varintField(1, 42), 1, 8),
			want:    AgentEvent{Type: AgentEventTokenDelta, Usage: &AgentUsage{OutputTokens: 42}},
		},
		{
			name:    "heartbeat 1.13",
			payload: nest(nil, 1, 13),
			want:    AgentEvent{Type: AgentEventHeartbeat},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAgentServerMessage(tc.payload)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got == nil {
				t.Fatal("expected an event, got nil")
			}
			if !reflect.DeepEqual(*got, tc.want) {
				t.Errorf("event = %+v, want %+v", *got, tc.want)
			}
		})
	}
}

// The partial tool call carries the call id at f1 and the argument fragment at
// f3 of the same message, so it needs a two-field fixture.
func TestParseAgentServerMessagePartialToolCall(t *testing.T) {
	var update Writer
	update.WriteString(fieldAgentPartialToolCallID, "call-9")
	update.WriteString(fieldAgentPartialToolCallArgs, `{"loc`)

	event, err := ParseAgentServerMessage(nest(update.Bytes(), 1, 7))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event == nil || event.Type != AgentEventToolCallArgs {
		t.Fatalf("event = %+v, want a tool-call-args event", event)
	}
	if event.Text != `{"loc` {
		t.Errorf("args fragment = %q", event.Text)
	}
	if event.ToolCall == nil || event.ToolCall.ID != "call-9" {
		t.Errorf("tool call = %+v, want id call-9", event.ToolCall)
	}
}

func TestParseAgentServerMessageTurnEndedUsage(t *testing.T) {
	var turn Writer
	turn.WriteInt64(fieldAgentTurnInputTokens, 1200)
	turn.WriteInt64(fieldAgentTurnOutputTokens, 340)
	turn.WriteInt64(fieldAgentTurnCacheReadTokens, 900)
	turn.WriteInt64(fieldAgentTurnCacheWriteTokens, 12)

	event, err := ParseAgentServerMessage(nest(turn.Bytes(), 1, 14))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event == nil || event.Type != AgentEventTurnEnded {
		t.Fatalf("event = %+v, want turn_ended", event)
	}
	want := &AgentUsage{InputTokens: 1200, OutputTokens: 340, CacheReadTokens: 900, CacheWriteTokens: 12}
	if !reflect.DeepEqual(event.Usage, want) {
		t.Errorf("usage = %+v, want %+v", event.Usage, want)
	}
}

// mcpArgsPayload builds AgentServerMessage.exec_server_message.mcp_args.
func mcpArgsPayload(t *testing.T, name, toolName, callID string, args map[string]any) []byte {
	t.Helper()
	var mcp Writer
	if name != "" {
		mcp.WriteString(fieldAgentMcpArgsName, name)
	}
	if callID != "" {
		mcp.WriteString(fieldAgentMcpArgsToolCallID, callID)
	}
	mcp.WriteString(fieldAgentMcpArgsProvider, "sub2api")
	if toolName != "" {
		mcp.WriteString(fieldAgentMcpArgsToolName, toolName)
	}
	for key, value := range args {
		var entry Writer
		entry.WriteString(fieldProtobufMapKey, key)
		entry.WriteBytes(fieldProtobufMapValue, encodeProtobufValue(value))
		mcp.WriteMessage(fieldAgentMcpArgsArgs, entry.Bytes())
	}
	return nest(mcp.Bytes(), fieldAgentServerExecMessage, fieldAgentExecMcpArgs)
}

func TestParseAgentServerMessageMcpToolCall(t *testing.T) {
	payload := mcpArgsPayload(t, "namespaced__Read", "Read", "call-1", map[string]any{
		"file_path": "/tmp/x",
		"limit":     5.0,
	})

	event, err := ParseAgentServerMessage(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event == nil || event.Type != AgentEventToolCall {
		t.Fatalf("event = %+v, want tool_call", event)
	}
	// tool_name(5) is the declared name and wins over the namespaced name(1).
	if event.ToolCall.Name != "Read" {
		t.Errorf("name = %q, want %q", event.ToolCall.Name, "Read")
	}
	if event.ToolCall.ID != "call-1" {
		t.Errorf("tool_call_id = %q", event.ToolCall.ID)
	}
	if event.ToolCall.ProviderIdentifier != "sub2api" {
		t.Errorf("provider_identifier = %q", event.ToolCall.ProviderIdentifier)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(event.ToolCall.Arguments), &decoded); err != nil {
		t.Fatalf("arguments are not valid JSON (%q): %v", event.ToolCall.Arguments, err)
	}
	want := map[string]any{"file_path": "/tmp/x", "limit": 5.0}
	if !reflect.DeepEqual(decoded, want) {
		t.Errorf("arguments = %#v, want %#v", decoded, want)
	}
}

func TestParseAgentServerMessageFallsBackToName(t *testing.T) {
	payload := mcpArgsPayload(t, "only_name", "", "", nil)
	event, err := ParseAgentServerMessage(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if event.ToolCall.Name != "only_name" {
		t.Errorf("name = %q, want the name(1) fallback", event.ToolCall.Name)
	}
	if event.ToolCall.Arguments != "{}" {
		t.Errorf("empty args = %q, want {}", event.ToolCall.Arguments)
	}
}

// Frames carrying nothing actionable must be skipped, not turned into empty
// events a consumer would have to filter.
func TestParseAgentServerMessageIgnoresUnknownArms(t *testing.T) {
	for name, payload := range map[string][]byte{
		"empty":                {},
		"checkpoint update":    nest(stringField(1, "state"), fieldAgentServerCheckpointUpdate),
		"kv server message":    nest(varintField(1, 3), fieldAgentServerKvMessage),
		"unhandled update arm": nest(stringField(1, "x"), 1, 9),
		"non-mcp exec arm":     nest(stringField(1, "x"), fieldAgentServerExecMessage, 2),
	} {
		t.Run(name, func(t *testing.T) {
			event, err := ParseAgentServerMessage(payload)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if event != nil {
				t.Errorf("event = %+v, want nil", event)
			}
		})
	}
}

func TestParseAgentServerMessageRejectsMalformedPayload(t *testing.T) {
	// A length prefix that runs past the end of the buffer.
	if _, err := ParseAgentServerMessage([]byte{0x0a, 0x7f, 0x01}); err == nil {
		t.Error("expected an error for a truncated message")
	}
}

func TestConnectCodeToHTTPStatus(t *testing.T) {
	for code, want := range map[string]int{
		"unauthenticated":         http.StatusUnauthorized,
		"permission_denied":       http.StatusForbidden,
		"resource_exhausted":      http.StatusTooManyRequests,
		"PERMISSION_DENIED":       http.StatusForbidden,
		"internal":                http.StatusBadGateway,
		"unavailable":             http.StatusBadGateway,
		"context_length_exceeded": http.StatusBadGateway,
		"":                        http.StatusBadGateway,
	} {
		if got := ConnectCodeToHTTPStatus(code); got != want {
			t.Errorf("ConnectCodeToHTTPStatus(%q) = %d, want %d", code, got, want)
		}
	}
}

func TestParseAgentTrailer(t *testing.T) {
	t.Run("clean ends", func(t *testing.T) {
		for name, payload := range map[string]string{
			"empty":        "",
			"empty object": "{}",
			"null error":   `{"error":null}`,
			"no error key": `{"metadata":{}}`,
		} {
			if got := ParseAgentTrailer([]byte(payload)); got != nil {
				t.Errorf("%s: got %+v, want nil", name, got)
			}
		}
	})

	t.Run("coded error", func(t *testing.T) {
		got := ParseAgentTrailer([]byte(`{"error":{"code":"unauthenticated","message":"bad token"}}`))
		if got == nil {
			t.Fatal("expected an error")
		}
		if got.Code != "unauthenticated" || got.Message != "bad token" {
			t.Errorf("code/message = %q/%q", got.Code, got.Message)
		}
		if got.HTTPStatus != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", got.HTTPStatus)
		}
		if got.Raw == "" {
			t.Error("raw error json must be preserved for diagnostics")
		}
	})

	t.Run("non-json body is surfaced not dropped", func(t *testing.T) {
		got := ParseAgentTrailer([]byte("Update Required"))
		if got == nil {
			t.Fatal("expected an error")
		}
		if got.Raw != "Update Required" {
			t.Errorf("raw = %q", got.Raw)
		}
		if got.HTTPStatus != http.StatusBadGateway {
			t.Errorf("status = %d, want 502", got.HTTPStatus)
		}
	})
}

func TestAgentErrorMessage(t *testing.T) {
	err := &AgentError{Code: "permission_denied", Message: "client too old", HTTPStatus: 403}
	if got := err.Error(); got == "" {
		t.Fatal("error string must not be empty")
	}
	var nilErr *AgentError
	if got := nilErr.Error(); got != "" {
		t.Errorf("nil AgentError.Error() = %q, want empty", got)
	}
}

func TestAgentEventTypeString(t *testing.T) {
	for typ, want := range map[AgentEventType]string{
		AgentEventText:      "text",
		AgentEventThinking:  "thinking",
		AgentEventToolCall:  "tool_call",
		AgentEventTurnEnded: "turn_ended",
		AgentEventError:     "error",
	} {
		if got := typ.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(typ), got, want)
		}
	}
}
