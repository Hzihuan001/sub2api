package migrations

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLateDiscoveredPlatformMigrationsKeepSuperset protects upgrades where
// migrations imported by a custom build are discovered after the database has
// already applied later upstream migrations. Every replacement constraint must
// accept the complete current platform set; otherwise existing rows can make the
// upgrade fail before the final repair migration is reached.
func TestLateDiscoveredPlatformMigrationsKeepSuperset(t *testing.T) {
	tests := []struct {
		file   string
		column string
	}{
		{"145_allow_kiro_user_platform_quotas.sql", "platform"},
		{"222_add_cursor_platform.sql", "platform"},
		{"222_add_cursor_platform.sql", "target_platform"},
		{"222_add_cursor_platform.sql", "provider"},
		{"227_user_platform_quotas_restore_kiro.sql", "platform"},
		{"229_composite_routes_add_kiro.sql", "target_platform"},
		{"229_channel_monitor_kiro_provider.sql", "provider"},
		{"230_restore_cursor_platform_constraints.sql", "platform"},
		{"230_restore_cursor_platform_constraints.sql", "target_platform"},
		{"230_restore_cursor_platform_constraints.sql", "provider"},
	}

	want := append([]string(nil), expectedUserPlatformQuotaPlatforms...)
	sort.Strings(want)

	for _, tc := range tests {
		t.Run(tc.file+"/"+tc.column, func(t *testing.T) {
			body, err := FS.ReadFile(tc.file)
			require.NoError(t, err)
			normalized := strings.Join(strings.Fields(string(body)), " ")
			re := regexp.MustCompile(`CHECK \(` + regexp.QuoteMeta(tc.column) + ` IN \(([^)]*)\)\)`)
			matches := re.FindAllStringSubmatch(normalized, -1)
			require.NotEmpty(t, matches)

			for _, match := range matches {
				var got []string
				for _, raw := range strings.Split(match[1], ",") {
					got = append(got, strings.Trim(strings.TrimSpace(raw), "'"))
				}
				sort.Strings(got)
				require.Equal(t, want, got,
					"%s must keep the complete platform superset for late-discovery upgrades", tc.file)
			}
		})
	}
}
