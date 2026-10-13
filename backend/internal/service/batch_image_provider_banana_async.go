package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const bananaRouterAsyncProtocol = "bananarouter_async"
const bananaRouterPollInterval = 5 * time.Second
const bananaRouterIdempotencyWindow = 24 * time.Hour

// State lives beside the original request in input_payload, not in a new
// schema. CAS prevents a stale worker from replacing a recovered task ID.
type BatchImageAsyncStateRepository interface {
	CompareAndSetBatchImageAsyncState(ctx context.Context, batchID, customID string, previous, next json.RawMessage) error
	RequestBatchImageAsyncCancel(ctx context.Context, batchID string) error
}

type bananaRouterItemState struct {
	Protocol              string          `json:"protocol"`
	TaskID                string          `json:"task_id,omitempty"`
	AccountID             int64           `json:"account_id"`
	CredentialFingerprint string          `json:"credential_fingerprint"`
	BaseURL               string          `json:"base_url"`
	Model                 string          `json:"model"`
	IdempotencyKey        string          `json:"idempotency_key"`
	StartedAt             time.Time       `json:"started_at"`
	NextAttemptAt         time.Time       `json:"next_attempt_at"`
	Response              json.RawMessage `json:"response,omitempty"`
	ErrorCode             string          `json:"error_code,omitempty"`
	ErrorMessage          string          `json:"error_message,omitempty"`
}

type bananaRouterPendingItem struct {
	item      *BatchImageItem
	payload   managedBatchImageItemPayload
	state     bananaRouterItemState
	rawState  json.RawMessage
	cancelled bool
}

