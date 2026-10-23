package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestRemoveOpenAIStoredResponseReferencesFromBodyKeepsCurrentInput(t *testing.T) {
	body := []byte(`{"previous_response_id":"resp_from_a","conversation":"conversation_from_a","input":[{"type":"item_reference","id":"item_from_a"},{"type":"message","role":"user","content":"keep this request"},{"type":"function_call_output","call_id":"call_from_a","output":"keep available output"}]}`)

	got, err := RemoveOpenAIStoredResponseReferencesFromBody(body)

	require.NoError(t, err)
	require.False(t, gjson.GetBytes(got, "previous_response_id").Exists())
	require.False(t, gjson.GetBytes(got, "conversation").Exists())
	require.Equal(t, 2, len(gjson.GetBytes(got, "input").Array()))
	require.Equal(t, "message", gjson.GetBytes(got, "input.0.type").String())
	require.Equal(t, "keep this request", gjson.GetBytes(got, "input.0.content").String())
	require.Equal(t, "keep available output", gjson.GetBytes(got, "input.1.output").String())
}
