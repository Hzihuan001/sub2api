package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	kiropkg "github.com/Wei-Shaw/sub2api/internal/pkg/kiro"
)

// keyAccountKiroProtocol observes the original binary wire, not synthesized
// Anthropic frames. In particular estimated input/cache usage is never proof.
type keyAccountKiroProtocol struct {
	mu         sync.Mutex
	wire       context.Context
	usage      kiropkg.Usage
	content    bool
	sideEffect bool
	err        error
}

func observeKeyAccountKiroProtocol(resp *http.Response, request *kiropkg.KiroRequestContext) *keyAccountKiroProtocol {
	wire := KeyAccountResponseContext(resp)
	if wire == nil || request == nil {
		return nil
	}
	state := &keyAccountKiroProtocol{wire: wire}
	request.ObserveEvent = state.observe
	return state
}

func (s *keyAccountKiroProtocol) observe(event kiropkg.EventStreamObservation) {
	s.mu.Lock()
	s.usage = event.Usage
	s.content = s.content || event.HasContent
	s.sideEffect = s.sideEffect || event.HasSideEffect
	s.mu.Unlock()
	if attempt := keyrouting.GetAccountAttempt(s.wire); attempt != nil {
		if keyAccountKiroActualUsage(event.Usage) {
			attempt.ObserveUsage()
		}
		if event.HasContent {
			attempt.ObserveFirstContent()
		}
		if event.HasSideEffect {
			attempt.ObserveSideEffect()
		}
		if event.Complete {
			attempt.PreventReplay(keyrouting.AttemptVetoCompleteResponse)
		}
	}
	if event.Failure != nil {
		// Error envelopes may themselves carry usage/output/tool evidence.
		// Keep that safety evidence even when it is not a billable Kiro usage
		// shape understood by the native calculator.
		observeKeyAccountProtocolPayload(s.wire, event.Failure.Payload)
		MarkKeyAccountVerifiedFailure(s.wire)
	}
}

func (s *keyAccountKiroProtocol) finish(err error) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

func keyAccountKiroActualUsage(usage kiropkg.Usage) bool {
	return usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.TotalTokens > 0 ||
		usage.CacheReadInputTokens > 0 || usage.CacheCreationInputTokens > 0 ||
		usage.CacheCreation5mInputTokens > 0 || usage.CacheCreation1hInputTokens > 0 || usage.KiroCredits > 0
}

// This response is a local protocol conversion but retains the immutable raw
// wire context. Its errors must never be rewritten into synthetic error frames
// that would be mistaken for decoded provider proof.
type keyAccountKiroStreamBody struct {
	io.ReadCloser
	reader io.Reader
	state  *keyAccountKiroProtocol
	// proofContext is deliberately bound to no account attempt. The translated
	// Anthropic SSE is a client-facing representation and may contain estimated
	// cache/input usage; only the original Kiro binary observer may mark usage or
	// replay safety on the real wire attempt.
	proofContext  context.Context
	closeUpstream func() error
	done          <-chan struct{}
	closeOnce     sync.Once
}

func (b *keyAccountKiroStreamBody) KeyAccountWireContext() context.Context {
	if b == nil {
		return nil
	}
	return b.proofContext
}

func (b *keyAccountKiroStreamBody) Read(p []byte) (int, error) {
	if b.reader != nil {
		return b.reader.Read(p)
	}
	return b.ReadCloser.Read(p)
}

func (b *keyAccountKiroStreamBody) Close() error {
	var err error
	b.closeOnce.Do(func() {
		err = b.ReadCloser.Close()
		if b.closeUpstream != nil {
			err = errors.Join(err, b.closeUpstream())
		}
		if b.done != nil {
			<-b.done // The original wire and translator own cleanup, not the pipe.
		}
	})
	return err
}

// keyAccountKiroNativeResult is a thin final-result adapter for Messages and
// its Chat/Responses bridges. It leaves unscoped/native success untouched and
// preserves only reported partial usage on a failed scoped generation.
func keyAccountKiroNativeResult(resp *http.Response, result *ForwardResult, err error, model, upstreamModel string, stream bool, start time.Time) (*ForwardResult, error) {
	if resp == nil {
		return result, err
	}
	body, ok := resp.Body.(*keyAccountKiroStreamBody)
	if !ok {
		return result, err
	}
	_ = body.Close() // Freeze producer state only after the original wire exits.
	result, err = body.state.nativeResult(result, err, model, upstreamModel, stream, start)
	if result != nil && result.RequestID == "" {
		result.RequestID = buildKiroRequestID(resp)
	}
	return result, err
}

func hasKeyAccountKiroProtocol(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	_, ok := resp.Body.(*keyAccountKiroStreamBody)
	return ok
}

func (s *keyAccountKiroProtocol) nativeResult(result *ForwardResult, err error, model, upstreamModel string, stream bool, start time.Time) (*ForwardResult, error) {
	if s == nil {
		return result, err
	}
	s.mu.Lock()
	upstreamErr, usage, content, sideEffect := s.err, s.usage, s.content, s.sideEffect
	s.mu.Unlock()
	if upstreamErr == nil {
		return result, err
	}
	if keyAccountKiroActualUsage(usage) {
		if result == nil {
			result = &ForwardResult{Model: model, UpstreamModel: upstreamModel, Stream: stream, Duration: time.Since(start)}
		}
		result.Usage = kiroUsageToClaude(usage, 0)
		return result, upstreamErr // Ordinary error: native handler settles partial usage.
	}
	var failure *kiropkg.EventStreamFailure
	blocked := false
	if attempt := keyrouting.GetAccountAttempt(s.wire); attempt != nil {
		blocked = attempt.ReplayBlocked()
	}
	if !content && !sideEffect && !blocked && errors.As(upstreamErr, &failure) {
		code := failure.StatusCode()
		if code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500 {
			return nil, &UpstreamFailoverError{StatusCode: code, ResponseBody: append([]byte(nil), failure.Payload...)}
		}
	}
	// Discard locally estimated/cache-emulated usage on a failed request. A
	// content/tool/unknown-protocol failure is not safe to silently replay.
	return nil, upstreamErr
}
