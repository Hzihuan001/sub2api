package kiro

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// EventStreamFailure is a decoded AWS exception, not an inference from an
// event's name or from a transport/read error. Payload is the original JSON.
type EventStreamFailure struct {
	ExceptionType string
	Message       string
	Payload       []byte
}

func (e *EventStreamFailure) Error() string {
	return fmt.Sprintf("kiro upstream exception %s: %s", e.ExceptionType, e.Message)
}

// StatusCode classifies only known, typed AWS exceptions. Unknown types remain
// unclassified rather than turning a local 502 into a claimed upstream status.
func (e *EventStreamFailure) StatusCode() int {
	if e == nil {
		return 0
	}
	switch e.ExceptionType {
	case "ThrottlingException", "TooManyRequestsException", "ServiceQuotaExceededException":
		return http.StatusTooManyRequests
	case "ServiceUnavailableException":
		return http.StatusServiceUnavailable
	case "InternalServerException", "InternalFailure":
		return http.StatusInternalServerError
	case "ModelTimeoutException", "TimeoutException":
		return http.StatusRequestTimeout
	case "ValidationException":
		return http.StatusBadRequest
	case "AccessDeniedException":
		return http.StatusForbidden
	case "ResourceNotFoundException":
		return http.StatusNotFound
	default:
		return 0
	}
}

// EventStreamObservation contains only actual upstream facts. Estimated input
// tokens and cache emulation are deliberately excluded. The callback is opt-in,
// synchronous, and is invoked before local Anthropic events are synthesized.
type EventStreamObservation struct {
	EventType     string
	Usage         Usage
	HasContent    bool
	HasSideEffect bool
	Complete      bool
	Failure       *EventStreamFailure
}

func isGenerationEvent(msg *eventStreamMessage, event map[string]any) bool {
	if event == nil || msg == nil {
		return false
	}
	switch msg.EventType {
	case "assistantResponseEvent", "reasoningContentEvent", "toolUseEvent":
		return true
	case "messageStopEvent", "message_stop":
		return readStopReason(nestedEvent(event, msg.EventType)) != ""
	default:
		return false
	}
}

func observeDecodedEventStreamMessage(msg *eventStreamMessage, event map[string]any, usage *Usage, observe func(EventStreamObservation)) error {
	if observe == nil || msg == nil {
		return nil
	}
	updateUsageFromEvent(usage, msg.EventType, event)
	observation := EventStreamObservation{EventType: msg.EventType, Usage: *usage}
	switch msg.EventType {
	case "assistantResponseEvent":
		assistant := nestedEvent(event, msg.EventType)
		observation.HasContent = getString(assistant, "content") != "" || getString(event, "content") != ""
		observation.HasSideEffect = len(readToolUses(assistant, event)) != 0
	case "reasoningContentEvent":
		reasoning := nestedEvent(event, msg.EventType)
		observation.HasContent = getString(reasoning, "text") != "" || getString(event, "text") != ""
	case "toolUseEvent":
		tool := nestedEvent(event, msg.EventType)
		observation.HasSideEffect = getString(tool, "toolUseId") != "" || getString(tool, "name") != ""
	case "messageStopEvent", "message_stop":
		observation.Complete = true
	}
	if msg.MessageType == "exception" || msg.MessageType == "error" {
		kind := msg.ExceptionType
		if msg.MessageType == "error" {
			kind = msg.ErrorCode
		}
		message := strings.TrimSpace(getString(event, "message"))
		if message == "" {
			message = strings.TrimSpace(getString(event, "Message"))
		}
		if kind == "" || message == "" || !json.Valid(msg.Payload) || event == nil {
			observe(observation)
			return fmt.Errorf("invalid kiro typed exception envelope")
		}
		observation.Failure = &EventStreamFailure{ExceptionType: kind, Message: message, Payload: append([]byte(nil), msg.Payload...)}
	}
	observe(observation)
	if observation.Failure != nil {
		return observation.Failure
	}
	return nil
}
