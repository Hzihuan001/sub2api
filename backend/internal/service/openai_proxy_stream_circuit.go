package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	defaultOpenAIProxyStreamFailureThreshold  = 2
	defaultOpenAIProxyStreamFailureWindow     = time.Minute
	defaultOpenAIProxyStreamQuarantineTTL     = 10 * time.Minute
	defaultOpenAIProxyStreamCircuitMaxEntries = 4096
	// defaultOpenAIProxyStreamFailureCollapse merges disconnects that land
	// within this interval into a single failure event. One proxy or HTTP/2
	// connection loss kills every stream multiplexed on it at the same
	// moment; counting each stream separately would trip the breaker from a
	// single upstream event (#5056).
	defaultOpenAIProxyStreamFailureCollapse = 3 * time.Second
	// openAIProxyStreamFailOpenLogInterval rate-limits the fail-open warning
	// so an outage-mode burst does not flood the log.
	openAIProxyStreamFailOpenLogInterval = 5 * time.Second
)

type openAIProxyStreamCircuitSettings struct {
	disabled         bool
	failureThreshold int
	failureWindow    time.Duration
	quarantineTTL    time.Duration
	collapseInterval time.Duration
	maxEntries       int
}

type openAIProxyStreamCircuitEntry struct {
	failureCount  int
	windowStart   time.Time
	lastFailureAt time.Time
	blockedUntil  time.Time
	lastTouched   time.Time
}

// openAIProxyStreamCircuitKey keeps observations for a shared proxy (or direct
// account) independent per OpenAI-compatible platform. A zero platform is a
// legacy wildcard used by older callers/tests and applies to every platform.
type openAIProxyStreamCircuitKey struct {
	id       int64
	platform string
}

func normalizeOpenAIStreamCircuitPlatform(platform string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return ""
	}
	return NormalizeOpenAICompatiblePlatform(platform)
}

// openAIProxyStreamCircuit is an in-process, bounded circuit. The same type is
// used for proxy IDs and direct-account IDs; the owner selects the correct
// instance/key space. It is intentionally ephemeral: a restart clears
// observations, while a tripped entry expires automatically after its TTL.
type openAIProxyStreamCircuit struct {
	mu       sync.Mutex
	settings openAIProxyStreamCircuitSettings
	entries  map[openAIProxyStreamCircuitKey]openAIProxyStreamCircuitEntry
}

func resolveOpenAIProxyStreamCircuitSettings(s *OpenAIGatewayService) openAIProxyStreamCircuitSettings {
	settings := openAIProxyStreamCircuitSettings{
		failureThreshold: defaultOpenAIProxyStreamFailureThreshold,
		failureWindow:    defaultOpenAIProxyStreamFailureWindow,
		quarantineTTL:    defaultOpenAIProxyStreamQuarantineTTL,
		collapseInterval: defaultOpenAIProxyStreamFailureCollapse,
		maxEntries:       defaultOpenAIProxyStreamCircuitMaxEntries,
	}
	if s == nil || s.cfg == nil {
		return settings
	}
	cfg := s.cfg.Gateway.OpenAIProxyStreamCircuit
	settings.disabled = cfg.Disabled
	if cfg.FailureThreshold > 0 {
		settings.failureThreshold = cfg.FailureThreshold
	}
	if cfg.WindowSeconds > 0 {
		settings.failureWindow = time.Duration(cfg.WindowSeconds) * time.Second
	}
	if cfg.TTLSeconds > 0 {
		settings.quarantineTTL = time.Duration(cfg.TTLSeconds) * time.Second
	}
	return settings
}

func newOpenAIProxyStreamCircuit(settings openAIProxyStreamCircuitSettings) *openAIProxyStreamCircuit {
	if settings.failureThreshold <= 0 {
		settings.failureThreshold = defaultOpenAIProxyStreamFailureThreshold
	}
	if settings.failureWindow <= 0 {
		settings.failureWindow = defaultOpenAIProxyStreamFailureWindow
	}
	if settings.quarantineTTL <= 0 {
		settings.quarantineTTL = defaultOpenAIProxyStreamQuarantineTTL
	}
	if settings.maxEntries <= 0 {
		settings.maxEntries = defaultOpenAIProxyStreamCircuitMaxEntries
	}
	if settings.collapseInterval < 0 {
		settings.collapseInterval = 0
	}
	return &openAIProxyStreamCircuit{
		settings: settings,
		entries:  make(map[openAIProxyStreamCircuitKey]openAIProxyStreamCircuitEntry),
	}
}

