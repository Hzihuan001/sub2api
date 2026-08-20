// Command cursor_e2e is a manual end-to-end probe against the real Cursor
// upstream (api2.cursor.sh). It speaks the wire protocol through
// internal/pkg/cursor — the same package the gateway uses — so whatever it
// observes is what production would observe.
//
// The credential is read from SUB2API_CURSOR_TOKEN only; it is never written to
// disk and never printed in full. Nothing here is wired into the server: this
// binary exists to answer "does a live account actually accept our frames?".
//
// The one exception to "never printed in full" is -mode exchange: the client
// token it mints is the probe's output, and the operator needs it verbatim to
// drive a chat run. The input web credential is still only ever fingerprinted.
//
// Usage examples:
//
//	cursor_e2e -mode models
//	cursor_e2e -mode chat -prompt "say hi" -model auto
//	cursor_e2e -mode tool -prompt "weather in Beijing?" -model auto -raw
//	cursor_e2e -mode exchange
//	cursor_e2e -mode chat -auto-exchange -model auto
//	cursor_e2e -mode agent -prompt "say hi" -model default
//
// -mode agent targets the current conversation protocol
// (agent.v1.AgentService/Run on api5), which is a different upstream from every
// other mode here: a bidirectional HTTP/2 stream, ten headers, and no identity
// block. The other modes speak api2's retired ChatService.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
	"github.com/google/uuid"
)

const (
	envToken = "SUB2API_CURSOR_TOKEN"

	defaultTimeout = 60 * time.Second
	defaultModel   = "gpt-4o-mini"
	defaultPrompt  = "Reply with a single short sentence confirming you are online."

	// maxBodyRead bounds a unary response so a runaway upstream cannot exhaust
	// memory. It is deliberately far above any real payload (AvailableModels is
	// a few hundred KiB) because everything read here is what gets parsed.
	maxBodyRead = 32 << 20
	// maxBodyDump caps how much of a body is *printed*. It must never reach a
	// parser: a clipped protobuf fails as "unexpected EOF" and the blame lands
	// on the wire format instead of on this cap.
	maxBodyDump = 64 << 10
	// maxPreview caps inline previews of byte fields in the protobuf dumper.
	maxPreview = 96
	// wireLen is the protobuf length-delimited wire type, needed by the
	// schema-less dumper (the package keeps its own copy unexported).
	wireLen = 2
)

// errUsage marks an invocation problem so main can exit 2 instead of 1.
var errUsage = errors.New("invalid usage")

func main() {
	// A probe that panics tells the operator nothing useful, so every failure
	// path — including a bug in this file — ends as a printed diagnostic.
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\n[PANIC] %v\n", r)
			os.Exit(1)
		}
	}()

	err := run()
	switch {
	case err == nil:
		os.Exit(0)
	case errors.Is(err, errUsage):
		fmt.Fprintf(os.Stderr, "\n[USAGE] %v\n", err)
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "\n[FAILED] %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	opts, err := parseOptions()
	if err != nil {
		return err
	}

	rawToken := os.Getenv(envToken)
	if strings.TrimSpace(rawToken) == "" {
		flag.Usage()
		return fmt.Errorf("%w: %s is not set", errUsage, envToken)
	}
	jwt, uid := cursor.ParseToken(rawToken)
	if jwt == "" {
		return fmt.Errorf("%w: %s holds no parseable token", errUsage, envToken)
	}

	printSetup(opts, rawToken, jwt, uid)

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	client := newClient(opts)

	if opts.mode == "exchange" {
		_, err := runExchange(ctx, client, opts, rawToken)
		return err
	}

	token := rawToken
	if opts.autoExchange {
		upgraded, err := maybeExchange(ctx, client, opts, rawToken)
		if err != nil {
			return err
		}
		token = upgraded
	}

	switch opts.mode {
	case "models":
		return runModels(ctx, client, opts, token)
	case "chat":
		return runChat(ctx, client, opts, token, false)
	case "tool":
		return runChat(ctx, client, opts, token, true)
	case "agent":
		return runAgent(ctx, client, opts, token)
	default:
		return fmt.Errorf("%w: unknown -mode %q", errUsage, opts.mode)
	}
}

// -------------------------------------------------------------------------
// options
// -------------------------------------------------------------------------

type options struct {
	mode      string
	base      string
	prompt    string
	model     string
	system    string
	timeout   time.Duration
	supported []int32
	raw       bool
	dump      bool
	http1     bool
	warmUp    bool
	acceptEnc string
	headers   headerFlags

	// Client identity knobs. Cursor's ChatService authenticates the whole block,
	// not just the Bearer token, so each of these is separately overridable to
	// bisect which one a live account is unhappy with.
	clientVer    string
	clientType   string
	clientOS     string
	clientArch   string
	clientOSVer  string
	deviceType   string
	timezone     string
	userAgent    string
	ghostMode    bool
	onboarding   bool
	machineID    string
	macMachineID string

	// Credential-exchange knobs. authBase and website are separate from base
	// because the loginDeepControl handshake always runs against the official
	// hosts, even when chat traffic is pointed at a relay.
	authBase         string
	website          string
	autoExchange     bool
	manualExchange   bool
	exchangeAttempts int
	exchangeInterval time.Duration

	// -mode agent knobs. The agent upstream is a separate host with its own
	// version floor, so it gets its own base URL rather than reusing -base.
	agentBase     string
	agentCwd      string
	agentTools    bool
	agentKeepOpen bool
	ghost         bool
	firstByte     time.Duration
	idleTimeout   time.Duration

	// explicit records which flags the operator actually passed, so a
	// mode-specific default can be applied without overriding a real choice.
	explicit map[string]bool
}

// profile turns the identity flags into the package's ClientProfile. Blank
// string flags keep the package default, so -h can advertise real values while
// an explicit empty override still means "the default".
func (o options) profile() cursor.ClientProfile {
	p := cursor.DefaultProfile()
	p.Version = firstNonEmpty(o.clientVer, p.Version)
	p.Type = firstNonEmpty(o.clientType, p.Type)
	p.OS = firstNonEmpty(o.clientOS, p.OS)
	p.Arch = firstNonEmpty(o.clientArch, p.Arch)
	p.DeviceType = firstNonEmpty(o.deviceType, p.DeviceType)
	p.Timezone = firstNonEmpty(o.timezone, p.Timezone)
	p.UserAgent = firstNonEmpty(o.userAgent, p.UserAgent)
	p.OSVersion = strings.TrimSpace(o.clientOSVer)
	p.MachineID = strings.TrimSpace(o.machineID)
	p.MacMachineID = strings.TrimSpace(o.macMachineID)
	p.GhostMode = o.ghostMode
	p.OnboardingCompleted = o.onboarding
	return p.Resolved()
}

func firstNonEmpty(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}

// headerFlags collects repeated -H "Name: value" overrides.
type headerFlags []string

func (h *headerFlags) String() string { return strings.Join(*h, ", ") }

func (h *headerFlags) Set(v string) error {
	if !strings.Contains(v, ":") && !strings.Contains(v, "=") {
		return fmt.Errorf("want %q, got %q", "Name: value", v)
	}
	*h = append(*h, v)
	return nil
}

