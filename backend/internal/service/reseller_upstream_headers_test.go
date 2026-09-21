package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestApplyResellerCredentialHeadersGeneratesFreshUUIDAndProtectsHeaders(t *testing.T) {
	headers := http.Header{
		"x-reseller-request-id":     []string{"caller-controlled"},
		"x-reseller-request-source": []string{"caller-controlled"},
	}

	applyResellerCredentialHeaders(headers, "sk-rs_station-key", resellerRequestSourceMonitor)
	firstID := headers.Get(resellerRequestIDHeader)
	require.NotEmpty(t, firstID)
	require.NoError(t, func() error { _, err := uuid.Parse(firstID); return err }())
	require.Equal(t, resellerRequestSourceMonitor, headers.Get(resellerRequestSourceHeader))
	require.Len(t, headers.Values(resellerRequestIDHeader), 1)
	require.Len(t, headers.Values(resellerRequestSourceHeader), 1)

	applyResellerCredentialHeaders(headers, "sk-rs_station-key", resellerRequestSourceMonitor)
	require.NotEqual(t, firstID, headers.Get(resellerRequestIDHeader), "each outbound attempt must get a new UUID")
}

func TestApplyResellerCredentialHeadersIgnoresOrdinaryProviderKeys(t *testing.T) {
	headers := make(http.Header)
	applyResellerCredentialHeaders(headers, "sk-provider-key", resellerRequestSourceMonitor)
	require.Empty(t, headers.Get(resellerRequestIDHeader))
	require.Empty(t, headers.Get(resellerRequestSourceHeader))
}

func TestApplyResellerAccountHeadersUsesMarkerDuringRollingUpgrade(t *testing.T) {
	headers := http.Header{
		"x-reseller-request-id":     []string{"stale-id"},
		"x-reseller-request-source": []string{"monitor"},
	}
	account := &Account{
		Credentials: map[string]any{"api_key": "rotated-key"},
		Extra:       map[string]any{"moshu_reseller_managed": true},
	}
	applyResellerAccountHeaders(headers, account, resellerRequestSourceUser)
	id := headers.Get(resellerRequestIDHeader)
	require.NotEqual(t, "stale-id", id)
	require.NoError(t, func() error { _, err := uuid.Parse(id); return err }())
	require.Equal(t, resellerRequestSourceUser, headers.Get(resellerRequestSourceHeader))
}

func TestApplyResellerAccountHeadersDoesNotTrustHeaderOnlyMarker(t *testing.T) {
	headers := make(http.Header)
	headers.Set(resellerRequestIDHeader, "caller-id")
	account := &Account{Credentials: map[string]any{"api_key": "sk-provider-key"}}
	applyResellerAccountHeaders(headers, account, resellerRequestSourceUser)
	require.Equal(t, "caller-id", headers.Get(resellerRequestIDHeader))
}
