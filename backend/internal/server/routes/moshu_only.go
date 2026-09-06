package routes

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// moshuOnlyEnabled turns the L1 deployment into a downstream-only tenant.
// It is deliberately an environment switch so the same upstream project can
// still be used for a normal standalone deployment.
func moshuOnlyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MOSHU_ONLY_MODE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func moshuOnlyMutationGuard(c *gin.Context) {
	if !moshuOnlyEnabled() {
		c.Next()
		return
	}
	response.Error(c, http.StatusForbidden, "This operation is unavailable for the configured upstream")
	c.Abort()
}

func moshuOnlyReadOnlyGuard(c *gin.Context) {
	if !moshuOnlyEnabled() || c.Request.Method == http.MethodGet {
		c.Next()
		return
	}
	moshuOnlyMutationGuard(c)
}

func moshuOnlyBackupGuard(c *gin.Context) {
	if !moshuOnlyEnabled() || !strings.HasSuffix(c.Request.URL.Path, "/restore") {
		c.Next()
		return
	}
	moshuOnlyMutationGuard(c)
}

// moshuOnlyReadWriteGuard allows read-only inspection and account connectivity
// tests, but prevents administrators from adding, replacing, importing,
// deleting, or re-authorizing upstream accounts.
func moshuOnlyAccountGuard(c *gin.Context) {
	if !moshuOnlyEnabled() || c.Request.Method == http.MethodGet || strings.HasSuffix(c.Request.URL.Path, "/test") {
		c.Next()
		return
	}
	moshuOnlyMutationGuard(c)
}

func moshuOnlyGroupGuard(c *gin.Context) {
	if !moshuOnlyEnabled() {
		c.Next()
		return
	}
	// Existing product groups remain visible. Retail admins may change the
	// selling multiplier and per-user overrides, but the product catalogue and
	// routing topology are owned by Moshu in this phase.
	if c.Request.Method == http.MethodGet ||
		strings.HasSuffix(c.Request.URL.Path, "/rate-multipliers") ||
		(strings.HasSuffix(c.Request.URL.Path, "/sort-order") && c.Request.Method == http.MethodPut) {
		c.Next()
		return
	}
	if c.Request.Method == http.MethodPut && isGroupUpdatePath(c.Request.URL.Path) && moshuOnlyRetailGroupUpdate(c) {
		c.Next()
		return
	}
	moshuOnlyMutationGuard(c)
}

func isGroupUpdatePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "admin" && parts[3] == "groups" && parts[4] != ""
}

func moshuOnlyRetailGroupUpdate(c *gin.Context) bool {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return false
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	allowed := map[string]bool{
		"rate_multiplier": true,
		"description":     true,
	}
	for key := range payload {
		if !allowed[key] {
			return false
		}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return len(payload) > 0
}
