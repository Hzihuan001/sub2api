package handler

import (
	"errors"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// tryKeyRouteSelectionUnavailable keeps storage/admission errors separate from
// the scheduler's explicit empty-pool result. Never infer availability from an
// error string: a database, user concurrency, balance or RPM failure must not
// acquire a different group's resources.
func tryKeyRouteSelectionUnavailable(c *gin.Context, err error) bool {
	return errors.Is(err, service.ErrNoAvailableAccounts) && tryKeyRouteUnavailable(c)
}

// tryKeyRouteUnavailable handles a real empty candidate pool without recording
// a fault rule outcome. It remains available when key_rules=[] disables fault
// cooldowns, but only before any client response has started. SelectNext owns
// authorization, native readiness and the strictly forward-only group cursor.
func tryKeyRouteUnavailable(c *gin.Context) bool {
	if c == nil || c.Request == nil || !c.GetBool("custom.key_route_dispatch") || !keyRoutingRequestActive(c.Request.Context()) || c.Request.Context().Err() != nil || c.Writer == nil || c.Writer.Written() || c.Writer.Size() > 0 {
		return false
	}
	key, ok := servermiddleware.GetAPIKeyFromContext(c)
	request, requestOK := keyRouteRequest(c)
	if !ok || key == nil || key.GroupID == nil || *key.GroupID <= 0 || len(key.BackupGroupIDs) == 0 || !requestOK {
		return false
	}
	value, _ := c.Get(keyRoutingSelectorContextKey)
	selector, ok := value.(keyRouteFailoverSelector)
	if !ok || selector == nil {
		return false
	}
	if _, switched := switchKeyRoute(c, key, request, selector); switched {
		c.Set("custom.key_route_restart", true)
		return true
	}
	return false
}
