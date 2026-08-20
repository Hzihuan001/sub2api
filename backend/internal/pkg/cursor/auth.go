package cursor

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Upstream endpoints and content types.
const (
	// DefaultBaseURL is Cursor's production API host.
	DefaultBaseURL = "https://api2.cursor.sh"

	// EndpointAvailableModels is the unary RPC that lists models. It uses
	// ContentTypeProto (no Connect envelope; body is raw protobuf).
	EndpointAvailableModels = "/aiserver.v1.AiService/AvailableModels"

	// EndpointStreamChat is the server-streaming chat RPC. It uses
	// ContentTypeConnectProto and the 5-byte envelope framing (see envelope.go).
	EndpointStreamChat = "/aiserver.v1.ChatService/StreamUnifiedChatWithTools"

	// ContentTypeProto is the unary content type (raw protobuf body).
	ContentTypeProto = "application/proto"

	// ContentTypeConnectProto is the streaming content type (enveloped frames).
	ContentTypeConnectProto = "application/connect+proto"
)

// Defaults for the client identity block. Cursor's backend treats these headers
// as part of authentication, not as telemetry: AvailableModels answers a bare
// Bearer token, but ChatService refuses one that does not also look like a real
// desktop client and reports the refusal as ERROR_NOT_LOGGED_IN.
//
// The values match every reverse-engineered client that can actually hold a
// conversation on /aiserver.v1.ChatService/StreamUnifiedChatWithTools:
// wisdgod/cursor-api (Rust), timxx/Cursor-To-OpenAI (Node),
// kaitranntt/ccs (TypeScript) and eisbaw/cursor_api_demo (Python) all send the
// same block and pin the same user agent.
const (
	// DefaultClientVersion is the advertised Cursor build. 2.6.22 is the newest
	// version a confirmed-working StreamUnifiedChatWithTools implementation
	// pins, and it is the build our protobuf field numbers were extracted from.
	//
	// The current shipping client is 3.x, but Cursor 3 moved chat off this
	// endpoint onto agentn.api5.cursor.sh with a different request message, so
	// claiming a 3.x version while speaking the 2.6.x chat protocol is more
	// likely to be rejected than accepted. Override it per request when a live
	// account proves otherwise.
	DefaultClientVersion = "2.6.22"

	// DefaultClientType is the x-cursor-client-type value for the desktop IDE.
	// The CLI uses "cli" and pairs it with a "cli-<date>-<sha>" version string.
	DefaultClientType = "ide"

	// DefaultDeviceType is the x-cursor-client-device-type value.
	DefaultDeviceType = "desktop"

	// DefaultUserAgent is the Connect client library the Cursor desktop app
	// ships. Go's default "Go-http-client/2.0" is the single most obvious way a
	// request announces that it is not the real client.
	DefaultUserAgent = "connect-es/1.6.1"

	// DefaultTimezone is the fallback IANA zone when the host has no named one.
	DefaultTimezone = "UTC"
)

// ClientProfile is the device identity a request advertises. Its zero value is
// not a usable profile: string fields fall back to the package defaults, but
// the booleans do not, so build one with DefaultProfile and adjust from there.
type ClientProfile struct {
	// Version is x-cursor-client-version (default DefaultClientVersion).
	Version string
	// Type is x-cursor-client-type (default DefaultClientType).
	Type string
	// OS is x-cursor-client-os, in Node's process.platform spelling
	// ("win32"/"darwin"/"linux"), defaulting to the host's.
	OS string
	// Arch is x-cursor-client-arch, in Node's process.arch spelling
	// ("x64"/"arm64"), defaulting to the host's.
	Arch string
	// OSVersion is x-cursor-client-os-version. The current desktop client omits
	// it, so an empty value means "do not send the header" rather than
	// "substitute a default".
	OSVersion string
	// DeviceType is x-cursor-client-device-type (default DefaultDeviceType).
	DeviceType string
	// Timezone is x-cursor-timezone, an IANA zone name.
	Timezone string
	// UserAgent is the user-agent header (default DefaultUserAgent).
	UserAgent string
	// GhostMode is x-ghost-mode: privacy mode, which keeps the conversation out
	// of Cursor's indexing. Every working implementation sends true.
	GhostMode bool
	// OnboardingCompleted is x-new-onboarding-completed.
	OnboardingCompleted bool

	// MachineID overrides the device id embedded in x-cursor-checksum. Empty
	// derives it from the token, which makes it change with every credential; a
	// real client reports one stable per-install id (storage.serviceMachineId
	// in Cursor's state.vscdb). Set it to pin a stable device identity.
	MachineID string
	// MacMachineID overrides the second checksum id the same way.
	MacMachineID string
}

