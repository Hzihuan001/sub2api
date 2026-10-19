//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/stretchr/testify/require"
)

func TestKeyRouteFirstContentServerSearchExecutionPreventsReplay(t *testing.T) {
	t.Run("Kiro MCP tools call only at actual dispatch", func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			method   string
			guarded  bool
			cooldown bool
			want     bool
		}{
			{"sync and stream shared tool execution", "tools/call", true, false, true},
			{"tool-list prefetch", "tools/list", true, false, false},
			{"original unguarded path", "tools/call", false, false, false},
			{"local admission failure", "tools/call", true, true, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				guard := keyrouting.NewFirstContentGuard(0)
				defer guard.Finish()
				ctx := context.Background()
				if tc.guarded {
					ctx = keyrouting.WithFirstContentGuard(ctx, guard)
				}
				syntheticError := errors.New("synthetic local transport stop")
				upstream := &httpUpstreamRecorder{err: syntheticError}
				cooldowns := &stubKiroCooldownStore{}
				if tc.cooldown {
					cooldowns.checkErr = syntheticError
				}
				svc := &GatewayService{httpUpstream: upstream, kiroCooldownStore: cooldowns}
				account := &Account{ID: 91919, Platform: PlatformKiro, Type: AccountTypeOAuth}
				payload := []byte(`{"jsonrpc":"2.0","id":"synthetic_guard","method":"` + tc.method + `","params":{"name":"web_search","arguments":{"query":"synthetic"}}}`)
				resp, token, err := svc.doKiroMCPJSONRequest(ctx, account, "https://synthetic.invalid/mcp", payload, "synthetic-token")
				require.Nil(t, resp)
				require.Equal(t, "synthetic-token", token)
				require.ErrorIs(t, err, syntheticError)
				require.Equal(t, tc.want, guard.ReplayBlocked())
				if tc.cooldown {
					require.Empty(t, upstream.requests)
				} else {
					require.Len(t, upstream.requests, 1)
				}
				_, observed := guard.FirstContentElapsed()
				require.False(t, observed)
			})
		}
	})
	t.Run("search emulator actual provider call", func(t *testing.T) {
		original := getWebSearchManager()
		SetWebSearchManager(websearch.NewManager(nil, nil))
		t.Cleanup(func() { SetWebSearchManager(original) })
		guard := keyrouting.NewFirstContentGuard(0)
		defer guard.Finish()
		// No providers means this is a deterministic in-process error without
		// network calls, but it still crosses the real server-execution seam.
		ctx := keyrouting.WithFirstContentGuard(context.Background(), guard)
		_, _, err := doWebSearch(ctx, &Account{}, "synthetic local search")
		require.ErrorContains(t, err, "no available provider")
		require.True(t, guard.ReplayBlocked())
		_, _, ordinaryErr := doWebSearch(context.Background(), &Account{}, "synthetic local search")
		require.EqualError(t, ordinaryErr, err.Error(), "the unguarded native outcome must not change")
	})
}
