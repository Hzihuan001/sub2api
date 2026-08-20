//go:build unit

package service

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

// agentEvents turns a fixed event list into the closed channel
// consumeCursorAgentEvents reads, standing in for a live Run turn.
func agentEvents(events ...cursorpkg.AgentEvent) <-chan cursorpkg.AgentEvent {
	ch := make(chan cursorpkg.AgentEvent, len(events))
	for _, event := range events {
		ch <- event
	}
	close(ch)
	return ch
}

func collectCursorDeltas(deltas *[]cursorDelta) func(cursorDelta) error {
	return func(delta cursorDelta) error {
		*deltas = append(*deltas, delta)
		return nil
	}
}

func TestConsumeCursorAgentEventsTextAndThinking(t *testing.T) {
	var deltas []cursorDelta
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventThinking, Text: "pondering"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventThinking, Text: " harder"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventThinkingEnd, Usage: &cursorpkg.AgentUsage{ThinkingDurationMs: 42}},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "Hello"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: " world"},
		// Heartbeats and Cursor's own agentic tools carry nothing an OpenAI
		// client can act on and must not reach the response.
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventHeartbeat},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventToolCallStarted, ToolCall: &cursorpkg.AgentToolCall{ID: "builtin_1"}},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventTurnEnded},
	), time.Now(), 0, collectCursorDeltas(&deltas))
	require.NoError(t, err)

	require.Equal(t, "Hello world", outcome.content)
	require.Equal(t, "pondering harder", outcome.reasoning)
	require.Equal(t, "stop", outcome.finishReason)
	require.Empty(t, outcome.toolCalls)
	require.NotNil(t, outcome.firstTokenMs)

	require.Len(t, deltas, 4)
	require.Equal(t, cursorDeltaReasoning, deltas[0].kind)
	require.Equal(t, "pondering", deltas[0].text)
	require.Equal(t, cursorDeltaText, deltas[2].kind)
	require.Equal(t, "Hello", deltas[2].text)
}

func TestConsumeCursorAgentEventsToolCall(t *testing.T) {
	var deltas []cursorDelta
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "Let me check."},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventToolCall, ToolCall: &cursorpkg.AgentToolCall{
			ID: "call_a", Name: "get_weather", Arguments: `{"city":"SF"}`,
		}},
	), time.Now(), 0, collectCursorDeltas(&deltas))
	require.NoError(t, err)

	require.Equal(t, "Let me check.", outcome.content)
	require.Equal(t, "tool_calls", outcome.finishReason)
	require.Len(t, outcome.toolCalls, 1)
	require.Equal(t, "call_a", outcome.toolCalls[0].ID)
	require.Equal(t, "function", outcome.toolCalls[0].Type)
	require.Equal(t, 0, *outcome.toolCalls[0].Index)
	require.Equal(t, "get_weather", outcome.toolCalls[0].Function.Name)
	// A native MCP call arrives complete, so arguments are not reassembled.
	require.Equal(t, `{"city":"SF"}`, outcome.toolCalls[0].Function.Arguments)

	require.Len(t, deltas, 2)
	require.Equal(t, cursorDeltaToolCall, deltas[1].kind)
	require.Equal(t, 0, deltas[1].toolIndex)
	require.Equal(t, `{"city":"SF"}`, deltas[1].toolArguments)
}

func TestConsumeCursorAgentEventsParallelToolCallsGetStableIndexes(t *testing.T) {
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventToolCall, ToolCall: &cursorpkg.AgentToolCall{
			ID: "call_a", Name: "get_weather", Arguments: `{"city":"SF"}`,
		}},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventToolCall, ToolCall: &cursorpkg.AgentToolCall{
			ID: "call_b", Name: "get_time", Arguments: `{"tz":"PT"}`,
		}},
	), time.Now(), 0, nil)
	require.NoError(t, err)

	require.Len(t, outcome.toolCalls, 2)
	require.Equal(t, 0, *outcome.toolCalls[0].Index)
	require.Equal(t, "get_weather", outcome.toolCalls[0].Function.Name)
	require.Equal(t, 1, *outcome.toolCalls[1].Index)
	require.Equal(t, "get_time", outcome.toolCalls[1].Function.Name)
}

func TestConsumeCursorAgentEventsErrorKeepsPartialOutput(t *testing.T) {
	upstreamErr := &cursorpkg.AgentError{Code: "unauthenticated", HTTPStatus: http.StatusUnauthorized}
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "partial"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventError, Err: upstreamErr},
	), time.Now(), 0, nil)

	require.ErrorIs(t, err, error(upstreamErr))
	// The text already streamed to the client stays in the outcome so a
	// post-first-byte failure can still be billed for what was delivered.
	require.Equal(t, "partial", outcome.content)
}

