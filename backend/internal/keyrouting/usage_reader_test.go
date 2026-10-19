package keyrouting

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type smallUsageChunks struct {
	value string
	step  int
	end   error
}

func (r *smallUsageChunks) Read(p []byte) (int, error) {
	if r.value == "" {
		return 0, r.end
	}
	n := min(len(p), r.step, len(r.value))
	copy(p, r.value[:n])
	r.value = r.value[n:]
	return n, nil
}
func (r *smallUsageChunks) Close() error { return nil }

func TestProtectUsageReaderProtocolUsagePreventsReplayBeforeParser(t *testing.T) {
	for _, payload := range []string{
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":{\"input_tokens\":5}}}\n\n",
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5}}\r\n\r\n",
		"event: message_start\ndata:{\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5}}}\n\n",
		"data: {\"usageMetadata\":{\"promptTokenCount\":5}}\n\n",
		"data: {\"response\":\ndata: {\"usage\":{\"input_tokens\":5}}}\n\n",
		`{"usage":{"prompt_tokens_details":{"cached_tokens":"5"}}}`,
	} {
		for _, chunkSize := range []int{1, 7, len(payload)} {
			guard := NewFirstContentGuard(time.Hour)
			ctx := WithFirstContentGuard(context.Background(), guard)
			guard.Detach(ctx)
			reader := ProtectUsageReader(ctx, &smallUsageChunks{value: payload, step: chunkSize, end: io.EOF})
			actual, err := io.ReadAll(reader)
			if err != nil || string(actual) != payload {
				t.Fatalf("observer changed protocol bytes/errors: err=%v actual=%q", err, actual)
			}
			// Simulate a native parser discarding its partial result and returning
			// nil,error. Protection cannot depend on that result surviving.
			guard.expire()
			if guard.Expired() || guard.ReplaySafe() {
				t.Fatalf("known raw usage allowed cancellation or replay: chunks=%d payload=%q", chunkSize, payload)
			}
			guard.Finish()
		}
	}
}

func TestProtectUsageReaderIgnoresContentAndZeroUsage(t *testing.T) {
	for _, payload := range []string{
		": ping\n\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n",
		"data: {\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\n",
		"data: {\"delta\":{\"text\":\"usage input_tokens 5\"}}\n\n",
		`{"response":{"output":[{"content":[{"usage":{"input_tokens":5}}]}]}}`,
		`{"message":{"content":"usage 5"},"metadata":{"usage":{"input_tokens":5}}}`,
	} {
		guard := NewFirstContentGuard(time.Hour)
		ctx := WithFirstContentGuard(context.Background(), guard)
		guard.Detach(ctx)
		_, err := io.ReadAll(ProtectUsageReader(ctx, io.NopCloser(strings.NewReader(payload))))
		if err != nil {
			t.Fatal(err)
		}
		guard.expire()
		if !guard.ReplaySafe() {
			t.Fatalf("zero usage or user content unexpectedly disabled replay: %q", payload)
		}
		guard.Finish()
	}
}

func TestProtectUsageReaderBoundedAndErrorsUnchanged(t *testing.T) {
	for _, prefix := range []string{"data: ", `{"padding":"`} {
		guard := NewFirstContentGuard(time.Hour)
		ctx := WithFirstContentGuard(context.Background(), guard)
		guard.Detach(ctx)
		payload := prefix + strings.Repeat("x", firstContentUsageObserverLimit*3)
		originalErr := errors.New("original upstream read error")
		reader := ProtectUsageReader(ctx, &smallUsageChunks{value: payload, step: 7, end: originalErr})
		actual, err := io.ReadAll(reader)
		if err != originalErr || string(actual) != payload {
			t.Fatal("bounded observer changed bytes or original read error")
		}
		wrapped := reader.(*firstContentUsageReader)
		if !wrapped.done || len(wrapped.line) != 0 || len(wrapped.data) != 0 {
			t.Fatal("oversized metadata must stop observation and release its bounded prefix")
		}
		guard.expire()
		if guard.ReplaySafe() {
			t.Fatal("uninspectable large metadata must conservatively block replay")
		}
		guard.Finish()
	}
	plain := io.NopCloser(strings.NewReader("unchanged"))
	if ProtectUsageReader(context.Background(), plain) != plain || ProtectUsageReader(context.Background(), nil) != nil {
		t.Fatal("unguarded reader was wrapped")
	}
}
