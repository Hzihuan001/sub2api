package cursor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The frame-to-event loop is exercised directly against an in-memory body.
// Nothing here opens a socket: the transport is covered separately, and the
// decoding rules are what a protocol change would break.
func newReaderStream(body io.Reader, opts AgentStreamOptions) *AgentStream {
	s := &AgentStream{
		opts:   opts.resolved(),
		body:   body,
		events: make(chan AgentEvent, agentEventBuffer),
		start:  time.Now(),
		cancel: func() {},
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	// The watchdog and the tool-call drain both stop a blocking read by closing
	// the body, which only works if the stream owns a closer, as it does in
	// production (resp.Body).
	if closer, ok := body.(io.Closer); ok {
		s.closer = closer
	}
	// OpenAgentStream starts the clock before the first read; without it a
	// watchdog started by a test would see an epoch-old last activity.
	s.touch()
	return s
}

func drainEvents(t *testing.T, s *AgentStream) []AgentEvent {
	t.Helper()
	go s.readResponseFrames()

	var events []AgentEvent
	timeout := time.After(5 * time.Second)
	for {
		select {
		case event, ok := <-s.events:
			if !ok {
				return events
			}
			events = append(events, event)
		case <-timeout:
			t.Fatal("timed out waiting for the event stream to close")
		}
	}
}

// dataFrame wraps a payload the way the upstream would.
func dataFrame(payload []byte) []byte { return EncodeFrame(payload, false) }

// trailerFrame builds an end-of-stream frame (flag 0x02, JSON payload).
func trailerFrame(json string) []byte { return encodeRawFrame(flagEndStream, []byte(json)) }

func textFrame(text string) []byte {
	return dataFrame(nest(stringField(fieldAgentDeltaText, text),
		fieldAgentServerInteractionUpdate, fieldAgentUpdateTextDelta))
}

// thinkingFrame is a reasoning delta: output, but not the answer.
func thinkingFrame(text string) []byte {
	return dataFrame(nest(stringField(fieldAgentDeltaText, text),
		fieldAgentServerInteractionUpdate, fieldAgentUpdateThinkingDelta))
}

// heartbeatFrame is the server keep-alive, an update with an empty body.
func heartbeatFrame() []byte {
	return dataFrame(nest(nil, fieldAgentServerInteractionUpdate, fieldAgentUpdateHeartbeat))
}

func turnEndedFrame(inputTokens, outputTokens int64) []byte {
	var turn Writer
	turn.WriteInt64(fieldAgentTurnInputTokens, inputTokens)
	turn.WriteInt64(fieldAgentTurnOutputTokens, outputTokens)
	return dataFrame(nest(turn.Bytes(), fieldAgentServerInteractionUpdate, fieldAgentUpdateTurnEnded))
}

func TestAgentStreamEmitsTextThenTurnEnd(t *testing.T) {
	body := bytes.NewReader(bytes.Join([][]byte{
		textFrame("Hel"),
		textFrame("lo"),
		trailerFrame("{}"),
	}, nil))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{}))
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(events), events)
	}

	var text strings.Builder
	for _, event := range events[:2] {
		if event.Type != AgentEventText {
			t.Fatalf("event = %s, want text", event.Type)
		}
		text.WriteString(event.Text)
	}
	if text.String() != "Hello" {
		t.Errorf("assembled text = %q, want %q", text.String(), "Hello")
	}
	if events[2].Type != AgentEventTurnEnded {
		t.Errorf("final event = %s, want turn_ended", events[2].Type)
	}
}

// A trailer carrying an error must terminate the turn with that error, not with
// a clean end that would look like an empty answer.
func TestAgentStreamSurfacesTrailerError(t *testing.T) {
	body := bytes.NewReader(bytes.Join([][]byte{
		textFrame("partial"),
		trailerFrame(`{"error":{"code":"permission_denied","message":"client version too old"}}`),
	}, nil))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{}))
	last := events[len(events)-1]
	if last.Type != AgentEventError {
		t.Fatalf("final event = %s, want error", last.Type)
	}

	var agentErr *AgentError
	if !errors.As(last.Err, &agentErr) {
		t.Fatalf("error = %v, want an *AgentError", last.Err)
	}
	if agentErr.Code != "permission_denied" {
		t.Errorf("code = %q", agentErr.Code)
	}
	if agentErr.HTTPStatus != http.StatusForbidden {
		t.Errorf("status = %d, want 403", agentErr.HTTPStatus)
	}
}

