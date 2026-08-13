package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// cursorChunkSynthesizer turns Cursor deltas into synthetic OpenAI Chat
// Completions chunks so downstream apicompat state machines (CC→Responses,
// CC→Anthropic) can be reused without a real CC upstream.
type cursorChunkSynthesizer struct {
	completionID string
	created      int64
	model        string
	roleSent     bool
	emit         func(*apicompat.ChatCompletionsChunk)
}

func newCursorChunkSynthesizer(model string, emit func(*apicompat.ChatCompletionsChunk)) *cursorChunkSynthesizer {
	return &cursorChunkSynthesizer{
		completionID: "chatcmpl-" + uuid.NewString(),
		created:      time.Now().Unix(),
		model:        model,
		emit:         emit,
	}
}

func (e *cursorChunkSynthesizer) chunk(delta apicompat.ChatDelta, finishReason *string, usage *apicompat.ChatUsage) *apicompat.ChatCompletionsChunk {
	return &apicompat.ChatCompletionsChunk{
		ID:      e.completionID,
		Object:  "chat.completion.chunk",
		Created: e.created,
		Model:   e.model,
		Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: delta, FinishReason: finishReason}},
		Usage:   usage,
	}
}

// onDelta maps a single cursor increment onto CC chunk(s).
func (e *cursorChunkSynthesizer) onDelta(delta cursorDelta) {
	if !e.roleSent {
		e.emit(e.chunk(apicompat.ChatDelta{Role: "assistant"}, nil, nil))
		e.roleSent = true
	}
	switch delta.kind {
	case cursorDeltaText:
		text := delta.text
		e.emit(e.chunk(apicompat.ChatDelta{Content: &text}, nil, nil))
	case cursorDeltaReasoning:
		reasoning := delta.text
		e.emit(e.chunk(apicompat.ChatDelta{ReasoningContent: &reasoning}, nil, nil))
	case cursorDeltaToolCall:
		e.emit(e.chunk(apicompat.ChatDelta{
			ToolCalls: []apicompat.ChatToolCall{cursorToolCallDelta(delta)},
		}, nil, nil))
	}
}

// finish emits the terminal chunk carrying finish_reason and the resolved usage
// (upstream accounting when the turn reported it, local estimate otherwise).
func (e *cursorChunkSynthesizer) finish(finishReason string, usage OpenAIUsage) {
	reason := finishReason
	e.emit(e.chunk(apicompat.ChatDelta{}, &reason, &apicompat.ChatUsage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
	}))
}

