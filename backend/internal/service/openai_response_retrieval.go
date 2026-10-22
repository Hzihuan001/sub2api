package service

// This file contains the deliberately narrow provider adapter used when an
// HTTP Responses continuation has to move away from its source API-key
// account. It is intentionally separate from the scheduler and forwarding
// paths: a failed or unsupported retrieval must never silently delete
// previous_response_id or enter the normal account failover loop.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	openAIResponseRecoveryTimeout    = 5 * time.Second
	openAIResponseRecoveryPageLimit  = 16
	openAIResponseRecoveryItemLimit  = 1600
	openAIResponseRecoveryBytesLimit = 8 << 20
	openAIResponseRecoveryBodyLimit  = 2 << 20
	openAIResponseRecoveryInputLimit = 8 << 20
	openAIResponseRecoveryPageSize   = 100
)

var (
	ErrOpenAIResponseRecoveryUnsupported = errors.New("openai response recovery is unsupported for this account or response")
	ErrOpenAIResponseRecoveryUnavailable = errors.New("openai response recovery source is unavailable")
)

// OpenAIResponseRecoveryResult is an in-memory request reconstruction. The
// gateway never persists the retrieved history or logs its contents.
type OpenAIResponseRecoveryResult struct {
	Body       []byte
	ItemCount  int
	SourceID   int64
	ResponseID string
}

// RecoverOpenAIHTTPContinuation retrieves the source response's complete
// ordered input/output items and appends the current turn input. It is only
// enabled for official OpenAI API-key accounts. OAuth/Codex, DeepSeek/Kimi,
// arbitrary OpenAI-compatible endpoints and provider-managed tool state fail
// closed because their response IDs are not portable across credentials.
func (s *OpenAIGatewayService) RecoverOpenAIHTTPContinuation(
	ctx context.Context,
	accountID int64,
	responseID string,
	requestBody []byte,
	expectedModel string,
) (*OpenAIResponseRecoveryResult, error) {
	if s == nil || s.accountRepo == nil || s.httpUpstream == nil {
		return nil, ErrOpenAIResponseRecoveryUnavailable
	}
	responseID = strings.TrimSpace(responseID)
	if responseID == "" || ClassifyOpenAIPreviousResponseIDKind(responseID) != OpenAIPreviousResponseIDKindResponseID || accountID <= 0 {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		if err != nil {
			return nil, fmt.Errorf("%w: load source account: %v", ErrOpenAIResponseRecoveryUnavailable, err)
		}
		return nil, ErrOpenAIResponseRecoveryUnavailable
	}
	if !openAIResponseRecoveryAccountAllowed(account) {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}
	baseURL, err := s.validateUpstreamBaseURL(account.GetOpenAIBaseURL())
	if err != nil || !openAIResponseRecoveryOfficialBase(baseURL) {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}
	if len(requestBody) == 0 || !gjson.ValidBytes(requestBody) {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}

	recoveryCtx, cancel := context.WithTimeout(ctx, openAIResponseRecoveryTimeout)
	defer cancel()
	recoveryCtx = WithHTTPUpstreamProfile(recoveryCtx, HTTPUpstreamProfileOpenAI)
	token, _, err := s.GetAccessToken(recoveryCtx, account)
	if err != nil {
		return nil, fmt.Errorf("%w: source authentication: %v", ErrOpenAIResponseRecoveryUnavailable, err)
	}
	auth, err := s.buildOpenAIAuthenticationHeaders(recoveryCtx, account, token)
	if err != nil {
		return nil, fmt.Errorf("%w: source authentication headers: %v", ErrOpenAIResponseRecoveryUnavailable, err)
	}
	proxyURL := resolveAccountProxyURL(account)

	items, err := s.collectOpenAIResponseRecoveryHistory(recoveryCtx, account, baseURL, proxyURL, auth, responseID, expectedModel, 0, make(map[string]struct{}))
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}

	rebuilt, err := rebuildOpenAIResponseRecoveryBody(requestBody, items)
	if err != nil {
		return nil, err
	}
	if len(rebuilt) > openAIResponseRecoveryInputLimit {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}
	return &OpenAIResponseRecoveryResult{Body: rebuilt, ItemCount: len(items), SourceID: account.ID, ResponseID: responseID}, nil
}

