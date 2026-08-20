package cursor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAvailableModelsResponse(t *testing.T) {
	t.Parallel()

	// Build a known AvailableModelsResponse byte sequence using the wire
	// primitives, then confirm the parser reconstructs it field-for-field.
	var param Writer
	param.WriteString(fieldParamID, "reasoning")
	param.WriteString(fieldParamValue, "high")

	var variant Writer
	variant.WriteMessage(fieldVariantParams, param.Bytes())
	variant.WriteString(fieldVariantDisplayName, "Thinking")
	variant.WriteBool(fieldVariantIsMaxMode, true)
	variant.WriteString(fieldVariantVariantString, "thinking")

	var m1 Writer
	m1.WriteString(fieldModelName, "gpt-5")
	m1.WriteBool(fieldModelSupportsImages, true)
	m1.WriteInt64(fieldModelContextTokenLimit, 200000)
	m1.WriteString(fieldModelClientDisplayName, "GPT-5")
	m1.WriteString(fieldModelServerModelName, "gpt-5-server")
	m1.WriteBool(fieldModelSupportsNonMaxMode, true)
	m1.WriteMessage(fieldModelParameterizedVariant, variant.Bytes())

	var m2 Writer
	m2.WriteString(fieldModelName, "auto")

	var resp Writer
	resp.WriteMessage(fieldRespModels, m1.Bytes())
	resp.WriteMessage(fieldRespModels, m2.Bytes())

	models, err := ParseAvailableModelsResponse(resp.Bytes())
	require.NoError(t, err)
	require.Len(t, models, 2)

	require.Equal(t, "gpt-5", models[0].Name)
	require.True(t, models[0].SupportsImages)
	require.Equal(t, int64(200000), models[0].ContextTokenLimit)
	require.Equal(t, "GPT-5", models[0].ClientDisplayName)
	require.Equal(t, "gpt-5-server", models[0].ServerModelName)
	require.True(t, models[0].SupportsNonMaxMode)

	require.Len(t, models[0].ParameterizedVariants, 1)
	v := models[0].ParameterizedVariants[0]
	require.Equal(t, "Thinking", v.DisplayName)
	require.True(t, v.IsMaxMode)
	require.Equal(t, "thinking", v.VariantString)
	require.Len(t, v.Params, 1)
	require.Equal(t, "reasoning", v.Params[0].ID)
	require.Equal(t, "high", v.Params[0].Value)

	require.Equal(t, "auto", models[1].Name)
	require.Empty(t, models[1].ParameterizedVariants)
}

func TestEncodeAvailableModelsRequest(t *testing.T) {
	t.Parallel()
	// Both flags false -> empty body (accepted by upstream).
	require.Empty(t, EncodeAvailableModelsRequest(false, false))

	body := EncodeAvailableModelsRequest(true, true)
	f, err := Decode(body)
	require.NoError(t, err)
	require.True(t, f.Bool(fieldReqUseModelParameters))
	require.True(t, f.Bool(fieldReqDoNotUseMarkdown))
}

func TestDefaultModelIDs(t *testing.T) {
	t.Parallel()
	ids := DefaultModelIDs()
	require.NotEmpty(t, ids)
	require.Contains(t, ids, "auto")
	require.Contains(t, ids, "composer-2.5")
}
