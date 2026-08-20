package cursor

// Protobuf field numbers for AiService/AvailableModels. Cross-checked against
// kaitranntt/ccs and wisdgod/cursor-api.
const (
	// AvailableModelsRequest
	fieldReqUseModelParameters = 5
	fieldReqDoNotUseMarkdown   = 7

	// AvailableModelsResponse
	fieldRespModels = 2

	// Model
	fieldModelName                 = 1
	fieldModelSupportsImages       = 10
	fieldModelSupportsMaxMode      = 14
	fieldModelContextTokenLimit    = 15
	fieldModelMaxModeContextLimit  = 16
	fieldModelClientDisplayName    = 17
	fieldModelServerModelName      = 18
	fieldModelSupportsNonMaxMode   = 19
	fieldModelParameterizedVariant = 30

	// Model.Variant
	fieldVariantParams             = 1
	fieldVariantDisplayName        = 2
	fieldVariantIsMaxMode          = 3
	fieldVariantIsDefaultMaxConfig = 4
	fieldVariantIsDefaultNonMax    = 5
	fieldVariantDisplayNameOutside = 8
	fieldVariantVariantString      = 9

	// Model.Variant.Param
	fieldParamID    = 1
	fieldParamValue = 2
)

// Model is one entry from AvailableModels. Field names mirror the protobuf
// message; zero values mean the field was absent upstream.
type Model struct {
	Name                     string
	SupportsImages           bool
	SupportsMaxMode          bool
	ContextTokenLimit        int64
	MaxModeContextTokenLimit int64
	ClientDisplayName        string
	ServerModelName          string
	SupportsNonMaxMode       bool
	ParameterizedVariants    []Variant
}

// Variant is one parameterized variant of a model (e.g. a max-mode config).
type Variant struct {
	Params                   []Param
	DisplayName              string
	IsMaxMode                bool
	IsDefaultMaxConfig       bool
	IsDefaultNonMaxConfig    bool
	DisplayNameOutsidePicker string
	VariantString            string
}

// Param is a single id/value pair inside a Variant.
type Param struct {
	ID    string
	Value string
}

// EncodeAvailableModelsRequest builds the (usually empty) request body. Both
// flags default false and, per proto3, are omitted when false — so
// EncodeAvailableModelsRequest(false, false) returns an empty body, which the
// upstream accepts. The result is the raw protobuf message: send it as the
// unary body with ContentTypeProto (no envelope).
func EncodeAvailableModelsRequest(useModelParameters, doNotUseMarkdown bool) []byte {
	var w Writer
	if useModelParameters {
		w.WriteBool(fieldReqUseModelParameters, true)
	}
	if doNotUseMarkdown {
		w.WriteBool(fieldReqDoNotUseMarkdown, true)
	}
	return w.Bytes()
}

// ParseAvailableModelsResponse decodes an AvailableModelsResponse body into
// Model structs. data is the raw protobuf response body (unary, un-enveloped).
func ParseAvailableModelsResponse(data []byte) ([]Model, error) {
	top, err := Decode(data)
	if err != nil {
		return nil, err
	}
	rawModels := top.AllBytes(fieldRespModels)
	models := make([]Model, 0, len(rawModels))
	for _, mb := range rawModels {
		m, err := parseModel(mb)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, nil
}

func parseModel(data []byte) (Model, error) {
	f, err := Decode(data)
	if err != nil {
		return Model{}, err
	}
	m := Model{
		Name:                     f.String(fieldModelName),
		SupportsImages:           f.Bool(fieldModelSupportsImages),
		SupportsMaxMode:          f.Bool(fieldModelSupportsMaxMode),
		ContextTokenLimit:        f.Int64(fieldModelContextTokenLimit),
		MaxModeContextTokenLimit: f.Int64(fieldModelMaxModeContextLimit),
		ClientDisplayName:        f.String(fieldModelClientDisplayName),
		ServerModelName:          f.String(fieldModelServerModelName),
		SupportsNonMaxMode:       f.Bool(fieldModelSupportsNonMaxMode),
	}
	for _, vb := range f.AllBytes(fieldModelParameterizedVariant) {
		v, err := parseVariant(vb)
		if err != nil {
			return Model{}, err
		}
		m.ParameterizedVariants = append(m.ParameterizedVariants, v)
	}
	return m, nil
}

func parseVariant(data []byte) (Variant, error) {
	f, err := Decode(data)
	if err != nil {
		return Variant{}, err
	}
	v := Variant{
		DisplayName:              f.String(fieldVariantDisplayName),
		IsMaxMode:                f.Bool(fieldVariantIsMaxMode),
		IsDefaultMaxConfig:       f.Bool(fieldVariantIsDefaultMaxConfig),
		IsDefaultNonMaxConfig:    f.Bool(fieldVariantIsDefaultNonMax),
		DisplayNameOutsidePicker: f.String(fieldVariantDisplayNameOutside),
		VariantString:            f.String(fieldVariantVariantString),
	}
	for _, pb := range f.AllBytes(fieldVariantParams) {
		pf, err := Decode(pb)
		if err != nil {
			return Variant{}, err
		}
		v.Params = append(v.Params, Param{
			ID:    pf.String(fieldParamID),
			Value: pf.String(fieldParamValue),
		})
	}
	return v, nil
}

// DefaultModelIDs is a small fallback list of Cursor model ids known as of
// 2026-08. It exists only so the gateway can still expose *something* if the
// upstream AvailableModels call fails; at runtime the authoritative list must
// come from ParseAvailableModelsResponse. Do not treat this as canonical.
func DefaultModelIDs() []string {
	return []string{
		"auto",
		"cursor-small",
		"composer-2.5",
		"composer-2.5-fast",
		"claude-4.5-sonnet",
		"claude-4.6-sonnet",
		"claude-opus-4.8",
		"gpt-5",
		"gpt-5.6-sol",
		"gemini-3-pro",
		"gemini-3.5-flash",
		"deepseek-v3.1",
		"grok-4.6",
	}
}
