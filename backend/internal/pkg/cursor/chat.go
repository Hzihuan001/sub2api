package cursor

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Enum values used by the chat protobuf messages.
const (
	// Message roles (ConversationMessage.type; MESSAGE_TYPE_HUMAN / _AI).
	RoleUser      int32 = 1
	RoleAssistant int32 = 2

	// Unified modes (StreamUnifiedChatRequest.unified_mode).
	UnifiedModeChat  int32 = 1
	UnifiedModeAgent int32 = 2

	// Unified mode labels (StreamUnifiedChatRequest.unified_mode_name).
	UnifiedModeNameAsk   = "Ask"
	UnifiedModeNameAgent = "Agent"

	// Thinking levels (StreamUnifiedChatRequest.thinking_level).
	ThinkingLevelUnspecified int32 = 0
	ThinkingLevelMedium      int32 = 1
	ThinkingLevelHigh        int32 = 2
)

// ClientSideToolV2 enum values, as carried by the repeated supported_tools
// fields. Only the handful we need are named; the full enum runs past 55.
// Values cross-checked between eisbaw/cursor_api_demo's extracted
// aiserver.v1 proto (cursor-grpc/server_full.proto, values 0..24) and its
// TASK-110 tool-enum mapping from a later bundle (values up to 55) — the two
// agree on every shared value.
const (
	// ToolReadSemsearchFiles is the value kaitranntt/ccs puts in
	// supported_tools to switch the request into tool-capable agent mode.
	ToolReadSemsearchFiles int32 = 1
	// ToolMCP is the legacy client-side MCP dispatch tool.
	ToolMCP int32 = 19
	// ToolCallMCPTool is the newer MCP dispatch tool.
	ToolCallMCPTool int32 = 49
)

// McpServerCustom is the synthetic MCP server name used for tools bridged in
// from an OpenAI-style `tools` array (matching kaitranntt/ccs).
const McpServerCustom = "custom"

// Protobuf field numbers for the chat request/response messages. Cross-checked
// against kaitranntt/ccs (cursor-protobuf-schema.ts, aligned to Cursor 2.6.22)
// and eisbaw/cursor_api_demo (cursor-grpc/server_full.proto, extracted from the
// Cursor bundle with original field names). Where the two disagree the named
// proto wins, since it carries the declared types as well as the numbers.
const (
	// StreamUnifiedChatRequestWithTools (top level)
	fieldReqWithToolsRequest = 1

	// StreamUnifiedChatRequest
	fieldChatMessages        = 1
	fieldChatInstruction     = 3
	fieldChatModel           = 5
	fieldChatConversationID  = 23
	fieldChatMetadata        = 26
	fieldChatIsAgentic       = 27
	fieldChatSupportedTools  = 29
	fieldChatMessageIDs      = 30
	fieldChatMCPTools        = 34
	fieldChatLargeContext    = 35
	fieldChatUnifiedMode     = 46
	fieldChatDisableTools    = 48
	fieldChatThinkingLevel   = 49
	fieldChatUnifiedModeName = 54

	// ConversationMessage
	fieldMsgContent        = 1
	fieldMsgImages         = 10
	fieldMsgID             = 13
	fieldMsgRole           = 2
	fieldMsgToolResults    = 18
	fieldMsgIsAgentic      = 29
	fieldMsgUnifiedMode    = 47
	fieldMsgSupportedTools = 51

	// ConversationToolResult. Field 8 is a ClientSideToolV2Result *message*,
	// not a string, so plain tool output goes into content=7 instead.
	fieldToolResultCallID  = 1
	fieldToolResultName    = 2
	fieldToolResultIndex   = 3
	fieldToolResultRawArgs = 5
	fieldToolResultContent = 7

	// MCPTool (StreamUnifiedChatRequest.mcp_tools)
	fieldMCPToolName   = 1
	fieldMCPToolDesc   = 2
	fieldMCPToolParams = 3
	fieldMCPToolServer = 4

	// ImageProto and its nested Dimension
	fieldImageData      = 1
	fieldImageDimension = 2
	fieldImageDimWidth  = 1
	fieldImageDimHeight = 2

	// Model (chat variant): name=1, empty=4
	fieldChatModelName  = 1
	fieldChatModelEmpty = 4

	// Instruction
	fieldInstructionText = 1

	// Metadata
	fieldMetaPlatform  = 1
	fieldMetaArch      = 2
	fieldMetaVersion   = 3
	fieldMetaCwd       = 4
	fieldMetaTimestamp = 5

	// MessageId
	fieldMsgIDID      = 1
	fieldMsgIDSummary = 2
	fieldMsgIDRole    = 3

	// StreamUnifiedChatResponseWithTools (response top level)
	fieldRespToolCall = 1
	fieldRespResponse = 2

	// StreamUnifiedChatResponse
	fieldChatRespText     = 1
	fieldChatRespThinking = 25

	// Thinking
	fieldThinkingText = 1

	// ClientSideToolV2Call
	fieldToolCallID        = 3
	fieldToolCallName      = 9
	fieldToolCallRawArgs   = 10
	fieldToolCallIsLast    = 11
	fieldToolCallMCPParams = 27

	// MCPParams (ClientSideToolV2Call.mcp_params) and its nested tool entry
	fieldMCPParamsTools    = 1
	fieldMCPParamsToolName = 1
	fieldMCPParamsToolArgs = 3
)

