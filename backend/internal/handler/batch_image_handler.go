package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type BatchImageHandler struct {
	service   *service.BatchImagePublicService
	download  *service.BatchImageDownloadService
	cleanup   *service.BatchImageCleanupService
	auditGate SecurityAuditGate
}

func NewBatchImageHandler(service *service.BatchImagePublicService, download *service.BatchImageDownloadService, cleanup *service.BatchImageCleanupService, auditGates ...SecurityAuditGate) *BatchImageHandler {
	var auditGate SecurityAuditGate
	if len(auditGates) > 0 {
		auditGate = auditGates[0]
	}
	return &BatchImageHandler{service: service, download: download, cleanup: cleanup, auditGate: auditGate}
}

func (h *BatchImageHandler) Submit(c *gin.Context) {
	var req service.BatchImageSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		batchImageError(c, batchImageSubmitBindError(requestLogger(c, "handler.batch_image.submit"), c.Request, err))
		return
	}
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	apiKey, _ := middleware.GetAPIKeyFromContext(c)
	if !checkKeyModelCreation(c, apiKey, []string{req.Model}) {
		return
	}
	if !h.checkSecurityAuditBeforeSubmit(c, &req) {
		return
	}
	if sessionID := service.ExtractClientSessionID(c); sessionID != "" {
		req.SessionID = &sessionID
	}
	got, err := h.service.Submit(c.Request.Context(), owner, req, c.GetHeader("Idempotency-Key"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

// Do not collapse a truncated/oversized upload into an item validation error.
// The decoder may have consumed only part of a reference-heavy request, so its
// partial values and raw error must never be included in operator diagnostics.
func batchImageSubmitBindError(reqLog *zap.Logger, req *http.Request, err error) error {
	status := http.StatusBadRequest
	code := "BATCH_IMAGE_INVALID_REQUEST"
	message := "Batch request fields have invalid types; use an items array, string prompts, and integer output_count values"
	kind := "invalid_request"
	fields := []zap.Field{}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var base64Err base64.CorruptInputError
	switch {
	case errors.As(err, &syntaxErr):
		code, kind = "BATCH_IMAGE_INVALID_JSON", "json_syntax"
		message = "Batch request must be valid JSON"
		fields = append(fields, zap.Int64("offset", syntaxErr.Offset))
	case errors.As(err, &base64Err):
		code, kind = "BATCH_IMAGE_INVALID_REFERENCE_IMAGE", "reference_base64"
		message = "Reference image data must be valid base64 without a data URL prefix"
	case errors.As(err, &typeErr):
		kind = "json_type"
		fields = append(fields, zap.Int64("offset", typeErr.Offset))
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		code, kind = "BATCH_IMAGE_INVALID_JSON", "incomplete_json"
		message = "Batch request body is empty or incomplete; send a complete JSON object"
	default:
		if maxErr, ok := extractMaxBytesError(err); ok {
			status, code, kind = http.StatusRequestEntityTooLarge, "BATCH_IMAGE_REQUEST_TOO_LARGE", "max_bytes"
			message = buildBodyTooLargeMessage(maxErr.Limit) + "; use shared_reference_images for shared inputs, reduce image sizes, or split the batch into smaller tasks"
			fields = append(fields, zap.Int64("limit_bytes", maxErr.Limit))
		} else if requestBodyReadErrorKind(err) != "io_read" {
			code, kind = "BATCH_IMAGE_REQUEST_READ_FAILED", requestBodyReadErrorKind(err)
			message = "Unable to read the complete batch request; retry the upload"
		}
	}
	if reqLog != nil {
		contentLength := int64(-1)
		contentEncoding := "identity"
		if req != nil {
			contentLength = req.ContentLength
			contentEncoding = requestContentEncodingCategory(req.Header.Get("Content-Encoding"))
		}
		fields = append(fields, zap.String("error_kind", kind), zap.Int64("content_length", contentLength), zap.String("content_encoding", contentEncoding))
		reqLog.Warn("batch image request binding failed", fields...)
	}
	return infraerrors.New(status, code, message)
}

func (h *BatchImageHandler) checkSecurityAuditBeforeSubmit(c *gin.Context, req *service.BatchImageSubmitRequest) bool {
	if h == nil || req == nil {
		return true
	}
	if h.auditGate == nil {
		batchImageError(c, errMissingSecurityAuditGate)
		return false
	}
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusInternalServerError, "USER_CONTEXT_REQUIRED", "User context not found"))
		return false
	}
	items := make([]map[string]string, 0, len(req.Items))
	for _, item := range req.Items {
		if prompt := strings.TrimSpace(item.Prompt); prompt != "" {
			items = append(items, map[string]string{"prompt": prompt})
		}
	}
	if len(items) == 0 {
		return true
	}
	body, err := json.Marshal(map[string]any{"request": map[string]any{"items": items}})
	if err != nil {
		batchImageError(c, infraerrors.New(http.StatusBadRequest, "INVALID_BATCH_PROMPT", "batch prompts are invalid"))
		return false
	}
	reqLog := requestLogger(c, "handler.batch_image.security_audit",
		zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID), zap.String("model", req.Model))
	decision := h.auditGate.Check(c, reqLog, apiKey, subject, service.ContentModerationProtocolOpenAIImages, req.Model, body)
	if decision != nil && !decision.AllowNextStage {
		writeOpenAISecurityAuditError(c, decision)
		return false
	}
	return true
}

