package cursor

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
)

// Response-side decoding for agent.v1.AgentService/Run.
//
// Every data frame is an AgentServerMessage. Only two of its oneof arms carry
// anything a gateway needs: interaction_update (the assistant's output) and
// exec_server_message (the model asking the client to run a tool). Frames that
// carry neither decode to a nil event and are skipped.

// Protobuf field numbers for the server-to-client messages.
const (
	// AgentServerMessage (oneof message)
	fieldAgentServerInteractionUpdate = 1
	fieldAgentServerExecMessage       = 2
	fieldAgentServerCheckpointUpdate  = 3
	fieldAgentServerKvMessage         = 4

	// InteractionUpdate (oneof message)
	fieldAgentUpdateTextDelta         = 1
	fieldAgentUpdateToolCallStarted   = 2
	fieldAgentUpdateThinkingDelta     = 4
	fieldAgentUpdateThinkingCompleted = 5
	fieldAgentUpdatePartialToolCall   = 7
	fieldAgentUpdateTokenDelta        = 8
	fieldAgentUpdateHeartbeat         = 13
	fieldAgentUpdateTurnEnded         = 14

	// TextDeltaUpdate / ThinkingDeltaUpdate
	fieldAgentDeltaText = 1

	// ThinkingCompletedUpdate
	fieldAgentThinkingDurationMs = 1

	// TokenDeltaUpdate
	fieldAgentTokenDeltaTokens = 1

	// ToolCallStartedUpdate / PartialToolCallUpdate
	fieldAgentToolCallStartedID   = 1
	fieldAgentPartialToolCallID   = 1
	fieldAgentPartialToolCallArgs = 3

	// TurnEndedUpdate
	fieldAgentTurnInputTokens      = 1
	fieldAgentTurnOutputTokens     = 2
	fieldAgentTurnCacheReadTokens  = 3
	fieldAgentTurnCacheWriteTokens = 4

	// ExecServerMessage.mcp_args and McpArgs
	fieldAgentExecMcpArgs       = 11
	fieldAgentMcpArgsName       = 1
	fieldAgentMcpArgsArgs       = 2
	fieldAgentMcpArgsToolCallID = 3
	fieldAgentMcpArgsProvider   = 4
	fieldAgentMcpArgsToolName   = 5
)

// AgentEventType classifies a decoded event from the agent stream.
type AgentEventType int

const (
	// AgentEventText is an assistant text delta.
	AgentEventText AgentEventType = iota
	// AgentEventThinking is a reasoning delta.
	AgentEventThinking
	// AgentEventThinkingEnd marks the end of a reasoning block. Usage carries
	// the duration in ThinkingDurationMs.
	AgentEventThinkingEnd
	// AgentEventToolCall is the model invoking one of our declared MCP tools.
	// It ends the turn: see AgentStream for why.
	AgentEventToolCall
	// AgentEventToolCallStarted announces one of Cursor's own agentic tools
	// (shell, read, edit...). Only the call id is decoded — the nested oneof
	// spans two dozen tool types this gateway does not service.
	AgentEventToolCallStarted
	// AgentEventToolCallArgs is an incremental fragment of a tool call's
	// arguments. Text holds the fragment, ToolCall.ID the call it belongs to.
	AgentEventToolCallArgs
	// AgentEventTokenDelta reports incremental token consumption. The count is
	// in Usage.OutputTokens.
	AgentEventTokenDelta
	// AgentEventHeartbeat is a server keep-alive.
	AgentEventHeartbeat
	// AgentEventTurnEnded is the clean end of the turn, with final usage.
	AgentEventTurnEnded
	// AgentEventError terminates the stream; Err is always set.
	AgentEventError
)

