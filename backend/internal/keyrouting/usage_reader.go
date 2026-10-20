package keyrouting

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
)

const firstContentUsageObserverLimit = 64 * 1024

// ProtectUsageReader observes raw upstream usage before a native parser can
// buffer metadata or discard an error result. It never reads ahead, changes
// bytes/errors, writes a response, or performs billing. Uninspectably large
// prefixes conservatively disable replay instead of retaining unbounded data.
func ProtectUsageReader(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	guard := GetFirstContentGuard(ctx)
	if guard == nil || body == nil {
		return body
	}
	if wrapped, ok := body.(*firstContentUsageReader); ok && wrapped.guard == guard {
		return body
	}
	return &firstContentUsageReader{ReadCloser: body, guard: guard, attempt: GetAccountAttempt(ctx)}
}

type firstContentUsageReader struct {
	io.ReadCloser
	guard   *FirstContentGuard
	attempt *AccountAttempt
	mode    byte // j: JSON; s: SSE; zero: no non-whitespace byte yet.
	line    []byte
	data    []byte
	done    bool
}

func (r *firstContentUsageReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 && !r.done {
		r.observe(p[:n])
	}
	if err != nil && !r.done {
		if r.mode == 's' {
			r.observeSSELine(r.line)
			r.inspect(r.data)
		} else {
			r.inspect(r.line)
		}
	}
	return n, err
}

func (r *firstContentUsageReader) protect() {
	r.guard.PreventReplay()
	r.done = true
	r.line = nil
	r.data = nil
}

func (r *firstContentUsageReader) observe(p []byte) {
	for _, b := range p {
		if r.done {
			return
		}
		if r.mode == 0 {
			if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
				continue
			}
			if b == '{' || b == '[' {
				r.mode = 'j'
			} else {
				r.mode = 's'
			}
		}
		if len(r.line) >= firstContentUsageObserverLimit {
			r.protect()
			return
		}
		if r.mode == 's' && b == '\n' {
			r.observeSSELine(r.line)
			r.line = r.line[:0]
			continue
		}
		r.line = append(r.line, b)
	}
	if r.mode == 'j' && !r.done {
		r.inspect(r.line)
	}
}

func (r *firstContentUsageReader) observeSSELine(line []byte) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		r.inspect(r.data)
		r.data = r.data[:0]
		return
	}
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	payload := bytes.TrimPrefix(line, []byte("data:"))
	if len(payload) > 0 && payload[0] == ' ' {
		payload = payload[1:]
	}
	if len(r.data)+len(payload)+1 > firstContentUsageObserverLimit {
		r.protect()
		return
	}
	if len(r.data) > 0 {
		r.data = append(r.data, '\n')
	}
	r.data = append(r.data, payload...)
	// Standard single-data-line events can contain known usage before the
	// blank event terminator. Also support legal multi-line JSON data events.
	r.inspect(r.data)
}

func (r *firstContentUsageReader) inspect(payload []byte) {
	if r.done || !json.Valid(payload) {
		return
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(payload, &root) != nil {
		return
	}
	for _, key := range []string{"usage", "usageMetadata"} {
		if usageHasPositiveNumber(root[key]) {
			if r.attempt != nil {
				r.attempt.ObserveUsage()
			}
			r.protect()
			return
		}
	}
	for _, key := range []string{"response", "message"} {
		var envelope map[string]json.RawMessage
		if json.Unmarshal(root[key], &envelope) == nil && usageHasPositiveNumber(envelope["usage"]) {
			if r.attempt != nil {
				r.attempt.ObserveUsage()
			}
			r.protect()
			return
		}
	}
}

func usageHasPositiveNumber(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	return usageValueHasPositiveNumber(value)
}

func usageValueHasPositiveNumber(value any) bool {
	switch v := value.(type) {
	case json.Number:
		number, err := v.Float64()
		return err == nil && number > 0
	case string:
		// Some compatible providers encode token counts as numeric strings.
		number, err := strconv.ParseFloat(v, 64)
		return err == nil && number > 0
	case map[string]any:
		for _, nested := range v {
			if usageValueHasPositiveNumber(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range v {
			if usageValueHasPositiveNumber(nested) {
				return true
			}
		}
	}
	return false
}
