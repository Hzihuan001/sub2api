package cursor

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// makeJWT builds an unsigned JWT with the given sub claim. Only the payload
// segment matters to the parser (the signature is never verified here).
func makeJWT(t *testing.T, sub string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payloadJSON, err := json.Marshal(map[string]string{"sub": sub})
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(payloadJSON)
	return header + "." + payload + ".sig"
}

func TestExtractUserID(t *testing.T) {
	t.Parallel()
	require.Equal(t, "user_123", ExtractUserID(makeJWT(t, "auth0|user_123")))
	require.Equal(t, "user_456", ExtractUserID(makeJWT(t, "user_456")))
	require.Equal(t, "", ExtractUserID("not-a-jwt"))
	require.Equal(t, "", ExtractUserID(""))
}

func TestParseToken(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(t, "auth0|user_999")

	tok, uid := ParseToken("user_999::" + jwt)
	require.Equal(t, jwt, tok)
	require.Equal(t, "user_999", uid)

	tok, uid = ParseToken(jwt)
	require.Equal(t, jwt, tok)
	require.Equal(t, "user_999", uid)

	// Empty left side falls back to the JWT sub.
	tok, uid = ParseToken("::" + jwt)
	require.Equal(t, jwt, tok)
	require.Equal(t, "user_999", uid)
}

func TestClientKeyDeterministic(t *testing.T) {
	t.Parallel()
	const token = "some-jwt-token"
	k1 := ClientKey(token)
	k2 := ClientKey(token)
	require.Equal(t, k1, k2)
	require.Len(t, k1, 64)
	_, err := hex.DecodeString(k1)
	require.NoError(t, err)
	require.NotEqual(t, k1, ClientKey("different"))
}

func TestSessionIDDeterministic(t *testing.T) {
	t.Parallel()
	const token = "some-jwt-token"
	s1 := SessionID(token)
	s2 := SessionID(token)
	require.Equal(t, s1, s2)
	parsed, err := uuid.Parse(s1)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(5), parsed.Version())
	require.NotEqual(t, s1, SessionID("different"))
}

func TestChecksumFormatAndDeterminism(t *testing.T) {
	t.Parallel()
	const token = "some-jwt-token"
	at := time.Unix(1_700_000_000, 0)

	c1 := ChecksumAt(token, at)
	c2 := ChecksumAt(token, at)
	require.Equal(t, c1, c2, "checksum must be deterministic at a fixed time")

	// Format: <encoded:8><machineId:64>/<macMachineId:64> = 137 chars.
	require.Len(t, c1, 137)
	require.Equal(t, 1, strings.Count(c1, "/"))

	left, right, ok := strings.Cut(c1, "/")
	require.True(t, ok)
	require.Len(t, left, 72)
	require.Len(t, right, 64)

	// The machine-id parts are hex SHA-256 digests and depend only on the token.
	machinePart := left[8:]
	_, err := hex.DecodeString(machinePart)
	require.NoError(t, err)
	_, err = hex.DecodeString(right)
	require.NoError(t, err)
	require.Equal(t, machineID(token), machinePart)
	require.Equal(t, macMachineID(token), right)
}

func TestBuildHeaders(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(t, "auth0|user_1")
	h := BuildHeaders("user_1::"+jwt, ContentTypeConnectProto)

	require.Equal(t, "Bearer "+jwt, h.Get("authorization"))
	require.Equal(t, ContentTypeConnectProto, h.Get("content-type"))
	require.Equal(t, "1", h.Get("connect-protocol-version"))
	require.Equal(t, DefaultClientVersion, h.Get("x-cursor-client-version"))
	require.Equal(t, ClientKey(jwt), h.Get("x-client-key"))
	require.Equal(t, SessionID(jwt), h.Get("x-session-id"))

	requestID := h.Get("x-request-id")
	require.NotEmpty(t, requestID)
	_, err := uuid.Parse(requestID)
	require.NoError(t, err)
	require.Equal(t, "Root="+requestID, h.Get("x-amzn-trace-id"))

	// checksum uses the clean JWT and carries the "/" machine-id separator.
	require.Contains(t, h.Get("x-cursor-checksum"), "/")

	_, err = uuid.Parse(h.Get("x-cursor-config-version"))
	require.NoError(t, err)
}

// TestBuildHeadersCarriesClientIdentity pins the block ChatService
// authenticates. Dropping any of these reproduces ERROR_NOT_LOGGED_IN on a
// credential AvailableModels happily accepts.
func TestBuildHeadersCarriesClientIdentity(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(t, "auth0|user_1")
	h := BuildHeaders(jwt, ContentTypeConnectProto)
	def := DefaultProfile()

	require.Equal(t, DefaultUserAgent, h.Get("user-agent"))
	require.Equal(t, DefaultClientType, h.Get("x-cursor-client-type"))
	require.Equal(t, DefaultDeviceType, h.Get("x-cursor-client-device-type"))
	require.Equal(t, def.OS, h.Get("x-cursor-client-os"))
	require.Equal(t, def.Arch, h.Get("x-cursor-client-arch"))
	require.Equal(t, def.Timezone, h.Get("x-cursor-timezone"))
	require.Equal(t, "true", h.Get("x-ghost-mode"))
	require.Equal(t, "false", h.Get("x-new-onboarding-completed"))

	// The current desktop client omits the os version rather than guessing one.
	require.Empty(t, h.Get("x-cursor-client-os-version"))
}