func parseOptions() (options, error) {
	var (
		opts         options
		supportedRaw string
	)
	def := cursor.DefaultProfile()
	flag.StringVar(&opts.mode, "mode", "models", `probe to run: "models", "chat", "tool", "exchange" or "agent"`)
	flag.StringVar(&opts.base, "base", cursor.DefaultBaseURL, "upstream base URL")
	flag.StringVar(&opts.authBase, "auth-base", cursor.DefaultBaseURL, "base URL serving /auth/poll during a credential exchange")
	flag.StringVar(&opts.website, "website", cursor.WebsiteBaseURL, "base URL serving the cookie-authorized deep-link approval")
	flag.BoolVar(&opts.autoExchange, "auto-exchange", false, `upgrade a "web" credential to a client one before -mode chat / -mode tool`)
	flag.BoolVar(&opts.manualExchange, "manual-exchange", false, "skip the cookie approval: print the deep-link URL and poll while a human approves it")
	flag.IntVar(&opts.exchangeAttempts, "exchange-attempts", 0, "poll attempts during an exchange (0 = 30 automatic / 150 manual)")
	flag.DurationVar(&opts.exchangeInterval, "exchange-interval", 0, "delay between exchange polls (0 = 1s automatic / 2s manual)")
	flag.StringVar(&opts.prompt, "prompt", "", "user prompt for -mode chat / -mode tool")
	flag.StringVar(&opts.model, "model", defaultModel, `Cursor model id ("auto" is accepted)`)
	flag.StringVar(&opts.system, "system", "", "optional system prompt (Instruction.text)")
	flag.DurationVar(&opts.timeout, "timeout", defaultTimeout, "overall deadline for the whole probe")
	flag.StringVar(&supportedRaw, "supported", strconv.Itoa(int(cursor.ToolReadSemsearchFiles)),
		"comma-separated ClientSideToolV2 values sent in supported_tools (-mode tool)")
	flag.StringVar(&opts.clientVer, "client-version", def.Version, "x-cursor-client-version to advertise")
	flag.StringVar(&opts.clientType, "client-type", def.Type, `x-cursor-client-type ("ide" for the desktop app, "cli" for the CLI)`)
	flag.StringVar(&opts.clientOS, "client-os", def.OS, `x-cursor-client-os ("win32", "darwin", "linux")`)
	flag.StringVar(&opts.clientArch, "client-arch", def.Arch, `x-cursor-client-arch ("x64", "arm64")`)
	flag.StringVar(&opts.clientOSVer, "client-os-version", def.OSVersion, "x-cursor-client-os-version (empty omits the header, as the desktop client does)")
	flag.StringVar(&opts.deviceType, "device-type", def.DeviceType, "x-cursor-client-device-type")
	flag.StringVar(&opts.timezone, "timezone", def.Timezone, "x-cursor-timezone (IANA zone name)")
	flag.StringVar(&opts.userAgent, "user-agent", def.UserAgent, "user-agent to advertise")
	flag.BoolVar(&opts.ghostMode, "ghost-mode", def.GhostMode, "x-ghost-mode (privacy mode)")
	flag.BoolVar(&opts.onboarding, "onboarding-completed", def.OnboardingCompleted, "x-new-onboarding-completed")
	flag.StringVar(&opts.machineID, "machine-id", "",
		"device id inside x-cursor-checksum (empty derives it from the token; a real client reports its stable storage.serviceMachineId)")
	flag.StringVar(&opts.macMachineID, "mac-machine-id", "", "second checksum device id (empty derives it from the token)")
	flag.BoolVar(&opts.warmUp, "warmup", true,
		"call AvailableModels on the same credential before a chat turn, as the working Node/Python clients do")
	flag.StringVar(&opts.acceptEnc, "accept-encoding", "identity",
		`accept-encoding to send (empty omits it; "identity" keeps a streamed body unbuffered)`)
	flag.BoolVar(&opts.raw, "raw", false, "decode with FrameReader and print every Connect frame instead of using StreamDecoder")
	flag.BoolVar(&opts.dump, "dump", false, "schema-less protobuf field dump of request/response payloads (implies -raw)")
	flag.BoolVar(&opts.http1, "http1", false, "force HTTP/1.1 instead of negotiating HTTP/2")
	flag.Var(&opts.headers, "H", `extra request header "Name: value", applied last (repeatable); an empty value deletes the header`)
	flag.StringVar(&opts.agentBase, "agent-base", cursor.DefaultAgentBaseURL, "agent.v1 upstream base URL (-mode agent)")
	flag.StringVar(&opts.agentCwd, "agent-cwd", cursor.AgentDefaultCwd, "working directory reported in the environment frame (-mode agent)")
	flag.BoolVar(&opts.agentTools, "agent-tools", false, "declare one MCP tool so the native tool-call path can be observed (-mode agent)")
	flag.BoolVar(&opts.agentKeepOpen, "agent-keep-open", false, "keep reading after a native tool call instead of ending the turn (-mode agent)")
	flag.BoolVar(&opts.ghost, "ghost", true, "x-ghost-mode for -mode agent (the captured CLI sends true)")
	flag.DurationVar(&opts.firstByte, "first-byte-timeout", cursor.AgentDefaultFirstByteTimeout, "budget for the first response frame (-mode agent)")
	flag.DurationVar(&opts.idleTimeout, "idle-timeout", cursor.AgentDefaultIdleTimeout, "budget between response frames once output started (-mode agent)")
	flag.Usage = usage
	flag.Parse()

	opts.explicit = make(map[string]bool)
	flag.Visit(func(f *flag.Flag) { opts.explicit[f.Name] = true })

	if opts.dump {
		opts.raw = true
	}
	switch opts.mode {
	case "models", "chat", "tool", "exchange":
	case "agent":
		// The api2 defaults are wrong for this upstream: "gpt-4o-mini" is not
		// an agent model id, and an api2-era client version is refused outright
		// (the failure arrives as a Connect permission_denied).
		if !opts.explicit["model"] {
			opts.model = cursor.AgentDefaultModel
		}
		if !opts.explicit["client-version"] {
			opts.clientVer = cursor.DefaultCLIClientVersion
		}
	default:
		flag.Usage()
		return opts, fmt.Errorf("%w: unknown -mode %q", errUsage, opts.mode)
	}
	if strings.TrimSpace(opts.agentBase) == "" {
		return opts, fmt.Errorf("%w: -agent-base must not be empty", errUsage)
	}
	if opts.firstByte <= 0 || opts.idleTimeout <= 0 {
		return opts, fmt.Errorf("%w: -first-byte-timeout and -idle-timeout must be positive", errUsage)
	}
	for name, value := range map[string]string{"-base": opts.base, "-auth-base": opts.authBase, "-website": opts.website} {
		if strings.TrimSpace(value) == "" {
			return opts, fmt.Errorf("%w: %s must not be empty", errUsage, name)
		}
		if !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://") {
			return opts, fmt.Errorf("%w: %s %q needs an http(s) scheme", errUsage, name, value)
		}
	}
	if opts.timeout <= 0 {
		return opts, fmt.Errorf("%w: -timeout must be positive", errUsage)
	}
	if opts.exchangeAttempts < 0 {
		return opts, fmt.Errorf("%w: -exchange-attempts must not be negative", errUsage)
	}
	if opts.exchangeInterval < 0 {
		return opts, fmt.Errorf("%w: -exchange-interval must not be negative", errUsage)
	}
	if opts.exchangeAttempts == 0 {
		opts.exchangeAttempts = 30
		if opts.manualExchange {
			opts.exchangeAttempts = 150
		}
	}
	if opts.exchangeInterval == 0 {
		opts.exchangeInterval = time.Second
		if opts.manualExchange {
			opts.exchangeInterval = 2 * time.Second
		}
	}
	if strings.TrimSpace(opts.prompt) == "" {
		opts.prompt = defaultPrompt
	}
	tools, err := parseSupportedTools(supportedRaw)
	if err != nil {
		return opts, err
	}
	opts.supported = tools
	return opts, nil
}

func parseSupportedTools(raw string) ([]int32, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var out []int32
	for _, part := range strings.Split(trimmed, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		v, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("%w: -supported %q is not an int list", errUsage, raw)
		}
		out = append(out, int32(v))
	}
	return out, nil
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `cursor_e2e - live probe against the Cursor upstream.

The credential comes from the %s environment variable, in either the
"userId::JWT" cookie form (percent-encoded or not) or as a bare JWT. It is
never printed in full.

Cursor issues two kinds of JWT. A "web" one (the WorkosCursorSessionToken
cookie) is accepted by AvailableModels but rejected by chat with
ERROR_NOT_LOGGED_IN; only a client token can hold a conversation. -mode
exchange upgrades the former into the latter and prints the result in full,
so it can be fed straight back into %s.

A client token is necessary but not sufficient: chat also authenticates the
whole x-cursor-* identity block (client type, version, os/arch, checksum). Each
piece has its own flag so a rejection can be bisected, and -H can delete a
header outright by giving it an empty value.

-mode agent is the odd one out. It targets agent.v1.AgentService/Run on api5,
the protocol Cursor actually serves today, over a bidirectional HTTP/2 stream.
It sends ten headers and no identity block at all, so the x-cursor-* flags above
do not apply; -client-version and -model take agent-shaped defaults instead
(a "cli-<date>-<sha>" build and "default"). HTTP/2 is mandatory there: the
upstream load balancer drops HTTP/1.1 with an empty 464.

  $env:%s = "<token>"
  cursor_e2e -mode models
  cursor_e2e -mode chat -prompt "say hi in one sentence" -model auto
  cursor_e2e -mode chat -model auto -client-version 3.15.19
  cursor_e2e -mode chat -model auto -machine-id <64-hex from storage.serviceMachineId>
  cursor_e2e -mode chat -model auto -warmup=false -H "x-cursor-streaming:"
  cursor_e2e -mode tool -prompt "what is the weather in Beijing?" -model auto -raw
  cursor_e2e -mode exchange
  cursor_e2e -mode exchange -manual-exchange -timeout 6m
  cursor_e2e -mode chat -auto-exchange -model auto
  cursor_e2e -mode agent -prompt "say hi" -model default
  cursor_e2e -mode agent -prompt "say hi" -raw -agent-tools

Flags:
`, envToken, envToken, envToken)
	flag.PrintDefaults()
}

