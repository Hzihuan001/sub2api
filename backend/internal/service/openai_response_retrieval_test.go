package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRebuildOpenAIResponseRecoveryBodyPreservesCurrentInstructionsAndRemovesPreviousID(t *testing.T) {
	body := []byte(`{"model":"gpt-5.6-sol","previous_response_id":"resp_old","instructions":[{"type":"input_text","text":"keep this turn"}],"input":[{"role":"user","content":"next"}]}`)
	history := []json.RawMessage{
		json.RawMessage(`{"id":"msg_1","type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}`),
		json.RawMessage(`{"id":"msg_2","type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}`),
	}
	rebuilt, err := rebuildOpenAIResponseRecoveryBody(body, history)
	require.NoError(t, err)
	var got map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(rebuilt, &got))
	_, hasPrevious := got["previous_response_id"]
	require.False(t, hasPrevious)
	require.JSONEq(t, `[{"type":"input_text","text":"keep this turn"}]`, string(got["instructions"]))
	var input []json.RawMessage
	require.NoError(t, json.Unmarshal(got["input"], &input))
	require.Len(t, input, 3)
	require.Contains(t, string(input[0]), `"id":"msg_1"`)
	require.Contains(t, string(input[2]), `"role":"user"`)
}

func TestValidateOpenAIResponseRecoveryItemRejectsProviderStateRecursively(t *testing.T) {
	for _, item := range []string{
		`{"type":"message","content":[{"type":"input_image","image_url":"https://example.invalid/a.png"}]}`,
		`{"type":"message","content":[{"type":"input_text","file_url":"https://example.invalid/a.txt"}]}`,
		`{"type":"function_call","name":"dangerous_tool","arguments":"{}"}`,
		`{"type":"reasoning","encrypted_content":"secret"}`,
	} {
		err := validateOpenAIResponseRecoveryItem(json.RawMessage(item))
		require.Error(t, err, item)
		require.True(t, errors.Is(err, ErrOpenAIResponseRecoveryUnsupported))
	}
}

func TestValidateOpenAIResponseRecoveryItemAllowsPlainTextMessage(t *testing.T) {
	err := validateOpenAIResponseRecoveryItem(json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}`))
	require.NoError(t, err)
}

func TestAppendUniqueOpenAIResponseRecoveryItemRejectsConflictingIDs(t *testing.T) {
	items := []json.RawMessage{json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant"}`)}
	_, err := appendUniqueOpenAIResponseRecoveryItem(items, json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant","content":[]}`))
	require.ErrorIs(t, err, ErrOpenAIResponseRecoveryUnsupported)
	items, err = appendUniqueOpenAIResponseRecoveryItem(items, json.RawMessage(`{"id":"msg_1","type":"message","role":"assistant"}`))
	require.NoError(t, err)
	require.Len(t, items, 1)
}

func TestValidateOpenAIResponseRecoveryMetadata(t *testing.T) {
	require.NoError(t, validateOpenAIResponseRecoveryMetadata([]byte(`{"object":"response","status":"completed","store":true,"model":"gpt-5.6-sol","previous_response_id":"resp_parent"}`), "gpt-5.6-sol"))
	require.Error(t, validateOpenAIResponseRecoveryMetadata([]byte(`{"object":"response","status":"in_progress","store":true,"model":"gpt-5.6-sol"}`), "gpt-5.6-sol"))
	require.Error(t, validateOpenAIResponseRecoveryMetadata([]byte(`{"object":"other","status":"completed","store":true,"model":"gpt-5.6-sol"}`), "gpt-5.6-sol"))
	require.Error(t, validateOpenAIResponseRecoveryMetadata([]byte(`{"object":"response","status":"completed","store":true,"model":"gpt-5.6-sol","previous_response_id":"msg_1"}`), "gpt-5.6-sol"))
}
