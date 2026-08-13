//go:build unit

package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

// cursorNativeOpts / cursorLegacyOpts pin the feature flags so tests do not
// depend on the process environment.
var (
	cursorNativeOpts = cursorTranslateOptions{nativeTools: true, nativeImages: true, cwd: cursorpkg.AgentDefaultCwd}
	cursorLegacyOpts = cursorTranslateOptions{cwd: cursorpkg.AgentDefaultCwd}
)

func TestBuildCursorAgentRunSingleUserMessageIsPlainPrompt(t *testing.T) {
	req := &apicompat.ChatCompletionsRequest{
		Model:    "claude-4.5-sonnet",
		Messages: []apicompat.ChatMessage{{Role: "user", Content: json.RawMessage(`"Hello"`)}},
	}

	params, input, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, cursorNativeOpts)
	require.NoError(t, err)

	// The common case reads as a plain prompt, with no synthetic role label.
	require.Equal(t, "Hello", params.Prompt)
	require.Empty(t, params.SystemPrompt)
	require.Equal(t, "claude-4.5-sonnet", params.Model)
	require.False(t, params.MaxMode)
	require.Equal(t, cursorpkg.AgentModeAgent, params.Mode)
	require.Equal(t, cursorpkg.AgentDefaultCwd, params.Cwd)
	// The stateless bridge never carries a prior conversation.
	require.Empty(t, params.ConversationID)
	require.Equal(t, "Hello", input.text)
	require.Zero(t, input.imageTokens)
}

func TestBuildCursorAgentRunFlattensHistoryWithRoleLabels(t *testing.T) {
	req := &apicompat.ChatCompletionsRequest{
		Model: "claude-4.5-sonnet",
		Messages: []apicompat.ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are helpful."`)},
			{Role: "user", Content: json.RawMessage(`"Hello"`)},
			{Role: "assistant", Content: json.RawMessage(`"Hi there"`)},
			{Role: "developer", Content: json.RawMessage(`"Be concise."`)},
			{Role: "user", Content: json.RawMessage(`"Next question"`)},
		},
	}

	params, input, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, cursorNativeOpts)
	require.NoError(t, err)

	// system/developer fold into custom_system_prompt, everything else into
	// one labelled transcript.
	require.Equal(t, "You are helpful.\n\nBe concise.", params.SystemPrompt)
	require.Equal(t, "User: Hello\n\nAssistant: Hi there\n\nUser: Next question", params.Prompt)

	require.Contains(t, input.text, "You are helpful.")
	require.Contains(t, input.text, "Hello")
	require.Contains(t, input.text, "Next question")
}

func TestBuildCursorAgentRunRendersToolRoundTripInTranscript(t *testing.T) {
	req := weatherToolRequest()
	req.Messages = append(req.Messages,
		apicompat.ChatMessage{
			Role: "assistant",
			ToolCalls: []apicompat.ChatToolCall{{
				ID: "call_1", Type: "function",
				Function: apicompat.ChatFunctionCall{Name: "get_weather", Arguments: `{"city":"SF"}`},
			}},
		},
		apicompat.ChatMessage{Role: "tool", ToolCallID: "call_1", Content: json.RawMessage(`"18C"`)},
		apicompat.ChatMessage{Role: "user", Content: json.RawMessage(`"thanks"`)},
	)

	params, input, err := buildCursorAgentRunParams("gpt-5.2", req, cursorNativeOpts)
	require.NoError(t, err)

	// The protocol has no field to replay a prior call, so the whole loop is
	// rendered as prose the model can read back.
	require.Equal(t, "User: call the tool\n\n"+
		"Assistant: [tool call] get_weather {\"city\":\"SF\"}\n\n"+
		"Tool result (call_1): 18C\n\n"+
		"User: thanks", params.Prompt)
	require.Contains(t, input.text, "18C")
	require.Contains(t, input.text, `{"city":"SF"}`)
	// The native tool schema is part of the billable input.
	require.Contains(t, input.text, "get_weather")
}

