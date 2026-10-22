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
	"github.com/stretchr/testify/require"
)

func keyAccountTransportFixture(t *testing.T, ctx context.Context, opts KeyAccountForwardOptions) (context.Context, *KeyAccountForwardScope, *Account) {
	t.Helper()
	if opts.Metadata.APIKeyID == 0 {
		opts.Metadata = keyrouting.AccountAttemptMetadata{Site: "test-site", APIKeyID: 1, GroupID: 2, AccountID: 999, Platform: "stale", Model: "public-model", Endpoint: "/v1/responses", PolicyRevision: 3}
	}
	ctx, scope := WithKeyAccountForwardScope(ctx, opts)
	return ctx, scope, &Account{ID: 7, Platform: PlatformOpenAI, Concurrency: 4}
}

func keyAccountTransportRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/v1/chat/completions", strings.NewReader(`{"model":"wire-model"}`))
	require.NoError(t, err)
	return req
}

type keyAccountTrackedTestBody struct {
	io.Reader
	closes atomic.Int32
}

func (b *keyAccountTrackedTestBody) Close() error { b.closes.Add(1); return nil }

func TestKeyAccountInferenceTransportPassThroughAndVariants(t *testing.T) {
	req := keyAccountTransportRequest(t, context.Background())
	response := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("native"))}
	got, err := KeyAccountInferenceDoFunc(req, nil, "", func(actual *http.Request) (*http.Response, error) {
		require.Same(t, req, actual)
		return response, nil
	})
	require.NoError(t, err)
	require.Same(t, response, got)
	upstream := &keyAccountTransportTestUpstream{response: response}
	account := &Account{ID: 7, Concurrency: 4}
	_, err = KeyAccountInferenceDo(upstream, req, "proxy", account, "m")
	require.NoError(t, err)
	profile := &tlsfingerprint.Profile{}
	_, err = KeyAccountInferenceDoWithTLS(upstream, req, "proxy", account, "m", profile)
	require.NoError(t, err)
	require.Equal(t, 1, upstream.plain)
	require.Equal(t, 1, upstream.tls)
	require.Same(t, profile, upstream.profile)
}

type keyAccountTransportTestUpstream struct {
	response   *http.Response
	plain, tls int
	profile    *tlsfingerprint.Profile
}

func (u *keyAccountTransportTestUpstream) Do(req *http.Request, proxy string, accountID int64, concurrency int) (*http.Response, error) {
	u.plain++
	return u.response, nil
}

func (u *keyAccountTransportTestUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.tls++
	u.profile = profile
	return u.response, nil
}

func TestKeyAccountInferenceTransportRealIdentityAndUsageWithoutTTFT(t *testing.T) {
	var events []keyrouting.AccountAttemptSample
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
		events = append(events, sample)
		return nil
	}})
	require.Nil(t, keyrouting.GetFirstContentGuard(ctx))
	preparedAt := time.Now()
	var captured context.Context
	const payload = `{"usage":{"input_tokens":3},"choices":[]}`
	resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(req *http.Request) (*http.Response, error) {
		captured = req.Context()
		require.NotNil(t, keyrouting.GetFirstContentGuard(captured))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}, nil
	})
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, payload, string(data))
	require.NoError(t, resp.Body.Close())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.Dispatched)
	require.True(t, sample.UsageKnown)
	require.True(t, sample.StartedAt.Compare(preparedAt) >= 0)
	require.Equal(t, int64(7), sample.Metadata.AccountID)
	require.Equal(t, PlatformOpenAI, sample.Metadata.Platform)
	require.Equal(t, "wire-model", sample.Metadata.UpstreamModel)
	require.Equal(t, "/v1/responses", sample.Metadata.Endpoint)
	require.Equal(t, "/v1/chat/completions", sample.Metadata.UpstreamEndpoint)
	require.Equal(t, "public-model", sample.Metadata.Model)
	require.Equal(t, 200, sample.StatusCode)
	require.Len(t, events, 1)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
	require.True(t, scope.ReplayBlocked())
	require.Same(t, scope.CurrentAttempt(), keyrouting.GetAccountAttempt(captured))
}

