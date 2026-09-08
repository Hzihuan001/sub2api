package moshureseller

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) Status(c *gin.Context) {
	result, err := h.service.Status(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

type enrollRequest struct {
	BaseURL        string `json:"base_url"`
	EnrollmentCode string `json:"enrollment_code" binding:"required"`
}

func (h *Handler) Enroll(c *gin.Context) {
	var req enrollRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	result, err := h.service.Enroll(c.Request.Context(), req.BaseURL, req.EnrollmentCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) SyncCatalog(c *gin.Context) {
	result, err := h.service.SyncCatalog(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Balance(c *gin.Context) {
	result, err := h.service.Balance(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

type configureProductRequest struct {
	Selected        bool    `json:"selected"`
	SalesName       string  `json:"sales_name"`
	SalesMultiplier float64 `json:"sales_multiplier"`
	Capacity        int     `json:"capacity"`
}

func (h *Handler) ConfigureProduct(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid product id")
		return
	}
	var req configureProductRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	result, err := h.service.ConfigureProduct(c.Request.Context(), id, req.Selected, req.SalesName, req.SalesMultiplier, req.Capacity)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) RotateCredential(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid product id")
		return
	}
	result, err := h.service.RotateCredential(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) EnsureProductTestAccount(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid product id")
		return
	}
	accountID, err := h.service.EnsureProductTestAccount(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, gin.H{"account_id": accountID})
}

func (h *Handler) SyncSettlements(c *gin.Context) {
	count, err := h.service.SyncSettlements(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, gin.H{"synced": count})
}

func (h *Handler) ListProfits(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.service.ListProfits(c.Request.Context(), page, pageSize)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *Handler) writeError(c *gin.Context, err error) {
	var upstream *upstreamRequestError
	switch {
	case errors.As(err, &upstream):
		// An upstream 401 must not log the local administrator out.
		status := http.StatusBadGateway
		if upstream.Status >= 400 && upstream.Status < 500 {
			status = http.StatusBadRequest
		}
		response.Error(c, status, upstream.Message)
	case errors.Is(err, ErrDisabled):
		response.Error(c, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, ErrNotConnected):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, ErrInvalidInput):
		response.BadRequest(c, err.Error())
	default:
		slog.Error("moshu_reseller.request_failed", "path", c.FullPath(), "error", err)
		response.InternalError(c, "代理商操作失败，请使用请求 ID 查询服务日志")
	}
}