func (s *OpenAIGatewayService) getOpenAIProxyStreamCircuit() *openAIProxyStreamCircuit {
	if s == nil {
		return nil
	}
	s.openaiProxyStreamCircuitOnce.Do(func() {
		if s.openaiProxyStreamCircuit == nil {
			s.openaiProxyStreamCircuit = newOpenAIProxyStreamCircuit(resolveOpenAIProxyStreamCircuitSettings(s))
		}
	})
	return s.openaiProxyStreamCircuit
}

// getOpenAIAccountStreamCircuit returns the circuit for direct API-key
// accounts. Keeping a separate map from the proxy circuit avoids an ID-space
// collision (account 15 and proxy 15 are unrelated resources) and preserves
// the existing proxy-wide quarantine semantics.
func (s *OpenAIGatewayService) getOpenAIAccountStreamCircuit() *openAIProxyStreamCircuit {
	if s == nil {
		return nil
	}
	s.openaiAccountStreamCircuitOnce.Do(func() {
		if s.openaiAccountStreamCircuit == nil {
			s.openaiAccountStreamCircuit = newOpenAIProxyStreamCircuit(resolveOpenAIProxyStreamCircuitSettings(s))
		}
	})
	return s.openaiAccountStreamCircuit
}

func (c *openAIProxyStreamCircuit) recordFailure(proxyID int64, now time.Time) (bool, time.Time) {
	return c.recordFailureForPlatform(proxyID, "", now)
}

func (c *openAIProxyStreamCircuit) recordFailureForPlatform(proxyID int64, platform string, now time.Time) (bool, time.Time) {
	if c == nil || c.settings.disabled || proxyID <= 0 {
		return false, time.Time{}
	}
	key := openAIProxyStreamCircuitKey{id: proxyID, platform: normalizeOpenAIStreamCircuitPlatform(platform)}
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.entries[key]
	if exists && now.Before(entry.blockedUntil) {
		entry.lastTouched = now
		c.entries[key] = entry
		return false, entry.blockedUntil
	}
	if !exists {
		c.ensureCapacityLocked(now)
	}
	if entry.windowStart.IsZero() || now.Before(entry.windowStart) || now.Sub(entry.windowStart) > c.settings.failureWindow {
		entry.failureCount = 0
		entry.windowStart = now
		entry.blockedUntil = time.Time{}
	}
	// Collapse a burst of disconnects into one failure event: when a proxy or
	// a multiplexed HTTP/2 connection dies, every in-flight stream reports the
	// same underlying incident within moments of each other.
	if c.settings.collapseInterval > 0 && !entry.lastFailureAt.IsZero() &&
		now.Sub(entry.lastFailureAt) >= 0 && now.Sub(entry.lastFailureAt) < c.settings.collapseInterval {
		entry.lastTouched = now
		c.entries[key] = entry
		return false, time.Time{}
	}
	entry.failureCount++
	entry.lastFailureAt = now
	entry.lastTouched = now
	tripped := entry.failureCount >= c.settings.failureThreshold
	if tripped {
		entry.blockedUntil = now.Add(c.settings.quarantineTTL)
	}
	c.entries[key] = entry
	return tripped, entry.blockedUntil
}

func (c *openAIProxyStreamCircuit) recordSuccess(proxyID int64) bool {
	return c.recordSuccessForPlatform(proxyID, "")
}

func (c *openAIProxyStreamCircuit) recordSuccessForPlatform(proxyID int64, platform string) bool {
	if c == nil || proxyID <= 0 {
		return false
	}
	platform = normalizeOpenAIStreamCircuitPlatform(platform)
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := []openAIProxyStreamCircuitKey{{id: proxyID, platform: platform}}
	if platform != "" {
		// A legacy wildcard observation remains valid for all platforms.
		keys = append(keys, openAIProxyStreamCircuitKey{id: proxyID})
	}
	for _, key := range keys {
		if _, ok := c.entries[key]; !ok {
			continue
		}
		delete(c.entries, key)
		return true
	}
	return false
}

