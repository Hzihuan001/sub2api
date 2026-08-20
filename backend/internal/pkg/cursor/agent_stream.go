package cursor

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Bidirectional transport for agent.v1.AgentService/Run.
//
// This is the part that has no counterpart in the api2 client. api2's chat RPC
// is server-streaming: one request message, then read until the trailer. Run is
// *bidirectional* — the client must keep writing while it reads, and the pacing
// is load-bearing rather than cosmetic:
//
//   - Half-closing the request stream before the server has produced its answer
//     fails the turn with "internal: No exec result". So the frame sequence is
//     dribbled out over several seconds and heartbeats continue afterwards.
//   - HTTP/2 is mandatory. The agent path is fronted by an AWS ALB that only
//     serves h2; an HTTP/1.1 request is rejected with an empty 464 before Cursor
//     ever sees it, which looks like a silent hang rather than an error. The
//     stream therefore refuses to proceed over HTTP/1.1 unless AllowHTTP1 is set.
//   - The server keeps the response side open after the assistant message when
//     it expects a tool exec-result. This client never sends one, so a turn is
//     also finished by an idle timeout when no explicit end ever arrives.

const (
	// AgentDefaultFirstByteTimeout bounds the wait for the first response
	// frame. Generation can take a while to start, so the budget is generous.
	AgentDefaultFirstByteTimeout = 60 * time.Second

	// AgentDefaultIdleTimeout ends a turn once output has gone quiet. It exists
	// because the server will not close a stream on which it is still waiting
	// for an exec result.
	//
	// A turn the upstream ends itself never reaches this budget, so a generous
	// one costs nothing and a short one is dangerous: it has to outlast the
	// longest silence a healthy turn contains, which is a reasoning model
	// emitting one thinking delta and then deliberating. At a few seconds those
	// turns were cut off mid-thought and only succeeded on retry.
	AgentDefaultIdleTimeout = 30 * time.Second

	// agentWatchdogTick is how often the idle/first-byte budget is re-checked.
	agentWatchdogTick = 250 * time.Millisecond

	// AgentDefaultToolCallDrainWindow is how long the reader keeps going after
	// the first native tool call when KeepReadingAfterToolCall is off.
	//
	// A model that decides to call three tools emits them as three frames a few
	// milliseconds apart. Returning on the first one dropped its siblings, so a
	// parallel tool turn reached the client as a single call. The window is
	// deliberately short: nothing useful follows a tool call other than more of
	// them, and the turn must not sit here waiting for an exec result that this
	// client never sends.
	AgentDefaultToolCallDrainWindow = 400 * time.Millisecond

	// agentEventBuffer keeps the reader a little ahead of a slow consumer.
	agentEventBuffer = 32

	// agentErrorBodyLimit caps how much of a non-2xx body is read for the
	// error message.
	agentErrorBodyLimit = 64 << 10
)

// AgentFrameInfo describes one frame as it crosses the wire. It exists for
// probes and logging; nothing in the protocol depends on it.
type AgentFrameInfo struct {
	// Index is 1-based within its direction.
	Index int
	// Label names a request frame (FramePlan.Label); empty for responses.
	Label string
	// PayloadBytes is the protobuf body size and FrameBytes adds the 5-byte
	// envelope. For a gzipped response frame both count the *decoded* payload,
	// since that is what the reader hands back.
	PayloadBytes int
	FrameBytes   int
	// DelayAfter is the pause scheduled after a request frame.
	DelayAfter time.Duration
	// Elapsed is the time since the turn was opened.
	Elapsed time.Duration
}