// String renders the event type for logs and probe output.
func (t AgentEventType) String() string {
	switch t {
	case AgentEventText:
		return "text"
	case AgentEventThinking:
		return "thinking"
	case AgentEventThinkingEnd:
		return "thinking_end"
	case AgentEventToolCall:
		return "tool_call"
	case AgentEventToolCallStarted:
		return "tool_call_started"
	case AgentEventToolCallArgs:
		return "tool_call_args"
	case AgentEventTokenDelta:
		return "token_delta"
	case AgentEventHeartbeat:
		return "heartbeat"
	case AgentEventTurnEnded:
		return "turn_ended"
	case AgentEventError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// AgentToolCall is a decoded native MCP tool invocation.
type AgentToolCall struct {
	// ID is McpArgs.tool_call_id.
	ID string
	// Name is tool_name when present, else name.
	Name string
	// Arguments is the decoded argument map re-serialized as JSON. Keys are
	// sorted (encoding/json sorts map keys), so it is stable.
	Arguments string
	// ProviderIdentifier is the MCP server the tool was declared under.
	ProviderIdentifier string
}

// AgentUsage is the token accounting reported at the end of a turn. It doubles
// as the carrier for the incremental token count and the thinking duration,
// which have no other home.
type AgentUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	// ThinkingDurationMs is only set on AgentEventThinkingEnd.
	ThinkingDurationMs int64
}

// AgentEvent is one decoded item from the agent stream.
type AgentEvent struct {
	Type AgentEventType
	// Text holds the delta for text, thinking and tool-call-argument events.
	Text string
	// ToolCall is set for the three tool-related event types.
	ToolCall *AgentToolCall
	// Usage is set for turn_ended, token_delta and thinking_end.
	Usage *AgentUsage
	// Err is set for AgentEventError.
	Err error
}

// AgentError is a Connect-level failure carried by the end-of-stream trailer.
type AgentError struct {
	// Code is the Connect error code, e.g. "unauthenticated".
	Code string
	// Message is the human-readable detail, when the upstream sent one.
	Message string
	// Raw is the trailer payload verbatim, kept for diagnostics when the shape
	// does not match.
	Raw string
	// HTTPStatus is the status a gateway should surface, per
	// ConnectCodeToHTTPStatus.
	HTTPStatus int
}

func (e *AgentError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("cursor agent error %s (%d): %s", e.Code, e.HTTPStatus, e.Message)
	case e.Code != "":
		return fmt.Sprintf("cursor agent error %s (%d)", e.Code, e.HTTPStatus)
	case e.Message != "":
		return "cursor agent error: " + e.Message
	case e.Raw != "":
		return "cursor agent error: " + e.Raw
	default:
		return "cursor agent error"
	}
}

// ConnectCodeToHTTPStatus maps a Connect error code onto the status a gateway
// should report. The three named codes preserve auth and quota semantics so a
// caller can react to them; everything else is an upstream failure the caller
// cannot act on, hence 502.
//
// The reference Rust client additionally maps "context_length_exceeded" to 400
// so its caller can compact and retry. That is deliberately not done here: this
// package has no opinion on retry policy, and the gateway layer that does can
// special-case the code from AgentError.Code.
func ConnectCodeToHTTPStatus(code string) int {
	switch strings.TrimSpace(strings.ToLower(code)) {
	case "unauthenticated":
		return http.StatusUnauthorized
	case "permission_denied":
		return http.StatusForbidden
	case "resource_exhausted":
		return http.StatusTooManyRequests
	default:
		return http.StatusBadGateway
	}
}

// ParseAgentTrailer decodes an end-of-stream frame (flag 0x02). It returns nil
// for a clean end — an empty body, "{}", or JSON with no "error" member — and an
// AgentError otherwise. A body that is not JSON at all is reported as an error
// with only Raw set, rather than being discarded.
func ParseAgentTrailer(payload []byte) *AgentError {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return &AgentError{Raw: trimmed, HTTPStatus: http.StatusBadGateway}
	}
	if len(body.Error) == 0 || string(body.Error) == "null" {
		return nil
	}
	var detail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body.Error, &detail)
	return &AgentError{
		Code:       detail.Code,
		Message:    detail.Message,
		Raw:        string(body.Error),
		HTTPStatus: ConnectCodeToHTTPStatus(detail.Code),
	}
}

