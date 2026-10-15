//go:build unit

package middleware

import (
	"bytes"
	"strings"
	"testing"
)

func TestKeyRoutingJSONUnambiguousRejectsConflictingParsers(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"duplicate model", `{"model":"allowed","model":"blocked"}`},
		{"same duplicate model", `{"model":"same","model":"same"}`},
		{"case folded model", `{"model":"allowed","Model":"blocked"}`},
		{"escaped model", `{"model":"allowed","\u006dodel":"blocked"}`},
		{"duplicate tools", `{"model":"gpt-x","tools":[],"tools":[{"type":"image_generation"}]}`},
		{"duplicate nested type", `{"input":[{"type":"message","type":"item_reference","id":"msg_saved"}]}`},
		{"case folded nested type", `{"input":[{"type":"message","Type":"item_reference"}]}`},
		{"duplicate ordinary field", `{"metadata":{"foo":"a","foo":"b"}}`},
		{"unicode folded session", `{"session":{},"\u017fession":{}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if keyRoutingJSONUnambiguous([]byte(tt.body)) {
				t.Fatal("ambiguous JSON was accepted")
			}
		})
	}
	for _, field := range []string{"model", "session", "tools", "tool_choice", "type", "input", "context_management", "previous_response_id", "conversation", "background"} {
		t.Run("semantic casing "+field, func(t *testing.T) {
			body := `{"` + field + `":null,"` + strings.ToUpper(field) + `":null}`
			if keyRoutingJSONUnambiguous([]byte(body)) {
				t.Fatalf("case-folded duplicate %q was accepted", field)
			}
		})
	}
}

func TestKeyRoutingJSONUnambiguousPreservesValidInput(t *testing.T) {
	for _, body := range []string{
		`{}`,
		` {"model":"gpt-x","input":"hello","background":false} `,
		`{"tools":[{"type":"function","parameters":{"type":"object","properties":{"foo":{"type":"string"},"Foo":{"type":"number"}}}}]}`,
		`{"metadata":{"foo":"a","Foo":"b"},"input":[{"type":"message"},{"type":"message"}]}`,
		`{"Model":"gpt-x","arbitrary":1e9999,"values":[null,true,false,"value",123]}`,
	} {
		t.Run(body, func(t *testing.T) {
			input := []byte(body)
			before := append([]byte(nil), input...)
			if !keyRoutingJSONUnambiguous(input) {
				t.Fatal("valid unambiguous JSON was rejected")
			}
			if !bytes.Equal(before, input) {
				t.Fatal("validation rewrote the input body")
			}
		})
	}
}

func TestKeyRoutingJSONUnambiguousRejectsMalformedOrNonObjectInput(t *testing.T) {
	for _, body := range []string{"", " ", "[]", "null", "true", `"text"`, "123", "{", `{"a":}`, `{"a":1,}`, `{"a":[}`, `{"a":01}`, "{}{}", "{} null", "{} trailing", `{"a":"\x"}`} {
		t.Run(body, func(t *testing.T) {
			if keyRoutingJSONUnambiguous([]byte(body)) {
				t.Fatal("malformed or non-object JSON was accepted")
			}
		})
	}
	if keyRoutingJSONUnambiguous([]byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}) {
		t.Fatal("invalid UTF-8 JSON was accepted")
	}
}

func TestKeyRoutingJSONUnambiguousBoundsDepth(t *testing.T) {
	for _, container := range []struct{ open, close string }{{`{"a":`, `}`}, {`[`, `]`}} {
		atLimit := `{"root":` + strings.Repeat(container.open, keyRoutingMaxJSONDepth-1) + `null` + strings.Repeat(container.close, keyRoutingMaxJSONDepth-1) + `}`
		if !keyRoutingJSONUnambiguous([]byte(atLimit)) {
			t.Fatal("maximum supported nesting depth was rejected")
		}
		tooDeep := `{"root":` + strings.Repeat(container.open, keyRoutingMaxJSONDepth) + `null` + strings.Repeat(container.close, keyRoutingMaxJSONDepth) + `}`
		if keyRoutingJSONUnambiguous([]byte(tooDeep)) {
			t.Fatal("excessive nesting depth was accepted")
		}
	}
}