// cursorChatCompletionsResponse builds a full CC response object from an
// aggregated cursor outcome (shared by every buffered path).
func cursorChatCompletionsResponse(model string, outcome cursorChatOutcome, usage OpenAIUsage) *apicompat.ChatCompletionsResponse {
	message := apicompat.ChatMessage{Role: "assistant"}
	if outcome.content != "" {
		if encoded, err := json.Marshal(outcome.content); err == nil {
			message.Content = json.RawMessage(encoded)
		}
	}
	if outcome.reasoning != "" {
		message.ReasoningContent = outcome.reasoning
	}
	if len(outcome.toolCalls) > 0 {
		message.ToolCalls = outcome.toolCalls
	}
	return &apicompat.ChatCompletionsResponse{
		ID:      "chatcmpl-" + uuid.NewString(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []apicompat.ChatChoice{{
			Index:        0,
			Message:      message,
			FinishReason: outcome.finishReason,
		}},
		Usage: &apicompat.ChatUsage{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.InputTokens + usage.OutputTokens,
		},
	}
}

// forwardCursorResponses serves an OpenAI Responses API request through a
// Cursor account: Responses → Chat Completions → agent.v1 Run, then the agent
// stream is synthesized back into CC chunks and replayed through the shared
// CC→Responses stream state machine.
func (s *OpenAIGatewayService) forwardCursorResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	originalModel string,
	reqStream bool,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("cursor: parse responses request: %w", err)
	}
	if strings.TrimSpace(originalModel) == "" {
		originalModel = strings.TrimSpace(responsesReq.Model)
	}
	if originalModel == "" {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("cursor: missing model in responses request")
	}

	effectiveTools, err := apicompat.EffectiveResponsesTools(&responsesReq)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("cursor: resolve responses tools: %w", err)
	}
	customTools := apicompat.CustomToolNames(effectiveTools)
	toolSearch := apicompat.HasToolSearchTool(effectiveTools)
	namespaceTools := apicompat.NamespaceToolNames(effectiveTools)

	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("cursor: convert responses to chat completions: %w", err)
	}

	meta := s.resolveCursorChatMeta(account, originalModel, "", reqStream)
	meta.maxOutputTokens = cursorRequestOutputLimit(chatReq)
	chatReq.Model = meta.upstreamModel

	params, input, err := buildCursorAgentRun(account, meta.upstreamModel, chatReq)
	if err != nil {
		writeOpenAIResponsesFallbackError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}

	stream, err := s.openCursorAgentStream(ctx, c, account, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	requestID := cursorAgentRequestID(stream, "resp-")

	if !reqStream {
		outcome, consumeErr := consumeCursorAgentEvents(stream.Events(), startTime, meta.maxOutputTokens, nil)
		if consumeErr != nil {
			return nil, s.cursorAgentFailure(c, account, consumeErr)
		}
		usage := resolveCursorUsage(input, outcome)
		ccResp := cursorChatCompletionsResponse(meta.originalModel, outcome, usage)
		responsesResp := apicompat.ChatCompletionsResponseToResponses(ccResp, meta.originalModel, customTools, toolSearch, namespaceTools)
		c.JSON(http.StatusOK, responsesResp)
		return &OpenAIForwardResult{
			RequestID:        requestID,
			Usage:            usage,
			Model:            meta.originalModel,
			BillingModel:     meta.billingModel,
			UpstreamModel:    meta.upstreamModel,
			UpstreamEndpoint: cursorAgentEndpoint,
			Stream:           false,
			Duration:         time.Since(startTime),
			FirstTokenMs:     outcome.firstTokenMs,
		}, nil
	}

	state := apicompat.NewChatCompletionsToResponsesStreamState(meta.originalModel)
	state.CustomTools = customTools
	state.ToolSearchDeclared = toolSearch
	state.NamespaceTools = namespaceTools

	headersWritten := false
	clientDisconnected := false
	ensureHeaders := func() {
		if headersWritten {
			return
		}
		writeCursorSSEHeaders(c)
		headersWritten = true
	}
	writeEvents := func(events []apicompat.ResponsesStreamEvent) {
		if clientDisconnected || len(events) == 0 {
			return
		}
		ensureHeaders()
		for _, event := range events {
			sse, sseErr := apicompat.ResponsesEventToSSE(event)
			if sseErr != nil {
				continue
			}
			if _, werr := fmt.Fprint(c.Writer, sse); werr != nil {
				clientDisconnected = true
				return
			}
		}
		c.Writer.Flush()
	}

	synth := newCursorChunkSynthesizer(meta.originalModel, func(chunk *apicompat.ChatCompletionsChunk) {
		writeEvents(apicompat.ChatCompletionsChunkToResponsesEvents(chunk, state))
	})
	outcome, consumeErr := consumeCursorAgentEvents(stream.Events(), startTime, meta.maxOutputTokens, func(delta cursorDelta) error {
		synth.onDelta(delta)
		return nil
	})
	upstreamError := ""
	if consumeErr != nil {
		if !headersWritten {
			return nil, s.cursorAgentFailure(c, account, consumeErr)
		}
		upstreamError = s.reportCursorStreamFailure(c, account, requestID, consumeErr)
	}

	usage := resolveCursorUsage(input, outcome)
	if upstreamError != "" {
		// The turn died mid-flight, so it gets an error event rather than a
		// completed/incomplete response envelope that would read as a clean end.
		if !clientDisconnected {
			ensureHeaders()
			_, _ = fmt.Fprint(c.Writer, cursorResponsesStreamErrorSSE(upstreamError))
			_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
		}
	} else {
		synth.finish(outcome.finishReason, usage)
		writeEvents(apicompat.FinalizeChatCompletionsResponsesStream(state))
		if !clientDisconnected {
			ensureHeaders()
			_, _ = fmt.Fprint(c.Writer, "data: [DONE]\n\n")
			c.Writer.Flush()
		}
	}

	return &OpenAIForwardResult{
		RequestID:        requestID,
		Usage:            usage,
		Model:            meta.originalModel,
		BillingModel:     meta.billingModel,
		UpstreamModel:    meta.upstreamModel,
		UpstreamEndpoint: cursorAgentEndpoint,
		Stream:           true,
		Duration:         time.Since(startTime),
		FirstTokenMs:     outcome.firstTokenMs,
	}, nil
}