// The upstream frequently closes the body instead of sending a trailer.
func TestAgentStreamTreatsCleanEOFAsEnd(t *testing.T) {
	events := drainEvents(t, newReaderStream(bytes.NewReader(textFrame("hi")), AgentStreamOptions{}))
	if len(events) != 2 || events[1].Type != AgentEventTurnEnded {
		t.Fatalf("events = %+v, want text then turn_ended", events)
	}
}

// A native tool call ends the turn by default: the model is now blocked waiting
// for an exec result this client never sends, so reading on would only stall.
func TestAgentStreamEndsTurnOnToolCall(t *testing.T) {
	call := mcpArgsPayload(t, "", "get_weather", "call-1", map[string]any{"location": "Beijing"})
	body := bytes.NewReader(bytes.Join([][]byte{
		dataFrame(call),
		textFrame("never read"),
	}, nil))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{}))
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1 (the stream must stop at the tool call): %+v", len(events), events)
	}
	if events[0].Type != AgentEventToolCall || events[0].ToolCall.Name != "get_weather" {
		t.Errorf("event = %+v, want a get_weather tool call", events[0])
	}
}

// Parallel tool calls arrive as separate frames a few milliseconds apart.
// Stopping at the first one turned a three-call turn into a one-call turn, so
// the reader collects the siblings before it ends the stream.
func TestAgentStreamDrainsSiblingToolCalls(t *testing.T) {
	frames := make([][]byte, 0, 3)
	for _, name := range []string{"read_file", "list_dir", "grep"} {
		frames = append(frames, dataFrame(mcpArgsPayload(t, "", name, "call-"+name, nil)))
	}
	body := bytes.NewReader(bytes.Join(frames, nil))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{
		// Long enough that the window cannot expire before the frames are read,
		// short enough that a regression does not hang the suite.
		ToolCallDrainWindow: 2 * time.Second,
	}))

	names := make([]string, 0, len(events))
	for _, event := range events {
		if event.Type == AgentEventToolCall {
			names = append(names, event.ToolCall.Name)
		}
	}
	if len(names) != 3 {
		t.Fatalf("tool calls = %v, want all three: %+v", names, events)
	}
	// The body runs out inside the window, which is a clean end, not a fault.
	last := events[len(events)-1]
	if last.Type != AgentEventTurnEnded {
		t.Errorf("final event = %s, want turn_ended", last.Type)
	}
}

// The window is a ceiling, not a wait: a turn that only ever produces one call
// must not sit on the stream once the window expires.
func TestAgentStreamToolCallDrainWindowBoundsTheWait(t *testing.T) {
	call := mcpArgsPayload(t, "", "get_weather", "call-1", nil)
	body := &blockingBody{prefix: bytes.NewReader(dataFrame(call)), unblock: make(chan struct{})}

	start := time.Now()
	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{ToolCallDrainWindow: 150 * time.Millisecond}))
	elapsed := time.Since(start)

	if elapsed > 3*time.Second {
		t.Fatalf("drain took %s; the window did not bound the wait", elapsed)
	}
	if len(events) != 2 || events[0].Type != AgentEventToolCall || events[1].Type != AgentEventTurnEnded {
		t.Fatalf("events = %+v, want tool_call then turn_ended", events)
	}
}

// blockingBody serves a fixed prefix and then blocks, mimicking an upstream
// that keeps the response open while it waits for an exec result. Close is what
// releases the read, exactly as closing the real response body does.
type blockingBody struct {
	prefix  *bytes.Reader
	unblock chan struct{}
	once    sync.Once
}

