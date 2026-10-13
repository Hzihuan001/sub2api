package service

// App-managed image batching. This provider deliberately keeps the parent
// batch protocol and billing path unchanged, but executes each item as one
// ordinary OpenAI-compatible /images/generations request in the queue worker.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type BatchImageItemLookup interface {
	ListBatchImageItems(ctx context.Context, batchID string, filter BatchImageItemFilter) ([]*BatchImageItem, error)
}

// BatchImageProgressUpdater is optional so existing test doubles and custom
// repositories remain source-compatible. The production repository implements
// it to expose per-item progress while the fan-out worker is still running.
type BatchImageProgressUpdater interface {
	UpdateBatchImageItemProgress(ctx context.Context, batchID, customID, status, mimeType, fileExtension, errorCode, errorMessage string, imageCount int) error
	RefreshBatchImageJobCounts(ctx context.Context, batchID string) error
}

// managedBatchImageItemPayload is stored with each item so a queued job can be
// resumed after a restart without relying on the original HTTP request. The
// response format is kept per item for backwards compatibility with the
// existing batch item schema (which has no job-level image settings columns).
type managedBatchImageItemPayload struct {
	Item             BatchImageSubmitItem `json:"item"`
	ResponseMimeType string               `json:"response_mime_type,omitempty"`
	ImageSize        string               `json:"image_size,omitempty"`
	AspectRatio      string               `json:"aspect_ratio,omitempty"`
}

type AppManagedBatchImageProviderOptions struct {
	HTTPUpstream HTTPUpstream
	ItemRepo     BatchImageItemLookup
	ResultDir    string
	Concurrency  int
	Timeout      time.Duration
}

type appManagedBatchImageProvider struct {
	httpUpstream HTTPUpstream
	itemRepo     BatchImageItemLookup
	resultDir    string
	concurrency  int
	timeout      time.Duration
}

func NewAppManagedBatchImageProvider(opts AppManagedBatchImageProviderOptions) BatchImageProvider {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 3
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Minute
	}
	if strings.TrimSpace(opts.ResultDir) == "" {
		opts.ResultDir = "./data/batch-images"
	}
	return &appManagedBatchImageProvider{
		httpUpstream: opts.HTTPUpstream,
		itemRepo:     opts.ItemRepo,
		resultDir:    filepath.Clean(opts.ResultDir),
		concurrency:  opts.Concurrency,
		timeout:      opts.Timeout,
	}
}

func NewConfiguredAppManagedBatchImageProvider(cfg *config.Config, itemRepo BatchImageItemLookup, upstream HTTPUpstream) BatchImageProvider {
	opts := AppManagedBatchImageProviderOptions{ItemRepo: itemRepo, HTTPUpstream: upstream}
	if cfg != nil {
		opts.ResultDir = cfg.BatchImage.AppManagedResultDir
		opts.Concurrency = cfg.BatchImage.AppManagedConcurrency
		opts.Timeout = time.Duration(cfg.BatchImage.AppManagedRequestTimeoutSeconds) * time.Second
	}
	return NewAppManagedBatchImageProvider(opts)
}

func (p *appManagedBatchImageProvider) Name() string { return BatchImageProviderAppManaged }

func (p *appManagedBatchImageProvider) SupportsAccount(account *Account) bool {
	if account == nil || (account.Type != AccountTypeAPIKey && account.Type != AccountTypeUpstream) || managedAccountAPIKey(account) == "" {
		return false
	}
	if account.IsGemini() {
		return isBananaRouterGeminiAccount(account)
	}
	// OpenAI-compatible providers expose a base URL through this accessor. Grok
	// keeps a separate URL helper, but its API-key transport is still compatible
	// with the same image endpoints.
	return managedAccountBaseURL(account) != ""
}

func (p *appManagedBatchImageProvider) Submit(ctx context.Context, job *BatchImageJob, account *Account, _ BatchImageInput) (*BatchProviderJob, error) {
	if !p.SupportsAccount(account) {
		return nil, ErrBatchImageProviderUnsupportedAccount
	}
	if job == nil || strings.TrimSpace(job.BatchID) == "" {
		return nil, ErrBatchImageProviderInvalidInput
	}
	return &BatchProviderJob{ProviderJobName: "managed:" + job.BatchID, RawState: string(BatchProviderStateQueued)}, nil
}