func (c *openAIProxyStreamCircuit) isBlocked(proxyID int64, now time.Time) bool {
	return c.isBlockedForPlatform(proxyID, "", now)
}

func (c *openAIProxyStreamCircuit) isBlockedForPlatform(proxyID int64, platform string, now time.Time) bool {
	if c == nil || c.settings.disabled || proxyID <= 0 {
		return false
	}
	platform = normalizeOpenAIStreamCircuitPlatform(platform)
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := []openAIProxyStreamCircuitKey{{id: proxyID, platform: platform}}
	if platform != "" {
		keys = append(keys, openAIProxyStreamCircuitKey{id: proxyID})
	}
	for _, key := range keys {
		entry, ok := c.entries[key]
		if !ok || entry.blockedUntil.IsZero() {
			continue
		}
		if !now.Before(entry.blockedUntil) {
			delete(c.entries, key)
			continue
		}
		return true
	}
	return false
}

// activeBlockCount reports how many proxies are currently quarantined. It
// gates the fail-open retry: a "no available accounts" selection result only
// warrants a second, quarantine-blind pass when the circuit is actually
// withholding capacity.
func (c *openAIProxyStreamCircuit) activeBlockCount(now time.Time) int {
	return c.activeBlockCountForPlatform(now, "")
}

func (c *openAIProxyStreamCircuit) activeBlockCountForPlatform(now time.Time, platform string) int {
	if c == nil || c.settings.disabled {
		return 0
	}
	platform = normalizeOpenAIStreamCircuitPlatform(platform)
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	seen := make(map[int64]struct{})
	for key, entry := range c.entries {
		if platform != "" && key.platform != "" && key.platform != platform {
			continue
		}
		if entry.blockedUntil.IsZero() || !now.Before(entry.blockedUntil) {
			continue
		}
		if _, exists := seen[key.id]; exists {
			continue
		}
		seen[key.id] = struct{}{}
		count++
	}
	return count
}

func (c *openAIProxyStreamCircuit) ensureCapacityLocked(now time.Time) {
	if len(c.entries) < c.settings.maxEntries {
		return
	}
	for key, entry := range c.entries {
		staleObservation := entry.blockedUntil.IsZero() && now.Sub(entry.lastTouched) > c.settings.failureWindow
		expiredQuarantine := !entry.blockedUntil.IsZero() && !now.Before(entry.blockedUntil)
		if staleObservation || expiredQuarantine {
			delete(c.entries, key)
		}
	}
	if len(c.entries) < c.settings.maxEntries {
		return
	}
	var oldestKey openAIProxyStreamCircuitKey
	var oldest time.Time
	for key, entry := range c.entries {
		if oldestKey.id == 0 || entry.lastTouched.Before(oldest) {
			oldestKey = key
			oldest = entry.lastTouched
		}
	}
	if oldestKey.id > 0 {
		delete(c.entries, oldestKey)
	}
}

func openAIProxyStreamCircuitProxyID(account *Account) (int64, bool) {
	if account == nil || !account.IsOpenAICompatible() || account.ProxyID == nil || *account.ProxyID <= 0 {
		return 0, false
	}
	return *account.ProxyID, true
}

// openAIAccountStreamCircuitID identifies a direct API-key endpoint. OAuth
// and native accounts are deliberately excluded: their transport/session
// health is managed by their existing runtime breakers, while this circuit is
// specifically for API-key providers (OpenAI, DeepSeek, Kimi, etc.) that can
// fail mid-stream behind a CDN without a ProxyID.
func openAIAccountStreamCircuitID(account *Account) (int64, bool) {
	if account == nil || account.ID <= 0 || !account.IsOpenAICompatible() ||
		account.Type != AccountTypeAPIKey || (account.ProxyID != nil && *account.ProxyID > 0) {
		return 0, false
	}
	return account.ID, true
}

