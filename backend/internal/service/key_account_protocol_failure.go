package service

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
)

// These adapters are called by native parsers, on the original wire context.
// Neither an SSE event name nor a local failover error is upstream proof.
func markKeyAccountOpenAIProtocolFailure(ctx context.Context, payload []byte) bool {
	if ctx == nil {
		return false
	}
	root := keyAccountProtocolObject(payload)
	if root == nil {
		return false
	}
	observeKeyAccountDecodedProtocolPayload(ctx, root)
	errObject, _ := root["error"].(map[string]any)
	if errObject == nil {
		if keyAccountProtocolString(root["type"]) == "error" && (keyAccountProtocolString(root["code"]) != "" || keyAccountProtocolString(root["message"]) != "") {
			errObject = root // Responses' documented flat error event.
		}
	}
	if errObject == nil {
		response, _ := root["response"].(map[string]any)
		errObject, _ = response["error"].(map[string]any)
		if keyAccountProtocolString(root["type"]) != "response.failed" {
			return false
		}
	}
	if !keyAccountProtocolErrorDetails(errObject) {
		return false
	}
	return markKeyAccountDecodedProtocolFailure(ctx, root)
}

func markKeyAccountAnthropicProtocolFailure(ctx context.Context, payload []byte) bool {
	if ctx == nil {
		return false
	}
	root := keyAccountProtocolObject(payload)
	if keyAccountProtocolString(root["type"]) != "error" {
		return false
	}
	errObject, _ := root["error"].(map[string]any)
	if keyAccountProtocolString(errObject["type"]) == "" || keyAccountProtocolString(errObject["message"]) == "" {
		return false
	}
	return markKeyAccountDecodedProtocolFailure(ctx, root)
}

// Native Gemini parsers historically accept an error object as an empty
// generation. Preserve that native billing path if it carries usage/output.
// Only a proven, replay-safe transient failure opts in to the existing retry
// signal. Unscoped callers keep their exact native behavior.
func keyAccountGeminiProtocolFailure(ctx context.Context, resp *http.Response, payload []byte) *UpstreamFailoverError {
	if ctx == nil {
		return nil
	}
	root := keyAccountProtocolObject(payload)
	errObject, _ := root["error"].(map[string]any)
	code, err := strconv.Atoi(keyAccountProtocolString(errObject["code"]))
	if err != nil || code < 400 || code > 599 || !keyAccountProtocolErrorDetails(errObject) {
		return nil
	}
	if !markKeyAccountDecodedProtocolFailure(ctx, root) {
		return nil
	}
	attempt := keyrouting.GetAccountAttempt(ctx)
	if attempt == nil || attempt.ReplayBlocked() || code != http.StatusRequestTimeout && code != http.StatusTooManyRequests && code < 500 {
		return nil
	}
	var headers http.Header
	if resp != nil {
		headers = resp.Header.Clone()
	}
	return &UpstreamFailoverError{StatusCode: code, ResponseBody: append([]byte(nil), payload...), ResponseHeaders: headers}
}

func keyAccountProtocolObject(payload []byte) map[string]any {
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if !json.Valid(payload) || decoder.Decode(&root) != nil {
		return nil
	}
	return root
}

func keyAccountProtocolString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func keyAccountProtocolErrorDetails(value map[string]any) bool {
	return value != nil && (keyAccountProtocolString(value["message"]) != "" || keyAccountProtocolString(value["type"]) != "" || keyAccountProtocolString(value["code"]) != "")
}

func markKeyAccountDecodedProtocolFailure(ctx context.Context, root map[string]any) bool {
	if ctx == nil || keyrouting.GetAccountAttempt(ctx) == nil {
		return false
	}
	// Decoding an error never erases evidence that work already happened.
	// Inspect decoded wrappers too: Gemini v1internal puts usageMetadata under
	// response, which is not the ordinary OpenAI response.usage shape.
	observeKeyAccountDecodedProtocolPayload(ctx, root)
	return MarkKeyAccountVerifiedFailure(ctx)
}

func observeKeyAccountProtocolPayload(ctx context.Context, payload []byte) {
	if ctx == nil {
		return
	}
	observeKeyAccountDecodedProtocolPayload(ctx, keyAccountProtocolObject(payload))
}

func observeKeyAccountDecodedProtocolPayload(ctx context.Context, root map[string]any) {
	if attempt := keyrouting.GetAccountAttempt(ctx); attempt != nil && root != nil {
		keyAccountObserveFailureSafety(attempt, root, 0)
	}
}

