package cursor

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// mustDecode decodes a protobuf body or fails the test. Every assertion below
// walks the encoded bytes back down the same nesting the encoder built, so a
// renumbered field shows up as a missing leaf rather than as a silent pass.
func mustDecode(t *testing.T, data []byte) Fields {
	t.Helper()
	fields, err := Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return fields
}

// descend walks a chain of nested single-occurrence message fields.
func descend(t *testing.T, data []byte, path ...int) Fields {
	t.Helper()
	fields := mustDecode(t, data)
	for i, field := range path {
		next := fields.Bytes(field)
		if next == nil {
			t.Fatalf("path %v: field %d missing at step %d", path, field, i)
		}
		fields = mustDecode(t, next)
	}
	return fields
}

func testRunParams() AgentRunParams {
	return AgentRunParams{
		Prompt:         "say hi",
		Model:          "gpt-5.6-sol-high",
		Mode:           AgentModeAgent,
		ConversationID: "conv-fixed",
		MessageID:      "msg-fixed",
		Cwd:            "/workspace",
	}
}

func TestEncodeAgentRunRequestCarriesPromptAndModel(t *testing.T) {
	payload := EncodeAgentRunRequest(testRunParams())

	// AgentClientMessage.run_request = 1
	run := descend(t, payload, fieldAgentClientRunRequest)

	if !run.Has(fieldAgentRunConversationState) {
		t.Error("conversation_state placeholder is missing")
	}
	if got := run.String(fieldAgentRunConversationID); got != "conv-fixed" {
		t.Errorf("conversation_id = %q, want %q", got, "conv-fixed")
	}
	if got := run.String(fieldAgentRunConversationGroupID); got != "conv-fixed" {
		t.Errorf("conversation_group_id = %q, want %q", got, "conv-fixed")
	}
	// exclude_workspace_context must be present and zero: a 1 makes the server
	// answer "workspace context exclusion is not allowed".
	if !run.Has(fieldAgentRunExcludeWorkspaceContext) {
		t.Error("exclude_workspace_context must be present")
	}
	if got := run.Varint(fieldAgentRunExcludeWorkspaceContext); got != 0 {
		t.Errorf("exclude_workspace_context = %d, want 0", got)
	}

	// action=2 -> user_message_action=1 -> user_message=1
	um := descend(t, run.Bytes(fieldAgentRunAction),
		fieldAgentActionUserMessage, fieldAgentUserMessageActionMessage)
	if got := um.String(fieldAgentUserMessageText); got != "say hi" {
		t.Errorf("user message text = %q, want %q", got, "say hi")
	}
	if got := um.String(fieldAgentUserMessageID); got != "msg-fixed" {
		t.Errorf("message_id = %q, want %q", got, "msg-fixed")
	}
	if got := um.Int32(fieldAgentUserMessageMode); got != AgentModeAgent {
		t.Errorf("mode = %d, want %d", got, AgentModeAgent)
	}
	if !um.Has(fieldAgentUserMessageContext) {
		t.Error("selected_context placeholder is missing")
	}

	model := mustDecode(t, run.Bytes(fieldAgentRunRequestedModel))
	if got := model.String(fieldAgentModelID); got != "gpt-5.6-sol-high" {
		t.Errorf("requested_model.model_id = %q", got)
	}
	if model.Has(fieldAgentModelMaxMode) {
		t.Error("max_mode must be omitted when not requested")
	}
	param := mustDecode(t, model.Bytes(fieldAgentModelParameters))
	if id, value := param.String(fieldAgentModelParamID), param.String(fieldAgentModelParamValue); id != "fast" || value != "false" {
		t.Errorf(`parameters = {%q: %q}, want {"fast": "false"}`, id, value)
	}

	// The subagent catalog is the bare "default" entry plus the target model.
	catalog := run.AllBytes(fieldAgentRunSelectedSubagentModels)
	if len(catalog) != 2 {
		t.Fatalf("selected_subagent_models entries = %d, want 2", len(catalog))
	}
	if got := mustDecode(t, catalog[0]).String(fieldAgentModelID); got != AgentDefaultModel {
		t.Errorf("first subagent model = %q, want %q", got, AgentDefaultModel)
	}
	if got := mustDecode(t, catalog[1]).String(fieldAgentModelID); got != "gpt-5.6-sol-high" {
		t.Errorf("second subagent model = %q", got)
	}
}