func isOpenAIStreamCircuitPlatform(platform string) bool {
	switch NormalizeOpenAICompatiblePlatform(platform) {
	case PlatformOpenAI, PlatformGrok, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo:
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) openAIStreamCircuitForAccount(account *Account) (*openAIProxyStreamCircuit, int64, string, bool) {
	if proxyID, ok := openAIProxyStreamCircuitProxyID(account); ok {
		return s.getOpenAIProxyStreamCircuit(), proxyID, "proxy", true
	}
	if accountID, ok := openAIAccountStreamCircuitID(account); ok {
		return s.getOpenAIAccountStreamCircuit(), accountID, "account", true
	}
	return nil, 0, "", false
}

func (s *OpenAIGatewayService) recordOpenAIProxyStreamDisconnect(account *Account, streamErr error, upstreamRequestID string) {
	circuit, key, keyType, ok := s.openAIStreamCircuitForAccount(account)
	if !ok || circuit == nil || streamErr == nil || errors.Is(streamErr, context.Canceled) || errors.Is(streamErr, context.DeadlineExceeded) {
		return
	}
	tripped, until := circuit.recordFailureForPlatform(key, account.Platform, time.Now())
	if !tripped {
		return
	}
	fields := []zap.Field{
		zap.String("circuit_key_type", keyType),
		zap.Int64("account_id", account.ID),
		zap.Time("until", until),
		zap.String("upstream_request_id", upstreamRequestID),
		zap.String("error", sanitizeUpstreamErrorMessage(streamErr.Error())),
	}
	if keyType == "proxy" {
		fields = append(fields, zap.Int64("proxy_id", key))
	}
	// Keep the historical log event name so existing dashboards/alerts continue
	// to match; circuit_key_type distinguishes the new direct-account case.
	logger.L().With(zap.String("component", "service.openai_gateway")).Warn("openai.proxy_quarantined_stream_disconnect", fields...)
}

func (s *OpenAIGatewayService) clearOpenAIProxyStreamDisconnect(account *Account) {
	circuit, key, _, ok := s.openAIStreamCircuitForAccount(account)
	if !ok || circuit == nil {
		return
	}
	circuit.recordSuccessForPlatform(key, account.Platform)
}

// openAIProxyStreamQuarantineBypassKey marks a selection pass that must ignore
// proxy quarantine. It is set for the second, fail-open selection attempt when
// the first pass found no available account while the circuit was withholding
// proxies: a degraded proxy is strictly better than answering 502 (#5056).
type openAIProxyStreamQuarantineBypassKey struct{}

func withOpenAIProxyStreamQuarantineBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, openAIProxyStreamQuarantineBypassKey{}, true)
}

func openAIProxyStreamQuarantineBypassed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	bypassed, _ := ctx.Value(openAIProxyStreamQuarantineBypassKey{}).(bool)
	return bypassed
}

func (s *OpenAIGatewayService) isOpenAIProxyStreamQuarantined(ctx context.Context, account *Account) bool {
	circuit, key, _, ok := s.openAIStreamCircuitForAccount(account)
	if !ok || circuit == nil {
		return false
	}
	if openAIProxyStreamQuarantineBypassed(ctx) {
		return false
	}
	return circuit.isBlockedForPlatform(key, account.Platform, time.Now())
}

func (s *OpenAIGatewayService) activeOpenAIStreamCircuitBlockCount(now time.Time, platform string) int {
	if s == nil {
		return 0
	}
	count := 0
	if circuit := s.getOpenAIProxyStreamCircuit(); circuit != nil {
		count += circuit.activeBlockCountForPlatform(now, platform)
	}
	if circuit := s.getOpenAIAccountStreamCircuit(); circuit != nil {
		count += circuit.activeBlockCountForPlatform(now, platform)
	}
	return count
}

// logOpenAIProxyStreamQuarantineFailOpen emits a rate-limited warning when a
// selection pass had to re-admit quarantined proxies to serve at all.
func (s *OpenAIGatewayService) logOpenAIProxyStreamQuarantineFailOpen(requestedModel string, blockedProxies int) {
	now := time.Now().UnixNano()
	last := s.openaiProxyStreamFailOpenLogAt.Load()
	if now-last < int64(openAIProxyStreamFailOpenLogInterval) ||
		!s.openaiProxyStreamFailOpenLogAt.CompareAndSwap(last, now) {
		return
	}
	logger.L().With(zap.String("component", "service.openai_gateway")).Warn(
		"openai.proxy_stream_quarantine_fail_open",
		zap.Int("blocked_proxies", blockedProxies),
		zap.String("model", requestedModel),
	)
}
