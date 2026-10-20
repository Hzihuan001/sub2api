package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/stretchr/testify/require"
)

type keyAccountFixedContextBody struct {
	io.ReadCloser
	wireContext context.Context
}

func (b *keyAccountFixedContextBody) KeyAccountWireContext() context.Context { return b.wireContext }

func TestKeyAccountInferenceTransportB2NativeWrapperKeepsFixedWireContext(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "first-model", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"overload"}`))}, nil
	})
	require.NoError(t, err)
	first := KeyAccountResponseContext(response)
	require.NotNil(t, first)
	response.Body = &keyAccountFixedContextBody{ReadCloser: response.Body, wireContext: first}
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	second, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "second-model", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	require.NoError(t, err)
	require.Same(t, first, KeyAccountResponseContext(response), "an old transformed body must never look up the active attempt")
	require.NotSame(t, first, KeyAccountResponseContext(second))
	require.NotEqual(t, keyrouting.GetAccountAttempt(first).Metadata().AttemptID, scope.CurrentAttempt().Metadata().AttemptID)
	require.NoError(t, second.Body.Close())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
	require.NoError(t, err)
	require.True(t, scope.CleanupAck())
	require.Nil(t, KeyAccountResponseContext(&http.Response{Body: io.NopCloser(strings.NewReader("native"))}))
}

func TestKeyAccountInferenceTransportB2RequiresActualModelOnlyInsideScope(t *testing.T) {
	var permits, sends atomic.Int32
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { permits.Add(1); return nil },
	})
	for _, model := range []string{"", " \t\n"} {
		_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, model, func(*http.Request) (*http.Response, error) {
			sends.Add(1)
			return nil, nil
		})
		require.Error(t, err)
	}
	require.Zero(t, permits.Load())
	require.Zero(t, sends.Load())
	require.Nil(t, scope.CurrentAttempt())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.False(t, sample.Dispatched)
	request := keyAccountTransportRequest(t, context.Background())
	_, err = KeyAccountInferenceDoFunc(request, nil, "", func(actual *http.Request) (*http.Response, error) {
		require.Same(t, request, actual)
		sends.Add(1)
		return nil, nil
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, sends.Load(), "unscoped native behavior is unchanged")
}

func TestKeyAccountInferenceTransportB2PreflightCannotSpendDispatchPermit(t *testing.T) {
	preflightErr := errors.New("no authoritative alternative snapshot")
	for _, failure := range []string{"preflight", "cancel-after-permit"} {
		t.Run(failure, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var order []string
			permits, sends, observations := 0, 0, 0
			ctx, scope, account := keyAccountTransportFixture(t, parent, KeyAccountForwardOptions{
				FirstContentTimeoutForDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) (time.Duration, error) {
					order = append(order, "preflight")
					if failure == "preflight" {
						return 0, preflightErr
					}
					return time.Second, nil
				},
				BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error {
					order = append(order, "permit")
					permits++
					cancel()
					return nil
				},
				ObserveAttempt: func(context.Context, keyrouting.AccountAttemptSample) error { observations++; return nil },
			})
			_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(*http.Request) (*http.Response, error) { sends++; return nil, nil })
			if failure == "preflight" {
				require.ErrorIs(t, err, preflightErr)
				require.Equal(t, []string{"preflight"}, order)
				require.Zero(t, permits)
			} else {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, []string{"preflight", "permit"}, order)
				require.Equal(t, 1, permits, "a final permission is conservative; it is not an actual-call counter")
			}
			require.Zero(t, sends)
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.False(t, sample.Dispatched)
			require.Zero(t, observations, "unissued permit must not manufacture health/error/usage evidence")
		})
	}
}

func TestKeyAccountInferenceTransportB2HTTPPreludeProofAndUnknownTimerRace(t *testing.T) {
	for _, tc := range []struct {
		name, path, contentType, prefix string
		wantSafe                        bool
	}{
		{name: "headers with no body yet", path: "/v1/chat/completions", contentType: "text/event-stream", wantSafe: true},
		{name: "complete heartbeat", path: "/v1/chat/completions", contentType: "text/event-stream", prefix: ": ping\n\n", wantSafe: true},
		{name: "role only", path: "/v1/chat/completions", contentType: "text/event-stream", prefix: "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"},\"finish_reason\":null}]}\n\n", wantSafe: true},
		{name: "response created", path: "/v1/responses", contentType: "text/event-stream", prefix: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[]}}\n\n", wantSafe: true},
		{name: "Anthropic zero usage", path: "/v1/messages", contentType: "text/event-stream", prefix: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n", wantSafe: true},
		{name: "partial prelude", path: "/v1/chat/completions", contentType: "text/event-stream", prefix: "data: {\"choices\":"},
		{name: "malformed JSON", path: "/v1/chat/completions", contentType: "text/event-stream", prefix: "data: {bad}\n\n"},
		{name: "unknown event", path: "/v1/chat/completions", contentType: "text/event-stream", prefix: "event: future-output\ndata: {\"choices\":[]}\n\n"},
		{name: "unknown response body", path: "/v1/chat/completions", contentType: "application/json", prefix: "{\"unrecognized\":true}"},
		{name: "effective content", path: "/v1/chat/completions", contentType: "text/event-stream", prefix: "data: {\"choices\":[{\"delta\":{\"content\":\"paid output\"}}]}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stop := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(200)
				_, _ = io.WriteString(w, tc.prefix)
				w.(http.Flusher).Flush()
				select {
				case <-request.Context().Done():
				case <-stop:
				}
			}))
			defer func() { close(stop); server.Close() }()
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 40 * time.Millisecond})
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+tc.path, strings.NewReader(`{"model":"wire-model"}`))
			require.NoError(t, err)
			response, err := KeyAccountInferenceDoFunc(request, account, "wire-model", server.Client().Do)
			require.NoError(t, err)
			wireCtx := KeyAccountResponseContext(response)
			require.NotNil(t, wireCtx)
			_, err = io.Copy(io.Discard, response.Body)
			require.ErrorIs(t, err, keyrouting.ErrAccountAttemptFirstContent)
			require.ErrorIs(t, context.Cause(wireCtx), keyrouting.ErrAccountAttemptFirstContent)
			require.NoError(t, response.Body.Close())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, sample.FirstContentExpired)
			require.Equal(t, 200, sample.StatusCode)
			require.Equal(t, tc.wantSafe, sample.VerifiedFirstContentCancellation)
			require.True(t, scope.CleanupAck())
			require.Equal(t, tc.wantSafe, scope.CanStartNext(), "expiry cannot reinterpret unknown paid data as a safe prelude")
		})
	}
}

func TestKeyAccountInferenceTransportB2HTTPEmptySuccessEOFNeverBecomesTTFTProof(t *testing.T) {
	for _, payload := range []string{"", ": ping\n\n", "data: {bad}\n\n"} {
		t.Run(payload, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, payload)
			}))
			defer server.Close()
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 20 * time.Millisecond})
			response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "wire-model", server.Client().Do)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err) // The success ended naturally, before the timer.
			<-KeyAccountResponseContext(response).Done()
			require.NoError(t, response.Body.Close())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, sample.FirstContentExpired)
			require.False(t, sample.VerifiedFirstContentCancellation)
			require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoUnknownProtocol)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}

func TestKeyAccountInferenceTransportB2CloseWinningBeforeTimerCannotForgeProof(t *testing.T) {
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": ping\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-request.Context().Done():
		case <-stop:
		}
	}))
	defer func() { close(stop); server.Close() }()
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 40 * time.Millisecond})
	response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "wire-model", server.Client().Do)
	require.NoError(t, err)
	wireCtx := KeyAccountResponseContext(response)
	guardCtx := scope.CurrentAttempt().Dispatch(context.Background())
	require.NoError(t, response.Body.Close())
	<-guardCtx.Done() // Force expiry after native Close already won cancellation.
	require.ErrorIs(t, context.Cause(wireCtx), keyrouting.ErrAccountAttemptFinished)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.FirstContentExpired)
	require.False(t, sample.VerifiedFirstContentCancellation)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportB2VerifiedFailureOverridesHealthSuccessNotBilling(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	var originalRequest *http.Request
	response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(request *http.Request) (*http.Response, error) {
		originalRequest = request
		return &http.Response{StatusCode: 200, Request: request, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"overloaded_error"},"usage":{"input_tokens":3}}`))}, nil
	})
	require.NoError(t, err)
	require.Same(t, originalRequest, response.Request)
	proofCtx := KeyAccountResponseContext(response)
	require.NotNil(t, proofCtx)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.True(t, MarkKeyAccountVerifiedFailure(proofCtx))
	require.NoError(t, response.Body.Close())
	result := &OpenAIForwardResult{}
	result.Usage.InputTokens = 3
	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
	require.NoError(t, err)
	require.Equal(t, keyrouting.OutcomeFailure, sample.Outcome)
	require.Equal(t, 200, sample.StatusCode)
	require.True(t, sample.VerifiedFailure)
	require.True(t, sample.UsageKnown)
	require.Equal(t, 3, result.Usage.InputTokens, "native partial result and billing evidence are unchanged")
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoCompleteResponse)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
	require.Nil(t, KeyAccountResponseContext(&http.Response{Body: io.NopCloser(strings.NewReader("native"))}))
}