func (p *appManagedBatchImageProvider) Get(ctx context.Context, job *BatchImageJob, account *Account) (*BatchProviderStatus, error) {
	if !p.SupportsAccount(account) {
		return nil, ErrBatchImageProviderUnsupportedAccount
	}
	if job == nil || strings.TrimSpace(job.BatchID) == "" {
		return nil, ErrBatchImageProviderInvalidInput
	}
	if _, err := os.Stat(p.cancelPath(job.BatchID)); err == nil {
		return &BatchProviderStatus{RawState: "CANCELLED", InternalState: BatchProviderStateCancelled, Done: true}, nil
	}
	path := p.resultPath(job.BatchID)
	if _, err := os.Stat(path); err == nil {
		return &BatchProviderStatus{RawState: "SUCCEEDED", InternalState: BatchProviderStateSucceeded, Done: true, ProviderOutputRef: path}, nil
	}
	if p.itemRepo == nil {
		return nil, ErrBatchImageProviderInvalidInput.WithCause(fmt.Errorf("managed provider item repository is unavailable"))
	}
	items, err := p.itemRepo.ListBatchImageItems(ctx, job.BatchID, BatchImageItemFilter{Limit: maxInt(job.ItemCount, 1000)})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrBatchImageProviderInvalidInput.WithCause(fmt.Errorf("batch has no items"))
	}
	if err := os.MkdirAll(p.resultDir, 0750); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	lines := make([][]byte, len(items))
	completed := loadManagedLines(path + ".partial")
	for i, item := range items {
		if item != nil {
			lines[i] = completed[item.CustomID]
			if len(lines[i]) > 0 {
				_ = p.persistManagedProgress(ctx, job.BatchID, item.CustomID, lines[i])
			}
		}
	}
	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	var writeMu sync.Mutex
	for i, item := range items {
		if item == nil {
			continue
		}
		wg.Add(1)
		go func(i int, item *BatchImageItem) {
			defer wg.Done()
			if len(lines[i]) > 0 {
				return
			}
			if _, err := os.Stat(p.cancelPath(job.BatchID)); err == nil {
				return
			}
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			var payload managedBatchImageItemPayload
			imageSize, responseMimeType, aspectRatio := "2K", "image/png", ""
			var in BatchImageSubmitItem
			if len(item.InputPayload) > 0 && json.Unmarshal(item.InputPayload, &payload) == nil && strings.TrimSpace(payload.Item.Prompt) != "" {
				in = payload.Item
				if strings.TrimSpace(payload.ImageSize) != "" {
					imageSize = payload.ImageSize
				}
				if strings.TrimSpace(payload.ResponseMimeType) != "" {
					responseMimeType = payload.ResponseMimeType
				}
				aspectRatio = strings.TrimSpace(payload.AspectRatio)
			} else if len(item.InputPayload) > 0 && json.Unmarshal(item.InputPayload, &in) == nil {
				// Legacy payloads stored the item directly.
			} else {
				in = BatchImageSubmitItem{CustomID: item.CustomID, Prompt: batchImageDerefString(item.PromptPreview), OutputCount: 1}
			}
			if in.CustomID == "" {
				in.CustomID = item.CustomID
			}
			line := p.generateOne(ctx, job, account, in, imageSize, aspectRatio, responseMimeType)
			// Context cancellation is a worker timeout, not a final item result.
			// Leave the item absent from the checkpoint so the next queue pass can
			// retry it instead of permanently recording a timeout failure.
			if ctx.Err() != nil {
				return
			}
			lines[i] = line
			_ = p.persistManagedProgress(ctx, job.BatchID, item.CustomID, line)
			if len(line) > 0 {
				writeMu.Lock()
				_ = appendManagedLine(path+".partial", line)
				writeMu.Unlock()
			}
		}(i, item)
	}
	wg.Wait()
	if _, err := os.Stat(p.cancelPath(job.BatchID)); err == nil {
		return &BatchProviderStatus{RawState: "CANCELLED", InternalState: BatchProviderStateCancelled, Done: true}, nil
	}
	// Never publish a partial result as a successful batch. A timeout can leave
	// some requests finished and others still running; returning a partial
	// JSONL would let settlement treat the missing items as unaccounted-for.
	// Keep the partial checkpoint and let the queue retry only the unfinished
	// items on the next worker pass.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, line := range lines {
		if len(line) == 0 {
			return nil, fmt.Errorf("managed batch fan-out incomplete: an item has no result")
		}
	}
	tmp := path + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	w := bufio.NewWriter(f)
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		_, _ = w.Write(line)
		_ = w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	_ = os.Remove(path + ".partial")
	return &BatchProviderStatus{RawState: "SUCCEEDED", InternalState: BatchProviderStateSucceeded, Done: true, ProviderOutputRef: path}, nil
}

