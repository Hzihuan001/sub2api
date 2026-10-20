package keyrouting

import (
	"bytes"
	"encoding/json"
	"strings"
)

// AccountAttemptPreludeProof is a bounded, conservative replay proof, not a
// provider parser or a source of billing/output. Only complete allowlisted SSE
// preludes can prove that already-read bytes contain no effective content.
// Any unknown field/event, incomplete prefix or non-SSE body fails closed.
type AccountAttemptPreludeProof struct {
	endpoint  string
	sse       bool
	invalid   bool
	line      []byte
	data      []byte
	event     string
	dataSeen  bool
	eventSeen bool
}

const accountAttemptPreludeLimit = 64 * 1024

func NewAccountAttemptPreludeProof(endpoint string, sse bool) *AccountAttemptPreludeProof {
	return &AccountAttemptPreludeProof{endpoint: endpoint, sse: sse}
}

func (p *AccountAttemptPreludeProof) Observe(data []byte) {
	if p == nil || p.invalid || len(data) == 0 {
		return
	}
	if !p.sse {
		p.invalidate()
		return
	}
	for _, b := range data {
		if p.invalid {
			return
		}
		if b == '\n' {
			p.observeLine(bytes.TrimSuffix(p.line, []byte{'\r'}))
			p.line = p.line[:0]
			continue
		}
		if len(p.line)+len(p.data)+len(p.event) >= accountAttemptPreludeLimit {
			p.invalidate()
			return
		}
		p.line = append(p.line, b)
	}
}

// Safe is meaningful only together with a proven local TTFT cancellation,
// never a normal EOF or an empty successful response. Zero bytes are safe;
// pending SSE fields and partial JSON are deliberately not.
func (p *AccountAttemptPreludeProof) Safe() bool {
	return p != nil && !p.invalid && len(p.line) == 0 && len(p.data) == 0 && !p.dataSeen && !p.eventSeen
}

func (p *AccountAttemptPreludeProof) invalidate() {
	p.invalid, p.line, p.data, p.event = true, nil, nil, ""
}

func (p *AccountAttemptPreludeProof) observeLine(line []byte) {
	if len(line) == 0 {
		if p.dataSeen {
			if !p.safeEvent() {
				p.invalidate()
				return
			}
		} else if p.eventSeen {
			p.invalidate()
			return
		}
		p.data, p.event = p.data[:0], ""
		p.dataSeen, p.eventSeen = false, false
		return
	}
	if line[0] == ':' { // An entire SSE comment line has no payload.
		return
	}
	field, value, found := bytes.Cut(line, []byte{':'})
	if !found {
		p.invalidate()
		return
	}
	value = bytes.TrimPrefix(value, []byte{' '})
	switch string(field) {
	case "event":
		if p.eventSeen {
			p.invalidate()
			return
		}
		p.event = string(value)
		p.eventSeen = true
	case "data":
		if p.dataSeen {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, value...)
		p.dataSeen = true
	default:
		p.invalidate()
	}
}

func (p *AccountAttemptPreludeProof) safeEvent() bool {
	var root map[string]json.RawMessage
	if json.Unmarshal(p.data, &root) != nil || root == nil || !uniquePreludeJSONMembers(p.data) {
		return false
	}
	switch {
	case strings.HasSuffix(p.endpoint, "/chat/completions"):
		return (p.event == "" || p.event == "message") && safeChatPrelude(root)
	case strings.HasSuffix(p.endpoint, "/responses"):
		return safeResponsesPrelude(p.event, root)
	case strings.HasSuffix(p.endpoint, "/messages"):
		return safeAnthropicPrelude(p.event, root)
	default:
		// Gemini has no assumed JSON heartbeat contract. SSE comments remain
		// valid, but unknown provider events never become proof by guessing.
		return false
	}
}

// Do not prove replay safety using last-key-wins while a native parser might
// consume the first member. Duplicate names at any depth are ambiguous, even
// when the final decoded value looks like an empty delta or zero usage.
func uniquePreludeJSONMembers(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		if depth > 128 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return true
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				token, err := decoder.Token()
				name, stringName := token.(string)
				if err != nil || !stringName {
					return false
				}
				if _, duplicate := seen[name]; duplicate {
					return false
				}
				seen[name] = struct{}{}
				if !value(depth + 1) {
					return false
				}
			}
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
		default:
			return false
		}
		end, err := decoder.Token()
		return err == nil && (delimiter == '{' && end == json.Delim('}') || delimiter == '[' && end == json.Delim(']'))
	}
	// safeEvent already validated a single complete JSON object, so Token's
	// recursion needs only enforce unambiguous member names and bounded depth.
	return value(0)
}

