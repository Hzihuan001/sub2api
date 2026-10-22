package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/keyrouting"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestmodel"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

const keyAccountNativeRetriesKey = "custom.key_account_native_retries"

// Native retry state survives a clean TTFT/admission re-entry into the SAME
// group. A different group retains its own native state; the transport session
// additionally bounds all groups. These are not new retry permissions.
type keyAccountNativeRetries struct {
	Failover                   *FailoverState
	ProfitVetoCount            int
	FirstOutputTimeoutSwitches int
	OAuth429                   service.OpenAIOAuth429FailoverState
	Passthrough                openAIPassthroughFailoverState
	activeSwitchCount          *int
}

func keyAccountRetryState(c *gin.Context, maxSwitches int, hasBoundSession bool) *keyAccountNativeRetries {
	fresh := func() *keyAccountNativeRetries {
		return &keyAccountNativeRetries{Failover: NewFailoverState(maxSwitches, hasBoundSession)}
	}
	if c == nil || c.Request == nil || service.GetKeyAccountRoutingSession(c.Request.Context()) == nil {
		return fresh()
	}
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	if !ok || key == nil || key.GroupID == nil {
		return fresh()
	}
	value, _ := c.Get(keyAccountNativeRetriesKey)
	states, _ := value.(map[int64]*keyAccountNativeRetries)
	if states == nil {
		states = make(map[int64]*keyAccountNativeRetries)
		c.Set(keyAccountNativeRetriesKey, states)
	}
	if state := states[*key.GroupID]; state != nil {
		return state
	}
	state := fresh()
	states[*key.GroupID] = state
	return state
}

func keyAccountFailoverState(c *gin.Context, maxSwitches int, hasBoundSession bool) *FailoverState {
	return keyAccountRetryState(c, maxSwitches, hasBoundSession).Failover
}

func existingKeyAccountRetryState(c *gin.Context, groupID int64) *keyAccountNativeRetries {
	value, _ := c.Get(keyAccountNativeRetriesKey)
	states, _ := value.(map[int64]*keyAccountNativeRetries)
	return states[groupID]
}

func keyAccountNativeSwitchAvailable(c *gin.Context, groupID int64) bool {
	state := existingKeyAccountRetryState(c, groupID)
	if state == nil {
		return false
	}
	count := state.Failover.SwitchCount
	if state.activeSwitchCount != nil {
		count = *state.activeSwitchCount
	}
	return count < state.Failover.MaxSwitches
}

// Avoid a native same-account sleep/retry when this request's just-observed
// result has already cooled that account. This is only an advisory hint: the
// transport still performs the authoritative final admission after any wait.
// Optional health failure retains native retry behavior and local exclusions;
// configuration/context errors never authorize a retry.
func keyAccountSameAccountRetryAllowed(ctx context.Context, accountID int64) bool {
	session := service.GetKeyAccountRoutingSession(ctx)
	if session == nil {
		return true
	}
	group, ok := ctx.Value(ctxkey.Group).(*service.Group)
	if !ok || group == nil {
		return false
	}
	allowed, err := session.CandidateEligible(ctx, group.ID, accountID)
	if errors.Is(err, service.ErrKeyAccountSoftHealthUnavailable) && ctx.Err() == nil {
		_, excluded := session.RequestExclusions(group.ID)[accountID]
		return !excluded
	}
	return err == nil && allowed
}

// Native handler defers have run before this loop can re-enter. TTFT is
// attempt-local; the old group-wide timer is never installed for account_v2.
func runKeyAccountRouteDispatch(c *gin.Context, next gin.HandlerFunc) {
	// A second bounded guard covers programming errors without granting extra
	// sends: BeforeDispatch still enforces the smaller shared/native budgets.
	for entry := 0; entry < 4096+keyrouting.MaxBackupGroups; entry++ {
		service.BeginOpsRouteErrorAttempt(c, 0)
		c.Set("custom.key_route_restart", false)
		c.Set("custom.key_account_restart", false)
		c.Set("custom.key_account_count_switch", false)
		next(c)
		if state := keyAccountForwardStateFromContext(c); state != nil && !state.ended {
			// Covers a native early return after scope installation. Its handler
			// has now fully returned, so all native defers have completed.
			finishKeyAccountNativeForward(c, nil, errors.New("native handler returned before attempt completion"))
		}
		if c.IsAborted() || c.Writer.Written() || c.Request.Context().Err() != nil || !c.GetBool("custom.key_route_restart") {
			return
		}
		if !c.GetBool("custom.key_account_restart") {
			continue // A proven-empty group has already installed its backup.
		}
		key, ok := servermiddleware.GetAPIKeyFromContext(c)
		request, requestOK := keyRouteRequest(c)
		value, _ := c.Get(keyRoutingSelectorContextKey)
		candidates, candidateOK := value.(keyAccountCandidateSelector)
		selector, selectorOK := value.(keyRouteFailoverSelector)
		if !ok || key == nil || key.GroupID == nil || !requestOK || !candidateOK || !selectorOK || keyAccountForwardReplayBlocked(c) {
			writeKeyAccountUnavailable(c, "Unable to safely continue account routing")
			return
		}
		if c.GetBool("custom.key_account_count_switch") {
			retries := existingKeyAccountRetryState(c, *key.GroupID)
			if retries == nil {
				writeKeyAccountUnavailable(c, "Unable to verify native account retry budget")
				return
			}
			if retries.Failover.SwitchCount >= retries.Failover.MaxSwitches {
				if !tryKeyAccountBudgetBackup(c, key, request, selector) {
					writeKeyAccountUnavailable(c, "Native account retry budget exhausted")
					return
				}
				continue
			}
			retries.Failover.SwitchCount++
			if state := keyAccountForwardStateFromContext(c); state != nil {
				retries.Failover.FailedAccountIDs[state.account] = struct{}{}
			}
		}
		hasCandidate, err := candidates.AccountGroupHasCandidate(c.Request.Context(), cloneKeyRoutingPrimaryKey(c, key), request, *key.GroupID)
		if err != nil {
			writeKeyAccountUnavailable(c, "Unable to verify remaining account candidates")
			return
		}
		if hasCandidate {
			if err := resetKeyAccountGroupContext(c, key, request, selector); err != nil {
				writeKeyAccountUnavailable(c, "Unable to prepare the next account attempt")
				return
			}
			continue
		}
		if _, switched := switchKeyRoute(c, key, request, selector); !switched {
			writeKeyAccountUnavailable(c, "No eligible account remains in the configured groups")
			return
		}
	}
	writeKeyAccountUnavailable(c, "Account routing safety budget exhausted")
}