func (p *appManagedBatchImageProvider) persistManagedProgress(ctx context.Context, batchID, customID string, line []byte) error {
	updater, ok := p.itemRepo.(BatchImageProgressUpdater)
	if !ok || updater == nil || len(line) == 0 {
		return nil
	}
	parsed, err := ParseBatchImageResultLine(line, 0)
	if err != nil {
		return err
	}
	if strings.TrimSpace(parsed.CustomID) == "" {
		parsed.CustomID = customID
	}
	if err := updater.UpdateBatchImageItemProgress(ctx, batchID, parsed.CustomID, parsed.Status, parsed.MimeType, parsed.FileExtension, parsed.ErrorCode, parsed.ErrorMessage, parsed.ImageCount); err != nil {
		return err
	}
	return updater.RefreshBatchImageJobCounts(ctx, batchID)
}

func (p *appManagedBatchImageProvider) generateOne(ctx context.Context, job *BatchImageJob, account *Account, item BatchImageSubmitItem, imageSize, aspectRatio, responseMimeType string) []byte {
	model := account.GetMappedModel(job.Model)
	if strings.TrimSpace(model) == "" {
		model = job.Model
	}
	if isBananaRouterGeminiAccount(account) {
		return p.generateOneBananaRouter(ctx, job, account, model, item, imageSize, aspectRatio)
	}
	imageSize = managedImageSize(imageSize)
	// Keep the requested output format in the persisted payload for future
	// providers, but do not send a non-standard response_format field to an
	// OpenAI-compatible endpoint: the API accepts b64_json/url there, while
	// PNG/JPEG/WebP is an output encoding choice made by the upstream.
	body := map[string]any{"model": model, "prompt": item.Prompt, "n": 1, "size": imageSize}
	if item.OutputCount > 1 {
		// One upstream call per output is intentional: this avoids providers
		// silently returning fewer images and gives each output independent retry.
		body["n"] = 1
	}
	var reqBody io.Reader
	contentType := "application/json"
	endpoint := "/v1/images/generations"
	if len(item.ReferenceImages) > 0 {
		var buffer bytes.Buffer
		mw := multipart.NewWriter(&buffer)
		_ = mw.WriteField("model", model)
		_ = mw.WriteField("prompt", item.Prompt)
		_ = mw.WriteField("size", imageSize)
		for _, ref := range item.ReferenceImages {
			if len(ref.Data) == 0 {
				return managedErrorLine(item.CustomID, "REFERENCE_UNAVAILABLE", "reference image bytes are unavailable")
			}
			name := strings.TrimSpace(ref.ID)
			if name == "" {
				name = "reference.png"
			}
			part, partErr := mw.CreateFormFile("image[]", filepath.Base(name))
			if partErr != nil {
				return managedErrorLine(item.CustomID, "REFERENCE_BUILD_FAILED", partErr.Error())
			}
			if _, partErr = part.Write(ref.Data); partErr != nil {
				return managedErrorLine(item.CustomID, "REFERENCE_BUILD_FAILED", partErr.Error())
			}
		}
		if err := mw.Close(); err != nil {
			return managedErrorLine(item.CustomID, "REFERENCE_BUILD_FAILED", err.Error())
		}
		reqBody, contentType = &buffer, mw.FormDataContentType()
		endpoint = "/v1/images/edits"
	} else {
		jsonBody, _ := json.Marshal(body)
		reqBody = bytes.NewReader(jsonBody)
	}
	base := managedAccountBaseURL(account)
	target := buildOpenAIEndpointURL(base, endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, reqBody)
	if err != nil {
		return managedErrorLine(item.CustomID, "REQUEST_BUILD_FAILED", err.Error())
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+managedAccountAPIKey(account))
	// Keep retries idempotent across a worker crash between the upstream
	// response and the durable partial-result append. Providers implementing
	// the standard header can return the existing result instead of charging a
	// duplicate image request.
	req.Header.Set("Idempotency-Key", managedItemIdempotencyKey(job, item.CustomID))
	if ua := account.GetOpenAIUserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	account.ApplyHeaderOverrides(req.Header)
	var resp *http.Response
	if p.httpUpstream != nil {
		proxyURL := ""
		if account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
		resp, err = p.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	} else {
		resp, err = (&http.Client{Timeout: p.timeout}).Do(req)
	}
	if err != nil {
		return managedErrorLine(item.CustomID, "UPSTREAM_REQUEST_FAILED", err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if readErr != nil {
		return managedErrorLine(item.CustomID, "UPSTREAM_READ_FAILED", readErr.Error())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return managedErrorLine(item.CustomID, strconv.Itoa(resp.StatusCode), strings.TrimSpace(string(responseBody)))
	}
	var out struct {
		Data []struct {
			B64 string `json:"b64_json"`
			URL string `json:"url"`
		} `json:"data"`
	}
	if json.Unmarshal(responseBody, &out) != nil || len(out.Data) == 0 {
		return managedErrorLine(item.CustomID, "EMPTY_IMAGE_OUTPUT", "upstream returned no image")
	}
	parts := make([]any, 0, len(out.Data))
	for _, img := range out.Data {
		data, mime, ok := decodeManagedImageData(img.B64, responseMimeType)
		if !ok && strings.TrimSpace(img.URL) != "" {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(img.URL)), "data:") {
				data, mime, _ = decodeManagedImageData(img.URL, responseMimeType)
			} else {
				data, mime = fetchManagedImage(ctx, img.URL)
			}
		}
		if data == "" {
			continue
		}
		parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": data}})
	}
	if len(parts) == 0 {
		return managedErrorLine(item.CustomID, "EMPTY_IMAGE_OUTPUT", "upstream returned no decodable image")
	}
	line, _ := json.Marshal(map[string]any{"key": item.CustomID, "response": map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": parts}}}}})
	return line
}

// generateOneBananaRouter uses BananaRouter's asynchronous Gemini-compatible
// endpoint for one item. The parent app-managed provider still supplies the
// same bounded fan-out, checkpointing, cancellation, and settlement behavior
// as GPT-image batches; only the per-item upstream protocol differs.
func (p *appManagedBatchImageProvider) generateOneBananaRouter(ctx context.Context, job *BatchImageJob, account *Account, model string, item BatchImageSubmitItem, imageSize, aspectRatio string) []byte {
	base := managedAccountBaseURL(account)
	parts := make([]map[string]any, 0, 1+len(item.ReferenceImages))
	parts = append(parts, map[string]any{"text": item.Prompt})
	for _, ref := range item.ReferenceImages {
		mimeType := normalizeBatchImageReferenceMimeType(ref.MimeType)
		if mimeType == "" {
			return managedErrorLine(item.CustomID, "REFERENCE_INVALID", "reference image mime_type is required")
		}
		switch {
		case len(ref.Data) > 0 && strings.TrimSpace(ref.FileURI) == "":
			parts = append(parts, map[string]any{"inlineData": map[string]any{
				"mimeType": mimeType,
				"data":     base64.StdEncoding.EncodeToString(ref.Data),
			}})
		case len(ref.Data) == 0 && strings.TrimSpace(ref.FileURI) != "":
			parts = append(parts, map[string]any{"fileData": map[string]any{
				"mimeType": mimeType,
				"fileUri":  strings.TrimSpace(ref.FileURI),
			}})
		default:
			return managedErrorLine(item.CustomID, "REFERENCE_INVALID", "reference image must contain exactly one of data or file_uri")
		}
	}
	imageConfig := map[string]any{}
	if v := strings.TrimSpace(aspectRatio); v != "" {
		imageConfig["aspectRatio"] = v
	}
	if v := bananaImageSize(imageSize); v != "" {
		imageConfig["imageSize"] = v
	}
	// Image batches should receive image-only candidates. Requesting TEXT as
	// well can produce a mixed response that cannot be persisted as an image.
	generationConfig := map[string]any{"responseModalities": []string{"IMAGE"}}
	if len(imageConfig) > 0 {
		generationConfig["imageConfig"] = imageConfig
	}
	body := map[string]any{
		"contents": []any{map[string]any{"parts": parts}},
		"generationConfig": generationConfig,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return managedErrorLine(item.CustomID, "REQUEST_BUILD_FAILED", err.Error())
	}
	path := "/v1beta/models/" + url.PathEscape(strings.TrimSpace(model)) + ":asyncGenerateContent"
	target := buildBananaRouterEndpointURL(base, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		return managedErrorLine(item.CustomID, "REQUEST_BUILD_FAILED", err.Error())
	}
	setBananaRouterHeaders(req, managedAccountAPIKey(account), managedItemIdempotencyKey(job, item.CustomID))
	resp, err := p.doManagedRequest(req, account)
	if err != nil {
		return managedErrorLine(item.CustomID, "UPSTREAM_REQUEST_FAILED", err.Error())
	}
	var submit bananaRouterAsyncResponse
	readErr := decodeManagedJSONResponse(resp, &submit)
	if readErr != nil {
		return managedErrorLine(item.CustomID, readErr.code, readErr.message)
	}
	if images := bananaRouterResponseImages(submit.Raw); len(images) > 0 {
		return managedImageResultLine(ctx, item.CustomID, images)
	}
	taskID := strings.TrimSpace(submit.TaskID)
	if taskID == "" {
		return managedErrorLine(item.CustomID, "EMPTY_ASYNC_TASK", "BananaRouter did not return a task id")
	}
	for {
		if err := waitManagedPoll(ctx, 5*time.Second); err != nil {
			return managedErrorLine(item.CustomID, "UPSTREAM_REQUEST_FAILED", err.Error())
		}
		pollPath := "/v1/async-tasks/" + url.PathEscape(taskID)
		pollReq, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, buildBananaRouterEndpointURL(base, pollPath), nil)
		if reqErr != nil {
			return managedErrorLine(item.CustomID, "REQUEST_BUILD_FAILED", reqErr.Error())
		}
		setBananaRouterHeaders(pollReq, managedAccountAPIKey(account), "")
		pollResp, pollErr := p.doManagedRequest(pollReq, account)
		if pollErr != nil {
			return managedErrorLine(item.CustomID, "UPSTREAM_REQUEST_FAILED", pollErr.Error())
		}
		var status bananaRouterAsyncResponse
		readErr = decodeManagedJSONResponse(pollResp, &status)
		if readErr != nil {
			return managedErrorLine(item.CustomID, readErr.code, readErr.message)
		}
		if images := bananaRouterResponseImages(status.Raw); len(images) > 0 {
			return managedImageResultLine(ctx, item.CustomID, images)
		}
		state := strings.ToLower(strings.TrimSpace(status.Status))
		if state == "failed" || state == "error" || state == "cancelled" || state == "canceled" || state == "expired" {
			message := strings.TrimSpace(status.ErrorMessage)
			if message == "" {
				message = "BananaRouter asynchronous task failed"
			}
			return managedErrorLine(item.CustomID, "BANANA_ASYNC_FAILED", message)
		}
		if state == "completed" || state == "succeeded" || state == "success" || state == "done" {
			return managedErrorLine(item.CustomID, "EMPTY_IMAGE_OUTPUT", "BananaRouter task completed without an image")
		}
	}
}

func (p *appManagedBatchImageProvider) doManagedRequest(req *http.Request, account *Account) (*http.Response, error) {
	if p.httpUpstream != nil {
		proxyURL := ""
		if account != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
		accountID := int64(0)
		concurrency := 0
		if account != nil {
			accountID = account.ID
			concurrency = account.Concurrency
		}
		return p.httpUpstream.Do(req, proxyURL, accountID, concurrency)
	}
	return (&http.Client{Timeout: p.timeout}).Do(req)
}

type bananaRouterAsyncResponse struct {
	TaskID      string
	Status      string
	ErrorMessage string
	Raw         map[string]json.RawMessage
}

func (r *bananaRouterAsyncResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.Raw = raw
	for _, key := range []string{"taskID", "taskId", "task_id", "id"} {
		if value, ok := raw[key]; ok {
			_ = json.Unmarshal(value, &r.TaskID)
			if strings.TrimSpace(r.TaskID) != "" {
				break
			}
		}
	}
	for _, key := range []string{"status", "state"} {
		if value, ok := raw[key]; ok {
			_ = json.Unmarshal(value, &r.Status)
			if strings.TrimSpace(r.Status) != "" {
				break
			}
		}
	}
	for _, key := range []string{"error", "message", "errorMessage", "error_message"} {
		if value, ok := raw[key]; ok {
			var text string
			if json.Unmarshal(value, &text) == nil {
				r.ErrorMessage = text
			} else {
				var object struct{ Message string `json:"message"` }
				_ = json.Unmarshal(value, &object)
				r.ErrorMessage = object.Message
			}
			if strings.TrimSpace(r.ErrorMessage) != "" {
				break
			}
		}
	}
	return nil
}

type managedHTTPReadError struct{ code, message string }

func decodeManagedJSONResponse(resp *http.Response, out any) *managedHTTPReadError {
	if resp == nil {
		return &managedHTTPReadError{code: "UPSTREAM_REQUEST_FAILED", message: "empty upstream response"}
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return &managedHTTPReadError{code: "UPSTREAM_READ_FAILED", message: err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &managedHTTPReadError{code: strconv.Itoa(resp.StatusCode), message: strings.TrimSpace(string(body))}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return &managedHTTPReadError{code: "INVALID_ASYNC_RESPONSE", message: err.Error()}
	}
	return nil
}

func bananaRouterResponseImages(raw map[string]json.RawMessage) []json.RawMessage {
	for _, key := range []string{"resultImages", "result_images", "images"} {
		if value, ok := raw[key]; ok {
			var images []json.RawMessage
			if json.Unmarshal(value, &images) == nil && len(images) > 0 {
				return images
			}
		}
	}
	for _, key := range []string{"result", "data", "output"} {
		if value, ok := raw[key]; ok {
			var nested map[string]json.RawMessage
			if json.Unmarshal(value, &nested) == nil {
				if images := bananaRouterResponseImages(nested); len(images) > 0 {
					return images
				}
			}
		}
	}
	return nil
}

func managedImageResultLine(ctx context.Context, customID string, images []json.RawMessage) []byte {
	parts := make([]any, 0, len(images))
	for _, raw := range images {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			data, mime, ok := decodeManagedImageData(text, "image/png")
			if !ok {
				data, mime = fetchManagedImage(ctx, text)
			}
			if data != "" {
				parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": mime, "data": data}})
			}
			continue
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			continue
		}
		var value string
		var mime string
		for _, key := range []string{"base64", "data", "b64_json", "image"} {
			if json.Unmarshal(object[key], &value) == nil && strings.TrimSpace(value) != "" {
				break
			}
		}
		for _, key := range []string{"mimeType", "mime_type", "contentType", "content_type"} {
			if json.Unmarshal(object[key], &mime) == nil && strings.TrimSpace(mime) != "" {
				break
			}
		}
		if value == "" {
			for _, key := range []string{"inlineData", "inline_data"} {
				var inline map[string]json.RawMessage
				if json.Unmarshal(object[key], &inline) != nil {
					continue
				}
				for _, dataKey := range []string{"data", "base64", "b64_json"} {
					if json.Unmarshal(inline[dataKey], &value) == nil && strings.TrimSpace(value) != "" {
						break
					}
				}
				for _, mimeKey := range []string{"mimeType", "mime_type"} {
					if json.Unmarshal(inline[mimeKey], &mime) == nil && strings.TrimSpace(mime) != "" {
						break
					}
				}
				if value != "" {
					break
				}
			}
		}
		if value == "" {
			for _, key := range []string{"url", "imageUrl", "image_url", "downloadUrl", "download_url"} {
				if json.Unmarshal(object[key], &value) == nil && strings.TrimSpace(value) != "" {
					break
				}
			}
		}
		data, detected, ok := decodeManagedImageData(value, mime)
		if !ok && value != "" {
			data, detected = fetchManagedImage(ctx, value)
		}
		if data != "" {
			if detected == "" {
				detected = normalizeManagedImageMime(mime)
			}
			parts = append(parts, map[string]any{"inlineData": map[string]any{"mimeType": detected, "data": data}})
		}
	}
	if len(parts) == 0 {
		return managedErrorLine(customID, "EMPTY_IMAGE_OUTPUT", "BananaRouter returned no image")
	}
	line, _ := json.Marshal(map[string]any{"key": customID, "response": map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": parts}}}}})
	return line
}

func waitManagedPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func setBananaRouterHeaders(req *http.Request, apiKey, idempotencyKey string) {
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("x-goog-api-key", apiKey)
	}
	if strings.TrimSpace(idempotencyKey) != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
}

func buildBananaRouterEndpointURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	// Submit uses /v1beta while task polling uses /v1. Accept either a host
	// root or a configured versioned base URL without producing /v1beta/v1.
	if strings.HasPrefix(path, "/v1beta/") || strings.HasPrefix(path, "/v1/") {
		base = strings.TrimSuffix(base, "/v1beta")
		base = strings.TrimSuffix(base, "/v1")
	}
	return base + path
}

func bananaImageSize(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "1K", "2K", "4K":
		return strings.ToUpper(strings.TrimSpace(value))
	default:
		return ""
	}
}

