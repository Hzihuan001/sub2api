package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelListingSource_EmptyResellerSnapshotRemainsAuthoritative(t *testing.T) {
	require.Equal(t, []string{"claude-sonnet-4-6"}, modelListingSource("deepseek", nil, []string{"claude-sonnet-4-6"}),
		"nil means discovery failed/legacy fallback is allowed")
	empty := modelListingSource("deepseek", []string{}, []string{"claude-sonnet-4-6"})
	require.NotNil(t, empty)
	require.Empty(t, empty)
}
