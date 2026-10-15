package service

import (
	"errors"
	"strings"
	"sync"

	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// A connection retains every observed response identity until it is closed.
// Evicting old IDs would make a sufficiently late frame look like a new turn.
// Bound memory by requesting a reconnect before accepting another identity,
// rather than forgetting an identity and risking duplicate accounting.
const openAIWSPassthroughIdentityLimit = 1024

type openAIWSPassthroughAttempt struct {
	sequence   uint64
	responseID string
	complete   bool
	effects    openAIFailureAttemptEffects
}

type openAIWSPassthroughFrameIdentity struct {
	observe bool
	attempt *openAIWSPassthroughAttempt
}

// openAIWSPassthroughResponseIdentity connects the transport's real writes to
// response IDs. beginAttempt runs in the writer; observe runs in the serialized
// reader. The reader alone owns lastFrame, readError, and attempt.effects.
// Nothing here counts failures, changes frames, or retries a request.
type openAIWSPassthroughResponseIdentity struct {
	mu        sync.Mutex
	sequence  uint64
	current   *openAIWSPassthroughAttempt
	responses map[string]*openAIWSPassthroughAttempt
	lastFrame openAIWSPassthroughFrameIdentity
	readError error
}

func newOpenAIWSPassthroughIdentityError() error {
	return NewOpenAIWSClientCloseError(
		coderws.StatusTryAgainLater,
		"upstream response identity is ambiguous; please reconnect",
		errors.New("upstream websocket response identity cannot be safely associated with the active request"),
	)
}

func (s *openAIWSPassthroughResponseIdentity) beginAttempt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readError != nil {
		return s.readError
	}
	if len(s.responses) >= openAIWSPassthroughIdentityLimit ||
		(s.current != nil && (!s.current.complete || s.current.responseID == "")) {
		return newOpenAIWSPassthroughIdentityError()
	}
	s.sequence++
	s.current = &openAIWSPassthroughAttempt{sequence: s.sequence}
	return nil
}

func (s *openAIWSPassthroughResponseIdentity) observe(msgType coderws.MessageType, payload []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFrame = openAIWSPassthroughFrameIdentity{observe: true, attempt: s.current}
	if msgType != coderws.MessageText && msgType != coderws.MessageBinary {
		return
	}
	values := gjson.GetManyBytes(payload, "type", "response.id", "response_id")
	eventType := strings.TrimSpace(values[0].String())
	if eventType == "" {
		return
	}
	responseID := strings.TrimSpace(values[1].String())
	if responseID == "" {
		responseID = strings.TrimSpace(values[2].String())
	}
	terminal := openAIWSPassthroughIsTerminalOutput(payload)
	if responseID == "" && terminal {
		// Keep the relay's existing terminal-only top-level ID compatibility;
		// event_id and error.id are never treated as response IDs.
		responseID = strings.TrimSpace(gjson.GetBytes(payload, "id").String())
	}
	if responseID != "" {
		if prior := s.responses[responseID]; prior != nil {
			if prior != s.current || prior.complete {
				s.lastFrame.observe = false
				return
			}
		} else {
			if s.current == nil || s.current.complete || s.current.responseID != "" || len(s.responses) >= openAIWSPassthroughIdentityLimit {
				s.lastFrame.observe = false
				s.readError = newOpenAIWSPassthroughIdentityError()
				return
			}
			if s.responses == nil {
				s.responses = make(map[string]*openAIWSPassthroughAttempt, 8)
			}
			s.current.responseID = responseID
			s.responses[responseID] = s.current
		}
	} else if eventType == "error" || terminal {
		// After the first attempt an anonymous error might be an old late
		// frame or a real new failure. Forward it, but do not guess ownership
		// or attach the preceding response ID. Reconnect rather than leaving
		// an ambiguous turn open or applying failure/accounting effects twice.
		if s.current == nil || s.current.complete || s.current.sequence > 1 {
			s.lastFrame.observe = false
			s.readError = newOpenAIWSPassthroughIdentityError()
			return
		}
	}
	if terminal && s.current != nil {
		s.current.complete = true
	}
}

// Only the upstream reader calls these methods. Its last frame decision stays
// fixed even if the client writer prepares the next attempt concurrently.
func (s *openAIWSPassthroughResponseIdentity) shouldObserveLastFrame() bool {
	return s == nil || s.lastFrame.observe
}

func (s *openAIWSPassthroughResponseIdentity) failureEffects() *openAIFailureAttemptEffects {
	if s == nil || !s.lastFrame.observe || s.lastFrame.attempt == nil {
		return nil
	}
	return &s.lastFrame.attempt.effects
}