// -------------------------------------------------------------------------
// setup / token reporting
// -------------------------------------------------------------------------

func printSetup(opts options, rawToken, jwt, uid string) {
	section("probe setup")
	kv("mode", opts.mode)
	kv("timeout", opts.timeout)
	kv("http/2", !opts.http1)
	kv("frame-level decode", opts.raw)
	kv("protobuf dump", opts.dump)
	// -base and -accept-encoding are api2 knobs; -mode agent reads neither, and
	// printing them would suggest a lever the agent transport does not have.
	if opts.mode != "agent" {
		kv("base url", opts.base)
		kv("accept-encoding", orNone(opts.acceptEnc))
	}
	if opts.mode == "chat" || opts.mode == "tool" {
		kv("model", opts.model)
		kv("prompt chars", len(opts.prompt))
		kv("known model ids", strings.Join(cursor.DefaultModelIDs(), ", "))
		kv("auto exchange", opts.autoExchange)
		kv("session warm-up", opts.warmUp)
	}
	if opts.mode == "agent" {
		// The x-cursor-* identity block is not sent on this endpoint, so
		// printing it would only invite bisecting a header that never travels.
		printAgentSetup(opts)
	} else {
		printProfile(opts.profile())
	}
	if opts.mode == "tool" {
		kv("supported_tools", fmt.Sprint(opts.supported))
	}
	if opts.mode == "exchange" || opts.autoExchange {
		kv("website url", opts.website)
		kv("auth url", opts.authBase)
		kv("exchange mode", exchangeModeLabel(opts))
		kv("poll budget", fmt.Sprintf("%d x %s", opts.exchangeAttempts, opts.exchangeInterval))
	}

	section("credential (never printed in full)")
	form := "bare JWT"
	switch {
	case strings.Contains(rawToken, "::"):
		form = `"userId::JWT"`
	case strings.Contains(strings.ToUpper(rawToken), "%3A%3A"):
		form = `"userId%3A%3AJWT" (browser cookie value)`
	}
	kv("env var", envToken)
	kv("stored form", form)
	kv("raw length", len(strings.TrimSpace(rawToken)))
	kv("jwt length", len(jwt))
	kv("jwt segments", len(strings.Split(jwt, ".")))
	kv("user id", orNone(uid))
	// A SHA-256 prefix is a one-way fingerprint (and is already sent upstream
	// as x-client-key), so it correlates runs without leaking the token.
	fp := cursor.ClientKey(jwt)
	kv("client-key fp", fp[:12]+"...")
	kv("session id", cursor.SessionID(jwt))
	printJWTClaims(jwt)
}

// printProfile reports the client identity that will be advertised. Cursor's
// ChatService authenticates this whole block, so a rejected run is read against
// it first.
func printProfile(p cursor.ClientProfile) {
	section("client identity (x-cursor-* headers)")
	kv("client version", p.Version)
	kv("client type", p.Type)
	kv("client os", p.OS)
	kv("client arch", p.Arch)
	kv("client os version", orNone(p.OSVersion)+" (omitted when none)")
	kv("device type", p.DeviceType)
	kv("timezone", p.Timezone)
	kv("user agent", p.UserAgent)
	kv("ghost mode", p.GhostMode)
	kv("onboarding completed", p.OnboardingCompleted)
	kv("machine id", checksumIDLabel(p.MachineID))
	kv("mac machine id", checksumIDLabel(p.MacMachineID))
}

// checksumIDLabel distinguishes a pinned device id from the token-derived one,
// which changes with every credential and is the thing a real client never does.
func checksumIDLabel(id string) string {
	if strings.TrimSpace(id) == "" {
		return "<derived from token>"
	}
	return id
}

// printJWTClaims surfaces the handful of unsigned claims that explain upstream
// rejections (a "web" session token behaves differently from a client one).
func printJWTClaims(jwt string) {
	parts := strings.Split(jwt, ".")
	if len(parts) < 2 {
		kv("jwt claims", "<not a JWT>")
		return
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		if payload, err = base64.URLEncoding.DecodeString(parts[1]); err != nil {
			kv("jwt claims", "<undecodable: "+err.Error()+">")
			return
		}
	}
	var claims map[string]json.RawMessage
	if err := json.Unmarshal(payload, &claims); err != nil {
		kv("jwt claims", "<unparseable: "+err.Error()+">")
		return
	}
	for _, name := range []string{"type", "scope", "aud", "iss", "iat", "exp"} {
		raw, ok := claims[name]
		if !ok {
			continue
		}
		value := strings.Trim(string(raw), `"`)
		if name == "exp" || name == "iat" {
			if secs, convErr := strconv.ParseInt(value, 10, 64); convErr == nil {
				at := time.Unix(secs, 0)
				suffix := ""
				if name == "exp" {
					suffix = " (expired)"
					if at.After(time.Now()) {
						suffix = fmt.Sprintf(" (in %s)", time.Until(at).Round(time.Minute))
					}
				}
				value = at.Format(time.RFC3339) + suffix
			}
		}
		kv("claim "+name, value)
	}
}

// -------------------------------------------------------------------------
// mode: exchange (web credential -> client credential)
// -------------------------------------------------------------------------

func exchangeModeLabel(opts options) string {
	if opts.manualExchange {
		return "manual (human approves in a browser)"
	}
	return "automatic (cookie-authorized approval)"
}

// maybeExchange upgrades the credential for a chat/tool run when -auto-exchange
// is set. A credential that is already a client token is left alone: exchanging
// it would only mint a second session for no reason.
func maybeExchange(ctx context.Context, client *http.Client, opts options, rawToken string) (string, error) {
	if !cursor.IsWebSessionToken(rawToken) {
		section("credential upgrade")
		kv("token type", orNone(cursor.TokenType(rawToken)))
		fmt.Println("  already a client credential; chat runs with the token as provided")
		return rawToken, nil
	}
	return runExchange(ctx, client, opts, rawToken)
}

// runExchange performs the loginDeepControl handshake and reports the client
// credential it produced. The automatic path calls the exact function the
// gateway uses, wrapped in a tracing transport so every leg is visible; the
// manual path drives the same primitives by hand so a human can approve in a
// browser when the cookie-authorized endpoint is unavailable.
func runExchange(ctx context.Context, client *http.Client, opts options, rawToken string) (string, error) {
	section("credential exchange")
	kv("input token type", orNone(cursor.TokenType(rawToken)))
	kv("approval endpoint", strings.TrimRight(opts.website, "/")+cursor.EndpointLoginDeepCallbackControl)
	kv("poll endpoint", strings.TrimRight(opts.authBase, "/")+cursor.EndpointAuthPoll)
	if !cursor.IsWebSessionToken(rawToken) {
		fmt.Printf("  [warn] input is not a %q token; running the exchange anyway so the result can be compared\n", cursor.TokenTypeWeb)
	}

	tracer := &tracingDoer{inner: client}
	exchangeOpts := cursor.ExchangeOptions{
		HTTPClient:     tracer,
		WebsiteBaseURL: opts.website,
		APIBaseURL:     opts.authBase,
		PollAttempts:   opts.exchangeAttempts,
		PollInterval:   opts.exchangeInterval,
	}

	start := time.Now()
	var (
		token *cursor.TokenResponse
		err   error
	)
	if opts.manualExchange {
		token, err = manualExchange(ctx, exchangeOpts)
	} else {
		token, err = cursor.ExchangeWebSessionWithOptions(ctx, rawToken, exchangeOpts)
	}
	elapsed := time.Since(start)

	kv("http calls", tracer.calls)
	kv("elapsed", elapsed.Round(time.Millisecond))
	if err != nil {
		return "", fmt.Errorf("exchange web session: %w", err)
	}

	printExchangedCredential(token)
	return token.AccessToken, nil
}