func (b *blockingBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	<-b.unblock
	return 0, io.EOF
}

func (b *blockingBody) Close() error {
	b.once.Do(func() { close(b.unblock) })
	return nil
}

func TestAgentStreamKeepReadingAfterToolCall(t *testing.T) {
	call := mcpArgsPayload(t, "", "get_weather", "call-1", nil)
	body := bytes.NewReader(bytes.Join([][]byte{
		dataFrame(call),
		textFrame("and more"),
		trailerFrame("{}"),
	}, nil))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{KeepReadingAfterToolCall: true}))
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %+v", len(events), events)
	}
	if events[1].Type != AgentEventText || events[1].Text != "and more" {
		t.Errorf("second event = %+v, want the trailing text", events[1])
	}
}

// turn_ended is a terminal event: anything after it belongs to no turn.
func TestAgentStreamStopsAtTurnEnded(t *testing.T) {
	body := bytes.NewReader(bytes.Join([][]byte{
		turnEndedFrame(10, 20),
		textFrame("after the end"),
	}, nil))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{}))
	if len(events) != 1 || events[0].Type != AgentEventTurnEnded {
		t.Fatalf("events = %+v, want a single turn_ended", events)
	}
	if events[0].Usage == nil || events[0].Usage.InputTokens != 10 || events[0].Usage.OutputTokens != 20 {
		t.Errorf("usage = %+v", events[0].Usage)
	}
}

// The idle budget measures silence on the wire, so every frame has to reset it
// — including ones that carry no answer text. A reasoning model emits one
// thinking delta and then deliberates behind heartbeats; when only text counted
// as activity (or the budget was a few seconds long) the watchdog cut the turn
// off mid-thought and the answer never arrived.
func TestAgentStreamIdleBudgetResetsOnEveryFrame(t *testing.T) {
	const (
		idleBudget = 500 * time.Millisecond
		frameGap   = 150 * time.Millisecond
	)

	pr, pw := io.Pipe()
	s := newReaderStream(pr, AgentStreamOptions{IdleTimeout: idleBudget})
	// Closing the read side is how the watchdog would interrupt the reader,
	// standing in for the response body it closes against a live upstream.
	s.closer = pr

	// Paced so no single gap reaches the budget while the whole turn runs well
	// past it: the turn only survives if each frame pushes the deadline out.
	go func() {
		defer func() { _ = pw.Close() }()
		frames := [][]byte{
			thinkingFrame("weighing the options"),
			heartbeatFrame(), heartbeatFrame(), heartbeatFrame(), heartbeatFrame(),
			textFrame("the answer"),
			trailerFrame("{}"),
		}
		for _, frame := range frames {
			time.Sleep(frameGap)
			if _, err := pw.Write(frame); err != nil {
				return
			}
		}
	}()

	go s.watchdog()
	events := drainEvents(t, s)

	if s.timedOut.Load() {
		t.Fatal("the idle watchdog fired while the upstream was still sending frames")
	}
	var text strings.Builder
	for _, event := range events {
		if event.Type == AgentEventText {
			text.WriteString(event.Text)
		}
	}
	if text.String() != "the answer" {
		t.Errorf("text = %q, want %q (the turn was cut short)", text.String(), "the answer")
	}
	if last := events[len(events)-1]; last.Type != AgentEventTurnEnded {
		t.Errorf("final event = %s, want turn_ended: %+v", last.Type, events)
	}
}