func TestEncodeAgentRunRequestDefaultsAndOptionals(t *testing.T) {
	run := descend(t, EncodeAgentRunRequest(AgentRunParams{Prompt: "hi"}), fieldAgentClientRunRequest)

	if got := run.String(fieldAgentRunConversationID); got == "" {
		t.Error("a blank conversation id must be filled with a fresh uuid")
	}
	if got := mustDecode(t, run.Bytes(fieldAgentRunRequestedModel)).String(fieldAgentModelID); got != AgentDefaultModel {
		t.Errorf("default model = %q, want %q", got, AgentDefaultModel)
	}
	um := descend(t, run.Bytes(fieldAgentRunAction),
		fieldAgentActionUserMessage, fieldAgentUserMessageActionMessage)
	if got := um.Int32(fieldAgentUserMessageMode); got != AgentModeAgent {
		t.Errorf("default mode = %d, want AGENT(%d)", got, AgentModeAgent)
	}
	if run.Has(fieldAgentRunCustomSystemPrompt) {
		t.Error("custom_system_prompt must be omitted when there is none")
	}
}

func TestEncodeAgentRunRequestSystemPromptAndMaxMode(t *testing.T) {
	params := testRunParams()
	params.SystemPrompt = "be terse"
	params.MaxMode = true

	run := descend(t, EncodeAgentRunRequest(params), fieldAgentClientRunRequest)
	if got := run.String(fieldAgentRunCustomSystemPrompt); got != "be terse" {
		t.Errorf("custom_system_prompt = %q", got)
	}
	if !mustDecode(t, run.Bytes(fieldAgentRunRequestedModel)).Bool(fieldAgentModelMaxMode) {
		t.Error("max_mode must be set when requested")
	}
}

// The empty McpTools wrapper has to be byte-identical to the empty-string
// placeholder a text-only turn sends: the field is observably expected even
// with no tools, and dropping it changes the request.
func TestEncodeAgentRunRequestEmptyToolsMatchPlaceholder(t *testing.T) {
	if body := encodeAgentMcpTools(nil); len(body) != 0 {
		t.Fatalf("empty McpTools body = %d bytes, want 0", len(body))
	}

	var withTools, placeholder Writer
	withTools.WriteBytes(fieldAgentRunMcpTools, encodeAgentMcpTools(nil))
	placeholder.WriteString(fieldAgentRunMcpTools, "")
	if !reflect.DeepEqual(withTools.Bytes(), placeholder.Bytes()) {
		t.Errorf("empty mcp_tools = % x, want % x", withTools.Bytes(), placeholder.Bytes())
	}

	run := descend(t, EncodeAgentRunRequest(testRunParams()), fieldAgentClientRunRequest)
	if !run.Has(fieldAgentRunMcpTools) {
		t.Error("mcp_tools must be present even with no tools")
	}
}

func TestEncodeAgentRunRequestEncodesTools(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"location": map[string]any{"type": "string"}},
		"required":   []any{"location"},
	}
	params := testRunParams()
	params.Tools = []AgentTool{{
		Name:        "get_weather",
		Description: "Get the weather.",
		InputSchema: schema,
	}}

	run := descend(t, EncodeAgentRunRequest(params), fieldAgentClientRunRequest)
	tools := mustDecode(t, run.Bytes(fieldAgentRunMcpTools)).AllBytes(fieldAgentMcpToolsList)
	if len(tools) != 1 {
		t.Fatalf("mcp tools = %d, want 1", len(tools))
	}

	def := mustDecode(t, tools[0])
	if got := def.String(fieldAgentMcpToolName); got != "get_weather" {
		t.Errorf("tool name = %q", got)
	}
	if got := def.String(fieldAgentMcpToolToolName); got != "get_weather" {
		t.Errorf("tool_name = %q", got)
	}
	if got := def.String(fieldAgentMcpToolDescription); got != "Get the weather." {
		t.Errorf("description = %q", got)
	}
	if got := def.String(fieldAgentMcpToolProvider); got != AgentMCPProvider {
		t.Errorf("provider_identifier = %q, want %q", got, AgentMCPProvider)
	}
	// input_schema travels as a google.protobuf.Value, not as JSON text.
	if got := decodeProtobufValue(def.Bytes(fieldAgentMcpToolInputSchema)); !reflect.DeepEqual(got, schema) {
		t.Errorf("input_schema round trip = %#v, want %#v", got, schema)
	}
}

