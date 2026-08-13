package service

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// Transport knobs and the HTTP client for the agent.v1 conversation upstream.
//
// Every knob resolves account credentials → account extra → process default
// (environment) → pkg/cursor constant, so a value can be pinned for one
// account, for the whole deployment, or left to the package. The pinned
// defaults are the combination a live account was verified against: the
// global agentn host, the CLI build in DefaultCLIClientVersion, and ghost mode
// on.

const (
	envCursorAgentBaseURL          = "SUB2API_CURSOR_AGENT_BASE_URL"
	envCursorAgentClientVersion    = "SUB2API_CURSOR_AGENT_CLIENT_VERSION"
	envCursorAgentGhostMode        = "SUB2API_CURSOR_AGENT_GHOST_MODE"
	envCursorAgentFirstByteTimeout = "SUB2API_CURSOR_AGENT_FIRST_BYTE_TIMEOUT"
	envCursorAgentIdleTimeout      = "SUB2API_CURSOR_AGENT_IDLE_TIMEOUT"
)

// Per-account overrides. The credential keys sit next to the existing
// "base_url" (which stays api2's, for /v1/models); the extra keys are prefixed
// because accounts.extra is shared with every other concern.
const (
	credCursorAgentBaseURL       = "agent_base_url"
	credCursorAgentClientVersion = "agent_client_version"
	credCursorAgentGhostMode     = "agent_ghost_mode"

	extraCursorAgentBaseURL       = "cursor_agent_base_url"
	extraCursorAgentClientVersion = "cursor_agent_client_version"
	extraCursorAgentGhostMode     = "cursor_agent_ghost_mode"
)

// cursorAgentDefaults is the deployment-wide fallback, read from the
// environment once so the request path never touches os.Getenv.
type cursorAgentDefaults struct {
	baseURL          string
	clientVersion    string
	ghostMode        bool
	firstByteTimeout time.Duration
	idleTimeout      time.Duration
}

var (
	cursorAgentDefaultsOnce  sync.Once
	cursorAgentDefaultsCache cursorAgentDefaults
)

func cursorAgentProcessDefaults() cursorAgentDefaults {
	cursorAgentDefaultsOnce.Do(func() {
		cursorAgentDefaultsCache = cursorAgentDefaults{
			baseURL:          firstNonEmpty(strings.TrimSpace(os.Getenv(envCursorAgentBaseURL)), cursorpkg.DefaultAgentBaseURL),
			clientVersion:    firstNonEmpty(strings.TrimSpace(os.Getenv(envCursorAgentClientVersion)), cursorpkg.DefaultCLIClientVersion),
			ghostMode:        parseEnvBoolDefaultTrue(os.Getenv(envCursorAgentGhostMode)),
			firstByteTimeout: parseEnvDuration(os.Getenv(envCursorAgentFirstByteTimeout), cursorpkg.AgentDefaultFirstByteTimeout),
			idleTimeout:      parseEnvDuration(os.Getenv(envCursorAgentIdleTimeout), cursorpkg.AgentDefaultIdleTimeout),
		}
	})
	return cursorAgentDefaultsCache
}