// manualExchange is the fallback for when the cookie-authorized approval cannot
// be used: it prints the deep-link URL and polls while a human clicks through.
func manualExchange(ctx context.Context, opts cursor.ExchangeOptions) (*cursor.TokenResponse, error) {
	login, err := cursor.BuildDeepLoginURL()
	if err != nil {
		return nil, err
	}
	section("manual approval required")
	kv("uuid", login.UUID)
	kv("challenge", login.Challenge)
	fmt.Println("  open this URL, sign in if asked, then click \"Yes, Log In\":")
	fmt.Println("    " + login.LoginURL)
	fmt.Println("  polling until the approval lands...")

	token, err := cursor.PollDeepLogin(ctx, opts, login.UUID, login.Verifier)
	if err != nil {
		return nil, err
	}
	if cursor.IsWebSessionToken(token.AccessToken) {
		return nil, cursor.ErrWebSessionNotUpgraded
	}
	return token, nil
}

// printExchangedCredential reports the minted credential in full. This is the
// one secret the probe prints: it is the answer the operator ran the probe for,
// and it has to be pasted into the next command.
func printExchangedCredential(token *cursor.TokenResponse) {
	jwt, uid := cursor.ParseToken(token.AccessToken)

	section("exchanged credential")
	kv("token type", orNone(cursor.TokenType(token.AccessToken)))
	kv("user id", orNone(uid))
	kv("auth id", orNone(token.AuthID))
	kv("access token chars", len(token.AccessToken))
	kv("refresh token chars", len(token.RefreshToken))
	kv("expires in", token.ExpiresIn)
	kv("client-key fp", cursor.ClientKey(jwt)[:12]+"...")
	printJWTClaims(jwt)

	section("exchanged credential (verbatim - handle as a secret)")
	fmt.Println("  access_token:")
	fmt.Println("    " + token.AccessToken)
	if token.RefreshToken != "" {
		fmt.Println("  refresh_token:")
		fmt.Println("    " + token.RefreshToken)
	}
	if uid != "" {
		fmt.Println("  cookie form (userId%3A%3AJWT):")
		fmt.Println("    " + cursor.NormalizeSessionCookie(token.AccessToken))
	}
	fmt.Println("  reuse it with:")
	fmt.Printf("    $env:%s = \"%s\"\n", envToken, token.AccessToken)
}

// tracingDoer reports each exchange leg as it happens, so a stuck handshake
// shows which call is not answering instead of a single opaque timeout.
type tracingDoer struct {
	inner *http.Client
	calls int
}

func (d *tracingDoer) Do(req *http.Request) (*http.Response, error) {
	d.calls++
	start := time.Now()
	resp, err := d.inner.Do(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	// The verifier travels in the poll query string; it is redeemable, so the
	// query is reported by key only.
	target := req.URL.Scheme + "://" + req.URL.Host + req.URL.Path
	if raw := req.URL.Query(); len(raw) > 0 {
		names := make([]string, 0, len(raw))
		for name := range raw {
			names = append(names, name)
		}
		sort.Strings(names)
		target += "?" + strings.Join(names, "&") + " (values redacted)"
	}
	if err != nil {
		fmt.Printf("  [exchange %d] %s %s -> error after %s: %v\n", d.calls, req.Method, target, elapsed, err)
		return nil, err
	}
	fmt.Printf("  [exchange %d] %s %s -> %s (%s)\n", d.calls, req.Method, target, resp.Status, elapsed)
	return resp, nil
}

// -------------------------------------------------------------------------
// http plumbing
// -------------------------------------------------------------------------

func newClient(opts options) *http.Client {
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		ForceAttemptHTTP2:   !opts.http1,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 20 * time.Second,
		// Content-Encoding is handled explicitly below so a streaming body is
		// never silently buffered or double-decompressed.
		DisableCompression: true,
	}
	if opts.http1 {
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	// No client-level Timeout: the context deadline covers the whole probe and
	// still lets a stream be read incrementally.
	return &http.Client{Transport: tr}
}

func applyHeaders(req *http.Request, base http.Header, opts options) {
	for name, values := range base {
		for _, v := range values {
			req.Header.Set(name, v)
		}
	}
	// Per-frame gzip (connect-accept-encoding) stays on; whole-body gzip is
	// declined by default so frames arrive as the server flushes them.
	if enc := strings.TrimSpace(opts.acceptEnc); enc != "" {
		req.Header.Set("accept-encoding", enc)
	} else {
		req.Header.Del("accept-encoding")
	}
	// -H is applied last so it can override anything above, including deleting
	// a header outright — the only way to A/B whether one of them is the
	// reason the upstream refuses a request.
	for _, raw := range opts.headers {
		name, value, ok := strings.Cut(raw, ":")
		if !ok {
			name, value, _ = strings.Cut(raw, "=")
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if value == "" {
			req.Header.Del(name)
			continue
		}
		req.Header.Set(name, value)
	}
}

// bodyReader unwraps a whole-body Content-Encoding the server applied anyway.
func bodyReader(resp *http.Response) (io.Reader, func(), error) {
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("content-encoding")), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, func() {}, fmt.Errorf("gzip response body: %w", err)
		}
		return gr, func() { _ = gr.Close() }, nil
	}
	return resp.Body, func() {}, nil
}

// wholeBodyEncoded reports whether the server compressed the whole body, in
// which case content-length counts compressed bytes and cannot be compared
// against how much we decoded.
func wholeBodyEncoded(resp *http.Response) bool {
	enc := strings.ToLower(strings.TrimSpace(resp.Header.Get("content-encoding")))
	return enc != "" && enc != "identity"
}

// readBody reads a unary response in full. The returned bytes are the ones a
// parser may see, so the only limit applied here is the memory guard: overflow
// is reported separately instead of silently handing back a clipped message.
func readBody(r io.Reader) (body []byte, overflow bool, err error) {
	body, err = io.ReadAll(io.LimitReader(r, maxBodyRead+1))
	if len(body) > maxBodyRead {
		return body[:maxBodyRead], true, err
	}
	return body, false, err
}

// bodyPreview shortens a body for printing. Callers keep the full slice for
// parsing and pass only the result of this call to the output helpers.
func bodyPreview(body []byte) (preview []byte, truncated bool) {
	if len(body) <= maxBodyDump {
		return body, false
	}
	return body[:maxBodyDump], true
}

func printRequest(req *http.Request, bodyLen int) {
	section("request")
	kv("method", req.Method)
	kv("url", req.URL.String())
	kv("body bytes", bodyLen)
	fmt.Println("  headers:")
	names := make([]string, 0, len(req.Header))
	for name := range req.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("    %-26s %s\n", strings.ToLower(name)+":", redactHeader(name, req.Header.Get(name)))
	}
}

func printResponseMeta(resp *http.Response, elapsed time.Duration) {
	section("response")
	kv("status", resp.Status)
	kv("proto", resp.Proto)
	kv("time to headers", elapsed.Round(time.Millisecond))
	kv("content-type", orNone(resp.Header.Get("content-type")))
	kv("content-encoding", orNone(resp.Header.Get("content-encoding")))
	kv("content-length", resp.ContentLength)
	fmt.Println("  headers:")
	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range resp.Header[name] {
			fmt.Printf("    %-26s %s\n", strings.ToLower(name)+":", redactHeader(name, value))
		}
	}
}

func redactHeader(name, value string) string {
	switch strings.ToLower(name) {
	case "authorization", "cookie", "set-cookie":
		return fmt.Sprintf("<redacted, %d chars>", len(value))
	}
	if len(value) > 80 {
		return fmt.Sprintf("%s... (%d chars)", value[:80], len(value))
	}
	return value
}

// -------------------------------------------------------------------------
// mode: models
// -------------------------------------------------------------------------

