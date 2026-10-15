package migrations

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDropPlatformCheckConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("242_drop_platform_check_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE user_platform_quotas DROP CONSTRAINT IF EXISTS user_platform_quotas_platform_check;")
	require.Contains(t, sql,
		"ALTER TABLE composite_model_routes DROP CONSTRAINT IF EXISTS composite_model_routes_target_platform_check;")
	require.NotContains(t, sql, "ADD CONSTRAINT")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitors_provider_check")
	require.NotContains(t, sql, "DROP CONSTRAINT IF EXISTS channel_monitor_request_templates_provider_check")
}

// The deployed custom 245 migration repairs the four platform CHECKs after the
// historical custom merge. Later replacements must retain that complete set,
// rather than restore an upstream-only whitelist that rejects existing rows.
func TestLaterPlatformConstraintsPreserveDeployedPlatformSet(t *testing.T) {
	const droppedAt = "242_drop_platform_check_constraints.sql"
	entries, err := fs.ReadDir(FS, ".")
	require.NoError(t, err)
	comments := regexp.MustCompile(`(?ms)/\*.*?\*/|--[^\n]*`)
	constraintNames := `user_platform_quotas_platform_check|composite_model_routes_target_platform_check|channel_monitors_provider_check|channel_monitor_request_templates_provider_check`
	addConstraint := regexp.MustCompile(`(?i)\bADD\s+CONSTRAINT\s+"?(` + constraintNames + `)\b`)
	platformCheck := regexp.MustCompile(`(?is)\bADD\s+CONSTRAINT\s+"?(` + constraintNames + `)"?\s+CHECK\s*\(\s*(platform|target_platform|provider)\s+IN\s*\(([^)]*)\)\s*\)`)
	wantPlatforms := []string{"anthropic", "openai", "gemini", "antigravity", "grok", "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "kiro", "cursor"}
	wantColumns := map[string]string{
		"user_platform_quotas_platform_check":              "platform",
		"composite_model_routes_target_platform_check":     "target_platform",
		"channel_monitors_provider_check":                  "provider",
		"channel_monitor_request_templates_provider_check": "provider",
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") || entry.Name() <= droppedAt {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			content, err := FS.ReadFile(entry.Name())
			require.NoError(t, err)
			sql := comments.ReplaceAll(content, nil)
			checks := platformCheck.FindAllSubmatch(sql, -1)
			require.Len(t, checks, len(addConstraint.FindAll(sql, -1)), "every replacement platform constraint must have a verifiable IN check")
			for _, check := range checks {
				name := strings.ToLower(string(check[1]))
				require.Equal(t, wantColumns[name], strings.ToLower(string(check[2])))
				var platforms []string
				for _, value := range strings.Split(string(check[3]), ",") {
					platforms = append(platforms, strings.Trim(strings.TrimSpace(value), "'"))
				}
				require.ElementsMatch(t, wantPlatforms, platforms, "%s must retain the deployed full platform set", name)
			}
		})
	}
}