func decodeBananaRouterItem(item *BatchImageItem) (*bananaRouterPendingItem, error) {
	if item == nil {
		return nil, fmt.Errorf("managed async item is missing")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(item.InputPayload, &raw); err != nil || raw == nil {
		return nil, fmt.Errorf("invalid managed async item payload")
	}
	in := &bananaRouterPendingItem{item: item, rawState: raw["managed_async"]}
	if err := json.Unmarshal(item.InputPayload, &in.payload); err != nil {
		return nil, err
	}
	if in.payload.Item.Prompt == "" {
		if err := json.Unmarshal(item.InputPayload, &in.payload.Item); err != nil {
			return nil, err
		}
	}
	in.payload.Item.CustomID = item.CustomID
	if in.payload.Item.Prompt == "" {
		return nil, fmt.Errorf("managed async item prompt is missing")
	}
	if len(in.rawState) > 0 && string(in.rawState) != "null" {
		if err := json.Unmarshal(in.rawState, &in.state); err != nil {
			return nil, err
		}
		if in.state.Protocol != bananaRouterAsyncProtocol || in.state.StartedAt.IsZero() {
			return nil, fmt.Errorf("invalid managed async recovery state")
		}
	}
	if value := raw["managed_async_cancel_requested"]; len(value) > 0 {
		if err := json.Unmarshal(value, &in.cancelled); err != nil {
			return nil, err
		}
	}
	return in, nil
}

func (p *appManagedBatchImageProvider) getBananaRouterBatch(ctx context.Context, job *BatchImageJob, account *Account) (*BatchProviderStatus, error) {
	repo, ok := p.itemRepo.(BatchImageAsyncStateRepository)
	if !ok {
		return nil, fmt.Errorf("managed async state repository is unavailable")
	}
	path := p.resultPath(job.BatchID)
	if _, err := os.Stat(path); err == nil {
		return bananaRouterFinished(path), nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Honor cancellation markers written by older workers, but still drain
	// persisted submitted tasks instead of releasing their holds early.
	if _, err := os.Stat(p.cancelPath(job.BatchID)); err == nil {
		if err := repo.RequestBatchImageAsyncCancel(ctx, job.BatchID); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	items, err := p.itemRepo.ListBatchImageItems(ctx, job.BatchID, BatchImageItemFilter{Limit: maxInt(job.ItemCount, 1000)})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("managed async batch has no items")
	}
	if err := os.MkdirAll(p.resultDir, 0750); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	completed := loadManagedLines(path + ".partial")
	pending := make([]*bananaRouterPendingItem, len(items))
	lines := make([][]byte, len(items))
	active := 0
	for i, item := range items {
		pending[i], err = decodeBananaRouterItem(item)
		if err != nil {
			return nil, err
		}
		lines[i] = completed[item.CustomID]
		if len(lines[i]) > 0 {
			if err := p.persistManagedProgress(ctx, job.BatchID, item.CustomID, lines[i]); err != nil {
				return nil, err
			}
		} else if !pending[i].state.StartedAt.IsZero() && pending[i].state.ErrorCode == "" {
			active++
		}
	}
	// Bound outstanding provider tasks, not merely concurrent HTTP requests.
	available := maxInt(0, p.concurrency-active)
	errs := make([]error, len(items))
	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	var checkpointMu sync.Mutex
	for i, in := range pending {
		if len(lines[i]) > 0 {
			continue
		}
		if in.state.StartedAt.IsZero() && !in.cancelled {
			if available == 0 {
				continue
			}
			available--
		}
		wg.Add(1)
		go func(i int, in *bananaRouterPendingItem) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			line, err := p.advanceBananaRouterItem(ctx, repo, job, account, in)
			if err != nil || len(line) == 0 {
				errs[i] = err
				return
			}
			checkpointMu.Lock()
			err = appendManagedLine(path+".partial", line)
			checkpointMu.Unlock()
			if err == nil {
				err = p.persistManagedProgress(ctx, job.BatchID, in.item.CustomID, line)
			}
			if err != nil {
				errs[i] = err
				return
			}
			lines[i] = line
		}(i, in)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	for _, line := range lines {
		if len(line) == 0 {
			return &BatchProviderStatus{RawState: "RUNNING", InternalState: BatchProviderStateRunning, SuggestedRequeueAfter: bananaRouterPollInterval}, nil
		}
	}
	// Even cancelled batches reach indexing: successful submitted images must
	// be retained and billed, while cancelled unsubmitted items cost nothing.
	if err := writeBananaRouterResult(path, lines); err != nil {
		return nil, err
	}
	_ = os.Remove(path + ".partial")
	return bananaRouterFinished(path), nil
}

func (p *appManagedBatchImageProvider) bananaRouterNow() time.Time {
	if p.bananaNow != nil {
		return p.bananaNow().UTC()
	}
	return time.Now().UTC()
}

func (p *appManagedBatchImageProvider) advanceBananaRouterItem(ctx context.Context, repo BatchImageAsyncStateRepository, job *BatchImageJob, account *Account, in *bananaRouterPendingItem) ([]byte, error) {
	save := func() error {
		next, err := json.Marshal(in.state)
		if err != nil {
			return err
		}
		if err := repo.CompareAndSetBatchImageAsyncState(ctx, job.BatchID, in.item.CustomID, in.rawState, next); err != nil {
			return err
		}
		in.rawState = next
		return nil
	}
	fail := func(code, message string) ([]byte, error) {
		in.state.ErrorCode, in.state.ErrorMessage = code, message
		if err := save(); err != nil {
			return nil, err
		}
		return managedErrorLine(in.item.CustomID, code, message), nil
	}
	if in.state.ErrorCode != "" {
		return managedErrorLine(in.item.CustomID, in.state.ErrorCode, in.state.ErrorMessage), nil
	}
	if in.state.StartedAt.IsZero() && in.cancelled {
		return managedErrorLine(in.item.CustomID, "BATCH_ITEM_CANCELLED", "cancelled before submission"), nil
	}
	now := p.bananaRouterNow()
	if now.Before(in.state.NextAttemptAt) {
		return nil, nil
	}
	base := buildBananaRouterEndpointURL(managedAccountBaseURL(account), "/v1/")
	fingerprint := sha256.Sum256([]byte(managedAccountAPIKey(account)))
	credentialFingerprint := hex.EncodeToString(fingerprint[:])
	if in.state.StartedAt.IsZero() {
		in.state = bananaRouterItemState{
			Protocol: bananaRouterAsyncProtocol, AccountID: account.ID,
			CredentialFingerprint: credentialFingerprint, BaseURL: base,
			Model: account.GetMappedModel(job.Model), StartedAt: now,
			IdempotencyKey: managedItemIdempotencyKey(job, in.item.CustomID),
		}
		if strings.TrimSpace(in.state.Model) == "" {
			in.state.Model = job.Model
		}
	} else if in.state.AccountID != account.ID || in.state.BaseURL != base || in.state.CredentialFingerprint != credentialFingerprint {
		// A task is only visible to its original token. Do not create a new
		// task or release a possibly billable task after credentials change.
		return nil, fmt.Errorf("managed async account credentials changed; restore original credentials to recover task")
	}
	if in.state.TaskID == "" && len(in.state.Response) == 0 && !now.Before(in.state.StartedAt.Add(bananaRouterIdempotencyWindow)) {
		return nil, fmt.Errorf("managed async submission outcome is unknown beyond the 24-hour idempotency window; manual reconciliation required")
	}
	in.state.NextAttemptAt = now.Add(bananaRouterPollInterval)
	if err := save(); err != nil {
		return nil, err
	}
	var response bananaRouterAsyncResponse
	if len(in.state.Response) > 0 {
		if err := json.Unmarshal(in.state.Response, &response); err != nil {
			return nil, err
		}
	} else {
		method := http.MethodGet
		path := "/v1/async-tasks/" + url.PathEscape(in.state.TaskID)
		var body []byte
		var err error
		if in.state.TaskID == "" {
			method = http.MethodPost
			path = "/v1beta/models/" + url.PathEscape(in.state.Model) + ":asyncGenerateContent"
			body, err = bananaRouterRequestBody(in.payload.Item, in.payload.ImageSize, in.payload.AspectRatio)
			if err != nil {
				return fail("REQUEST_BUILD_FAILED", err.Error())
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, buildBananaRouterEndpointURL(managedAccountBaseURL(account), path), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		setBananaRouterHeaders(req, managedAccountAPIKey(account), "")
		if method == http.MethodPost {
			req.Header.Set("Idempotency-Key", in.state.IdempotencyKey)
		}
		resp, err := p.doManagedRequest(req, account)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, fmt.Errorf("managed async request returned an empty response")
		}
		statusCode := resp.StatusCode
		if readErr := decodeManagedJSONResponse(resp, &response); readErr != nil {
			if statusCode >= 400 && statusCode < 500 && statusCode != 408 && statusCode != 429 && method == http.MethodPost {
				return fail(readErr.code, readErr.message)
			}
			// Poll failures do not establish that an already submitted task
			// failed. Keep its identity and balance hold for recovery.
			return nil, fmt.Errorf("managed async request: %s: %s", readErr.code, readErr.message)
		}
		if taskID := strings.TrimSpace(response.TaskID); taskID != "" {
			if in.state.TaskID != "" && in.state.TaskID != taskID {
				return nil, fmt.Errorf("managed async response task ID does not match")
			}
			in.state.TaskID = taskID
		}
		if len(bananaRouterResponseImages(response.Raw)) > 0 {
			in.state.Response, err = json.Marshal(response.Raw)
			if err != nil {
				return nil, err
			}
		}
		if err := save(); err != nil {
			return nil, err
		}
	}
	if images := bananaRouterResponseImages(response.Raw); len(images) > 0 {
		line := managedImageResultLine(ctx, in.item.CustomID, images)
		result, err := ParseBatchImageResultLine(line, 0)
		if err != nil || result.Status != BatchImageParsedStatusSucceeded || result.ImageCount != len(images) {
			return nil, fmt.Errorf("managed async result download incomplete; retrying saved result")
		}
		return line, nil
	}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "failed", "error", "cancelled", "canceled", "expired":
		message := strings.TrimSpace(response.ErrorMessage)
		if message == "" {
			message = "BananaRouter asynchronous task failed"
		}
		return fail("BANANA_ASYNC_FAILED", message)
	case "completed", "succeeded", "success", "done":
		return fail("EMPTY_IMAGE_OUTPUT", "BananaRouter task completed without an image")
	}
	if in.state.TaskID == "" {
		return nil, fmt.Errorf("managed async response omitted task ID; retrying original idempotency key")
	}
	return nil, nil
}

func bananaRouterFinished(path string) *BatchProviderStatus {
	return &BatchProviderStatus{RawState: "SUCCEEDED", InternalState: BatchProviderStateSucceeded, Done: true, ProviderOutputRef: path}
}

func writeBananaRouterResult(path string, lines [][]byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".banana-result-*")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = os.Remove(f.Name()) }()
	for _, line := range lines {
		if _, err := f.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