func TestResolveCursorUsagePrefersUpstreamAccounting(t *testing.T) {
	outcome := cursorChatOutcome{
		content: "hello",
		usage: &cursorpkg.AgentUsage{
			InputTokens: 120, OutputTokens: 34, CacheReadTokens: 7, CacheWriteTokens: 3,
		},
	}
	usage := resolveCursorUsage(cursorInputEstimate{text: "a much longer prompt than the answer"}, outcome)
	require.Equal(t, 120, usage.InputTokens)
	require.Equal(t, 34, usage.OutputTokens)
	require.Equal(t, 7, usage.CacheReadInputTokens)
	require.Equal(t, 3, usage.CacheCreationInputTokens)
}

func TestResolveCursorUsageFallsBackToEstimateWithoutTurnEndedFrame(t *testing.T) {
	outcome := cursorChatOutcome{content: "hello there"}
	usage := resolveCursorUsage(cursorInputEstimate{text: "what is up"}, outcome)
	require.Equal(t, estimateTokensForText("what is up"), usage.InputTokens)
	require.Equal(t, estimateTokensForText("hello there"), usage.OutputTokens)
	require.Positive(t, usage.InputTokens)
	require.Positive(t, usage.OutputTokens)
}

// A turn that ends with an all-zero usage frame is indistinguishable from one
// that reported nothing, so it must not bill as zero.
func TestResolveCursorUsageIgnoresEmptyUsageFrame(t *testing.T) {
	outcome := cursorChatOutcome{content: "hello there", usage: &cursorpkg.AgentUsage{}}
	usage := resolveCursorUsage(cursorInputEstimate{text: "what is up"}, outcome)
	require.Equal(t, estimateTokensForText("hello there"), usage.OutputTokens)
}

// Images have no textual form, so a request that carries only images used to
// bill zero input tokens.
func TestResolveCursorUsageEstimateCountsImages(t *testing.T) {
	usage := resolveCursorUsage(cursorInputEstimate{imageTokens: 1500}, cursorChatOutcome{content: "ok"})
	require.Equal(t, 1500, usage.InputTokens)

	withText := resolveCursorUsage(
		cursorInputEstimate{text: "describe this", imageTokens: 1500},
		cursorChatOutcome{content: "ok"},
	)
	require.Equal(t, estimateTokensForText("describe this")+1500, withText.InputTokens)
}

// A locally truncated turn must not bill the upstream's output count: that
// includes the tail this gateway dropped and never delivered.
func TestResolveCursorUsageIgnoresUpstreamOutputWhenTruncated(t *testing.T) {
	outcome := cursorChatOutcome{
		content:   "delivered",
		truncated: true,
		usage:     &cursorpkg.AgentUsage{InputTokens: 120, OutputTokens: 9000},
	}
	usage := resolveCursorUsage(cursorInputEstimate{text: "prompt"}, outcome)
	require.Equal(t, 120, usage.InputTokens, "upstream input accounting is still the best available")
	require.Equal(t, estimateTokensForText("delivered"), usage.OutputTokens)
}

func TestResolveCursorUsageEstimateCoversToolCalls(t *testing.T) {
	outcome := cursorChatOutcome{
		finishReason: "tool_calls",
		toolCalls: []apicompat.ChatToolCall{{
			ID:       "call_a",
			Type:     "function",
			Function: apicompat.ChatFunctionCall{Name: "get_weather", Arguments: `{"city":"SF"}`},
		}},
	}
	usage := resolveCursorUsage(cursorInputEstimate{text: "weather in SF?"}, outcome)
	require.Equal(t, estimateTokensForText(`get_weather{"city":"SF"}`), usage.OutputTokens)
}

// The agent protocol has no max_tokens field, so the ceiling is enforced while
// relaying: output stops at the budget and the turn reports "length", which the
// Anthropic and Responses bridges translate into max_tokens / max_output_tokens.
func TestConsumeCursorAgentEventsTruncatesAtMaxTokens(t *testing.T) {
	var deltas []cursorDelta
	// ~4 ASCII chars per token, so 2 tokens is about 8 characters.
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "12345678"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "this must never reach the client"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventTurnEnded, Usage: &cursorpkg.AgentUsage{OutputTokens: 500}},
	), time.Now(), 2, collectCursorDeltas(&deltas))

	require.NoError(t, err)
	require.True(t, outcome.truncated)
	require.Equal(t, "length", outcome.finishReason)
	require.Equal(t, "12345678", outcome.content)
	require.Len(t, deltas, 1)
	// The upstream frame that followed the cut is never consumed, so its usage
	// cannot leak into billing either.
	require.Nil(t, outcome.usage)
}

