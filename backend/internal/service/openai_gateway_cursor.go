package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Cursor conversations run on agent.v1.AgentService/Run (api5), not on api2's
// ChatService: Cursor retired StreamUnifiedChatWithTools and now answers it
// with "Update Required" for every client version. api2 is still used for
// AvailableModels, which is why the account keeps two base URLs.
//
// Run is a bidirectional HTTP/2 stream with a paced request-frame sequence, so
// the whole transport lives in pkg/cursor's AgentStream; this file only maps
// OpenAI shapes onto it and back.

// cursorAgentEndpoint is the ops label for the Cursor conversation RPC.
const cursorAgentEndpoint = "cursor:" + cursorpkg.EndpointAgentRun

// cursorChatMeta carries the resolved model identity and stream flag for a
// single Cursor chat turn.
type cursorChatMeta struct {
	originalModel string
	billingModel  string
	upstreamModel string
	stream        bool
	// maxOutputTokens is the client's output ceiling (max_tokens /
	// max_completion_tokens / max_output_tokens), or 0 for none. The agent
	// protocol has no such field, so it is enforced locally while relaying.
	maxOutputTokens int
}

// cursorDeltaKind classifies an increment handed to a downstream writer.
type cursorDeltaKind int

const (
	cursorDeltaText cursorDeltaKind = iota
	cursorDeltaReasoning
	cursorDeltaToolCall
)

// cursorDelta is one user-visible increment, normalized away from the agent
// protocol so the SSE writer and the bridge synthesizers share one shape.
type cursorDelta struct {
	kind cursorDeltaKind
	// text is the content or reasoning fragment.
	text string
	// toolIndex is the stable per-call index within the turn.
	toolIndex     int
	toolID        string
	toolName      string
	toolArguments string
}

// cursorChatOutcome is the normalized result of consuming the upstream stream,
// shared by every downstream protocol adapter.
type cursorChatOutcome struct {
	content      string
	reasoning    string
	toolCalls    []apicompat.ChatToolCall
	finishReason string
	firstTokenMs *int
	// usage is the upstream's own accounting, when the turn ended with a
	// TurnEndedUpdate. It is often absent — see resolveCursorUsage.
	usage *cursorpkg.AgentUsage
	// truncated reports that this gateway cut the output short to honour the
	// client's max_tokens. Billing must then ignore any upstream output count,
	// which would include text the client never received.
	truncated bool
}

// forwardCursorChatCompletions serves an OpenAI Chat Completions request through
// a Cursor account by translating to agent.v1.AgentService/Run and back.
func (s *OpenAIGatewayService) forwardCursorChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	startTime := time.Now()

	var chatReq apicompat.ChatCompletionsRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
		return nil, fmt.Errorf("cursor: parse chat completions request: %w", err)
	}
	meta := s.resolveCursorChatMeta(account, chatReq.Model, defaultMappedModel, chatReq.Stream)
	meta.maxOutputTokens = cursorRequestOutputLimit(&chatReq)

	params, input, err := buildCursorAgentRun(account, meta.upstreamModel, &chatReq)
	if err != nil {
		writeChatCompletionsError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return nil, err
	}

	stream, err := s.openCursorAgentStream(ctx, c, account, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	if meta.stream {
		return s.streamCursorChatCompletions(c, account, stream, meta, input, startTime)
	}
	return s.bufferCursorChatCompletions(c, account, stream, meta, input, startTime)
}

// resolveCursorChatMeta resolves the billing/upstream model identity reusing the
// account model_mapping, matching the Grok raw path.
//
// Deliberately not normalizeOpenAIModelForUpstream: that is the Codex model
// table, and for an OAuth account it rewrites ids to whatever Codex serves —
// gpt-5 becomes gpt-5.4, and its normalization drops the "-max" suffix that
// cursorAgentWireModel needs to set max mode. Cursor's own model ids are
// authoritative here; the only line-level mapping the agent protocol wants is
// auto → default, which cursorAgentWireModel applies. Anything else and the turn
// either asks for a model this account cannot address or silently loses max mode.
func (s *OpenAIGatewayService) resolveCursorChatMeta(account *Account, requestedModel, defaultMappedModel string, stream bool) cursorChatMeta {
	billingModel := resolveOpenAIForwardModel(account, requestedModel, defaultMappedModel)
	return cursorChatMeta{
		originalModel: requestedModel,
		billingModel:  billingModel,
		upstreamModel: strings.TrimSpace(billingModel),
		stream:        stream,
	}
}

