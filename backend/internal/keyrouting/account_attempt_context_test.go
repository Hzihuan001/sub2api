package keyrouting

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type testAttemptGuardProvider struct{ guard *FirstContentGuard }

func (p *testAttemptGuardProvider) CurrentFirstContentGuard() *FirstContentGuard { return p.guard }

func TestAccountAttemptDynamicGuardCapture(t *testing.T) {
	legacy := NewFirstContentGuard(0)
	provider := &testAttemptGuardProvider{}
	ctx := WithFirstContentGuardProvider(WithFirstContentGuard(context.Background(), legacy), provider)
	require.Nil(t, GetFirstContentGuard(ctx), "an explicit empty scope must shadow an old group guard")
	first := NewAccountAttempt(AccountAttemptMetadata{AccountID: 1}, 0)
	provider.guard = first.Guard()
	captured := WithAccountAttempt(ctx, first)
	require.Same(t, first.Guard(), GetFirstContentGuard(ctx))
	second := NewAccountAttempt(AccountAttemptMetadata{AccountID: 2}, 0)
	provider.guard = second.Guard()
	require.Same(t, second.Guard(), GetFirstContentGuard(ctx))
	require.Same(t, first.Guard(), GetFirstContentGuard(captured), "old raw readers must never observe a new attempt")
	require.Same(t, first, GetAccountAttempt(captured))
	require.Same(t, legacy, GetFirstContentGuard(WithFirstContentGuard(ctx, legacy)))
}
