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
	"github.com/tidwall/gjson"
)

func TestKeyRouteFirstContentHostedToolDeclarations(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"plain text", `{"input":"image_generation web_search","model":"gpt-5.4"}`, false},
		{"function schema is not a hosted declaration", `{"tools":[{"type":"function","name":"web_search","parameters":{"type":"object","properties":{"type":{"const":"image_generation"}}}}]}`, false},
		{"custom client tool", `{"tools":[{"type":"custom","name":"image_generation"}]}`, false},
		{"image tool", `{"tools":[{"type":"image_generation"}]}`, true},
		{"hosted search", `{"tools":[{"type":"web_search"},{"type":"x_search"}]}`, true},
		{"future builtin", `{"tools":[{"type":"future_hosted_tool"}]}`, true},
		{"disabled cache declarations", `{"tools":[{"type":"web_search"}],"tool_choice":"none"}`, false},
		{"lite hosted tools", `{"input":[{"type":"additional_tools","tools":[{"type":"web_search"}]}]}`, true},
		{"lite client tools", `{"input":[{"type":"additional_tools","tools":[{"type":"function","name":"lookup"}]}]}`, false},
		{"tool text in a message", `{"input":[{"type":"message","content":[{"type":"input_text","text":"web_search"}],"tools":[{"type":"web_search"}]}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guard := keyrouting.NewFirstContentGuard(0)
			defer guard.Finish()
			body := []byte(tc.body)
			original := append([]byte(nil), body...)
			protectKeyRouteHostedTools(keyrouting.WithFirstContentGuard(context.Background(), guard), body)
			require.Equal(t, tc.want, guard.ReplayBlocked())
			require.Equal(t, original, body, "the guard must not change the upstream payload")
			_, observed := guard.FirstContentElapsed()
			require.False(t, observed, "a declaration is not first valid output")
			// No guard means no routing behavior or response/settlement mutation.
			protectKeyRouteHostedTools(context.Background(), body)
			require.Equal(t, original, body)
		})
	}
}

func TestKeyRouteFirstContentHostedToolsProtectedAtFinalNativeBuilders(t *testing.T) {
	for _, name := range []string{"responses", "passthrough", "grok"} {
		t.Run(name, func(t *testing.T) {
			for _, guarded := range []bool{false, true} {
				guard := keyrouting.NewFirstContentGuard(50 * time.Millisecond)
				defer guard.Finish()
				ctx := context.Background()
				if guarded {
					ctx = keyrouting.WithFirstContentGuard(ctx, guard)
				}
				child, release := detachUpstreamContext(ctx)
				release()
				c, _ := newOpenAIImageGenerationControlTestContext(true, "unit-test-agent/1.0")
				account := newOpenAIImageGenerationControlTestAccount()
				svc := newOpenAIImageGenerationControlTestService(&httpUpstreamRecorder{})
				body := []byte(`{"model":"gpt-5.4","input":"test","tools":[{"type":"image_generation"}]}`)
				var req *http.Request
				var err error
				switch name {
				case "responses":
					req, err = svc.buildUpstreamRequest(child, c, account, body, "synthetic", false, "", false)
				case "passthrough":
					req, err = svc.buildUpstreamRequestOpenAIPassthrough(child, c, account, body, "synthetic")
				case "grok":
					account.Platform = PlatformGrok
					req, err = buildGrokResponsesRequest(child, c, account, body, "synthetic", "", &config.Config{})
				}
				require.NoError(t, err)
				sentBody, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, body, sentBody)
				require.Equal(t, guarded, guard.ReplayBlocked())
				if guarded {
					time.Sleep(75 * time.Millisecond)
					require.NoError(t, child.Err(), "hosted execution must not be canceled for speculative replay")
					require.False(t, guard.ReplaySafe())
				}
			}
		})
	}
}

func TestKeyRouteFirstContentCodexImageInjectionPreventsReplay(t *testing.T) {
	for _, bridgeEnabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary text unchanged", true: "injected image blocks replay"}[bridgeEnabled], func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"id":"synthetic_no_usage","model":"gpt-5.4","output":[],"usage":{"input_tokens":0,"output_tokens":0}}`)),
			}}
			svc := newOpenAIImageGenerationControlTestService(upstream)
			svc.cfg.Gateway.CodexImageGenerationBridgeEnabled = bridgeEnabled
			c, _ := newOpenAIImageGenerationControlTestContext(true, "codex_cli_rs/0.98.0")
			guard := keyrouting.NewFirstContentGuard(0)
			defer guard.Finish()
			ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
			c.Request = c.Request.WithContext(ctx)
			body := []byte(`{"model":"gpt-5.4","input":"write code","stream":false}`)
			original := append([]byte(nil), body...)
			result, err := svc.Forward(ctx, c, newOpenAIImageGenerationControlTestAccount(), body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, bridgeEnabled, gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists())
			require.Equal(t, bridgeEnabled, guard.ReplayBlocked(), "there is no response usage or semantic observer in this fixture")
			require.Equal(t, original, body)
		})
	}
}

func TestKeyRouteFirstContentAutoSearchInjectionPreventsReplay(t *testing.T) {
	t.Run("OAuth search history plus client functions", func(t *testing.T) {
		body, changed, err := ensureOpenAIOAuthWebSearchToolForHistoryBody([]byte(`{"input":[{"type":"web_search_call","id":"synthetic_history"}],"tools":[{"type":"function","name":"lookup"}]}`), false)
		require.NoError(t, err)
		require.True(t, changed)
		guard := keyrouting.NewFirstContentGuard(0)
		defer guard.Finish()
		protectKeyRouteHostedTools(keyrouting.WithFirstContentGuard(context.Background(), guard), body)
		require.True(t, guard.ReplayBlocked())
	})
	t.Run("Grok native search from client functions", func(t *testing.T) {
		body, err := appendMissingGrokFreeCacheNativeTools([]byte(`{"tools":[{"type":"function","name":"lookup"},{"type":"function","name":"web_search"}]}`))
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(body, `tools.#(type=="x_search")`).Exists())
		guard := keyrouting.NewFirstContentGuard(0)
		defer guard.Finish()
		protectKeyRouteHostedTools(keyrouting.WithFirstContentGuard(context.Background(), guard), body)
		require.True(t, guard.ReplayBlocked())
	})
	t.Run("Grok plain text cached-only tools remain safe", func(t *testing.T) {
		body, err := applyGrokResponsesCacheIdentity([]byte(`{"input":"hello"}`), []byte(`{"input":"hello"}`), "synthetic_cache", true)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(body, `tools.#(type=="web_search")`).Exists())
		require.Equal(t, "none", gjson.GetBytes(body, "tool_choice").String())
		guard := keyrouting.NewFirstContentGuard(0)
		defer guard.Finish()
		protectKeyRouteHostedTools(keyrouting.WithFirstContentGuard(context.Background(), guard), body)
		require.False(t, guard.ReplayBlocked())
	})
}
