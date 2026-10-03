package routes

import "github.com/Wei-Shaw/sub2api/internal/custom"

// resolveCustomHost selects the explicitly supplied host when route tests or
// a caller provide one, and otherwise falls back to the process-wide host.
// Keeping the fallback here lets existing route registration call sites keep
// their upstream-compatible signatures while custom builds opt in centrally.
func resolveCustomHost(hosts []*custom.Host) *custom.Host {
	if len(hosts) > 0 && hosts[0] != nil {
		return hosts[0]
	}
	return custom.DefaultHost()
}
