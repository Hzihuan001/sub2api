package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrepareOpenAIWSAccountFallbackKeepsAvailableInputAndDisablesOldState(t *testing.T) {
	original := []byte(`{"type":"response.create","model":"gpt-5.6-sol","previous_response_id":"resp_from_a","conversation":{"id":"conversation_a"},"input":[{"type":"item_reference","id":"item_from_a"},{"type":"function_call_output","call_id":"call_1","output":"available tool result"},{"type":"message","role":"user","content":"continue this request"}]}`)

	ctx, body, err := prepareOpenAIWSAccountFallback(context.Background(), original)

	require.NoError(t, err)
	disabled, ok := ctx.Value(ctxkey.OpenAIWSDisableStoredTurnState).(bool)
	require.True(t, ok)
	require.True(t, disabled)
	require.False(t, gjson.GetBytes(body, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(body, "conversation").Exists())
	require.Equal(t, 2, len(gjson.GetBytes(body, "input").Array()))
	require.Equal(t, "function_call_output", gjson.GetBytes(body, "input.0.type").String())
	require.Equal(t, "available tool result", gjson.GetBytes(body, "input.0.output").String())
	require.Equal(t, "continue this request", gjson.GetBytes(body, "input.1.content").String())
}