func TestKeyAccountInferenceTransportNativeRetriesHaveIndependentSamples(t *testing.T) {
	var events []keyrouting.AccountAttemptSample
	var admissions []keyrouting.AccountAttemptMetadata
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		BeforeDispatch: func(_ context.Context, metadata keyrouting.AccountAttemptMetadata) error {
			admissions = append(admissions, metadata)
			return nil
		},
		ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
			events = append(events, sample)
			return nil
		},
	})
	firstBody := &keyAccountTrackedTestBody{Reader: strings.NewReader(`{"error":"overloaded"}`)}
	var firstCtx context.Context
	first, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model", func(req *http.Request) (*http.Response, error) {
		firstCtx = req.Context()
		return &http.Response{StatusCode: 503, Body: firstBody}, nil
	})
	require.NoError(t, err)
	_, err = io.ReadAll(first.Body) // Native code may defer Close until Forward exits.
	require.NoError(t, err)
	firstAttempt := keyrouting.GetAccountAttempt(firstCtx)
	require.Nil(t, keyrouting.GetFirstContentGuard(ctx), "retry preparation must not retain the first timer")
	second, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "wire-model-2", func(req *http.Request) (*http.Response, error) {
		require.NotSame(t, firstAttempt.Guard(), keyrouting.GetFirstContentGuard(req.Context()))
		require.Same(t, firstAttempt.Guard(), keyrouting.GetFirstContentGuard(firstCtx))
		return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader(`{"error":"bad gateway"}`))}, nil
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, firstBody.closes.Load())
	require.Len(t, events, 1)
	require.Equal(t, 503, events[0].StatusCode)
	require.NoError(t, first.Body.Close())
	require.EqualValues(t, 1, firstBody.closes.Load())
	require.NoError(t, second.Body.Close())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, ClientStatusCode: 500})
	require.NoError(t, err)
	require.Equal(t, 502, sample.StatusCode)
	require.Len(t, events, 2)
	require.Len(t, admissions, 2)
	require.NotEqual(t, admissions[0].AttemptID, admissions[1].AttemptID)
	require.NotEqual(t, events[0].Metadata.AttemptID, events[1].Metadata.AttemptID)
	require.Equal(t, "wire-model-2", sample.Metadata.UpstreamModel)
	require.False(t, scope.CanStartNext())
	require.True(t, scope.CleanupAck())
	require.True(t, scope.CanStartNext())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess})
	require.NoError(t, err)
	require.Len(t, events, 2, "outer completion must not duplicate wire events")
}

func TestKeyAccountInferenceTransportBodyAndUnknownSuccessBarriers(t *testing.T) {
	for _, status := range []int{101, 200, 204, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
			calls := 0
			do := func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("body"))}, nil
			}
			resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", do)
			require.NoError(t, err)
			_, err = KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", do)
			require.ErrorIs(t, err, ErrKeyAccountBodyPending)
			require.Equal(t, 1, calls)
			require.NoError(t, resp.Body.Close())
			if status == 503 {
				_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
				require.NoError(t, err)
				require.True(t, scope.CleanupAck())
				require.True(t, scope.CanStartNext())
			} else {
				_, err = KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", do)
				require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
				require.Equal(t, 1, calls)
				require.True(t, scope.ReplayBlocked())
			}
		})
	}
}

