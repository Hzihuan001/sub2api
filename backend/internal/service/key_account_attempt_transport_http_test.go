package service

import (
	"context"
	"errors"
	"fmt"
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

func keyAccountLocalHTTPRequest(t *testing.T, ctx context.Context, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/chat/completions", strings.NewReader(`{"model":"wire-model"}`))
	require.NoError(t, err)
	return req
}

func TestKeyAccountInferenceTransportHTTPRetriesFreezeRealTimeoutEvidence(t *testing.T) {
	statuses := []int{503, 522, 524, 200}
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := int(sends.Add(1)) - 1
		if index >= len(statuses) {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(statuses[index])
		_, _ = io.WriteString(w, `{"error":{"message":"controlled response"}}`)
	}))
	defer server.Close()
	var samples []keyrouting.AccountAttemptSample
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
		samples = append(samples, sample)
		return nil
	}})
	for range statuses {
		response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "wire-model", server.Client().Do)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	_, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true})
	require.NoError(t, err)
	require.Len(t, samples, 4)
	identities := make(map[string]bool)
	for i, sample := range samples {
		require.True(t, sample.Dispatched)
		require.Equal(t, statuses[i], sample.StatusCode)
		require.Equal(t, keyrouting.AttemptStatusUpstream, sample.StatusSource)
		require.False(t, identities[sample.Metadata.AttemptID])
		identities[sample.Metadata.AttemptID] = true
		require.Equal(t, account.ID, sample.Metadata.AccountID)
	}
	require.Equal(t, keyrouting.OutcomeFailure, samples[0].Outcome)
	require.Equal(t, keyrouting.OutcomeTimeout, samples[1].Outcome)
	require.Equal(t, keyrouting.OutcomeTimeout, samples[2].Outcome)
	require.Equal(t, keyrouting.OutcomeSuccess, samples[3].Outcome)
	require.EqualValues(t, 4, sends.Load())
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportHTTPUnknownSuccessCannotReplay(t *testing.T) {
	for _, status := range []int{200, 204, 302} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var sends atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				sends.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"unexpected":"not a verified provider error"}`)
			}))
			defer server.Close()
			client := server.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
			response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", client.Do)
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			// A native parser synthesizing 502 does not make HTTP 200 a verified
			// failure. This must block both an internal retry and outer failover.
			_, err = KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", client.Do)
			require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UpstreamStatusCode: 502, ClientStatusCode: 502, ClientStatusSynthetic: true})
			require.NoError(t, err)
			require.Equal(t, status, sample.UpstreamStatusCode)
			require.Equal(t, status, sample.StatusCode)
			require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoUnknownProtocol)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
			require.EqualValues(t, 1, sends.Load())
		})
	}
}

func TestKeyAccountInferenceTransportHTTPUnknownSuccessOuterFinishIsConservative(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"unexpected":true}`)
	}))
	defer server.Close()
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", server.Client().Do)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.NotZero(t, sample.ReplayVeto&keyrouting.AttemptVetoUnknownProtocol)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext(), "outer failover must not require a failed internal retry to install the veto")
}

func TestKeyAccountInferenceTransportHTTPVerifiedFailureIsAnExplicitNarrowProof(t *testing.T) {
	for _, internal := range []bool{false, true} {
		t.Run(fmt.Sprint("internal=", internal), func(t *testing.T) {
			var sends atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if sends.Add(1) > 1 {
					w.WriteHeader(503)
				}
				_, _ = io.WriteString(w, `{"error":{"type":"overloaded_error","message":"controlled"}}`)
			}))
			defer server.Close()
			var samples []keyrouting.AccountAttemptSample
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
				samples = append(samples, sample)
				return nil
			}})
			require.False(t, MarkKeyAccountVerifiedFailure(ctx), "no dispatch cannot provide parser proof")
			var oldWireContext context.Context
			response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", func(req *http.Request) (*http.Response, error) {
				oldWireContext = req.Context()
				return server.Client().Do(req)
			})
			require.NoError(t, err)
			_, err = io.Copy(io.Discard, response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			// This call represents a native parser's verified error-envelope
			// branch. Merely obtaining UpstreamFailoverError is not sufficient.
			require.True(t, MarkKeyAccountVerifiedFailure(oldWireContext))
			if internal {
				response, err = KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", server.Client().Do)
				require.NoError(t, err)
				require.False(t, MarkKeyAccountVerifiedFailure(oldWireContext), "old wire proof must never mark the new attempt")
				require.NoError(t, response.Body.Close())
			}
			_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, samples[0].VerifiedFailure)
			require.Equal(t, 200, samples[0].StatusCode, "parser proof never invents an upstream 503")
			require.True(t, scope.CleanupAck())
			require.True(t, scope.CanStartNext())
			require.False(t, MarkKeyAccountVerifiedFailure(ctx), "frozen scope cannot be reopened")
		})
	}
}

