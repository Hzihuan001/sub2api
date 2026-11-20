package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/imagerelay"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

type imageRelayRuntime struct {
	contract config.ImageRelayConfig
	signer   *imagerelay.Signer
	spool    *imagerelay.Spool
	journal  *imageRelayJournal
	results  *imagerelay.ResultStore
	err      error
}

func (s *OpenAIGatewayService) startImageRelayMaintenance() {
	if s.cfg == nil || s.cfg.Gateway.ImageRelay.Role == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.imageRelayMaintenanceCancel = cancel
	s.imageRelayMaintenanceDone = make(chan struct{})
	go func() {
		defer close(s.imageRelayMaintenanceDone)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		r := s.imageRelayRuntime()
		if r.err != nil {
			logger.LegacyPrintf("service.image_relay", "Persistent image relay storage unavailable: %v", r.err)
			return
		}
		for {
			attemptCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			_, err := s.RecoverImageRelaySettlements(attemptCtx, 8)
			stop()
			if err != nil && ctx.Err() == nil {
				logger.LegacyPrintf("service.image_relay", "Image relay settlement recovery deferred: %v", err)
			}
			_, _ = r.journal.Prune(time.Now())
			_, _ = r.results.Prune(time.Now())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *OpenAIGatewayService) StopImageRelayMaintenance() error {
	if s.imageRelayMaintenanceCancel == nil {
		return nil
	}
	s.imageRelayMaintenanceCancel()
	<-s.imageRelayMaintenanceDone
	r := s.imageRelay
	if r == nil {
		return nil
	}
	var resultErr, journalErr error
	if r.results != nil {
		resultErr = r.results.Close()
	}
	if r.journal != nil {
		journalErr = r.journal.Close()
	}
	return errors.Join(resultErr, journalErr)
}

const (
	imageRelaySpoolFiles = 18
	imageRelayDiskFloor  = 8 << 30
	imageRelayRetention  = 15 * time.Minute
)

func (s *OpenAIGatewayService) imageRelayRuntime() *imageRelayRuntime {
	if s == nil || s.cfg == nil || s.cfg.Gateway.ImageRelay.Role == "" {
		return nil
	}
	s.imageRelayOnce.Do(func() {
		cfg := s.cfg.Gateway.ImageRelay
		r := &imageRelayRuntime{contract: cfg}
		s.imageRelay = r
		secret, err := hex.DecodeString(cfg.SecretHex)
		if err != nil || len(secret) != 32 {
			r.err = errors.New("invalid image relay key")
			return
		}
		r.signer, r.err = imagerelay.NewSigner(cfg.KeyID, map[string][]byte{cfg.KeyID: secret})
		if r.err != nil {
			return
		}
		r.journal, r.err = newImageRelayJournal(cfg.PrivateDir)
		if r.err != nil {
			return
		}
		maxResponseBytes := min(resolveUpstreamResponseReadLimit(s.cfg), int64(imagerelay.MaxResultBodyBytes))
		const retainedFiles = 256
		r.results, r.err = imagerelay.NewResultStore(filepath.Join(cfg.PrivateDir, "results"), retainedFiles,
			(1<<30)+imageRelaySpoolFiles*(maxResponseBytes+(17<<10)), imageRelayDiskFloor, imageRelayRetention)
	})
	return s.imageRelay
}

type imageRelayInboundKey struct{}

type imageRelayInboundOperationKey struct{}

type imageRelayOperationContext struct {
	owner ImageRelayOwner
	op    ImageRelayOperation
}

const ImageRelayOperationHeader = "X-Image-Operation-Id"

func (s *OpenAIGatewayService) recoverImageRelay(c *gin.Context, r *imageRelayRuntime, owner ImageRelayOwner, op ImageRelayOperation) (int, error) {
	c.Header(ImageRelayOperationHeader, op.ID)
	c.Header("Cache-Control", "private, no-store")
	if op.State == ImageRelayResultReady {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 12*time.Second)
		defer cancel()
		if err := s.finishImageRelaySettlement(ctx, r, op); err == nil {
			op, _ = r.journal.Lookup(owner, op.ID)
		}
	}
	switch op.State {
	case ImageRelaySettled:
	case ImageRelayExpired:
		return http.StatusGone, errors.New("image result expired")
	case ImageRelayFailed:
		return http.StatusConflict, errors.New("image operation failed")
	default:
		c.Header("Retry-After", "5")
		return http.StatusConflict, errors.New("image operation is pending or delivery is uncertain")
	}
	stored, err := r.results.Open(c.Request.Context(), op.ID, owner.APIKeyID)
	if errors.Is(err, imagerelay.ErrResultNotFound) {
		return http.StatusGone, errors.New("image result expired")
	}
	if err != nil {
		return http.StatusServiceUnavailable, errors.New("image result could not be read")
	}
	defer stored.Close()
	delivery := imageRelayDelivery{file: stored, contentType: stored.Metadata.ContentType,
		contentLength: stored.Metadata.BodyBytes, status: http.StatusOK}
	return 0, delivery.serve(c)
}

func (s *OpenAIGatewayService) RecoverImageRelayResult(c *gin.Context, key *APIKey, operationID string) (int, error) {
	r := s.imageRelayRuntime()
	if key == nil || r == nil || r.contract.Role != "l1" {
		return http.StatusNotFound, errors.New("image operation not found")
	}
	if r.err != nil {
		return http.StatusServiceUnavailable, errors.New("image result storage unavailable")
	}
	owner := ImageRelayOwner{UserID: key.UserID, APIKeyID: key.ID}
	op, err := r.journal.Lookup(owner, operationID)
	if err != nil {
		return http.StatusNotFound, errors.New("image operation not found")
	}
	return s.recoverImageRelay(c, r, owner, op)
}

func (s *OpenAIGatewayService) storeMainImageRelay(c *gin.Context, resp *http.Response, parsed *OpenAIImagesRequest, body []byte, usage OpenAIUsage, count int, sizes []string) (*imageRelayDelivery, error) {
	r := s.imageRelayRuntime()
	x, ok := c.Request.Context().Value(imageRelayInboundOperationKey{}).(imageRelayOperationContext)
	if !ok || r.results == nil {
		return nil, errors.New("image relay storage unavailable")
	}
	if err := s.signImageRelayResult(c, resp.StatusCode, parsed, body, usage, count, sizes); err != nil {
		return nil, err
	}
	signed := c.Writer.Header().Get(imagerelay.ResultHeader)
	imagerelay.StripHeaders(c.Writer.Header())
	reservation, ok := c.Request.Context().Value(imageRelayInboundReservationKey{}).(*imagerelay.ResultReservation)
	if !ok {
		return nil, errors.New("image relay reservation missing")
	}
	ctx := context.WithoutCancel(c.Request.Context())
	metadata := imagerelay.ResultMetadata{Status: http.StatusOK, ContentType: "application/json", BodyBytes: int64(len(body)), BodySHA256: imagerelay.SHA256Hex(body)}
	if err := reservation.Save(ctx, bytes.NewReader(body), metadata); err != nil {
		return nil, err
	}
	if err := r.journal.MarkResult(x.owner, x.op.ID, ImageRelayStoredResult{BodyBytes: metadata.BodyBytes, BodySHA256: metadata.BodySHA256, ContentType: metadata.ContentType}, time.Now()); err != nil {
		return nil, err
	}
	stored, err := r.results.Open(ctx, x.op.ID, x.owner.APIKeyID)
	if err != nil {
		return nil, err
	}
	return &imageRelayDelivery{file: stored, contentLength: metadata.BodyBytes, contentType: metadata.ContentType,
		status: http.StatusOK, receipt: signed, op: &x.op}, nil
}

type imageRelayInboundReservationKey struct{}

func isSynchronousImageRelayRequest(c *gin.Context, parsed *OpenAIImagesRequest) bool {
	if c == nil || c.Request == nil || parsed == nil || parsed.Stream || c.Request.Method != http.MethodPost {
		return false
	}
	path := c.FullPath()
	return path == parsed.Endpoint && (path == openAIImagesGenerationsEndpoint || path == openAIImagesEditsEndpoint)
}

// AuthorizeImageRelay attaches a private exchange only after normal API-key
// authentication. FullPath distinguishes the real synchronous ingress from a
// synthetic synchronous handler invocation by a workbench task.
func (s *OpenAIGatewayService) AuthorizeImageRelay(c *gin.Context, apiKey *APIKey, parsed *OpenAIImagesRequest, body []byte) (int, error) {
	if !isSynchronousImageRelayRequest(c, parsed) || !imagerelay.HasRequest(c.Request.Header) {
		return 0, nil
	}
	r := s.imageRelayRuntime()
	if r == nil || r.contract.Role != "main" {
		return 0, nil
	}
	cfg := r.contract
	peer, _, _ := net.SplitHostPort(c.Request.RemoteAddr)
	addr, _ := netip.ParseAddr(peer)
	if apiKey == nil || apiKey.ID != cfg.MainAPIKeyID || (!addr.IsPrivate() && !addr.IsLoopback()) {
		return http.StatusForbidden, errors.New("image relay authentication failed")
	}
	if r.err != nil || r.journal == nil {
		return http.StatusServiceUnavailable, errors.New("image relay unavailable")
	}
	now := time.Now()
	verified, err := r.signer.VerifyRequest(c.Request.Header, imagerelay.Binding{
		Source: cfg.SourceSite, Destination: cfg.DestinationSite, MainAPIKeyID: apiKey.ID,
		Method: http.MethodPost, Path: parsed.Endpoint, ContentType: c.Request.Header.Get("Content-Type"),
		BodySHA256: imagerelay.SHA256Hex(body),
	}, now)
	if err != nil {
		return http.StatusForbidden, errors.New("image relay authentication failed")
	}
	owner := ImageRelayOwner{UserID: apiKey.UserID, APIKeyID: apiKey.ID}
	fingerprint := imageRelayRequestFingerprint(verified.Path, verified.ContentType, body)
	op, dispatch, err := r.journal.BeginWithID(owner, verified.OperationID, fingerprint, now)
	if err != nil {
		return http.StatusServiceUnavailable, errors.New("image relay unavailable")
	}
	if !dispatch {
		return http.StatusConflict, errors.New("image relay operation already submitted")
	}
	ctx := context.WithValue(c.Request.Context(), imageRelayInboundKey{}, verified)
	c.Request = c.Request.WithContext(context.WithValue(ctx, imageRelayInboundOperationKey{}, imageRelayOperationContext{owner: owner, op: op}))
	return 0, nil
}

func inboundImageRelay(ctx context.Context) (imagerelay.Request, bool) {
	if ctx == nil {
		return imagerelay.Request{}, false
	}
	req, ok := ctx.Value(imageRelayInboundKey{}).(imagerelay.Request)
	return req, ok
}

func (s *OpenAIGatewayService) ImageRelayDirectResponse(c *gin.Context, account *Account, parsed *OpenAIImagesRequest) bool {
	if _, ok := inboundImageRelay(c.Request.Context()); ok {
		return true
	}
	return s.cfg != nil && isSynchronousImageRelayRequest(c, parsed) && account != nil &&
		s.cfg.Gateway.ImageRelay.Role == "l1" && account.ID == s.cfg.Gateway.ImageRelay.L1AccountID &&
		(!ImagesURLToB64JSONEnabled(account) || parsed.ResponseFormat == "url")
}

// ImageRelayPendingDelivery identifies a successful L1 result whose body was
// spooled, so the handler can finish accounting before completing delivery.
func (r *OpenAIForwardResult) ImageRelayPendingDelivery() bool {
	return r != nil && r.imageRelayDelivery != nil
}

func (r *OpenAIForwardResult) CloseImageRelayDelivery() error {
	return r.imageRelayDelivery.close()
}

func (r *OpenAIForwardResult) ServeImageRelayDelivery(c *gin.Context) error {
	return r.imageRelayDelivery.serve(c)
}

func (s *OpenAIGatewayService) signImageRelayResult(c *gin.Context, status int, parsed *OpenAIImagesRequest, body []byte, usage OpenAIUsage, count int, sizes []string) error {
	req, ok := inboundImageRelay(c.Request.Context())
	if !ok {
		return nil
	}
	if status != http.StatusOK || c.Writer.Written() {
		return errors.New("image relay response is unavailable")
	}
	return s.imageRelayRuntime().signer.SignResult(c.Writer.Header(), req, imagerelay.Metadata{
		BodyBytes: int64(len(body)), BodySHA256: imagerelay.SHA256Hex(body),
		ImageCount: count, ImageSizes: sizes, Model: parsed.Model,
		Usage: imagerelay.Usage{
			InputTokens: usage.InputTokens, ImageInputTokens: usage.ImageInputTokens,
			ImageCacheReadTokens: usage.ImageCacheReadTokens, OutputTokens: usage.OutputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens, CacheCreation5mTokens: usage.CacheCreation5mTokens,
			CacheCreation1hTokens: usage.CacheCreation1hTokens, CacheCreation30mInputTokens: usage.CacheCreation30mInputTokens,
			CacheCreation12hInputTokens: usage.CacheCreation12hInputTokens, CacheReadInputTokens: usage.CacheReadInputTokens,
			ImageOutputTokens: usage.ImageOutputTokens, CacheCreationInputTokensPresent: usage.cacheCreationInputTokensPresent,
		},
	}, time.Now())
}
