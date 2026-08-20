package service

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	cursorpkg "github.com/Wei-Shaw/sub2api/internal/pkg/cursor"
)

const (
	cursorObservedModelsExtraKey = "cursor_observed_models"
	cursorObservedModelsTTL      = 6 * time.Hour
	cursorObservedModelsTimeout  = 15 * time.Second
)

// cursorObservedModelsSnapshot is the persisted shape of an account's observed
// upstream model list (accounts.extra["cursor_observed_models"]).
type cursorObservedModelsSnapshot struct {
	Models    []string `json:"models"`
	FetchedAt string   `json:"fetched_at"`
	Source    string   `json:"source,omitempty"`
}

var cursorObservedModelsFlight sync.Map // accountID -> in-flight marker

// scheduleCursorObservedModelsSync best-effort fetches the upstream
// AvailableModels RPC for a Cursor account and stores the IDs in Extra.
// Fire-and-forget: callers invoke it after a successful authenticated request
// so the public /v1/models union stays fresh without blocking the hot path.
func (s *OpenAIGatewayService) scheduleCursorObservedModelsSync(account *Account) {
	if s == nil || account == nil || !account.IsCursor() || s.accountRepo == nil || s.httpUpstream == nil {
		return
	}
	// Skip when the stored snapshot is still fresh (cheap check before spawning).
	if snap := parseCursorObservedModels(account.Extra); snap != nil {
		if t, err := time.Parse(time.RFC3339, snap.FetchedAt); err == nil && time.Since(t) < cursorObservedModelsTTL {
			return
		}
	}
	id := account.ID
	if _, loaded := cursorObservedModelsFlight.LoadOrStore(id, struct{}{}); loaded {
		return
	}
	acc := *account
	go func() {
		defer cursorObservedModelsFlight.Delete(id)
		ctx, cancel := context.WithTimeout(context.Background(), cursorObservedModelsTimeout)
		defer cancel()
		if err := s.syncCursorObservedModels(ctx, &acc); err != nil {
			slog.Debug("cursor_observed_models_sync_failed", "account_id", id, "error", err)
		}
	}()
}

func (s *OpenAIGatewayService) syncCursorObservedModels(ctx context.Context, account *Account) error {
	token := ""
	if s.cursorTokenProvider != nil {
		if at, err := s.cursorTokenProvider.GetAccessToken(ctx, account); err == nil {
			token = strings.TrimSpace(at)
		}
	}
	if token == "" {
		token = strings.TrimSpace(account.GetCursorAccessToken())
	}
	if token == "" {
		return nil
	}

	targetURL := strings.TrimRight(account.GetCursorBaseURL(), "/") + cursorpkg.EndpointAvailableModels
	payload := cursorpkg.EncodeAvailableModelsRequest(false, false)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	for key, values := range cursorpkg.BuildHeaders(token, cursorpkg.ContentTypeProto) {
		for _, v := range values {
			req.Header.Set(key, v)
		}
	}
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	} else if account.ProxyID != nil {
		// Fail closed: an unresolved proxy would send this account's bearer
		// direct from the gateway's own IP.
		return errCursorAgentProxyUnresolved
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return nil
	}
	models, err := cursorpkg.ParseAvailableModelsResponse(body)
	if err != nil {
		return err
	}
	ids := cursorObservedModelIDs(models)
	if len(ids) == 0 {
		return nil
	}
	snap := cursorObservedModelsSnapshot{
		Models:    ids,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    "upstream_available_models",
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err != nil {
		return err
	}
	return s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		cursorObservedModelsExtraKey: asMap,
	})
}

// cursorObservedModelIDs flattens the AvailableModels response into unique
// public model IDs (the picker name is what clients address models by).
func cursorObservedModelIDs(models []cursorpkg.Model) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, m := range models {
		id := strings.TrimSpace(m.Name)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// cursorObservedLookupKey folds a model id onto the identity an observed
// snapshot records it under, so a mapping target can be checked against it.
//
// The snapshot holds picker names, while a mapping target may carry the max-mode
// suffix (which travels as a protocol flag, not part of the id) and may say
// "auto" where the agent protocol says "default". Comparing raw strings would
// drop legitimate aliases from /v1/models.
func cursorObservedLookupKey(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	for _, suffix := range []string{"-max", ":max"} {
		if trimmed := strings.TrimSuffix(id, suffix); trimmed != id && trimmed != "" {
			id = trimmed
			break
		}
	}
	if id == "auto" {
		return cursorpkg.AgentDefaultModel
	}
	return id
}

// CursorObservedModelSet folds an account's observed model ids into lookup keys
// so a mapping target can be tested against them. Nil when nothing is observed,
// which callers must read as "no snapshot", not as "nothing is available".
func CursorObservedModelSet(extra map[string]any) map[string]struct{} {
	ids := CursorObservedModelIDs(extra)
	if len(ids) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if key := cursorObservedLookupKey(id); key != "" {
			set[key] = struct{}{}
		}
	}
	return set
}

// CursorModelObserved reports whether target names a model the snapshot covers.
func CursorModelObserved(observed map[string]struct{}, target string) bool {
	if len(observed) == 0 {
		return false
	}
	_, ok := observed[cursorObservedLookupKey(target)]
	return ok
}

// CursorObservedModelIDs exposes an account's observed Cursor model IDs to
// handlers (admin available-models view). Returns nil when nothing observed.
func CursorObservedModelIDs(extra map[string]any) []string {
	snap := parseCursorObservedModels(extra)
	if snap == nil {
		return nil
	}
	return append([]string(nil), snap.Models...)
}

func parseCursorObservedModels(extra map[string]any) *cursorObservedModelsSnapshot {
	if extra == nil {
		return nil
	}
	raw, ok := extra[cursorObservedModelsExtraKey]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snap cursorObservedModelsSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil
	}
	if len(snap.Models) == 0 {
		return nil
	}
	return &snap
}