// openCursorAgentStream resolves the account credential and opens one Run turn.
// It maps credential and transport failures onto the gateway failover contract.
//
// The agent endpoint authenticates the bearer token alone: no checksum, no
// x-cursor-* identity block, and therefore no place to apply the account's
// header overrides — BuildAgentHeaders emits exactly the ten headers the CLI
// sends, and adding to them is observable upstream.
func (s *OpenAIGatewayService) openCursorAgentStream(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	params cursorpkg.AgentRunParams,
) (*cursorpkg.AgentStream, error) {
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		if account.IsCursorOAuth() {
			return nil, s.newCursorCredentialFailover(c, account, err)
		}
		return nil, err
	}
	if strings.TrimSpace(token) == "" {
		return nil, s.newCursorCredentialFailover(c, account, errCursorAccessTokenMissing)
	}

	baseURL, accountOverride := cursorAgentBaseURLSource(account)
	if err := validateCursorAgentHost(s.cfg, baseURL, accountOverride); err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	httpClient, err := cursorAgentHTTPClient(account)
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}

	SetActualOpenAIUpstreamEndpoint(c, cursorAgentEndpoint)

	defaults := cursorAgentProcessDefaults()
	logger.L().Debug("cursor agent turn: opening",
		zap.Int64("account_id", cursorAccountID(account)),
		zap.String("wire_model", params.Model),
		zap.Bool("max_mode", params.MaxMode),
		zap.Int("prompt_bytes", len(params.Prompt)),
		zap.Int("system_bytes", len(params.SystemPrompt)),
		zap.Int("tool_count", len(params.Tools)),
		zap.Int("image_count", len(params.Images)),
	)
	stream, err := cursorpkg.OpenAgentStream(ctx, params, cursorpkg.AgentStreamOptions{
		BaseURL:          baseURL,
		Token:            token,
		ClientVersion:    cursorAgentClientVersion(account),
		GhostMode:        cursorAgentGhostMode(account),
		RequestID:        uuid.NewString(),
		HTTPClient:       httpClient,
		FirstByteTimeout: defaults.firstByteTimeout,
		IdleTimeout:      defaults.idleTimeout,
	})
	if err != nil {
		return nil, s.cursorAgentFailure(c, account, err)
	}
	// Successful authenticated turn: opportunistically refresh this account's
	// observed model list so the public /v1/models union stays current.
	s.scheduleCursorObservedModelsSync(account)
	return stream, nil
}