const openAIResponseRecoveryMaxChainDepth = 16

// collectOpenAIResponseRecoveryHistory reconstructs a bounded response chain
// in chronological order. input_items contains the request-side items, while
// response.output contains the assistant/tool items that must follow them.
// Both are required for a portable stateless replay.
func (s *OpenAIGatewayService) collectOpenAIResponseRecoveryHistory(
	ctx context.Context,
	account *Account,
	baseURL, proxyURL string,
	auth http.Header,
	responseID, expectedModel string,
	depth int,
	seen map[string]struct{},
) ([]json.RawMessage, error) {
	if depth >= openAIResponseRecoveryMaxChainDepth {
		return nil, fmt.Errorf("%w: response chain depth limit", ErrOpenAIResponseRecoveryUnsupported)
	}
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return nil, fmt.Errorf("%w: empty response chain id", ErrOpenAIResponseRecoveryUnsupported)
	}
	if _, ok := seen[responseID]; ok {
		return nil, fmt.Errorf("%w: response chain cycle", ErrOpenAIResponseRecoveryUnsupported)
	}
	seen[responseID] = struct{}{}
	responseURL := buildOpenAIEndpointURL(baseURL, "/v1/responses/"+url.PathEscape(responseID))
	metadata, err := s.openAIResponseRecoveryGET(ctx, account, responseURL, proxyURL, auth)
	if err != nil {
		return nil, err
	}
	if err := validateOpenAIResponseRecoveryMetadata(metadata, expectedModel); err != nil {
		return nil, err
	}
	if metadataID := strings.TrimSpace(gjson.GetBytes(metadata, "id").String()); metadataID != "" && metadataID != responseID {
		return nil, fmt.Errorf("%w: response id mismatch", ErrOpenAIResponseRecoveryUnsupported)
	}

	history := make([]json.RawMessage, 0, 32)
	parentID := strings.TrimSpace(gjson.GetBytes(metadata, "previous_response_id").String())
	if parentID != "" {
		parent, err := s.collectOpenAIResponseRecoveryHistory(ctx, account, baseURL, proxyURL, auth, parentID, expectedModel, depth+1, seen)
		if err != nil {
			return nil, err
		}
		history = append(history, parent...)
	}
	items, err := s.listOpenAIResponseRecoveryItems(ctx, account, responseURL+"/input_items", proxyURL, auth)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if err := validateOpenAIResponseRecoveryItem(item); err != nil {
			return nil, err
		}
		var appendErr error
		history, appendErr = appendUniqueOpenAIResponseRecoveryItem(history, item)
		if appendErr != nil {
			return nil, appendErr
		}
	}
	outputs := gjson.GetBytes(metadata, "output")
	if !outputs.Exists() || !outputs.IsArray() || len(outputs.Array()) == 0 {
		return nil, fmt.Errorf("%w: response output is missing", ErrOpenAIResponseRecoveryUnsupported)
	}
	for _, output := range outputs.Array() {
		raw := json.RawMessage(output.Raw)
		if err := validateOpenAIResponseRecoveryItem(raw); err != nil {
			return nil, err
		}
		var appendErr error
		history, appendErr = appendUniqueOpenAIResponseRecoveryItem(history, raw)
		if appendErr != nil {
			return nil, appendErr
		}
	}
	return history, nil
}

func appendUniqueOpenAIResponseRecoveryItem(items []json.RawMessage, item json.RawMessage) ([]json.RawMessage, error) {
	// IDs are stable across a response chain. De-duplicate only when an item
	// has an ID; two identical text messages without IDs remain distinct turns.
	id := strings.TrimSpace(gjson.GetBytes(item, "id").String())
	if id == "" {
		return append(items, item), nil
	}
	for _, existing := range items {
		if strings.TrimSpace(gjson.GetBytes(existing, "id").String()) == id {
			if string(existing) == string(item) {
				return items, nil
			}
			return nil, fmt.Errorf("%w: duplicate item id with different content", ErrOpenAIResponseRecoveryUnsupported)
		}
	}
	return append(items, item), nil
}

func openAIResponseRecoveryAccountAllowed(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.IsOpenAIApiKey()
}

