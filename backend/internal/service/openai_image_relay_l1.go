package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
)

var errImageRelayResponse = errors.New("invalid main site image relay response")

var errImageRelayHandled = errors.New("image relay response handled")

type imageRelayOutbound struct {
	signer       *imagerelay.Signer
	binding      imagerelay.Request
	reservation  *imagerelay.Reservation
	resultLease  *imagerelay.ResultReservation
	runtime      *imageRelayRuntime
	operation    *imageRelayOperationContext
	headerFilter *responseheaders.CompiledHeaderFilter
}

// prepareImageRelayL1 signs the actual request built for the main site and
// reserves the space needed to receive an image result before dispatch.
func (s *OpenAIGatewayService) prepareImageRelayL1(ctx context.Context, c *gin.Context, req *http.Request, account *Account, parsed *OpenAIImagesRequest, body []byte) (*imageRelayOutbound, error) {
	imagerelay.StripHeaders(req.Header)
	runtime := s.imageRelayRuntime()
	if runtime == nil || runtime.contract.Role != "l1" || account == nil ||
		account.ID != runtime.contract.L1AccountID || !isSynchronousImageRelayRequest(c, parsed) {
		return nil, nil
	}
	if runtime.err != nil {
		return nil, errors.New("image relay storage unavailable")
	}
	if ImagesURLToB64JSONEnabled(account) && parsed.ResponseFormat != "url" {
		return nil, nil
	}
	if account.ProxyID != nil || account.Proxy != nil || !imageRelayMatchesMainOrigin(req, runtime.contract.MainOrigin) {
		return nil, errors.New("image relay account target does not match configured main site")
	}
	if len(body) == 0 || req.ContentLength != int64(len(body)) {
		return nil, errors.New("image relay request body does not match framing")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if (runtime.spool == nil && runtime.results == nil) || runtime.signer == nil {
		return nil, errors.New("image relay runtime is unavailable")
	}
	budget := min(resolveUpstreamResponseReadLimit(s.cfg), int64(imagerelay.MaxResultBodyBytes))
	outbound := &imageRelayOutbound{
		signer:       runtime.signer,
		headerFilter: s.responseHeaderFilter,
	}
	id, err := s.reserveImageRelayL1(c, runtime, parsed, budget, outbound)
	if err != nil {
		return nil, err
	}
	binding := imagerelay.Binding{
		OperationID:  id,
		Source:       runtime.contract.SourceSite,
		Destination:  runtime.contract.DestinationSite,
		MainAPIKeyID: runtime.contract.MainAPIKeyID,
		Method:       req.Method,
		Path:         req.URL.EscapedPath(),
		ContentType:  req.Header.Get("Content-Type"),
		BodySHA256:   imagerelay.SHA256Hex(body),
	}
	outbound.binding, err = runtime.signer.SignRequest(req.Header, binding, time.Now())
	if err != nil {
		outbound.close()
		return nil, err
	}

	*req = *req.WithContext(WithHTTPUpstreamRedirectsDisabled(req.Context()))
	req.GetBody = nil
	req.Header.Set("Accept-Encoding", "identity")
	return outbound, nil
}

func imageRelayMatchesMainOrigin(req *http.Request, rawOrigin string) bool {
	origin, err := url.Parse(rawOrigin)
	if err != nil || origin.Host == "" || (origin.Scheme != "https" && origin.Scheme != "http") ||
		origin.User != nil || origin.Opaque != "" || (origin.Path != "" && origin.Path != "/") ||
		origin.RawPath != "" || origin.ForceQuery || origin.RawQuery != "" || origin.Fragment != "" {
		return false
	}
	target := req.URL
	return target != nil && target.User == nil && target.Opaque == "" &&
		!target.ForceQuery && target.RawQuery == "" && target.Fragment == "" &&
		strings.EqualFold(target.Scheme, origin.Scheme) && strings.EqualFold(target.Host, origin.Host) &&
		(req.Host == "" || strings.EqualFold(req.Host, origin.Host))
}

func (s *OpenAIGatewayService) reserveImageRelayL1(c *gin.Context, runtime *imageRelayRuntime, parsed *OpenAIImagesRequest, budget int64, outbound *imageRelayOutbound) (string, error) {
	if runtime.results == nil {
		reservation, err := runtime.spool.Reserve(budget)
		if err != nil {
			return "", err
		}
		outbound.reservation = reservation
		id, err := imagerelay.NewOperationID()
		if err != nil {
			reservation.Close()
		}
		return id, err
	}
	apiKey := getAPIKeyFromContext(c)
	if apiKey == nil {
		return "", errors.New("image relay customer key unavailable")
	}
	keys := c.Request.Header.Values("Idempotency-Key")
	if len(keys) > 1 || (len(keys) == 1 && (len(keys[0]) == 0 || len(keys[0]) > 128 || strings.Contains(keys[0], ","))) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "Invalid Idempotency-Key", "type": "invalid_request_error"}})
		return "", errImageRelayHandled
	}
	idempotencyKey := c.GetHeader("Idempotency-Key")
	owner := ImageRelayOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}
	fingerprint := imageRelayRequestFingerprint(parsed.Endpoint, parsed.ContentType, parsed.Body)
	op, dispatch, err := runtime.journal.Begin(owner, idempotencyKey, fingerprint, time.Now())
	if err != nil {
		if errors.Is(err, ErrImageRelayOperationConflict) || errors.Is(err, ErrImageRelayOperationInvalid) {
			status := http.StatusConflict
			if errors.Is(err, ErrImageRelayOperationInvalid) {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": gin.H{"message": err.Error(), "type": "invalid_request_error"}})
			return "", errImageRelayHandled
		}
		return "", err
	}
	c.Header(ImageRelayOperationHeader, op.ID)
	if !dispatch {
		status, recoverErr := s.recoverImageRelay(c, runtime, owner, op)
		if status != 0 && !c.Writer.Written() {
			c.JSON(status, gin.H{"error": gin.H{"message": recoverErr.Error(), "type": "api_error"}})
		}
		return "", errImageRelayHandled
	}
	reservation, err := runtime.results.Reserve(op.ID, owner.APIKeyID, budget)
	if err != nil {
		_ = runtime.journal.MarkFailed(owner, op.ID, time.Now())
		return "", err
	}
	outbound.resultLease = reservation
	outbound.runtime = runtime
	outbound.operation = &imageRelayOperationContext{owner: owner, op: op}
	return op.ID, nil
}