// consumeCursorAgentEvents drains an agent turn, invoking onDelta for each
// user-visible increment and aggregating the full outcome.
//
// maxOutputTokens is the client's output ceiling, or 0 for none. The agent
// protocol carries no equivalent field, so the ceiling is applied here: the turn
// stops at the first increment that would exceed it, and the caller closing the
// stream is what cancels the upstream. Without this a client's max_tokens was
// silently dropped and finish_reason was always "stop".
//
// It takes the channel rather than the stream so the mapping is testable
// without an upstream; callers still own closing the stream.
func consumeCursorAgentEvents(
	events <-chan cursorpkg.AgentEvent,
	startTime time.Time,
	maxOutputTokens int,
	onDelta func(cursorDelta) error,
) (cursorChatOutcome, error) {
	outcome := cursorChatOutcome{finishReason: "stop"}
	var contentBuilder, reasoningBuilder strings.Builder
	toolIndexByID := make(map[string]int)
	spentTokens := 0
	limited := maxOutputTokens > 0

	// admit returns the leading part of a text increment that still fits within
	// the budget, and whether anything had to be dropped. The budget is spent per
	// increment rather than recomputed over the whole output, which keeps a long
	// answer from costing O(n²) to relay.
	admit := func(text string) (string, bool) {
		if !limited || text == "" {
			return text, false
		}
		remaining := maxOutputTokens - spentTokens
		if remaining <= 0 {
			return "", true
		}
		fitted, cost := cursorFitTextToTokenBudget(text, remaining)
		spentTokens += cost
		return fitted, fitted != text
	}

	markFirstToken := func() {
		if outcome.firstTokenMs == nil {
			ms := int(time.Since(startTime).Milliseconds())
			outcome.firstTokenMs = &ms
		}
	}
	emit := func(delta cursorDelta) error {
		if onDelta == nil {
			return nil
		}
		return onDelta(delta)
	}
	// Every exit carries the output produced so far: a turn that fails midway
	// still has to bill for what already reached the client.
	finish := func(err error) (cursorChatOutcome, error) {
		outcome.content = contentBuilder.String()
		outcome.reasoning = reasoningBuilder.String()
		return outcome, err
	}
	// truncate ends the turn at the client's ceiling. "length" is what the
	// OpenAI paths report; the Anthropic and Responses bridges translate it into
	// stop_reason "max_tokens" and incomplete/max_output_tokens respectively.
	truncate := func() (cursorChatOutcome, error) {
		outcome.truncated = true
		outcome.finishReason = "length"
		return finish(nil)
	}

	for event := range events {
		switch event.Type {
		case cursorpkg.AgentEventText:
			if event.Text == "" {
				continue
			}
			fitted, dropped := admit(event.Text)
			if fitted != "" {
				markFirstToken()
				contentBuilder.WriteString(fitted)
				if err := emit(cursorDelta{kind: cursorDeltaText, text: fitted}); err != nil {
					return finish(err)
				}
			}
			if dropped {
				return truncate()
			}

		case cursorpkg.AgentEventThinking:
			if event.Text == "" {
				continue
			}
			fitted, dropped := admit(event.Text)
			if fitted != "" {
				markFirstToken()
				reasoningBuilder.WriteString(fitted)
				if err := emit(cursorDelta{kind: cursorDeltaReasoning, text: fitted}); err != nil {
					return finish(err)
				}
			}
			if dropped {
				return truncate()
			}

		case cursorpkg.AgentEventToolCall:
			if event.ToolCall == nil {
				continue
			}
			if limited {
				// A tool call cannot be emitted in part — half an argument object
				// is not parseable JSON — so it is either admitted whole or the
				// turn ends before it.
				if maxOutputTokens-spentTokens <= 0 {
					return truncate()
				}
				spentTokens += estimateTokensForText(event.ToolCall.Name + event.ToolCall.Arguments)
			}
			markFirstToken()
			// Unlike api2, a native MCP call arrives complete in one frame, so
			// there is nothing to reassemble across deltas.
			index, existed := toolIndexByID[event.ToolCall.ID]
			if !existed {
				index = len(outcome.toolCalls)
				toolIndexByID[event.ToolCall.ID] = index
				outcome.toolCalls = append(outcome.toolCalls, apicompat.ChatToolCall{
					Index: intPtr(index),
					ID:    event.ToolCall.ID,
					Type:  "function",
					Function: apicompat.ChatFunctionCall{
						Name:      event.ToolCall.Name,
						Arguments: event.ToolCall.Arguments,
					},
				})
			}
			outcome.finishReason = "tool_calls"
			if err := emit(cursorDelta{
				kind:          cursorDeltaToolCall,
				toolIndex:     index,
				toolID:        event.ToolCall.ID,
				toolName:      event.ToolCall.Name,
				toolArguments: event.ToolCall.Arguments,
			}); err != nil {
				return finish(err)
			}

		case cursorpkg.AgentEventTurnEnded:
			outcome.usage = event.Usage

		case cursorpkg.AgentEventError:
			return finish(event.Err)

		default:
			// Heartbeats, thinking_end, token deltas and Cursor's own agentic
			// tool calls (shell/read/edit) carry nothing an OpenAI client can
			// act on: this gateway never services them, so they are skipped.
		}
	}
	return finish(nil)
}

