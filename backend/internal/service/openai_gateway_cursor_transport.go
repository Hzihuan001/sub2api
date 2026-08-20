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
	baseURL, _ := cursorAgentBaseURLSource(account)
	return baseURL
}

// cursorAgentBaseURLSource also reports whether the value came from the
// account row. That distinction drives how hard it is validated: a per-account
// override is operator input that reaches the request path through the
// database, while the process default is compiled-in or set by env at boot.
func cursorAgentBaseURLSource(account *Account) (string, bool) {
	if override := cursorAgentAccountOverride(account, credCursorAgentBaseURL, extraCursorAgentBaseURL); override != "" {
		return strings.TrimRight(override, "/"), true
	}
	return cursorAgentProcessDefaults().baseURL, false
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

	// errCursorAgentProxyUnresolved fails a turn closed when the account names a
	// proxy that was not loaded. Falling back to a direct dial would send the
	// account's real credential from the gateway's own IP, which is the exact
	// outcome configuring a proxy is meant to prevent.
	errCursorAgentProxyUnresolved = errors.New("cursor: account has a proxy configured but it was not resolved")
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
		// A configured-but-missing proxy is a load failure, not a request for a
		// direct connection.
		if account != nil && account.ProxyID != nil {
			return nil, errCursorAgentProxyUnresolved
		}
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

// validateCursorAgentHost applies the same guard httpUpstream applies to every
// other upstream call. Agent turns dial directly — the request body stays open
// for the whole turn, which the shared Do(req) port cannot express — so an
// operator-configured agent base URL would otherwise skip validation entirely
// and this credential-bearing stream would go wherever the value pointed.
//
// Three layers, matching the rest of the codebase: scheme and host format
// always; the operator's UpstreamHosts allowlist for a per-account override,
// which is the untrusted input here (the process default is compiled in or set
// at boot, and holding it to a list the operator wrote for third-party relays
// would break every default deployment); then a resolved-IP check against DNS
// rebinding, since validation and dialling are separate steps.
func validateCursorAgentHost(cfg *config.Config, baseURL string, accountOverride bool) error {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		return errors.New("cursor: agent base url is empty")
	}

	allowInsecureHTTP := false
	allowlistEnabled := false
	allowPrivate := false
	var upstreamHosts []string
	if cfg != nil {
		allowInsecureHTTP = cfg.Security.URLAllowlist.AllowInsecureHTTP
		allowlistEnabled = cfg.Security.URLAllowlist.Enabled
		allowPrivate = cfg.Security.URLAllowlist.AllowPrivateHosts
		upstreamHosts = cfg.Security.URLAllowlist.UpstreamHosts
	}

	if allowlistEnabled && accountOverride {
		if _, err := urlvalidator.ValidateHTTPURL(trimmed, allowInsecureHTTP, urlvalidator.ValidationOptions{
			AllowedHosts:     upstreamHosts,
			RequireAllowlist: true,
			AllowPrivate:     allowPrivate,
		}); err != nil {
			return fmt.Errorf("cursor: agent base url rejected: %w", err)
		}
	} else if _, err := urlvalidator.ValidateHTTPURL(trimmed, allowInsecureHTTP, urlvalidator.ValidationOptions{
		AllowPrivate: allowPrivate || !allowlistEnabled,
	}); err != nil {
		return fmt.Errorf("cursor: agent base url rejected: %w", err)
	}

	if !allowlistEnabled || allowPrivate {
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("cursor: invalid agent base url: %w", err)
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("cursor: agent base url %q has no host", baseURL)
	}
	return urlvalidator.ValidateResolvedIP(host)
}
