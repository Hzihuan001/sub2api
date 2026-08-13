package cursor

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Request-side encoding for agent.v1.AgentService/Run.
//
// One logical turn is not one message: the CLI splits it across a dozen frames
// on an open request stream, paced over several seconds. BuildRunFrameSequence
// produces that plan; agent_stream.go drives it.
//
// Field numbers below are named after the decompiled agent.v1 schema
// (0xlane/reverse-cursor-agent, docs/proto/agent_v1.proto) and the byte layout
// matches the working capture in pleaseai/shunt. Where the two differ this file
// follows the capture, because the schema lists many fields the live server does
// not actually tolerate being absent or present.

// AgentMode values (UserMessage.mode). The full enum also has DEBUG=4,
// TRIAGE=5, PROJECT=6 and MULTITASK=7; only the three a gateway turn can map
// onto are named.
const (
	AgentModeAgent int32 = 1
	AgentModeAsk   int32 = 2
	AgentModePlan  int32 = 3
)

const (
	// AgentDefaultModel is the "let Cursor choose" model id, the agent-protocol
	// equivalent of api2's "auto".
	AgentDefaultModel = "default"

	// AgentDefaultCwd is the working directory reported to the upstream when
	// the caller has none. A gateway request has no real workspace, but the
	// environment block is not optional, so a plausible path is sent.
	AgentDefaultCwd = "/tmp"

	// AgentMCPProvider is the synthetic MCP server name bridged tools are
	// grouped under (McpToolDefinition.provider_identifier).
	AgentMCPProvider = "sub2api"
)

// Pacing for the request frame sequence. The delays are load-bearing, not
// politeness: half-closing the request stream before the server has streamed
// its answer makes it fail the turn with "internal: No exec result", so the
// marker frames are dribbled out and heartbeats keep the stream open after.
const (
	AgentDelayAfterRunRequest = 1500 * time.Millisecond
	AgentDelayAfterContext    = 800 * time.Millisecond
	AgentDelayAfterMarker     = 400 * time.Millisecond
	AgentHeartbeatInterval    = 5 * time.Second
)

// agentKvAckCount is how many numbered KV acknowledgement frames the captured
// client sends after the unnumbered one.
const agentKvAckCount = 8