// An end the upstream states outright is honoured on the spot. The idle budget
// is only the safety net for a stream that stopped talking without saying so,
// so it must not be what a finished turn is waiting on.
func TestAgentStreamExplicitEndBeatsIdleBudget(t *testing.T) {
	const idleBudget = 200 * time.Millisecond

	bodies := map[string][]byte{
		"turn_ended update":   bytes.Join([][]byte{thinkingFrame("hm"), turnEndedFrame(10, 20)}, nil),
		"end-of-stream frame": bytes.Join([][]byte{thinkingFrame("hm"), trailerFrame("{}")}, nil),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			s := newReaderStream(bytes.NewReader(body), AgentStreamOptions{IdleTimeout: idleBudget})
			s.closer = io.NopCloser(strings.NewReader(""))
			go s.watchdog()

			events := drainEvents(t, s)
			if last := events[len(events)-1]; last.Type != AgentEventTurnEnded {
				t.Fatalf("final event = %s, want turn_ended: %+v", last.Type, events)
			}

			// Well past the budget: a watchdog still armed here would mean the
			// turn ended on a timer rather than on the upstream's word.
			time.Sleep(idleBudget + 2*agentWatchdogTick)
			if s.timedOut.Load() {
				t.Error("the idle watchdog fired after the upstream ended the turn")
			}
		})
	}
}

func TestAgentStreamReportsMalformedFrame(t *testing.T) {
	// A frame whose payload is not a decodable protobuf message.
	body := bytes.NewReader(dataFrame([]byte{0x0a, 0x7f, 0x01}))

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{}))
	if len(events) != 1 || events[0].Type != AgentEventError {
		t.Fatalf("events = %+v, want a single error", events)
	}
}

// A truncated body must not be reported as a clean end, which would silently
// hand back a partial answer.
func TestAgentStreamReportsTruncatedFrame(t *testing.T) {
	full := textFrame("hello")
	body := bytes.NewReader(full[:len(full)-2])

	events := drainEvents(t, newReaderStream(body, AgentStreamOptions{}))
	if len(events) != 1 || events[0].Type != AgentEventError {
		t.Fatalf("events = %+v, want a single error", events)
	}
	if !strings.Contains(events[0].Err.Error(), "unexpected EOF") {
		t.Errorf("error = %v, want an unexpected-EOF read failure", events[0].Err)
	}
}

func TestIsAgentOutput(t *testing.T) {
	// This only picks which budget applies — first-byte until the model has
	// produced something, idle afterwards. A server heartbeat must not make a
	// turn that never started look productive, even though it does keep the
	// idle deadline moving once one has.
	for typ, want := range map[AgentEventType]bool{
		AgentEventText:            true,
		AgentEventThinking:        true,
		AgentEventToolCall:        true,
		AgentEventToolCallStarted: true,
		AgentEventHeartbeat:       false,
		AgentEventTokenDelta:      false,
		AgentEventTurnEnded:       false,
	} {
		if got := isAgentOutput(typ); got != want {
			t.Errorf("isAgentOutput(%s) = %v, want %v", typ, got, want)
		}
	}
}

func TestOpenAgentStreamRequiresToken(t *testing.T) {
	_, err := OpenAgentStream(context.Background(), AgentRunParams{Prompt: "hi"}, AgentStreamOptions{})
	if err == nil {
		t.Fatal("expected an error when no token is configured")
	}
}

// The agent path is fronted by a load balancer that drops HTTP/1.1 with an
// empty 464, so a connection that did not negotiate h2 has to fail loudly
// rather than look like a hang later.
//
// The guard is checked directly on a synthetic response because an HTTP/1.1
// server cannot even get this far: Go's HTTP/1 client withholds the response
// until the request body finishes writing, and this body stays open for the
// life of the turn. That is the same dead end the real transport would hit.
func TestAcceptResponseRequiresHTTP2(t *testing.T) {
	http1Response := func() *http.Response {
		return &http.Response{
			Status: "200 OK", StatusCode: http.StatusOK,
			Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{},
			Body:   io.NopCloser(strings.NewReader("")),
		}
	}

	err := newReaderStream(nil, AgentStreamOptions{}).acceptResponse(http1Response())
	if err == nil {
		t.Fatal("expected an HTTP/2 requirement error")
	}
	if !strings.Contains(err.Error(), "HTTP/2") {
		t.Errorf("error = %v, want it to name the HTTP/2 requirement", err)
	}

	if err := newReaderStream(nil, AgentStreamOptions{AllowHTTP1: true}).acceptResponse(http1Response()); err != nil {
		t.Errorf("AllowHTTP1 must waive the check, got %v", err)
	}
}

