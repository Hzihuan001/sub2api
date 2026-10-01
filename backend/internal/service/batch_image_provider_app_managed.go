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
			imageSize, responseMimeType := "1K", "image/png"
			var in BatchImageSubmitItem
			if len(item.InputPayload) > 0 && json.Unmarshal(item.InputPayload, &payload) == nil && strings.TrimSpace(payload.Item.Prompt) != "" {
				in = payload.Item
				if strings.TrimSpace(payload.ImageSize) != "" {
					imageSize = payload.ImageSize
				}
				if strings.TrimSpace(payload.ResponseMimeType) != "" {
					responseMimeType = payload.ResponseMimeType
				}
			} else if len(item.InputPayload) > 0 && json.Unmarshal(item.InputPayload, &in) == nil {
				// Legacy payloads stored the item directly.
			} else {
				in = BatchImageSubmitItem{CustomID: item.CustomID, Prompt: batchImageDerefString(item.PromptPreview), OutputCount: 1}
			}
			if in.CustomID == "" {
				in.CustomID = item.CustomID
			}
			line := p.generateOne(ctx, job, account, in, imageSize, responseMimeType)
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

func (p *appManagedBatchImageProvider) generateOne(ctx context.Context, job *BatchImageJob, account *Account, item BatchImageSubmitItem, imageSize, responseMimeType string) []byte {
	model := account.GetMappedModel(job.Model)
	if strings.TrimSpace(model) == "" {
		model = job.Model
	}
	imageSize = managedImageSize(imageSize)
	// Keep the requested output format in the persisted payload for future
	// providers, but do not send a non-standard response_format field to an
	// OpenAI-compatible endpoint: the API accepts b64_json/url there, while
	// PNG/JPEG/WebP is an output encoding choice made by the upstream.
	_ = responseMimeType
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
		data := strings.TrimSpace(img.B64)
		mime := "image/png"
		if data == "" && strings.TrimSpace(img.URL) != "" {
			data, mime = fetchManagedImage(ctx, img.URL)
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
	case "2K":
		return "2048x2048"
	case "4K":
		return "4096x4096"
	case "512", "512X512":
		return "512x512"
	default:
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
	if base := strings.TrimSpace(account.GetOpenAIBaseURL()); base != "" {
		return base
	}
	if account.IsGrok() {
		return strings.TrimSpace(account.GetGrokBaseURL())
	}
	return ""
}

func managedErrorLine(customID, code, message string) []byte {
	b, _ := json.Marshal(map[string]any{"key": customID, "error": map[string]string{"code": code, "message": message}})
	return b
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
