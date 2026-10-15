package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

const keyRoutingMaxJSONDepth = 256

// keyRoutingJSONUnambiguous validates without rewriting the original body.
// Duplicate object members can disagree between gjson (first value), Go's JSON
// binding (last value, case-insensitive fields), and upstream parsers. Reject
// exact duplicates everywhere and case-folded duplicates for routing fields.
// Callers keep this check inside the multiple-group routing branch.
func keyRoutingJSONUnambiguous(body []byte) bool {
	if !utf8.Valid(body) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return false
	}
	if !keyRoutingJSONValue(decoder, first, 1) {
		return false
	}
	_, err = decoder.Token()
	return err == io.EOF
}

func keyRoutingJSONValue(decoder *json.Decoder, token json.Token, depth int) bool {
	delimiter, isContainer := token.(json.Delim)
	if !isContainer {
		return true
	}
	if depth > keyRoutingMaxJSONDepth {
		return false
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			field, err := decoder.Token()
			name, ok := field.(string)
			if err != nil || !ok {
				return false
			}
			name = keyRoutingJSONFieldName(name)
			if _, exists := seen[name]; exists {
				return false
			}
			seen[name] = struct{}{}
			value, err := decoder.Token()
			if err != nil || !keyRoutingJSONValue(decoder, value, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim('}')
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil || !keyRoutingJSONValue(decoder, value, depth+1) {
				return false
			}
		}
		end, err := decoder.Token()
		return err == nil && end == json.Delim(']')
	default:
		return false
	}
}

func keyRoutingJSONFieldName(name string) string {
	for _, field := range [...]string{
		"model", "session", "tools", "tool_choice", "type", "input",
		"context_management", "previous_response_id", "conversation", "background",
	} {
		if strings.EqualFold(name, field) {
			return field
		}
	}
	return name
}