func openAIResponseRecoveryOfficialBase(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	return host == "api.openai.com"
}

func (s *OpenAIGatewayService) openAIResponseRecoveryGET(ctx context.Context, account *Account, target, proxyURL string, auth http.Header) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build retrieval request: %v", ErrOpenAIResponseRecoveryUnavailable, err)
	}
	req.Header.Set("Accept", "application/json")
	for key, values := range auth {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	if err != nil {
		return nil, fmt.Errorf("%w: retrieval request: %v", ErrOpenAIResponseRecoveryUnavailable, err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, openAIResponseRecoveryBodyLimit+1))
	if readErr != nil || len(body) > openAIResponseRecoveryBodyLimit {
		return nil, fmt.Errorf("%w: retrieval response body limit", ErrOpenAIResponseRecoveryUnavailable)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: retrieval status %d", ErrOpenAIResponseRecoveryUnavailable, resp.StatusCode)
	}
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("%w: retrieval response is not JSON", ErrOpenAIResponseRecoveryUnavailable)
	}
	return body, nil
}

func validateOpenAIResponseRecoveryMetadata(body []byte, expectedModel string) error {
	if object := strings.TrimSpace(gjson.GetBytes(body, "object").String()); object != "response" {
		return fmt.Errorf("%w: response object %q", ErrOpenAIResponseRecoveryUnsupported, object)
	}
	status := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "status").String()))
	if status != "completed" {
		return fmt.Errorf("%w: response status %q", ErrOpenAIResponseRecoveryUnsupported, status)
	}
	if store := gjson.GetBytes(body, "store"); store.Exists() && store.Type == gjson.False {
		return fmt.Errorf("%w: response was not stored", ErrOpenAIResponseRecoveryUnsupported)
	}
	if expected := strings.TrimSpace(expectedModel); expected != "" {
		model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
		if model == "" || !strings.EqualFold(model, expected) {
			return fmt.Errorf("%w: response model mismatch", ErrOpenAIResponseRecoveryUnsupported)
		}
	}
	if parent := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String()); parent != "" && ClassifyOpenAIPreviousResponseIDKind(parent) != OpenAIPreviousResponseIDKindResponseID {
		return fmt.Errorf("%w: invalid previous response chain id", ErrOpenAIResponseRecoveryUnsupported)
	}
	return nil
}

func (s *OpenAIGatewayService) listOpenAIResponseRecoveryItems(ctx context.Context, account *Account, target, proxyURL string, auth http.Header) ([]json.RawMessage, error) {
	items := make([]json.RawMessage, 0, 32)
	after := ""
	previousAfter := ""
	bytesRead := 0
	for page := 0; page < openAIResponseRecoveryPageLimit; page++ {
		u, err := url.Parse(target)
		if err != nil {
			return nil, fmt.Errorf("%w: input-items URL: %v", ErrOpenAIResponseRecoveryUnavailable, err)
		}
		q := u.Query()
		q.Set("order", "asc")
		q.Set("limit", fmt.Sprintf("%d", openAIResponseRecoveryPageSize))
		if after != "" {
			q.Set("after", after)
		}
		u.RawQuery = q.Encode()
		body, err := s.openAIResponseRecoveryGET(ctx, account, u.String(), proxyURL, auth)
		if err != nil {
			return nil, err
		}
		bytesRead += len(body)
		if bytesRead > openAIResponseRecoveryBytesLimit {
			return nil, fmt.Errorf("%w: input-items byte limit", ErrOpenAIResponseRecoveryUnsupported)
		}
		var pageBody struct {
			Data    []json.RawMessage `json:"data"`
			HasMore bool              `json:"has_more"`
			LastID  string            `json:"last_id"`
		}
		if err := json.Unmarshal(body, &pageBody); err != nil {
			return nil, fmt.Errorf("%w: input-items JSON: %v", ErrOpenAIResponseRecoveryUnavailable, err)
		}
		if len(items)+len(pageBody.Data) > openAIResponseRecoveryItemLimit {
			return nil, fmt.Errorf("%w: input-items count limit", ErrOpenAIResponseRecoveryUnsupported)
		}
		items = append(items, pageBody.Data...)
		if !pageBody.HasMore {
			return items, nil
		}
		if strings.TrimSpace(pageBody.LastID) == "" {
			return nil, fmt.Errorf("%w: input-items pagination cursor missing", ErrOpenAIResponseRecoveryUnavailable)
		}
		if pageBody.LastID == after || pageBody.LastID == previousAfter {
			return nil, fmt.Errorf("%w: input-items pagination made no progress", ErrOpenAIResponseRecoveryUnavailable)
		}
		previousAfter = after
		after = pageBody.LastID
	}
	return nil, fmt.Errorf("%w: input-items page limit", ErrOpenAIResponseRecoveryUnsupported)
}

