package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// Request-side translation: OpenAI Chat Completions → cursor.AgentRunParams
// (agent.v1.AgentService/Run).
//
// The bridge is stateless. AgentService models a turn as one prompt plus an
// optional conversation_state blob it minted itself; a gateway has no such
// blob, so the whole client-side history is flattened into the prompt and
// conversation_state is left empty. That costs prompt-cache locality but keeps
// every request self-contained, which is what a pooled-account gateway needs.

// Escape hatches for the two protobuf paths whose field numbers are inferred
// from reverse-engineered clients rather than an official schema. Both default
// to on; set the variable to 0/false/no/off to fall back to the previous
// text-only behaviour (tool declarations injected as an instruction block,
// images dropped) without redeploying a different build.
const (
	envCursorNativeTools  = "SUB2API_CURSOR_NATIVE_TOOLS"
	envCursorNativeImages = "SUB2API_CURSOR_NATIVE_IMAGES"
)

// Role labels for the flattened transcript. A single-user-message turn is sent
// unlabelled so the common case reads as a plain prompt.
const (
	cursorPromptUserLabel      = "User"
	cursorPromptAssistantLabel = "Assistant"
	cursorPromptToolLabel      = "Tool result"
)

// cursorThinkingSuffix is how the agent protocol names a reasoning variant:
// there is no thinking flag on the wire, only a different model_id.
const cursorThinkingSuffix = "-thinking"

// cursorTranslateOptions carries the per-account facts the mapping needs plus
// the native-path feature flags.
type cursorTranslateOptions struct {
	nativeTools  bool
	nativeImages bool
	// observedModels is the account's last known upstream model list. It gates
	// the "-thinking" suffix: naming a model_id the upstream does not serve
	// fails the whole turn, so the variant is only requested when it was seen.
	observedModels []string
	// cwd is reported in the environment frame. A gateway has no workspace, so
	// this is a neutral placeholder.
	cwd string
}

var (
	cursorNativeOnce  sync.Once
	cursorNativeCache cursorTranslateOptions
)

func cursorNativeFlags() cursorTranslateOptions {
	cursorNativeOnce.Do(func() {
		cursorNativeCache = cursorTranslateOptions{
			nativeTools:  parseEnvBoolDefaultTrue(os.Getenv(envCursorNativeTools)),
			nativeImages: parseEnvBoolDefaultTrue(os.Getenv(envCursorNativeImages)),
		}
	})
	return cursorNativeCache
}

func parseEnvBoolDefaultTrue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// cursorToolPlan is the resolved tool configuration for one turn: the native
// MCP declarations to send, plus whatever residual instruction text is needed
// for constraints the protobuf has no field for (tool_choice) or for the
// degraded path.
type cursorToolPlan struct {
	// declarations are the tools to send natively in mcp_tools.
	declarations []cursorpkg.AgentTool
	// instruction is appended to the system prompt: the full tool catalogue in
	// degraded mode, or just a tool_choice nudge in native mode.
	instruction string
}

// buildCursorAgentRun translates an OpenAI Chat Completions request into the
// agent turn for the given account.
func buildCursorAgentRun(
	account *Account,
	upstreamModel string,
	req *apicompat.ChatCompletionsRequest,
) (cursorpkg.AgentRunParams, string, error) {
	opts := cursorNativeFlags()
	opts.cwd = cursorpkg.AgentDefaultCwd
	// The observed list is only consulted to pick a "-thinking" variant, and
	// reading it costs a JSON round trip through Extra, so it is left empty on
	// the turns that cannot use it.
	if account != nil && req != nil && cursorAgentWantsThinking(req.ReasoningEffort) {
		opts.observedModels = CursorObservedModelIDs(account.Extra)
	}
	return buildCursorAgentRunParams(upstreamModel, req, opts)
}