// ChatRequest is a clean, upstream-agnostic description of one chat turn. The
// encoder maps it onto the Cursor protobuf. Defaults target a plain-text CHAT
// request; the agent-oriented fields (IsAgentic, SupportedTools, MCPTools,
// MessageIDs, per-message tool results) opt into tool-capable AGENT mode.
type ChatRequest struct {
	// Model is the Cursor model id (e.g. "claude-4.5-sonnet").
	Model string
	// Messages is the ordered conversation.
	Messages []ChatMessage
	// SystemPrompt, when set, becomes the top-level Instruction.
	SystemPrompt string
	// ThinkingLevel is 0/1/2 (unspecified/medium/high).
	ThinkingLevel int32
	// LargeContext toggles the large-context flag.
	LargeContext bool
	// ConversationID is an optional stable id for the conversation.
	ConversationID string
	// Metadata is optional client metadata (platform/arch/version/cwd/ts).
	Metadata *Metadata

	// UnifiedMode defaults to CHAT (1) when zero. Set AGENT (2) for agentic use.
	UnifiedMode int32
	// UnifiedModeName is an optional human-readable mode label.
	UnifiedModeName string
	// IsAgentic enables the agentic flag (optional; default off).
	IsAgentic bool
	// ShouldDisableTools disables upstream tools (optional).
	ShouldDisableTools bool
	// SupportedTools lists the ClientSideToolV2 enum values the client can
	// service. Encoded as a packed repeated enum.
	SupportedTools []int32
	// MCPTools declares custom tools (name/description/JSON schema) that the
	// model may call. This is how an OpenAI-style `tools` array is bridged.
	MCPTools []MCPTool
	// MessageIDs is optional message-id metadata (summaries/roles).
	MessageIDs []MessageID
}

// ChatMessage is one conversation message. Content + Role are the essentials;
// the remaining fields support tool round-trips, images and agent mode.
type ChatMessage struct {
	Content        string
	Role           int32
	ID             string
	Images         []Image
	ToolResults    []ToolResult
	IsAgentic      bool
	UnifiedMode    int32
	SupportedTools []int32
}

// ToolResult carries the result of a prior tool call back to the model. It maps
// onto ConversationToolResult; Result lands in content=7 (optional string)
// because result=8 is a structured ClientSideToolV2Result message.
type ToolResult struct {
	CallID  string
	Name    string
	Index   int32
	RawArgs string
	Result  string
}

// MCPTool is one custom tool declaration (StreamUnifiedChatRequest.mcp_tools).
// Parameters is the tool's JSON Schema, serialized as a JSON string.
type MCPTool struct {
	Name        string
	Description string
	Parameters  string
	Server      string
}

