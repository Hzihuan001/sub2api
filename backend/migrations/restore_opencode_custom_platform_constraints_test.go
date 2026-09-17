package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRestoreOpenCodeCustomPlatformConstraintsMigration(t *testing.T) {
	content, err := FS.ReadFile("239_restore_opencode_custom_platform_constraints.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, name := range []string{
		"user_platform_quotas_platform_check",
		"composite_model_routes_target_platform_check",
		"channel_monitors_provider_check",
		"channel_monitor_request_templates_provider_check",
	} {
		require.Contains(t, sql, "DROP CONSTRAINT IF EXISTS "+name)
		require.Contains(t, sql, "ADD CONSTRAINT "+name)
	}
	require.Equal(t, 4, strings.Count(sql, "IF constraint_def IS NULL"))
	checks := regexp.MustCompile(`CHECK \((?:platform|target_platform|provider) IN \([^)]*\)\)`).FindAllString(sql, -1)
	require.Len(t, checks, 4)
	for _, platform := range []string{"opencode_go", "kiro", "cursor"} {
		require.Equal(t, 4, strings.Count(sql, "position('"+platform+"' IN constraint_def) = 0"))
		for _, check := range checks {
			require.Contains(t, check, "'"+platform+"'")
		}
	}
}