// reportCursorStreamFailure records an upstream failure that arrived after the
// response was already committed.
//
// Failover is no longer possible and the status line already says 200, so the
// only thing left is to tell the truth in band and in the logs. Previously this
// case only logged and then emitted a normal finish_reason plus [DONE]: the
// client was told a truncated answer was complete, and ops saw a success.
func (s *OpenAIGatewayService) reportCursorStreamFailure(c *gin.Context, account *Account, requestID string, err error) string {
	message := sanitizeStreamError(err)
	status := http.StatusBadGateway
	connectCode := ""
	var agentErr *cursorpkg.AgentError
	if errors.As(err, &agentErr) {
		connectCode = agentErr.Code
		if agentErr.HTTPStatus > 0 {
			status = agentErr.HTTPStatus
		} else if mapped := cursorpkg.ConnectCodeToHTTPStatus(agentErr.Code); mapped > 0 {
			status = mapped
		}
	}
	logger.L().Warn("cursor agent stream: upstream error after first byte",
		zap.String("request_id", requestID),
		zap.Int64("account_id", cursorAccountID(account)),
		zap.String("connect_code", connectCode),
		zap.Int("mapped_status", status),
		zap.Error(err),
	)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           PlatformCursor,
		AccountID:          cursorAccountID(account),
		AccountName:        cursorAccountName(account),
		UpstreamStatusCode: status,
		Stage:              "upstream",
		Kind:               "stream_error",
		Message:            message,
	})
	// CountTowardsSLA: the turn failed, even though the wire says 200.
	MarkOpsStreamFailure(c, "upstream_error", "cursor_stream_failed", message, status)
	return message
}

// cursorChatStreamErrorSSE renders an OpenAI Chat Completions in-band error.
func cursorChatStreamErrorSSE(message string) string {
	payload, err := json.Marshal(gin.H{
		"error": gin.H{"type": "upstream_error", "message": message},
	})
	if err != nil {
		return "data: {\"error\":{\"type\":\"upstream_error\",\"message\":\"cursor upstream error\"}}\n\n"
	}
	return "data: " + string(payload) + "\n\n"
}

// cursorResponsesStreamErrorSSE renders a Responses API `error` event, the shape
// an OpenAI SDK recognizes as a stream that failed rather than one that ended.
func cursorResponsesStreamErrorSSE(message string) string {
	payload, err := json.Marshal(gin.H{
		"type":    "error",
		"code":    "upstream_error",
		"message": message,
	})
	if err != nil {
		return "event: error\ndata: {\"type\":\"error\",\"code\":\"upstream_error\",\"message\":\"cursor upstream error\"}\n\n"
	}
	return "event: error\ndata: " + string(payload) + "\n\n"
}