// DefaultProfile is the identity every request advertises unless the caller
// overrides it.
func DefaultProfile() ClientProfile {
	return ClientProfile{
		Version:             DefaultClientVersion,
		Type:                DefaultClientType,
		OS:                  hostOS(),
		Arch:                hostArch(),
		DeviceType:          DefaultDeviceType,
		Timezone:            hostTimezone(),
		UserAgent:           DefaultUserAgent,
		GhostMode:           true,
		OnboardingCompleted: false,
	}
}

// Resolved fills every blank string field from DefaultProfile, so callers can
// report exactly what will go on the wire. OSVersion is left alone: it has no
// default, and omitting the header is what the current client does.
func (p ClientProfile) Resolved() ClientProfile {
	d := DefaultProfile()
	p.Version = firstNonBlank(p.Version, d.Version)
	p.Type = firstNonBlank(p.Type, d.Type)
	p.OS = firstNonBlank(p.OS, d.OS)
	p.Arch = firstNonBlank(p.Arch, d.Arch)
	p.DeviceType = firstNonBlank(p.DeviceType, d.DeviceType)
	p.Timezone = firstNonBlank(p.Timezone, d.Timezone)
	p.UserAgent = firstNonBlank(p.UserAgent, d.UserAgent)
	p.OSVersion = headerSafe(strings.TrimSpace(p.OSVersion))
	p.MachineID = headerSafe(strings.TrimSpace(p.MachineID))
	p.MacMachineID = headerSafe(strings.TrimSpace(p.MacMachineID))
	return p
}

func firstNonBlank(value, fallback string) string {
	if trimmed := headerSafe(strings.TrimSpace(value)); trimmed != "" {
		return trimmed
	}
	return fallback
}

// headerSafe drops bytes an HTTP header value cannot carry, so an operator
// pasting a machine id straight out of a database cannot produce a request the
// transport refuses to write.
func headerSafe(value string) string {
	var b strings.Builder
	for i := 0; i < len(value); i++ {
		if c := value[i]; c >= 0x20 && c < 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// hostOS reports the host platform in the spelling Cursor expects, which is
// Node's process.platform rather than Go's GOOS.
func hostOS() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

// hostArch reports the host architecture in Node's process.arch spelling.
func hostArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH
	}
}

// hostTimezone reports the host's IANA zone name. Go names an unresolvable zone
// "Local", which is not a zone Cursor would ever see from a real client.
func hostTimezone() string {
	name := time.Local.String()
	if name == "" || name == "Local" {
		return DefaultTimezone
	}
	return name
}

// ParseToken normalizes a stored credential into a clean JWT and a user id.
//
// It accepts three shapes:
//   - "userId::JWT" (Cursor's WorkosCursorSessionToken cookie form): the id is
//     taken verbatim from the left of "::" and the JWT from the right.
//   - "userId%3A%3AJWT", the same value as the browser stores it.
//   - a bare JWT: the id is derived from the JWT's own "sub" claim.
//
// When the "userId::JWT" left side is empty it falls back to the JWT sub.
func ParseToken(raw string) (token string, uid string) {
	raw = decodeCookieSeparator(strings.TrimSpace(raw))
	if raw == "" {
		return "", ""
	}
	if idx := strings.Index(raw, "::"); idx >= 0 {
		uid = strings.TrimSpace(raw[:idx])
		token = strings.TrimSpace(raw[idx+2:])
		if uid == "" {
			uid = ExtractUserID(token)
		}
		return token, uid
	}
	return raw, ExtractUserID(raw)
}

