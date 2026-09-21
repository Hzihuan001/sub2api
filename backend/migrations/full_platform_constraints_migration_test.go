package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFullPlatformConstraintsRepairMigration(t *testing.T) {
	content, err := FS.ReadFile("245_restore_full_platform_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, fragment := range []string{
		"CHECK (platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'))",
		"CHECK (target_platform IN ('anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'))",
		"CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok', 'antigravity', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go', 'kiro', 'cursor'))",
	} {
		require.Contains(t, sql, fragment)
	}
}
