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

	params, inputText, err := buildCursorAgentRun(account, meta.upstreamModel, &chatReq)
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
		return s.streamCursorChatCompletions(c, account, stream, meta, inputText, startTime)
	}
	return s.bufferCursorChatCompletions(c, account, stream, meta, inputText, startTime)
}

// resolveCursorChatMeta resolves the billing/upstream model identity reusing the
// account model_mapping, matching the Grok raw path.
func (s *OpenAIGatewayService) resolveCursorChatMeta(account *Account, requestedModel, defaultMappedModel string, stream bool) cursorChatMeta {
	billingModel := resolveOpenAIForwardModel(account, requestedModel, defaultMappedModel)
	upstreamModel := normalizeOpenAIModelForUpstream(account, billingModel)
	return cursorChatMeta{
		originalModel: requestedModel,
		billingModel:  billingModel,
		upstreamModel: upstreamModel,
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

	baseURL := cursorAgentBaseURL(account)
	if err := validateCursorAgentHost(s.cfg, baseURL); err != nil {
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
// It takes the channel rather than the stream so the mapping is testable
// without an upstream; callers still own closing the stream.
func consumeCursorAgentEvents(
	events <-chan cursorpkg.AgentEvent,
	startTime time.Time,
	onDelta func(cursorDelta) error,
) (cursorChatOutcome, error) {
	outcome := cursorChatOutcome{finishReason: "stop"}
	var contentBuilder, reasoningBuilder strings.Builder
	toolIndexByID := make(map[string]int)

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

	for event := range events {
		switch event.Type {
		case cursorpkg.AgentEventText:
			if event.Text == "" {
				continue
			}
			markFirstToken()
			contentBuilder.WriteString(event.Text)
			if err := emit(cursorDelta{kind: cursorDeltaText, text: event.Text}); err != nil {
				return finish(err)
			}

		case cursorpkg.AgentEventThinking:
			if event.Text == "" {
				continue
			}
			markFirstToken()
			reasoningBuilder.WriteString(event.Text)
			if err := emit(cursorDelta{kind: cursorDeltaReasoning, text: event.Text}); err != nil {
				return finish(err)
			}

		case cursorpkg.AgentEventToolCall:
			if event.ToolCall == nil {
				continue
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

// streamCursorChatCompletions relays the Cursor stream to the client as OpenAI
// Chat Completions SSE chunks. The first client byte is withheld until the
// first upstream delta so upstream errors can still map to a clean status.
func (s *OpenAIGatewayService) streamCursorChatCompletions(
	c *gin.Context,
	account *Account,
	stream *cursorpkg.AgentStream,
	meta cursorChatMeta,
	inputText string,
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

	outcome, err := consumeCursorAgentEvents(stream.Events(), startTime, func(delta cursorDelta) error {
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
	if err != nil {
		if !headersWritten {
			// No client bytes yet: surface as failover so another account can try.
			return nil, s.cursorAgentFailure(c, account, err)
		}
		logger.L().Warn("cursor agent stream: upstream error after first byte",
			zap.String("request_id", requestID),
			zap.Error(err),
		)
	}

	if !clientDisconnected {
		ensureHeaders()
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
		_, _ = c.Writer.WriteString("data: [DONE]\n\n")
		c.Writer.Flush()
	}

	usage := resolveCursorUsage(inputText, outcome)
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
	inputText string,
	startTime time.Time,
) (*OpenAIForwardResult, error) {
	requestID := cursorAgentRequestID(stream, "chatcmpl-")
	outcome, err := consumeCursorAgentEvents(stream.Events(), startTime, nil)
	if err != nil {
		return nil, s.cursorAgentFailure(c, account, err)
	}

	usage := resolveCursorUsage(inputText, outcome)
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
func resolveCursorUsage(inputText string, outcome cursorChatOutcome) OpenAIUsage {
	if u := outcome.usage; u != nil && (u.InputTokens > 0 || u.OutputTokens > 0) {
		return OpenAIUsage{
			InputTokens:              int(u.InputTokens),
			OutputTokens:             int(u.OutputTokens),
			CacheReadInputTokens:     int(u.CacheReadTokens),
			CacheCreationInputTokens: int(u.CacheWriteTokens),
		}
	}
	return estimateCursorUsage(inputText, outcome)
}

// estimateCursorUsage computes local token estimates for billing.
func estimateCursorUsage(inputText string, outcome cursorChatOutcome) OpenAIUsage {
	output := outcome.content + outcome.reasoning
	for _, tc := range outcome.toolCalls {
		output += tc.Function.Name + tc.Function.Arguments
	}
	return OpenAIUsage{
		InputTokens:  estimateTokensForText(inputText),
		OutputTokens: estimateTokensForText(output),
	}
}