// Restore the ingress body/context before a same-group native re-entry. Never
// carry a canceled wire, previous parser, transformed body or pricing context.
func resetKeyAccountGroupContext(c *gin.Context, key *service.APIKey, request service.KeyRoutingRequest, selector keyRouteFailoverSelector) error {
	base, ok := c.Get("custom.key_route_base_context")
	ctx, valid := base.(context.Context)
	if !ok || !valid || key == nil || key.Group == nil {
		return service.ErrKeyAccountRouteScope
	}
	ctx = context.WithValue(ctx, ctxkey.Group, key.Group)
	ctx = context.WithValue(ctx, ctxkey.UserID, key.UserID)
	ctx = context.WithValue(ctx, ctxkey.KeyRoutingActive, true)
	preparer, ok := selector.(keyRouteContextPreparer)
	if !ok {
		return service.ErrKeyAccountRouteScope
	}
	ctx, mapped, err := preparer.PrepareRouteContext(ctx, key, request)
	if err != nil {
		return err
	}
	value, ok := c.Get("custom.key_route_body")
	original, valid := value.([]byte)
	if !ok || !valid {
		return service.ErrKeyAccountRouteScope
	}
	body := original
	if mapped != "" && mapped != request.Model {
		body, err = sjson.SetBytes(original, "model", mapped)
		if err != nil {
			return err
		}
	}
	requestmodel.ResetRequestBody(c.Request, body)
	c.Request = c.Request.WithContext(ctx)
	return nil
}

// A native retry ceiling is not evidence that every candidate was tried.
// The approved budget exception can move forward safely without marking any
// untried candidate failed or cooled, but never resets the shared wire budget.
func tryKeyAccountGroupExhausted(c *gin.Context) bool {
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	request, requestOK := keyRouteRequest(c)
	value, _ := c.Get(keyRoutingSelectorContextKey)
	candidates, candidateOK := value.(keyAccountCandidateSelector)
	if !ok || key == nil || key.GroupID == nil || !requestOK || !candidateOK || keyAccountForwardReplayBlocked(c) {
		return false
	}
	hasCandidate, err := candidates.AccountGroupHasCandidate(c.Request.Context(), cloneKeyRoutingPrimaryKey(c, key), request, *key.GroupID)
	if err != nil {
		return false
	}
	if hasCandidate {
		selector, ok := value.(keyRouteFailoverSelector)
		if !ok || existingKeyAccountRetryState(c, *key.GroupID) == nil || keyAccountNativeSwitchAvailable(c, *key.GroupID) {
			return false
		}
		return tryKeyAccountBudgetBackup(c, key, request, selector)
	}
	return tryKeyRouteUnavailable(c)
}

func tryKeyAccountBudgetBackup(c *gin.Context, key *service.APIKey, request service.KeyRoutingRequest, selector keyRouteFailoverSelector) bool {
	if c == nil || c.Request == nil || keyAccountForwardReplayBlocked(c) {
		return false
	}
	session := service.GetKeyAccountRoutingSession(c.Request.Context())
	if session == nil || session.HasAttemptBudget(c.Request.Context()) != nil {
		return false
	}
	if _, switched := switchKeyRoute(c, key, request, selector); !switched {
		return false
	}
	c.Set("custom.key_route_restart", true)
	return true
}

func writeKeyAccountUnavailable(c *gin.Context, message string) {
	if c == nil || c.Request == nil || c.Writer.Written() || c.Request.Context().Err() != nil {
		return
	}
	if wait := service.KeyAccountRouteRetryAfter(c.Request.Context()); wait > 0 {
		c.Header("Retry-After", strconv.FormatInt(int64((wait+time.Second-1)/time.Second), 10))
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"type": "key_account_route_unavailable", "message": message}})
}
