package cursor

import (
	"sort"
	"strings"
	"testing"
)

// AgentService authenticates the bearer token alone. Sending the api2 identity
// block (checksum, machine ids, os/arch) is a change in observable behaviour,
// not a harmless superset, so the header set is pinned exactly.
func TestBuildAgentHeadersSendsExactlyTheCLISet(t *testing.T) {
	headers := BuildAgentHeaders("jwt-value", "cli-2026.01.01-abcdef0", true, "req-1")

	want := map[string]string{
		"Authorization":            "Bearer jwt-value",
		"Content-Type":             ContentTypeConnectProto,
		"Connect-Protocol-Version": "1",
		// gzip only: this client can decode nothing else per frame.
		"Connect-Accept-Encoding": "gzip",
		"X-Cursor-Client-Version":  "cli-2026.01.01-abcdef0",
		"X-Cursor-Client-Type":     "cli",
		"X-Ghost-Mode":             "true",
		"X-Request-Id":             "req-1",
		"X-Original-Request-Id":    "req-1",
		"User-Agent":               "connect-es/1.6.1",
	}

	if len(headers) != len(want) {
		got := make([]string, 0, len(headers))
		for name := range headers {
			got = append(got, name)
		}
		sort.Strings(got)
		t.Fatalf("header count = %d, want %d: %v", len(headers), len(want), got)
	}
	for name, value := range want {
		if got := headers.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}

	// The api2 identity block must not leak onto this endpoint.
	for _, forbidden := range []string{
		"x-cursor-checksum", "x-client-key", "x-session-id",
		"x-cursor-client-os", "x-cursor-client-arch", "x-cursor-client-device-type",
		"x-cursor-timezone", "x-new-onboarding-completed", "x-cursor-config-version",
	} {
		if headers.Get(forbidden) != "" {
			t.Errorf("%s must not be sent to AgentService", forbidden)
		}
	}
}

func TestBuildAgentHeadersGhostModeOff(t *testing.T) {
	if got := BuildAgentHeaders("jwt", "v", false, "id").Get("x-ghost-mode"); got != "false" {
		t.Errorf("x-ghost-mode = %q, want %q", got, "false")
	}
}

// A pasted browser cookie carries "userId::JWT"; only the JWT half is a bearer.
func TestBuildAgentHeadersStripsCookieUserID(t *testing.T) {
	for name, raw := range map[string]string{
		"decoded":         "user_123::jwt-value",
		"percent-encoded": "user_123%3A%3Ajwt-value",
	} {
		if got := BuildAgentHeaders(raw, "v", true, "id").Get("authorization"); got != "Bearer jwt-value" {
			t.Errorf("%s: authorization = %q, want %q", name, got, "Bearer jwt-value")
		}
	}
}

func TestBuildAgentHeadersFillsBlanks(t *testing.T) {
	headers := BuildAgentHeaders("jwt", "  ", true, "  ")

	if got := headers.Get("x-cursor-client-version"); got != DefaultCLIClientVersion {
		t.Errorf("client version = %q, want the pinned default %q", got, DefaultCLIClientVersion)
	}
	requestID := headers.Get("x-request-id")
	if requestID == "" {
		t.Fatal("a blank request id must be replaced with a fresh uuid")
	}
	// The CLI sends the same id twice: this attempt and the logical request.
	if got := headers.Get("x-original-request-id"); got != requestID {
		t.Errorf("x-original-request-id = %q, want it to match x-request-id %q", got, requestID)
	}
}

func TestAgentRunURL(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"default":        {"", DefaultAgentBaseURL + EndpointAgentRun},
		"plain":          {"https://agent.api5.cursor.sh", "https://agent.api5.cursor.sh" + EndpointAgentRun},
		"trailing slash": {"https://agent.api5.cursor.sh/", "https://agent.api5.cursor.sh" + EndpointAgentRun},
		"padded":         {"  https://agentn.us.api5.cursor.sh  ", "https://agentn.us.api5.cursor.sh" + EndpointAgentRun},
	} {
		if got := AgentRunURL(tc.in); got != tc.want {
			t.Errorf("%s: AgentRunURL(%q) = %q, want %q", name, tc.in, got, tc.want)
		}
	}
}

// The pinned version has to look like a CLI build: Cursor rejects an api2-era
// version string such as "2.6.22" on this endpoint.
func TestDefaultCLIClientVersionLooksLikeACLIBuild(t *testing.T) {
	if !strings.HasPrefix(DefaultCLIClientVersion, "cli-") {
		t.Errorf("DefaultCLIClientVersion = %q, want a cli-<date>-<sha> build", DefaultCLIClientVersion)
	}
	if ParseCLIVersionFromInstallScript(DefaultCLIClientVersion) != DefaultCLIClientVersion {
		t.Errorf("the pinned version %q does not match the version pattern", DefaultCLIClientVersion)
	}
}

func TestParseCLIVersionFromInstallScript(t *testing.T) {
	script := "#!/bin/sh\nVERSION=\"cli-2026.08.11-e8db854\"\nurl=\"https://downloads.cursor.com/x\"\n"
	if got := ParseCLIVersionFromInstallScript(script); got != "cli-2026.08.11-e8db854" {
		t.Errorf("parsed version = %q", got)
	}
	// No recognizable version means "keep the current pin", not "no version".
	if got := ParseCLIVersionFromInstallScript("#!/bin/sh\necho hi\n"); got != "" {
		t.Errorf("parsed version = %q, want empty", got)
	}
}
