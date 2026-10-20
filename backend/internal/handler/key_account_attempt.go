package handler

import (
	"context"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/gin-gonic/gin"
)

type keyAccountAttemptScopeContextKey struct{}

type keyAccountAttemptScope struct {
	parent context.Context
	closed bool
}

// installKeyAccountAttempt replaces only request-local attempt/guard values.
// It deliberately preserves inbound cancellation and does not dispatch: call
// attempt.Dispatch immediately before real inference, passing the returned
// detached child to the native transport instead of replacing the inbound
// request context. Timers never access Gin state.
func installKeyAccountAttempt(c *gin.Context, attempt *keyrouting.AccountAttempt) func() {
	if c == nil || c.Request == nil || attempt == nil {
		return func() {}
	}
	parent := c.Request.Context()
	scope := &keyAccountAttemptScope{parent: parent}
	bound := keyrouting.WithAccountAttempt(parent, attempt)
	c.Request = c.Request.WithContext(context.WithValue(bound, keyAccountAttemptScopeContextKey{}, scope))
	var once sync.Once
	return func() {
		once.Do(func() {
			scope.closed = true
			// A stale scope must not remove a subsequently installed attempt.
			if c.Request != nil && c.Request.Context().Value(keyAccountAttemptScopeContextKey{}) == scope {
				restore := parent
				for {
					previous, _ := restore.Value(keyAccountAttemptScopeContextKey{}).(*keyAccountAttemptScope)
					if previous == nil || !previous.closed {
						break
					}
					restore = previous.parent
				}
				c.Request = c.Request.WithContext(restore)
			}
		})
	}
}

func keyAccountAttemptFromContext(c *gin.Context) *keyrouting.AccountAttempt {
	if c == nil || c.Request == nil {
		return nil
	}
	return keyrouting.GetAccountAttempt(c.Request.Context())
}

// finishKeyAccountAttempt reuses the native result's full billing vocabulary.
// It records no usage and acknowledges no cleanup: the native caller retains
// billing and slot ownership, then separately invokes MarkCleanupComplete.
func finishKeyAccountAttempt(c *gin.Context, attempt *keyrouting.AccountAttempt, nativeResult any, terminal keyrouting.AccountAttemptResult) keyrouting.AccountAttemptSample {
	if attempt == nil {
		return keyrouting.AccountAttemptSample{Outcome: keyrouting.OutcomeIgnored, StatusSource: keyrouting.AttemptStatusNone}
	}
	if keyRouteFirstContentHasUsage(nativeResult) {
		terminal.UsageKnown = true
	}
	if c != nil && c.Request != nil && c.Request.Context().Err() != nil {
		attempt.ObserveClientCancellation()
	}
	if c != nil && c.Writer != nil && (c.Writer.Written() || c.Writer.Size() > 0) {
		terminal.ResponseCommitted = true
	}
	return attempt.Finish(terminal)
}

// Preserve a context-only lookup for native builders that do not receive Gin.
func keyAccountAttemptFromRequestContext(ctx context.Context) *keyrouting.AccountAttempt {
	return keyrouting.GetAccountAttempt(ctx)
}
