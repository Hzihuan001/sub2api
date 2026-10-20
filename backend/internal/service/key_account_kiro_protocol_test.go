//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func keyAccountKiroExceptionFrame(t *testing.T, payload string) []byte {
	t.Helper()
	var headers bytes.Buffer
	for _, pair := range [][2]string{{":message-type", "exception"}, {":exception-type", "ServiceUnavailableException"}} {
		_ = headers.WriteByte(byte(len(pair[0])))
		_, _ = headers.WriteString(pair[0])
		_ = headers.WriteByte(7)
		require.NoError(t, binary.Write(&headers, binary.BigEndian, uint16(len(pair[1]))))
		_, _ = headers.WriteString(pair[1])
	}
	var frame bytes.Buffer
	require.NoError(t, binary.Write(&frame, binary.BigEndian, uint32(16+headers.Len()+len(payload))))
	require.NoError(t, binary.Write(&frame, binary.BigEndian, uint32(headers.Len())))
	require.NoError(t, binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
	_, _ = frame.Write(headers.Bytes())
	_, _ = frame.WriteString(payload)
	require.NoError(t, binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
	return frame.Bytes()
}

func TestKeyAccountKiroNativeFailuresPreserveUsageAndNeverInventSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"messages", "chat/completions", "responses", "responses-compact-direct"} {
		for _, stream := range []bool{false, true} {
			for _, mode := range []string{"immediate", "partial", "tool", "completed", "opaque"} {
				t.Run(fmt.Sprintf("%s/stream=%v/%s", endpoint, stream, mode), func(t *testing.T) {
					wireEndpoint := endpoint
					if endpoint == "responses-compact-direct" {
						// Compaction is primary-only at ingress. Exercise direct
						// scoped service invocation only as a defensive guard.
						wireEndpoint = "responses"
					}
					var wire bytes.Buffer
					if mode == "partial" {
						_, _ = wire.Write(buildKiroEventStreamFrame(t, "assistantResponseEvent", map[string]any{"content": "partial output"}))
						_, _ = wire.Write(buildKiroEventStreamFrame(t, "messageMetadataEvent", map[string]any{"tokenUsage": map[string]any{"uncachedInputTokens": 7, "outputTokens": 3, "totalTokens": 10}}))
						_, _ = wire.Write(buildKiroEventStreamFrame(t, "meteringEvent", map[string]any{"usage": 0.04}))
					}
					if mode == "tool" {
						_, _ = wire.Write(buildKiroEventStreamFrame(t, "toolUseEvent", map[string]any{"toolUseId": "tool-1", "name": "read_file", "input": "{}", "stop": true}))
					}
					if mode == "completed" {
						_, _ = wire.Write(buildKiroEventStreamFrame(t, "messageStopEvent", map[string]any{"stopReason": "end_turn"}))
					}
					payload := `{"message":"busy"}`
					if mode == "opaque" {
						payload = `{"message":`
					}
					_, _ = wire.Write(keyAccountKiroExceptionFrame(t, payload))
					raw := &keyAccountTrackedTestBody{Reader: bytes.NewReader(wire.Bytes())}
					upstream := &queuedHTTPUpstream{responses: []*http.Response{{StatusCode: 200, Header: http.Header{"Content-Type": {"application/vnd.amazon.eventstream"}}, Body: raw}}}
					svc := &GatewayService{
						httpUpstream: upstream, kiroCooldownStore: &stubKiroCooldownStore{}, tlsFPProfileService: &TLSFingerprintProfileService{}, rateLimitService: &RateLimitService{},
						cfg: &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
					}
					account := &Account{ID: 421, Name: "scoped-kiro", Platform: PlatformKiro, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "local-test", "profile_arn": "arn:aws:codewhisperer:us-east-1:123456789012:profile/TEST"}}
					ctx, scope := WithKeyAccountForwardScope(context.Background(), KeyAccountForwardOptions{Metadata: keyrouting.AccountAttemptMetadata{Site: "local", APIKeyID: 1, GroupID: 2, AccountID: account.ID, Platform: PlatformKiro, Model: "claude-sonnet-4-6", Endpoint: "/v1/" + wireEndpoint, PolicyRevision: 3}})
					body := fmt.Sprintf(`{"model":"claude-sonnet-4-6","stream":%v,"messages":[{"role":"user","content":"hi"}]}`, stream)
					if endpoint == "responses" {
						body = fmt.Sprintf(`{"model":"claude-sonnet-4-6","stream":%v,"input":"hi"}`, stream)
					}
					if endpoint == "responses-compact-direct" {
						body = fmt.Sprintf(`{"model":"claude-sonnet-4-6","stream":%v,"input":[{"type":"message","role":"user","content":"hi"},{"type":"compaction_trigger"}]}`, stream)
					}
					parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(body)), wireEndpoint)
					require.NoError(t, err)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+wireEndpoint, strings.NewReader(body)).WithContext(ctx)
					var result *ForwardResult
					switch endpoint {
					case "messages":
						result, err = svc.Forward(ctx, c, account, parsed)
					case "chat/completions":
						result, err = svc.ForwardAsChatCompletions(ctx, c, account, []byte(body), parsed)
					case "responses", "responses-compact-direct":
						result, err = svc.ForwardAsResponses(ctx, c, account, []byte(body), parsed)
					}
					require.Error(t, err)
					var failover *UpstreamFailoverError
					require.Equal(t, mode == "immediate", errors.As(err, &failover))
					if mode == "partial" {
						require.NotNil(t, result)
						require.Equal(t, 7, result.Usage.InputTokens)
						require.Equal(t, 3, result.Usage.OutputTokens)
						require.InDelta(t, 0.04, result.Usage.KiroCredits, 0.000001)
						require.Zero(t, result.Usage.CacheReadInputTokens)
					} else {
						require.Nil(t, result, "do not bill locally estimated or cache-emulated usage after failure")
					}
					require.NotContains(t, recorder.Body.String(), `response.completed`)
					require.NotContains(t, recorder.Body.String(), `[DONE]`)
					require.NotContains(t, recorder.Body.String(), `message_stop`)
					if mode == "immediate" {
						require.Empty(t, recorder.Body.String(), "pure failure must be routed before synthetic client output")
					}
					sample, finishErr := scope.Finish(nil, keyrouting.AccountAttemptResult{Outcome: keyrouting.OutcomeFailure, UsageKnown: KeyAccountForwardResultHasUsage(result)})
					require.NoError(t, finishErr)
					require.Equal(t, mode != "opaque", sample.VerifiedFailure)
					require.Equal(t, 200, sample.UpstreamStatusCode)
					require.Equal(t, mode == "partial", sample.UsageKnown)
					require.True(t, scope.CleanupAck(), "producer and original wire must both be closed")
					require.Equal(t, mode == "immediate", scope.CanStartNext())
					require.EqualValues(t, 1, raw.closes.Load())
					require.Len(t, upstream.requests, 1)
				})
			}
		}
	}
}
