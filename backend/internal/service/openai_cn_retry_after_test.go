//go:build unit

package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCNProviderReactive429_SnapshotHonorsRetryAfterFloor(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo} {
		t.Run(platform, func(t *testing.T) {
			for _, retryAfter := range []string{"120", "http-date", "1", "invalid", "-1", ""} {
				t.Run(retryAfter, func(t *testing.T) {
					now := time.Now().Truncate(time.Second)
					windowReset := now.Add(30 * time.Second)
					account := &Account{
						ID: 8201, Platform: platform, Type: AccountTypeAPIKey,
						Credentials: map[string]any{"account_mode": AccountModeCoding},
						Extra: map[string]any{
							cnExtraKey(platform, cnExtraSuffix5hReset):     windowReset.Format(time.RFC3339),
							cnExtraKey(platform, cnExtraSuffixWeeklyReset): now.Add(24 * time.Hour).Format(time.RFC3339),
						},
					}
					headers := http.Header{}
					if retryAfter == "http-date" {
						headers.Set("Retry-After", now.Add(2*time.Minute).UTC().Format(http.TimeFormat))
					} else if retryAfter != "" {
						headers.Set("Retry-After", retryAfter)
					}
					repo := &rateLimitAccountRepoStub{}
					service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
					before := time.Now()
					require.True(t, service.applyCNProviderReactive429(context.Background(), account, headers, nil))
					after := time.Now()
					require.Equal(t, 1, repo.rateLimitedCalls)
					require.Zero(t, repo.tempCalls)
					switch retryAfter {
					case "120":
						require.False(t, repo.lastRateLimitedAt.Before(before.Add(2*time.Minute)))
						require.False(t, repo.lastRateLimitedAt.After(after.Add(2*time.Minute)))
					case "http-date":
						require.Equal(t, now.Add(2*time.Minute).Unix(), repo.lastRateLimitedAt.Unix())
					default:
						require.Equal(t, windowReset.Unix(), repo.lastRateLimitedAt.Unix(), "preserve the earliest quota window, not the weekly reset")
					}
				})
			}
		})
	}
}

func TestOpenCodeGoReactive429_BodyResetHonorsRetryAfterFloor(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resetAfter time.Duration
		retryAfter string
		wantHeader bool
		wantHandle bool
	}{
		{name: "longer numeric header", resetAfter: 30 * time.Second, retryAfter: "120", wantHeader: true, wantHandle: true},
		{name: "longer date header", resetAfter: 30 * time.Second, retryAfter: "http-date", wantHeader: true, wantHandle: true},
		{name: "longer body window", resetAfter: 5 * time.Minute, retryAfter: "120", wantHandle: true},
		{name: "invalid header", resetAfter: 30 * time.Second, retryAfter: "invalid", wantHandle: true},
		{name: "past body with valid header", resetAfter: -time.Minute, retryAfter: "120", wantHeader: true, wantHandle: true},
		{name: "past body without valid header", resetAfter: -time.Minute, retryAfter: "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().Truncate(time.Second)
			bodyReset := now.Add(tc.resetAfter)
			body := []byte(fmt.Sprintf(`{"error":{"type":"GoUsageLimitError","resets_at":%d}}`, bodyReset.Unix()))
			headers := http.Header{}
			if tc.retryAfter == "http-date" {
				headers.Set("Retry-After", now.Add(2*time.Minute).UTC().Format(http.TimeFormat))
			} else {
				headers.Set("Retry-After", tc.retryAfter)
			}
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{ID: 8202, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey}
			before := time.Now()
			require.Equal(t, tc.wantHandle, service.applyCNProviderReactive429(context.Background(), account, headers, body))
			after := time.Now()
			if !tc.wantHandle {
				require.Zero(t, repo.rateLimitedCalls, "an expired reset must fall through to the normal cooldown policy")
				return
			}
			require.Equal(t, 1, repo.rateLimitedCalls)
			require.Zero(t, repo.tempCalls)
			if !tc.wantHeader {
				require.Equal(t, bodyReset.Unix(), repo.lastRateLimitedAt.Unix())
			} else if tc.retryAfter == "http-date" {
				require.Equal(t, now.Add(2*time.Minute).Unix(), repo.lastRateLimitedAt.Unix())
			} else {
				require.False(t, repo.lastRateLimitedAt.Before(before.Add(2*time.Minute)))
				require.False(t, repo.lastRateLimitedAt.After(after.Add(2*time.Minute)))
			}
		})
	}
}

func TestCNProviderReactive429_BalanceKeepsDedicatedRecovery(t *testing.T) {
	for _, platform := range []string{PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax} {
		t.Run(platform, func(t *testing.T) {
			repo := &rateLimitAccountRepoStub{}
			service := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			account := &Account{ID: 8203, Platform: platform, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"account_mode": AccountModeCoding},
				Extra:       map[string]any{cnExtraKey(platform, cnExtraSuffix5hReset): time.Now().Add(time.Minute).Format(time.RFC3339)}}
			headers := http.Header{"Retry-After": []string{"7200"}}
			body := []byte(`{"error":{"message":"Insufficient balance"}}`)

			require.True(t, service.applyCNProviderReactive429(context.Background(), account, headers, body))
			require.Equal(t, 1, repo.tempCalls)
			require.Contains(t, repo.lastTempReason, cnBalanceLowReasonPrefix)
			require.Equal(t, true, repo.lastExtraUpdates[cnExtraKey(platform, cnBalanceExtraSuffixLow)])
			require.Zero(t, repo.rateLimitedCalls, "balance detection must retain its dedicated temporary recovery policy")
			require.Zero(t, repo.setErrorCalls)
		})
	}
}