// cursorFitTextToTokenBudget returns the longest prefix of text whose estimated
// token count fits in budget, plus what that prefix costs.
//
// The search is over runes rather than bytes so a cut never splits one, and it
// is a binary search because estimateTokensForText inspects the whole string
// (its per-token ratio depends on how much of the text is ASCII), so there is no
// per-rune cost to walk.
func cursorFitTextToTokenBudget(text string, budget int) (string, int) {
	if budget <= 0 || text == "" {
		return "", 0
	}
	if cost := estimateTokensForText(text); cost <= budget {
		return text, cost
	}

	runes := []rune(text)
	bestLen, bestCost := 0, 0
	low, high := 1, len(runes)
	for low <= high {
		mid := (low + high) / 2
		cost := estimateTokensForText(string(runes[:mid]))
		if cost <= budget {
			bestLen, bestCost = mid, cost
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	return string(runes[:bestLen]), bestCost
}

// cursorRequestOutputLimit reads the client's output ceiling off a Chat
// Completions request. Both Anthropic's max_tokens and Responses'
// max_output_tokens arrive here as max_completion_tokens, so this one reader
// covers all three inbound protocols.
func cursorRequestOutputLimit(req *apicompat.ChatCompletionsRequest) int {
	if req == nil {
		return 0
	}
	if req.MaxCompletionTokens != nil && *req.MaxCompletionTokens > 0 {
		return *req.MaxCompletionTokens
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		return *req.MaxTokens
	}
	return 0
}

// streamCursorChatCompletions relays the Cursor stream to the client as OpenAI
// Chat Completions SSE chunks. The first client byte is withheld until the
// first upstream delta so upstream errors can still map to a clean status.
func (s *OpenAIGatewayService) streamCursorChatCompletions(
	c *gin.Context,
	account *Account,
	stream *cursorpkg.AgentStream,
	meta cursorChatMeta,
	input cursorInputEstimate,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := cursorAgentRequestID(stream, "chatcmpl-")
	completionID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	headersWritten := false
	clientDisconnected := false
	roleSent := false

	ensureHeaders := func() {
		if headersWritten {
			return
		}
		writeCursorSSEHeaders(c)
		headersWritten = true
	}

	writeChunk := func(chunk apicompat.ChatCompletionsChunk) {
		if clientDisconnected {
			return
		}
		payload, err := json.Marshal(chunk)
		if err != nil {
			return
		}
		ensureHeaders()
		if _, werr := c.Writer.WriteString("data: " + string(payload) + "\n\n"); werr != nil {
			clientDisconnected = true
			return
		}
		c.Writer.Flush()
	}

	emitDelta := func(delta apicompat.ChatDelta) {
		writeChunk(apicompat.ChatCompletionsChunk{
			ID:      completionID,
			Object:  "chat.completion.chunk",
			Created: created,
			Model:   meta.originalModel,
			Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: delta}},
		})
	}

	outcome, err := consumeCursorAgentEvents(stream.Events(), startTime, meta.maxOutputTokens, func(delta cursorDelta) error {
		if !roleSent {
			emitDelta(apicompat.ChatDelta{Role: "assistant"})
			roleSent = true
		}
		switch delta.kind {
		case cursorDeltaText:
			text := delta.text
			emitDelta(apicompat.ChatDelta{Content: &text})
		case cursorDeltaReasoning:
			reasoning := delta.text
			emitDelta(apicompat.ChatDelta{ReasoningContent: &reasoning})
		case cursorDeltaToolCall:
			emitDelta(apicompat.ChatDelta{ToolCalls: []apicompat.ChatToolCall{cursorToolCallDelta(delta)}})
		}
		return nil
	})
	upstreamError := ""
	if err != nil {
		if !headersWritten {
			// No client bytes yet: surface as failover so another account can try.
			return nil, s.cursorAgentFailure(c, account, err)
		}
		upstreamError = s.reportCursorStreamFailure(c, account, requestID, err)
	}

	if !clientDisconnected {
		ensureHeaders()
		if upstreamError != "" {
			// No synthetic finish_reason: the answer is incomplete, and saying
			// "stop" here would be a lie the client cannot detect.
			_, _ = c.Writer.WriteString(cursorChatStreamErrorSSE(upstreamError))
		} else {
			finish := outcome.finishReason
			final := apicompat.ChatCompletionsChunk{
				ID:      completionID,
				Object:  "chat.completion.chunk",
				Created: created,
				Model:   meta.originalModel,
				Choices: []apicompat.ChatChunkChoice{{Index: 0, Delta: apicompat.ChatDelta{}, FinishReason: &finish}},
			}
			if payload, mErr := json.Marshal(final); mErr == nil {
				_, _ = c.Writer.WriteString("data: " + string(payload) + "\n\n")
			}
		}
		_, _ = c.Writer.WriteString("data: [DONE]\n\n")
		c.Writer.Flush()
	}

	usage := resolveCursorUsage(input, outcome)
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

// bufferCursorChatCompletions aggregates the Cursor stream into a single OpenAI
// chat.completion response.
func (s *OpenAIGatewayService) bufferCursorChatCompletions(
	c *gin.Context,
	account *Account,
	stream *cursorpkg.AgentStream,
	meta cursorChatMeta,
	input cursorInputEstimate,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := cursorAgentRequestID(stream, "chatcmpl-")
	outcome, err := consumeCursorAgentEvents(stream.Events(), startTime, meta.maxOutputTokens, nil)
	if err != nil {
		return nil, s.cursorAgentFailure(c, account, err)
	}

	usage := resolveCursorUsage(input, outcome)
	chatResp := cursorChatCompletionsResponse(meta.originalModel, outcome, usage)

	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Writer.WriteHeader(http.StatusOK)
	if payload, mErr := json.Marshal(chatResp); mErr == nil {
		_, _ = c.Writer.Write(payload)
	}

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

// cursorToolCallDelta renders one tool-call increment as an OpenAI delta. The
// agent protocol delivers the whole call at once, so name and arguments travel
// together instead of being streamed apart.
func cursorToolCallDelta(delta cursorDelta) apicompat.ChatToolCall {
	return apicompat.ChatToolCall{
		Index:    intPtr(delta.toolIndex),
		ID:       delta.toolID,
		Type:     "function",
		Function: apicompat.ChatFunctionCall{Name: delta.toolName, Arguments: delta.toolArguments},
	}
}

// cursorAgentRequestID prefers the upstream's own request id so a report can be
// correlated with Cursor's logs, falling back to a locally minted one.
func cursorAgentRequestID(stream *cursorpkg.AgentStream, prefix string) string {
	if stream != nil {
		if resp := stream.Response(); resp != nil {
			if id := strings.TrimSpace(resp.Header.Get("x-request-id")); id != "" {
				return id
			}
		}
	}
	return prefix + uuid.NewString()
}

// cursorAgentFailure maps an agent-turn failure onto the failover contract,
// distinguishing Connect-coded upstream verdicts from transport faults. It
// covers both the open handshake and a stream that failed before any client
// byte was written.
func (s *OpenAIGatewayService) cursorAgentFailure(c *gin.Context, account *Account, err error) error {
	var agentErr *cursorpkg.AgentError
	if !errors.As(err, &agentErr) {
		// A transport fault carries no Connect code, so this is the only record
		// of what actually went wrong.
		logger.L().Warn("cursor agent turn failed: transport",
			zap.Int64("account_id", cursorAccountID(account)),
			zap.Error(err),
		)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           PlatformCursor,
			AccountID:          cursorAccountID(account),
			UpstreamStatusCode: http.StatusBadGateway,
			Kind:               "failover",
			Message:            err.Error(),
		})
		// The turn never reached Cursor's application layer: a TLS handshake
		// that timed out, a reset connection, response headers that never
		// arrived. api5 produces these intermittently and they say nothing
		// about the account, so the turn is retried in place. Without this a
		// single flaky handshake burns the account for the whole request and a
		// one-account pool answers 502 immediately.
		retryable := !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		return &UpstreamFailoverError{
			StatusCode:             http.StatusBadGateway,
			RetryableOnSameAccount: retryable,
			RequestScopedTransient: retryable,
		}
	}

	status := agentErr.HTTPStatus
	if status == 0 {
		status = cursorpkg.ConnectCodeToHTTPStatus(agentErr.Code)
	}
	// Codes this package has no mapping for all collapse to 502, so the verdict
	// itself is logged: without it a 502 is indistinguishable from a hang.
	logger.L().Warn("cursor agent turn failed: upstream verdict",
		zap.Int64("account_id", cursorAccountID(account)),
		zap.String("connect_code", agentErr.Code),
		zap.String("upstream_message", agentErr.Message),
		zap.String("upstream_raw", agentErr.Raw),
		zap.Int("mapped_status", status),
	)
	if isCursorNotLoggedIn(agentErr) {
		return s.cursorNotLoggedInFailure(c, account, agentErr)
	}
	// permission_denied is Cursor's answer to both an expired credential and a
	// client-version it refuses to serve. Only the former is an account fault;
	// mistaking the latter for one walks the whole pool and disables every
	// account over a single wrong config value.
	if isCursorClientVersionRejected(agentErr) {
		return s.cursorClientVersionFailure(c, account, agentErr)
	}

	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		// A credential the upstream refuses: drop the cached token so the next
		// scheduling of this account re-derives it, and let another account try.
		if account.IsCursorOAuth() && s.cursorTokenProvider != nil && c != nil && c.Request != nil {
			_ = s.cursorTokenProvider.InvalidateToken(context.WithoutCancel(c.Request.Context()), account)
		}
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           PlatformCursor,
			AccountID:          cursorAccountID(account),
			AccountName:        cursorAccountName(account),
			UpstreamStatusCode: status,
			Stage:              string(GatewayFailureStageAccountAuth),
			Scope:              string(GatewayFailureScopeAccount),
			Reason:             string(CursorCredentialReasonExpired),
			Kind:               "failover",
			Message:            agentErr.Error(),
		})
		return &UpstreamFailoverError{
			Stage:             GatewayFailureStageAccountAuth,
			Scope:             GatewayFailureScopeAccount,
			Reason:            CursorCredentialReasonExpired,
			NextAccountAction: NextAccountRetry,
			ClientStatusCode:  http.StatusServiceUnavailable,
			ClientMessage:     CursorCredentialUnavailableClientMessage,
		}

	case http.StatusTooManyRequests:
		// Transient upstream capacity ("resource_exhausted"): retry another
		// account, do not bill or quarantine this one.
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:  PlatformCursor,
			AccountID: cursorAccountID(account),
			Stage:     "upstream",
			Kind:      "failover",
			Message:   agentErr.Error(),
		})
		return &UpstreamFailoverError{
			StatusCode:             http.StatusServiceUnavailable,
			RequestScopedTransient: true,
		}

	default:
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform:           PlatformCursor,
			AccountID:          cursorAccountID(account),
			AccountName:        cursorAccountName(account),
			UpstreamStatusCode: status,
			Kind:               "failover",
			Message:            agentErr.Error(),
		})
		return &UpstreamFailoverError{
			StatusCode:   status,
			ResponseBody: cursorUpstreamErrorBody(agentErr),
		}
	}
}

