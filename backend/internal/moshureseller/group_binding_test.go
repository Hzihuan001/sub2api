package moshureseller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductAccountGroupIDsPreservesExistingBindings(t *testing.T) {
	productGroupID := int64(30)
	existing := []int64{10, 20}

	got := productAccountGroupIDs(existing, &productGroupID)

	require.Equal(t, []int64{10, 20, 30}, got)
	// The caller's slice must not be mutated while the sync input is prepared.
	require.Equal(t, []int64{10, 20}, existing)
}

func TestProductAccountGroupIDsDoesNotDuplicateCurrentProductGroup(t *testing.T) {
	productGroupID := int64(20)

	require.Equal(t, []int64{10, 20}, productAccountGroupIDs([]int64{10, 20}, &productGroupID))
}

func TestProductAccountGroupIDsWithoutProductGroupPreservesBindings(t *testing.T) {
	existing := []int64{10, 20}

	got := productAccountGroupIDs(existing, nil)

	require.Equal(t, existing, got)
	require.NotSame(t, &existing[0], &got[0])
}
