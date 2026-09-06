package routes

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
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
	if !moshuOnlyEnabled() {
		c.Next()
		return
	}
	// The generic account export contains reversible upstream credentials and
	// proxy passwords. A downstream tenant may inspect redacted account data,
	// but must never be able to export the owner-managed secret material.
	if c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/accounts/data") {
		moshuOnlyMutationGuard(c)
		return
	}
	if c.Request.Method == http.MethodGet || strings.HasSuffix(c.Request.URL.Path, "/test") {
		c.Next()
		return
	}
	if c.Request.Method == http.MethodPut && isAccountUpdatePath(c.Request.URL.Path) && moshuOnlyAccountGroupUpdate(c) {
		c.Next()
		return
	}
	moshuOnlyMutationGuard(c)
}

func moshuOnlyGroupGuard(c *gin.Context) {
	// Retail administrators own their sales groups. Upstream account and proxy
	// topology remains protected by the dedicated account/proxy guards.
	c.Next()
}

func isAccountUpdatePath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 5 || parts[0] != "api" || parts[1] != "v1" || parts[2] != "admin" || parts[3] != "accounts" {
		return false
	}
	_, err := strconv.ParseInt(parts[4], 10, 64)
	return err == nil
}

func moshuOnlyAccountGroupUpdate(c *gin.Context) bool {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return false
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	allowed := map[string]bool{
		"group_ids":                  true,
		"confirm_mixed_channel_risk": true,
	}
	for key := range payload {
		if !allowed[key] {
			return false
		}
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	_, hasGroupIDs := payload["group_ids"]
	return hasGroupIDs
}