func parseEnvDuration(raw string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

// cursorAgentBaseURL resolves the api5 host serving AgentService/Run. It is
// deliberately separate from GetCursorBaseURL: that one still points at api2,
// which continues to serve AvailableModels.
func cursorAgentBaseURL(account *Account) string {
	if override := cursorAgentAccountOverride(account, credCursorAgentBaseURL, extraCursorAgentBaseURL); override != "" {
		return strings.TrimRight(override, "/")
	}
	return cursorAgentProcessDefaults().baseURL
}

func cursorAgentClientVersion(account *Account) string {
	if override := cursorAgentAccountOverride(account, credCursorAgentClientVersion, extraCursorAgentClientVersion); override != "" {
		return override
	}
	return cursorAgentProcessDefaults().clientVersion
}

// cursorAgentGhostMode reports the x-ghost-mode value. Privacy mode also
// selects which api5 host actually serves the turn, so it travels with the
// base URL rather than being hard-coded.
func cursorAgentGhostMode(account *Account) bool {
	if override := cursorAgentAccountOverride(account, credCursorAgentGhostMode, extraCursorAgentGhostMode); override != "" {
		if parsed, err := strconv.ParseBool(override); err == nil {
			return parsed
		}
	}
	return cursorAgentProcessDefaults().ghostMode
}

func cursorAgentAccountOverride(account *Account, credentialKey, extraKey string) string {
	if account == nil {
		return ""
	}
	if value := strings.TrimSpace(account.GetCredential(credentialKey)); value != "" {
		return value
	}
	return strings.TrimSpace(account.GetExtraString(extraKey))
}

// Agent turns need an *http.Client rather than the shared HTTPUpstream port:
// the request body stays open for the life of the turn, so the transport must
// be handed to OpenAgentStream directly. Clients are cached per proxy so a
// busy account does not build a new connection pool per request.
const (
	cursorAgentClientCacheMaxEntries = 64
	cursorAgentClientCacheIdleTTL    = 30 * time.Minute
)

type cursorAgentClientEntry struct {
	client       *http.Client
	lastUsedNano int64
}

var (
	cursorAgentClientMu      sync.Mutex
	cursorAgentClients       = make(map[string]*cursorAgentClientEntry)
	cursorAgentDirectClient  *http.Client
	cursorAgentDirectOnce    sync.Once
	errCursorAgentProxyEmpty = errors.New("cursor: proxy url is empty")
	errCursorAgentTransport  = errors.New("cursor: agent transport is not an *http.Transport")
)

// cursorAgentHTTPClient returns an HTTP/2-capable client honouring the
// account's proxy. A direct account shares one process-wide client; proxied
// accounts get one client per proxy URL, evicted when idle.
func cursorAgentHTTPClient(account *Account) (*http.Client, error) {
	proxyURL := ""
	if account != nil && account.Proxy != nil {
		proxyURL = strings.TrimSpace(account.Proxy.URL())
	}
	if proxyURL == "" {
		cursorAgentDirectOnce.Do(func() {
			cursorAgentDirectClient = cursorpkg.NewAgentHTTPClient()
		})
		return cursorAgentDirectClient, nil
	}
	return cursorAgentProxyHTTPClient(proxyURL)
}

func cursorAgentProxyHTTPClient(proxyURL string) (*http.Client, error) {
	if strings.TrimSpace(proxyURL) == "" {
		return nil, errCursorAgentProxyEmpty
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("cursor: invalid proxy url: %w", err)
	}
	now := time.Now().UnixNano()

	cursorAgentClientMu.Lock()
	defer cursorAgentClientMu.Unlock()
	if entry, ok := cursorAgentClients[proxyURL]; ok && entry != nil && entry.client != nil {
		entry.lastUsedNano = now
		return entry.client, nil
	}
	evictIdleCursorAgentClientsLocked(now)

	client := cursorpkg.NewAgentHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		return nil, errCursorAgentTransport
	}
	if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
		return nil, fmt.Errorf("cursor: configure agent proxy: %w", err)
	}
	cursorAgentClients[proxyURL] = &cursorAgentClientEntry{client: client, lastUsedNano: now}
	evictOldestCursorAgentClientsLocked()
	return client, nil
}

func evictIdleCursorAgentClientsLocked(nowNano int64) {
	now := time.Unix(0, nowNano)
	for key, entry := range cursorAgentClients {
		if entry == nil || entry.client == nil {
			delete(cursorAgentClients, key)
			continue
		}
		if now.Sub(time.Unix(0, entry.lastUsedNano)) > cursorAgentClientCacheIdleTTL {
			closeCursorAgentClient(entry.client)
			delete(cursorAgentClients, key)
		}
	}
}

func evictOldestCursorAgentClientsLocked() {
	for len(cursorAgentClients) > cursorAgentClientCacheMaxEntries {
		oldestKey := ""
		oldestNano := int64(0)
		for key, entry := range cursorAgentClients {
			lastUsed := int64(0)
			if entry != nil {
				lastUsed = entry.lastUsedNano
			}
			if oldestKey == "" || lastUsed < oldestNano {
				oldestKey = key
				oldestNano = lastUsed
			}
		}
		if oldestKey == "" {
			return
		}
		if entry := cursorAgentClients[oldestKey]; entry != nil {
			closeCursorAgentClient(entry.client)
		}
		delete(cursorAgentClients, oldestKey)
	}
}

func closeCursorAgentClient(client *http.Client) {
	if client == nil {
		return
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
}

// validateCursorAgentHost repeats the SSRF guard httpUpstream applies to every
// other upstream call. Agent turns dial directly — the request body stays open
// for the whole turn, which the shared Do(req) port cannot express — so an
// operator-configured agent base URL would otherwise skip the check.
func validateCursorAgentHost(cfg *config.Config, baseURL string) error {
	if cfg == nil || !cfg.Security.URLAllowlist.Enabled || cfg.Security.URLAllowlist.AllowPrivateHosts {
		return nil
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return fmt.Errorf("cursor: invalid agent base url: %w", err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("cursor: agent base url %q has no host", baseURL)
	}
	return urlvalidator.ValidateResolvedIP(host)
}