// AgentStreamOptions configures one Run turn.
type AgentStreamOptions struct {
	// BaseURL defaults to DefaultAgentBaseURL.
	BaseURL string
	// Token is the subscription session credential, bare JWT or "userId::JWT".
	// No official crsr_ API key is needed: AgentService accepts the same
	// credential the deep-link exchange mints.
	Token string
	// ClientVersion defaults to DefaultCLIClientVersion.
	ClientVersion string
	// GhostMode sets x-ghost-mode. The captured CLI sends true; this is a plain
	// bool rather than a defaulted one because privacy mode should be an
	// explicit decision at the call site.
	GhostMode bool
	// RequestID pins x-request-id / x-original-request-id. Empty mints a uuid.
	RequestID string

	// HTTPClient defaults to NewAgentHTTPClient(). A caller-supplied client
	// must negotiate HTTP/2 and must not buffer request bodies.
	HTTPClient *http.Client

	// FirstByteTimeout and IdleTimeout default to the Agent* constants above.
	FirstByteTimeout time.Duration
	IdleTimeout      time.Duration
	// HeartbeatInterval defaults to AgentHeartbeatInterval.
	HeartbeatInterval time.Duration

	// KeepReadingAfterToolCall continues the stream past a native MCP tool
	// call. The default (false) ends the turn there, because the model is now
	// blocked waiting for an exec result this client never sends.
	KeepReadingAfterToolCall bool

	// ToolCallDrainWindow defaults to AgentDefaultToolCallDrainWindow and only
	// applies when KeepReadingAfterToolCall is off: it bounds the extra time
	// spent collecting sibling tool calls from the same turn.
	ToolCallDrainWindow time.Duration

	// AllowHTTP1 disables the HTTP/2 requirement. Only useful for tests against
	// a local server; against the real upstream an HTTP/1.1 request is dropped
	// by the load balancer.
	AllowHTTP1 bool

	// OnRequestFrame and OnResponseFrame observe traffic for diagnostics.
	OnRequestFrame  func(AgentFrameInfo)
	OnResponseFrame func(AgentFrameInfo, *Frame)
}

func (o AgentStreamOptions) resolved() AgentStreamOptions {
	if strings.TrimSpace(o.BaseURL) == "" {
		o.BaseURL = DefaultAgentBaseURL
	}
	if strings.TrimSpace(o.ClientVersion) == "" {
		o.ClientVersion = DefaultCLIClientVersion
	}
	if o.HTTPClient == nil {
		o.HTTPClient = NewAgentHTTPClient()
	}
	if o.FirstByteTimeout <= 0 {
		o.FirstByteTimeout = AgentDefaultFirstByteTimeout
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = AgentDefaultIdleTimeout
	}
	if o.HeartbeatInterval <= 0 {
		o.HeartbeatInterval = AgentHeartbeatInterval
	}
	if o.ToolCallDrainWindow <= 0 {
		o.ToolCallDrainWindow = AgentDefaultToolCallDrainWindow
	}
	return o
}

// NewAgentHTTPClient builds a client suitable for the agent transport: HTTP/2
// via ALPN, no whole-body compression, and no client-level timeout so the
// context governs the turn.
//
// The standard transport is used rather than golang.org/x/net/http2's directly
// (which is also a module dependency) because http2.Transport has no proxy
// support; ForceAttemptHTTP2 gets the same h2 connection through the stdlib's
// bundled copy while keeping HTTP_PROXY working. The resulting connection must
// still negotiate h2 — OpenAgentStream verifies that on the response.
func NewAgentHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			ForceAttemptHTTP2: true,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			// Per-frame gzip is negotiated with connect-accept-encoding, so
			// whole-body compression is declined: it would make the transport
			// buffer the response and defeat streaming.
			DisableCompression:  true,
			TLSHandshakeTimeout: 20 * time.Second,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// AgentStream is one open Run turn. Response headers have arrived; the request
// stream is still open and being fed by a background goroutine.
type AgentStream struct {
	opts   AgentStreamOptions
	resp   *http.Response
	body   io.Reader
	closer io.Closer

	events chan AgentEvent
	start  time.Time

	cancel   context.CancelFunc
	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	pr       *io.PipeReader
	pw       *io.PipeWriter

	lastActivity atomic.Int64
	gotOutput    atomic.Bool
	timedOut     atomic.Bool
	drained      atomic.Bool
}

