package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// Exercise the real manager/runtime framing path without starting a plugin
// process or contacting an external model. One Forward represents one native
// plugin dispatch; fallback Do is independently counted.
type keyAccountOpenAIPluginAPI struct {
	pluginv1.TransportPluginClient
	mu      sync.Mutex
	calls   int
	frames  func(int) []*pluginv1.ForwardResponse
	onStart func()
	streams []*keyAccountOpenAIPluginStream
}

func (p *keyAccountOpenAIPluginAPI) Forward(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[pluginv1.ForwardRequest, pluginv1.ForwardResponse], error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.onStart != nil {
		p.onStart()
	}
	stream := &keyAccountOpenAIPluginStream{ctx: ctx, frames: p.frames(p.calls), sent: make(chan struct{})}
	p.streams = append(p.streams, stream)
	return stream, nil
}

type keyAccountOpenAIPluginStream struct {
	grpc.ClientStream
	ctx    context.Context
	frames []*pluginv1.ForwardResponse
	next   int
	sent   chan struct{}
	once   sync.Once
}

func (s *keyAccountOpenAIPluginStream) Context() context.Context            { return s.ctx }
func (s *keyAccountOpenAIPluginStream) Send(*pluginv1.ForwardRequest) error { return nil }
func (s *keyAccountOpenAIPluginStream) CloseSend() error {
	s.once.Do(func() { close(s.sent) })
	return nil
}
func (s *keyAccountOpenAIPluginStream) Recv() (*pluginv1.ForwardResponse, error) {
	select {
	case <-s.sent:
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
	if s.next >= len(s.frames) {
		return nil, io.EOF
	}
	frame := s.frames[s.next]
	s.next++
	return frame, nil
}

func keyAccountOpenAIFrames(status int, body string) []*pluginv1.ForwardResponse {
	return []*pluginv1.ForwardResponse{
		{Frame: &pluginv1.ForwardResponse_Start{Start: &pluginv1.ForwardResponseStart{StatusCode: int32(status), Headers: map[string]*pluginv1.HeaderValues{"Content-Type": {Values: []string{"application/json"}}}}}},
		{Frame: &pluginv1.ForwardResponse_BodyChunk{BodyChunk: []byte(body)}},
		{Frame: &pluginv1.ForwardResponse_End{End: &pluginv1.ForwardResponseEnd{}}},
	}
}

type keyAccountOpenAIHTTP struct {
	calls int
	do    func(*http.Request, int) (*http.Response, error)
}

func (u *keyAccountOpenAIHTTP) Do(request *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return u.do(request, u.calls)
}
func (u *keyAccountOpenAIHTTP) DoWithTLS(request *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(request, proxy, accountID, concurrency)
}

func keyAccountOpenAIServiceFixture(pluginHit bool, frames func(int) []*pluginv1.ForwardResponse, upstream *keyAccountOpenAIHTTP) (*OpenAIGatewayService, *keyAccountOpenAIPluginAPI, *pluginRuntime) {
	api := &keyAccountOpenAIPluginAPI{frames: frames}
	runtime := &pluginRuntime{client: &hcplugin.Client{}, api: api, done: make(chan struct{})}
	manager := &PluginManager{}
	if pluginHit {
		manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, runtime: runtime})
	}
	return &OpenAIGatewayService{pluginManager: manager, httpUpstream: upstream}, api, runtime
}

func TestKeyAccountOpenAITransportOneDispatchOneSampleWithActualModel(t *testing.T) {
	for _, mode := range []string{"plugin-hit", "plugin-unbound", "api-key-bypasses-plugin"} {
		t.Run(mode, func(t *testing.T) {
			var samples []keyrouting.AccountAttemptSample
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
				samples = append(samples, sample)
				return nil
			}})
			account.Type = AccountTypeOAuth
			if mode == "api-key-bypasses-plugin" {
				account.Type = AccountTypeAPIKey
			}
			const body = `{"usage":{"input_tokens":3},"choices":[]}`
			upstream := &keyAccountOpenAIHTTP{do: func(request *http.Request, _ int) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			}}
			svc, api, runtime := keyAccountOpenAIServiceFixture(mode != "plugin-unbound", func(int) []*pluginv1.ForwardResponse { return keyAccountOpenAIFrames(200, body) }, upstream)
			response, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "native-mapped-model")
			require.NoError(t, err)
			require.NotNil(t, KeyAccountResponseContext(response), "the manager and usage wrapper must not hide the fixed wire context")
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
			require.NoError(t, err)
			require.Len(t, samples, 1)
			require.Equal(t, 1, api.calls+upstream.calls)
			require.Equal(t, map[bool]int{true: 1, false: 0}[mode == "plugin-hit"], api.calls)
			require.Equal(t, "native-mapped-model", samples[0].Metadata.UpstreamModel)
			require.Equal(t, "public-model", samples[0].Metadata.Model)
			require.Equal(t, "/v1/responses", samples[0].Metadata.Endpoint)
			require.Equal(t, "/v1/chat/completions", samples[0].Metadata.UpstreamEndpoint)
			require.Equal(t, account.ID, samples[0].Metadata.AccountID)
			require.True(t, samples[0].Dispatched)
			require.True(t, samples[0].UsageKnown)
			require.True(t, scope.CleanupAck())
			require.Zero(t, runtime.inFlight.Load())
		})
	}
}