// TestBuildHeadersStreamingOnlyHeaders keeps the unary AvailableModels request
// exactly as it was, since that call already works against live accounts.
func TestBuildHeadersStreamingOnlyHeaders(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(t, "auth0|user_1")

	stream := BuildHeaders(jwt, ContentTypeConnectProto)
	require.Equal(t, "gzip", stream.Get("connect-content-encoding"))
	require.Equal(t, "true", stream.Get("x-cursor-streaming"))

	unary := BuildHeaders(jwt, ContentTypeProto)
	require.Empty(t, unary.Get("connect-content-encoding"))
	require.Empty(t, unary.Get("x-cursor-streaming"))
	require.Equal(t, "gzip", unary.Get("connect-accept-encoding"))
}

func TestBuildHeadersWithProfileOverrides(t *testing.T) {
	t.Parallel()
	jwt := makeJWT(t, "auth0|user_1")

	p := DefaultProfile()
	p.Version = "3.15.19"
	p.Type = "cli"
	p.OS = "darwin"
	p.Arch = "arm64"
	p.OSVersion = "24.6.0"
	p.DeviceType = "laptop"
	p.Timezone = "Asia/Shanghai"
	p.UserAgent = "connect-es/2.0.0"
	p.GhostMode = false
	p.OnboardingCompleted = true

	h := BuildHeadersWithProfile(jwt, ContentTypeConnectProto, p)
	require.Equal(t, "3.15.19", h.Get("x-cursor-client-version"))
	require.Equal(t, "cli", h.Get("x-cursor-client-type"))
	require.Equal(t, "darwin", h.Get("x-cursor-client-os"))
	require.Equal(t, "arm64", h.Get("x-cursor-client-arch"))
	require.Equal(t, "24.6.0", h.Get("x-cursor-client-os-version"))
	require.Equal(t, "laptop", h.Get("x-cursor-client-device-type"))
	require.Equal(t, "Asia/Shanghai", h.Get("x-cursor-timezone"))
	require.Equal(t, "connect-es/2.0.0", h.Get("user-agent"))
	require.Equal(t, "false", h.Get("x-ghost-mode"))
	require.Equal(t, "true", h.Get("x-new-onboarding-completed"))
}

// TestProfileResolvedFillsBlanks documents that a blank string means "use the
// default", so a caller can override one field without restating the rest.
func TestProfileResolvedFillsBlanks(t *testing.T) {
	t.Parallel()
	def := DefaultProfile()
	got := ClientProfile{Version: " 3.15.19 "}.Resolved()

	require.Equal(t, "3.15.19", got.Version, "a set field is trimmed and kept")
	require.Equal(t, def.Type, got.Type)
	require.Equal(t, def.OS, got.OS)
	require.Equal(t, def.Arch, got.Arch)
	require.Equal(t, def.DeviceType, got.DeviceType)
	require.Equal(t, def.Timezone, got.Timezone)
	require.Equal(t, def.UserAgent, got.UserAgent)
	require.Empty(t, got.OSVersion, "os version has no default")

	// Header-hostile bytes never reach the wire.
	require.Equal(t, "3.15.19", ClientProfile{Version: "3.15\r\n.19"}.Resolved().Version)
}

// TestProfileChecksumPinsMachineID covers the reason the override exists: a
// token-derived device id changes with every credential, which no real install
// does.
func TestProfileChecksumPinsMachineID(t *testing.T) {
	t.Parallel()
	at := time.Unix(1_700_000_000, 0)
	const (
		machine = "1111111111111111111111111111111111111111111111111111111111111111"
		mac     = "2222222222222222222222222222222222222222222222222222222222222222"
	)
	tokenA := makeJWT(t, "auth0|user_a")
	tokenB := makeJWT(t, "auth0|user_b")

	p := DefaultProfile()
	p.MachineID = machine
	p.MacMachineID = mac

	got := p.ChecksumAt(tokenA, at)
	require.Equal(t, obfuscatedTimestamp(at)+machine+"/"+mac, got)
	require.Equal(t, got, p.ChecksumAt(tokenB, at), "a pinned device id is the same for every token")

	// Without an override the digest still tracks the token, as before.
	derived := DefaultProfile().ChecksumAt(tokenA, at)
	require.Equal(t, ChecksumAt(tokenA, at), derived)
	require.NotEqual(t, derived, DefaultProfile().ChecksumAt(tokenB, at))

	// The cookie form resolves to the same checksum as the bare JWT.
	require.Equal(t, derived, DefaultProfile().ChecksumAt("user_a::"+tokenA, at))
}