// forwardCursorAnthropic serves an Anthropic Messages request through a Cursor
// account: Anthropic → Chat Completions → agent.v1 Run, then back through the
// shared CC→Anthropic stream state machine.
func (s *OpenAIGatewayService) forwardCursorAnthropic(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	var anthropicReq apicompat.AnthropicRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("cursor: parse anthropic request: %w", err)
	}
	originalModel := anthropicReq.Model
	if strings.TrimSpace(originalModel) == "" {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
		return nil, fmt.Errorf("cursor: missing model in anthropic request")
	}
	// max_tokens is required by the Messages API, and on this path it is also
	// load-bearing: the agent protocol has no output ceiling, so it is the only
	// thing that bounds the answer. Accepting the request without it would return
	// an unbounded reply under a spec that promises the opposite.
	if anthropicReq.MaxTokens <= 0 {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", "max_tokens: field required")
		return nil, fmt.Errorf("cursor: anthropic request is missing max_tokens")
	}
	clientStream := anthropicReq.Stream

	chatReq, err := apicompat.AnthropicToChatCompletionsRequest(&anthropicReq)
	if err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, fmt.Errorf("cursor: convert anthropic to chat completions: %w", err)
	}

	meta := s.resolveCursorChatMeta(account, originalModel, defaultMappedModel, clientStream)
	meta.maxOutputTokens = cursorRequestOutputLimit(chatReq)
	if meta.maxOutputTokens <= 0 {
		// The bridge normally carries max_tokens across as
		// max_completion_tokens; fall back to the inbound value so a mapping
		// change cannot silently disable the ceiling.
		meta.maxOutputTokens = anthropicReq.MaxTokens
	}
	chatReq.Model = meta.upstreamModel

	params, input, err := buildCursorAgentRun(account, meta.upstreamModel, chatReq)
	if err != nil {
		writeAnthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}

	stream, err := s.openCursorAgentStream(ctx, c, account, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	requestID := cursorAgentRequestID(stream, "msg-")

	if !clientStream {
		outcome, consumeErr := consumeCursorAgentEvents(stream.Events(), startTime, meta.maxOutputTokens, nil)
		if consumeErr != nil {
			return nil, s.cursorAgentFailure(c, account, consumeErr)
		}
		usage := resolveCursorUsage(input, outcome)
		ccResp := cursorChatCompletionsResponse(meta.originalModel, outcome, usage)
		anthropicResp := apicompat.ChatCompletionsResponseToAnthropic(ccResp, meta.originalModel)
		c.JSON(http.StatusOK, anthropicResp)
		return &OpenAIForwardResult{
			RequestID:        requestID,
			Usage:            usage,
			Model:            meta.originalModel,
			BillingModel:     meta.billingModel,
			UpstreamModel:    meta.upstreamModel,
			UpstreamEndpoint: cursorAgentEndpoint,
			Stream:           false,
			Duration:         time.Since(startTime),
			FirstTokenMs:     outcome.firstTokenMs,
		}, nil
	}

	state := apicompat.NewChatCompletionsToAnthropicStreamState(meta.originalModel)
	headersWritten := false
	clientDisconnected := false
	ensureHeaders := func() {
		if headersWritten {
			return
		}
		writeCursorSSEHeaders(c)
		headersWritten = true
	}
	writeEvents := func(events []apicompat.AnthropicStreamEvent) {
		if clientDisconnected || len(events) == 0 {
			return
		}
		ensureHeaders()
		for _, event := range events {
			sse, sseErr := apicompat.ResponsesAnthropicEventToSSE(event)
			if sseErr != nil {
				continue
			}
			if _, werr := fmt.Fprint(c.Writer, sse); werr != nil {
				clientDisconnected = true
				return
			}
		}
		c.Writer.Flush()
	}

	synth := newCursorChunkSynthesizer(meta.originalModel, func(chunk *apicompat.ChatCompletionsChunk) {
		writeEvents(apicompat.ChatCompletionsChunkToAnthropicEvents(chunk, state))
	})
	outcome, consumeErr := consumeCursorAgentEvents(stream.Events(), startTime, meta.maxOutputTokens, func(delta cursorDelta) error {
		synth.onDelta(delta)
		return nil
	})
	upstreamError := ""
	if consumeErr != nil {
		if !headersWritten {
			return nil, s.cursorAgentFailure(c, account, consumeErr)
		}
		upstreamError = s.reportCursorStreamFailure(c, account, requestID, consumeErr)
	}

	usage := resolveCursorUsage(input, outcome)
	if upstreamError != "" {
		// Anthropic's own upstream ends a broken stream with an error event and
		// no message_stop; synthesizing message_delta + message_stop here would
		// present the truncated answer as a finished one.
		if !clientDisconnected {
			ensureHeaders()
			_, _ = fmt.Fprint(c.Writer, buildAnthropicStreamErrorSSE("api_error", upstreamError))
			c.Writer.Flush()
		}
	} else {
		synth.finish(outcome.finishReason, usage)
		writeEvents(apicompat.FinalizeChatCompletionsAnthropicStream(state))
		if !clientDisconnected {
			c.Writer.Flush()
		}
	}

	return &OpenAIForwardResult{
		RequestID:        requestID,
		Usage:            usage,
		Model:            meta.originalModel,
		BillingModel:     meta.billingModel,
		UpstreamModel:    meta.upstreamModel,
		UpstreamEndpoint: cursorAgentEndpoint,
		Stream:           true,
		Duration:         time.Since(startTime),
		FirstTokenMs:     outcome.firstTokenMs,
	}, nil
}

// writeCursorSSEHeaders sets the standard SSE response headers for cursor
// streams (headers deferred until the first upstream byte).
func writeCursorSSEHeaders(c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)
}
