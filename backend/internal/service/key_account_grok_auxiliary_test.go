//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const keyAccountGrokComposerModel = "grok-composer-2.5-fast"
const keyAccountGrokCaptionResponse = `{"id":"vision","object":"response","model":"grok-build-0.1","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Diagram ABC."}]}],"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}`
const keyAccountGrokMainResponse = `{"id":"chatcmpl_composer","object":"chat.completion","model":"grok-composer-2.5-fast","choices":[{"index":0,"message":{"role":"assistant","content":"It shows ABC."},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`

func keyAccountGrokImageBody(images int) []byte {
	parts := []string{`{"type":"text","text":"Describe"}`}
	for i := 0; i < images; i++ {
		parts = append(parts, `{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}}`)
	}
	return []byte(`{"model":"` + keyAccountGrokComposerModel + `","messages":[{"role":"user","content":[` + strings.Join(parts, ",") + `]}],"stream":false}`)
}

func keyAccountGrokAuxiliaryFixture(t *testing.T, body []byte, options KeyAccountForwardOptions, do func(*http.Request, int) (*http.Response, error)) (*OpenAIGatewayService, context.Context, *KeyAccountForwardScope, *gin.Context, *Account, *keyAccountOpenAIHTTP) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	options.Metadata = keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 5501, GroupID: 33, AccountID: 55, Model: keyAccountGrokComposerModel, Endpoint: "/v1/chat/completions", PolicyRevision: 3}
	ctx, scope := WithKeyAccountForwardScope(context.Background(), options)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("api_key", &APIKey{ID: 5501})
	account := healthyGrokOAuthGatewayTestAccount(55, "test-token")
	repo := &grokQuotaAccountRepo{mockAccountRepoForPlatform: &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{55: account}}}
	upstream := &keyAccountOpenAIHTTP{do: do}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream, grokTokenProvider: NewGrokTokenProvider(repo, nil), accountRepo: repo}
	return svc, ctx, scope, c, account, upstream
}

func keyAccountGrokAuxiliaryResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestKeyAccountGrokAuxiliarySeparateAttemptsAndSingleNativeUsageMerge(t *testing.T) {
	var samples []keyrouting.AccountAttemptSample
	options := KeyAccountForwardOptions{
		FirstContentTimeout: 5 * time.Millisecond,
		FirstContentTimeoutForDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) (time.Duration, error) {
			t.Error("auxiliary or main media chain must not install a replay timer")
			return 5 * time.Millisecond, nil
		},
		ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
			samples = append(samples, sample)
			return nil
		},
	}
	body := keyAccountGrokImageBody(1)
	svc, ctx, scope, c, account, upstream := keyAccountGrokAuxiliaryFixture(t, body, options, func(req *http.Request, call int) (*http.Response, error) {
		if call == 1 {
			require.Equal(t, "/v1/responses", req.URL.Path)
			return keyAccountGrokAuxiliaryResponse(200, keyAccountGrokCaptionResponse), nil
		}
		require.Equal(t, "/v1/chat/completions", req.URL.Path)
		time.Sleep(20 * time.Millisecond) // Main must remain alive past the configured text TTFT.
		require.NoError(t, req.Context().Err())
		return keyAccountGrokAuxiliaryResponse(200, keyAccountGrokMainResponse), nil
	})
	result, err := svc.forwardAsRawChatCompletions(ctx, c, account, body, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 14, result.Usage.InputTokens)
	require.Equal(t, 12, result.Usage.OutputTokens)
	require.Equal(t, 2, upstream.calls)
	require.Len(t, samples, 1, "caption owns a separate completed scope")
	require.Equal(t, grokComposerImageBridgeVisionModel, samples[0].Metadata.UpstreamModel)
	require.Equal(t, "/v1/responses", samples[0].Metadata.UpstreamEndpoint)
	require.Equal(t, keyAccountGrokComposerModel, samples[0].Metadata.Model)
	require.True(t, samples[0].Dispatched)
	require.True(t, samples[0].UsageKnown)
	require.True(t, scope.ReplayBlocked())
	_, err = scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true})
	require.NoError(t, err)
	require.Len(t, samples, 2)
	require.Equal(t, keyAccountGrokComposerModel, samples[1].Metadata.UpstreamModel)
	require.NotEqual(t, samples[0].Metadata.AttemptID, samples[1].Metadata.AttemptID)
	require.NotZero(t, samples[1].ReplayVeto&keyrouting.AttemptVetoNativeGuard)
	require.False(t, samples[1].FirstContentExpired)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGrokAuxiliaryRetainsRealUsageOnMainOrBridgeFailure(t *testing.T) {
	for _, mode := range []string{"main-http503", "main-transport", "main-admission", "second-caption-http503-usage", "second-caption-empty-with-usage", "second-caption-malformed", "main-parse"} {
		t.Run(mode, func(t *testing.T) {
			var samples []keyrouting.AccountAttemptSample
			options := KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
				samples = append(samples, sample)
				return nil
			}}
			if mode == "main-admission" {
				options.BeforeDispatch = func(_ context.Context, metadata keyrouting.AccountAttemptMetadata) error {
					if metadata.UpstreamModel != grokComposerImageBridgeVisionModel {
						return errors.New("local main admission denied")
					}
					return nil
				}
			}
			images := 1
			if strings.HasPrefix(mode, "second-caption") {
				images = 2
			}
			body := keyAccountGrokImageBody(images)
			svc, ctx, scope, c, account, upstream := keyAccountGrokAuxiliaryFixture(t, body, options, func(_ *http.Request, call int) (*http.Response, error) {
				if call == 1 {
					return keyAccountGrokAuxiliaryResponse(200, keyAccountGrokCaptionResponse), nil
				}
				switch mode {
				case "main-transport":
					return nil, io.ErrUnexpectedEOF
				case "second-caption-empty-with-usage":
					return keyAccountGrokAuxiliaryResponse(200, `{"output":[],"usage":{"input_tokens":2,"output_tokens":1}}`), nil
				case "second-caption-http503-usage":
					return keyAccountGrokAuxiliaryResponse(503, `{"error":{"message":"busy"},"usage":{"input_tokens":2,"output_tokens":1}}`), nil
				case "second-caption-malformed", "main-parse":
					return keyAccountGrokAuxiliaryResponse(200, `{broken`), nil
				default:
					return keyAccountGrokAuxiliaryResponse(503, `{"error":{"message":"busy"}}`), nil
				}
			})
			result, err := svc.forwardAsRawChatCompletions(ctx, c, account, body, "")
			require.Error(t, err)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover), "media partial usage cannot retain a native retry classification")
			require.NotNil(t, result, "real caption usage must reach native partial-result settlement")
			wantInput, wantOutput := 11, 7
			if mode == "second-caption-empty-with-usage" || mode == "second-caption-http503-usage" {
				wantInput, wantOutput = 13, 8
			}
			require.Equal(t, wantInput, result.Usage.InputTokens)
			require.Equal(t, wantOutput, result.Usage.OutputTokens)
			require.Equal(t, keyAccountGrokComposerModel, result.BillingModel, "retain native same-account Composer pricing")
			require.True(t, scope.ReplayBlocked())
			_, finishErr := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, finishErr)
			require.Len(t, samples, upstream.calls, "each actual caption/main Do has one sample")
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
			if mode == "main-admission" {
				require.Equal(t, 1, upstream.calls)
			} else {
				require.Equal(t, 2, upstream.calls)
			}
		})
	}
}