// cursorNotLoggedInFailure handles the verdict Cursor returns when a credential
// authenticates but is not a client token — the exact failure a browser cookie
// produces. The cached token is dropped so the next scheduling of this account
// re-derives it, which is what triggers the deep-link upgrade.
func (s *OpenAIGatewayService) cursorNotLoggedInFailure(c *gin.Context, account *Account, agentErr *cursorpkg.AgentError) error {
	if account != nil && account.IsCursorOAuth() && s.cursorTokenProvider != nil && c != nil && c.Request != nil {
		_ = s.cursorTokenProvider.InvalidateToken(context.WithoutCancel(c.Request.Context()), account)
	}
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:  PlatformCursor,
		AccountID: cursorAccountID(account),
		Stage:     string(GatewayFailureStageAccountAuth),
		Scope:     string(GatewayFailureScopeAccount),
		Reason:    string(CursorCredentialReasonWebSession),
		Kind:      "failover",
		Message:   agentErr.Error(),
	})
	return &UpstreamFailoverError{
		Stage:             GatewayFailureStageAccountAuth,
		Scope:             GatewayFailureScopeAccount,
		Reason:            CursorCredentialReasonWebSession,
		NextAccountAction: NextAccountRetry,
		ClientStatusCode:  http.StatusServiceUnavailable,
		ClientMessage:     CursorCredentialUnavailableClientMessage,
	}
}