// Protobuf field numbers, grouped by message.
const (
	// AgentClientMessage (oneof message)
	fieldAgentClientRunRequest         = 1
	fieldAgentClientExecMessage        = 2
	fieldAgentClientKvMessage          = 3
	fieldAgentClientExecControlMessage = 5
	fieldAgentClientHeartbeat          = 7

	// AgentRunRequest
	fieldAgentRunConversationState       = 1
	fieldAgentRunAction                  = 2
	fieldAgentRunMcpTools                = 4
	fieldAgentRunConversationID          = 5
	fieldAgentRunCustomSystemPrompt      = 8
	fieldAgentRunRequestedModel          = 9
	fieldAgentRunExcludeWorkspaceContext = 12
	fieldAgentRunSelectedSubagentModels  = 14
	fieldAgentRunConversationGroupID     = 16

	// ConversationAction (oneof action) / UserMessageAction
	fieldAgentActionUserMessage        = 1
	fieldAgentUserMessageActionMessage = 1
	fieldAgentUserMessageActionContext = 2

	// UserMessage
	fieldAgentUserMessageText    = 1
	fieldAgentUserMessageID      = 2
	fieldAgentUserMessageContext = 3
	fieldAgentUserMessageMode    = 4

	// RequestedModel / its nested parameter entry
	fieldAgentModelID         = 1
	fieldAgentModelMaxMode    = 2
	fieldAgentModelParameters = 3
	fieldAgentModelParamID    = 1
	fieldAgentModelParamValue = 2

	// McpTools / McpToolDefinition
	fieldAgentMcpToolsList       = 1
	fieldAgentMcpToolName        = 1
	fieldAgentMcpToolDescription = 2
	fieldAgentMcpToolInputSchema = 3
	fieldAgentMcpToolProvider    = 4
	fieldAgentMcpToolToolName    = 5

	// SelectedContext / SelectedImage
	fieldAgentSelectedImages         = 1
	fieldAgentSelectedImageUUID      = 2
	fieldAgentSelectedImagePath      = 3
	fieldAgentSelectedImageDimension = 4
	fieldAgentSelectedImageMimeType  = 7
	fieldAgentSelectedImageData      = 8
	fieldAgentSelectedImageDimWidth  = 1
	fieldAgentSelectedImageDimHeight = 2

	// ExecClientMessage / RequestContextResult / RequestContextSuccess / RequestContext
	fieldAgentExecRequestContextResult = 10
	fieldAgentRequestContextSuccess    = 1
	fieldAgentRequestContextInner      = 1
	fieldAgentRequestContextEnv        = 4

	// RequestContextEnv
	fieldAgentEnvOSVersion         = 1
	fieldAgentEnvWorkspacePaths    = 2
	fieldAgentEnvShell             = 3
	fieldAgentEnvSandboxSupported  = 14
	fieldAgentEnvSandboxNetDefault = 16
	fieldAgentEnvComputerUse       = 19
	fieldAgentEnvWorkingDirIsHome  = 20
	fieldAgentEnvProcessWorkingDir = 21
	fieldAgentEnvTimeZone          = 10
	fieldAgentEnvProjectFolder     = 11
	fieldAgentEnvUnknown22         = 22

	// ExecClientControlMessage / KvClientMessage
	fieldAgentExecControlStreamClose = 1
	fieldAgentKvID                   = 1
	fieldAgentKvSetBlobResult        = 3

	// google.protobuf.Value (oneof kind) and its Struct/ListValue wrappers
	fieldProtobufValueNull    = 1
	fieldProtobufValueNumber  = 2
	fieldProtobufValueString  = 3
	fieldProtobufValueBool    = 4
	fieldProtobufValueStruct  = 5
	fieldProtobufValueList    = 6
	fieldProtobufStructFields = 1
	fieldProtobufMapKey       = 1
	fieldProtobufMapValue     = 2
	fieldProtobufListValues   = 1
)

// AgentTool is one client tool advertised to Cursor as a native MCP tool. The
// model invokes it on the exec channel, which agent_response.go surfaces as an
// AgentEventToolCall.
type AgentTool struct {
	Name        string
	Description string
	// InputSchema is the tool's JSON Schema as a decoded Go value — what
	// json.Unmarshal into `any` produces. Unlike api2's mcp_tools, which take
	// the schema as a JSON *string*, agent.v1 wants it as a
	// google.protobuf.Value, so the schema is re-encoded rather than embedded.
	InputSchema any
	// ProviderIdentifier groups the tool under an MCP server name. Empty means
	// AgentMCPProvider.
	ProviderIdentifier string
}

// AgentImage is one inline image attached to the user message. Data is the raw
// (already base64-decoded) image bytes.
type AgentImage struct {
	Data     []byte
	MimeType string
	Path     string
	// UUID identifies the image within the turn; empty mints a fresh one.
	UUID string
	// Width and Height are optional pixel dimensions. They are left out of the
	// captured client's request entirely, and the published schema does not
	// spell out the nested Dimension message, so the {width=1,height=2} layout
	// used here is carried over from api2's ImageProto and is unverified for
	// agent.v1. Leaving both at zero reproduces the captured bytes exactly.
	Width  int32
	Height int32
}

// AgentEnv is the environment block reported in the second request frame.
type AgentEnv struct {
	// OSVersion is RequestContextEnv.os_version (default "linux").
	OSVersion string
	// Shell is the reported login shell (default "bash").
	Shell string
	// TimeZone is an IANA zone name (default "UTC").
	TimeZone string
	// Cwd fills workspace_paths, project_folder and process_working_directory.
	Cwd string
}

func (e AgentEnv) resolved(cwd string) AgentEnv {
	if e.OSVersion == "" {
		e.OSVersion = "linux"
	}
	if e.Shell == "" {
		e.Shell = "bash"
	}
	if e.TimeZone == "" {
		e.TimeZone = DefaultTimezone
	}
	if e.Cwd == "" {
		e.Cwd = cwd
	}
	if e.Cwd == "" {
		e.Cwd = AgentDefaultCwd
	}
	return e
}

