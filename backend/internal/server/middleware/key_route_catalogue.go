package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// setAPIKeyRouteCatalogueSource is a metadata-only extension seam. The model
// handler may ask this source for authorized candidates, but ingress must not
// select a group, inspect a model body or update routing statistics here.
// Native provider catalogues and the Codex manifest keep their existing paths.
func setAPIKeyRouteCatalogueSource(c *gin.Context, selector APIKeyRouteSelector) {
	if selector == nil || c.Request.Method != http.MethodGet || HasForcePlatform(c) || c.IsWebsocket() || c.Query("client_version") != "" {
		return
	}
	path := c.Request.URL.Path
	ordinary := path == "/v1/models" || path == "/models"
	if !ordinary {
		for _, prefix := range []string{"/v1/models/", "/models/"} {
			if strings.HasPrefix(path, prefix) {
				model := strings.TrimPrefix(path, prefix)
				ordinary = model != "" && !strings.Contains(model, "/")
				break
			}
		}
	}
	if ordinary {
		c.Set("custom.key_route_catalogue_source", selector)
	}
}
