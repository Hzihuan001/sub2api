package migrations

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImageNoDiscountMigrationDoesNotRewriteHistoricalBilling(t *testing.T) {
	content, err := FS.ReadFile("9000_custom_batch_image_no_discount.sql")
	require.NoError(t, err)

	// Ignore comments so the historical-billing warning itself does not count
	// as a mutation. Defaults may change on jobs; existing rows may not.
	sql := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(string(content), " ")
	sql = regexp.MustCompile(`(?m)--[^\r\n]*`).ReplaceAllString(sql, " ")
	sql = strings.ToLower(strings.Join(strings.Fields(sql), " "))
	protectedUpdate := regexp.MustCompile(`\bupdate\s+(?:only\s+)?(?:"?[a-z_][a-z0-9_]*"?\s*\.\s*)?"?(?:batch_image_jobs|users|usage_logs)"?\b`)
	require.NotRegexp(t, protectedUpdate, sql,
		"the migration must not reprice existing jobs, debit users, or rewrite usage logs")

	// Restrict data mutations to group configuration. This also catches
	// destructive replacement of billing rows instead of a direct UPDATE.
	mutation := regexp.MustCompile(`\b(update|insert\s+into|delete\s+from|merge\s+into|truncate(?:\s+table)?)\s+(?:only\s+)?(?:"?[a-z_][a-z0-9_]*"?\s*\.\s*)?"?([a-z_][a-z0-9_]*)"?`)
	matches := mutation.FindAllStringSubmatch(sql, -1)
	require.NotEmpty(t, matches, "the migration must normalize existing group configuration")
	for _, match := range matches {
		require.Equal(t, "update", match[1], "unexpected data mutation: %s", match[0])
		require.Equal(t, "groups", match[2], "only group configuration may be rewritten: %s", match[0])
	}
}
