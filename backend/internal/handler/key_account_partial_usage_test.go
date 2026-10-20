package handler

import (
	"context"
	"testing"
	"time"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestKeyAccountPartialUsageFreezesActualNativeBillingInputs(t *testing.T) {
	f := newAccountDispatchFixture(t)
	key, _ := servermiddleware.GetAPIKeyFromContext(f.c)
	account := &f.selector.accounts[1][0]
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	f.c.Request.Header.Set("User-Agent", "original-client")
	result := &service.ForwardResult{Model: "model", UpstreamModel: "actual-model", Usage: service.ClaudeUsage{InputTokens: 7, OutputTokens: 2}}
	input := keyAccountPartialForwardUsageInput(f.c, result, key, account, nil, at, []byte(`{"model":"model"}`), service.ChannelMappingResult{}, "model")
	require.NotNil(t, input)
	require.Equal(t, at, input.PricingAt)
	require.Same(t, result, input.Result)
	require.Same(t, account, input.Account)
	require.Equal(t, int64(1), *input.APIKey.GroupID)
	require.Equal(t, "original-client", input.UserAgent)
	*key.GroupID = 2
	f.c.Request.Header.Set("User-Agent", "later-client")
	f.c.Request = f.c.Request.WithContext(context.Background())
	require.Equal(t, int64(1), *input.APIKey.GroupID, "async billing must not follow mutable effective group IDs")
	require.Equal(t, "original-client", input.UserAgent)
	require.Equal(t, 7, input.Result.Usage.InputTokens)
	require.Equal(t, 2, input.Result.Usage.OutputTokens)
}

func TestKeyAccountPartialUsageDoesNotCreateChargesForUnsentOrLegacyResults(t *testing.T) {
	f := newAccountDispatchFixture(t)
	key, _ := servermiddleware.GetAPIKeyFromContext(f.c)
	account := &f.selector.accounts[1][0]
	for _, result := range []*service.ForwardResult{nil, {}, {Model: "model"}} {
		require.Nil(t, keyAccountPartialForwardUsageInput(f.c, result, key, account, nil, time.Now(), nil, service.ChannelMappingResult{}, "model"))
	}
	result := &service.ForwardResult{Usage: service.ClaudeUsage{InputTokens: 1}}
	f.c.Request = f.c.Request.WithContext(context.Background())
	require.Nil(t, keyAccountPartialForwardUsageInput(f.c, result, key, account, nil, time.Now(), nil, service.ChannelMappingResult{}, "model"), "legacy requests keep their original settlement behavior")
}