func (p *appManagedBatchImageProvider) Cancel(_ context.Context, job *BatchImageJob, _ *Account) error {
	if job != nil {
		_ = os.Remove(p.resultPath(job.BatchID))
		if err := os.MkdirAll(p.resultDir, 0750); err == nil {
			_ = os.WriteFile(p.cancelPath(job.BatchID), []byte("cancelled"), 0600)
		}
	}
	return nil
}

func (p *appManagedBatchImageProvider) OpenResult(_ context.Context, job *BatchImageJob, _ *Account) (io.ReadCloser, string, error) {
	if job == nil {
		return nil, "", ErrBatchImageProviderMissingResultRef
	}
	path := batchImageProviderOutputRef(job)
	if path == "" {
		path = p.resultPath(job.BatchID)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", ErrBatchImageProviderMissingResultRef.WithCause(err)
	}
	return f, path, nil
}

func (p *appManagedBatchImageProvider) Cleanup(_ context.Context, job *BatchImageJob, _ *Account, _ CleanupTarget) error {
	if job != nil {
		if err := os.Remove(p.resultPath(job.BatchID)); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = os.Remove(p.cancelPath(job.BatchID))
	}
	return nil
}

func (p *appManagedBatchImageProvider) resultPath(batchID string) string {
	return filepath.Join(p.resultDir, filepath.Base(strings.TrimSpace(batchID))+".jsonl")
}