// AgentRunParams describes one agent turn. Zero values are filled in by
// resolved(), so the minimum viable turn is just a Prompt.
type AgentRunParams struct {
	Prompt  string
	Model   string
	MaxMode bool
	// SystemPrompt becomes AgentRunRequest.custom_system_prompt.
	SystemPrompt string
	// Mode is an AgentMode value; zero means AgentModeAgent.
	Mode   int32
	Tools  []AgentTool
	Images []AgentImage
	// ConversationID is reused as the conversation *group* id, as the CLI does.
	// Empty mints a fresh uuid.
	ConversationID string
	Cwd            string
	// MessageID pins the user message uuid. Empty mints a fresh one; setting it
	// makes the encoded frames deterministic.
	MessageID string
	// Env overrides the environment block; its own zero fields get defaults.
	Env AgentEnv
}

func (p AgentRunParams) resolved() AgentRunParams {
	if p.Model == "" {
		p.Model = AgentDefaultModel
	}
	if p.Mode == 0 {
		p.Mode = AgentModeAgent
	}
	if p.ConversationID == "" {
		p.ConversationID = uuid.NewString()
	}
	if p.MessageID == "" {
		p.MessageID = uuid.NewString()
	}
	if p.Cwd == "" {
		p.Cwd = AgentDefaultCwd
	}
	p.Env = p.Env.resolved(p.Cwd)
	return p
}

// FramePlan is one request frame plus how long to wait before sending the next.
// Payload is the bare protobuf message; the caller wraps it with EncodeFrame.
type FramePlan struct {
	// Label names the frame for logging. It is not sent.
	Label      string
	Payload    []byte
	DelayAfter time.Duration
}

// BuildRunFrameSequence returns the ordered, paced request frames for one turn:
// the run request, the environment context, then the marker frames the captured
// client emits before it goes quiet. The caller keeps the stream open afterwards
// with EncodeClientHeartbeat every AgentHeartbeatInterval.
//
// The whole sequence is reproduced because it is what a live account is known to
// accept. The trailing stream-close and KV acknowledgement frames look like
// acknowledgements of server messages that never arrive on a fresh turn, so they
// may well be droppable — but that is untested against the real upstream, so
// they stay until a probe proves otherwise.
func BuildRunFrameSequence(p AgentRunParams) []FramePlan {
	p = p.resolved()

	plans := make([]FramePlan, 0, 4+agentKvAckCount)
	plans = append(plans,
		FramePlan{
			Label:      "run_request",
			Payload:    EncodeAgentRunRequest(p),
			DelayAfter: AgentDelayAfterRunRequest,
		},
		FramePlan{
			Label:      "request_context_env",
			Payload:    EncodeRequestContextEnvFrame(p.Env),
			DelayAfter: AgentDelayAfterContext,
		},
		FramePlan{
			Label:      "exec_stream_close",
			Payload:    EncodeStreamClose(),
			DelayAfter: AgentDelayAfterMarker,
		},
		FramePlan{
			Label:      "kv_set_blob_ack",
			Payload:    EncodeKvSetBlobAck(0),
			DelayAfter: AgentDelayAfterMarker,
		},
	)
	for id := uint32(1); id <= agentKvAckCount; id++ {
		plans = append(plans, FramePlan{
			Label:      fmt.Sprintf("kv_set_blob_ack#%d", id),
			Payload:    EncodeKvSetBlobAck(id),
			DelayAfter: AgentDelayAfterMarker,
		})
	}
	return plans
}