// Image is one attached image (ConversationMessage.images / ImageProto). Data
// is the raw (already base64-decoded) image bytes; Width/Height are optional
// pixel dimensions.
type Image struct {
	Data   []byte
	Width  int32
	Height int32
}

// Metadata is optional client environment metadata.
type Metadata struct {
	Platform  string
	Arch      string
	Version   string
	Cwd       string
	Timestamp int64
}

// MessageID is per-message id metadata (id/summary/role).
type MessageID struct {
	ID      string
	Summary string
	Role    int32
}

// EncodeChatRequest encodes a ChatRequest into the StreamUnifiedChatRequest-
// WithTools protobuf. The result is the raw message body WITHOUT envelope
// framing — the caller wraps it via EncodeFrame before sending on the streaming
// endpoint.
func EncodeChatRequest(req ChatRequest) []byte {
	inner := encodeStreamUnifiedChatRequest(req)
	var w Writer
	w.WriteMessage(fieldReqWithToolsRequest, inner)
	return w.Bytes()
}

func encodeStreamUnifiedChatRequest(req ChatRequest) []byte {
	var w Writer

	for _, m := range req.Messages {
		w.WriteMessage(fieldChatMessages, encodeMessage(m))
	}

	if req.SystemPrompt != "" {
		var instr Writer
		instr.WriteString(fieldInstructionText, req.SystemPrompt)
		w.WriteMessage(fieldChatInstruction, instr.Bytes())
	}

	w.WriteMessage(fieldChatModel, encodeModel(req.Model))

	if req.ConversationID != "" {
		w.WriteString(fieldChatConversationID, req.ConversationID)
	}
	if req.Metadata != nil {
		w.WriteMessage(fieldChatMetadata, encodeMetadata(req.Metadata))
	}
	if req.IsAgentic {
		w.WriteBool(fieldChatIsAgentic, true)
	}
	if packed := packEnums(req.SupportedTools); len(packed) > 0 {
		w.WriteBytes(fieldChatSupportedTools, packed)
	}
	for _, id := range req.MessageIDs {
		w.WriteMessage(fieldChatMessageIDs, encodeMessageID(id))
	}
	for _, tool := range req.MCPTools {
		w.WriteMessage(fieldChatMCPTools, encodeMCPTool(tool))
	}
	if req.LargeContext {
		w.WriteBool(fieldChatLargeContext, true)
	}

	mode := req.UnifiedMode
	if mode == 0 {
		mode = UnifiedModeChat
	}
	w.WriteInt32(fieldChatUnifiedMode, mode)

	if req.ShouldDisableTools {
		w.WriteBool(fieldChatDisableTools, true)
	}
	if req.ThinkingLevel != 0 {
		w.WriteInt32(fieldChatThinkingLevel, req.ThinkingLevel)
	}
	if req.UnifiedModeName != "" {
		w.WriteString(fieldChatUnifiedModeName, req.UnifiedModeName)
	}
	return w.Bytes()
}

func encodeMessage(m ChatMessage) []byte {
	var w Writer
	if m.Content != "" {
		w.WriteString(fieldMsgContent, m.Content)
	}
	if m.Role != 0 {
		w.WriteInt32(fieldMsgRole, m.Role)
	}
	for _, img := range m.Images {
		if len(img.Data) == 0 {
			continue
		}
		w.WriteMessage(fieldMsgImages, encodeImage(img))
	}
	if m.ID != "" {
		w.WriteString(fieldMsgID, m.ID)
	}
	for _, tr := range m.ToolResults {
		w.WriteMessage(fieldMsgToolResults, encodeToolResult(tr))
	}
	if m.IsAgentic {
		w.WriteBool(fieldMsgIsAgentic, true)
	}
	if m.UnifiedMode != 0 {
		w.WriteInt32(fieldMsgUnifiedMode, m.UnifiedMode)
	}
	if packed := packEnums(m.SupportedTools); len(packed) > 0 {
		w.WriteBytes(fieldMsgSupportedTools, packed)
	}
	return w.Bytes()
}

