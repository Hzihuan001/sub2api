package service

// Grok Composer's image description is an existing media-assisted chain, not
// an automatically replayable text turn. Separate samples/guards keep the
// auxiliary response from completing the main scope. Its real dispatch then
// permanently forbids main retries; native usage remains one same-account bill.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

type keyAccountGrokAuxiliaryKey struct{}

type keyAccountGrokAuxiliary struct {
	parent        *KeyAccountForwardScope
	ack           func(bool)
	cleanupFailed bool
}

func (s *KeyAccountForwardScope) hasAuxiliaryDispatch() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auxiliaryDispatched
}

// Called only inside the auxiliary's real Do closure, never during credential,
// request building, final admission or budget preflight. The first main Do is
// still legal. All subsequent main sends and outer replays are not.
func (s *KeyAccountForwardScope) markAuxiliaryDispatch() func(bool) {
	s.mu.Lock()
	s.auxiliaryDispatched = true
	s.auxiliaryPending++
	s.mu.Unlock()
	var once sync.Once
	return func(clean bool) {
		if clean {
			once.Do(func() {
				s.mu.Lock()
				s.auxiliaryPending--
				s.mu.Unlock()
			})
		}
	}
}

func (s *OpenAIGatewayService) describeKeyAccountGrokComposerImage(ctx context.Context, c *gin.Context, account *Account, token, imageURL string, index int) (string, OpenAIUsage, error) {
	parent := KeyAccountForwardScopeFromContext(ctx)
	parent.mu.Lock()
	options := parent.opts
	parent.mu.Unlock()
	options.FirstContentTimeout = 0
	options.FirstContentTimeoutForDispatch = nil
	aux := &keyAccountGrokAuxiliary{parent: parent}
	ctx = context.WithValue(ctx, keyAccountGrokAuxiliaryKey{}, aux)
	ctx, scope := WithKeyAccountForwardScope(ctx, options)
	description, usage, err := s.describeGrokComposerImageNative(ctx, c, account, token, imageURL, index)
	terminal := keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure}
	if err == nil {
		terminal.Outcome, terminal.CompleteResponse = keyrouting.OutcomeSuccess, true
	}
	if description != "" && scope.CurrentAttempt() != nil {
		scope.CurrentAttempt().ObserveFirstContent()
	}
	_, observationErr := scope.Finish(&OpenAIForwardResult{Usage: usage}, terminal)
	if observationErr != nil {
		slog.WarnContext(ctx, "key_account_grok_auxiliary_observation_failed", "account_id", account.ID, "error", observationErr)
	}
	clean := scope.CleanupAck() && !aux.cleanupFailed
	if aux.ack != nil {
		aux.ack(clean)
	}
	if !clean && err == nil {
		err = errors.New("Grok image description cleanup was not acknowledged")
	}
	return description, usage, err
}

func (s *OpenAIGatewayService) doKeyAccountGrokAuxiliaryUpstream(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	aux, _ := req.Context().Value(keyAccountGrokAuxiliaryKey{}).(*keyAccountGrokAuxiliary)
	if aux == nil {
		if KeyAccountForwardScopeFromContext(req.Context()) != nil {
			return nil, fmt.Errorf("%w: missing Grok auxiliary scope", ErrKeyAccountRouteScope)
		}
		return s.doOpenAIUpstream(req, proxyURL, account, grokComposerImageBridgeVisionModel)
	}
	return KeyAccountInferenceDoFunc(req, account, grokComposerImageBridgeVisionModel, func(wire *http.Request) (*http.Response, error) {
		aux.ack = aux.parent.markAuxiliaryDispatch()
		return s.httpUpstream.Do(wire, proxyURL, account.ID, account.Concurrency)
	})
}

// Preserve a complete, independently parseable usage envelope even when a
// final transport read errors. Malformed/truncated JSON is never guessed into
// a bill. Unscoped callers keep the original response-read helper unchanged.
func readKeyAccountGrokAuxiliaryResponse(resp *http.Response, c *gin.Context, s *OpenAIGatewayService) ([]byte, error) {
	limit := resolveUpstreamResponseReadLimit(s.cfg)
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(body)) > limit {
		setOpsUpstreamError(c, http.StatusBadGateway, "upstream response too large", "")
		return nil, fmt.Errorf("%w: limit=%d", ErrUpstreamResponseBodyTooLarge, limit)
	}
	return body, err
}

func keyAccountGrokAuxiliaryUsage(ctx context.Context, body []byte) OpenAIUsage {
	if _, ok := ctx.Value(keyAccountGrokAuxiliaryKey{}).(*keyAccountGrokAuxiliary); !ok {
		return OpenAIUsage{}
	}
	var parsed apicompat.ResponsesResponse
	if keyAccountProtocolObject(body) == nil || decodeOpenAIJSONUseNumber(body, &parsed) != nil {
		return OpenAIUsage{}
	}
	return copyOpenAIUsageFromResponsesUsage(parsed.Usage)
}

func keyAccountGrokPartialResult(result *OpenAIForwardResult, usage OpenAIUsage, originalModel, billingModel, upstreamModel string, stream bool) *OpenAIForwardResult {
	if !KeyAccountForwardResultHasUsage(&OpenAIForwardResult{Usage: usage}) {
		return result
	}
	if result == nil {
		result = &OpenAIForwardResult{Model: originalModel, BillingModel: billingModel, UpstreamModel: upstreamModel, UpstreamEndpoint: openAIResponsesUpstreamEndpoint, Stream: stream}
	}
	addOpenAIUsage(&result.Usage, usage)
	return result
}