// EncodeAgentRunRequest encodes the first frame: an AgentClientMessage carrying
// the AgentRunRequest. The result is the bare protobuf body, without envelope
// framing.
func EncodeAgentRunRequest(p AgentRunParams) []byte {
	p = p.resolved()

	var req Writer
	// conversation_state=1 is an empty placeholder: a fresh turn has no prior
	// state, but the captured client still emits the field.
	req.WriteString(fieldAgentRunConversationState, "")
	req.WriteMessage(fieldAgentRunAction, encodeAgentConversationAction(p))
	// mcp_tools=4 is written even with no tools. An empty McpTools serializes
	// to zero bytes, so this is byte-identical to the placeholder a text-only
	// turn sends — the field is observably expected either way.
	req.WriteBytes(fieldAgentRunMcpTools, encodeAgentMcpTools(p.Tools))
	req.WriteString(fieldAgentRunConversationID, p.ConversationID)
	if p.SystemPrompt != "" {
		req.WriteString(fieldAgentRunCustomSystemPrompt, p.SystemPrompt)
	}
	req.WriteMessage(fieldAgentRunRequestedModel, encodeAgentRequestedModel(p.Model, p.MaxMode))
	// exclude_workspace_context=12 must be present and zero. Setting it to 1
	// makes the server reject the turn with "workspace context exclusion is not
	// allowed".
	req.WriteInt32(fieldAgentRunExcludeWorkspaceContext, 0)
	// A minimal subagent-model catalog: the bare "default" entry followed by
	// the model this turn actually wants.
	req.WriteMessage(fieldAgentRunSelectedSubagentModels, encodeAgentModelID(AgentDefaultModel))
	req.WriteMessage(fieldAgentRunSelectedSubagentModels, encodeAgentRequestedModel(p.Model, p.MaxMode))
	req.WriteString(fieldAgentRunConversationGroupID, p.ConversationID)

	var msg Writer
	msg.WriteMessage(fieldAgentClientRunRequest, req.Bytes())
	return msg.Bytes()
}

// encodeAgentConversationAction builds ConversationAction.user_message_action →
// UserMessageAction.user_message → UserMessage.
//
// UserMessageAction.request_context=2 is deliberately left empty: the
// environment travels in its own frame instead (EncodeRequestContextEnvFrame),
// which is what the captured client does.
func encodeAgentConversationAction(p AgentRunParams) []byte {
	var um Writer
	um.WriteString(fieldAgentUserMessageText, p.Prompt)
	um.WriteString(fieldAgentUserMessageID, p.MessageID)
	if ctx := encodeAgentSelectedContext(p.Images); len(ctx) > 0 {
		um.WriteBytes(fieldAgentUserMessageContext, ctx)
	} else {
		// An empty selected_context is still sent, matching a text-only turn.
		um.WriteString(fieldAgentUserMessageContext, "")
	}
	um.WriteInt32(fieldAgentUserMessageMode, p.Mode)

	var action Writer
	action.WriteMessage(fieldAgentUserMessageActionMessage, um.Bytes())

	var conv Writer
	conv.WriteMessage(fieldAgentActionUserMessage, action.Bytes())
	return conv.Bytes()
}

// encodeAgentModelID encodes a bare model reference (model_id only).
func encodeAgentModelID(model string) []byte {
	var w Writer
	w.WriteString(fieldAgentModelID, model)
	return w.Bytes()
}

// encodeAgentRequestedModel encodes RequestedModel. The "fast" parameter is
// always present and false, as in the capture; max_mode is only written when
// asked for, so the default request stays byte-identical to the known-good one.
func encodeAgentRequestedModel(model string, maxMode bool) []byte {
	var w Writer
	w.WriteString(fieldAgentModelID, model)
	if maxMode {
		w.WriteBool(fieldAgentModelMaxMode, true)
	}
	var param Writer
	param.WriteString(fieldAgentModelParamID, "fast")
	param.WriteString(fieldAgentModelParamValue, "false")
	w.WriteMessage(fieldAgentModelParameters, param.Bytes())
	return w.Bytes()
}

// encodeAgentMcpTools encodes the McpTools wrapper body. No tools means zero
// bytes, which is intentional — see EncodeAgentRunRequest.
func encodeAgentMcpTools(tools []AgentTool) []byte {
	var w Writer
	for _, tool := range tools {
		w.WriteMessage(fieldAgentMcpToolsList, encodeAgentMcpToolDefinition(tool))
	}
	return w.Bytes()
}

func encodeAgentMcpToolDefinition(tool AgentTool) []byte {
	var w Writer
	w.WriteString(fieldAgentMcpToolName, tool.Name)
	w.WriteString(fieldAgentMcpToolDescription, tool.Description)
	w.WriteBytes(fieldAgentMcpToolInputSchema, encodeProtobufValue(tool.InputSchema))
	provider := tool.ProviderIdentifier
	if provider == "" {
		provider = AgentMCPProvider
	}
	w.WriteString(fieldAgentMcpToolProvider, provider)
	// tool_name is the name the model calls; it matches name for bridged tools.
	w.WriteString(fieldAgentMcpToolToolName, tool.Name)
	return w.Bytes()
}