func TestKeyAccountGrokAuxiliaryPreDispatchVetoDoesNotPoisonParent(t *testing.T) {
	body := keyAccountGrokImageBody(1)
	svc, ctx, scope, c, account, upstream := keyAccountGrokAuxiliaryFixture(t, body, KeyAccountForwardOptions{BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error { return errors.New("local veto") }}, func(*http.Request, int) (*http.Response, error) {
		t.Fatal("local failure must not send auxiliary HTTP")
		return nil, nil
	})
	_, _, err := svc.describeGrokComposerImage(ctx, c, account, "token", "data:image/png;base64,QUJD", 1)
	require.Error(t, err)
	require.Zero(t, upstream.calls)
	require.False(t, scope.hasAuxiliaryDispatch())
	require.False(t, scope.ReplayBlocked())
	require.Zero(t, scope.auxiliaryPending)
	require.Nil(t, scope.CurrentAttempt())
}

func TestKeyAccountGrokAuxiliaryUnknownDispatchBlocksReplayWithoutInventingUsage(t *testing.T) {
	for _, mode := range []string{"transport-error", "nil-response", "nil-body"} {
		t.Run(mode, func(t *testing.T) {
			body := keyAccountGrokImageBody(1)
			svc, ctx, scope, c, account, upstream := keyAccountGrokAuxiliaryFixture(t, body, KeyAccountForwardOptions{}, func(*http.Request, int) (*http.Response, error) {
				switch mode {
				case "nil-response":
					return nil, nil
				case "nil-body":
					return &http.Response{StatusCode: 200}, nil
				default:
					return nil, io.ErrUnexpectedEOF
				}
			})
			result, err := svc.forwardAsRawChatCompletions(ctx, c, account, body, "")
			require.Error(t, err)
			require.Nil(t, result)
			require.Equal(t, 1, upstream.calls)
			require.True(t, scope.ReplayBlocked())
			_, err = scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}

type keyAccountGrokCloseErrorBody struct{ io.Reader }

func (b keyAccountGrokCloseErrorBody) Close() error {
	return errors.New("native auxiliary close failed")
}

func TestKeyAccountGrokAuxiliaryCloseFailureKeepsCleanupBarrier(t *testing.T) {
	body := keyAccountGrokImageBody(1)
	svc, ctx, scope, c, account, upstream := keyAccountGrokAuxiliaryFixture(t, body, KeyAccountForwardOptions{}, func(*http.Request, int) (*http.Response, error) {
		response := keyAccountGrokAuxiliaryResponse(200, keyAccountGrokCaptionResponse)
		response.Body = keyAccountGrokCloseErrorBody{strings.NewReader(keyAccountGrokCaptionResponse)}
		return response, nil
	})
	result, err := svc.forwardAsRawChatCompletions(ctx, c, account, body, "")
	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 11, result.Usage.InputTokens)
	require.Equal(t, 1, upstream.calls, "a failed auxiliary cleanup must not start main")
	_, err = scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.False(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGrokAuxiliaryMainInternalRetryIsNotSecondSend(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{FirstContentTimeout: time.Millisecond})
	ack := scope.markAuxiliaryDispatch()
	ack(true)
	upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) {
		return keyAccountGrokAuxiliaryResponse(503, `{"error":"busy"}`), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	response, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "main-model")
	require.NoError(t, err, "first native main is allowed despite auxiliary completion")
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	_, err = svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "main-model")
	require.ErrorIs(t, err, ErrKeyAccountReplayBlocked)
	require.Equal(t, 1, upstream.calls)
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGrokAuxiliaryPendingPreventsFirstMainSend(t *testing.T) {
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{})
	ack := scope.markAuxiliaryDispatch()
	upstream := &keyAccountOpenAIHTTP{do: func(*http.Request, int) (*http.Response, error) {
		return keyAccountGrokAuxiliaryResponse(503, `{"error":"busy"}`), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	_, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "main-model")
	require.ErrorIs(t, err, ErrKeyAccountBodyPending)
	require.Zero(t, upstream.calls)
	ack(true)
	response, err := svc.doOpenAIUpstream(keyAccountTransportRequest(t, ctx), "", account, "main-model")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
	require.NoError(t, response.Body.Close())
	_, err = scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
	require.NoError(t, err)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())
}

func TestKeyAccountGrokAuxiliaryNativeBillingKeepsOneCombinedCommand(t *testing.T) {
	for _, mainSuccess := range []bool{false, true} {
		name := "main-failed"
		if mainSuccess {
			name = "main-succeeded"
		}
		t.Run(name, func(t *testing.T) {
			var samples []keyrouting.AccountAttemptSample
			body := keyAccountGrokImageBody(1)
			options := KeyAccountForwardOptions{ObserveAttempt: func(_ context.Context, sample keyrouting.AccountAttemptSample) error {
				samples = append(samples, sample)
				return nil
			}}
			svc, ctx, scope, c, account, upstream := keyAccountGrokAuxiliaryFixture(t, body, options, func(_ *http.Request, call int) (*http.Response, error) {
				if call == 1 {
					return keyAccountGrokAuxiliaryResponse(200, keyAccountGrokCaptionResponse), nil
				}
				if mainSuccess {
					return keyAccountGrokAuxiliaryResponse(200, keyAccountGrokMainResponse), nil
				}
				return keyAccountGrokAuxiliaryResponse(503, `{"error":{"message":"busy"}}`), nil
			})
			result, forwardErr := svc.forwardAsRawChatCompletions(ctx, c, account, body, "")
			if mainSuccess {
				require.NoError(t, forwardErr)
			} else {
				require.Error(t, forwardErr)
			}
			require.NotNil(t, result)
			require.Equal(t, 2, upstream.calls)
			logs := &openAIRecordUsageLogRepoStub{inserted: true}
			billing := &openAIRecordUsageBillingRepoStub{}
			users := &openAIRecordUsageUserRepoStub{}
			subs := &openAIRecordUsageSubRepoStub{}
			usageService := newOpenAIRecordUsageServiceWithBillingRepoForTest(logs, billing, users, subs, nil)
			groupID := int64(33)
			key := &APIKey{ID: 5501, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 1.2}}
			require.NoError(t, usageService.RecordUsage(ctx, &OpenAIRecordUsageInput{Result: result, APIKey: key, User: &User{ID: 77}, Account: account}))
			require.Equal(t, 1, logs.calls)
			require.Equal(t, 1, billing.calls, "reuse one native combined settlement, not an auxiliary billing override")
			require.Zero(t, users.deductCalls)
			require.Zero(t, subs.incrementCalls)
			require.Equal(t, result.Usage.InputTokens, billing.lastCmd.InputTokens)
			require.Equal(t, result.Usage.OutputTokens, billing.lastCmd.OutputTokens)
			require.Equal(t, account.ID, billing.lastCmd.AccountID)
			require.Equal(t, groupID, *logs.lastLog.GroupID)
			require.Equal(t, keyAccountGrokComposerModel, billing.lastCmd.Model)
			expected := expectedOpenAICost(t, usageService, keyAccountGrokComposerModel, result.Usage, 1.2)
			require.InDelta(t, expected.ActualCost, billing.lastCmd.BalanceCost, 1e-12)
			require.InDelta(t, expected.ActualCost, logs.lastLog.ActualCost, 1e-12)
			_, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure})
			require.NoError(t, err)
			require.Len(t, samples, 2)
			require.True(t, samples[0].UsageKnown)
			require.Equal(t, mainSuccess, samples[1].UsageKnown, "caption usage must not be attributed to the main HTTP attempt")
			require.True(t, scope.CleanupAck())
			require.False(t, scope.CanStartNext())
		})
	}
}