// OpenAgentStream starts a turn and returns once the response headers arrive. A
// non-2xx status, a failed transport, or a connection that did not negotiate
// HTTP/2 is reported here rather than as a stream event.
func OpenAgentStream(ctx context.Context, params AgentRunParams, opts AgentStreamOptions) (*AgentStream, error) {
	opts = opts.resolved()
	if strings.TrimSpace(opts.Token) == "" {
		return nil, errors.New("cursor: agent stream needs a token")
	}

	reqCtx, cancel := context.WithCancel(ctx)
	pr, pw := io.Pipe()

	s := &AgentStream{
		opts:   opts,
		events: make(chan AgentEvent, agentEventBuffer),
		start:  time.Now(),
		cancel: cancel,
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		pr:     pr,
		pw:     pw,
	}
	s.touch()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, AgentRunURL(opts.BaseURL), pr)
	if err != nil {
		s.abort()
		return nil, fmt.Errorf("cursor: build agent request: %w", err)
	}
	// An unknown-length body is mandatory: a Content-Length would force the
	// transport to buffer the whole request before sending, which cannot work
	// for a stream that stays open for the life of the turn.
	req.ContentLength = -1
	for name, values := range BuildAgentHeaders(opts.Token, opts.ClientVersion, opts.GhostMode, opts.RequestID) {
		for _, value := range values {
			req.Header.Set(name, value)
		}
	}

	go s.writeRequestFrames(params)

	resp, err := s.awaitResponse(req)
	if err != nil {
		s.abort()
		return nil, err
	}
	if err := s.acceptResponse(resp); err != nil {
		s.abort()
		_ = resp.Body.Close()
		return nil, err
	}

	go s.readResponseFrames()
	go s.watchdog()
	return s, nil
}

// agentDoResult carries an in-flight client.Do outcome back to awaitResponse.
type agentDoResult struct {
	resp *http.Response
	err  error
}

// awaitResponse waits for headers under the first-byte budget. client.Do blocks
// until the server responds, and with an open request body that could otherwise
// be the whole context deadline.
func (s *AgentStream) awaitResponse(req *http.Request) (*http.Response, error) {
	ch := make(chan agentDoResult, 1)
	go func() {
		resp, err := s.opts.HTTPClient.Do(req)
		ch <- agentDoResult{resp, err}
	}()

	timer := time.NewTimer(s.opts.FirstByteTimeout)
	defer timer.Stop()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, fmt.Errorf("cursor: agent request failed: %w", res.err)
		}
		return res.resp, nil
	case <-req.Context().Done():
		s.discardLateResponse(ch)
		return nil, fmt.Errorf("cursor: agent request cancelled: %w", req.Context().Err())
	case <-timer.C:
		// Cancelling the request context unblocks the in-flight Do.
		s.cancel()
		s.discardLateResponse(ch)
		return nil, fmt.Errorf("cursor: no response headers within %s", s.opts.FirstByteTimeout)
	}
}

// discardLateResponse drains the pending Do so a response that arrives after we
// gave up still gets its body closed.
func (s *AgentStream) discardLateResponse(ch <-chan agentDoResult) {
	go func() {
		if res := <-ch; res.resp != nil {
			_ = res.resp.Body.Close()
		}
	}()
}

// acceptResponse validates status and protocol, then installs the body reader.
func (s *AgentStream) acceptResponse(resp *http.Response) error {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, agentErrorBodyLimit))
		if agentErr := ParseAgentTrailer(body); agentErr != nil && agentErr.Code != "" {
			return agentErr
		}
		return &AgentError{
			Message:    fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(body))),
			Raw:        strings.TrimSpace(string(body)),
			HTTPStatus: resp.StatusCode,
		}
	}
	if resp.ProtoMajor < 2 && !s.opts.AllowHTTP1 {
		return fmt.Errorf("cursor: agent stream needs HTTP/2 but negotiated %s; "+
			"the upstream load balancer drops HTTP/1.1 requests with an empty 464", resp.Proto)
	}

	s.resp = resp
	s.closer = resp.Body
	s.body = resp.Body
	// The transport is told not to accept whole-body compression, but a proxy
	// or a caller-supplied client may still deliver one.
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("content-encoding")), "gzip") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("cursor: gzip agent response: %w", err)
		}
		s.body = gr
	}
	return nil
}

// Events returns the event channel. It is closed when the turn ends; the final
// event is an AgentEventTurnEnded or an AgentEventError.
func (s *AgentStream) Events() <-chan AgentEvent { return s.events }

// Response exposes the response for header inspection. Its Body must not be
// read directly.
func (s *AgentStream) Response() *http.Response { return s.resp }

// Close ends the turn early and releases everything. It is safe to call more
// than once and after the stream has finished on its own.
func (s *AgentStream) Close() error {
	s.signalStop()
	if s.closer != nil {
		_ = s.closer.Close()
	}
	<-s.done
	return nil
}