// buildCursorAgentRunParams translates an OpenAI Chat Completions request into
// an AgentRunParams. It returns the params plus the concatenated input text
// used for the local token estimate when the upstream reports no usage.
//
// System messages fold into custom_system_prompt; the remaining messages are
// rendered into a single labelled transcript that becomes the user message.
// Declared tools go out natively as MCP tools, and inline images ride along as
// selected_images.
func buildCursorAgentRunParams(
	upstreamModel string,
	req *apicompat.ChatCompletionsRequest,
	opts cursorTranslateOptions,
) (cursorpkg.AgentRunParams, string, error) {
	if req == nil {
		return cursorpkg.AgentRunParams{}, "", fmt.Errorf("cursor: nil chat request")
	}

	plan := planCursorAgentTools(req, opts.nativeTools)
	var (
		systemParts []string
		turns       []cursorAgentTurn
		images      []cursorpkg.AgentImage
		inputParts  []string
	)
	appendInput := func(s string) {
		if s != "" {
			inputParts = append(inputParts, s)
		}
	}
	appendTurn := func(label, text string) {
		if text == "" {
			return
		}
		turns = append(turns, cursorAgentTurn{label: label, text: text})
		appendInput(text)
	}

	if instructions := strings.TrimSpace(req.Instructions); instructions != "" {
		systemParts = append(systemParts, instructions)
		appendInput(instructions)
	}

	for _, msg := range req.Messages {
		text, msgImages := cursorAgentMessageParts(msg, opts.nativeImages)
		images = append(images, msgImages...)
		switch strings.ToLower(strings.TrimSpace(msg.Role)) {
		case "system", "developer":
			if text != "" {
				systemParts = append(systemParts, text)
				appendInput(text)
			}
		case "assistant":
			appendTurn(cursorPromptAssistantLabel, joinCursorPromptParts(text, cursorAssistantToolCallText(msg)))
		case "tool", "function":
			appendTurn(cursorToolResultLabel(msg), text)
		default: // user and anything else
			if text == "" && len(msgImages) == 0 {
				continue
			}
			appendTurn(cursorPromptUserLabel, text)
		}
	}

	systemPrompt := strings.Join(systemParts, "\n\n")
	if plan.instruction != "" {
		systemPrompt = joinCursorPromptParts(systemPrompt, plan.instruction)
	}

	model, maxMode := cursorAgentWireModel(upstreamModel, req.ReasoningEffort, opts.observedModels)
	params := cursorpkg.AgentRunParams{
		Prompt:       renderCursorAgentPrompt(turns),
		Model:        model,
		MaxMode:      maxMode,
		SystemPrompt: systemPrompt,
		Mode:         cursorpkg.AgentModeAgent,
		Tools:        plan.declarations,
		Images:       images,
		Cwd:          opts.cwd,
	}
	return params, strings.Join(inputParts, "\n"), nil
}

// cursorAgentTurn is one rendered history entry in the flattened prompt.
type cursorAgentTurn struct {
	label string
	text  string
}

// renderCursorAgentPrompt flattens the transcript into the single prompt string
// AgentService accepts. A lone user message is sent verbatim; anything longer
// gets role labels so the model can tell the turns apart.
func renderCursorAgentPrompt(turns []cursorAgentTurn) string {
	if len(turns) == 1 && turns[0].label == cursorPromptUserLabel {
		return turns[0].text
	}
	parts := make([]string, 0, len(turns))
	for _, turn := range turns {
		parts = append(parts, turn.label+": "+turn.text)
	}
	return strings.Join(parts, "\n\n")
}

func joinCursorPromptParts(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\n\n")
}

// cursorToolResultLabel names the call a `role:"tool"` message answers, so the
// flattened transcript still pairs results with their invocation.
func cursorToolResultLabel(msg apicompat.ChatMessage) string {
	name := strings.TrimSpace(msg.Name)
	if name == "" {
		name = strings.TrimSpace(msg.ToolCallID)
	}
	if name == "" {
		return cursorPromptToolLabel
	}
	return cursorPromptToolLabel + " (" + name + ")"
}

// cursorAssistantToolCallText renders a prior assistant tool call as prose. The
// stateless bridge has no wire field to replay the call itself, so the model is
// told about it in the transcript instead.
func cursorAssistantToolCallText(msg apicompat.ChatMessage) string {
	if len(msg.ToolCalls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		rendered := "[tool call] " + name
		if args := strings.TrimSpace(call.Function.Arguments); args != "" {
			rendered += " " + args
		}
		parts = append(parts, rendered)
	}
	return strings.Join(parts, "\n")
}

