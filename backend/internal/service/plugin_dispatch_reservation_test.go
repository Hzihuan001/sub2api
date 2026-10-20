package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	pluginv1 "github.com/Wei-Shaw/sub2api/pkg/pluginapi/v1"
	hclog "github.com/hashicorp/go-hclog"
	hcplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// This child only verifies go-plugin's real Exited flag; it is not a model
// plugin and never opens a listener, contacts an upstream or reads credentials.
func TestKeyAccountPluginExitHelperProcess(t *testing.T) {
	if os.Getenv("MOSHU_KEY_ACCOUNT_PLUGIN_EXIT_HELPER") == "1" {
		os.Exit(0)
	}
}

func keyAccountExitedPluginClient(t *testing.T) *hcplugin.Client {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestKeyAccountPluginExitHelperProcess$")
	command.Env = append(os.Environ(), "MOSHU_KEY_ACCOUNT_PLUGIN_EXIT_HELPER=1")
	client := hcplugin.NewClient(&hcplugin.ClientConfig{
		HandshakeConfig:  pluginv1.HandshakeConfig,
		Cmd:              command,
		AllowedProtocols: []hcplugin.Protocol{hcplugin.ProtocolGRPC},
		StartTimeout:     time.Second,
		Logger:           hclog.NewNullLogger(),
	})
	t.Cleanup(client.Kill)
	_, err := client.Start()
	require.Error(t, err)
	require.Eventually(t, client.Exited, time.Second, time.Millisecond)
	return client
}

func TestKeyAccountOpenAITransportLocalReadinessDoesNotDispatch(t *testing.T) {
	for _, mode := range []string{"missing-runtime", "missing-client", "missing-api", "draining", "exited"} {
		t.Run(mode, func(t *testing.T) {
			var permits, observations atomic.Int32
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
				BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { permits.Add(1); return nil },
				ObserveAttempt: func(context.Context, keyrouting.AccountAttemptSample) error { observations.Add(1); return nil },
			})
			account.Type = AccountTypeOAuth
			upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) { return nil, errors.New("unexpected fallback") }}
			svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse { return nil }, upstream)
			route := svc.pluginManager.route.Load()
			switch mode {
			case "missing-runtime":
				route.runtime = nil
			case "missing-client":
				runtime.client = nil
			case "missing-api":
				runtime.api = nil
			case "draining":
				runtime.draining.Store(true)
			case "exited":
				runtime.client = keyAccountExitedPluginClient(t)
			}
			_, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
			require.Error(t, err)
			require.Zero(t, permits.Load())
			require.Zero(t, api.calls+upstream.calls)
			require.Zero(t, runtime.inFlight.Load())
			require.Nil(t, scope.CurrentAttempt())
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.False(t, sample.Dispatched)
			require.Zero(t, observations.Load())
		})
	}
}

func TestKeyAccountOpenAITransportInvalidNativeInputDoesNotDispatch(t *testing.T) {
	for _, mode := range []string{"nil-request", "missing-url", "nil-account", "invalid-account"} {
		t.Run(mode, func(t *testing.T) {
			var permits, observations atomic.Int32
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
				BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { permits.Add(1); return nil },
				ObserveAttempt: func(context.Context, keyrouting.AccountAttemptSample) error { observations.Add(1); return nil },
			})
			account.Type = AccountTypeOAuth
			request := keyAccountTransportRequest(t, ctx)
			switch mode {
			case "nil-request":
				request = nil
			case "missing-url":
				request.URL = nil
			case "nil-account":
				account = nil
			case "invalid-account":
				account.ID = 0
			}
			upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) { return nil, errors.New("unexpected fallback") }}
			svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse { return nil }, upstream)
			_, err := svc.doOpenAIUpstream(request, "", account, "mapped")
			require.Error(t, err)
			require.Zero(t, permits.Load())
			require.Zero(t, observations.Load())
			require.Zero(t, api.calls+upstream.calls)
			require.Zero(t, runtime.inFlight.Load())
			require.Nil(t, scope.CurrentAttempt())
		})
	}
}

func TestKeyAccountPluginReservationOwnership(t *testing.T) {
	for _, mode := range []string{"cancel", "response", "error", "route-replaced-and-draining"} {
		t.Run(mode, func(t *testing.T) {
			svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse {
				if mode == "error" {
					return []*pluginv1.ForwardResponse{{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "NOT_SENT", RequestSent: false}}}}
				}
				return keyAccountOpenAIFrames(200, `{}`)
			}, nil)
			account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
			reservation, handled, err := svc.pluginManager.reserveOpenAIOAuth(account)
			require.NoError(t, err)
			require.True(t, handled)
			require.EqualValues(t, 1, runtime.inFlight.Load())
			if mode == "cancel" {
				require.True(t, reservation.cancelBeforeDispatch())
				require.False(t, reservation.cancelBeforeDispatch())
				_, err = reservation.roundTrip(context.Background(), keyAccountTransportRequest(t, context.Background()), "", account)
				require.ErrorIs(t, err, errOpenAIPluginReservationUsed)
				require.Zero(t, api.calls)
			} else {
				if mode == "route-replaced-and-draining" {
					runtime.draining.Store(true)
					svc.pluginManager.route.Store(&pluginRoute{pluginID: 2, rolloutPercent: 100, unavailable: "new route unavailable"})
				}
				response, err := reservation.roundTrip(context.Background(), keyAccountTransportRequest(t, context.Background()), "", account)
				if mode == "error" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
					require.EqualValues(t, 1, runtime.inFlight.Load(), "native body, not reservation cancel, owns response cleanup")
					require.False(t, reservation.cancelBeforeDispatch())
					_, err = io.Copy(io.Discard, response.Body)
					require.NoError(t, err)
					require.NoError(t, response.Body.Close())
					require.NoError(t, response.Body.Close())
				}
				require.False(t, reservation.cancelBeforeDispatch())
				_, err = reservation.roundTrip(context.Background(), keyAccountTransportRequest(t, context.Background()), "", account)
				require.ErrorIs(t, err, errOpenAIPluginReservationUsed)
				require.Equal(t, 1, api.calls)
			}
			require.Zero(t, runtime.inFlight.Load())
		})
	}
}