func observeKeyAccountOpenAISSEBody(ctx context.Context, body []byte) {
	if ctx == nil {
		return
	}
	var parser openAICompatSSEFrameParser
	for _, line := range bytes.Split(body, []byte("\n")) {
		if frame, ok := parser.AddLine(strings.TrimRight(string(line), "\r")); ok {
			observeKeyAccountProtocolPayload(ctx, []byte(frame.Data))
		}
	}
	if frame, ok := parser.Finish(); ok {
		observeKeyAccountProtocolPayload(ctx, []byte(frame.Data))
	}
}

func collectGeminiSSEForKeyAccount(resp *http.Response, isOAuth bool, observe func([]byte)) (map[string]any, *ClaudeUsage, geminiSSECollectStats, error) {
	proofContext := KeyAccountResponseContext(resp)
	if proofContext == nil {
		return collectGeminiSSEObserved(resp.Body, isOAuth, observe)
	}
	var failurePayload []byte
	var actualUsage *ClaudeUsage
	collected, usage, stats, err := collectGeminiSSEObserved(resp.Body, isOAuth, func(payload []byte) {
		observeKeyAccountProtocolPayload(proofContext, payload)
		if u := extractGeminiUsage(payload); u.hasObservedTokens() {
			actualUsage = u
		}
		if keyAccountGeminiProtocolError(proofContext, resp, payload) != nil {
			failurePayload = append(failurePayload[:0], payload...)
		}
		if observe != nil {
			observe(payload)
		}
	})
	if actualUsage != nil {
		usage = actualUsage
	}
	if err == nil && len(failurePayload) > 0 {
		// A later event may supply usage/output. Re-evaluate only after the
		// native collector has drained everything; never replay an early error
		// followed by billable content.
		if failure := keyAccountGeminiProtocolError(proofContext, resp, failurePayload); failure != nil {
			if usage.hasObservedTokens() {
				failure = keyAccountKeepPartialError(failure)
			}
			return collected, usage, stats, failure
		}
	}
	if err != nil && usage.hasObservedTokens() {
		err = keyAccountKeepPartialError(err)
	}
	return collected, usage, stats, err
}

func keyAccountObserveFailureSafety(attempt *keyrouting.AccountAttempt, value any, depth int) {
	if depth > 16 {
		attempt.PreventReplay(keyrouting.AttemptVetoUnknownProtocol)
		return
	}
	switch object := value.(type) {
	case map[string]any:
		for key, nested := range object {
			switch key {
			case "usage", "usageMetadata", "metrics", "metering":
				if keyAccountFailurePositiveNumber(nested) {
					attempt.ObserveUsage()
				}
			case "output", "output_text", "content", "choices", "candidates", "parts", "delta", "audio", "images":
				if keyAccountFailureNonempty(nested) {
					attempt.PreventReplay(keyrouting.AttemptVetoFirstContent)
				}
			case "tool_calls", "tool_use", "functionCall", "function_call", "toolUse":
				if keyAccountFailureNonempty(nested) {
					attempt.ObserveSideEffect()
				}
			case "status":
				if state := keyAccountProtocolString(nested); state == "completed" || state == "done" {
					attempt.PreventReplay(keyrouting.AttemptVetoCompleteResponse)
				}
			}
			keyAccountObserveFailureSafety(attempt, nested, depth+1)
		}
	case []any:
		for _, nested := range object {
			keyAccountObserveFailureSafety(attempt, nested, depth+1)
		}
	}
}

func keyAccountFailurePositiveNumber(value any) bool {
	return keyAccountFailurePositiveNumberAtDepth(value, 0)
}

func keyAccountFailurePositiveNumberAtDepth(value any, depth int) bool {
	if depth > 16 {
		return true // Uninspectable usage must never be guessed free.
	}
	switch v := value.(type) {
	case string:
		n, err := strconv.ParseFloat(v, 64)
		return err == nil && n > 0
	case json.Number:
		n, err := v.Float64()
		return err == nil && n > 0
	case map[string]any:
		for _, nested := range v {
			if keyAccountFailurePositiveNumberAtDepth(nested, depth+1) {
				return true
			}
		}
	case []any:
		for _, nested := range v {
			if keyAccountFailurePositiveNumberAtDepth(nested, depth+1) {
				return true
			}
		}
	}
	return false
}

func keyAccountFailureNonempty(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}