// isCursorNotLoggedIn reports whether an upstream verdict is Cursor's
// "ERROR_NOT_LOGGED_IN", which a web cookie produces on the conversation
// endpoints even though the same cookie is accepted by AvailableModels.
func isCursorNotLoggedIn(agentErr *cursorpkg.AgentError) bool {
	if agentErr == nil {
		return false
	}
	haystack := strings.ToUpper(agentErr.Code + " " + agentErr.Message + " " + agentErr.Raw)
	return strings.Contains(haystack, "ERROR_NOT_LOGGED_IN")
}

// isCursorClientVersionRejected reports whether a permission_denied verdict is
// about the advertised client version rather than the credential. Cursor
// enforces a minimum client version on the agent endpoints and rejects older
// ones with the same Connect code it uses for a dead token.
func isCursorClientVersionRejected(agentErr *cursorpkg.AgentError) bool {
	if agentErr == nil {
		return false
	}
	haystack := strings.ToUpper(agentErr.Code + " " + agentErr.Message + " " + agentErr.Raw)
	for _, marker := range []string{
		"UPDATE REQUIRED",
		"UPDATE_REQUIRED",
		"UPDATE YOUR",
		"PLEASE UPDATE",
		"OUTDATED",
		"OUT OF DATE",
		"UNSUPPORTED_CLIENT",
		"UNSUPPORTED CLIENT",
		"CLIENT_VERSION",
		"CLIENT VERSION",
		"CLIENT-VERSION",
		"MINIMUM VERSION",
		"VERSION TOO OLD",
		"TOO OLD",
		"ERROR_CLIENT_TOO_OLD",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

// cursorClientVersionFailure reports a client-version rejection as an operator
// configuration fault: the account is left healthy, the failover loop stops,
// and the message names the knob to change. Rotating accounts cannot help — the
// gateway advertises the same version on every one of them.
func (s *OpenAIGatewayService) cursorClientVersionFailure(c *gin.Context, account *Account, agentErr *cursorpkg.AgentError) error {
	logger.L().Warn("cursor agent turn failed: client version rejected",
		zap.Int64("account_id", cursorAccountID(account)),
		zap.String("connect_code", agentErr.Code),
		zap.String("upstream_message", agentErr.Message),
	)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform:           PlatformCursor,
		AccountID:          cursorAccountID(account),
		AccountName:        cursorAccountName(account),
		UpstreamStatusCode: http.StatusForbidden,
		Stage:              string(GatewayFailureStageAccountAuth),
		Scope:              string(GatewayFailureScopeProvider),
		Reason:             string(CursorCredentialReasonClientVersion),
		Kind:               "config_error",
		Message:            agentErr.Error(),
	})
	return &UpstreamFailoverError{
		Stage:             GatewayFailureStageAccountAuth,
		Scope:             GatewayFailureScopeProvider,
		Reason:            CursorCredentialReasonClientVersion,
		NextAccountAction: NextAccountStop,
		StatusCode:        http.StatusForbidden,
		ResponseBody:      cursorUpstreamErrorBody(agentErr),
		ClientStatusCode:  http.StatusBadGateway,
		ClientMessage:     CursorClientVersionRejectedClientMessage,
	}
}