// The budget can run out inside an increment; the part that fits is still
// delivered rather than being dropped whole.
func TestConsumeCursorAgentEventsTruncatesMidIncrement(t *testing.T) {
	var deltas []cursorDelta
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: strings.Repeat("a", 400)},
	), time.Now(), 4, collectCursorDeltas(&deltas))

	require.NoError(t, err)
	require.True(t, outcome.truncated)
	require.Equal(t, "length", outcome.finishReason)
	require.NotEmpty(t, outcome.content)
	require.Less(t, len(outcome.content), 400)
	require.LessOrEqual(t, estimateTokensForText(outcome.content), 4)
	require.Len(t, deltas, 1)
	require.Equal(t, outcome.content, deltas[0].text)
}

// Reasoning is output too: a thinking-heavy turn must not slip past the ceiling.
func TestConsumeCursorAgentEventsCountsReasoningTowardsMaxTokens(t *testing.T) {
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventThinking, Text: strings.Repeat("b", 200)},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "answer"},
	), time.Now(), 3, nil)

	require.NoError(t, err)
	require.True(t, outcome.truncated)
	require.Empty(t, outcome.content, "the ceiling was spent on reasoning")
	require.NotEmpty(t, outcome.reasoning)
}

// A tool call is all-or-nothing: half an argument object is not parseable, so
// the turn ends before it rather than shipping a fragment.
func TestConsumeCursorAgentEventsDoesNotSplitToolCallsAtCeiling(t *testing.T) {
	var deltas []cursorDelta
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "1234"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventToolCall, ToolCall: &cursorpkg.AgentToolCall{
			ID: "call_a", Name: "get_weather", Arguments: `{"city":"SF"}`,
		}},
	), time.Now(), 1, collectCursorDeltas(&deltas))

	require.NoError(t, err)
	require.True(t, outcome.truncated)
	require.Equal(t, "length", outcome.finishReason)
	require.Empty(t, outcome.toolCalls)
	require.Len(t, deltas, 1)

	// With budget left, the call is admitted whole even though its own cost
	// overshoots: the arguments must stay valid JSON.
	whole, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventToolCall, ToolCall: &cursorpkg.AgentToolCall{
			ID: "call_a", Name: "get_weather", Arguments: `{"city":"SF"}`,
		}},
	), time.Now(), 1, nil)
	require.NoError(t, err)
	require.False(t, whole.truncated)
	require.Equal(t, "tool_calls", whole.finishReason)
	require.Len(t, whole.toolCalls, 1)
	require.Equal(t, `{"city":"SF"}`, whole.toolCalls[0].Function.Arguments)
}

// A turn that finishes inside the budget is a normal stop, not a truncation.
func TestConsumeCursorAgentEventsUnderBudgetIsNotTruncated(t *testing.T) {
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "short"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventTurnEnded, Usage: &cursorpkg.AgentUsage{OutputTokens: 2}},
	), time.Now(), 4096, nil)

	require.NoError(t, err)
	require.False(t, outcome.truncated)
	require.Equal(t, "stop", outcome.finishReason)
	require.Equal(t, "short", outcome.content)
	require.NotNil(t, outcome.usage)
}

func TestCursorFitTextToTokenBudget(t *testing.T) {
	// Fits whole.
	text, cost := cursorFitTextToTokenBudget("hello", 100)
	require.Equal(t, "hello", text)
	require.Equal(t, estimateTokensForText("hello"), cost)

	// No budget at all.
	text, cost = cursorFitTextToTokenBudget("hello", 0)
	require.Empty(t, text)
	require.Zero(t, cost)

	// Cuts on a rune boundary, never mid-rune.
	text, cost = cursorFitTextToTokenBudget(strings.Repeat("\u754c", 50), 10)
	require.True(t, utf8.ValidString(text))
	require.Equal(t, 10, len([]rune(text)), "CJK text estimates one token per rune")
	require.LessOrEqual(t, cost, 10)
}

func TestCursorRequestOutputLimitReadsEitherField(t *testing.T) {
	require.Zero(t, cursorRequestOutputLimit(nil))
	require.Zero(t, cursorRequestOutputLimit(&apicompat.ChatCompletionsRequest{}))

	maxTokens := 128
	require.Equal(t, 128, cursorRequestOutputLimit(&apicompat.ChatCompletionsRequest{MaxTokens: &maxTokens}))

	// max_completion_tokens is the newer field and wins; both bridges set it.
	maxCompletion := 64
	require.Equal(t, 64, cursorRequestOutputLimit(&apicompat.ChatCompletionsRequest{
		MaxTokens: &maxTokens, MaxCompletionTokens: &maxCompletion,
	}))

	zero := 0
	require.Zero(t, cursorRequestOutputLimit(&apicompat.ChatCompletionsRequest{MaxCompletionTokens: &zero}))
}