func encodeToolResult(tr ToolResult) []byte {
	var w Writer
	if tr.CallID != "" {
		w.WriteString(fieldToolResultCallID, tr.CallID)
	}
	if tr.Name != "" {
		w.WriteString(fieldToolResultName, tr.Name)
	}
	if tr.Index != 0 {
		w.WriteInt32(fieldToolResultIndex, tr.Index)
	}
	// raw_args is the arguments the model produced for the call. Upstream
	// clients always send a JSON object here, so fall back to "{}".
	rawArgs := tr.RawArgs
	if rawArgs == "" {
		rawArgs = "{}"
	}
	w.WriteString(fieldToolResultRawArgs, rawArgs)
	if tr.Result != "" {
		w.WriteString(fieldToolResultContent, tr.Result)
	}
	return w.Bytes()
}

// encodeMCPTool encodes one MCPTool. Server defaults to McpServerCustom so the
// upstream groups every bridged tool under a single synthetic server.
func encodeMCPTool(tool MCPTool) []byte {
	var w Writer
	if tool.Name != "" {
		w.WriteString(fieldMCPToolName, tool.Name)
	}
	if tool.Description != "" {
		w.WriteString(fieldMCPToolDesc, tool.Description)
	}
	if tool.Parameters != "" {
		w.WriteString(fieldMCPToolParams, tool.Parameters)
	}
	server := tool.Server
	if server == "" {
		server = McpServerCustom
	}
	w.WriteString(fieldMCPToolServer, server)
	return w.Bytes()
}

// encodeImage encodes one ImageProto: raw bytes plus an optional Dimension.
// There is no mime-type field in ImageProto — the upstream sniffs the format
// from the payload's magic bytes.
//
// Provenance: ConversationMessage.images=10 and ImageProto{data=1,
// dimension=2{width=1,height=2}} come from the named aiserver.v1 proto in
// eisbaw/cursor_api_demo (cursor-grpc/server_full.proto), whose
// ConversationMessage field numbers match kaitranntt/ccs exactly on every
// field the two have in common (content=1, role=2, id=13, tool_results=18,
// is_agentic=29, unified_mode=47, supported_tools=51). No second source
// confirms the image path specifically, so treat it as unverified until an
// end-to-end run against a real account.
func encodeImage(img Image) []byte {
	var w Writer
	w.WriteBytes(fieldImageData, img.Data)
	if img.Width > 0 && img.Height > 0 {
		var dim Writer
		dim.WriteInt32(fieldImageDimWidth, img.Width)
		dim.WriteInt32(fieldImageDimHeight, img.Height)
		w.WriteMessage(fieldImageDimension, dim.Bytes())
	}
	return w.Bytes()
}

// packEnums encodes a repeated enum field's payload in packed form (the proto3
// default): a length-delimited run of varints. Zero values are dropped since
// they are the enum's UNSPECIFIED member.
func packEnums(values []int32) []byte {
	var w Writer
	for _, v := range values {
		if v == 0 {
			continue
		}
		w.WriteVarint(uint64(v))
	}
	return w.Bytes()
}

// encodeModel encodes the chat Model message. Only name=1 is written; the
// "empty" field=4 is an empty-string marker that proto3 omits, so it is left
// out. If integration finds it must be present, add w.WriteString(4, "").
func encodeModel(name string) []byte {
	var w Writer
	if name != "" {
		w.WriteString(fieldChatModelName, name)
	}
	return w.Bytes()
}

func encodeMetadata(m *Metadata) []byte {
	var w Writer
	if m.Platform != "" {
		w.WriteString(fieldMetaPlatform, m.Platform)
	}
	if m.Arch != "" {
		w.WriteString(fieldMetaArch, m.Arch)
	}
	if m.Version != "" {
		w.WriteString(fieldMetaVersion, m.Version)
	}
	if m.Cwd != "" {
		w.WriteString(fieldMetaCwd, m.Cwd)
	}
	if m.Timestamp != 0 {
		w.WriteInt64(fieldMetaTimestamp, m.Timestamp)
	}
	return w.Bytes()
}