// ParseAgentServerMessage decodes one data-frame payload into a single event.
// It returns (nil, nil) when the frame carries nothing a caller can act on, so
// callers can simply skip nil events.
func ParseAgentServerMessage(payload []byte) (*AgentEvent, error) {
	top, err := Decode(payload)
	if err != nil {
		return nil, err
	}
	if b := top.Bytes(fieldAgentServerInteractionUpdate); b != nil {
		return parseAgentInteractionUpdate(b)
	}
	if b := top.Bytes(fieldAgentServerExecMessage); b != nil {
		return parseAgentExecServerMessage(b)
	}
	return nil, nil
}

func parseAgentInteractionUpdate(data []byte) (*AgentEvent, error) {
	u, err := Decode(data)
	if err != nil {
		return nil, err
	}

	switch {
	case u.Has(fieldAgentUpdateTextDelta):
		text, err := decodeAgentDeltaText(u.Bytes(fieldAgentUpdateTextDelta))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{Type: AgentEventText, Text: text}, nil

	case u.Has(fieldAgentUpdateThinkingDelta):
		text, err := decodeAgentDeltaText(u.Bytes(fieldAgentUpdateThinkingDelta))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{Type: AgentEventThinking, Text: text}, nil

	case u.Has(fieldAgentUpdateThinkingCompleted):
		inner, err := Decode(u.Bytes(fieldAgentUpdateThinkingCompleted))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{
			Type:  AgentEventThinkingEnd,
			Usage: &AgentUsage{ThinkingDurationMs: inner.Int64(fieldAgentThinkingDurationMs)},
		}, nil

	case u.Has(fieldAgentUpdateToolCallStarted):
		inner, err := Decode(u.Bytes(fieldAgentUpdateToolCallStarted))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{
			Type:     AgentEventToolCallStarted,
			ToolCall: &AgentToolCall{ID: inner.String(fieldAgentToolCallStartedID)},
		}, nil

	case u.Has(fieldAgentUpdatePartialToolCall):
		inner, err := Decode(u.Bytes(fieldAgentUpdatePartialToolCall))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{
			Type:     AgentEventToolCallArgs,
			Text:     inner.String(fieldAgentPartialToolCallArgs),
			ToolCall: &AgentToolCall{ID: inner.String(fieldAgentPartialToolCallID)},
		}, nil

	case u.Has(fieldAgentUpdateTokenDelta):
		inner, err := Decode(u.Bytes(fieldAgentUpdateTokenDelta))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{
			Type:  AgentEventTokenDelta,
			Usage: &AgentUsage{OutputTokens: inner.Int64(fieldAgentTokenDeltaTokens)},
		}, nil

	case u.Has(fieldAgentUpdateHeartbeat):
		return &AgentEvent{Type: AgentEventHeartbeat}, nil

	case u.Has(fieldAgentUpdateTurnEnded):
		inner, err := Decode(u.Bytes(fieldAgentUpdateTurnEnded))
		if err != nil {
			return nil, err
		}
		return &AgentEvent{
			Type: AgentEventTurnEnded,
			Usage: &AgentUsage{
				InputTokens:      inner.Int64(fieldAgentTurnInputTokens),
				OutputTokens:     inner.Int64(fieldAgentTurnOutputTokens),
				CacheReadTokens:  inner.Int64(fieldAgentTurnCacheReadTokens),
				CacheWriteTokens: inner.Int64(fieldAgentTurnCacheWriteTokens),
			},
		}, nil
	}
	return nil, nil
}

// decodeAgentDeltaText unwraps a {text=1} delta message. TextDeltaUpdate and
// ThinkingDeltaUpdate share the layout.
func decodeAgentDeltaText(data []byte) (string, error) {
	inner, err := Decode(data)
	if err != nil {
		return "", err
	}
	return inner.String(fieldAgentDeltaText), nil
}