func weatherToolRequest() *apicompat.ChatCompletionsRequest {
	return &apicompat.ChatCompletionsRequest{
		Model: "gpt-5.2",
		Messages: []apicompat.ChatMessage{
			{Role: "user", Content: json.RawMessage(`"call the tool"`)},
		},
		Tools: []apicompat.ChatTool{{
			Type: "function",
			Function: &apicompat.ChatFunction{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters:  json.RawMessage(`{"type": "object", "properties": {"city": {"type": "string"}}}`),
			},
		}},
	}
}

func TestBuildCursorAgentRunDeclaresToolsNatively(t *testing.T) {
	req := weatherToolRequest()
	req.Tools = append(req.Tools, apicompat.ChatTool{
		Type:     "function",
		Function: &apicompat.ChatFunction{Name: "get_time"},
	})

	params, _, err := buildCursorAgentRunParams("gpt-5.2", req, cursorNativeOpts)
	require.NoError(t, err)

	require.Len(t, params.Tools, 2)
	require.Equal(t, "get_weather", params.Tools[0].Name)
	require.Equal(t, "Get weather", params.Tools[0].Description)
	// agent.v1 takes the schema as a structured value, not as JSON text.
	require.Equal(t, map[string]any{
		"type":       "object",
		"properties": map[string]any{"city": map[string]any{"type": "string"}},
	}, params.Tools[0].InputSchema)
	// A tool without parameters still gets a parseable object schema.
	require.Equal(t, "get_time", params.Tools[1].Name)
	require.Equal(t, map[string]any{"type": "object", "properties": map[string]any{}}, params.Tools[1].InputSchema)

	// Nothing about the tools leaks into the prompt on the native path.
	require.NotContains(t, params.SystemPrompt, "get_weather")
}