// encodeAgentSelectedContext encodes SelectedContext.selected_images. It returns
// nil when there is nothing to attach so the caller can emit the empty
// placeholder instead.
func encodeAgentSelectedContext(images []AgentImage) []byte {
	var w Writer
	for _, img := range images {
		if len(img.Data) == 0 {
			continue
		}
		w.WriteMessage(fieldAgentSelectedImages, encodeAgentSelectedImage(img))
	}
	return w.Bytes()
}

func encodeAgentSelectedImage(img AgentImage) []byte {
	var w Writer
	id := img.UUID
	if id == "" {
		id = uuid.NewString()
	}
	w.WriteString(fieldAgentSelectedImageUUID, id)
	if img.Path != "" {
		w.WriteString(fieldAgentSelectedImagePath, img.Path)
	}
	if img.Width > 0 && img.Height > 0 {
		var dim Writer
		dim.WriteInt32(fieldAgentSelectedImageDimWidth, img.Width)
		dim.WriteInt32(fieldAgentSelectedImageDimHeight, img.Height)
		w.WriteMessage(fieldAgentSelectedImageDimension, dim.Bytes())
	}
	if img.MimeType != "" {
		w.WriteString(fieldAgentSelectedImageMimeType, img.MimeType)
	}
	// data=8 carries the bytes inline. The alternative in the oneof is a
	// blob_id, which would require a separate upload round trip.
	w.WriteBytes(fieldAgentSelectedImageData, img.Data)
	return w.Bytes()
}

// EncodeRequestContextEnvFrame encodes the second frame: the environment block,
// delivered as if it were the client's answer to a request-context exec call.
// The nesting is AgentClientMessage.exec_client_message →
// ExecClientMessage.request_context_result → RequestContextResult.success →
// RequestContextSuccess.request_context → RequestContext.env.
func EncodeRequestContextEnvFrame(env AgentEnv) []byte {
	env = env.resolved("")

	var e Writer
	e.WriteString(fieldAgentEnvOSVersion, env.OSVersion)
	e.WriteString(fieldAgentEnvWorkspacePaths, env.Cwd)
	e.WriteString(fieldAgentEnvShell, env.Shell)
	e.WriteString(fieldAgentEnvTimeZone, env.TimeZone)
	e.WriteString(fieldAgentEnvProjectFolder, env.Cwd)
	// The capability flags are written explicitly, including the false ones:
	// they are proto3 `optional`, so presence is observable and the captured
	// client sends all of them.
	e.WriteBool(fieldAgentEnvSandboxSupported, true)
	e.WriteBool(fieldAgentEnvSandboxNetDefault, true)
	e.WriteBool(fieldAgentEnvComputerUse, false)
	e.WriteBool(fieldAgentEnvWorkingDirIsHome, false)
	e.WriteString(fieldAgentEnvProcessWorkingDir, env.Cwd)
	// Field 22 is absent from the published RequestContextEnv (which stops at
	// 21) yet the captured client sends it as a zero varint. Kept for fidelity.
	e.WriteInt32(fieldAgentEnvUnknown22, 0)

	var rc Writer
	rc.WriteMessage(fieldAgentRequestContextEnv, e.Bytes())

	var success Writer
	success.WriteMessage(fieldAgentRequestContextInner, rc.Bytes())

	var result Writer
	result.WriteMessage(fieldAgentRequestContextSuccess, success.Bytes())

	var exec Writer
	exec.WriteMessage(fieldAgentExecRequestContextResult, result.Bytes())

	var msg Writer
	msg.WriteMessage(fieldAgentClientExecMessage, exec.Bytes())
	return msg.Bytes()
}

// EncodeClientHeartbeat encodes the keep-alive frame sent every
// AgentHeartbeatInterval once the fixed sequence has been written. ClientHeartbeat
// is an empty message, so this is a bare tag with a zero length.
func EncodeClientHeartbeat() []byte {
	var w Writer
	w.WriteBytes(fieldAgentClientHeartbeat, nil)
	return w.Bytes()
}

