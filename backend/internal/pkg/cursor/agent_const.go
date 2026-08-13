package cursor

// Endpoints, headers and version pinning for Cursor's *current* conversation
// protocol, agent.v1.AgentService on the api5 hosts.
//
// This is a second, independent upstream from the api2 ChatService the rest of
// this package speaks. api2's StreamUnifiedChatWithTools has been retired by
// Cursor (it answers "Update Required" for every client version), so new
// conversations go through AgentService/Run instead.
//
// Two things differ sharply from api2 and are the usual cause of a rejected
// request:
//
//   - The identity block is *not* sent. No x-cursor-checksum, no
//     x-cursor-client-os / -arch / -device-type, no x-client-key, no
//     x-session-id. BuildAgentHeaders emits exactly ten headers; adding the
//     api2 block (BuildHeadersWithProfile) is a change in observable behaviour,
//     not a harmless superset.
//   - The transport is a *bidirectional* HTTP/2 stream. See agent_stream.go.
//
// The Connect envelope framing itself is unchanged, so envelope.go and proto.go
// are reused verbatim.
//
// Provenance: field numbers and header set were cross-checked against the
// decompiled agent.v1 schema published by 0xlane/reverse-cursor-agent
// (docs/proto/agent_v1.proto) and the working Rust implementation in
// pleaseai/shunt (src/adapters/cursor/agent.rs), which captured the wire from
// the real cursor-agent CLI via 1jehuang/jcode. Field numbers and framing are
// protocol facts; every line here is original Go.

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	// DefaultAgentBaseURL is the host the current cursor-agent CLI talks to.
	DefaultAgentBaseURL = "https://agentn.global.api5.cursor.sh"

	// AgentBaseURLDirect and AgentBaseURLRegionUS are the other api5 agent
	// hosts seen in the wild. They are kept as named alternates so a probe can
	// bisect a regional failure without hard-coding a string at the call site.
	AgentBaseURLDirect   = "https://agent.api5.cursor.sh"
	AgentBaseURLRegionUS = "https://agentn.us.api5.cursor.sh"

	// EndpointAgentRun is the bidirectional streaming RPC that holds a
	// conversation. It uses ContentTypeConnectProto and envelope framing.
	EndpointAgentRun = "/agent.v1.AgentService/Run"

	// EndpointGetUsableModels lists the models the credential may request. It
	// is a unary RPC on the same host.
	EndpointGetUsableModels = "/agent.v1.AgentService/GetUsableModels"

	// AgentClientType is the x-cursor-client-type value. AgentService is the
	// CLI's endpoint; "ide" is not a value it expects here.
	AgentClientType = "cli"

	// AgentAcceptEncoding is the per-frame compression the client accepts
	// (connect-accept-encoding, not the HTTP accept-encoding).
	AgentAcceptEncoding = "gzip,br"

	// ConnectProtocolVersion is the connect-protocol-version header value.
	ConnectProtocolVersion = "1"
)

// DefaultCLIClientVersion is the cursor-agent CLI build advertised in
// x-cursor-client-version. It is a variable, not a constant, because Cursor
// raises the accepted-version floor over time and rejects stale clients: the
// failure signal is a Connect "permission_denied" on an otherwise valid
// credential (an api2-era version string such as "2.6.22" is refused outright).
// Override it per request via AgentRunParams / BuildAgentHeaders, or globally at
// startup, when a live account proves the floor has moved.
var DefaultCLIClientVersion = "cli-2026.08.11-e8db854"

// cliVersionPattern matches the CLI's version string, "cli-<YYYY.MM.DD>-<sha>".
var cliVersionPattern = regexp.MustCompile(`cli-20\d{2}\.\d{2}\.\d{2}-[0-9a-f]{7,40}`)

// ParseCLIVersionFromInstallScript extracts the advertised CLI build from the
// text of Cursor's install script (https://cursor.com/install). It is a pure
// parser: fetching the script is the caller's decision, so nothing here reaches
// the network — neither at build time nor on the request path — and the pinned
// DefaultCLIClientVersion stays the default.
//
// It returns "" when the script carries no recognizable version, which callers
// must treat as "keep the current pin" rather than as an empty version.
func ParseCLIVersionFromInstallScript(script string) string {
	return cliVersionPattern.FindString(script)
}

// AgentRunURL joins an agent base URL with the Run endpoint path.
func AgentRunURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = DefaultAgentBaseURL
	}
	return base + EndpointAgentRun
}

// BuildAgentHeaders returns the exact header set the cursor-agent CLI sends on
// AgentService/Run — ten headers, no more.
//
// The api2 identity block is deliberately absent: AgentService authenticates the
// bearer token alone, and the checksum/machine-id headers that api2 requires
// have no counterpart here. Sending them is not a no-op, so BuildHeadersWithProfile
// must not be substituted.
//
// token may be a bare JWT or the "userId::JWT" cookie form; only the JWT half
// reaches the wire. A blank clientVersion falls back to DefaultCLIClientVersion
// and a blank requestID gets a fresh UUID, so the caller can pass zero values
// and still produce a well-formed request.
func BuildAgentHeaders(token, clientVersion string, ghost bool, requestID string) http.Header {
	jwt, _ := ParseToken(token)
	if jwt == "" {
		jwt = strings.TrimSpace(token)
	}
	if clientVersion = strings.TrimSpace(clientVersion); clientVersion == "" {
		clientVersion = DefaultCLIClientVersion
	}
	if requestID = strings.TrimSpace(requestID); requestID == "" {
		requestID = uuid.NewString()
	}

	h := make(http.Header, 10)
	h.Set("authorization", "Bearer "+jwt)
	h.Set("content-type", ContentTypeConnectProto)
	h.Set("connect-protocol-version", ConnectProtocolVersion)
	h.Set("connect-accept-encoding", AgentAcceptEncoding)
	h.Set("x-cursor-client-version", clientVersion)
	h.Set("x-cursor-client-type", AgentClientType)
	h.Set("x-ghost-mode", strconv.FormatBool(ghost))
	// The CLI sends the same uuid twice: x-request-id identifies this attempt
	// and x-original-request-id the logical request it retries.
	h.Set("x-request-id", requestID)
	h.Set("x-original-request-id", requestID)
	h.Set("user-agent", DefaultUserAgent)
	return h
}