func TestKeyAccountOpenAITransportVetoOrMissingModelNeverSends(t *testing.T) {
	for _, mode := range []string{"omitted", "empty", "whitespace", "admission-veto"} {
		t.Run(mode, func(t *testing.T) {
			var observations atomic.Int32
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
				BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { return errors.New("admission veto") },
				ObserveAttempt: func(context.Context, keyrouting.AccountAttemptSample) error { observations.Add(1); return nil },
			})
			account.Type = AccountTypeOAuth
			upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) {
				t.Fatal("must not send fallback HTTP")
				return nil, nil
			}}
			svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse { t.Fatal("must not send plugin RPC"); return nil }, upstream)
			var actualModel []string
			switch mode {
			case "empty":
				actualModel = []string{""}
			case "whitespace":
				actualModel = []string{" \t"}
			case "admission-veto":
				actualModel = []string{"mapped"}
			}
			_, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, actualModel...)
			require.Error(t, err)
			require.Zero(t, api.calls+upstream.calls)
			require.Zero(t, runtime.inFlight.Load(), "an unused runtime reservation must be released")
			require.Nil(t, scope.CurrentAttempt())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.False(t, sample.Dispatched)
			require.Zero(t, observations.Load())
		})
	}
}

func TestKeyAccountOpenAITransportNoScopePreservesNativeRequest(t *testing.T) {
	for _, pluginHit := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "plugin"}[pluginHit], func(t *testing.T) {
			request := keyAccountTransportRequest(t, context.Background())
			upstream := &keyAccountOpenAIHTTP{do: func(actual *http.Request, _ int) (*http.Response, error) {
				require.Same(t, request, actual)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Request: actual}, nil
			}}
			svc, api, _ := keyAccountOpenAIServiceFixture(pluginHit, func(int) []*pluginv1.ForwardResponse { return keyAccountOpenAIFrames(200, `{}`) }, upstream)
			response, err := svc.doOpenAIUpstream(request, "", &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
			require.NoError(t, err)
			require.Same(t, request, response.Request)
			require.Nil(t, KeyAccountResponseContext(response))
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, 1, api.calls+upstream.calls)
		})
	}
}

func TestKeyAccountOpenAITransportCountTokensDoesNotSample(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error {
		t.Fatal("count_tokens is not an inference")
		return nil
	}})
	account.Type = AccountTypeOAuth
	upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) {
		t.Fatal("plugin should handle count_tokens natively")
		return nil, nil
	}}
	svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse { return keyAccountOpenAIFrames(200, `{"input_tokens":3}`) }, upstream)
	request := keyAccountTransportRequest(t, ctx)
	request.URL.Path = "/v1/responses/input_tokens"
	response, err := svc.doOpenAINonInferenceUpstream(request, "", account)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, api.calls)
	require.Zero(t, runtime.inFlight.Load())
	require.Nil(t, scope.CurrentAttempt())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
	require.NoError(t, err)
	require.False(t, sample.Dispatched)
}

func TestKeyAccountOpenAITransportInternalRetryWaitsForBodyCleanup(t *testing.T) {
	for _, pluginHit := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "plugin"}[pluginHit], func(t *testing.T) {
			var samples []keyrouting.AccountAttemptSample
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
				samples = append(samples, sample)
				return nil
			}})
			account.Type = AccountTypeOAuth
			first := &keyAccountTrackedTestBody{Reader: strings.NewReader(`{"error":"overload"}`)}
			upstream := &keyAccountOpenAIHTTP{do: func(_ *http.Request, call int) (*http.Response, error) {
				if call == 1 {
					return &http.Response{StatusCode: 503, Body: first}, nil
				}
				require.EqualValues(t, 1, first.closes.Load(), "old body must close before another real Do")
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}}
			svc, api, runtime := keyAccountOpenAIServiceFixture(pluginHit, func(call int) []*pluginv1.ForwardResponse {
				if call == 1 {
					return keyAccountOpenAIFrames(503, `{"error":"overload"}`)
				}
				return keyAccountOpenAIFrames(200, `{}`)
			}, upstream)
			api.onStart = func() {
				require.EqualValues(t, 1, runtime.inFlight.Load(), "previous plugin response must release its native request before retry")
			}
			response, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped-first")
			require.NoError(t, err)
			_, err = svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped-second")
			require.ErrorIs(t, err, ErrKeyAccountBodyPending)
			require.Equal(t, 1, api.calls+upstream.calls)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			response, err = svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped-second")
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
			require.NoError(t, err)
			require.Len(t, samples, 2)
			require.Equal(t, 2, api.calls+upstream.calls)
			require.NotEqual(t, samples[0].Metadata.AttemptID, samples[1].Metadata.AttemptID)
			require.Equal(t, "mapped-first", samples[0].Metadata.UpstreamModel)
			require.Equal(t, "mapped-second", samples[1].Metadata.UpstreamModel)
			require.Equal(t, 503, samples[0].StatusCode)
			require.True(t, scope.CleanupAck())
			require.Zero(t, runtime.inFlight.Load())
		})
	}
}

func TestKeyAccountOpenAITransportPluginRequestSentVetoesReplay(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: time.Second})
	account.Type = AccountTypeOAuth
	upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) {
		t.Fatal("handled plugin failure must not fall back to HTTP")
		return nil, nil
	}}
	svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse {
		return []*pluginv1.ForwardResponse{{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "UPSTREAM_EOF", RequestSent: true}}}}
	}, upstream)
	_, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
	var pluginErr *PluginTransportError
	require.ErrorAs(t, err, &pluginErr)
	require.True(t, pluginErr.RequestSent)
	require.True(t, scope.ReplayBlocked())
	_, err = svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
	require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
	require.Equal(t, 1, api.calls)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoNativeGuard)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
	require.Zero(t, runtime.inFlight.Load())
}