// abort tears down a turn that never got as far as a readable response. Closing
// the pipe's read side is what actually stops the writer: it may be blocked in
// a Write that no longer has a reader, where the stop channel cannot reach it.
func (s *AgentStream) abort() {
	s.signalStop()
	_ = s.pr.Close()
	close(s.done)
}

func (s *AgentStream) signalStop() {
	s.stopOnce.Do(func() {
		close(s.stop)
		s.cancel()
	})
}

func (s *AgentStream) touch() { s.lastActivity.Store(time.Now().UnixNano()) }

func (s *AgentStream) idleFor() time.Duration {
	return time.Since(time.Unix(0, s.lastActivity.Load()))
}

// writeRequestFrames plays out the paced frame sequence and then keeps the
// request stream open with heartbeats. Closing the pipe writer half-closes the
// request body, which is exactly what must not happen too early.
func (s *AgentStream) writeRequestFrames(params AgentRunParams) {
	defer func() { _ = s.pw.Close() }()

	index := 0
	write := func(label string, payload []byte, delay time.Duration) bool {
		index++
		frame := EncodeFrame(payload, false)
		if _, err := s.pw.Write(frame); err != nil {
			return false
		}
		if s.opts.OnRequestFrame != nil {
			s.opts.OnRequestFrame(AgentFrameInfo{
				Index:        index,
				Label:        label,
				PayloadBytes: len(payload),
				FrameBytes:   len(frame),
				DelayAfter:   delay,
				Elapsed:      time.Since(s.start),
			})
		}
		return s.pause(delay)
	}

	for _, plan := range BuildRunFrameSequence(params) {
		if !write(plan.Label, plan.Payload, plan.DelayAfter) {
			return
		}
	}

	heartbeat := EncodeClientHeartbeat()
	ticker := time.NewTicker(s.opts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if !write("client_heartbeat", heartbeat, 0) {
				return
			}
		}
	}
}