func (p *appManagedBatchImageProvider) cancelPath(batchID string) string {
	return filepath.Join(p.resultDir, filepath.Base(strings.TrimSpace(batchID))+".cancelled")
}

func managedImageSize(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "1K":
		return "1024x1024"
	case "2K":
		return "2048x2048"
	case "4K":
		// The supported 4K contract is capped at 3840x2160. Keep the
		// tier's default on that legal landscape canvas instead of emitting
		// an unsupported 4096x4096 request.
		return "3840x2160"
	case "512", "512X512":
		return "512x512"
	default:
		if normalized, ok := normalizeBatchImageSize(value); ok {
			if strings.Contains(normalized, "x") {
				return normalized
			}
		}
		return "1024x1024"
	}
}

func managedItemIdempotencyKey(job *BatchImageJob, customID string) string {
	batchID := ""
	if job != nil {
		batchID = strings.TrimSpace(job.BatchID)
	}
	sum := sha256.Sum256([]byte(batchID + "\x00" + strings.TrimSpace(customID)))
	return "sub2api-batch-" + hex.EncodeToString(sum[:])
}

func managedAccountAPIKey(account *Account) string {
	if account == nil {
		return ""
	}
	if key := strings.TrimSpace(account.GetOpenAIProtocolAPIKey()); key != "" {
		return key
	}
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeUpstream {
		return strings.TrimSpace(account.GetCredential("api_key"))
	}
	return ""
}