func TestKeyAccountPluginReservationCancelDispatchCompetition(t *testing.T) {
	for iteration := 0; iteration < 50; iteration++ {
		svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse { return keyAccountOpenAIFrames(200, `{}`) }, nil)
		account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		reservation, handled, err := svc.pluginManager.reserveOpenAIOAuth(account)
		require.NoError(t, err)
		require.True(t, handled)
		request := keyAccountTransportRequest(t, context.Background())
		start := make(chan struct{})
		cancelled := make(chan bool, 1)
		type result struct {
			response *http.Response
			err      error
		}
		dispatched := make(chan result, 1)
		go func() { <-start; cancelled <- reservation.cancelBeforeDispatch() }()
		go func() {
			<-start
			response, err := reservation.roundTrip(context.Background(), request, "", account)
			dispatched <- result{response, err}
		}()
		close(start)
		cancelWon, sent := <-cancelled, <-dispatched
		if cancelWon {
			require.ErrorIs(t, sent.err, errOpenAIPluginReservationUsed)
			require.Zero(t, api.calls)
		} else {
			require.NoError(t, sent.err)
			_, err = io.Copy(io.Discard, sent.response.Body)
			require.NoError(t, err)
			require.NoError(t, sent.response.Body.Close())
			require.Equal(t, 1, api.calls)
		}
		require.Zero(t, runtime.inFlight.Load())
	}
}

func TestKeyAccountOpenAITransportBodyFaultAfterForwardIsActualAmbiguousAttempt(t *testing.T) {
	var permits atomic.Int32
	var samples []keyrouting.AccountAttemptSample
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { permits.Add(1); return nil },
		ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
			samples = append(samples, sample)
			return nil
		},
	})
	account.Type = AccountTypeOAuth
	svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(int) []*pluginv1.ForwardResponse { return keyAccountOpenAIFrames(200, `{}`) }, nil)
	request := keyAccountTransportRequest(t, ctx)
	request.Body = pluginFailingRequestBody{}
	_, err := svc.doOpenAIUpstream(request, "", account, "mapped")
	var pluginErr *PluginTransportError
	require.ErrorAs(t, err, &pluginErr)
	require.True(t, pluginErr.RequestSent)
	require.EqualValues(t, 1, permits.Load())
	require.Equal(t, 1, api.calls)
	require.True(t, scope.ReplayBlocked())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.Len(t, samples, 1)
	require.True(t, samples[0].Dispatched)
	require.Zero(t, runtime.inFlight.Load())
}

func TestKeyAccountOpenAITransportExplicitPluginNotSentAllowsFreshAttempt(t *testing.T) {
	var samples []keyrouting.AccountAttemptSample
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
		samples = append(samples, sample)
		return nil
	}})
	account.Type = AccountTypeOAuth
	svc, api, runtime := keyAccountOpenAIServiceFixture(true, func(call int) []*pluginv1.ForwardResponse {
		if call == 1 {
			return []*pluginv1.ForwardResponse{{Frame: &pluginv1.ForwardResponse_Error{Error: &pluginv1.ForwardResponseError{Code: "REJECTED_BEFORE_UPSTREAM", RequestSent: false}}}}
		}
		return keyAccountOpenAIFrames(200, `{}`)
	}, nil)
	_, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
	var pluginErr *PluginTransportError
	require.ErrorAs(t, err, &pluginErr)
	require.False(t, pluginErr.RequestSent)
	require.False(t, scope.ReplayBlocked())
	response, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
	require.NoError(t, err)
	require.Len(t, samples, 2)
	require.NotEqual(t, samples[0].Metadata.AttemptID, samples[1].Metadata.AttemptID)
	require.Zero(t, samples[0].StatusCode, "a plugin error is not a fabricated HTTP status")
	require.Equal(t, 2, api.calls)
	require.Zero(t, runtime.inFlight.Load())
}

type keyAccountPluginCancellationAPI struct {
	pluginv1.TransportPluginClient
	calls atomic.Int32
}

func (p *keyAccountPluginCancellationAPI) Forward(ctx context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[pluginv1.ForwardRequest, pluginv1.ForwardResponse], error) {
	p.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestKeyAccountOpenAITransportUntypedCancellationNeverProvesPluginNotSent(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 10 * time.Millisecond})
	account.Type = AccountTypeOAuth
	api := &keyAccountPluginCancellationAPI{}
	runtime := &pluginRuntime{client: &hcplugin.Client{}, api: api, done: make(chan struct{})}
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, runtime: runtime})
	svc := &OpenAIGatewayService{pluginManager: manager}
	_, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, scope.ReplayBlocked(), "a lost RequestSent bit cannot authorize a duplicate plugin inference")
	_, err = svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "mapped")
	require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeTimeout})
	require.NoError(t, err)
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoNativeGuard)
	require.EqualValues(t, 1, api.calls.Load())
	require.Zero(t, runtime.inFlight.Load())
}
