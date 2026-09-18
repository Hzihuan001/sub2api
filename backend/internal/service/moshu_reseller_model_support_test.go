package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountIsModelSupported_ResellerSnapshotIsAuthoritativeAcrossProviders(t *testing.T) {
	providers := []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			account := &Account{
				Platform: provider,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"model_mapping": map[string]any{"claude-sonnet-4-6": "claude-sonnet-4-6"},
				},
				Extra: map[string]any{
					"moshu_reseller_managed":           true,
					MoshuResellerModelSnapshotExtraKey: []string{"provider-live-model"},
				},
			}

			require.True(t, account.IsModelSupported("provider-live-model"))
			require.False(t, account.IsModelSupported("claude-sonnet-4-6"),
				"stale built-in or local mappings must not authorize a reseller account")
		})
	}
}

func TestAccountIsModelSupported_ResellerEmptySnapshotIsDenyByDefault(t *testing.T) {
	account := &Account{
		Platform: PlatformDeepseek,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"moshu_reseller_managed":           true,
			MoshuResellerModelSnapshotExtraKey: []string{},
		},
	}

	supported, authoritative := account.IsMoshuResellerModelSupported("deepseek-v4.1-flash")
	require.False(t, supported)
	require.True(t, authoritative)
	require.False(t, account.IsModelSupported("deepseek-v4.1-flash"))
}

func TestAccountIsModelSupported_ResellerSnapshotNormalizesPublicModelForms(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Extra: map[string]any{
			"moshu_reseller_managed":           true,
			MoshuResellerModelSnapshotExtraKey: []string{"gemini-3.1-pro-preview"},
		},
	}

	require.True(t, account.IsModelSupported("gemini-3.1-pro-preview-customtools"))
	require.True(t, account.IsModelSupported("models/gemini-3.1-pro-preview"))
}