func runModels(ctx context.Context, client *http.Client, opts options, token string) error {
	payload := cursor.EncodeAvailableModelsRequest(false, false)
	target := strings.TrimRight(opts.base, "/") + cursor.EndpointAvailableModels

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	applyHeaders(req, cursor.BuildHeadersWithProfile(token, cursor.ContentTypeProto, opts.profile()), opts)
	printRequest(req, len(payload))
	if opts.dump && len(payload) > 0 {
		section("request protobuf")
		dumpProto("  ", payload, 0)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("transport error after %s: %w", time.Since(start).Round(time.Millisecond), err)
	}
	defer func() { _ = resp.Body.Close() }()
	printResponseMeta(resp, time.Since(start))

	reader, closeReader, err := bodyReader(resp)
	if err != nil {
		return err
	}
	defer closeReader()

	body, overflow, readErr := readBody(reader)
	kv("body bytes", len(body))
	if readErr != nil {
		fmt.Printf("  [warn] read body: %v\n", readErr)
	}
	if !wholeBodyEncoded(resp) && resp.ContentLength >= 0 && int64(len(body)) != resp.ContentLength {
		fmt.Printf("  [warn] read %d bytes but content-length announced %d\n", len(body), resp.ContentLength)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		section("error body (verbatim)")
		printBody(body)
		return fmt.Errorf("AvailableModels returned %s", resp.Status)
	}
	// Parsing an incomplete message only produces a misleading "unexpected EOF",
	// so a short or oversized read is reported as what it is: a read problem.
	if overflow {
		section("oversized success body")
		printBody(body)
		return fmt.Errorf("AvailableModels body exceeds the %d byte read cap; refusing to parse a clipped message", maxBodyRead)
	}
	if readErr != nil {
		section("incomplete success body")
		printBody(body)
		return fmt.Errorf("read AvailableModels body (stopped after %d bytes): %w", len(body), readErr)
	}

	models, err := cursor.ParseAvailableModelsResponse(body)
	if err != nil {
		section("undecodable success body")
		printBody(body)
		return fmt.Errorf("parse AvailableModels response: %w", err)
	}
	if opts.dump {
		section("response protobuf")
		dumpProto("  ", body, 0)
	}
	printModels(models)
	return nil
}

func printModels(models []cursor.Model) {
	section("models")
	kv("total", len(models))
	if len(models) == 0 {
		fmt.Println("  <upstream returned an empty model list>")
		return
	}
	fmt.Printf("  %-4s %-34s %-34s %14s %8s %8s %9s\n",
		"#", "Name", "ServerModelName", "CtxTokenLimit", "Images", "MaxMode", "Variants")
	for i, m := range models {
		fmt.Printf("  %-4d %-34s %-34s %14d %8v %8v %9d\n",
			i+1, truncate(m.Name, 34), truncate(m.ServerModelName, 34),
			m.ContextTokenLimit, m.SupportsImages, m.SupportsMaxMode, len(m.ParameterizedVariants))
	}

	section("model details")
	for _, m := range models {
		fmt.Printf("  - %s\n", m.Name)
		fmt.Printf("      server_model_name=%q client_display_name=%q\n", m.ServerModelName, m.ClientDisplayName)
		fmt.Printf("      context_token_limit=%d max_mode_context_token_limit=%d\n", m.ContextTokenLimit, m.MaxModeContextTokenLimit)
		fmt.Printf("      supports_images=%v supports_max_mode=%v supports_non_max_mode=%v\n", m.SupportsImages, m.SupportsMaxMode, m.SupportsNonMaxMode)
		for _, v := range m.ParameterizedVariants {
			fmt.Printf("      variant %q max_mode=%v default_max=%v default_non_max=%v params=%d string=%q\n",
				v.DisplayName, v.IsMaxMode, v.IsDefaultMaxConfig, v.IsDefaultNonMaxConfig, len(v.Params), v.VariantString)
		}
	}

	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	section("model ids (comma-separated)")
	fmt.Println("  " + strings.Join(names, ","))
}

// -------------------------------------------------------------------------
// modes: chat / tool
// -------------------------------------------------------------------------

func runChat(ctx context.Context, client *http.Client, opts options, token string, withTools bool) error {
	if opts.warmUp {
		warmUpSession(ctx, client, opts, token)
	}

	chatReq := buildChatRequest(opts, withTools)
	printChatRequest(chatReq, withTools)

	payload := cursor.EncodeChatRequest(chatReq)
	frame := cursor.EncodeFrame(payload, false)
	target := strings.TrimRight(opts.base, "/") + cursor.EndpointStreamChat

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(frame))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	applyHeaders(req, cursor.BuildHeadersWithProfile(token, cursor.ContentTypeConnectProto, opts.profile()), opts)
	printRequest(req, len(frame))
	kv("proto bytes", len(payload))
	kv("frame bytes", len(frame))
	kv("frame flag", fmt.Sprintf("0x%02x", frame[0]))
	if opts.dump {
		section("request protobuf")
		dumpProto("  ", payload, 0)
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("transport error after %s: %w", time.Since(start).Round(time.Millisecond), err)
	}
	defer func() { _ = resp.Body.Close() }()
	printResponseMeta(resp, time.Since(start))

	reader, closeReader, err := bodyReader(resp)
	if err != nil {
		return err
	}
	defer closeReader()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _, bodyErr := readBody(reader)
		if bodyErr != nil {
			fmt.Printf("  [warn] read body: %v\n", bodyErr)
		}
		section("error body (verbatim)")
		printBody(body)
		return fmt.Errorf("StreamUnifiedChatWithTools returned %s", resp.Status)
	}

	// The success path never buffers: reader stays the raw body so FrameReader
	// and StreamDecoder see frames as the server flushes them, with no overall
	// length limit beyond the package's per-frame guard.
	section("stream")
	state := newStreamState(start)
	var consumeErr error
	if opts.raw {
		consumeErr = consumeRawFrames(reader, state, opts.dump)
	} else {
		consumeErr = consumeDecoded(reader, state)
	}
	state.closeChannel()
	printStreamSummary(state, time.Since(start), consumeErr)

	if consumeErr != nil {
		return fmt.Errorf("stream aborted: %w", consumeErr)
	}
	if state.endErr != nil {
		return fmt.Errorf("upstream ended with error: %w", state.endErr)
	}
	return nil
}

// warmUpSession replays what the working Node and Python clients do before a
// chat turn: one AvailableModels call on the same credential and the same
// identity headers. Whether the upstream actually requires it is unproven, so
// this is reported and never fatal — a failed warm-up still lets the chat run,
// which is the only way to tell the two apart.
func warmUpSession(ctx context.Context, client *http.Client, opts options, token string) {
	section("session warm-up (AvailableModels)")
	target := strings.TrimRight(opts.base, "/") + cursor.EndpointAvailableModels
	payload := cursor.EncodeAvailableModelsRequest(false, false)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		fmt.Printf("  [warn] build warm-up request: %v\n", err)
		return
	}
	applyHeaders(req, cursor.BuildHeadersWithProfile(token, cursor.ContentTypeProto, opts.profile()), opts)

	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("  [warn] warm-up failed after %s: %v\n", elapsed, err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	read, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, maxBodyRead))
	kv("status", resp.Status)
	kv("body bytes", read)
	kv("elapsed", elapsed)
}

// buildChatRequest mirrors the gateway's translation layer: a plain turn stays
// in CHAT mode, while a tool turn flips to AGENT mode with mcp_tools,
// supported_tools and per-message ids — the same shape
// service.applyCursorToolPlan produces.
func buildChatRequest(opts options, withTools bool) cursor.ChatRequest {
	req := cursor.ChatRequest{
		Model:        opts.model,
		SystemPrompt: opts.system,
		UnifiedMode:  cursor.UnifiedModeChat,
		Messages: []cursor.ChatMessage{{
			Content: opts.prompt,
			Role:    cursor.RoleUser,
		}},
	}
	if !withTools {
		return req
	}

	req.MCPTools = []cursor.MCPTool{weatherTool()}
	req.IsAgentic = true
	req.UnifiedMode = cursor.UnifiedModeAgent
	req.UnifiedModeName = cursor.UnifiedModeNameAgent
	req.SupportedTools = opts.supported
	req.ConversationID = uuid.NewString()
	for i := range req.Messages {
		req.Messages[i].IsAgentic = true
		req.Messages[i].UnifiedMode = cursor.UnifiedModeAgent
		if req.Messages[i].ID == "" {
			req.Messages[i].ID = uuid.NewString()
		}
		req.MessageIDs = append(req.MessageIDs, cursor.MessageID{
			ID:   req.Messages[i].ID,
			Role: req.Messages[i].Role,
		})
	}
	if last := len(req.Messages) - 1; last >= 0 {
		req.Messages[last].SupportedTools = opts.supported
	}
	return req
}

func weatherTool() cursor.MCPTool {
	return cursor.MCPTool{
		Name:        "get_weather",
		Description: "Get the current weather for a location.",
		Parameters: `{"type":"object","properties":{"location":{"type":"string",` +
			`"description":"City name, for example Beijing"}},"required":["location"]}`,
		Server: cursor.McpServerCustom,
	}
}

