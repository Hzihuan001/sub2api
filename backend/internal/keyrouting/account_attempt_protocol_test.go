package keyrouting

import (
	"strings"
	"testing"
)

func TestAccountAttemptPreludeProofStrictAllowlist(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, data string
		sse, safe            bool
	}{
		{name: "zero bytes before cancellation", endpoint: "/v1/responses", safe: true},
		{name: "SSE heartbeat", endpoint: "/v1/chat/completions", sse: true, data: ": ping\n\n", safe: true},
		{name: "Gemini comment not guessed JSON", endpoint: "/v1beta/models/g:streamGenerateContent", sse: true, data: ": keep-alive\n\n", safe: true},
		{name: "chat no choices", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[]}\n\n", safe: true},
		{name: "chat role only", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"\"},\"finish_reason\":null}]}\n\n", safe: true},
		{name: "response created", endpoint: "/v1/responses", sse: true, data: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[],\"usage\":null}}\n\n", safe: true},
		{name: "response in progress", endpoint: "/v1/responses", sse: true, data: "data: {\"type\":\"response.in_progress\",\"sequence_number\":1,\"response\":{\"status\":\"in_progress\",\"output\":[],\"reasoning\":{\"effort\":\"low\",\"summary\":null}}}\n\n", safe: true},
		{name: "Anthropic zero usage", endpoint: "/v1/messages", sse: true, data: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}\n\n", safe: true},
		{name: "Anthropic ping", endpoint: "/v1/messages", sse: true, data: "event: ping\ndata: {\"type\":\"ping\"}\n\n", safe: true},
		{name: "non SSE body", endpoint: "/v1/chat/completions", data: "{}"},
		{name: "partial comment", endpoint: "/v1/chat/completions", sse: true, data: ": ping"},
		{name: "partial JSON", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":"},
		{name: "event without delimiter", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[]}\n"},
		{name: "empty data event is not heartbeat", endpoint: "/v1/chat/completions", sse: true, data: "data:\n\n"},
		{name: "unknown event", endpoint: "/v1/chat/completions", sse: true, data: "event: mystery\ndata: {\"choices\":[]}\n\n"},
		{name: "bad JSON", endpoint: "/v1/chat/completions", sse: true, data: "data: {bad}\n\n"},
		{name: "unknown fields", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[],\"new_output\":\"hidden\"}\n\n"},
		{name: "chat content", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[{\"delta\":{\"content\":\"text\"}}]}\n\n"},
		{name: "chat reasoning", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thought\"}}]}\n\n"},
		{name: "chat tool arguments", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{}]}}]}\n\n"},
		{name: "chat finish", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"},
		{name: "positive usage", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[],\"usage\":{\"input_tokens\":1}}\n\n"},
		{name: "nonzero usage must not underflow to zero", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[],\"usage\":{\"input_tokens\":1e-400}}\n\n"},
		{name: "exact signed zero usage", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[],\"usage\":{\"input_tokens\":-0.00e-400}}\n\n", safe: true},
		{name: "duplicate content must not hide output", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[{\"delta\":{\"content\":\"paid\",\"content\":\"\"}}]}\n\n"},
		{name: "escaped duplicate names must not hide usage", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[],\"usage\":{\"input_tokens\":7,\"input_\\u0074okens\":0}}\n\n"},
		{name: "duplicate choices must not hide output", endpoint: "/v1/chat/completions", sse: true, data: "data: {\"choices\":[{\"delta\":{\"content\":\"paid\"}}],\"choices\":[]}\n\n"},
		{name: "DONE means completion", endpoint: "/v1/chat/completions", sse: true, data: "data: [DONE]\n\n"},
		{name: "response output", endpoint: "/v1/responses", sse: true, data: "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[{}]}}\n\n"},
		{name: "response reasoning content", endpoint: "/v1/responses", sse: true, data: "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[],\"reasoning\":{\"summary\":[{\"text\":\"hidden\"}]}}}\n\n"},
		{name: "response unknown field", endpoint: "/v1/responses", sse: true, data: "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[],\"new_output\":[]}}\n\n"},
		{name: "Anthropic positive usage", endpoint: "/v1/messages", sse: true, data: "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1}}}\n\n"},
		{name: "Gemini JSON not guessed", endpoint: "/v1beta/models/g:streamGenerateContent", sse: true, data: "data: {}\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, split := range []bool{false, true} {
				proof := NewAccountAttemptPreludeProof(tc.endpoint, tc.sse)
				if split {
					for _, b := range []byte(tc.data) {
						proof.Observe([]byte{b})
					}
				} else {
					proof.Observe([]byte(tc.data))
				}
				if proof.Safe() != tc.safe {
					t.Fatalf("safe=%v want=%v split=%v", proof.Safe(), tc.safe, split)
				}
			}
		})
	}
}

func TestAccountAttemptPreludeProofUnknownAndOversizeNeverRecover(t *testing.T) {
	for _, bad := range []string{"data: {bad}\n\n", strings.Repeat("x", accountAttemptPreludeLimit+1)} {
		proof := NewAccountAttemptPreludeProof("/v1/chat/completions", true)
		proof.Observe([]byte(bad))
		proof.Observe([]byte(": ping\n\ndata: {\"choices\":[]}\n\n"))
		if proof.Safe() {
			t.Fatal("a later safe prelude reopened an unknown prefix")
		}
		if len(proof.line)+len(proof.data) > accountAttemptPreludeLimit {
			t.Fatal("proof retained unbounded input")
		}
	}
}