func TestEncodeAgentRunRequestEncodesInlineImage(t *testing.T) {
	params := testRunParams()
	params.Images = []AgentImage{{
		Data:     []byte{0x89, 'P', 'N', 'G'},
		MimeType: "image/png",
		Path:     "shot.png",
		UUID:     "img-1",
	}}

	run := descend(t, EncodeAgentRunRequest(params), fieldAgentClientRunRequest)
	um := descend(t, run.Bytes(fieldAgentRunAction),
		fieldAgentActionUserMessage, fieldAgentUserMessageActionMessage)
	images := mustDecode(t, um.Bytes(fieldAgentUserMessageContext)).AllBytes(fieldAgentSelectedImages)
	if len(images) != 1 {
		t.Fatalf("selected_images = %d, want 1", len(images))
	}

	img := mustDecode(t, images[0])
	if got := img.String(fieldAgentSelectedImageUUID); got != "img-1" {
		t.Errorf("image uuid = %q", got)
	}
	if got := img.String(fieldAgentSelectedImageMimeType); got != "image/png" {
		t.Errorf("mime_type = %q", got)
	}
	if got := img.Bytes(fieldAgentSelectedImageData); !reflect.DeepEqual(got, params.Images[0].Data) {
		t.Errorf("inline data = % x, want % x", got, params.Images[0].Data)
	}
	if img.Has(fieldAgentSelectedImageDimension) {
		t.Error("dimension must stay out unless width and height are given")
	}
}

func TestEncodeRequestContextEnvFrame(t *testing.T) {
	payload := EncodeRequestContextEnvFrame(AgentEnv{Cwd: "/workspace"})

	// exec_client_message=2 -> request_context_result=10 -> success=1 ->
	// request_context=1 -> env=4
	env := descend(t, payload,
		fieldAgentClientExecMessage,
		fieldAgentExecRequestContextResult,
		fieldAgentRequestContextSuccess,
		fieldAgentRequestContextInner,
		fieldAgentRequestContextEnv,
	)

	for _, tc := range []struct {
		name  string
		field int
		want  string
	}{
		{"os_version", fieldAgentEnvOSVersion, "linux"},
		{"workspace_paths", fieldAgentEnvWorkspacePaths, "/workspace"},
		{"shell", fieldAgentEnvShell, "bash"},
		{"time_zone", fieldAgentEnvTimeZone, "UTC"},
		{"project_folder", fieldAgentEnvProjectFolder, "/workspace"},
		{"process_working_directory", fieldAgentEnvProcessWorkingDir, "/workspace"},
	} {
		if got := env.String(tc.field); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
	if !env.Bool(fieldAgentEnvSandboxSupported) {
		t.Error("sandbox_supported must be true")
	}
	// The false flags are written explicitly: they are proto3 optional, so
	// presence is observable and the captured client sends them.
	if !env.Has(fieldAgentEnvComputerUse) || env.Bool(fieldAgentEnvComputerUse) {
		t.Error("computer_use_supported must be present and false")
	}
	if !env.Has(fieldAgentEnvWorkingDirIsHome) || env.Bool(fieldAgentEnvWorkingDirIsHome) {
		t.Error("is_working_dir_home_dir must be present and false")
	}
}

func TestEncodeMarkerFrames(t *testing.T) {
	heartbeat := mustDecode(t, EncodeClientHeartbeat())
	if !heartbeat.Has(fieldAgentClientHeartbeat) {
		t.Error("heartbeat frame must set client_heartbeat")
	}
	if body := heartbeat.Bytes(fieldAgentClientHeartbeat); len(body) != 0 {
		t.Errorf("ClientHeartbeat is an empty message, got %d bytes", len(body))
	}

	ctrl := descend(t, EncodeStreamClose(), fieldAgentClientExecControlMessage)
	if !ctrl.Has(fieldAgentExecControlStreamClose) {
		t.Error("stream close frame must set exec_client_control_message.stream_close")
	}

	unnumbered := descend(t, EncodeKvSetBlobAck(0), fieldAgentClientKvMessage)
	if unnumbered.Has(fieldAgentKvID) {
		t.Error("id 0 must be omitted, matching the first captured KV frame")
	}
	if !unnumbered.Has(fieldAgentKvSetBlobResult) {
		t.Error("KV frame must set set_blob_result")
	}
	numbered := descend(t, EncodeKvSetBlobAck(3), fieldAgentClientKvMessage)
	if got := numbered.Varint(fieldAgentKvID); got != 3 {
		t.Errorf("KV id = %d, want 3", got)
	}
}

func TestBuildRunFrameSequencePacing(t *testing.T) {
	plans := BuildRunFrameSequence(testRunParams())

	if len(plans) != 4+agentKvAckCount {
		t.Fatalf("frame count = %d, want %d", len(plans), 4+agentKvAckCount)
	}
	wantLabels := []string{"run_request", "request_context_env", "exec_stream_close", "kv_set_blob_ack"}
	for i, want := range wantLabels {
		if plans[i].Label != want {
			t.Errorf("frame %d label = %q, want %q", i, plans[i].Label, want)
		}
	}
	if plans[0].DelayAfter != AgentDelayAfterRunRequest {
		t.Errorf("run_request delay = %s, want %s", plans[0].DelayAfter, AgentDelayAfterRunRequest)
	}
	if plans[1].DelayAfter != AgentDelayAfterContext {
		t.Errorf("context delay = %s, want %s", plans[1].DelayAfter, AgentDelayAfterContext)
	}
	for i := 2; i < len(plans); i++ {
		if plans[i].DelayAfter != AgentDelayAfterMarker {
			t.Errorf("frame %d delay = %s, want %s", i, plans[i].DelayAfter, AgentDelayAfterMarker)
		}
		if len(plans[i].Payload) == 0 {
			t.Errorf("frame %d has an empty payload", i)
		}
	}

	// Every payload must survive envelope framing as an uncompressed data frame.
	for i, plan := range plans {
		frame := EncodeFrame(plan.Payload, false)
		if frame[0] != 0 {
			t.Errorf("frame %d flag = 0x%02x, want 0x00", i, frame[0])
		}
		if len(frame) != len(plan.Payload)+5 {
			t.Errorf("frame %d length = %d, want %d", i, len(frame), len(plan.Payload)+5)
		}
	}
}

// The sequence must be stable across calls once the ids are pinned, so a retry
// cannot silently change the request.
func TestBuildRunFrameSequenceIsDeterministic(t *testing.T) {
	first := BuildRunFrameSequence(testRunParams())
	second := BuildRunFrameSequence(testRunParams())
	for i := range first {
		if !reflect.DeepEqual(first[i].Payload, second[i].Payload) {
			t.Fatalf("frame %d (%s) differs between builds", i, first[i].Label)
		}
	}
}

func TestEncodeProtobufValueRoundTrip(t *testing.T) {
	value := map[string]any{
		"str":    "two",
		"num":    3.5,
		"yes":    true,
		"no":     false,
		"nil":    nil,
		"list":   []any{1.0, "a", false, nil},
		"nested": map[string]any{"deep": map[string]any{"leaf": "end"}},
	}
	if got := decodeProtobufValue(encodeProtobufValue(value)); !reflect.DeepEqual(got, value) {
		t.Errorf("round trip = %#v, want %#v", got, value)
	}
}

func TestEncodeProtobufValueScalars(t *testing.T) {
	for name, tc := range map[string]struct {
		in   any
		want any
	}{
		"nil":         {nil, nil},
		"bool":        {true, true},
		"string":      {"hello", "hello"},
		"float64":     {1.25, 1.25},
		"int":         {int(7), 7.0},
		"int64":       {int64(-9), -9.0},
		"uint32":      {uint32(11), 11.0},
		"json.Number": {json.Number("42.5"), 42.5},
		"unsupported": {struct{ A int }{1}, nil},
	} {
		if got := decodeProtobufValue(encodeProtobufValue(tc.in)); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: round trip = %#v, want %#v", name, got, tc.want)
		}
	}
}