func TestIsCursorNotLoggedIn(t *testing.T) {
	require.True(t, isCursorNotLoggedIn(&cursorpkg.AgentError{Raw: `{"code":"ERROR_NOT_LOGGED_IN"}`}))
	require.True(t, isCursorNotLoggedIn(&cursorpkg.AgentError{Message: "error_not_logged_in"}))
	require.False(t, isCursorNotLoggedIn(&cursorpkg.AgentError{Code: "resource_exhausted"}))
	require.False(t, isCursorNotLoggedIn(nil))
}

func TestCursorUpstreamErrorBodyPrefersRawUpstreamPayload(t *testing.T) {
	raw := `{"code":"invalid_argument","message":"bad model"}`
	require.Equal(t, raw, string(cursorUpstreamErrorBody(&cursorpkg.AgentError{
		Code: "invalid_argument", Message: "bad model", Raw: raw,
	})))

	// No upstream payload: the formatted verdict is what passthrough rules see.
	body := string(cursorUpstreamErrorBody(&cursorpkg.AgentError{Code: "internal", HTTPStatus: 502}))
	require.Contains(t, body, "internal")

	// Truncation must not leave a split rune behind.
	long := &cursorpkg.AgentError{Raw: strings.Repeat("\u754c", cursorUpstreamErrorBodyLimit)}
	truncated := cursorUpstreamErrorBody(long)
	require.LessOrEqual(t, len(truncated), cursorUpstreamErrorBodyLimit)
	require.True(t, utf8.Valid(truncated))
}

func TestCursorAgentRequestIDFallsBackWhenNoUpstreamHeader(t *testing.T) {
	id := cursorAgentRequestID(nil, "chatcmpl-")
	require.True(t, len(id) > len("chatcmpl-"))
	require.Contains(t, id, "chatcmpl-")
}

func TestCursorChunkSynthesizerDeltaMapping(t *testing.T) {
	var chunks []*apicompat.ChatCompletionsChunk
	synth := newCursorChunkSynthesizer("claude-4.5-sonnet", func(chunk *apicompat.ChatCompletionsChunk) {
		chunks = append(chunks, chunk)
	})

	synth.onDelta(cursorDelta{kind: cursorDeltaText, text: "Hello"})
	synth.onDelta(cursorDelta{kind: cursorDeltaReasoning, text: "pondering"})
	synth.onDelta(cursorDelta{
		kind: cursorDeltaToolCall, toolIndex: 0, toolID: "call_1",
		toolName: "get_weather", toolArguments: `{"city":"SF"}`,
	})
	synth.finish("stop", OpenAIUsage{InputTokens: 10, OutputTokens: 5})

	// role chunk + text + thinking + tool call + finish
	require.Len(t, chunks, 5)

	require.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role)

	require.NotNil(t, chunks[1].Choices[0].Delta.Content)
	require.Equal(t, "Hello", *chunks[1].Choices[0].Delta.Content)

	require.NotNil(t, chunks[2].Choices[0].Delta.ReasoningContent)
	require.Equal(t, "pondering", *chunks[2].Choices[0].Delta.ReasoningContent)

	toolCalls := chunks[3].Choices[0].Delta.ToolCalls
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call_1", toolCalls[0].ID)
	require.Equal(t, "get_weather", toolCalls[0].Function.Name)
	require.Equal(t, `{"city":"SF"}`, toolCalls[0].Function.Arguments)

	final := chunks[4]
	require.NotNil(t, final.Choices[0].FinishReason)
	require.Equal(t, "stop", *final.Choices[0].FinishReason)
	require.NotNil(t, final.Usage)
	require.Equal(t, 10, final.Usage.PromptTokens)
	require.Equal(t, 5, final.Usage.CompletionTokens)
	require.Equal(t, 15, final.Usage.TotalTokens)

	// All chunks share the same completion id and model.
	for _, chunk := range chunks {
		require.Equal(t, chunks[0].ID, chunk.ID)
		require.Equal(t, "claude-4.5-sonnet", chunk.Model)
	}
}

// A downstream writer that gives up (client gone) must stop the turn rather
// than being called for every remaining frame.
func TestConsumeCursorAgentEventsStopsOnDeltaError(t *testing.T) {
	writeErr := errors.New("client disconnected")
	calls := 0
	outcome, err := consumeCursorAgentEvents(agentEvents(
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "one"},
		cursorpkg.AgentEvent{Type: cursorpkg.AgentEventText, Text: "two"},
	), time.Now(), 0, func(cursorDelta) error {
		calls++
		return writeErr
	})
	require.ErrorIs(t, err, writeErr)
	require.Equal(t, 1, calls)
	// What was already delivered still has to be billable.
	require.Equal(t, "one", outcome.content)
}
