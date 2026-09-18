package moshureseller

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func validSettlementEvent(eventID, settlementID int64) RemoteSettlementEvent {
	return RemoteSettlementEvent{
		EventID:    eventID,
		Settlement: RemoteSettlement{ID: settlementID},
	}
}

func TestValidateSettlementEventPageAcceptsEmptyAndValidPages(t *testing.T) {
	require.NoError(t, validateSettlementEventPage(&RemoteSettlementEventPage{}))
	require.NoError(t, validateSettlementEventPage(&RemoteSettlementEventPage{
		Items: []RemoteSettlementEvent{validSettlementEvent(1, 11), validSettlementEvent(2, 12)},
	}))
}

func TestValidateSettlementEventPageRejectsMalformedPages(t *testing.T) {
	tests := []struct {
		name  string
		page  *RemoteSettlementEventPage
		match string
	}{
		{name: "nil page", page: nil, match: "page is empty"},
		{name: "zero event id", page: &RemoteSettlementEventPage{Items: []RemoteSettlementEvent{validSettlementEvent(0, 11)}}, match: "invalid id"},
		{name: "zero settlement id", page: &RemoteSettlementEventPage{Items: []RemoteSettlementEvent{validSettlementEvent(1, 0)}}, match: "invalid settlement id"},
		{name: "duplicate event id", page: &RemoteSettlementEventPage{Items: []RemoteSettlementEvent{validSettlementEvent(1, 11), validSettlementEvent(1, 12)}}, match: "duplicate id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSettlementEventPage(tt.page)
			require.ErrorContains(t, err, tt.match)
		})
	}
}

func TestValidateSettlementEventPageRejectsOversizedPage(t *testing.T) {
	page := &RemoteSettlementEventPage{Items: make([]RemoteSettlementEvent, settlementEventPageLimit+1)}
	for i := range page.Items {
		page.Items[i] = validSettlementEvent(int64(i+1), int64(i+1))
	}
	require.ErrorContains(t, validateSettlementEventPage(page), "exceeds limit")
}