func onlyPreludeKeys(object map[string]json.RawMessage, keys ...string) bool {
	for key := range object {
		allowed := false
		for _, candidate := range keys {
			if key == candidate {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

func preludeNull(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func preludeEmptyArray(raw json.RawMessage) bool {
	var values []json.RawMessage
	return !preludeNull(raw) && json.Unmarshal(raw, &values) == nil && values != nil && len(values) == 0
}

func preludeString(raw json.RawMessage, expected string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == expected
}

func preludeZeroUsage(raw json.RawMessage) bool {
	if preludeNull(raw) {
		return true
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	if _, ok := value.(map[string]any); !ok {
		return false
	}
	var zero func(any) bool
	zero = func(value any) bool {
		switch v := value.(type) {
		case nil:
			return true
		case json.Number:
			// Float conversion could underflow a real nonzero amount to zero.
			// The decoder already checked JSON number syntax; inspect the exact
			// mantissa instead. An exponent never changes a zero mantissa.
			mantissa := string(v)
			if exponent := strings.IndexAny(mantissa, "eE"); exponent >= 0 {
				mantissa = mantissa[:exponent]
			}
			for _, digit := range mantissa {
				if digit != '0' && digit != '-' && digit != '.' {
					return false
				}
			}
			return true
		case map[string]any:
			for _, nested := range v {
				if !zero(nested) {
					return false
				}
			}
			return true
		default:
			return false
		}
	}
	return zero(value)
}

func safeChatPrelude(root map[string]json.RawMessage) bool {
	if !onlyPreludeKeys(root, "id", "object", "created", "model", "choices", "usage", "system_fingerprint", "service_tier") ||
		!preludeZeroUsage(root["usage"]) || !preludeNull(root["object"]) && !preludeString(root["object"], "chat.completion.chunk") {
		return false
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(root["choices"], &choices) != nil || choices == nil {
		return false
	}
	for _, choice := range choices {
		if !onlyPreludeKeys(choice, "index", "delta", "finish_reason", "logprobs") || !preludeNull(choice["finish_reason"]) || !preludeNull(choice["logprobs"]) {
			return false
		}
		var delta map[string]json.RawMessage
		if json.Unmarshal(choice["delta"], &delta) != nil || delta == nil ||
			!onlyPreludeKeys(delta, "role", "content", "refusal", "reasoning", "reasoning_content", "tool_calls", "function_call") {
			return false
		}
		if !preludeNull(delta["role"]) && !preludeString(delta["role"], "assistant") {
			return false
		}
		for _, key := range []string{"content", "refusal", "reasoning", "reasoning_content"} {
			if !preludeNull(delta[key]) && !preludeString(delta[key], "") {
				return false
			}
		}
		if !preludeNull(delta["tool_calls"]) && !preludeEmptyArray(delta["tool_calls"]) || !preludeNull(delta["function_call"]) {
			return false
		}
	}
	return true
}

func safeResponsesPrelude(event string, root map[string]json.RawMessage) bool {
	var kind string
	if json.Unmarshal(root["type"], &kind) != nil || kind != "response.created" && kind != "response.in_progress" || event != "" && event != kind ||
		!onlyPreludeKeys(root, "type", "response", "sequence_number") {
		return false
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(root["response"], &response) != nil || response == nil || !preludeEmptyArray(response["output"]) ||
		!preludeNull(response["usage"]) || !preludeNull(response["error"]) || !preludeNull(response["completed_at"]) ||
		!preludeNull(response["incomplete_details"]) || !preludeNull(response["output_text"]) && !preludeString(response["output_text"], "") {
		return false
	}
	if !preludeString(response["status"], "in_progress") && !preludeString(response["status"], "queued") {
		return false
	}
	if !preludeNull(response["reasoning"]) {
		var reasoning map[string]json.RawMessage
		if json.Unmarshal(response["reasoning"], &reasoning) != nil || reasoning == nil ||
			!onlyPreludeKeys(reasoning, "effort", "summary") || !preludeNull(reasoning["summary"]) {
			return false
		}
	}
	return onlyPreludeKeys(response, "id", "object", "created_at", "status", "background", "error", "incomplete_details", "instructions", "max_output_tokens", "max_tool_calls", "model", "output", "parallel_tool_calls", "previous_response_id", "prompt_cache_key", "prompt_cache_retention", "reasoning", "safety_identifier", "service_tier", "store", "temperature", "text", "tool_choice", "tools", "top_logprobs", "top_p", "truncation", "usage", "user", "metadata", "completed_at", "output_text")
}

func safeAnthropicPrelude(event string, root map[string]json.RawMessage) bool {
	var kind string
	if json.Unmarshal(root["type"], &kind) != nil || event != "" && event != kind {
		return false
	}
	if kind == "ping" {
		return onlyPreludeKeys(root, "type")
	}
	if kind != "message_start" || !onlyPreludeKeys(root, "type", "message") {
		return false
	}
	var message map[string]json.RawMessage
	return json.Unmarshal(root["message"], &message) == nil && message != nil &&
		onlyPreludeKeys(message, "id", "type", "role", "model", "content", "stop_reason", "stop_sequence", "usage") &&
		preludeString(message["type"], "message") && preludeString(message["role"], "assistant") &&
		preludeEmptyArray(message["content"]) && preludeNull(message["stop_reason"]) && preludeNull(message["stop_sequence"]) && preludeZeroUsage(message["usage"])
}
