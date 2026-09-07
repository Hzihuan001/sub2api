package service

import (
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestResellerGroupRateAllowsZeroWithoutChangingOrdinaryGroups(t *testing.T) {
	require.NoError(t, validateGroupRateMultiplier(0, true))
	require.Error(t, validateGroupRateMultiplier(0, false))
	for _, allowZero := range []bool{false, true} {
		for _, rate := range []float64{0.0001, 0.01, 1, 100} {
			require.NoError(t, validateGroupRateMultiplier(rate, allowZero))
		}
		for _, rate := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
			require.Error(t, validateGroupRateMultiplier(rate, allowZero))
		}
	}
}