// Struct keys are sorted so the same schema always frames to the same bytes.
func TestEncodeProtobufValueSortsStructKeys(t *testing.T) {
	value := map[string]any{"z": "1", "a": "2", "m": "3"}
	first := encodeProtobufValue(value)
	for i := 0; i < 8; i++ {
		if !reflect.DeepEqual(encodeProtobufValue(value), first) {
			t.Fatal("struct encoding is not deterministic")
		}
	}
}

// A number literal wider than float64 keeps its text rather than being rounded
// into a different value.
func TestEncodeProtobufValueKeepsOversizedNumberAsText(t *testing.T) {
	huge := json.Number("1e999")
	if got := decodeProtobufValue(encodeProtobufValue(huge)); got != "1e999" {
		t.Errorf("oversized number = %#v, want the literal text", got)
	}
}

func TestDecodeProtobufValueBoundsRecursion(t *testing.T) {
	value := any("leaf")
	for i := 0; i < maxProtobufValueDepth+16; i++ {
		value = []any{value}
	}
	// Decoding must terminate at the cap instead of overflowing the stack.
	decoded := decodeProtobufValue(encodeProtobufValue(value))
	depth := 0
	for {
		list, ok := decoded.([]any)
		if !ok || len(list) == 0 {
			break
		}
		decoded = list[0]
		depth++
	}
	if depth > maxProtobufValueDepth {
		t.Errorf("decoded depth = %d, want at most %d", depth, maxProtobufValueDepth)
	}
}

func TestWriteDoubleUsesFixed64LittleEndian(t *testing.T) {
	var w Writer
	w.WriteDouble(2, math.Pi)
	fields := mustDecode(t, w.Bytes())
	if got := math.Float64frombits(fields.Varint(2)); got != math.Pi {
		t.Errorf("double round trip = %v, want %v", got, math.Pi)
	}
}
