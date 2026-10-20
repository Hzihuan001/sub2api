package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type keyAccountProviderTestUpstream struct{ calls int }

type keyAccountProviderCooldownStore struct{ KiroCooldownStore }

func (*keyAccountProviderCooldownStore) CheckCooldown(context.Context, string) error { return nil }

func (u *keyAccountProviderTestUpstream) Do(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.calls++
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"totalTokens":3}`))}, nil
}

func (u *keyAccountProviderTestUpstream) DoWithTLS(req *http.Request, proxy string, accountID int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, accountID, concurrency)
}

func TestKeyAccountProviderInferenceUsesNativeResolvedIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const publicModel = "public-alias"
	const anthropicModel = "claude-sonnet-4-20250514"
	messageBody := []byte(`{"model":"public-alias","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`)
	cases := []struct {
		name, platform, model string
		call                  func(context.Context, *gin.Context, *Account, HTTPUpstream)
	}{
		{"anthropic", PlatformAnthropic, anthropicModel, func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: u, tlsFPProfileService: &TLSFingerprintProfileService{}}
			parsed, err := ParseGatewayRequest(NewRequestBodyRef(messageBody), PlatformAnthropic)
			require.NoError(t, err)
			_, _ = svc.Forward(ctx, c, a, parsed)
		}},
		{"anthropic-passthrough", PlatformAnthropic, anthropicModel, func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: u, tlsFPProfileService: &TLSFingerprintProfileService{}}
			_, _ = svc.forwardAnthropicAPIKeyPassthrough(ctx, c, a, []byte(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}]}`), anthropicModel, publicModel, false, time.Now())
		}},
		{"anthropic-chat-bridge", PlatformAnthropic, anthropicModel, func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: u, tlsFPProfileService: &TLSFingerprintProfileService{}}
			_, _ = svc.ForwardAsChatCompletions(ctx, c, a, messageBody, nil)
		}},
		{"anthropic-responses-bridge", PlatformAnthropic, anthropicModel, func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: u, tlsFPProfileService: &TLSFingerprintProfileService{}}
			_, _ = svc.ForwardAsResponses(ctx, c, a, []byte(`{"model":"public-alias","input":"hello"}`), nil)
		}},
		{"bedrock", PlatformAnthropic, "us.anthropic.claude-sonnet-4-20250514-v1:0", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			a.Type = AccountTypeBedrock
			a.Credentials["auth_mode"] = "apikey"
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: u}
			_, _ = svc.executeBedrockUpstream(ctx, c, a, []byte(`{"messages":[]}`), "us.anthropic.claude-sonnet-4-20250514-v1:0", "us-east-1", false, nil, "secret", "")
		}},
		{"gemini-messages", PlatformGemini, "gemini-2.5-flash", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			a.Credentials["model_mapping"] = map[string]any{publicModel: "gemini-2.5-flash"}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: u}
			_, _ = svc.Forward(ctx, c, a, messageBody)
		}},
		{"gemini-chat", PlatformGemini, "gemini-2.5-flash", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			a.Credentials["model_mapping"] = map[string]any{publicModel: "gemini-2.5-flash"}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: u}
			_, _ = svc.ForwardAsChatCompletions(ctx, c, a, messageBody)
		}},
		{"gemini-native-generation", PlatformGemini, "gemini-2.5-flash", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			a.Credentials["model_mapping"] = map[string]any{publicModel: "gemini-2.5-flash"}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: u}
			_, _ = svc.ForwardNative(ctx, c, a, publicModel, "generateContent", false, []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
		}},
		{"kiro", PlatformKiro, "claude-sonnet-4.6", func(ctx context.Context, _ *gin.Context, a *Account, u HTTPUpstream) {
			svc := &GatewayService{httpUpstream: u, tlsFPProfileService: &TLSFingerprintProfileService{}, kiroCooldownStore: &keyAccountProviderCooldownStore{}}
			_, _, _ = svc.executeKiroUpstream(ctx, a, messageBody, "claude-sonnet-4-6", publicModel, "token", make(http.Header))
		}},
		{"antigravity-upstream", PlatformAntigravity, publicModel, func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			a.Type = AccountTypeUpstream
			a.Credentials["base_url"] = "https://upstream.example"
			svc := &AntigravityGatewayService{httpUpstream: u}
			_, _ = svc.ForwardUpstream(ctx, c, a, messageBody)
		}},
		{"antigravity-main-retry", PlatformAntigravity, "gemini-2.5-flash", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			svc := &AntigravityGatewayService{httpUpstream: u}
			_, _ = svc.antigravityRetryLoop(antigravityRetryLoopParams{ctx: ctx, c: c, account: a, accessToken: "token", action: "streamGenerateContent", body: []byte(`{"model":"gemini-2.5-flash","request":{}}`), requestedModel: publicModel, httpUpstream: u})
		}},
		{"antigravity-credits-inference-retry", PlatformAntigravity, "gemini-2.5-flash", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			svc := &AntigravityGatewayService{httpUpstream: u}
			_ = svc.attemptCreditsOveragesRetry(antigravityRetryLoopParams{ctx: ctx, c: c, account: a, accessToken: "token", action: "streamGenerateContent", body: []byte(`{"model":"gemini-2.5-flash","request":{}}`), requestedModel: publicModel, httpUpstream: u}, "https://upstream.example", "misleading-error-model", 0, http.StatusTooManyRequests, nil)
		}},
		{"systemone", PlatformTypeSafe, "jev-latest", func(ctx context.Context, c *gin.Context, a *Account, u HTTPUpstream) {
			a.Credentials["base_url"] = "https://typesafe.example"
			svc := &GatewayService{cfg: &config.Config{}, httpUpstream: u}
			_, _ = svc.ForwardSystemOne(ctx, c, a, []byte(`{"model":"jev-latest","state":"hello","questions":{"q":{"type":"noul"}}}`))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			var dispatches []keyrouting.AccountAttemptMetadata
			ctx, scope := WithKeyAccountForwardScope(parent, KeyAccountForwardOptions{
				Metadata: keyrouting.AccountAttemptMetadata{Site: "test", APIKeyID: 1, GroupID: 2, Model: publicModel, Endpoint: "/v1/messages", PolicyRevision: 3},
				BeforeDispatch: func(_ context.Context, metadata keyrouting.AccountAttemptMetadata) error {
					dispatches = append(dispatches, metadata)
					cancel() // No external request, native retry loops also stop immediately.
					return context.Canceled
				},
			})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil).WithContext(ctx)
			account := &Account{ID: 7, Platform: tc.platform, Type: AccountTypeAPIKey, Concurrency: 4, Credentials: map[string]any{"api_key": "secret", "model_mapping": map[string]any{publicModel: anthropicModel}}}
			upstream := &keyAccountProviderTestUpstream{}
			tc.call(ctx, c, account, upstream)
			require.Len(t, dispatches, 1, "native inference must pass the opt-in admission seam")
			require.Equal(t, tc.model, dispatches[0].UpstreamModel)
			require.Equal(t, tc.platform, dispatches[0].Platform)
			require.Equal(t, account.ID, dispatches[0].AccountID)
			require.Equal(t, publicModel, dispatches[0].Model)
			require.NotEmpty(t, dispatches[0].UpstreamEndpoint)
			require.Zero(t, upstream.calls, "admission veto must prevent the native transport send")
			require.Nil(t, scope.CurrentAttempt(), "a rejected admission is not a dispatched sample")
		})
	}
}

func TestKeyAccountProviderGeminiCountTokensStaysNative(t *testing.T) {
	ctx, scope := WithKeyAccountForwardScope(context.Background(), KeyAccountForwardOptions{BeforeDispatch: func(context.Context, keyrouting.AccountAttemptMetadata) error {
		t.Fatal("countTokens must not enter the inference seam")
		return context.Canceled
	}})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-2.5-flash:countTokens", nil).WithContext(ctx)
	account := &Account{ID: 7, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "secret"}}
	upstream := &keyAccountProviderTestUpstream{}
	svc := &GeminiMessagesCompatService{cfg: &config.Config{}, httpUpstream: upstream}
	result, err := svc.ForwardNative(ctx, c, account, "gemini-2.5-flash", "countTokens", false, []byte(`{"contents":[{"parts":[{"text":"hello"}]}]}`))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, upstream.calls)
	require.Nil(t, scope.CurrentAttempt())
}

func TestKeyAccountProviderJSONModelUsesWireEnvelope(t *testing.T) {
	for _, tc := range []struct{ body, model string }{
		{`{"model":"mapped-upstream","request":{"model":"not-the-wire-model"}}`, "mapped-upstream"},
		{`{"model":"gemini-2.5-flash","request":{"enabledCreditTypes":["GOOGLE_ONE_AI"]}}`, "gemini-2.5-flash"},
		{`{"request":{"model":"nested-only"}}`, ""},
		{`{"model":123}`, ""},
		{`{"model":null}`, ""},
		{`{}`, ""},
	} {
		require.Equal(t, tc.model, keyAccountInferenceJSONModel([]byte(tc.body)))
	}
}