// ExtractUserID decodes a JWT payload (without verifying the signature) and
// returns the user id: the segment after the last '|' in the "sub" claim
// (rsplit('|')[-1]). Cursor subs look like "auth0|user_01ABC"; the id is the
// trailing "user_01ABC". Returns "" if the token is not a parseable JWT.
func ExtractUserID(jwt string) string {
	jwt = strings.TrimSpace(jwt)
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	sub := strings.TrimSpace(claims.Sub)
	if sub == "" {
		return ""
	}
	if idx := strings.LastIndex(sub, "|"); idx >= 0 {
		return sub[idx+1:]
	}
	return sub
}

// decodeJWTSegment base64url-decodes a JWT segment, tolerating both padded and
// unpadded encodings.
func decodeJWTSegment(seg string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(seg); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(seg)
}

// sha256hex returns the lowercase hex SHA-256 of s.
func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// machineID derives the device machine id from the token, matching the
// wisdgod/cursor-api convention: sha256hex(token + "machineId").
func machineID(token string) string { return sha256hex(token + "machineId") }

// macMachineID derives the secondary machine id: sha256hex(token + "macMachineId").
func macMachineID(token string) string { return sha256hex(token + "macMachineId") }

// machineIDFor returns the device id this profile puts in the checksum: the
// override verbatim when set, otherwise the token-derived digest. The override
// is not reshaped, so an operator can reproduce a real install's identity by
// pasting whatever its storage.serviceMachineId holds.
func (p ClientProfile) machineIDFor(token string) string {
	if id := headerSafe(strings.TrimSpace(p.MachineID)); id != "" {
		return id
	}
	return machineID(token)
}

// macMachineIDFor is machineIDFor for the second half of the checksum.
func (p ClientProfile) macMachineIDFor(token string) string {
	if id := headerSafe(strings.TrimSpace(p.MacMachineID)); id != "" {
		return id
	}
	return macMachineID(token)
}

// Checksum builds this profile's x-cursor-checksum value for the current time.
func (p ClientProfile) Checksum(token string) string { return p.ChecksumAt(token, time.Now()) }

// ChecksumAt builds this profile's x-cursor-checksum value at a fixed instant.
// The layout is the one ChecksumAt documents; only the machine ids differ, and
// only when the profile pins them.
func (p ClientProfile) ChecksumAt(token string, t time.Time) string {
	jwt, _ := ParseToken(token)
	return obfuscatedTimestamp(t) + p.machineIDFor(jwt) + "/" + p.macMachineIDFor(jwt)
}

// ClientKey is the x-client-key header value: sha256hex(token) (no salt).
func ClientKey(token string) string { return sha256hex(token) }

// SessionID is the x-session-id header value: a deterministic UUIDv5 in the DNS
// namespace derived from the token, so a given token always maps to the same
// session id.
func SessionID(token string) string {
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(token)).String()
}

// Checksum builds the x-cursor-checksum value for the current time. See
// ChecksumAt for the algorithm and format.
func Checksum(token string) string { return ChecksumAt(token, time.Now()) }

// ChecksumAt builds the x-cursor-checksum value for a specific instant. Taking
// the time as a parameter makes the output deterministic for testing (the
// time-derived prefix aside, the machineId/macMachineId suffix is fully
// determined by the token).
//
// Format: <encoded><machineId>/<macMachineId> where
//   - encoded is 8 chars of URL-safe base64 (no padding) over an 6-byte,
//     Jyh-obfuscated timestamp,
//   - machineId and macMachineId are 64-char hex SHA-256 digests.
func ChecksumAt(token string, t time.Time) string {
	return obfuscatedTimestamp(t) + machineID(token) + "/" + macMachineID(token)
}

