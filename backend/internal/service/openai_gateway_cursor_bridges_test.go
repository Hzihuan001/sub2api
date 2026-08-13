//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func cursorFailureContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c
}

// A turn that died in transport never reached Cursor's application layer, so it
// says nothing about the account: it has to be retried in place. Without this a
// single flaky TLS handshake burns the account for the whole request, and a
// one-account pool answers 502 straight away.
// Cursor model ids must reach the agent protocol untouched. Running them through
// the Codex table rewrote gpt-5 to gpt-5.4 (a model no Cursor account can
// address) and stripped the "-max" suffix that selects max mode.
func TestResolveCursorChatMetaDoesNotApplyCodexModelRules(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 1, Platform: PlatformCursor, Type: AccountTypeOAuth}

	for _, model := range []string{
		"gpt-5",
		"gpt-5-codex",
		"claude-4.5-sonnet-max",
		"composer-2.5-fast",
		"auto",
	} {
		meta := svc.resolveCursorChatMeta(account, model, "", false)
		require.Equal(t, model, meta.upstreamModel, "cursor must forward %q verbatim", model)
		require.Equal(t, model, meta.billingModel)

		// Cross-check against the Codex normalizer to keep this test honest: it
		// only proves something while the two actually disagree.
		if codex := normalizeOpenAIModelForUpstream(account, model); codex != model {
			require.NotEqual(t, codex, meta.upstreamModel,
				"the codex table rewrites %q to %q; cursor must not follow it", model, codex)
		}
	}
}

// The account's own model_mapping still applies: it is how an operator points a
// client-facing name at a Cursor model id.
func TestResolveCursorChatMetaAppliesAccountModelMapping(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{
		ID:       1,
		Platform: PlatformCursor,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-4.5-sonnet-max"},
		},
	}

	meta := svc.resolveCursorChatMeta(account, "claude-sonnet-4-5", "", false)
	require.Equal(t, "claude-4.5-sonnet-max", meta.upstreamModel)
	require.Equal(t, "claude-sonnet-4-5", meta.originalModel)

	// An unmapped model falls back to the dispatch default, then to itself.
	require.Equal(t, "gpt-5", svc.resolveCursorChatMeta(account, "gpt-5", "", false).upstreamModel)
}

func TestCursorAgentFailureTransportRetriesSameAccount(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7, Platform: PlatformCursor, Type: AccountTypeOAuth}

	for _, transportErr := range []error{
		errors.New(`cursor: agent request failed: Post "https://agentn.global.api5.cursor.sh/agent.v1.AgentService/Run": net/http: TLS handshake timeout`),
		errors.New("cursor: no response headers within 1m0s"),
		errors.New("cursor: upstream sent no output within 1m0s"),
	} {
		err := svc.cursorAgentFailure(cursorFailureContext(), account, transportErr)

		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr, transportErr.Error())
		require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
		require.True(t, failoverErr.RetryableOnSameAccount, transportErr.Error())
		// Request-scoped: the account must not be quarantined for a fault that
		// would hit every other account in the pool identically.
		require.True(t, failoverErr.RequestScopedTransient, transportErr.Error())
	}
}

// A cancelled request is the client giving up, not a flaky upstream; retrying it
// only burns the account's retry budget on a turn nobody is waiting for.
func TestCursorAgentFailureClientCancellationIsNotRetried(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7, Platform: PlatformCursor, Type: AccountTypeOAuth}

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := svc.cursorAgentFailure(cursorFailureContext(), account,
			fmt.Errorf("cursor: agent request cancelled: %w", cause))

		var failoverErr *UpstreamFailoverError
		require.ErrorAs(t, err, &failoverErr)
		require.False(t, failoverErr.RetryableOnSameAccount, cause.Error())
		require.False(t, failoverErr.RequestScopedTransient, cause.Error())
	}
}

// An unmapped Connect code is a verdict from Cursor itself, so it keeps the
// pre-existing "switch accounts" contract rather than being retried in place.
func TestCursorAgentFailureConnectVerdictKeepsFailoverContract(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 7, Platform: PlatformCursor, Type: AccountTypeOAuth}

	err := svc.cursorAgentFailure(cursorFailureContext(), account, &cursorpkg.AgentError{
		Code:       "internal",
		Message:    "No exec result",
		Raw:        `{"code":"internal","message":"No exec result"}`,
		HTTPStatus: http.StatusBadGateway,
	})

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, failoverErr.RetryableOnSameAccount)
	require.Contains(t, string(failoverErr.ResponseBody), "No exec result")
}

// The three inbound protocols reach Cursor through the same agent turn, so an
// equivalent request must produce an identical AgentRunParams whichever door it
// came in by. This is what makes /v1/messages and /v1/responses behave like the
// already-verified /v1/chat/completions path instead of quietly dropping the
// user's text or naming a model_id the upstream does not serve.
func TestCursorAgentRunParamsIdenticalAcrossInboundProtocols(t *testing.T) {
	const userText = "confirm you are online"

	build := func(t *testing.T, req *apicompat.ChatCompletionsRequest) (cursorpkg.AgentRunParams, string) {
		t.Helper()
		params, inputText, err := buildCursorAgentRunParams("auto", req, cursorTranslateOptions{
			nativeTools: true, nativeImages: true, cwd: cursorpkg.AgentDefaultCwd,
		})
		require.NoError(t, err)
		return params, inputText
	}

	var chatReq apicompat.ChatCompletionsRequest
	require.NoError(t, json.Unmarshal([]byte(
		`{"model":"auto","messages":[{"role":"user","content":"`+userText+`"}]}`), &chatReq))
	wantParams, wantInput := build(t, &chatReq)

	// The reference turn is the one already verified end to end: a lone user
	// message goes out as a bare prompt against the default model.
	require.Equal(t, cursorpkg.AgentDefaultModel, wantParams.Model)
	require.False(t, wantParams.MaxMode)
	require.Equal(t, userText, wantParams.Prompt)

	t.Run("anthropic messages", func(t *testing.T) {
		var req apicompat.AnthropicRequest
		require.NoError(t, json.Unmarshal([]byte(
			`{"model":"auto","max_tokens":256,"messages":[{"role":"user","content":"`+userText+`"}]}`), &req))

		converted, err := apicompat.AnthropicToChatCompletionsRequest(&req)
		require.NoError(t, err)
		// The Anthropic bridge always names an effort, which is what selects a
		// "-thinking" model_id further down; on an unmapped model that must not
		// change the wire model.
		require.NotEmpty(t, converted.ReasoningEffort)

		params, inputText := build(t, converted)
		require.Equal(t, wantParams, params)
		require.Equal(t, wantInput, inputText)
	})

	// A bare-string input is the shorthand form of the Responses API; it has to
	// carry the user's text just like the structured form Codex actually sends.
	for name, body := range map[string]string{
		"responses string input": `{"model":"auto","input":"` + userText + `"}`,
		"responses typed input": `{"model":"auto","input":[{"type":"message","role":"user",` +
			`"content":[{"type":"input_text","text":"` + userText + `"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var req apicompat.ResponsesRequest
			require.NoError(t, json.Unmarshal([]byte(body), &req))

			converted, err := apicompat.ResponsesToChatCompletionsRequest(&req)
			require.NoError(t, err)

			params, inputText := build(t, converted)
			require.Equal(t, wantParams, params)
			require.Equal(t, wantInput, inputText)
		})
	}
}