func (h *BatchImageHandler) Get(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.Get(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) List(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	got, err := h.service.List(c.Request.Context(), owner, service.BatchImageJobsQuery{
		Status:     c.Query("status"),
		TaskName:   c.Query("task_name"),
		Downloaded: c.Query("downloaded"),
		From:       c.Query("from"),
		To:         c.Query("to"),
		Limit:      limit,
		Cursor:     c.Query("cursor"),
	})
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Models(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.ListModels(c.Request.Context(), owner)
	if err != nil {
		batchImageError(c, err)
		return
	}
	// 分组级模型白名单：ListModels 已按分组账号枚举，这里再按白名单过滤一次。
	if apiKey, ok := middleware.GetAPIKeyFromContext(c); ok && apiKey != nil && apiKey.Group != nil {
		got.Data = filterBatchImageModelsByAllowlist(got.Data, apiKey.Group.ModelAllowlist)
	}
	if apiKey, ok := middleware.GetAPIKeyFromContext(c); ok {
		got.Data = filterBatchImageModelsByKeyPolicy(got.Data, apiKey)
	}
	c.JSON(http.StatusOK, got)
}

func filterBatchImageModelsByKeyPolicy(models []service.BatchImagePublicModel, apiKey *service.APIKey) []service.BatchImagePublicModel {
	if !apiKey.KeyModelPolicyEnabled() {
		return models
	}
	filtered := make([]service.BatchImagePublicModel, 0, len(models))
	for _, model := range models {
		if apiKey.AllowsPublicModel(model.ID) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

// filterBatchImageModelsByAllowlist 按分组模型白名单过滤批量生图模型列表。
// 白名单未开启时原样返回。
func filterBatchImageModelsByAllowlist(models []service.BatchImagePublicModel, allowlist service.GroupModelAllowlist) []service.BatchImagePublicModel {
	if !allowlist.Enabled {
		return models
	}
	filtered := make([]service.BatchImagePublicModel, 0, len(models))
	for _, model := range models {
		if allowlist.Allows(model.ID) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (h *BatchImageHandler) Items(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	got, err := h.service.ListItems(c.Request.Context(), owner, c.Param("id"), service.BatchImageItemsQuery{
		Status: c.Query("status"),
		Limit:  limit,
		Cursor: c.Query("cursor"),
	})
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) Cancel(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.service.Cancel(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func (h *BatchImageHandler) ItemContent(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	imageIndex := 0
	if raw := c.Query("image_index"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			batchImageError(c, service.ErrBatchImageItemImageIndexOutOfRange)
			return
		}
		imageIndex = parsed
	}
	stream, err := h.download.OpenItemContent(c.Request.Context(), owner, c.Param("id"), c.Param("custom_id"), imageIndex)
	if err != nil {
		batchImageError(c, err)
		return
	}
	defer func() { _ = stream.Reader.Close() }()

	c.Header("Content-Type", stream.ContentType)
	c.Header("Content-Disposition", service.BatchImageContentDispositionAttachment(stream.Filename))
	c.Header("Cache-Control", "private, max-age=300")
	c.Header("X-Content-Type-Options", "nosniff")
	if stream.ContentLength != nil && *stream.ContentLength >= 0 {
		c.Header("Content-Length", strconv.FormatInt(*stream.ContentLength, 10))
	}
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, stream.Reader); err != nil {
		return
	}
	h.markDownloadedBestEffort(c, owner)
}

// markDownloadedBestEffort 在响应体已写出后标记下载状态；
// 此时无法再向客户端返回错误，失败只能记日志（不能静默丢弃）。
func (h *BatchImageHandler) markDownloadedBestEffort(c *gin.Context, owner service.BatchImageOwner) {
	if err := h.service.MarkDownloaded(c.Request.Context(), owner, c.Param("id")); err != nil {
		logger.L().Warn("batch_image.mark_downloaded_failed",
			zap.String("batch_id", c.Param("id")),
			zap.Error(err),
		)
	}
}

func (h *BatchImageHandler) Download(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	maxItems, _ := strconv.Atoi(c.Query("max_items"))

	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", service.BatchImageContentDispositionAttachment(c.Param("id")+".zip"))
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	result, err := h.download.StreamZip(c.Request.Context(), owner, c.Param("id"), service.BatchImageZipOptions{
		Status:          c.Query("status"),
		MaxItems:        maxItems,
		IncludeManifest: true,
	}, c.Writer)
	if err != nil {
		if result == nil || !c.Writer.Written() {
			batchImageError(c, err)
		}
		return
	}
	h.markDownloadedBestEffort(c, owner)
}

func (h *BatchImageHandler) DeleteRecord(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	if err := h.service.DeleteRecord(c.Request.Context(), owner, c.Param("id")); err != nil {
		batchImageError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *BatchImageHandler) DeleteOutputs(c *gin.Context) {
	owner, ok := batchImageOwnerFromContext(c)
	if !ok {
		batchImageError(c, infraerrors.New(http.StatusUnauthorized, "API_KEY_REQUIRED", "API key is required"))
		return
	}
	got, err := h.cleanup.DeleteOutputsForOwner(c.Request.Context(), owner, c.Param("id"))
	if err != nil {
		batchImageError(c, err)
		return
	}
	c.JSON(http.StatusOK, got)
}

func batchImageOwnerFromContext(c *gin.Context) (service.BatchImageOwner, bool) {
	apiKey, ok := middleware.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.ID <= 0 || apiKey.UserID <= 0 {
		return service.BatchImageOwner{}, false
	}
	return service.BatchImageOwner{
		UserID:   apiKey.UserID,
		APIKeyID: apiKey.ID,
		GroupID:  apiKey.GroupID,
	}, true
}

func batchImageError(c *gin.Context, err error) {
	status := infraerrors.Code(err)
	code := infraerrors.Reason(err)
	message := infraerrors.Message(err)
	if err == nil {
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
		message = "internal error"
	}
	if status == 0 || (status == http.StatusInternalServerError && strings.TrimSpace(code) == "") {
		status = http.StatusInternalServerError
		code = "INTERNAL_ERROR"
		message = "internal error"
	}
	if errors.Is(err, service.ErrBatchImageJobNotFound) {
		status = http.StatusNotFound
		code = "BATCH_IMAGE_NOT_FOUND"
		message = "batch image job not found"
	}
	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    "invalid_request_error",
			"code":    code,
			"message": message,
		},
	})
}
