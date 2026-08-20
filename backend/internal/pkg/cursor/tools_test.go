package cursor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// decodeChatRequest unwraps StreamUnifiedChatRequestWithTools -> the inner
// StreamUnifiedChatRequest fields.
func decodeChatRequest(t *testing.T, body []byte) Fields {
	t.Helper()
	top, err := Decode(body)
	require.NoError(t, err)
	inner := top.Bytes(fieldReqWithToolsRequest)
	require.NotNil(t, inner)
	chat, err := Decode(inner)
	require.NoError(t, err)
	return chat
}

// unpackEnums reads a packed repeated-enum payload back into its values.
func unpackEnums(t *testing.T, payload []byte) []int32 {
	t.Helper()
	var out []int32
	for pos := 0; pos < len(payload); {
		v, n, err := ReadVarint(payload[pos:])
		require.NoError(t, err)
		pos += n
		out = append(out, int32(v))
	}
	return out
}

func TestEncodeChatRequestNativeTools(t *testing.T) {
	t.Parallel()
	req := ChatRequest{
		Model:           "claude-4.5-sonnet",
		IsAgentic:       true,
		UnifiedMode:     UnifiedModeAgent,
		UnifiedModeName: UnifiedModeNameAgent,
		SupportedTools:  []int32{ToolReadSemsearchFiles},
		MCPTools: []MCPTool{
			{Name: "get_weather", Description: "Get weather", Parameters: `{"type":"object"}`},
			{Name: "get_time", Server: "other"},
		},
		Messages: []ChatMessage{{
			Role:           RoleUser,
			Content:        "what is the weather",
			IsAgentic:      true,
			UnifiedMode:    UnifiedModeAgent,
			SupportedTools: []int32{ToolReadSemsearchFiles},
		}},
	}

	chat := decodeChatRequest(t, EncodeChatRequest(req))

	require.True(t, chat.Bool(fieldChatIsAgentic))
	require.Equal(t, UnifiedModeAgent, chat.Int32(fieldChatUnifiedMode))
	require.Equal(t, UnifiedModeNameAgent, chat.String(fieldChatUnifiedModeName))
	require.Equal(t, []int32{ToolReadSemsearchFiles}, unpackEnums(t, chat.Bytes(fieldChatSupportedTools)))

	tools := chat.AllBytes(fieldChatMCPTools)
	require.Len(t, tools, 2)

	first, err := Decode(tools[0])
	require.NoError(t, err)
	require.Equal(t, "get_weather", first.String(fieldMCPToolName))
	require.Equal(t, "Get weather", first.String(fieldMCPToolDesc))
	require.Equal(t, `{"type":"object"}`, first.String(fieldMCPToolParams))
	// An unset server falls back to the synthetic "custom" bucket.
	require.Equal(t, McpServerCustom, first.String(fieldMCPToolServer))

	second, err := Decode(tools[1])
	require.NoError(t, err)
	require.Equal(t, "get_time", second.String(fieldMCPToolName))
	require.Equal(t, "other", second.String(fieldMCPToolServer))
	require.False(t, second.Has(fieldMCPToolParams))

	msgs := chat.AllBytes(fieldChatMessages)
	require.Len(t, msgs, 1)
	msg, err := Decode(msgs[0])
	require.NoError(t, err)
	require.True(t, msg.Bool(fieldMsgIsAgentic))
	require.Equal(t, UnifiedModeAgent, msg.Int32(fieldMsgUnifiedMode))
	require.Equal(t, []int32{ToolReadSemsearchFiles}, unpackEnums(t, msg.Bytes(fieldMsgSupportedTools)))
}

func TestEncodeChatRequestOmitsToolFieldsWhenUnset(t *testing.T) {
	t.Parallel()
	chat := decodeChatRequest(t, EncodeChatRequest(ChatRequest{
		Model:    "gpt-5.2",
		Messages: []ChatMessage{{Role: RoleUser, Content: "hi"}},
	}))

	require.False(t, chat.Has(fieldChatMCPTools))
	require.False(t, chat.Has(fieldChatSupportedTools))
	require.False(t, chat.Has(fieldChatIsAgentic))
	require.False(t, chat.Has(fieldChatDisableTools))
	require.Equal(t, UnifiedModeChat, chat.Int32(fieldChatUnifiedMode))
}

func TestEncodeChatRequestMultipleSupportedTools(t *testing.T) {
	t.Parallel()
	chat := decodeChatRequest(t, EncodeChatRequest(ChatRequest{
		Model:          "gpt-5.2",
		SupportedTools: []int32{ToolReadSemsearchFiles, 0, ToolMCP, ToolCallMCPTool},
		Messages:       []ChatMessage{{Role: RoleUser, Content: "hi"}},
	}))

	// The UNSPECIFIED (0) member is dropped rather than encoded.
	require.Equal(t,
		[]int32{ToolReadSemsearchFiles, ToolMCP, ToolCallMCPTool},
		unpackEnums(t, chat.Bytes(fieldChatSupportedTools)),
	)
}

