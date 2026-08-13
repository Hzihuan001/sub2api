package main

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/stretchr/testify/require"
)

// baseOptions is what parseOptions produces for a default -mode chat run: the
// identity flags default to the package profile, so the probe advertises the
// same headers the gateway does unless an operator overrides one.
func baseOptions() options {
	def := cursor.DefaultProfile()
	return options{
		mode:        "chat",
		base:        cursor.DefaultBaseURL,
		acceptEnc:   "identity",
		clientVer:   def.Version,
		clientType:  def.Type,
		clientOS:    def.OS,
		clientArch:  def.Arch,
		clientOSVer: def.OSVersion,
		deviceType:  def.DeviceType,
		timezone:    def.Timezone,
		userAgent:   def.UserAgent,
		ghostMode:   def.GhostMode,
		onboarding:  def.OnboardingCompleted,
	}
}

func TestProfileMatchesPackageDefaults(t *testing.T) {
	t.Parallel()
	require.Equal(t, cursor.DefaultProfile().Resolved(), baseOptions().profile())
}

func TestProfileAppliesOverrides(t *testing.T) {
	t.Parallel()
	opts := baseOptions()
	opts.clientVer = "3.15.19"
	opts.clientType = "cli"
	opts.timezone = "Asia/Shanghai"
	opts.ghostMode = false
	opts.onboarding = true
	opts.machineID = "  deadbeef  "
	opts.macMachineID = "cafebabe"

	got := opts.profile()
	require.Equal(t, "3.15.19", got.Version)
	require.Equal(t, "cli", got.Type)
	require.Equal(t, "Asia/Shanghai", got.Timezone)
	require.False(t, got.GhostMode)
	require.True(t, got.OnboardingCompleted)
	require.Equal(t, "deadbeef", got.MachineID)
	require.Equal(t, "cafebabe", got.MacMachineID)
}

// TestProfileBlankFlagKeepsDefault covers `-client-version ""`, which reads as
// "I do not care" rather than "send an empty header".
func TestProfileBlankFlagKeepsDefault(t *testing.T) {
	t.Parallel()
	opts := baseOptions()
	opts.clientVer = "   "
	opts.userAgent = ""
	require.Equal(t, cursor.DefaultClientVersion, opts.profile().Version)
	require.Equal(t, cursor.DefaultUserAgent, opts.profile().UserAgent)
}

func TestApplyHeaders(t *testing.T) {
	t.Parallel()
	opts := baseOptions()
	base := cursor.BuildHeadersWithProfile("token", cursor.ContentTypeConnectProto, opts.profile())

	req := httpRequest(t)
	applyHeaders(req, base, opts)

	require.Equal(t, cursor.DefaultUserAgent, req.Header.Get("user-agent"))
	require.Equal(t, cursor.DefaultClientType, req.Header.Get("x-cursor-client-type"))
	require.Equal(t, "true", req.Header.Get("x-cursor-streaming"))
	require.Equal(t, "identity", req.Header.Get("accept-encoding"))
}

// TestApplyHeadersOverrideAndDelete is the bisecting tool: -H sets a header, and
// an empty value removes it so a single suspect can be taken off the wire.
func TestApplyHeadersOverrideAndDelete(t *testing.T) {
	t.Parallel()
	opts := baseOptions()
	opts.acceptEnc = ""
	opts.headers = headerFlags{
		"x-cursor-client-type: cli",
		"x-cursor-streaming:",
		"user-agent:",
		"x-extra=added",
	}
	base := cursor.BuildHeadersWithProfile("token", cursor.ContentTypeConnectProto, opts.profile())

	req := httpRequest(t)
	applyHeaders(req, base, opts)

	require.Equal(t, "cli", req.Header.Get("x-cursor-client-type"))
	require.Equal(t, "added", req.Header.Get("x-extra"))
	require.Empty(t, req.Header.Get("x-cursor-streaming"), "an empty -H value deletes the header")
	require.Empty(t, req.Header.Get("user-agent"))
	require.Empty(t, req.Header.Get("accept-encoding"), `-accept-encoding "" omits the header`)
}

func TestHeaderFlagsSetRejectsMalformed(t *testing.T) {
	t.Parallel()
	var h headerFlags
	require.Error(t, h.Set("no-separator"))
	require.NoError(t, h.Set("name: value"))
	require.NoError(t, h.Set("name:"))
	require.Len(t, h, 2)
}

func httpRequest(t *testing.T) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, cursor.DefaultBaseURL+cursor.EndpointStreamChat, nil)
	require.NoError(t, err)
	return req
}
