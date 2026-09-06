package admin

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/stretchr/testify/require"
)

func TestRedactManagerEmailSettings(t *testing.T) {
	settings := dto.SystemSettings{
		SMTPHost:                        "smtp.example.com",
		SMTPPort:                        587,
		SMTPUsername:                    "owner",
		SMTPPasswordConfigured:          true,
		SMTPFrom:                        "owner@example.com",
		SMTPFromName:                    "Owner",
		SMTPUseTLS:                      true,
		BalanceLowNotifyEnabled:         true,
		BalanceLowNotifyThreshold:       10,
		BalanceLowNotifyRechargeURL:     "https://example.com/recharge",
		SubscriptionExpiryNotifyEnabled: true,
		AccountQuotaNotifyEnabled:       true,
		AccountQuotaNotifyEmails:        []dto.NotifyEmailEntry{{Email: "owner@example.com"}},
	}

	redactManagerEmailSettings(&settings)

	require.Empty(t, settings.SMTPHost)
	require.Zero(t, settings.SMTPPort)
	require.False(t, settings.SMTPPasswordConfigured)
	require.False(t, settings.BalanceLowNotifyEnabled)
	require.False(t, settings.SubscriptionExpiryNotifyEnabled)
	require.False(t, settings.AccountQuotaNotifyEnabled)
	require.Nil(t, settings.AccountQuotaNotifyEmails)
}