func TestEncodeChatRequestToolResults(t *testing.T) {
	t.Parallel()
	chat := decodeChatRequest(t, EncodeChatRequest(ChatRequest{
		Model: "claude-4.5-sonnet",
		Messages: []ChatMessage{{
			Role: RoleUser,
			ToolResults: []ToolResult{
				{CallID: "call_1", Name: "get_weather", Index: 0, RawArgs: `{"city":"SF"}`, Result: "18C"},
				{CallID: "call_2", Name: "get_time", Index: 1, Result: "12:00"},
			},
		}},
	}))

	msgs := chat.AllBytes(fieldChatMessages)
	require.Len(t, msgs, 1)
	msg, err := Decode(msgs[0])
	require.NoError(t, err)

	results := msg.AllBytes(fieldMsgToolResults)
	require.Len(t, results, 2)

	first, err := Decode(results[0])
	require.NoError(t, err)
	require.Equal(t, "call_1", first.String(fieldToolResultCallID))
	require.Equal(t, "get_weather", first.String(fieldToolResultName))
	require.Equal(t, int32(0), first.Int32(fieldToolResultIndex))
	require.Equal(t, `{"city":"SF"}`, first.String(fieldToolResultRawArgs))
	// The model-visible output lands in content=7, not the structured result=8.
	require.Equal(t, "18C", first.String(fieldToolResultContent))

	second, err := Decode(results[1])
	require.NoError(t, err)
	require.Equal(t, "call_2", second.String(fieldToolResultCallID))
	require.Equal(t, int32(1), second.Int32(fieldToolResultIndex))
	// Missing arguments still encode as a parseable empty object.
	require.Equal(t, "{}", second.String(fieldToolResultRawArgs))
	require.Equal(t, "12:00", second.String(fieldToolResultContent))
}

func TestEncodeChatRequestImages(t *testing.T) {
	t.Parallel()
	payload := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	chat := decodeChatRequest(t, EncodeChatRequest(ChatRequest{
		Model: "claude-4.5-sonnet",
		Messages: []ChatMessage{{
			Role:    RoleUser,
			Content: "what is in this image",
			Images: []Image{
				{Data: payload, Width: 320, Height: 200},
				{Data: []byte{0xff, 0xd8, 0xff}},
				{Data: nil, Width: 10, Height: 10}, // dropped: no payload
			},
		}},
	}))

	msgs := chat.AllBytes(fieldChatMessages)
	require.Len(t, msgs, 1)
	msg, err := Decode(msgs[0])
	require.NoError(t, err)
	require.Equal(t, "what is in this image", msg.String(fieldMsgContent))

	images := msg.AllBytes(fieldMsgImages)
	require.Len(t, images, 2)

	first, err := Decode(images[0])
	require.NoError(t, err)
	require.Equal(t, payload, first.Bytes(fieldImageData))
	dim, err := Decode(first.Bytes(fieldImageDimension))
	require.NoError(t, err)
	require.Equal(t, int32(320), dim.Int32(fieldImageDimWidth))
	require.Equal(t, int32(200), dim.Int32(fieldImageDimHeight))

	second, err := Decode(images[1])
	require.NoError(t, err)
	require.Equal(t, []byte{0xff, 0xd8, 0xff}, second.Bytes(fieldImageData))
	// Unknown dimensions are omitted rather than sent as 0x0.
	require.False(t, second.Has(fieldImageDimension))
}

func TestParseChatResponseFrameToolCallMCPParams(t *testing.T) {
	t.Parallel()
	var nested Writer
	nested.WriteString(fieldMCPParamsToolName, "get_weather")
	nested.WriteString(fieldMCPParamsToolArgs, `{"city":"SF"}`)
	var params Writer
	params.WriteMessage(fieldMCPParamsTools, nested.Bytes())

	var tc Writer
	tc.WriteString(fieldToolCallID, "call_1")
	tc.WriteString(fieldToolCallName, "mcp")
	tc.WriteMessage(fieldToolCallMCPParams, params.Bytes())
	tc.WriteBool(fieldToolCallIsLast, true)
	var top Writer
	top.WriteMessage(fieldRespToolCall, tc.Bytes())

	ev, err := ParseChatResponseFrame(top.Bytes())
	require.NoError(t, err)
	require.NotNil(t, ev)
	require.Equal(t, ChatEventToolCall, ev.Type)
	require.Equal(t, "call_1", ev.ToolCall.ID)
	// The nested MCP tool name/arguments win over the outer dispatch tool.
	require.Equal(t, "get_weather", ev.ToolCall.Name)
	require.Equal(t, `{"city":"SF"}`, ev.ToolCall.RawArgs)
	require.True(t, ev.ToolCall.ArgsComplete)
	require.True(t, ev.ToolCall.IsLast)
}

func TestParseChatResponseFrameToolCallMalformedMCPParamsFallsBack(t *testing.T) {
	t.Parallel()
	var tc Writer
	tc.WriteString(fieldToolCallID, "call_1")
	tc.WriteString(fieldToolCallName, "get_weather")
	tc.WriteString(fieldToolCallRawArgs, `{"city":`)
	// A payload that is not a decodable MCPParams envelope.
	tc.WriteBytes(fieldToolCallMCPParams, []byte{0xff, 0xff, 0xff})
	var top Writer
	top.WriteMessage(fieldRespToolCall, tc.Bytes())

	ev, err := ParseChatResponseFrame(top.Bytes())
	require.NoError(t, err)
	require.NotNil(t, ev)
	require.Equal(t, "get_weather", ev.ToolCall.Name)
	require.Equal(t, `{"city":`, ev.ToolCall.RawArgs)
	require.False(t, ev.ToolCall.ArgsComplete)
}

func TestParseChatResponseFrameToolCallStreamsRawArgs(t *testing.T) {
	t.Parallel()
	// Without an mcp_params envelope the raw_args fragment stays incremental.
	var tc Writer
	tc.WriteString(fieldToolCallID, "call_1")
	tc.WriteString(fieldToolCallRawArgs, `{"ci`)
	var top Writer
	top.WriteMessage(fieldRespToolCall, tc.Bytes())

	ev, err := ParseChatResponseFrame(top.Bytes())
	require.NoError(t, err)
	require.Equal(t, `{"ci`, ev.ToolCall.RawArgs)
	require.False(t, ev.ToolCall.ArgsComplete)
	require.False(t, ev.ToolCall.IsLast)
}