// obfuscatedTimestamp reproduces Cursor's timestamp cipher: take unixMillis /
// 1_000_000, keep the low 6 bytes big-endian, then run the "Jyh" rolling XOR
// (seed t=165; b[i] = ((b[i]^t) + (i%256)) & 0xFF; t = b[i]), then URL-safe
// base64 without padding.
func obfuscatedTimestamp(at time.Time) string {
	ts := at.UnixMilli() / 1_000_000
	b := []byte{
		byte(ts >> 40),
		byte(ts >> 32),
		byte(ts >> 24),
		byte(ts >> 16),
		byte(ts >> 8),
		byte(ts),
	}
	t := byte(165)
	for i := range b {
		b[i] = byte((int(b[i]^t) + (i % 256)) & 0xFF)
		t = b[i]
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// EncodeChecksumStd is the standard-base64 (no padding) variant of the
// timestamp encoding, provided in case an upstream build expects '+'/'/'
// instead of '-'/'_'. BuildHeaders defaults to the URL-safe form.
func EncodeChecksumStd(token string, t time.Time) string {
	ts := t.UnixMilli() / 1_000_000
	b := []byte{
		byte(ts >> 40), byte(ts >> 32), byte(ts >> 24),
		byte(ts >> 16), byte(ts >> 8), byte(ts),
	}
	k := byte(165)
	for i := range b {
		b[i] = byte((int(b[i]^k) + (i % 256)) & 0xFF)
		k = b[i]
	}
	return base64.RawStdEncoding.EncodeToString(b) + machineID(token) + "/" + macMachineID(token)
}

// BuildHeaders assembles the request headers for a Cursor upstream call using
// DefaultProfile. See BuildHeadersWithProfile.
func BuildHeaders(token, contentType string) http.Header {
	return BuildHeadersWithProfile(token, contentType, DefaultProfile())
}

// BuildHeadersWithProfile assembles the request headers for a Cursor upstream
// call. token may be a bare JWT or the "userId::JWT" form; the clean JWT is
// used for the Bearer token, checksum, client key and session id. contentType
// selects unary (ContentTypeProto) vs streaming (ContentTypeConnectProto), and
// the streaming-only headers follow from it.
//
// x-request-id, x-amzn-trace-id and x-cursor-config-version are per-request
// random UUIDs (trace id embeds the request id as its Root). x-client-key and
// x-session-id are deterministic per token, as is the checksum's machine-id
// suffix unless the profile pins one.
func BuildHeadersWithProfile(token, contentType string, profile ClientProfile) http.Header {
	jwt, _ := ParseToken(token)
	p := profile.Resolved()
	requestID := uuid.NewString()
	streaming := contentType == ContentTypeConnectProto

	h := make(http.Header)
	h.Set("authorization", "Bearer "+jwt)
	h.Set("content-type", contentType)
	h.Set("connect-protocol-version", "1")
	// We decode gzip frames (see FrameReader), so advertise that the server may
	// compress streaming responses.
	h.Set("connect-accept-encoding", "gzip")
	if streaming {
		// Declares the algorithm a request frame would use if its envelope flag
		// set the compressed bit. Our frames are uncompressed, which is legal
		// and is what the reference clients send.
		h.Set("connect-content-encoding", "gzip")
		h.Set("x-cursor-streaming", "true")
	}
	h.Set("user-agent", p.UserAgent)
	h.Set("x-cursor-checksum", p.Checksum(jwt))
	h.Set("x-client-key", ClientKey(jwt))
	h.Set("x-session-id", SessionID(jwt))
	h.Set("x-request-id", requestID)
	h.Set("x-amzn-trace-id", "Root="+requestID)
	h.Set("x-cursor-config-version", uuid.NewString())
	h.Set("x-cursor-client-version", p.Version)
	h.Set("x-cursor-client-type", p.Type)
	h.Set("x-cursor-client-os", p.OS)
	h.Set("x-cursor-client-arch", p.Arch)
	h.Set("x-cursor-client-device-type", p.DeviceType)
	if p.OSVersion != "" {
		h.Set("x-cursor-client-os-version", p.OSVersion)
	}
	h.Set("x-cursor-timezone", p.Timezone)
	h.Set("x-ghost-mode", boolHeader(p.GhostMode))
	h.Set("x-new-onboarding-completed", boolHeader(p.OnboardingCompleted))
	return h
}

func boolHeader(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
