package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResellerPricingChangesBroadcastAndCoalesce(t *testing.T) {
	first, signal := ResellerPricingChangeCursor()
	other, otherSignal := ResellerPricingChangeCursor()
	require.Equal(t, first, other)
	NotifyResellerPricingChanged()
	select {
	case <-signal:
	default:
		t.Fatal("first receiver was not notified")
	}
	select {
	case <-otherSignal:
	default:
		t.Fatal("second receiver was not notified")
	}
	second, newSignal := ResellerPricingChangeCursor()
	require.NotEqual(t, first, second)
	select {
	case <-newSignal:
		t.Fatal("new listener must wait for the next change")
	default:
	}
	NotifyResellerPricingChanged()
	NotifyResellerPricingChanged()
	third, _ := ResellerPricingChangeCursor()
	require.NotEqual(t, second, third)
}