func printChatRequest(req cursor.ChatRequest, withTools bool) {
	section("chat request")
	kv("model", req.Model)
	kv("unified_mode", req.UnifiedMode)
	kv("unified_mode_name", orNone(req.UnifiedModeName))
	kv("is_agentic", req.IsAgentic)
	kv("disable_tools", req.ShouldDisableTools)
	kv("thinking_level", req.ThinkingLevel)
	kv("conversation_id", orNone(req.ConversationID))
	kv("system prompt", orNone(truncate(req.SystemPrompt, 120)))
	kv("messages", len(req.Messages))
	for i, m := range req.Messages {
		fmt.Printf("    [%d] role=%d id=%s supported_tools=%v content=%q\n",
			i, m.Role, orNone(m.ID), m.SupportedTools, truncate(m.Content, 160))
	}
	if withTools {
		kv("supported_tools", fmt.Sprint(req.SupportedTools))
		kv("mcp_tools", len(req.MCPTools))
		for _, t := range req.MCPTools {
			fmt.Printf("    - name=%q server=%q description=%q\n", t.Name, t.Server, t.Description)
			fmt.Printf("      parameters=%s\n", t.Parameters)
		}
	}
}

// -------------------------------------------------------------------------
// mode: agent (agent.v1.AgentService/Run, the current conversation protocol)
// -------------------------------------------------------------------------

func printAgentSetup(opts options) {
	section("agent transport (agent.v1.AgentService/Run)")
	kv("agent base url", opts.agentBase)
	kv("run endpoint", cursor.AgentRunURL(opts.agentBase))
	kv("model", opts.model)
	kv("client version", opts.clientVer)
	kv("ghost mode", opts.ghost)
	kv("cwd", opts.agentCwd)
	kv("prompt chars", len(opts.prompt))
	kv("system prompt", orNone(truncate(opts.system, 120)))
	kv("declare mcp tool", opts.agentTools)
	kv("keep open after tool", opts.agentKeepOpen)
	kv("first-byte budget", opts.firstByte)
	kv("idle budget", opts.idleTimeout)
	kv("heartbeat interval", cursor.AgentHeartbeatInterval)
	fmt.Println("  note: no x-cursor-* identity block is sent on this endpoint")
}

func runAgent(ctx context.Context, client *http.Client, opts options, token string) error {
	// The request id is minted here rather than inside the package so the
	// headers printed below are exactly the ones that travel.
	requestID := uuid.NewString()
	params := cursor.AgentRunParams{
		Prompt:         opts.prompt,
		Model:          opts.model,
		SystemPrompt:   opts.system,
		Mode:           cursor.AgentModeAgent,
		ConversationID: uuid.NewString(),
		MessageID:      uuid.NewString(),
		Cwd:            opts.agentCwd,
	}
	if opts.agentTools {
		params.Tools = []cursor.AgentTool{agentWeatherTool()}
	}

	printAgentRequest(params, opts, token, requestID)

	state := newAgentState(time.Now())
	stream, err := cursor.OpenAgentStream(ctx, params, cursor.AgentStreamOptions{
		BaseURL:                  opts.agentBase,
		Token:                    token,
		ClientVersion:            opts.clientVer,
		GhostMode:                opts.ghost,
		RequestID:                requestID,
		HTTPClient:               client,
		FirstByteTimeout:         opts.firstByte,
		IdleTimeout:              opts.idleTimeout,
		KeepReadingAfterToolCall: opts.agentKeepOpen,
		// -http1 forces the transport that the upstream load balancer drops;
		// waiving the guard is the only way to see what it actually answers.
		AllowHTTP1:     opts.http1,
		OnRequestFrame: printAgentRequestFrame(opts),
		OnResponseFrame: func(info cursor.AgentFrameInfo, frame *cursor.Frame) {
			state.recordResponseFrame(info, frame, opts)
		},
	})
	if err != nil {
		section("agent stream failed before any output")
		printAgentOpenError(err)
		return fmt.Errorf("open agent stream: %w", err)
	}
	defer func() { _ = stream.Close() }()

	printResponseMeta(stream.Response(), time.Since(state.start))

	section("stream")
	for event := range stream.Events() {
		state.handle(event)
	}
	state.closeChannel()
	printAgentSummary(state)

	if state.endErr != nil {
		return fmt.Errorf("agent turn ended with error: %w", state.endErr)
	}
	return nil
}

// agentWeatherTool mirrors the tool the api2 -mode tool probe declares, so the
// two protocols can be compared on the same input.
func agentWeatherTool() cursor.AgentTool {
	return cursor.AgentTool{
		Name:        "get_weather",
		Description: "Get the current weather for a location.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{
					"type":        "string",
					"description": "City name, for example Beijing",
				},
			},
			"required": []any{"location"},
		},
	}
}

func printAgentRequest(params cursor.AgentRunParams, opts options, token, requestID string) {
	section("agent request")
	kv("conversation_id", params.ConversationID)
	kv("message_id", params.MessageID)
	kv("mode", fmt.Sprintf("AGENT(%d)", params.Mode))
	kv("model", params.Model)
	kv("mcp tools", len(params.Tools))
	for _, tool := range params.Tools {
		fmt.Printf("    - name=%q description=%q\n", tool.Name, tool.Description)
	}

	// These are the ten headers the CLI sends, and the only ones we send.
	section("agent request headers (as sent)")
	headers := cursor.BuildAgentHeaders(token, opts.clientVer, opts.ghost, requestID)
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("    %-26s %s\n", strings.ToLower(name)+":", redactHeader(name, headers.Get(name)))
	}
	kv("header count", len(headers))

	section("agent request frame plan")
	plans := cursor.BuildRunFrameSequence(params)
	kv("frames", len(plans))
	total := 0
	for i, plan := range plans {
		payload := len(plan.Payload)
		total += payload + 5
		fmt.Printf("    [%2d] %-22s payload=%6dB frame=%6dB delay_after=%s\n",
			i, plan.Label, payload, payload+5, plan.DelayAfter)
	}
	kv("total request bytes", total)
	kv("then", fmt.Sprintf("one heartbeat every %s until the turn ends", cursor.AgentHeartbeatInterval))
	if opts.dump {
		section("run_request protobuf")
		dumpProto("  ", plans[0].Payload, 0)
		section("environment frame protobuf")
		dumpProto("  ", plans[1].Payload, 0)
	}
}

// printAgentOpenError renders the failure that stopped the turn before the
// stream existed. A Connect-coded error is the interesting case: it carries the
// upstream's own verdict rather than a transport symptom.
func printAgentOpenError(err error) {
	var agentErr *cursor.AgentError
	if !errors.As(err, &agentErr) {
		kv("error", err.Error())
		return
	}
	kv("connect code", orNone(agentErr.Code))
	kv("message", orNone(agentErr.Message))
	kv("mapped http status", agentErr.HTTPStatus)
	kv("raw body", orNone(agentErr.Raw))
	if agentErr.Code == "permission_denied" {
		fmt.Println("  hint: permission_denied on a valid credential usually means the")
		fmt.Println("        advertised -client-version is below Cursor's accepted floor")
	}
}

func printAgentRequestFrame(opts options) func(cursor.AgentFrameInfo) {
	return func(info cursor.AgentFrameInfo) {
		fmt.Printf("[req frame %2d] %-22s payload=%6dB frame=%6dB delay_after=%-8s (+%s)\n",
			info.Index, info.Label, info.PayloadBytes, info.FrameBytes,
			info.DelayAfter, info.Elapsed.Round(time.Millisecond))
	}
}

// aggAgentTool is one native tool call as reported by the upstream.
type aggAgentTool struct {
	ID        string
	Name      string
	Arguments string
	Provider  string
}

type agentState struct {
	start   time.Time
	channel string

	haveFirst  bool
	firstEvent time.Duration

	text     strings.Builder
	thinking strings.Builder

	counts map[cursor.AgentEventType]int

	respFrames int
	respBytes  int

	tools     []aggAgentTool
	usage     *cursor.AgentUsage
	tokenSeen int64

	endErr error
}

func newAgentState(start time.Time) *agentState {
	return &agentState{start: start, counts: make(map[cursor.AgentEventType]int)}
}

// switchChannel keeps interleaved text/thinking deltas readable, exactly as the
// api2 stream printer does.
func (s *agentState) switchChannel(name string) {
	if s.channel == name {
		return
	}
	if s.channel != "" {
		fmt.Println()
	}
	if name != "" {
		fmt.Printf("--- %s ---\n", name)
	}
	s.channel = name
}

func (s *agentState) closeChannel() { s.switchChannel("") }