func encodeMessageID(id MessageID) []byte {
	var w Writer
	if id.ID != "" {
		w.WriteString(fieldMsgIDID, id.ID)
	}
	if id.Summary != "" {
		w.WriteString(fieldMsgIDSummary, id.Summary)
	}
	if id.Role != 0 {
		w.WriteInt32(fieldMsgIDRole, id.Role)
	}
	return w.Bytes()
}

// ChatEventType classifies a decoded streaming event.
type ChatEventType int

const (
	// ChatEventText is an assistant text delta.
	ChatEventText ChatEventType = iota
	// ChatEventThinking is a reasoning/thinking delta.
	ChatEventThinking
	// ChatEventToolCall is a (possibly partial) tool-call delta.
	ChatEventToolCall
	// ChatEventEnd marks the end of the stream; Err is set on failure.
	ChatEventEnd
)

// ToolCall is a decoded ClientSideToolV2Call delta.
type ToolCall struct {
	ID      string
	Name    string
	RawArgs string
	IsLast  bool
	// ArgsComplete reports that RawArgs is the whole argument payload rather
	// than an incremental fragment, so consumers must replace instead of
	// append. Set when the call arrived via the mcp_params envelope, which
	// carries fully-formed arguments.
	ArgsComplete bool
}

// ChatEvent is one decoded item from the chat stream.
type ChatEvent struct {
	Type ChatEventType
	// Text holds the delta for ChatEventText / ChatEventThinking.
	Text string
	// ToolCall is set for ChatEventToolCall.
	ToolCall *ToolCall
	// Err is set for ChatEventEnd when the stream ended with an error.
	Err error
}

// StreamError is a structured upstream error carried by the end-of-stream
// trailer frame.
type StreamError struct {
	Code    string
	Message string
	// Raw is the raw JSON of the "error" field, kept for diagnostics when the
	// code/message shape does not match.
	Raw string
}

func (e *StreamError) Error() string {
	switch {
	case e == nil:
		return ""
	case e.Message != "" && e.Code != "":
		return fmt.Sprintf("cursor upstream error %s: %s", e.Code, e.Message)
	case e.Message != "":
		return "cursor upstream error: " + e.Message
	case e.Code != "":
		return "cursor upstream error: " + e.Code
	case e.Raw != "":
		return "cursor upstream error: " + e.Raw
	default:
		return "cursor upstream error"
	}
}

// ParseChatResponseFrame decodes one DATA frame payload (a
// StreamUnifiedChatResponseWithTools message) into a single ChatEvent. It
// returns (nil, nil) when the frame carries no user-visible delta (e.g. a
// keep-alive or metadata-only frame), so callers can simply skip nil events.
func ParseChatResponseFrame(payload []byte) (*ChatEvent, error) {
	top, err := Decode(payload)
	if err != nil {
		return nil, err
	}

	if b := top.Bytes(fieldRespToolCall); b != nil {
		tc, err := parseToolCall(b)
		if err != nil {
			return nil, err
		}
		return &ChatEvent{Type: ChatEventToolCall, ToolCall: tc}, nil
	}

	if b := top.Bytes(fieldRespResponse); b != nil {
		resp, err := Decode(b)
		if err != nil {
			return nil, err
		}
		if tb := resp.Bytes(fieldChatRespThinking); tb != nil {
			th, err := Decode(tb)
			if err != nil {
				return nil, err
			}
			return &ChatEvent{Type: ChatEventThinking, Text: th.String(fieldThinkingText)}, nil
		}
		if resp.Has(fieldChatRespText) {
			return &ChatEvent{Type: ChatEventText, Text: resp.String(fieldChatRespText)}, nil
		}
	}

	return nil, nil
}