// cursorAgentWireModel maps a resolved gateway model onto the agent protocol's
// model_id and max-mode flag.
//
// "auto" is api2's name for "let Cursor choose"; the agent protocol calls the
// same thing "default", and a free-tier account is only served that one. A
// "-max" suffix is the picker's max-mode variant, which travels as a flag
// rather than as part of the id.
//
// Reasoning is requested by naming the "-thinking" variant, since the protocol
// has no thinking field. The suffix is only appended when the account has
// actually observed that model upstream: an unknown model_id fails the turn
// outright, which would be a worse outcome than a non-reasoning answer.
func cursorAgentWireModel(model, reasoningEffort string, observedModels []string) (string, bool) {
	id := strings.TrimSpace(model)
	switch strings.ToLower(id) {
	case "", "auto", cursorpkg.AgentDefaultModel:
		return cursorpkg.AgentDefaultModel, false
	}

	maxMode := false
	for _, suffix := range []string{"-max", ":max"} {
		if trimmed := strings.TrimSuffix(id, suffix); trimmed != id && trimmed != "" {
			id, maxMode = trimmed, true
			break
		}
	}

	if cursorAgentWantsThinking(reasoningEffort) && !strings.HasSuffix(strings.ToLower(id), cursorThinkingSuffix) {
		if candidate := id + cursorThinkingSuffix; containsFold(observedModels, candidate) {
			id = candidate
		}
	}
	return id, maxMode
}

// cursorAgentWantsThinking reports whether an OpenAI reasoning_effort asks for
// a reasoning turn. Only an explicit "none" opts out; an absent value keeps the
// model's own default.
func cursorAgentWantsThinking(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "medium", "high", "xhigh", "minimal":
		return true
	default:
		return false
	}
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

// planCursorAgentTools resolves the declared tools and tool_choice into either
// native MCP declarations or the degraded instruction block.
func planCursorAgentTools(req *apicompat.ChatCompletionsRequest, nativeTools bool) cursorToolPlan {
	decls := collectCursorToolDeclarations(req)
	choice := parseCursorToolChoice(req.ToolChoice)
	if len(decls) == 0 || choice.none {
		// tool_choice:"none" means the tools must not be offered at all, which
		// on this protocol is simply an empty mcp_tools.
		return cursorToolPlan{}
	}
	if !nativeTools {
		return cursorToolPlan{instruction: cursorToolInstruction(decls, choice)}
	}

	plan := cursorToolPlan{declarations: make([]cursorpkg.AgentTool, 0, len(decls))}
	for _, decl := range decls {
		plan.declarations = append(plan.declarations, cursorpkg.AgentTool{
			Name:        decl.Name,
			Description: decl.Description,
			InputSchema: cursorAgentToolSchema(decl.Parameters),
		})
	}
	// The protobuf has no tool_choice equivalent, so a forced choice is the one
	// thing still expressed in prose.
	plan.instruction = cursorToolChoiceInstruction(choice)
	return plan
}

// cursorToolDeclaration is one normalized tool definition, sourced from either
// `tools` or the legacy `functions` array.
type cursorToolDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func collectCursorToolDeclarations(req *apicompat.ChatCompletionsRequest) []cursorToolDeclaration {
	var decls []cursorToolDeclaration
	seen := make(map[string]struct{})
	add := func(decl cursorToolDeclaration) {
		decl.Name = strings.TrimSpace(decl.Name)
		if decl.Name == "" {
			return
		}
		if _, dup := seen[decl.Name]; dup {
			return
		}
		seen[decl.Name] = struct{}{}
		decls = append(decls, decl)
	}
	for _, tool := range req.Tools {
		// Only plain function tools bridge cleanly; provider-native tools
		// (x_search and friends) have no MCP equivalent.
		if !strings.EqualFold(strings.TrimSpace(tool.Type), "function") && strings.TrimSpace(tool.Type) != "" {
			continue
		}
		if tool.Function == nil {
			continue
		}
		add(cursorToolDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	for _, fn := range req.Functions {
		add(cursorToolDeclaration{Name: fn.Name, Description: fn.Description, Parameters: fn.Parameters})
	}
	return decls
}

// cursorAgentToolSchema decodes a tool's JSON Schema into the Go value
// McpToolDefinition.input_schema expects. Unlike api2, which takes the schema
// as a JSON string, agent.v1 wants a google.protobuf.Value, so the schema is
// decoded here and re-encoded on the wire. An unusable schema degrades to an
// empty object rather than dropping the tool.
func cursorAgentToolSchema(params json.RawMessage) any {
	trimmed := strings.TrimSpace(string(params))
	if trimmed == "" || trimmed == "null" {
		return cursorEmptyAgentToolSchema()
	}
	var decoded any
	if err := json.Unmarshal(params, &decoded); err != nil || decoded == nil {
		return cursorEmptyAgentToolSchema()
	}
	return decoded
}

func cursorEmptyAgentToolSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// cursorToolChoice is the parsed form of the OpenAI tool_choice field.
type cursorToolChoice struct {
	none     bool
	required bool
	function string
}

func parseCursorToolChoice(raw json.RawMessage) cursorToolChoice {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return cursorToolChoice{}
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		switch strings.ToLower(strings.TrimSpace(asString)) {
		case "none":
			return cursorToolChoice{none: true}
		case "required", "any":
			return cursorToolChoice{required: true}
		default:
			return cursorToolChoice{}
		}
	}
	var object struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return cursorToolChoice{}
	}
	if strings.EqualFold(strings.TrimSpace(object.Type), "none") {
		return cursorToolChoice{none: true}
	}
	name := strings.TrimSpace(object.Function.Name)
	if name == "" {
		name = strings.TrimSpace(object.Name)
	}
	if name == "" {
		return cursorToolChoice{}
	}
	return cursorToolChoice{required: true, function: name}
}