// pause waits out a frame's delay, returning false if the turn ended meanwhile.
func (s *AgentStream) pause(d time.Duration) bool {
	if d <= 0 {
		select {
		case <-s.stop:
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-s.stop:
		return false
	case <-timer.C:
		return true
	}
}

// readResponseFrames turns the response frame stream into events. It owns the
// events channel and closes it on exit.
func (s *AgentStream) readResponseFrames() {
	defer close(s.done)
	defer close(s.events)
	defer s.signalStop()

	// Only set once the first tool call has been seen; see the drain comment at
	// the bottom of the loop.
	var drainTimer *time.Timer
	defer func() {
		if drainTimer != nil {
			drainTimer.Stop()
		}
	}()

	fr := NewFrameReader(s.body)
	for index := 1; ; index++ {
		frame, err := fr.Next()
		if err != nil {
			s.emitTerminal(err)
			return
		}
		// Every frame is activity, not just assistant text: a thinking delta, a
		// token_delta and a heartbeat all say the upstream is alive. The idle
		// budget measures silence on the wire, so it is reset here — before the
		// payload is even looked at — rather than per event type.
		s.touch()
		if s.opts.OnResponseFrame != nil {
			s.opts.OnResponseFrame(AgentFrameInfo{
				Index:        index,
				PayloadBytes: len(frame.Payload),
				FrameBytes:   len(frame.Payload) + 5,
				Elapsed:      time.Since(s.start),
			}, frame)
		}

		// An explicit end is honoured on the spot rather than left to the idle
		// budget: returning here retires the watchdog through signalStop.
		if frame.EndStream {
			if agentErr := ParseAgentTrailer(frame.Payload); agentErr != nil {
				s.emit(AgentEvent{Type: AgentEventError, Err: agentErr})
				return
			}
			s.emit(AgentEvent{Type: AgentEventTurnEnded})
			return
		}

		event, err := ParseAgentServerMessage(frame.Payload)
		if err != nil {
			s.emit(AgentEvent{Type: AgentEventError, Err: fmt.Errorf("cursor: parse agent frame %d: %w", index, err)})
			return
		}
		if event == nil {
			continue
		}
		// While draining after a tool call, anything that is not another tool
		// call means the turn moved on. It is dropped rather than forwarded,
		// which is exactly what returning at the first call used to do.
		if drainTimer != nil && !isAgentToolCallEvent(event.Type) && event.Type != AgentEventTurnEnded {
			return
		}
		if isAgentOutput(event.Type) {
			s.gotOutput.Store(true)
		}
		if !s.emit(*event) {
			return
		}
		if event.Type == AgentEventTurnEnded {
			return
		}
		// A native tool call ends the assistant's turn: the model is now
		// waiting for an exec result on the stream. A stateless bridge surfaces
		// the call and re-runs with the result in history instead.
		//
		// Siblings from the same turn arrive as their own frames right behind
		// the first one, so the reader stays for ToolCallDrainWindow to collect
		// them. It stops itself by closing the body out from under the blocking
		// read, the same lever the watchdog uses; the resulting read error is
		// then reported as a clean end rather than a fault.
		if event.Type == AgentEventToolCall && !s.opts.KeepReadingAfterToolCall && drainTimer == nil {
			drainTimer = time.AfterFunc(s.opts.ToolCallDrainWindow, func() {
				s.drained.Store(true)
				if s.closer != nil {
					_ = s.closer.Close()
				}
			})
			continue
		}
	}
}

// isAgentToolCallEvent reports whether an event belongs to a tool call, which
// is what the post-tool-call drain window is allowed to keep collecting.
func isAgentToolCallEvent(t AgentEventType) bool {
	switch t {
	case AgentEventToolCall, AgentEventToolCallStarted, AgentEventToolCallArgs:
		return true
	default:
		return false
	}
}

// isAgentOutput reports whether an event counts as upstream progress, which
// switches the watchdog from the first-byte budget to the idle budget.
func isAgentOutput(t AgentEventType) bool {
	switch t {
	case AgentEventText, AgentEventThinking, AgentEventThinkingEnd,
		AgentEventToolCall, AgentEventToolCallStarted, AgentEventToolCallArgs:
		return true
	default:
		return false
	}
}

// emitTerminal decides the last event when the frame reader stops.
func (s *AgentStream) emitTerminal(err error) {
	switch {
	case errors.Is(err, io.EOF):
		// A clean end on a frame boundary. The upstream often closes this way
		// instead of sending a trailer.
		s.emit(AgentEvent{Type: AgentEventTurnEnded})

	case s.drained.Load():
		// The tool-call drain window closed the body on purpose. The calls
		// already emitted are the turn's result.
		s.emit(AgentEvent{Type: AgentEventTurnEnded})

	case s.timedOut.Load() && s.gotOutput.Load():
		// Expected: the server holds the response open waiting for an exec
		// result that never comes. The output already received is the answer.
		s.emit(AgentEvent{Type: AgentEventTurnEnded})

	case s.timedOut.Load():
		s.emit(AgentEvent{Type: AgentEventError, Err: fmt.Errorf(
			"cursor: upstream sent no output within %s", s.opts.FirstByteTimeout)})

	case s.stopped():
		// Close() was called; the read error is a consequence, not a fault.

	default:
		s.emit(AgentEvent{Type: AgentEventError, Err: fmt.Errorf("cursor: read agent stream: %w", err)})
	}
}

func (s *AgentStream) stopped() bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

// emit delivers an event, or reports false when the turn was stopped and no
// consumer will read it.
func (s *AgentStream) emit(event AgentEvent) bool {
	select {
	case s.events <- event:
		return true
	case <-s.stop:
		return false
	}
}

// watchdog enforces the first-byte and idle budgets. The frame reader blocks in
// a read that only a closed body can interrupt, so the budget is applied by
// closing the body out from under it.
//
// The idle budget is a last resort for a stream that will never speak again. A
// turn the upstream ends itself never reaches it: the reader returns first and
// signalStop retires this goroutine.
func (s *AgentStream) watchdog() {
	ticker := time.NewTicker(agentWatchdogTick)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-s.stop:
			return
		case <-ticker.C:
			budget := s.opts.FirstByteTimeout
			if s.gotOutput.Load() {
				budget = s.opts.IdleTimeout
			}
			if s.idleFor() < budget {
				continue
			}
			s.timedOut.Store(true)
			if s.closer != nil {
				_ = s.closer.Close()
			}
			return
		}
	}
}
