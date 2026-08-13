package cursor

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodeChatRequestRoundTrip(t *testing.T) {
	t.Parallel()
	req := ChatRequest{
		Model:         "claude-4.5-sonnet",
		SystemPrompt:  "be concise",
		ThinkingLevel: ThinkingLevelHigh,
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "hi", ID: "m1"},
			{Role: RoleAssistant, Content: "hello"},
		},
	}
	body := EncodeChatRequest(req)

	top, err := Decode(body)
	require.NoError(t, err)
	inner := top.Bytes(fieldReqWithToolsRequest)
	require.NotNil(t, inner)

	chat, err := Decode(inner)
	require.NoError(t, err)

	msgs := chat.AllBytes(fieldChatMessages)
	require.Len(t, msgs, 2)

	m0, err := Decode(msgs[0])
	require.NoError(t, err)
	require.Equal(t, "hi", m0.String(fieldMsgContent))
	require.Equal(t, RoleUser, m0.Int32(fieldMsgRole))
	require.Equal(t, "m1", m0.String(fieldMsgID))

	m1, err := Decode(msgs[1])
	require.NoError(t, err)
	require.Equal(t, "hello", m1.String(fieldMsgContent))
	require.Equal(t, RoleAssistant, m1.Int32(fieldMsgRole))

	instr, err := Decode(chat.Bytes(fieldChatInstruction))
	require.NoError(t, err)
	require.Equal(t, "be concise", instr.String(fieldInstructionText))

	model, err := Decode(chat.Bytes(fieldChatModel))
	require.NoError(t, err)
	require.Equal(t, "claude-4.5-sonnet", model.String(fieldChatModelName))

	// unified_mode defaults to CHAT when unset.
	require.Equal(t, UnifiedModeChat, chat.Int32(fieldChatUnifiedMode))
	require.Equal(t, ThinkingLevelHigh, chat.Int32(fieldChatThinkingLevel))
	// Pure-text CHAT default does not set the agentic flag.
	require.False(t, chat.Has(fieldChatIsAgentic))
}

func TestParseChatResponseFrameText(t *testing.T) {
	t.Parallel()
	var resp Writer
	resp.WriteString(fieldChatRespText, "Hello")
	var top Writer
	top.WriteMessage(fieldRespResponse, resp.Bytes())

	ev, err := ParseChatResponseFrame(top.Bytes())
	require.NoError(t, err)
	require.NotNil(t, ev)
	require.Equal(t, ChatEventText, ev.Type)
	require.Equal(t, "Hello", ev.Text)
}

func TestParseChatResponseFrameThinking(t *testing.T) {
	t.Parallel()
	var thinking Writer
	thinking.WriteString(fieldThinkingText, "let me think")
	var resp Writer
	resp.WriteMessage(fieldChatRespThinking, thinking.Bytes())
	var top Writer
	top.WriteMessage(fieldRespResponse, resp.Bytes())

	ev, err := ParseChatResponseFrame(top.Bytes())
	require.NoError(t, err)
	require.NotNil(t, ev)
	require.Equal(t, ChatEventThinking, ev.Type)
	require.Equal(t, "let me think", ev.Text)
}

func TestParseChatResponseFrameToolCall(t *testing.T) {
	t.Parallel()
	var tc Writer
	tc.WriteString(fieldToolCallID, "call_123\nextra-context")
	tc.WriteString(fieldToolCallName, "search")
	tc.WriteString(fieldToolCallRawArgs, `{"q":"x"}`)
	tc.WriteBool(fieldToolCallIsLast, true)
	var top Writer
	top.WriteMessage(fieldRespToolCall, tc.Bytes())

	ev, err := ParseChatResponseFrame(top.Bytes())
	require.NoError(t, err)
	require.NotNil(t, ev)
	require.Equal(t, ChatEventToolCall, ev.Type)
	require.NotNil(t, ev.ToolCall)
	// Only the first line of the id is kept.
	require.Equal(t, "call_123", ev.ToolCall.ID)
	require.Equal(t, "search", ev.ToolCall.Name)
	require.Equal(t, `{"q":"x"}`, ev.ToolCall.RawArgs)
	require.True(t, ev.ToolCall.IsLast)
}

func TestParseChatResponseFrameNoDelta(t *testing.T) {
	t.Parallel()
	ev, err := ParseChatResponseFrame(nil)
	require.NoError(t, err)
	require.Nil(t, ev)
}

func TestStreamDecoderNormalEnd(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer

	var resp Writer
	resp.WriteString(fieldChatRespText, "Hi")
	var top Writer
	top.WriteMessage(fieldRespResponse, resp.Bytes())
	buf.Write(EncodeFrame(top.Bytes(), false))
	buf.Write(encodeRawFrame(flagEndStream, []byte("{}")))

	sd := NewStreamDecoder(&buf)

	ev1, err := sd.Next()
	require.NoError(t, err)
	require.Equal(t, ChatEventText, ev1.Type)
	require.Equal(t, "Hi", ev1.Text)

	ev2, err := sd.Next()
	require.NoError(t, err)
	require.Equal(t, ChatEventEnd, ev2.Type)
	require.NoError(t, ev2.Err)

	_, err = sd.Next()
	require.ErrorIs(t, err, io.EOF)
}

func TestStreamDecoderErrorEnd(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	errJSON := `{"error":{"code":"rate_limit_error","message":"slow down"}}`
	buf.Write(encodeRawFrame(flagEndStream, []byte(errJSON)))

	sd := NewStreamDecoder(&buf)
	ev, err := sd.Next()
	require.NoError(t, err)
	require.Equal(t, ChatEventEnd, ev.Type)
	require.Error(t, ev.Err)

	var se *StreamError
	require.ErrorAs(t, ev.Err, &se)
	require.Equal(t, "rate_limit_error", se.Code)
	require.Equal(t, "slow down", se.Message)
}

func TestStreamDecoderGzippedEndStream(t *testing.T) {
	t.Parallel()
	// A compressed trailer frame (flag 0x03) must be gunzipped, then parsed as
	// a clean end.
	var buf bytes.Buffer
	buf.Write(encodeRawFrame(flagEndStream|flagCompressed, gzipBytes([]byte(`{}`))))

	sd := NewStreamDecoder(&buf)
	ev, err := sd.Next()
	require.NoError(t, err)
	require.Equal(t, ChatEventEnd, ev.Type)
	require.NoError(t, ev.Err)
}