func (r *imageRelayOutbound) storeResult(ctx context.Context, body io.Reader, receipt imagerelay.Metadata, contentType string) (io.ReadCloser, error) {
	x := r.operation
	if err := r.resultLease.Save(ctx, body, imagerelay.ResultMetadata{Status: http.StatusOK, ContentType: contentType,
		BodyBytes: receipt.BodyBytes, BodySHA256: receipt.BodySHA256}); err != nil {
		return nil, err
	}
	if err := r.runtime.journal.MarkResult(x.owner, x.op.ID, ImageRelayStoredResult{BodyBytes: receipt.BodyBytes,
		BodySHA256: receipt.BodySHA256, ContentType: contentType}, time.Now()); err != nil {
		return nil, err
	}
	return r.runtime.results.Open(ctx, x.op.ID, x.owner.APIKeyID)
}

func (r *imageRelayOutbound) close() error {
	if r.resultLease != nil {
		return r.resultLease.Close()
	}
	return r.reservation.Close()
}

func (r *imageRelayOutbound) receive(ctx context.Context, response *http.Response, model string) (usage OpenAIUsage, count int, sizes []string, delivery *imageRelayDelivery, err error) {
	receipt, err := r.signer.VerifyResult(response.Header, r.binding, time.Now())
	imagerelay.StripHeaders(response.Header)
	if err != nil {
		return usage, 0, nil, nil, fmt.Errorf("%w: %w", errImageRelayResponse, err)
	}
	contentType := response.Header.Values("Content-Type")
	var mediaType string
	if len(contentType) == 1 {
		mediaType, _, err = mime.ParseMediaType(contentType[0])
	}
	encoding := response.Header.Values("Content-Encoding")
	if response.StatusCode != http.StatusOK || response.Uncompressed || err != nil ||
		len(contentType) != 1 || mediaType != "application/json" || len(encoding) > 1 ||
		(len(encoding) == 1 && !strings.EqualFold(encoding[0], "identity")) ||
		len(response.TransferEncoding) != 0 || response.ContentLength != receipt.BodyBytes ||
		receipt.Model != model || response.Body == nil {
		return usage, 0, nil, nil, errImageRelayResponse
	}

	var file io.ReadCloser
	if r.resultLease != nil {
		file, err = r.storeResult(ctx, response.Body, receipt, contentType[0])
	} else {
		file, err = r.reservation.Receive(ctx, response.Body, receipt.BodyBytes, receipt.BodySHA256)
	}
	if err != nil {
		return usage, 0, nil, nil, fmt.Errorf("%w: %w", errImageRelayResponse, err)
	}
	usage = OpenAIUsage{
		InputTokens:                     receipt.Usage.InputTokens,
		ImageInputTokens:                receipt.Usage.ImageInputTokens,
		ImageCacheReadTokens:            receipt.Usage.ImageCacheReadTokens,
		OutputTokens:                    receipt.Usage.OutputTokens,
		CacheCreationInputTokens:        receipt.Usage.CacheCreationInputTokens,
		CacheCreation5mTokens:           receipt.Usage.CacheCreation5mTokens,
		CacheCreation1hTokens:           receipt.Usage.CacheCreation1hTokens,
		CacheCreation30mInputTokens:     receipt.Usage.CacheCreation30mInputTokens,
		CacheCreation12hInputTokens:     receipt.Usage.CacheCreation12hInputTokens,
		CacheReadInputTokens:            receipt.Usage.CacheReadInputTokens,
		ImageOutputTokens:               receipt.Usage.ImageOutputTokens,
		cacheCreationInputTokensPresent: receipt.Usage.CacheCreationInputTokensPresent,
	}
	delivery = &imageRelayDelivery{
		file:          file,
		contentLength: receipt.BodyBytes,
		contentType:   contentType[0],
		status:        response.StatusCode,
		header:        responseheaders.FilterHeaders(response.Header, r.headerFilter),
	}
	if r.operation != nil {
		delivery.op = &r.operation.op
	}
	return usage, receipt.ImageCount, receipt.ImageSizes, delivery, nil
}

type imageRelayDelivery struct {
	file          io.ReadCloser
	contentLength int64
	contentType   string
	status        int
	header        http.Header
	receipt       string
	op            *ImageRelayOperation
}

func (d *imageRelayDelivery) close() error {
	return d.file.Close()
}

// serve delivers a fully verified result after the local billing boundary.
func (d *imageRelayDelivery) serve(c *gin.Context) error {
	for key, values := range d.header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}
	imagerelay.StripHeaders(c.Writer.Header())
	if d.receipt != "" {
		c.Header(imagerelay.ResultHeader, d.receipt)
	}
	if d.op != nil && d.receipt == "" {
		c.Header(ImageRelayOperationHeader, d.op.ID)
		c.Header("Cache-Control", "private, no-store")
	}
	c.Header("Content-Type", d.contentType)
	c.Header("Content-Length", strconv.FormatInt(d.contentLength, 10))
	c.Writer.WriteHeader(d.status)
	written, err := io.CopyN(c.Writer, d.file, d.contentLength)
	if err == nil && written != d.contentLength {
		err = io.ErrUnexpectedEOF
	}
	return err
}
