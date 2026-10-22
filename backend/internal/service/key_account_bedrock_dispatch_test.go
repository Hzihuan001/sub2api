package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountBedrockDispatchRecordsNativeTerminalUsageAndBlocksReplay(t *testing.T) {
	const publicModel = "claude-sonnet-4-20250514"
	const upstreamModel = "us.anthropic.claude-sonnet-4-20250514-v1:0"
	const responseBody = `{"id":"msg_bedrock","type":"message","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","amazon-bedrock-invocationMetrics":{"inputTokenCount":12,"outputTokenCount":7}}`
	body := &keyAccountTrackedTestBody{Reader: strings.NewReader(responseBody)}
	var sends, accepted int
	upstream := &systemOneHTTPUpstream{do: func(req *http.Request) (*http.Response, error) {
		sends++
		require.Equal(t, http.MethodPost, req.Method)
		require.Equal(t, "bedrock-runtime.us-east-1.amazonaws.com", req.URL.Host)
		require.Equal(t, "/model/"+upstreamModel+"/invoke", req.URL.Path)
		require.Equal(t, "Bearer local-test-key", req.Header.Get("Authorization"))
		wireBody, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Contains(t, string(wireBody), `"anthropic_version":"bedrock-2023-05-31"`)
		require.NotContains(t, string(wireBody), `"model"`)
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		header.Set("x-amzn-requestid", "bedrock-key-route")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: body, Request: req}, nil
	}}
	ctx, scope, account := keyAccountTransportFixture(t, context.Background(), KeyAccountForwardOptions{
		Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 11, GroupID: 21, Model: publicModel, Endpoint: "/v1/messages", PolicyRevision: 4},
	})
	account.Platform = PlatformAnthropic
	account.Type = AccountTypeBedrock
	account.Credentials = map[string]any{"auth_mode": "apikey", "api_key": "local-test-key", "aws_region": "us-east-1"}
	c, recorder := keyAccountProviderTestContext(ctx, "/v1/messages")
	parsed := &ParsedRequest{
		Body:               NewRequestBodyRef([]byte(`{"model":"claude-sonnet-4-20250514","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`)),
		Model:              publicModel,
		OnUpstreamAccepted: func() { accepted++ },
	}
	svc := &GatewayService{cfg: &config.Config{}, httpUpstream: upstream}

	result, err := svc.forwardBedrock(ctx, c, account, parsed, time.Now())
	require.NoError(t, err)
	require.Equal(t, 1, sends)
	require.Equal(t, 1, accepted)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, upstreamModel, result.UpstreamModel)
	require.Equal(t, "bedrock-key-route", result.RequestID)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"stop_reason":"end_turn"`)
	require.NotContains(t, recorder.Body.String(), "amazon-bedrock-invocationMetrics")
	require.EqualValues(t, 1, body.closes.Load())

	// Metrics-only 2xx responses have already been accepted and consumed.
	// Even before the caller reports its terminal result, they cannot be replayed.
	_, err = svc.forwardBedrock(ctx, c, account, parsed, time.Now())
	require.Error(t, err)
	require.Equal(t, 1, sends)
	require.Equal(t, 1, accepted)
	require.True(t, scope.ReplayBlocked())

	sample, err := scope.Finish(result, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeSuccess, CompleteResponse: true, ResponseCommitted: true})
	require.NoError(t, err)
	require.True(t, sample.Dispatched)
	require.Equal(t, keyrouting.OutcomeSuccess, sample.Outcome)
	require.Equal(t, http.StatusOK, sample.UpstreamStatusCode)
	require.True(t, sample.UsageKnown)
	require.True(t, sample.ResponseCommitted)
	require.NotZero(t, sample.ReplayVeto)
	require.Equal(t, account.ID, sample.Metadata.AccountID)
	require.Equal(t, PlatformAnthropic, sample.Metadata.Platform)
	require.Equal(t, publicModel, sample.Metadata.Model)
	require.Equal(t, upstreamModel, sample.Metadata.UpstreamModel)
	require.Equal(t, "/model/"+upstreamModel+"/invoke", sample.Metadata.UpstreamEndpoint)
	require.True(t, scope.CleanupAck())
	require.False(t, scope.CanStartNext())

	// Even an accidental native retry after a completed response must not send.
	_, err = svc.forwardBedrock(ctx, c, account, parsed, time.Now())
	// The native Bedrock layer maps guarded transport errors to failover errors.
	// The dispatch boundary must still reject the call before the transport runs.
	require.Error(t, err)
	require.Equal(t, 1, sends)
	require.Equal(t, 1, accepted)
	require.EqualValues(t, 1, body.closes.Load())
}