func managedAccountBaseURL(account *Account) string {
	if account == nil {
		return ""
	}
	if account.IsGemini() {
		return strings.TrimRight(strings.TrimSpace(account.GetGeminiBaseURL("")), "/")
	}
	if base := strings.TrimSpace(account.GetOpenAIBaseURL()); base != "" {
		return base
	}
	if account.IsGrok() {
		return strings.TrimSpace(account.GetGrokBaseURL())
	}
	return ""
}

// BananaRouter and compatible Gemini gateways expose the native Generate
// Content API but not Google's file-upload Batch API. Treat a custom Gemini
// base URL as an async managed-fanout account; official Google endpoints stay
// on the existing native batch provider.
func isBananaRouterGeminiAccount(account *Account) bool {
	if account == nil || !account.IsGemini() || account.Type != AccountTypeAPIKey || managedAccountAPIKey(account) == "" {
		return false
	}
	base := strings.TrimSpace(account.GetCredential("base_url"))
	if base == "" {
		return false
	}
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" {
		return true
	}
	host := strings.ToLower(u.Hostname())
	return host != "generativelanguage.googleapis.com" &&
		host != "aiplatform.googleapis.com" &&
		!strings.HasSuffix(host, ".googleapis.com")
}

func managedErrorLine(customID, code, message string) []byte {
	b, _ := json.Marshal(map[string]any{"key": customID, "error": map[string]string{"code": code, "message": message}})
	return b
}

