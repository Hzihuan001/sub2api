package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
)

const firstContentPreludeLimit = 64 << 10

// Only an active, replay-safe TTFT attempt holds protocol preludes. No timer
// goroutine writes Gin state. Semantic content, tool starts, unknown protocol
// data or known usage commit irrevocably; last-resort groups never hold SSE.
type firstContentWriter struct {
	gin.ResponseWriter
	guard   *keyrouting.FirstContentGuard
	headers http.Header
	status  int
	hold    bool
	pending []byte
	frames  []byte
	observe func() bool
}

func newFirstContentWriter(w gin.ResponseWriter, guard *keyrouting.FirstContentGuard, hold bool) *firstContentWriter {
	return &firstContentWriter{ResponseWriter: w, guard: guard, headers: w.Header().Clone(), status: http.StatusOK, hold: hold, observe: guard.Observe}
}

func (w *firstContentWriter) Header() http.Header {
	if w.hold {
		return w.headers
	}
	return w.ResponseWriter.Header()
}
func (w *firstContentWriter) Status() int {
	if w.hold {
		return w.status
	}
	return w.ResponseWriter.Status()
}
func (w *firstContentWriter) Size() int {
	if w.hold {
		return -1
	}
	return w.ResponseWriter.Size()
}
func (w *firstContentWriter) Written() bool { return !w.hold && w.ResponseWriter.Written() }
func (w *firstContentWriter) WriteHeader(status int) {
	if w.hold {
		w.status = status
		return
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *firstContentWriter) WriteHeaderNow() {
	if !w.hold {
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *firstContentWriter) Flush() {
	if w.hold && w.guard.ReplayBlocked() {
		_ = w.commit()
	}
	if !w.hold {
		w.ResponseWriter.Flush()
	}
}
func (w *firstContentWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

func (w *firstContentWriter) commit() error {
	if !w.hold {
		return nil
	}
	for key := range w.ResponseWriter.Header() {
		delete(w.ResponseWriter.Header(), key)
	}
	for key, values := range w.headers {
		w.ResponseWriter.Header()[key] = append([]string(nil), values...)
	}
	w.ResponseWriter.WriteHeader(w.status)
	w.hold = false
	if len(w.pending) > 0 {
		_, err := w.ResponseWriter.Write(w.pending)
		w.pending = nil
		return err
	}
	return nil
}

func (w *firstContentWriter) Write(p []byte) (int, error) {
	// A final request builder or raw usage observer may have disarmed replay
	// before the native parser writes anything. Preserve native heartbeats;
	// there is no longer a safe backup attempt to buffer them for.
	if w.hold && w.guard.ReplayBlocked() {
		if err := w.commit(); err != nil {
			return 0, err
		}
	}
	if !w.hold {
		w.inspect(p)
		return w.ResponseWriter.Write(p)
	}
	if len(w.pending)+len(p) > firstContentPreludeLimit {
		// Bounded buffering is a safety limit, not permission to discard a
		// potentially billable result. Stop active cancellation and stream it.
		w.guard.PreventReplay()
		if err := w.commit(); err != nil {
			return 0, err
		}
		return w.ResponseWriter.Write(p)
	}
	w.pending = append(w.pending, p...)
	if w.inspect(p) {
		if err := w.commit(); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Finalize is called before Finish: normal completion flushes a harmless
// prelude/error; a safe expiry leaves the underlying writer entirely untouched.
func (w *firstContentWriter) Finalize(discard bool) {
	if discard {
		w.pending = nil
		w.frames = nil
		return
	}
	if w.hold {
		_ = w.commit()
	}
}

func (w *firstContentWriter) inspect(p []byte) bool {
	if w.Status() >= 400 {
		return false
	}
	if !strings.Contains(strings.ToLower(w.Header().Get("Content-Type")), "text/event-stream") {
		// A JSON result is already materialized. If it races expiry, keep it
		// rather than silently discard potentially charged upstream output.
		if len(bytes.TrimSpace(p)) > 0 {
			w.guard.PreventReplay()
			return w.observe()
		}
		return false
	}
	w.frames = append(w.frames, p...)
	if len(w.frames) > firstContentPreludeLimit {
		w.frames = nil
		w.guard.PreventReplay()
		return true
	}
	commit := false
	for {
		end := bytes.Index(w.frames, []byte("\n\n"))
		delimiter := 2
		if cr := bytes.Index(w.frames, []byte("\r\n\r\n")); cr >= 0 && (end < 0 || cr < end) {
			end = cr
			delimiter = 4
		}
		if end < 0 {
			return commit
		}
		frame := append([]byte(nil), w.frames[:end]...)
		w.frames = w.frames[end+delimiter:]
		semantic, irrevocable := firstContentFrameSignals(frame)
		if irrevocable {
			// Late semantic frames are an irrevocable result, even if the
			// timer won just before their write. Never launch duplicate work.
			w.guard.PreventReplay()
			commit = true
		}
		if semantic {
			w.observe()
			return true
		}
	}
}

func firstContentFrame(frame []byte) bool {
	semantic, _ := firstContentFrameSignals(frame)
	return semantic
}

// Billing metadata is irrevocable but is not the first useful model content.
// Unknown frames and terminal-only results are committed conservatively without
// inventing a fast TTFT sample. Body text, reasoning and tool starts are observed.
func firstContentFrameSignals(frame []byte) (semantic, irrevocable bool) {
	var lines []string
	for _, line := range strings.Split(string(frame), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "data:") {
			lines = append(lines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if len(lines) == 0 {
		return false, false
	}
	data := strings.Join(lines, "\n")
	if data == "[DONE]" {
		return false, true
	}
	var value map[string]any
	if json.Unmarshal([]byte(data), &value) != nil {
		return false, true
	} // unknown means not safely replayable
	irrevocable = firstContentHasUsage(value)
	if choices, ok := value["choices"].([]any); ok {
		for _, choice := range choices {
			item, _ := choice.(map[string]any)
			if item["finish_reason"] != nil {
				irrevocable = true
			}
			for _, field := range []string{"delta", "message"} {
				body, _ := item[field].(map[string]any)
				for _, key := range []string{"content", "reasoning_content", "reasoning", "refusal", "tool_calls", "function_call"} {
					if nonemptyProtocolValue(body[key]) {
						return true, true
					}
				}
			}
		}
		return false, irrevocable
	}
	kind, _ := value["type"].(string)
	switch kind {
	case "ping", "message_start", "response.created", "response.in_progress", "response.queued", "response.output_item.added", "response.content_part.added", "content_block_start":
		if item, ok := value["item"].(map[string]any); ok {
			if typ, _ := item["type"].(string); typ != "message" && typ != "reasoning" {
				return true, true
			}
		}
		if block, ok := value["content_block"].(map[string]any); ok {
			if block["type"] != "text" && block["type"] != "thinking" {
				return true, true
			}
			semantic = nonemptyProtocolValue(block["text"]) || nonemptyProtocolValue(block["thinking"])
			return semantic, semantic || irrevocable
		}
		return false, irrevocable
	case "content_block_delta":
		delta, _ := value["delta"].(map[string]any)
		for _, key := range []string{"text", "thinking", "partial_json", "signature"} {
			if nonemptyProtocolValue(delta[key]) {
				return true, true
			}
		}
		return false, irrevocable
	case "response.failed", "error":
		return false, irrevocable
	case "response.output_text.delta", "response.refusal.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta":
		semantic = nonemptyProtocolValue(value["delta"])
		return semantic, semantic || irrevocable
	case "":
		if value["error"] != nil {
			return false, irrevocable
		}
		return false, true
	default:
		// Terminal events and unknown protocol extensions disarm but do not
		// pretend to be the first useful content.
		return false, true
	}
}

func firstContentHasUsage(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, item := range current {
			if key == "usage" {
				if protocolUsageNonzero(item) {
					return true
				}
			}
			if nested, ok := item.(map[string]any); ok && firstContentHasUsage(nested) {
				return true
			}
		}
	}
	return false
}
func protocolUsageNonzero(value any) bool {
	switch current := value.(type) {
	case float64:
		return current > 0
	case map[string]any:
		for _, item := range current {
			if protocolUsageNonzero(item) {
				return true
			}
		}
	case []any:
		for _, item := range current {
			if protocolUsageNonzero(item) {
				return true
			}
		}
	}
	return false
}
func nonemptyProtocolValue(value any) bool {
	switch current := value.(type) {
	case string:
		return current != ""
	case []any:
		return len(current) > 0
	case map[string]any:
		return len(current) > 0
	}
	return false
}