func parseAgentExecServerMessage(data []byte) (*AgentEvent, error) {
	exec, err := Decode(data)
	if err != nil {
		return nil, err
	}
	args := exec.Bytes(fieldAgentExecMcpArgs)
	if args == nil {
		// Every other exec arm asks the client to run one of Cursor's own
		// tools, which this gateway does not service.
		return nil, nil
	}
	call, err := parseAgentMcpArgs(args)
	if err != nil {
		return nil, err
	}
	return &AgentEvent{Type: AgentEventToolCall, ToolCall: call}, nil
}

// parseAgentMcpArgs decodes McpArgs into a tool call. tool_name(5) wins over
// name(1): the former is the tool as declared, the latter can be namespaced.
func parseAgentMcpArgs(data []byte) (*AgentToolCall, error) {
	f, err := Decode(data)
	if err != nil {
		return nil, err
	}
	call := &AgentToolCall{
		ID:                 f.String(fieldAgentMcpArgsToolCallID),
		Name:               f.String(fieldAgentMcpArgsName),
		ProviderIdentifier: f.String(fieldAgentMcpArgsProvider),
	}
	if toolName := f.String(fieldAgentMcpArgsToolName); toolName != "" {
		call.Name = toolName
	}

	// args is a map<string, google.protobuf.Value>: one length-delimited entry
	// per key, each {key=1, value=2}.
	args := make(map[string]any)
	for _, raw := range f.AllBytes(fieldAgentMcpArgsArgs) {
		entry, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		args[entry.String(fieldProtobufMapKey)] = decodeProtobufValue(entry.Bytes(fieldProtobufMapValue))
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("cursor: encode mcp tool arguments: %w", err)
	}
	call.Arguments = string(encoded)
	return call, nil
}

// maxProtobufValueDepth bounds google.protobuf.Value recursion so a hostile or
// malformed payload cannot overflow the stack. Nesting past the cap decodes to
// nil rather than failing the whole frame.
const maxProtobufValueDepth = 64

// decodeProtobufValue decodes a google.protobuf.Value into the Go shapes
// encoding/json produces: nil, bool, float64, string, []any, map[string]any.
func decodeProtobufValue(data []byte) any { return decodeProtobufValueAt(data, 0) }

func decodeProtobufValueAt(data []byte, depth int) any {
	if depth >= maxProtobufValueDepth {
		return nil
	}
	f, err := Decode(data)
	if err != nil {
		return nil
	}
	switch {
	case f.Has(fieldProtobufValueNull):
		return nil
	case f.Has(fieldProtobufValueNumber):
		// number_value is a fixed64 double; Decode parks the raw bits in Varint.
		return math.Float64frombits(f.Varint(fieldProtobufValueNumber))
	case f.Has(fieldProtobufValueString):
		return f.String(fieldProtobufValueString)
	case f.Has(fieldProtobufValueBool):
		return f.Bool(fieldProtobufValueBool)
	case f.Has(fieldProtobufValueStruct):
		return decodeProtobufStructAt(f.Bytes(fieldProtobufValueStruct), depth+1)
	case f.Has(fieldProtobufValueList):
		return decodeProtobufListAt(f.Bytes(fieldProtobufValueList), depth+1)
	default:
		return nil
	}
}

func decodeProtobufStructAt(data []byte, depth int) any {
	f, err := Decode(data)
	if err != nil {
		return nil
	}
	out := make(map[string]any)
	for _, raw := range f.AllBytes(fieldProtobufStructFields) {
		entry, err := Decode(raw)
		if err != nil {
			continue
		}
		out[entry.String(fieldProtobufMapKey)] = decodeProtobufValueAt(entry.Bytes(fieldProtobufMapValue), depth)
	}
	return out
}

func decodeProtobufListAt(data []byte, depth int) any {
	f, err := Decode(data)
	if err != nil {
		return nil
	}
	raws := f.AllBytes(fieldProtobufListValues)
	out := make([]any, 0, len(raws))
	for _, raw := range raws {
		out = append(out, decodeProtobufValueAt(raw, depth))
	}
	return out
}