// decodeManagedImageData accepts either a provider's plain base64 payload or a
// data URL, then derives the MIME type from the decoded bytes. Some upstreams
// (notably native Gemini image generation) return JPEG/WebP even when the
// requested output format defaults to PNG, so the response must not be tagged
// with a hard-coded MIME type.
func decodeManagedImageData(value, fallbackMime string) (string, string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", false
	}
	if strings.HasPrefix(strings.ToLower(value), "data:") {
		comma := strings.IndexByte(value, ',')
		if comma < 0 || !strings.Contains(strings.ToLower(value[:comma]), ";base64") {
			return "", "", false
		}
		value = strings.TrimSpace(value[comma+1:])
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil || len(decoded) == 0 {
		return "", "", false
	}

	mime := strings.TrimSpace(strings.SplitN(http.DetectContentType(decoded), ";", 2)[0])
	switch mime {
	case "image/jpeg", "image/png", "image/webp":
		// Keep the signature-derived MIME type even when the provider's
		// requested format or data URL declaration disagrees with it.
	default:
		mime = normalizeManagedImageMime(fallbackMime)
		if mime == "" {
			mime = "image/png"
		}
	}
	return base64.StdEncoding.EncodeToString(decoded), mime, true
}

func normalizeManagedImageMime(value string) string {
	mime := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
	switch mime {
	case "image/jpeg", "image/png", "image/webp":
		return mime
	default:
		return ""
	}
}

func loadManagedLines(path string) map[string][]byte {
	out := make(map[string][]byte)
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(line, &envelope) == nil && strings.TrimSpace(envelope.Key) != "" {
			out[envelope.Key] = append([]byte(nil), line...)
		}
	}
	return out
}

func appendManagedLine(path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(line); err != nil {
		return err
	}
	_, err = f.Write([]byte("\n"))
	return err
}

func fetchManagedImage(ctx context.Context, rawURL string) (string, string) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", ""
	}
	mime := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if mime == "" {
		mime = "image/png"
	}
	return base64.StdEncoding.EncodeToString(b), mime
}
