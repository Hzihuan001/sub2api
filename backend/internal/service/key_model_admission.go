package service

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// KeyModelPolicyBodyCandidates is stricter than routing extraction. Empty,
// null, or non-string model fields can override an earlier allowed occurrence
// in another parser and select an unverified upstream default. Restricted keys
// therefore reject them rather than silently discarding those occurrences.
// Callers without a key model policy keep using the original extraction path.
func KeyModelPolicyBodyCandidates(contentType string, body []byte) ([]string, error) {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err == nil && strings.EqualFold(mediaType, "multipart/form-data") {
		boundary := strings.TrimSpace(params["boundary"])
		if boundary == "" {
			return nil, fmt.Errorf("multipart model policy validation requires a boundary")
		}
		reader := multipart.NewReader(bytes.NewReader(body), boundary)
		var models []string
		for {
			part, readErr := reader.NextPart()
			if readErr == io.EOF {
				return models, nil
			}
			if readErr != nil {
				return nil, fmt.Errorf("invalid multipart payload for API key model policy")
			}
			name := strings.TrimSpace(part.FormName())
			if part.FileName() != "" || (!strings.EqualFold(name, "model") && !strings.EqualFold(name, "session")) {
				continue
			}
			data, readErr := io.ReadAll(part)
			if readErr != nil {
				return nil, fmt.Errorf("failed to read model policy field")
			}
			if strings.EqualFold(name, "model") {
				model := strings.TrimSpace(string(data))
				if model == "" {
					return nil, fmt.Errorf("empty model cannot be checked against the API key model policy")
				}
				models = append(models, model)
			} else {
				sessionModels, parseErr := keyModelPolicyJSONCandidates(data)
				if parseErr != nil {
					return nil, parseErr
				}
				models = append(models, sessionModels...)
			}
		}
	}
	return keyModelPolicyJSONCandidates(body)
}

func keyModelPolicyJSONCandidates(body []byte) ([]string, error) {
	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("invalid JSON cannot be checked against the API key model policy")
	}
	var models []string
	var parseErr error
	addModel := func(value gjson.Result) bool {
		if value.Type != gjson.String || strings.TrimSpace(value.String()) == "" {
			parseErr = fmt.Errorf("model must be a non-empty string for the API key model policy")
			return false
		}
		models = append(models, strings.TrimSpace(value.String()))
		return true
	}
	gjson.ParseBytes(body).ForEach(func(key, value gjson.Result) bool {
		if strings.EqualFold(key.String(), "model") {
			return addModel(value)
		}
		if strings.EqualFold(key.String(), "session") {
			if !value.IsObject() {
				parseErr = fmt.Errorf("session must be an object for the API key model policy")
				return false
			}
			value.ForEach(func(sessionKey, sessionValue gjson.Result) bool {
				if strings.EqualFold(sessionKey.String(), "model") {
					return addModel(sessionValue)
				}
				return true
			})
		}
		return parseErr == nil
	})
	return models, parseErr
}

// CheckKeyModelPolicyWSFrame validates the original, public-facing frame before
// any channel/account mapping. Session mutations are checked immediately rather
// than after they have already reached upstream. It is intentionally separate
// from billing, account scheduling and the group's wildcard model allowlist.
func CheckKeyModelPolicyWSFrame(c *gin.Context, payload []byte, fallbackModel string) error {
	if c == nil {
		return nil
	}
	apiKey := getAPIKeyFromContext(c)
	if !apiKey.KeyModelPolicyEnabled() {
		return nil
	}
	deny := func(message string) error {
		MarkOpsClientBusinessLimited(c, OpsClientBusinessLimitedReasonLocalModelConfiguration)
		return NewOpenAIWSClientCloseError(websocket.StatusPolicyViolation, message, nil)
	}
	// Check both top-level and session candidates, including duplicate and
	// case-variant keys. Different parsers can bind different occurrences.
	models, err := keyModelPolicyJSONCandidates(payload)
	if err != nil {
		return deny(err.Error())
	}
	eventType := strings.TrimSpace(gjson.GetBytes(payload, "type").String())
	if eventType == "response.create" || eventType == "" {
		if model := strings.TrimSpace(fallbackModel); model != "" && strings.TrimSpace(gjson.GetBytes(payload, "model").String()) == "" {
			models = append(models, model)
		}
		if len(models) == 0 {
			return deny("An explicit model is required by the API key model policy")
		}
	}
	for _, model := range models {
		if !apiKey.AllowsPublicModel(model) {
			return deny(fmt.Sprintf("Model %q is not available for this API key", model))
		}
	}
	return nil
}
