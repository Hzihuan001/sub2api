package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const managedFanoutProtocol = "managed_fanout"

// The existing CAS state is the dispatch boundary, shared with cancellation.
// A claimed item is never mistaken for unsubmitted work after a restart.
type managedFanoutState struct {
	Protocol  string    `json:"protocol"`
	StartedAt time.Time `json:"started_at"`
}

const managedBatchItemPageSize = 500

func listAllManagedBatchItems(ctx context.Context, repo BatchImageItemLookup, batchID string) ([]*BatchImageItem, error) {
	if repo == nil {
		return nil, fmt.Errorf("managed provider item repository is unavailable")
	}
	var out []*BatchImageItem
	for offset := 0; ; offset += managedBatchItemPageSize {
		page, err := repo.ListBatchImageItems(ctx, batchID, BatchImageItemFilter{Limit: managedBatchItemPageSize, Offset: offset})
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < managedBatchItemPageSize {
			return out, nil
		}
	}
}

func (p *appManagedBatchImageProvider) getManagedFanoutBatch(ctx context.Context, job *BatchImageJob, account *Account) (*BatchProviderStatus, error) {
	repo, ok := p.itemRepo.(BatchImageAsyncStateRepository)
	if !ok {
		return nil, fmt.Errorf("managed fan-out state repository is unavailable")
	}
	path := p.resultPath(job.BatchID)
	if _, err := os.Stat(path); err == nil {
		return bananaRouterFinished(path), nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Migrate an old cancellation marker, but preserve completed/partial output.
	if _, err := os.Stat(p.cancelPath(job.BatchID)); err == nil {
		if err := repo.RequestBatchImageAsyncCancel(ctx, job.BatchID); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	items, err := listAllManagedBatchItems(ctx, p.itemRepo, job.BatchID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrBatchImageProviderInvalidInput.WithCause(fmt.Errorf("batch has no items"))
	}
	if err := os.MkdirAll(p.resultDir, 0750); err != nil {
		return nil, err
	}
	completed, err := loadManagedLinesStrict(path + ".partial")
	if err != nil {
		return nil, err
	}
	if managedFanoutCancelOnly(items, completed) {
		// No item crossed the dispatch boundary. Preserve the public cancelled
		// state so the normal processor release path returns the full hold.
		return &BatchProviderStatus{RawState: "CANCELLED", InternalState: BatchProviderStateCancelled, Done: true}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	lines := make([][]byte, len(items))
	errs := make([]error, len(items))
	sem := make(chan struct{}, p.concurrency)
	var wg sync.WaitGroup
	var checkpointMu sync.Mutex
	// Validate/recover the entire input before launching any goroutine. An
	// early restore failure must not leave a detached upstream request running.
	for i, item := range items {
		if item == nil {
			return nil, fmt.Errorf("managed fan-out item is missing")
		}
		if line := completed[item.CustomID]; len(line) > 0 {
			lines[i] = line
			if err := p.persistManagedProgress(ctx, job.BatchID, item.CustomID, line); err != nil {
				return nil, err
			}
		}
	}
	for i, item := range items {
		if len(lines[i]) > 0 {
			continue
		}
		wg.Add(1)
		go func(i int, item *BatchImageItem) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				errs[i] = ctx.Err()
				return
			}
			defer func() { <-sem }()
			line, err := p.generateManagedFanoutItem(ctx, repo, job, account, item)
			if err != nil {
				errs[i] = err
				return
			}
			// A result must be durable before it becomes visible. Do not discard a
			// late success merely because the user's cancel request has arrived.
			checkpointMu.Lock()
			err = appendManagedLine(path+".partial", line)
			checkpointMu.Unlock()
			if err == nil {
				err = p.persistManagedProgress(ctx, job.BatchID, item.CustomID, line)
			}
			if err != nil {
				errs[i] = err
				return
			}
			lines[i] = line
		}(i, item)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	for _, line := range lines {
		if len(line) == 0 {
			return nil, fmt.Errorf("managed batch fan-out incomplete: an item has no result")
		}
	}
	// Cancellation is not a refund terminal state. Index all results first;
	// the existing idempotent capture settles successful images and releases
	// only the unused portion of the original hold.
	if err := writeBananaRouterResult(path, lines); err != nil {
		return nil, err
	}
	_ = os.Remove(path + ".partial")
	return bananaRouterFinished(path), nil
}

func managedFanoutCancelOnly(items []*BatchImageItem, completed map[string][]byte) bool {
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		if item == nil {
			return false
		}
		if len(completed[item.CustomID]) > 0 {
			return false
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(item.InputPayload, &raw) != nil || raw == nil {
			return false
		}
		if state := raw["managed_async"]; len(state) > 0 && string(state) != "null" {
			return false
		}
		if string(raw["managed_async_cancel_requested"]) != "true" {
			return false
		}
	}
	return true
}

func (p *appManagedBatchImageProvider) generateManagedFanoutItem(ctx context.Context, repo BatchImageAsyncStateRepository, job *BatchImageJob, account *Account, item *BatchImageItem) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(item.InputPayload, &raw); err != nil || raw == nil {
		return nil, fmt.Errorf("invalid managed fan-out item payload")
	}
	var state managedFanoutState
	previous := raw["managed_async"]
	if len(previous) > 0 && string(previous) != "null" {
		if err := json.Unmarshal(previous, &state); err != nil || state.Protocol != managedFanoutProtocol || state.StartedAt.IsZero() {
			return nil, fmt.Errorf("invalid managed fan-out recovery state")
		}
		// No durable checkpoint means the outcome cannot be established. A
		// cancel must not turn it into free work or initiate a duplicate request.
		return nil, fmt.Errorf("managed fan-out submission outcome is unknown; retaining balance hold")
	}
	var cancelled bool
	if value := raw["managed_async_cancel_requested"]; len(value) > 0 {
		if err := json.Unmarshal(value, &cancelled); err != nil {
			return nil, err
		}
	}
	if cancelled {
		return managedErrorLine(item.CustomID, "BATCH_ITEM_CANCELLED", "cancelled before submission"), nil
	}
	// This happens after acquiring a concurrency slot. The CAS checks the
	// persisted cancel flag again, so queued goroutines cannot slip through
	// using the snapshot loaded before cancellation.
	state = managedFanoutState{Protocol: managedFanoutProtocol, StartedAt: time.Now().UTC()}
	next, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if err := repo.CompareAndSetBatchImageAsyncState(ctx, job.BatchID, item.CustomID, previous, next); err != nil {
		if errors.Is(err, ErrBatchImageIndexStateConflict) {
			// Cancellation can win after the worker loaded its item snapshot.
			// Reload to distinguish it from another worker's dispatch claim.
			if stopped, reloadErr := p.managedItemCancelledBeforeDispatch(ctx, job.BatchID, item.CustomID); reloadErr != nil {
				return nil, reloadErr
			} else if stopped {
				return managedErrorLine(item.CustomID, "BATCH_ITEM_CANCELLED", "cancelled before submission"), nil
			}
		}
		return nil, err
	}
	var payload managedBatchImageItemPayload
	imageSize, responseMimeType, aspectRatio := "2K", "image/png", ""
	var in BatchImageSubmitItem
	if json.Unmarshal(item.InputPayload, &payload) == nil && strings.TrimSpace(payload.Item.Prompt) != "" {
		in = payload.Item
		if strings.TrimSpace(payload.ImageSize) != "" {
			imageSize = payload.ImageSize
		}
		if strings.TrimSpace(payload.ResponseMimeType) != "" {
			responseMimeType = payload.ResponseMimeType
		}
		aspectRatio = strings.TrimSpace(payload.AspectRatio)
	} else if json.Unmarshal(item.InputPayload, &in) != nil {
		in = BatchImageSubmitItem{CustomID: item.CustomID, Prompt: batchImageDerefString(item.PromptPreview), OutputCount: 1}
	}
	if in.CustomID == "" {
		in.CustomID = item.CustomID
	}
	line, err := p.generateOne(ctx, job, account, in, imageSize, aspectRatio, responseMimeType)
	if err != nil {
		return nil, err
	}
	// generateOne returns an error for unknown submission outcomes, even when
	// its HTTP transport times out independently of the worker context. Only
	// confirmed results or definite pre-send/rejection failures reach here.
	return line, nil
}

func (p *appManagedBatchImageProvider) managedItemCancelledBeforeDispatch(ctx context.Context, batchID, customID string) (bool, error) {
	for offset := 0; ; {
		items, err := p.itemRepo.ListBatchImageItems(ctx, batchID, BatchImageItemFilter{Limit: managedBatchItemPageSize, Offset: offset})
		if err != nil {
			return false, err
		}
		for _, item := range items {
			if item.CustomID != customID {
				continue
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(item.InputPayload, &raw); err != nil {
				return false, err
			}
			state := raw["managed_async"]
			return string(raw["managed_async_cancel_requested"]) == "true" && (len(state) == 0 || string(state) == "null"), nil
		}
		if len(items) < managedBatchItemPageSize {
			return false, nil
		}
		offset += len(items)
	}
}
