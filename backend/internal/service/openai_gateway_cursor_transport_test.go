//go:build unit

package service

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

func cursorAccount() *Account {
	return &Account{Platform: PlatformCursor, Type: AccountTypeOAuth}
}

func TestCursorAgentKnobsFallBackToProcessDefaults(t *testing.T) {
	defaults := cursorAgentProcessDefaults()
	account := cursorAccount()

	require.Equal(t, defaults.baseURL, cursorAgentBaseURL(account))
	require.Equal(t, defaults.clientVersion, cursorAgentClientVersion(account))
	require.Equal(t, defaults.ghostMode, cursorAgentGhostMode(account))
	require.Equal(t, defaults.baseURL, cursorAgentBaseURL(nil))

	// With no environment override the process defaults are the package pins,
	// which is the combination a live account was verified against.
	if os.Getenv(envCursorAgentBaseURL) == "" {
		require.Equal(t, cursorpkg.DefaultAgentBaseURL, defaults.baseURL)
	}
	if os.Getenv(envCursorAgentClientVersion) == "" {
		require.Equal(t, cursorpkg.DefaultCLIClientVersion, defaults.clientVersion)
	}
	if os.Getenv(envCursorAgentGhostMode) == "" {
		require.True(t, defaults.ghostMode)
	}
	if os.Getenv(envCursorAgentFirstByteTimeout) == "" {
		require.Equal(t, cursorpkg.AgentDefaultFirstByteTimeout, defaults.firstByteTimeout)
	}
	if os.Getenv(envCursorAgentIdleTimeout) == "" {
		require.Equal(t, cursorpkg.AgentDefaultIdleTimeout, defaults.idleTimeout)
	}
}

func TestCursorAgentKnobsPreferCredentialsThenExtra(t *testing.T) {
	account := cursorAccount()
	account.Extra = map[string]any{
		extraCursorAgentBaseURL:       "https://agentn.us.api5.cursor.sh/",
		extraCursorAgentClientVersion: "cli-2026.01.01-aaaaaaa",
		extraCursorAgentGhostMode:     "false",
	}
	require.Equal(t, "https://agentn.us.api5.cursor.sh", cursorAgentBaseURL(account))
	require.Equal(t, "cli-2026.01.01-aaaaaaa", cursorAgentClientVersion(account))
	require.False(t, cursorAgentGhostMode(account))

	account.Credentials = map[string]any{
		credCursorAgentBaseURL:       "https://agent.api5.cursor.sh",
		credCursorAgentClientVersion: "cli-2026.02.02-bbbbbbb",
		credCursorAgentGhostMode:     "true",
	}
	require.Equal(t, "https://agent.api5.cursor.sh", cursorAgentBaseURL(account))
	require.Equal(t, "cli-2026.02.02-bbbbbbb", cursorAgentClientVersion(account))
	require.True(t, cursorAgentGhostMode(account))
}

// An unparseable ghost-mode override must not silently flip privacy mode.
func TestCursorAgentGhostModeIgnoresUnparseableOverride(t *testing.T) {
	account := cursorAccount()
	account.Extra = map[string]any{extraCursorAgentGhostMode: "maybe"}
	require.Equal(t, cursorAgentProcessDefaults().ghostMode, cursorAgentGhostMode(account))
}

func TestParseEnvDuration(t *testing.T) {
	require.Equal(t, 90*time.Second, parseEnvDuration("90s", time.Second))
	require.Equal(t, time.Second, parseEnvDuration("", time.Second))
	require.Equal(t, time.Second, parseEnvDuration("not-a-duration", time.Second))
	require.Equal(t, time.Second, parseEnvDuration("-5s", time.Second))
}

func TestCursorAgentHTTPClientReusesOneClientPerProxy(t *testing.T) {
	t.Cleanup(resetCursorAgentClients)
	resetCursorAgentClients()

	direct, err := cursorAgentHTTPClient(cursorAccount())
	require.NoError(t, err)
	require.NotNil(t, direct)
	again, err := cursorAgentHTTPClient(cursorAccount())
	require.NoError(t, err)
	require.Same(t, direct, again)

	proxied := cursorAccount()
	proxied.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8080}
	first, err := cursorAgentHTTPClient(proxied)
	require.NoError(t, err)
	require.NotSame(t, direct, first)

	second, err := cursorAgentHTTPClient(proxied)
	require.NoError(t, err)
	require.Same(t, first, second)

	other := cursorAccount()
	other.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 9090}
	third, err := cursorAgentHTTPClient(other)
	require.NoError(t, err)
	require.NotSame(t, first, third)

	// The proxy has to reach the transport, or every turn would leak past it.
	transport, ok := first.Transport.(*http.Transport)
	require.True(t, ok)
	require.NotNil(t, transport.Proxy)
	require.True(t, transport.ForceAttemptHTTP2, "agent turns require HTTP/2")
}

func TestCursorAgentHTTPClientRejectsUnsupportedProxyScheme(t *testing.T) {
	t.Cleanup(resetCursorAgentClients)
	account := cursorAccount()
	account.Proxy = &Proxy{Protocol: "ftp", Host: "127.0.0.1", Port: 21}
	_, err := cursorAgentHTTPClient(account)
	require.Error(t, err)
}

func TestCursorAgentProxyClientCacheEvictsOldest(t *testing.T) {
	t.Cleanup(resetCursorAgentClients)
	resetCursorAgentClients()

	for port := 0; port < cursorAgentClientCacheMaxEntries+5; port++ {
		account := cursorAccount()
		account.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 10000 + port}
		_, err := cursorAgentHTTPClient(account)
		require.NoError(t, err)
	}

	cursorAgentClientMu.Lock()
	size := len(cursorAgentClients)
	cursorAgentClientMu.Unlock()
	require.LessOrEqual(t, size, cursorAgentClientCacheMaxEntries)
}

func TestValidateCursorAgentHostAppliesSSRFGuardOnlyWhenConfigured(t *testing.T) {
	private := "http://127.0.0.1:8080"

	// No config, or an allowlist that is off / permits private hosts: the
	// guard stays out of the way.
	require.NoError(t, validateCursorAgentHost(nil, private))
	require.NoError(t, validateCursorAgentHost(&config.Config{}, private))

	permissive := &config.Config{}
	permissive.Security.URLAllowlist.Enabled = true
	permissive.Security.URLAllowlist.AllowPrivateHosts = true
	require.NoError(t, validateCursorAgentHost(permissive, private))

	strict := &config.Config{}
	strict.Security.URLAllowlist.Enabled = true
	require.Error(t, validateCursorAgentHost(strict, private),
		"a private agent base url must be refused when the allowlist forbids private hosts")
	require.Error(t, validateCursorAgentHost(strict, "agentn.global.api5.cursor.sh"),
		"a base url with no scheme parses to an empty host and must be refused")
}

func resetCursorAgentClients() {
	cursorAgentClientMu.Lock()
	defer cursorAgentClientMu.Unlock()
	for key, entry := range cursorAgentClients {
		if entry != nil {
			closeCursorAgentClient(entry.client)
		}
		delete(cursorAgentClients, key)
	}
}
