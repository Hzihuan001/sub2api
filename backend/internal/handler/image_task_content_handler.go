package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetImage serves a completed immutable image after the same ownership check
// as task polling. ServeContent supports conditional/range GETs, so reconnects
// download the saved object without submitting another generation request.
func (h *AsyncImageHandler) GetImage(c *gin.Context) {
	if !h.pollable() {
		imageTaskError(c, service.ErrImageTaskNotFound)
		return
	}
	key, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || key == nil {
		imageTaskError(c, service.ErrImageTaskForbidden)
		return
	}
	f, err := h.tasks.OpenLocalImage(c.Request.Context(), service.ImageTaskOwner{UserID: key.UserID, APIKeyID: key.ID}, c.Param("task_id"), c.Param("filename"))
	if err != nil {
		imageTaskError(c, err)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		imageTaskError(c, service.ErrImageTaskNotFound)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), f)
}