// cursorUpstreamErrorBodyLimit caps what an upstream verdict contributes to the
// failover body, which error-passthrough rules match on.
const cursorUpstreamErrorBodyLimit = 512

// cursorUpstreamErrorBody is the payload the failover contract carries: the
// upstream's own error JSON when there is one, so passthrough rules can match
// Cursor's shapes, otherwise the formatted verdict.
func cursorUpstreamErrorBody(agentErr *cursorpkg.AgentError) []byte {
	body := strings.TrimSpace(agentErr.Raw)
	if body == "" {
		body = agentErr.Error()
	}
	if len(body) > cursorUpstreamErrorBodyLimit {
		// Cutting at a byte offset can split a rune; drop the remnant rather
		// than emitting invalid UTF-8 into a body that may reach the client.
		body = strings.ToValidUTF8(body[:cursorUpstreamErrorBodyLimit], "")
	}
	return []byte(body)
}

func cursorAccountID(account *Account) int64 {
	if account == nil {
		return 0
	}
	return account.ID
}

func cursorAccountName(account *Account) string {
	if account == nil {
		return ""
	}
	return account.Name
}

// resolveCursorUsage prefers the upstream's own token accounting and falls back
// to a local estimate.
//
// The fallback is the common path, not an error case: a Run turn frequently
// ends by idle timeout (the server holds the stream open waiting for an exec
// result this gateway never sends) and no TurnEndedUpdate ever arrives, so
// billing cannot depend on the usage frame being there.
func resolveCursorUsage(input cursorInputEstimate, outcome cursorChatOutcome) OpenAIUsage {
	if u := outcome.usage; u != nil && (u.InputTokens > 0 || u.OutputTokens > 0) {
		usage := OpenAIUsage{
			InputTokens:              int(u.InputTokens),
			OutputTokens:             int(u.OutputTokens),
			CacheReadInputTokens:     int(u.CacheReadTokens),
			CacheCreationInputTokens: int(u.CacheWriteTokens),
		}
		if outcome.truncated {
			// Upstream counted everything it generated, including the tail this
			// gateway dropped at the client's ceiling. Bill what was delivered.
			usage.OutputTokens = estimateCursorOutputTokens(outcome)
		}
		return usage
	}
	return estimateCursorUsage(input, outcome)
}

// estimateCursorUsage computes local token estimates for billing.
func estimateCursorUsage(input cursorInputEstimate, outcome cursorChatOutcome) OpenAIUsage {
	return OpenAIUsage{
		InputTokens:  estimateTokensForText(input.text) + input.imageTokens,
		OutputTokens: estimateCursorOutputTokens(outcome),
	}
}

// estimateCursorOutputTokens estimates what the turn actually delivered: text,
// reasoning and the rendered tool calls.
func estimateCursorOutputTokens(outcome cursorChatOutcome) int {
	output := outcome.content + outcome.reasoning
	for _, tc := range outcome.toolCalls {
		output += tc.Function.Name + tc.Function.Arguments
	}
	return estimateTokensForText(output)
}
