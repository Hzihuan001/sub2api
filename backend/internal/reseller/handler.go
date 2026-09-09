package reseller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

const resellerIDContextKey = "reseller_protocol_tenant_id"

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

type createTenantRequest struct {
	UserID       int64    `json:"user_id" binding:"required"`
	Name         string   `json:"name" binding:"required"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
}

func (h *Handler) CreateTenant(c *gin.Context) {
	var req createTenantRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	tenant, err := h.service.CreateTenant(c.Request.Context(), req.UserID, req.Name, req.AllowedCIDRs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Created(c, tenant)
}

func (h *Handler) ListTenants(c *gin.Context) {
	tenants, err := h.service.ListTenants(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, tenants)
}

type updateTenantRequest struct {
	Status       string   `json:"status" binding:"required"`
	AllowedCIDRs []string `json:"allowed_cidrs"`
}

func (h *Handler) ChangeBillingAccount(c *gin.Context) {
	id, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		UserID int64 `json:"user_id" binding:"required,gt=0"`
	}
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid billing account")
		return
	}
	tenant, err := h.service.ChangeBillingAccount(c.Request.Context(), id, req.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, tenant)
}

func (h *Handler) UpdateTenant(c *gin.Context) {
	id, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	var req updateTenantRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	tenant, err := h.service.UpdateTenant(c.Request.Context(), id, req.Status, req.AllowedCIDRs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, tenant)
}

type upsertProductRequest struct {
	MoshuGroupID int64  `json:"moshu_group_id" binding:"required"`
	ProductCode  string `json:"product_code" binding:"required"`
	DisplayName  string `json:"display_name" binding:"required"`
	Enabled      *bool  `json:"enabled"`
}

func (h *Handler) DeleteTenant(c *gin.Context) {
	id, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	if err := h.service.DeleteTenant(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *Handler) UpsertProduct(c *gin.Context) {
	resellerID, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	var req upsertProductRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	product, err := h.service.UpsertProduct(c.Request.Context(), resellerID, req.MoshuGroupID, req.ProductCode, req.DisplayName, enabled)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, product)
}

func (h *Handler) ListProducts(c *gin.Context) {
	resellerID, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	products, err := h.service.ListProducts(c.Request.Context(), resellerID, false)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, products)
}

func (h *Handler) DeleteProduct(c *gin.Context) {
	resellerID, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	productID, ok := positiveParam(c, "product_id")
	if !ok {
		return
	}
	if err := h.service.DeleteProduct(c.Request.Context(), resellerID, productID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

type enrollmentRequest struct {
	ProductIDs []int64 `json:"product_ids" binding:"required"`
	TTLMinutes int     `json:"ttl_minutes"`
}

func (h *Handler) CreateEnrollment(c *gin.Context) {
	resellerID, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	var req enrollmentRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	createdBy := int64(0)
	if subject, exists := middleware.GetAuthSubjectFromContext(c); exists {
		createdBy = subject.UserID
	}
	code, expiresAt, err := h.service.CreateEnrollment(c.Request.Context(), resellerID, createdBy, req.ProductIDs, time.Duration(req.TTLMinutes)*time.Minute)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Created(c, gin.H{"enrollment_code": code, "expires_at": expiresAt})
}

func (h *Handler) AdminRotateCredential(c *gin.Context) {
	resellerID, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	productID, ok := positiveParam(c, "product_id")
	if !ok {
		return
	}
	credential, err := h.service.RotateCredential(c.Request.Context(), resellerID, productID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, credential)
}

func (h *Handler) AdminListSettlements(c *gin.Context) {
	resellerID, ok := positiveParam(c, "id")
	if !ok {
		return
	}
	h.listSettlements(c, resellerID)
}

type exchangeEnrollmentRequest struct {
	EnrollmentCode       string `json:"enrollment_code" binding:"required"`
	InstanceID           string `json:"instance_id" binding:"required"`
	ExpectedResellerID   int64  `json:"expected_reseller_id"`
	ReauthorizationMode  string `json:"reauthorization_mode"`
	PreviousRefreshToken string `json:"previous_refresh_token"`
}

func (h *Handler) ExchangeEnrollment(c *gin.Context) {
	var req exchangeEnrollmentRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	result, err := h.service.exchangeEnrollment(c.Request.Context(), req.EnrollmentCode, req.InstanceID, c.ClientIP(), req.ExpectedResellerID, req.ReauthorizationMode, req.PreviousRefreshToken)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, result)
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
	InstanceID   string `json:"instance_id" binding:"required"`
}

func (h *Handler) RefreshAccessToken(c *gin.Context) {
	var req refreshTokenRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	access, refresh, expiresIn, err := h.service.RefreshAccessToken(c.Request.Context(), req.RefreshToken, req.InstanceID, c.ClientIP())
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, gin.H{"access_token": access, "refresh_token": refresh, "expires_in": expiresIn})
}

func (h *Handler) Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(c.GetHeader("Authorization"))
		parts := strings.SplitN(raw, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.Unauthorized(c, "reseller authorization required")
			c.Abort()
			return
		}
		claims, err := h.service.ValidateAccessToken(c.Request.Context(), parts[1], c.ClientIP())
		if err != nil {
			h.writeError(c, err)
			c.Abort()
			return
		}
		c.Set(resellerIDContextKey, claims.ResellerID)
		c.Next()
	}
}

func (h *Handler) Catalog(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	catalog, err := h.service.Catalog(c.Request.Context(), resellerID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	etag, err := catalogETag(*catalog)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("ETag", etag)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	response.Success(c, catalog)
}

type rotateCredentialRequest struct {
	ProductID int64 `json:"product_id" binding:"required"`
}

func (h *Handler) RotateCredential(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	var req rotateCredentialRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "invalid request")
		return
	}
	credential, err := h.service.RotateCredential(c.Request.Context(), resellerID, req.ProductID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, credential)
}

func (h *Handler) ListSettlements(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	h.listSettlements(c, resellerID)
}

func (h *Handler) listSettlements(c *gin.Context, resellerID int64) {
	afterID, _ := strconv.ParseInt(c.DefaultQuery("after_id", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	result, err := h.service.ListSettlements(c.Request.Context(), resellerID, afterID, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) GetSettlement(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	result, err := h.service.GetSettlement(c.Request.Context(), resellerID, c.Param("request_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) Balance(c *gin.Context) {
	resellerID, ok := resellerIDFromContext(c)
	if !ok {
		return
	}
	result, err := h.service.Balance(c.Request.Context(), resellerID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, result)
}

func (h *Handler) GatewayMiddleware() gin.HandlerFunc {
	if h == nil || h.service == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		if err := h.service.BeginGatewayRequest(c); err != nil {
			status := http.StatusInternalServerError
			message := "reseller request rejected"
			switch {
			case errors.Is(err, ErrDisabled):
				status = http.StatusServiceUnavailable
				message = "reseller protocol is disabled"
			case errors.Is(err, ErrDuplicateRequest):
				status = http.StatusConflict
				message = err.Error()
			case errors.Is(err, ErrInvalidInput):
				status = http.StatusBadRequest
				message = err.Error()
			case errors.Is(err, ErrForbidden), errors.Is(err, ErrUnauthorized), errors.Is(err, ErrInsufficientBalance):
				status = http.StatusForbidden
				message = err.Error()
			}
			c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"type": "reseller_protocol_error", "message": message}})
			return
		}
		c.Next()
		h.service.FinishGatewayRequest(c)
	}
}

func (h *Handler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrDisabled):
		response.Error(c, http.StatusServiceUnavailable, "reseller protocol is disabled")
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrEnrollmentInvalid):
		response.Unauthorized(c, err.Error())
	case errors.Is(err, ErrForbidden):
		response.Forbidden(c, err.Error())
	case errors.Is(err, ErrNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, ErrDuplicateRequest):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidInput):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "reseller request failed")
	}
}

func positiveParam(c *gin.Context, name string) (int64, bool) {
	value, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || value <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return value, true
}

func resellerIDFromContext(c *gin.Context) (int64, bool) {
	value, exists := c.Get(resellerIDContextKey)
	resellerID, ok := value.(int64)
	if !exists || !ok || resellerID <= 0 {
		response.Unauthorized(c, "reseller authorization required")
		return 0, false
	}
	return resellerID, true
}