func TestKeyAccountInferenceTransportB2NativeCommittedOutputDisablesReplayNotCurrentForward(t *testing.T) {
	for _, initiallyCommitted := range []bool{true, false} {
		t.Run(map[bool]string{true: "already committed before forward", false: "committed before internal retry"}[initiallyCommitted], func(t *testing.T) {
			committed := initiallyCommitted
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
				FirstContentTimeout: 2 * time.Millisecond,
				ReplayUnsafe:        func() bool { return committed },
			})
			response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(request *http.Request) (*http.Response, error) {
				if initiallyCommitted {
					select {
					case <-request.Context().Done():
						t.Fatal("pre-existing native output must disable the proactive timer")
					case <-time.After(5 * time.Millisecond):
					}
				}
				return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"controlled"}`))}, nil
			})
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			committed = true
			_, err = KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(*http.Request) (*http.Response, error) {
				t.Fatal("native committed output veto must cover internal retries too")
				return nil, nil
			})
			require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, sample.ResponseCommitted)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}

func TestKeyAccountInferenceTransportB2HTTPVerifiedTTFTAllowsIndependentInternalDispatch(t *testing.T) {
	stop := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, ": ping\n\n")
			w.(http.Flusher).Flush()
			select {
			case <-request.Context().Done():
			case <-stop:
			}
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"second attempt\"}}]}\n\n")
	}))
	defer func() { close(stop); server.Close() }()
	var samples []keyrouting.AccountAttemptSample
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		FirstContentTimeout: 30 * time.Millisecond,
		ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
			samples = append(samples, sample)
			return nil
		},
	})
	nativeCtx, nativeCleanup := context.WithCancel(ctx)
	response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, nativeCtx, server.URL), account, "wire-model", server.Client().Do)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.ErrorIs(t, err, keyrouting.ErrAccountAttemptFirstContent)
	require.NoError(t, response.Body.Close())
	nativeCleanup() // Parser-owned cleanup after the read cannot erase captured proof.
	require.Nil(t, scope.CurrentFirstContentGuard(), "a completed safe error must not leak its cancelled guard into the next native builder")
	response, err = KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "wire-model", server.Client().Do)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
	require.NoError(t, err)
	require.EqualValues(t, 2, calls.Load())
	require.Len(t, samples, 2)
	require.NotEqual(t, samples[0].Metadata.AttemptID, samples[1].Metadata.AttemptID)
	require.True(t, samples[0].VerifiedFirstContentCancellation)
	require.Equal(t, keyrouting.OutcomeTimeout, samples[0].Outcome, "raw local TTFT fact is normalized separately by the health mapper, not presented as an upstream HTTP timeout")
	require.Equal(t, keyrouting.AttemptFirstContentExpiry, samples[0].CancellationCause)
	require.Equal(t, keyrouting.OutcomeSuccess, samples[1].Outcome)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext(), "completed success cannot be replayed")
}

func TestKeyAccountInferenceTransportB2HTTPLateUnknownOrUsageAlwaysVetoes(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		usage         bool
	}{
		{name: "unknown late envelope", payload: "data: {\"future_output\":true}\n\n"},
		{name: "late usage", payload: "data: {\"usage\":{\"input_tokens\":7}}\n\n", usage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				<-release
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer server.Close()
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 20 * time.Millisecond})
			response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "wire-model", func(request *http.Request) (*http.Response, error) {
				go func() { <-request.Context().Done(); close(release) }()
				// A native usage drain is deliberately detached from cancellation.
				return server.Client().Do(request.WithContext(context.WithoutCancel(request.Context())))
			})
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			_, err = KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "wire-model", func(*http.Request) (*http.Response, error) {
				t.Fatal("late unknown/output/usage cannot be reinterpreted as a safe TTFT prefix")
				return nil, nil
			})
			require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, sample.FirstContentExpired)
			require.Equal(t, tc.usage, sample.UsageKnown)
			require.False(t, sample.VerifiedFirstContentCancellation)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}