func parseToolCall(data []byte) (*ToolCall, error) {
	f, err := Decode(data)
	if err != nil {
		return nil, err
	}
	// The id field can carry extra newline-separated context; Cursor clients
	// keep only the first line as the actual call id.
	id := f.String(fieldToolCallID)
	if nl := strings.IndexByte(id, '\n'); nl >= 0 {
		id = id[:nl]
	}
	call := &ToolCall{
		ID:      id,
		Name:    f.String(fieldToolCallName),
		RawArgs: f.String(fieldToolCallRawArgs),
		IsLast:  f.Bool(fieldToolCallIsLast),
	}
	// Tools declared through mcp_tools come back wrapped: the outer name is the
	// MCP dispatch tool, and the real name/arguments sit in mcp_params. Those
	// arguments arrive complete, so they replace anything streamed via
	// raw_args rather than extending it.
	if name, args, ok := parseToolCallMCPParams(f.Bytes(fieldToolCallMCPParams)); ok {
		if name != "" {
			call.Name = name
		}
		call.RawArgs = args
		call.ArgsComplete = true
	}
	return call, nil
}

// parseToolCallMCPParams unwraps ClientSideToolV2Call.mcp_params →
// MCPParams.tools[0] → {name, params}. A malformed envelope is reported as
// "not present" so the caller keeps the top-level name/raw_args.
func parseToolCallMCPParams(data []byte) (name, args string, ok bool) {
	if len(data) == 0 {
		return "", "", false
	}
	params, err := Decode(data)
	if err != nil {
		return "", "", false
	}
	entry := params.Bytes(fieldMCPParamsTools)
	if entry == nil {
		return "", "", false
	}
	tool, err := Decode(entry)
	if err != nil {
		return "", "", false
	}
	return tool.String(fieldMCPParamsToolName), tool.String(fieldMCPParamsToolArgs), true
}

// StreamDecoder turns a Connect frame stream into ChatEvents. It wraps a
// FrameReader: data frames become text/thinking/tool-call events, and the
// end-of-stream trailer frame (flag&0x02) becomes a ChatEventEnd whose Err is
// set when the trailer JSON reports an error.
type StreamDecoder struct {
	fr   *FrameReader
	done bool
}

// NewStreamDecoder builds a StreamDecoder reading Connect frames from r.
func NewStreamDecoder(r io.Reader) *StreamDecoder {
	return &StreamDecoder{fr: NewFrameReader(r)}
}

// Next returns the next event. After it returns a ChatEventEnd, subsequent
// calls return io.EOF. It also returns io.EOF if the stream closes without an
// explicit trailer frame.
func (sd *StreamDecoder) Next() (*ChatEvent, error) {
	if sd.done {
		return nil, io.EOF
	}
	for {
		frame, err := sd.fr.Next()
		if err != nil {
			if err == io.EOF {
				sd.done = true
			}
			return nil, err
		}
		if frame.EndStream {
			sd.done = true
			return parseEndStream(frame.Payload)
		}
		ev, err := ParseChatResponseFrame(frame.Payload)
		if err != nil {
			return nil, err
		}
		if ev == nil {
			continue // no delta in this frame; read the next one
		}
		return ev, nil
	}
}

// parseEndStream decodes the trailer frame JSON. An empty body or a body
// without an "error" field is a clean end; otherwise the error is surfaced as a
// StreamError.
func parseEndStream(payload []byte) (*ChatEvent, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" || trimmed == "{}" {
		return &ChatEvent{Type: ChatEventEnd}, nil
	}
	var body struct {
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		// Not valid JSON: treat the raw text as an error rather than dropping it.
		return &ChatEvent{Type: ChatEventEnd, Err: &StreamError{Raw: trimmed}}, nil
	}
	if len(body.Error) == 0 || string(body.Error) == "null" {
		return &ChatEvent{Type: ChatEventEnd}, nil
	}
	var detail struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body.Error, &detail)
	return &ChatEvent{Type: ChatEventEnd, Err: &StreamError{
		Code:    detail.Code,
		Message: detail.Message,
		Raw:     string(body.Error),
	}}, nil
}