func (s *agentState) recordResponseFrame(info cursor.AgentFrameInfo, frame *cursor.Frame, opts options) {
	s.respFrames++
	s.respBytes += info.FrameBytes
	if !opts.raw {
		return
	}
	s.closeChannel()
	fmt.Printf("[resp frame %d] flag=0x%02x compressed=%v end_stream=%v payload=%dB (+%s)\n",
		info.Index, frame.Flag, frame.Compressed, frame.EndStream,
		info.PayloadBytes, info.Elapsed.Round(time.Millisecond))
	if frame.EndStream {
		fmt.Printf("  trailer json: %s\n", orNone(strings.TrimSpace(string(frame.Payload))))
		return
	}
	if opts.dump {
		dumpProto("  ", frame.Payload, 0)
	}
}

func (s *agentState) handle(event cursor.AgentEvent) {
	s.counts[event.Type]++
	if !s.haveFirst && event.Type != cursor.AgentEventTurnEnded && event.Type != cursor.AgentEventError {
		s.firstEvent = time.Since(s.start)
		s.haveFirst = true
	}

	switch event.Type {
	case cursor.AgentEventText:
		s.text.WriteString(event.Text)
		s.switchChannel("assistant text")
		fmt.Print(event.Text)

	case cursor.AgentEventThinking:
		s.thinking.WriteString(event.Text)
		s.switchChannel("thinking")
		fmt.Print(event.Text)

	case cursor.AgentEventThinkingEnd:
		s.closeChannel()
		fmt.Printf("[thinking_end] duration=%dms\n", usageOrZero(event.Usage).ThinkingDurationMs)

	case cursor.AgentEventToolCall:
		s.closeChannel()
		call := event.ToolCall
		s.tools = append(s.tools, aggAgentTool{
			ID: call.ID, Name: call.Name, Arguments: call.Arguments, Provider: call.ProviderIdentifier,
		})
		fmt.Printf("[tool_call] id=%q name=%q provider=%q\n", call.ID, call.Name, call.ProviderIdentifier)
		fmt.Printf("            arguments=%s\n", call.Arguments)

	case cursor.AgentEventToolCallStarted:
		s.closeChannel()
		fmt.Printf("[tool_call_started] id=%q (one of Cursor's own agentic tools)\n", event.ToolCall.ID)

	case cursor.AgentEventToolCallArgs:
		s.closeChannel()
		fmt.Printf("[tool_call_args] id=%q delta=%q\n", event.ToolCall.ID, event.Text)

	case cursor.AgentEventTokenDelta:
		s.tokenSeen += usageOrZero(event.Usage).OutputTokens

	case cursor.AgentEventHeartbeat:
		// Counted only; printing every keep-alive would bury the answer.

	case cursor.AgentEventTurnEnded:
		s.closeChannel()
		s.usage = event.Usage
		fmt.Printf("[turn_ended] after %s\n", time.Since(s.start).Round(time.Millisecond))
		if event.Usage != nil {
			fmt.Printf("             input=%d output=%d cache_read=%d cache_write=%d\n",
				event.Usage.InputTokens, event.Usage.OutputTokens,
				event.Usage.CacheReadTokens, event.Usage.CacheWriteTokens)
		} else {
			fmt.Println("             no TurnEndedUpdate arrived; the stream ended without usage")
		}

	case cursor.AgentEventError:
		s.closeChannel()
		s.endErr = event.Err
		var agentErr *cursor.AgentError
		if errors.As(event.Err, &agentErr) {
			fmt.Printf("[error] code=%q message=%q http=%d\n", agentErr.Code, agentErr.Message, agentErr.HTTPStatus)
			fmt.Printf("        raw trailer json: %s\n", orNone(agentErr.Raw))
			return
		}
		fmt.Printf("[error] %v\n", event.Err)
	}
}

func usageOrZero(u *cursor.AgentUsage) cursor.AgentUsage {
	if u == nil {
		return cursor.AgentUsage{}
	}
	return *u
}

func printAgentSummary(state *agentState) {
	section("agent stream summary")
	kv("total elapsed", time.Since(state.start).Round(time.Millisecond))
	if state.haveFirst {
		kv("time to first event", state.firstEvent.Round(time.Millisecond))
	} else {
		kv("time to first event", "<no event received>")
	}
	kv("response frames", state.respFrames)
	kv("response bytes", state.respBytes)
	for _, typ := range []cursor.AgentEventType{
		cursor.AgentEventText, cursor.AgentEventThinking, cursor.AgentEventThinkingEnd,
		cursor.AgentEventToolCall, cursor.AgentEventToolCallStarted, cursor.AgentEventToolCallArgs,
		cursor.AgentEventTokenDelta, cursor.AgentEventHeartbeat,
		cursor.AgentEventTurnEnded, cursor.AgentEventError,
	} {
		kv(typ.String()+" events", state.counts[typ])
	}
	kv("assistant chars", state.text.Len())
	kv("thinking chars", state.thinking.Len())
	if state.tokenSeen > 0 {
		kv("token deltas summed", state.tokenSeen)
	}
	if state.usage != nil {
		kv("input tokens", state.usage.InputTokens)
		kv("output tokens", state.usage.OutputTokens)
		kv("cache read tokens", state.usage.CacheReadTokens)
		kv("cache write tokens", state.usage.CacheWriteTokens)
	}
	if state.endErr != nil {
		kv("stream error", state.endErr.Error())
	} else {
		kv("stream error", "<none>")
	}

	if state.text.Len() > 0 {
		section("assistant text (full)")
		fmt.Println(state.text.String())
	}
	if state.thinking.Len() > 0 {
		section("thinking (full)")
		fmt.Println(state.thinking.String())
	}
	if len(state.tools) > 0 {
		section("native tool calls")
		for i, tool := range state.tools {
			fmt.Printf("  #%d id=%q name=%q provider=%q\n", i, tool.ID, tool.Name, tool.Provider)
			fmt.Printf("      arguments: %s\n", tool.Arguments)
			if json.Valid([]byte(tool.Arguments)) {
				fmt.Println("      arguments parse as valid JSON")
			} else {
				fmt.Println("      arguments are NOT valid JSON")
			}
		}
	}
}

// -------------------------------------------------------------------------
// stream consumption
// -------------------------------------------------------------------------

// aggTool is one tool call reassembled across frames.
type aggTool struct {
	ID           string
	Name         string
	Args         string
	ArgsComplete bool
	IsLast       bool
	Frames       int
}

type streamState struct {
	start   time.Time
	channel string

	haveFirst  bool
	firstEvent time.Duration

	text     strings.Builder
	thinking strings.Builder

	textEvents  int
	thinkEvents int
	toolEvents  int
	frames      int
	dataFrames  int

	toolIndex map[string]int
	tools     []aggTool

	endReceived bool
	endErr      error
	endRaw      string
}

func newStreamState(start time.Time) *streamState {
	return &streamState{start: start, toolIndex: make(map[string]int)}
}

// switchChannel keeps interleaved text/thinking deltas readable: consecutive
// deltas of the same kind print inline, a change starts a new labelled block.
func (s *streamState) switchChannel(name string) {
	if s.channel == name {
		return
	}
	if s.channel != "" {
		fmt.Println()
	}
	if name != "" {
		fmt.Printf("--- %s ---\n", name)
	}
	s.channel = name
}

func (s *streamState) closeChannel() { s.switchChannel("") }

func (s *streamState) handle(ev *cursor.ChatEvent) {
	if ev == nil {
		return
	}
	if !s.haveFirst && ev.Type != cursor.ChatEventEnd {
		s.firstEvent = time.Since(s.start)
		s.haveFirst = true
	}

	switch ev.Type {
	case cursor.ChatEventText:
		s.textEvents++
		s.text.WriteString(ev.Text)
		s.switchChannel("assistant text")
		fmt.Print(ev.Text)

	case cursor.ChatEventThinking:
		s.thinkEvents++
		s.thinking.WriteString(ev.Text)
		s.switchChannel("thinking")
		fmt.Print(ev.Text)

	case cursor.ChatEventToolCall:
		s.toolEvents++
		s.closeChannel()
		tc := ev.ToolCall
		if tc == nil {
			fmt.Println("[tool_call] <event carried no call>")
			return
		}
		idx := s.mergeToolCall(tc)
		fmt.Printf("[tool_call #%d] id=%q name=%q is_last=%v args_complete=%v (mcp_params=%v)\n",
			idx, tc.ID, tc.Name, tc.IsLast, tc.ArgsComplete, tc.ArgsComplete)
		fmt.Printf("               raw_args=%q\n", tc.RawArgs)

	case cursor.ChatEventEnd:
		s.endReceived = true
		s.endErr = ev.Err
		s.closeChannel()
		if ev.Err == nil {
			fmt.Printf("[end] clean end-of-stream after %s\n", time.Since(s.start).Round(time.Millisecond))
			return
		}
		var streamErr *cursor.StreamError
		if errors.As(ev.Err, &streamErr) {
			fmt.Printf("[end] upstream error: code=%q message=%q\n", streamErr.Code, streamErr.Message)
			fmt.Printf("      raw error json: %s\n", streamErr.Raw)
			return
		}
		fmt.Printf("[end] error: %v\n", ev.Err)
	}
}