// newAgentTestServer starts a loopback HTTP/2 server. Real h2 is required to
// exercise the transport at all, since the whole design rests on writing the
// request while reading the response.
func newAgentTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func TestOpenAgentStreamSurfacesUpstreamStatus(t *testing.T) {
	server := newAgentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		go func() { _, _ = io.Copy(io.Discard, r.Body) }()
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"unauthenticated","message":"bad token"}}`))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := OpenAgentStream(ctx, AgentRunParams{Prompt: "hi"}, AgentStreamOptions{
		BaseURL:    server.URL,
		Token:      "test-token",
		HTTPClient: server.Client(),
	})
	var agentErr *AgentError
	if !errors.As(err, &agentErr) {
		t.Fatalf("error = %v, want an *AgentError", err)
	}
	if agentErr.Code != "unauthenticated" || agentErr.HTTPStatus != http.StatusUnauthorized {
		t.Errorf("error = %+v, want unauthenticated/401", agentErr)
	}
}

// End to end over loopback h2: the response is read back while the request
// frames are still being paced out. That concurrency is the whole transport.
func TestOpenAgentStreamReadsResponseWhileWriting(t *testing.T) {
	handlerEntered := make(chan struct{})
	server := newAgentTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		close(handlerEntered)
		// The client keeps writing for the life of the turn, so the body is
		// drained in the background and never waited on.
		go func() { _, _ = io.Copy(io.Discard, r.Body) }()

		w.Header().Set("content-type", ContentTypeConnectProto)
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("test server response is not flushable")
			return
		}
		_, _ = w.Write(textFrame("hi there"))
		flusher.Flush()
		_, _ = w.Write(trailerFrame("{}"))
		flusher.Flush()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	frames := make(chan AgentFrameInfo, 32)
	stream, err := OpenAgentStream(ctx, AgentRunParams{Prompt: "hi", MessageID: "m", ConversationID: "c"}, AgentStreamOptions{
		BaseURL:    server.URL,
		Token:      "test-token",
		HTTPClient: server.Client(),
		OnRequestFrame: func(info AgentFrameInfo) {
			select {
			case frames <- info:
			default:
			}
		},
	})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if got := stream.Response().ProtoMajor; got != 2 {
		t.Fatalf("negotiated HTTP/%d, want HTTP/2", got)
	}

	var text strings.Builder
	var ended bool
	for event := range stream.Events() {
		switch event.Type {
		case AgentEventText:
			text.WriteString(event.Text)
		case AgentEventTurnEnded:
			ended = true
		case AgentEventError:
			t.Fatalf("unexpected stream error: %v", event.Err)
		}
	}
	if text.String() != "hi there" {
		t.Errorf("text = %q, want %q", text.String(), "hi there")
	}
	if !ended {
		t.Error("stream did not end cleanly")
	}

	<-handlerEntered
	first := <-frames
	if first.Label != "run_request" {
		t.Errorf("first request frame = %q, want run_request", first.Label)
	}
	if first.FrameBytes != first.PayloadBytes+5 {
		t.Errorf("frame accounting = %+v, want payload plus the 5-byte envelope", first)
	}
	// Only the first two frames can have been sent by the time a fast server
	// answers: the rest are still behind their pacing delays. Reading a
	// complete response before the request stream is done is exactly the
	// full-duplex behaviour HTTP/1.1 cannot provide.
	if len(frames) > len(BuildRunFrameSequence(AgentRunParams{Prompt: "hi"}))-1 {
		t.Errorf("request stream finished before the response was read: %d frames queued", len(frames)+1)
	}
}

// Close must be safe to call at any point and must not hang, including after
// the stream has already finished on its own.
func TestAgentStreamCloseIsIdempotent(t *testing.T) {
	s := newReaderStream(bytes.NewReader(trailerFrame("{}")), AgentStreamOptions{})
	s.closer = io.NopCloser(strings.NewReader(""))
	drainEvents(t, s)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.Close()
		_ = s.Close()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close blocked")
	}
}