func TestBuildCursorAgentRunToolChoiceNoneDropsTools(t *testing.T) {
	req := weatherToolRequest()
	req.ToolChoice = json.RawMessage(`"none"`)

	params, _, err := buildCursorAgentRunParams("gpt-5.2", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Empty(t, params.Tools)
	require.Empty(t, params.SystemPrompt)
}

func TestBuildCursorAgentRunToolChoiceRequiredAddsNudge(t *testing.T) {
	req := weatherToolRequest()
	req.ToolChoice = json.RawMessage(`"required"`)

	params, _, err := buildCursorAgentRunParams("gpt-5.2", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Len(t, params.Tools, 1)
	require.Contains(t, params.SystemPrompt, "must call at least one of the available tools")
}

func TestBuildCursorAgentRunToolChoiceNamedFunction(t *testing.T) {
	req := weatherToolRequest()
	req.ToolChoice = json.RawMessage(`{"type":"function","function":{"name":"get_weather"}}`)

	params, _, err := buildCursorAgentRunParams("gpt-5.2", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Len(t, params.Tools, 1)
	require.Contains(t, params.SystemPrompt, "must call the tool `get_weather`")
}

func TestBuildCursorAgentRunLegacyFunctionsAndDedup(t *testing.T) {
	req := weatherToolRequest()
	req.Functions = []apicompat.ChatFunction{
		{Name: "get_weather", Description: "duplicate, dropped"},
		{Name: "legacy_fn", Description: "legacy"},
		{Name: "   "},
	}
	// A provider-native tool has no MCP equivalent and must be skipped.
	req.Tools = append(req.Tools, apicompat.ChatTool{Type: "x_search"})

	params, _, err := buildCursorAgentRunParams("gpt-5.2", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Len(t, params.Tools, 2)
	require.Equal(t, "get_weather", params.Tools[0].Name)
	require.Equal(t, "legacy_fn", params.Tools[1].Name)
}

func TestBuildCursorAgentRunFallsBackToToolInstruction(t *testing.T) {
	req := weatherToolRequest()
	req.ToolChoice = json.RawMessage(`"required"`)

	params, _, err := buildCursorAgentRunParams("gpt-5.2", req, cursorLegacyOpts)
	require.NoError(t, err)

	require.Empty(t, params.Tools)
	require.Contains(t, params.SystemPrompt, "get_weather")
	require.Contains(t, params.SystemPrompt, "Tool choice preference")
}

func TestBuildCursorAgentRunAttachesInlineImages(t *testing.T) {
	uri, raw := testPNGDataURI(t, 6, 3)
	req := &apicompat.ChatCompletionsRequest{
		Model: "claude-4.5-sonnet",
		Messages: []apicompat.ChatMessage{{
			Role: "user",
			Content: json.RawMessage(`[
				{"type":"text","text":"what is this"},
				{"type":"image_url","image_url":{"url":"` + uri + `"}}
			]`),
		}},
	}

	params, _, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Equal(t, "what is this", params.Prompt)
	require.Len(t, params.Images, 1)
	require.Equal(t, raw, params.Images[0].Data)
	require.Equal(t, "image/png", params.Images[0].MimeType)
	require.Equal(t, int32(6), params.Images[0].Width)
	require.Equal(t, int32(3), params.Images[0].Height)
}

func TestBuildCursorAgentRunImageOnlyMessageKeepsImage(t *testing.T) {
	uri, _ := testPNGDataURI(t, 2, 2)
	req := &apicompat.ChatCompletionsRequest{
		Model: "claude-4.5-sonnet",
		Messages: []apicompat.ChatMessage{{
			Role:    "user",
			Content: json.RawMessage(`[{"type":"image_url","image_url":{"url":"` + uri + `"}}]`),
		}},
	}

	params, _, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Empty(t, params.Prompt)
	require.Len(t, params.Images, 1)
}

func TestBuildCursorAgentRunRemoteImageDegradesToText(t *testing.T) {
	req := &apicompat.ChatCompletionsRequest{
		Model: "claude-4.5-sonnet",
		Messages: []apicompat.ChatMessage{{
			Role: "user",
			Content: json.RawMessage(`[
				{"type":"text","text":"describe"},
				{"type":"image_url","image_url":{"url":"https://example.com/x.png"}}
			]`),
		}},
	}

	params, _, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, cursorNativeOpts)
	require.NoError(t, err)
	require.Empty(t, params.Images)
	// Remote URLs are never fetched server-side; the model is told about them.
	require.Equal(t, "describe\n[image: https://example.com/x.png]", params.Prompt)
}

func TestBuildCursorAgentRunDropsImagesWhenNativeImagesOff(t *testing.T) {
	uri, _ := testPNGDataURI(t, 2, 2)
	req := &apicompat.ChatCompletionsRequest{
		Model: "claude-4.5-sonnet",
		Messages: []apicompat.ChatMessage{{
			Role: "user",
			Content: json.RawMessage(`[
				{"type":"text","text":"describe"},
				{"type":"image_url","image_url":{"url":"` + uri + `"}}
			]`),
		}},
	}

	params, _, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, cursorLegacyOpts)
	require.NoError(t, err)
	require.Empty(t, params.Images)
	require.Equal(t, "describe", params.Prompt)
}

func TestCursorAgentWireModel(t *testing.T) {
	observed := []string{"claude-4.5-sonnet", "claude-4.5-sonnet-thinking", "gpt-5"}

	// "auto" is api2's name for the same thing agent.v1 calls "default", which
	// is the only model a free account is served.
	for _, requested := range []string{"", "auto", "AUTO", "default"} {
		id, maxMode := cursorAgentWireModel(requested, "high", observed)
		require.Equal(t, cursorpkg.AgentDefaultModel, id, "requested %q", requested)
		require.False(t, maxMode)
	}

	id, maxMode := cursorAgentWireModel("claude-4.5-sonnet", "", observed)
	require.Equal(t, "claude-4.5-sonnet", id)
	require.False(t, maxMode)

	// Max mode is a flag on the wire, not part of the model id.
	id, maxMode = cursorAgentWireModel("claude-4.5-sonnet-max", "", observed)
	require.Equal(t, "claude-4.5-sonnet", id)
	require.True(t, maxMode)

	// Reasoning is requested by naming the "-thinking" variant.
	id, _ = cursorAgentWireModel("claude-4.5-sonnet", "high", observed)
	require.Equal(t, "claude-4.5-sonnet-thinking", id)

	// ...but only when the account has seen it upstream: an unknown model_id
	// fails the whole turn, which is worse than a non-reasoning answer.
	id, _ = cursorAgentWireModel("gpt-5", "high", observed)
	require.Equal(t, "gpt-5", id)
	id, _ = cursorAgentWireModel("claude-4.5-sonnet", "high", nil)
	require.Equal(t, "claude-4.5-sonnet", id)

	// An already-thinking model is not suffixed twice.
	id, _ = cursorAgentWireModel("claude-4.5-sonnet-thinking", "high", observed)
	require.Equal(t, "claude-4.5-sonnet-thinking", id)
}

func TestCursorAgentWantsThinking(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "xhigh", "MINIMAL"} {
		require.True(t, cursorAgentWantsThinking(effort), "effort %q", effort)
	}
	for _, effort := range []string{"", "  ", "none", "unknown"} {
		require.False(t, cursorAgentWantsThinking(effort), "effort %q", effort)
	}
}

func TestBuildCursorAgentRunUsesThinkingVariantFromReasoningEffort(t *testing.T) {
	req := &apicompat.ChatCompletionsRequest{
		Model:           "claude-4.5-sonnet",
		ReasoningEffort: "high",
		Messages:        []apicompat.ChatMessage{{Role: "user", Content: json.RawMessage(`"think"`)}},
	}
	opts := cursorNativeOpts
	opts.observedModels = []string{"claude-4.5-sonnet", "claude-4.5-sonnet-thinking"}

	params, _, err := buildCursorAgentRunParams("claude-4.5-sonnet", req, opts)
	require.NoError(t, err)
	require.Equal(t, "claude-4.5-sonnet-thinking", params.Model)
}

func TestBuildCursorAgentRunReadsThinkingVariantFromAccountObservedModels(t *testing.T) {
	account := cursorAccount()
	account.Extra = map[string]any{
		cursorObservedModelsExtraKey: map[string]any{
			"models":     []any{"claude-4.5-sonnet", "claude-4.5-sonnet-thinking"},
			"fetched_at": time.Now().UTC().Format(time.RFC3339),
		},
	}
	req := &apicompat.ChatCompletionsRequest{
		Model:           "claude-4.5-sonnet",
		ReasoningEffort: "high",
		Messages:        []apicompat.ChatMessage{{Role: "user", Content: json.RawMessage(`"think"`)}},
	}

	params, _, err := buildCursorAgentRun(account, "claude-4.5-sonnet", req)
	require.NoError(t, err)
	require.Equal(t, "claude-4.5-sonnet-thinking", params.Model)
	require.Equal(t, cursorpkg.AgentDefaultCwd, params.Cwd)

	// Without reasoning_effort the observed list is never consulted.
	req.ReasoningEffort = ""
	params, _, err = buildCursorAgentRun(account, "claude-4.5-sonnet", req)
	require.NoError(t, err)
	require.Equal(t, "claude-4.5-sonnet", params.Model)
}

func TestCursorAgentMessagePartsFlattensMultimodalParts(t *testing.T) {
	msg := apicompat.ChatMessage{
		Role: "user",
		Content: json.RawMessage(`[
			{"type":"text","text":"first"},
			{"type":"image_url","image_url":{"url":"https://example.com/x.png"}},
			{"type":"text","text":"second"}
		]`),
	}
	text, images := cursorAgentMessageParts(msg, false)
	require.Equal(t, "first\nsecond", text)
	require.Empty(t, images)
}

func TestDataURIMediaType(t *testing.T) {
	require.Equal(t, "image/png", dataURIMediaType("data:image/png;base64,AAAA"))
	require.Equal(t, "image/jpeg", dataURIMediaType("DATA:image/jpeg;base64,AAAA"))
	require.Equal(t, "", dataURIMediaType("data:;base64,AAAA"))
	require.Equal(t, "", dataURIMediaType("https://example.com/x.png"))
	require.Equal(t, "", dataURIMediaType("data:image/png;base64"))
}

func TestParseEnvBoolDefaultTrue(t *testing.T) {
	for _, raw := range []string{"", "1", "true", "anything", "ON"} {
		require.True(t, parseEnvBoolDefaultTrue(raw), "raw %q", raw)
	}
	for _, raw := range []string{"0", "false", "No", " OFF "} {
		require.False(t, parseEnvBoolDefaultTrue(raw), "raw %q", raw)
	}
}

func testPNGDataURI(t *testing.T, width, height int) (uri string, raw []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	raw = buf.Bytes()
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), raw
}