// mergeToolCall folds a delta into the aggregate call and returns its index.
func (s *streamState) mergeToolCall(tc *cursor.ToolCall) int {
	idx, ok := s.toolIndex[tc.ID]
	if !ok {
		idx = len(s.tools)
		s.toolIndex[tc.ID] = idx
		s.tools = append(s.tools, aggTool{ID: tc.ID})
	}
	agg := &s.tools[idx]
	agg.Frames++
	if tc.Name != "" {
		agg.Name = tc.Name
	}
	if tc.ArgsComplete {
		agg.Args = tc.RawArgs
		agg.ArgsComplete = true
	} else {
		agg.Args += tc.RawArgs
	}
	if tc.IsLast {
		agg.IsLast = true
	}
	return idx
}

// consumeDecoded is the production path: StreamDecoder turns frames straight
// into ChatEvents.
func consumeDecoded(r io.Reader, state *streamState) error {
	decoder := cursor.NewStreamDecoder(r)
	for {
		ev, err := decoder.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		state.handle(ev)
	}
}

// consumeRawFrames is the diagnostic path: every Connect frame is reported with
// its flag byte and size, and the trailer JSON is printed verbatim.
func consumeRawFrames(r io.Reader, state *streamState, dump bool) error {
	reader := cursor.NewFrameReader(r)
	for {
		frame, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("frame %d: %w", state.frames+1, err)
		}
		state.frames++
		state.closeChannel()
		fmt.Printf("[frame %d] flag=0x%02x compressed=%v end_stream=%v payload=%dB (+%s)\n",
			state.frames, frame.Flag, frame.Compressed, frame.EndStream,
			len(frame.Payload), time.Since(state.start).Round(time.Millisecond))

		if frame.EndStream {
			state.endReceived = true
			state.endRaw = strings.TrimSpace(string(frame.Payload))
			fmt.Printf("  trailer json: %s\n", orNone(state.endRaw))
			return nil
		}

		state.dataFrames++
		if dump {
			dumpProto("  ", frame.Payload, 0)
		}
		ev, parseErr := cursor.ParseChatResponseFrame(frame.Payload)
		if parseErr != nil {
			fmt.Printf("  [warn] parse frame: %v\n", parseErr)
			fmt.Println(hexPreview("  ", frame.Payload))
			continue
		}
		if ev == nil {
			fmt.Println("  <no user-visible delta in this frame>")
			continue
		}
		state.handle(ev)
	}
}

func printStreamSummary(state *streamState, total time.Duration, consumeErr error) {
	section("stream summary")
	kv("total elapsed", total.Round(time.Millisecond))
	if state.haveFirst {
		kv("time to first event", state.firstEvent.Round(time.Millisecond))
	} else {
		kv("time to first event", "<no event received>")
	}
	if state.frames > 0 {
		kv("frames read", state.frames)
		kv("data frames", state.dataFrames)
	}
	kv("text events", state.textEvents)
	kv("thinking events", state.thinkEvents)
	kv("tool_call events", state.toolEvents)
	kv("end frame received", state.endReceived)
	if state.endRaw != "" {
		kv("trailer json", state.endRaw)
	}
	switch {
	case consumeErr != nil:
		kv("stream error", consumeErr.Error())
	case state.endErr != nil:
		kv("stream error", state.endErr.Error())
		var streamErr *cursor.StreamError
		if errors.As(state.endErr, &streamErr) {
			kv("error code", orNone(streamErr.Code))
			kv("error message", orNone(streamErr.Message))
			kv("error raw json", orNone(streamErr.Raw))
		}
	default:
		kv("stream error", "<none>")
	}

	kv("assistant chars", state.text.Len())
	kv("thinking chars", state.thinking.Len())
	if state.text.Len() > 0 {
		section("assistant text (full)")
		fmt.Println(state.text.String())
	}
	if state.thinking.Len() > 0 {
		section("thinking (full)")
		fmt.Println(state.thinking.String())
	}
	if len(state.tools) > 0 {
		section("tool calls (aggregated)")
		for i, t := range state.tools {
			fmt.Printf("  #%d id=%q name=%q frames=%d is_last=%v args_complete=%v\n",
				i, t.ID, t.Name, t.Frames, t.IsLast, t.ArgsComplete)
			fmt.Printf("      arguments: %s\n", t.Args)
			if json.Valid([]byte(t.Args)) {
				fmt.Println("      arguments parse as valid JSON")
			} else {
				fmt.Println("      arguments are NOT valid JSON (possibly a partial stream)")
			}
		}
	}
}

// -------------------------------------------------------------------------
// output helpers
// -------------------------------------------------------------------------

func section(title string) {
	fmt.Printf("\n=== %s ===\n", title)
}

func kv(key string, value any) {
	fmt.Printf("  %-22s %v\n", key+":", value)
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<none>"
	}
	return s
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

// printBody renders an unexpected body verbatim when it is text, and as a hex
// dump plus a best-effort protobuf view when it is binary. It takes the full
// body and shortens it here, so display limits can never leak into parsing.
func printBody(body []byte) {
	if len(body) == 0 {
		fmt.Println("  <empty body>")
		return
	}
	shown, truncated := bodyPreview(body)
	if isMostlyText(body) {
		for _, line := range strings.Split(strings.TrimRight(string(shown), "\n"), "\n") {
			fmt.Println("  " + line)
		}
	} else {
		fmt.Println(indent("  ", hex.Dump(shown[:min(len(shown), 1024)])))
		// The field view walks the whole body: it is the only rendering that
		// needs complete input to line up field boundaries.
		fmt.Println("  best-effort protobuf view:")
		dumpProto("    ", body, 0)
	}
	if truncated {
		fmt.Printf("  <display truncated at %d of %d bytes>\n", len(shown), len(body))
	}
}

// dumpProto prints a schema-less view of a protobuf message using the package
// decoder, so an unexpected payload can still be inspected field by field.
func dumpProto(prefix string, data []byte, depth int) {
	if depth > 6 {
		fmt.Printf("%s<max depth reached>\n", prefix)
		return
	}
	fields, err := cursor.Decode(data)
	if err != nil {
		fmt.Printf("%s<not decodable as protobuf: %v>\n", prefix, err)
		return
	}
	numbers := make([]int, 0, len(fields))
	for number := range fields {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	for _, number := range numbers {
		for _, value := range fields[number] {
			if value.WireType != wireLen {
				fmt.Printf("%sfield %d: varint %d\n", prefix, number, value.Varint)
				continue
			}
			if isMostlyText(value.Bytes) {
				fmt.Printf("%sfield %d: string(%dB) %q\n", prefix, number, len(value.Bytes), truncate(string(value.Bytes), maxPreview))
				continue
			}
			if nested, nestedErr := cursor.Decode(value.Bytes); nestedErr == nil && len(nested) > 0 {
				fmt.Printf("%sfield %d: message(%dB)\n", prefix, number, len(value.Bytes))
				dumpProto(prefix+"  ", value.Bytes, depth+1)
				continue
			}
			fmt.Printf("%sfield %d: bytes(%dB) %x\n", prefix, number, len(value.Bytes),
				value.Bytes[:min(len(value.Bytes), 48)])
		}
	}
}

func hexPreview(prefix string, data []byte) string {
	return indent(prefix, hex.Dump(data[:min(len(data), 256)]))
}

func indent(prefix, block string) string {
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// isMostlyText reports whether data can be shown as-is: valid UTF-8 with only a
// negligible amount of control bytes.
func isMostlyText(data []byte) bool {
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	bad := 0
	for _, r := range string(data) {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
		case r < 0x20 || r == 0x7f:
			bad++
		}
	}
	return bad*20 <= len(data)
}