// EncodeStreamClose encodes ExecClientControlMessage.stream_close — an empty
// StreamClose, i.e. "no exec stream is open on my side". It is one of the marker
// frames the captured client sends before going quiet.
func EncodeStreamClose() []byte {
	var ctrl Writer
	ctrl.WriteBytes(fieldAgentExecControlStreamClose, nil)

	var w Writer
	w.WriteMessage(fieldAgentClientExecControlMessage, ctrl.Bytes())
	return w.Bytes()
}

// EncodeKvSetBlobAck encodes KvClientMessage.set_blob_result, an empty
// SetBlobResult (no error). id 0 omits the id field, matching the first such
// frame the captured client sends; ids 1..8 follow.
func EncodeKvSetBlobAck(id uint32) []byte {
	var kv Writer
	if id != 0 {
		kv.WriteInt64(fieldAgentKvID, int64(id))
	}
	kv.WriteBytes(fieldAgentKvSetBlobResult, nil)

	var w Writer
	w.WriteMessage(fieldAgentClientKvMessage, kv.Bytes())
	return w.Bytes()
}

// WriteDouble appends a protobuf double: fixed64 wire type, little-endian IEEE
// 754. google.protobuf.Value.number_value is the only place this package needs
// it, so it lives here rather than in proto.go.
func (w *Writer) WriteDouble(field int, v float64) {
	w.WriteTag(field, wireFixed64)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
	w.buf = append(w.buf, b[:]...)
}

// encodeProtobufValue encodes a JSON-shaped Go value as a google.protobuf.Value.
// This is what McpToolDefinition.input_schema expects: the schema travels as a
// structured Value, not as JSON text.
//
// Accepted inputs are what json.Unmarshal into `any` produces (nil, bool,
// float64, string, []any, map[string]any), plus json.Number and the Go integer
// and float types, so a hand-built schema does not have to round-trip through
// JSON first. Anything else encodes as null rather than being dropped, which
// keeps the field present and the message well-formed.
func encodeProtobufValue(v any) []byte {
	var w Writer
	switch value := v.(type) {
	case nil:
		w.WriteInt32(fieldProtobufValueNull, 0)
	case bool:
		w.WriteBool(fieldProtobufValueBool, value)
	case string:
		w.WriteString(fieldProtobufValueString, value)
	case json.Number:
		f, err := value.Float64()
		if err != nil {
			// A number literal too large for float64 has no lossless Value
			// representation; keep the text rather than silently rounding.
			w.WriteString(fieldProtobufValueString, value.String())
			break
		}
		w.WriteDouble(fieldProtobufValueNumber, f)
	case float64:
		w.WriteDouble(fieldProtobufValueNumber, value)
	case float32:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case int:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case int8:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case int16:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case int32:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case int64:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case uint:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case uint8:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case uint16:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case uint32:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case uint64:
		w.WriteDouble(fieldProtobufValueNumber, float64(value))
	case map[string]any:
		w.WriteBytes(fieldProtobufValueStruct, encodeProtobufStruct(value))
	case []any:
		w.WriteBytes(fieldProtobufValueList, encodeProtobufList(value))
	default:
		w.WriteInt32(fieldProtobufValueNull, 0)
	}
	return w.Bytes()
}

// encodeProtobufStruct encodes google.protobuf.Struct's map<string, Value>.
// Keys are sorted so the same schema always produces the same bytes.
func encodeProtobufStruct(fields map[string]any) []byte {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var w Writer
	for _, key := range keys {
		var entry Writer
		entry.WriteString(fieldProtobufMapKey, key)
		entry.WriteBytes(fieldProtobufMapValue, encodeProtobufValue(fields[key]))
		w.WriteMessage(fieldProtobufStructFields, entry.Bytes())
	}
	return w.Bytes()
}

// encodeProtobufList encodes google.protobuf.ListValue's repeated Value.
func encodeProtobufList(items []any) []byte {
	var w Writer
	for _, item := range items {
		w.WriteBytes(fieldProtobufListValues, encodeProtobufValue(item))
	}
	return w.Bytes()
}