func validateOpenAIResponseRecoveryItem(item json.RawMessage) error {
	if len(item) == 0 || !gjson.ValidBytes(item) || !gjson.ParseBytes(item).IsObject() {
		return fmt.Errorf("%w: malformed input item", ErrOpenAIResponseRecoveryUnsupported)
	}
	return validateOpenAIResponseRecoveryNode(gjson.ParseBytes(item))
}

func validateOpenAIResponseRecoveryNode(node gjson.Result) error {
	if node.IsArray() {
		for _, child := range node.Array() {
			if err := validateOpenAIResponseRecoveryNode(child); err != nil {
				return err
			}
		}
		return nil
	}
	if !node.IsObject() {
		return nil
	}
	for key, value := range node.Map() {
		switch key {
		case "file_id", "item_reference", "container_id", "conversation_id", "encrypted_content", "file_data", "file_url", "image_url", "audio_url", "uri", "server_label":
			return fmt.Errorf("%w: item contains external %s", ErrOpenAIResponseRecoveryUnsupported, key)
		case "type":
			typ := strings.ToLower(strings.TrimSpace(value.String()))
			if !openAIResponseRecoveryPortableType(typ) {
				return fmt.Errorf("%w: provider-managed item type %q", ErrOpenAIResponseRecoveryUnsupported, typ)
			}
		}
		if err := validateOpenAIResponseRecoveryNode(value); err != nil {
			return err
		}
	}
	return nil
}

func openAIResponseRecoveryPortableType(typ string) bool {
	switch typ {
	case "message", "reasoning", "input_text", "output_text", "refusal", "summary_text":
		return true
	default:
		return false
	}
}

func rebuildOpenAIResponseRecoveryBody(requestBody []byte, history []json.RawMessage) ([]byte, error) {
	var request map[string]json.RawMessage
	if err := json.Unmarshal(requestBody, &request); err != nil || request == nil {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}
	if previous, ok := request["previous_response_id"]; ok && strings.TrimSpace(string(previous)) != "" {
		delete(request, "previous_response_id")
	}
	if _, exists := request["conversation"]; exists || gjson.GetBytes(requestBody, "background").Bool() {
		return nil, ErrOpenAIResponseRecoveryUnsupported
	}
	items := append([]json.RawMessage(nil), history...)
	if raw, ok := request["input"]; ok && string(raw) != "null" {
		if gjson.ParseBytes(raw).IsArray() {
			var current []json.RawMessage
			if err := json.Unmarshal(raw, &current); err != nil {
				return nil, ErrOpenAIResponseRecoveryUnsupported
			}
			for _, item := range current {
				if err := validateOpenAIResponseRecoveryItem(item); err != nil {
					return nil, err
				}
			}
			items = append(items, current...)
		} else if gjson.ParseBytes(raw).Type == gjson.String {
			var text string
			if err := json.Unmarshal(raw, &text); err != nil {
				return nil, ErrOpenAIResponseRecoveryUnsupported
			}
			wrapped, _ := json.Marshal(map[string]any{"role": "user", "content": text})
			items = append(items, wrapped)
		} else if gjson.ParseBytes(raw).IsObject() {
			if err := validateOpenAIResponseRecoveryItem(raw); err != nil {
				return nil, err
			}
			items = append(items, raw)
		} else {
			return nil, ErrOpenAIResponseRecoveryUnsupported
		}
	}
	request["input"], _ = json.Marshal(items)
	// Do not synthesize instructions from the stored response. The Responses
	// API treats instructions as request-local; copying an old system prompt
	// would change the user's current request semantics. The current body is
	// the sole source of instructions.
	return json.Marshal(request)
}
