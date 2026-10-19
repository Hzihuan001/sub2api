package service

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/tidwall/gjson"
)

func preventKeyRouteHostedToolReplay(ctx context.Context) {
	keyrouting.GetFirstContentGuard(ctx).PreventReplay()
}

// Native compatibility transforms can add hosted tools after ingress routing
// checks (Codex image bridge, OAuth search history, and Grok cache routing).
// Inspect only the final protocol declarations, never prompts or tool schemas.
// Once provider-side execution is possible, a pre-content timeout is not proof
// that replay is safe. Keep the original native response and settlement path.
func protectKeyRouteHostedTools(ctx context.Context, body []byte) {
	guard := keyrouting.GetFirstContentGuard(ctx)
	if guard == nil {
		return
	}
	request := gjson.ParseBytes(body)
	// Some compatibility transforms declare cached-only tools with choice=none.
	// They cannot execute and must not disable ordinary text routing.
	if choice := request.Get("tool_choice"); choice.Type == gjson.String && strings.TrimSpace(choice.Str) == "none" {
		return
	}
	if keyRouteHasHostedTools(request.Get("tools")) {
		guard.PreventReplay()
		return
	}
	// Responses Lite carries hosted declarations in protocol additional_tools
	// items. Do not recursively scan arbitrary user messages or function inputs.
	request.Get("input").ForEach(func(_, item gjson.Result) bool {
		if item.Get("type").String() == "additional_tools" && keyRouteHasHostedTools(item.Get("tools")) {
			guard.PreventReplay()
			return false
		}
		return true
	})
}

func keyRouteHasHostedTools(tools gjson.Result) bool {
	if !tools.IsArray() {
		return false
	}
	hosted := false
	tools.ForEach(func(_, tool gjson.Result) bool {
		switch strings.TrimSpace(tool.Get("type").String()) {
		case "function", "custom":
			return true // Client-executed declarations are safe until tool output.
		default:
			hosted = true // Unknown future provider tool types fail closed.
			return false
		}
	})
	return hosted
}