func TestKeyAccountInferenceTransportHTTPVerifiedFailureNeverClearsUsageVeto(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"usage":{"input_tokens":1},"error":{"type":"overloaded_error"}}`)
	}))
	defer server.Close()
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", server.Client().Do)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.True(t, MarkKeyAccountVerifiedFailure(ctx))
	_, err = KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", func(*http.Request) (*http.Response, error) {
		t.Fatal("usage takes precedence over a verified error envelope")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.VerifiedFailure)
	require.True(t, sample.UsageKnown)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportHTTPFirstContentCancelAndLateUsage(t *testing.T) {
	t.Run("real HTTP cancellation before headers", func(t *testing.T) {
		stop := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			// Consume the request so net/http can observe a peer disconnect;
			// waiting on Context alone with an unread POST body can deadlock
			// httptest teardown independently of the client-side cancellation.
			_, _ = io.Copy(io.Discard, r.Body)
			select {
			case <-r.Context().Done():
			case <-stop:
			}
		}))
		defer func() { close(stop); server.Close() }()
		ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 25 * time.Millisecond})
		_, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", server.Client().Do)
		require.ErrorIs(t, err, keyrouting.ErrAccountAttemptFirstContent)
		sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UpstreamStatusCode: 504, ClientStatusCode: 504, ClientStatusSynthetic: true})
		require.NoError(t, err)
		require.True(t, sample.FirstContentExpired)
		require.Zero(t, sample.StatusCode)
		require.Zero(t, sample.UpstreamStatusCode, "synthetic parser status is not HTTP evidence")
		require.True(t, scope.CleanupAck())
		require.True(t, scope.CanStartNext())
	})
	t.Run("native drain receives usage after local cancellation", func(t *testing.T) {
		release := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(503)
			w.(http.Flusher).Flush()
			<-release
			_, _ = io.WriteString(w, "data: {\"usage\":{\"input_tokens\":7}}\n\n")
		}))
		defer server.Close()
		ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 25 * time.Millisecond})
		response, err := KeyAccountInferenceDoFunc(keyAccountLocalHTTPRequest(t, ctx, server.URL), account, "m", func(req *http.Request) (*http.Response, error) {
			go func() { <-req.Context().Done(); close(release) }()
			// Model a native usage-drain transport that deliberately finishes
			// the already dispatched response after local TTFT cancellation.
			return server.Client().Do(req.WithContext(context.WithoutCancel(req.Context())))
		})
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
		require.NoError(t, err)
		require.True(t, sample.FirstContentExpired)
		require.True(t, sample.UsageKnown)
		require.Equal(t, 503, sample.StatusCode)
		require.True(t, scope.CleanupAck())
		require.False(t, scope.CanStartNext())
	})
}

func TestKeyAccountInferenceTransportTypedTimeoutNeverUsesErrorWording(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want keyrouting.RouteOutcomeKind
	}{
		{name: "wrapped real deadline", err: fmt.Errorf("native transport: %w", context.DeadlineExceeded), want: keyrouting.OutcomeTimeout},
		{name: "timeout wording is not evidence", err: errors.New("timeout overload 524"), want: keyrouting.OutcomeFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
			_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) { return nil, tc.err })
			require.ErrorIs(t, err, tc.err)
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UpstreamStatusCode: 524})
			require.NoError(t, err)
			require.Equal(t, tc.want, sample.Outcome)
			require.Zero(t, sample.UpstreamStatusCode)
			require.Zero(t, sample.StatusCode)
			require.False(t, sample.FirstContentExpired)
		})
	}
}

type keyAccountFailedCloseBody struct {
	reads int
}

func (b *keyAccountFailedCloseBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *keyAccountFailedCloseBody) Close() error             { return errors.New("controlled close failure") }

func TestKeyAccountInferenceTransportCloseFailureNeverAcknowledgesCleanup(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	body := &keyAccountFailedCloseBody{}
	response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: body}, nil
	})
	require.NoError(t, err)
	require.Error(t, response.Body.Close())
	_, err = response.Body.Read(make([]byte, 1))
	require.ErrorIs(t, err, http.ErrBodyReadAfterClose)
	require.Zero(t, body.reads, "a newly started read after Close must not touch a released native body")
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.False(t, scope.CleanupAck(), "failed close is not proof the transport was released")
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportRealHTTPStatusOverridesSyntheticTimeoutClass(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"error":"controlled"}`))}, nil
	})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeTimeout, UpstreamStatusCode: 504})
	require.NoError(t, err)
	require.Equal(t, 503, sample.UpstreamStatusCode)
	require.Equal(t, keyrouting.OutcomeFailure, sample.Outcome, "synthetic timeout must not override actual non-timeout HTTP failure")
}