func TestKeyAccountInferenceTransportErrorUsageVetoesInternalRetry(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"usage":{"output_tokens":1},"error":"late"}`))}, nil
	})
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, err = KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		t.Fatal("must not launch a second potentially billable inference")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.UsageKnown)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportAdmissionFailureIsNotDispatched(t *testing.T) {
	want := errors.New("budget or admission denied")
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { return want },
		ObserveAttempt: func(context.Context, keyrouting.AccountAttemptSample) error {
			t.Fatal("no inference was sent")
			return nil
		},
	})
	_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) { t.Fatal("admission failed"); return nil, nil })
	require.ErrorIs(t, err, want)
	require.Nil(t, scope.CurrentAttempt())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.False(t, sample.Dispatched)
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportConcurrentDispatchAndFinishBlocked(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
			close(entered)
			<-release
			return nil, errors.New("network failed")
		})
		done <- err
	}()
	<-entered
	_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) { t.Fatal("parallel send"); return nil, nil })
	require.ErrorIs(t, err, ErrKeyAccountDispatchBusy)
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.ErrorIs(t, err, ErrKeyAccountDispatchBusy)
	close(release)
	require.Error(t, <-done)
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, scope.CleanupAck())
	require.True(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportObservationFailureDoesNotAbortRetryOrFinish(t *testing.T) {
	want := errors.New("soft health store unavailable")
	var observed atomic.Int32
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{ObserveAttempt: func(context.Context, keyrouting.AccountAttemptSample) error { observed.Add(1); return want }})
	for i := 0; i < 2; i++ {
		response, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("overloaded"))}, nil
		})
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.ErrorIs(t, err, want)
			require.True(t, sample.Dispatched)
		}()
	}
	wg.Wait()
	require.EqualValues(t, 2, observed.Load())
}

func TestKeyAccountInferenceTransportDeadlineAndClientCancellation(t *testing.T) {
	t.Run("local TTFT", func(t *testing.T) {
		ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 5 * time.Millisecond})
		_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})
		require.ErrorIs(t, err, context.Canceled)
		sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, ClientStatusCode: 504})
		require.NoError(t, err)
		require.True(t, sample.FirstContentExpired)
		require.Equal(t, 0, sample.StatusCode)
		require.Equal(t, keyrouting.AttemptFirstContentExpiry, sample.CancellationCause)
		require.True(t, scope.CleanupAck())
		require.True(t, scope.CanStartNext())
	})
	t.Run("client cancel is not a bad account", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		ctx, scope, account := keyAccountTransportFixture(t, parent, KeyAccountForwardOptions{})
		_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(req *http.Request) (*http.Response, error) {
			cancel()
			return nil, errors.New("native drain completed")
		})
		require.Error(t, err)
		sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UpstreamStatusCode: 503})
		require.NoError(t, err)
		require.Equal(t, keyrouting.OutcomeIgnored, sample.Outcome)
		require.Equal(t, 0, sample.StatusCode)
		require.True(t, scope.CleanupAck())
		require.False(t, scope.CanStartNext())
	})
}

func TestKeyAccountInferenceTransportEOFDoesNotAcknowledgeNativeCleanup(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("error"))}, nil
	})
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.False(t, scope.CleanupAck(), "EOF is not a native body Close/slot acknowledgement")
	require.False(t, scope.CanStartNext())
	require.NoError(t, resp.Body.Close())
	require.True(t, scope.CleanupAck())
	require.True(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportPreservesProviderCancellation(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	nativeCtx, nativeCancel := context.WithCancel(ctx)
	defer nativeCancel()
	_, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, nativeCtx), account, "m", func(req *http.Request) (*http.Response, error) {
		nativeCancel()
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(time.Second):
			t.Fatal("provider-owned cancellation was detached")
			return nil, nil
		}
	})
	require.ErrorIs(t, err, context.Canceled)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeTimeout})
	require.NoError(t, err)
	require.Equal(t, keyrouting.OutcomeTimeout, sample.Outcome)
	require.NotEqual(t, keyrouting.AttemptClientCancelled, sample.CancellationCause)
	require.False(t, sample.FirstContentExpired)
	require.NoError(t, ctx.Err())
}

type keyAccountLateReadTestBody struct {
	entered chan struct{}
	release chan struct{}
}

func (b *keyAccountLateReadTestBody) Read(p []byte) (int, error) {
	close(b.entered)
	<-b.release
	return copy(p, `{"usage":{"input_tokens":1}}`), io.EOF
}

func (b *keyAccountLateReadTestBody) Close() error { return nil }

func TestKeyAccountInferenceTransportCloseDoesNotSkipInFlightReader(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	body := &keyAccountLateReadTestBody{entered: make(chan struct{}), release: make(chan struct{})}
	resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: body}, nil
	})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() { defer close(done); _, _ = resp.Body.Read(make([]byte, 256)) }()
	<-body.entered
	require.NoError(t, resp.Body.Close())
	_, err = KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		t.Fatal("old Read is still running")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrKeyAccountBodyPending)
	close(body.release)
	<-done
	_, err = KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		t.Fatal("late usage must veto replay")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.UsageKnown)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportLateUsageWinsExpiredTimer(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: 5 * time.Millisecond})
	resp, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":"1"}}`))}, nil
	})
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, sample.FirstContentExpired)
	require.True(t, sample.UsageKnown)
	require.Equal(t, 503, sample.StatusCode, "known real response status remains distinct from a synthetic local timeout")
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountInferenceTransportResolvesLastCandidateForEveryDispatch(t *testing.T) {
	checks := 0
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		FirstContentTimeout: time.Millisecond,
		FirstContentTimeoutForDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) (time.Duration, error) {
			checks++
			if checks == 1 {
				return time.Second, nil
			}
			return 0, nil // Other candidates became unavailable before retry.
		},
	})
	first, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("error"))}, nil
	})
	require.NoError(t, err)
	require.NoError(t, first.Body.Close())
	second, err := KeyAccountInferenceDoFunc(keyAccountTransportRequest(t, ctx), account, "m", func(req *http.Request) (*http.Response, error) {
		select {
		case <-req.Context().Done():
			t.Fatal("last candidate was proactively cancelled")
		case <-time.After(10 * time.Millisecond):
		}
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("error"))}, nil
	})
	require.NoError(t, err)
	require.NoError(t, second.Body.Close())
	sample, err := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.Equal(t, 2, checks)
	require.False(t, sample.FirstContentExpired)
	require.True(t, scope.CleanupAck())
}