// cursorToolChoiceInstruction renders the part of tool_choice the protobuf
// cannot express. Returns "" for the default (auto) behaviour.
func cursorToolChoiceInstruction(choice cursorToolChoice) string {
	switch {
	case choice.function != "":
		return "You must call the tool `" + choice.function + "` before replying."
	case choice.required:
		return "You must call at least one of the available tools before replying."
	default:
		return ""
	}
}

// cursorToolInstruction renders the degraded tool catalogue used when native
// encoding is switched off, so the model still knows the tools exist.
func cursorToolInstruction(decls []cursorToolDeclaration, choice cursorToolChoice) string {
	encoded, err := json.Marshal(decls)
	if err != nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("You may call the following tools when helpful. ")
	b.WriteString("Respond with a JSON tool call when you decide to use one.\n")
	b.WriteString("Available tools: ")
	b.Write(encoded)
	if nudge := cursorToolChoiceInstruction(choice); nudge != "" {
		b.WriteString("\nTool choice preference: ")
		b.WriteString(nudge)
	}
	return b.String()
}

// cursorAgentMessageParts extracts the plain text of a Chat Completions message
// plus any natively-attachable images. Multi-modal arrays are flattened to
// their text parts; image parts become agent images when they carry inline
// base64 data, and degrade to a text mention otherwise (remote URLs are never
// fetched server-side).
func cursorAgentMessageParts(msg apicompat.ChatMessage, nativeImages bool) (string, []cursorpkg.AgentImage) {
	raw := strings.TrimSpace(string(msg.Content))
	if raw == "" || raw == "null" {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(msg.Content, &asString); err == nil {
		return strings.TrimSpace(asString), nil
	}
	var parts []apicompat.ChatContentPart
	if err := json.Unmarshal(msg.Content, &parts); err != nil {
		return "", nil
	}

	var textParts []string
	var images []cursorpkg.AgentImage
	for _, part := range parts {
		switch {
		case strings.EqualFold(part.Type, "text"):
			if strings.TrimSpace(part.Text) != "" {
				textParts = append(textParts, part.Text)
			}
		case strings.EqualFold(part.Type, "image_url") && part.ImageURL != nil:
			url := strings.TrimSpace(part.ImageURL.URL)
			if url == "" || !nativeImages {
				continue
			}
			img, err := cursorpkg.ParseImageDataURI(url)
			if err != nil {
				textParts = append(textParts, cursorImageFallbackText(url, err))
				continue
			}
			images = append(images, cursorpkg.AgentImage{
				Data:     img.Data,
				MimeType: dataURIMediaType(url),
				Width:    img.Width,
				Height:   img.Height,
			})
		}
	}
	return strings.TrimSpace(strings.Join(textParts, "\n")), images
}

// dataURIMediaType returns the media type of a data URI ("image/png"), or ""
// when the URI carries none. ParseImageDataURI validates the rest of the shape,
// so this only has to read the header it already accepted.
func dataURIMediaType(uri string) string {
	raw := strings.TrimSpace(uri)
	if !strings.HasPrefix(strings.ToLower(raw), "data:") {
		return ""
	}
	comma := strings.IndexByte(raw, ',')
	if comma < 0 {
		return ""
	}
	mediaType, _, _ := strings.Cut(raw[len("data:"):comma], ";")
	return strings.ToLower(strings.TrimSpace(mediaType))
}

// cursorImageFallbackText describes an image that could not be attached so the
// model at least knows it was referenced.
func cursorImageFallbackText(url string, err error) string {
	if strings.HasPrefix(strings.ToLower(url), "data:") {
		logger.L().Debug("cursor: dropping inline image", zap.Error(err))
		return "[image omitted: could not be decoded]"
	}
	return "[image: " + url + "]"
}
