// Package imagerelay implements the synchronous image relay contract between
// configured Sub2API sites.
package imagerelay

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	HeaderPrefix       = "X-Sub2api-Image-Relay"
	RequestHeader      = HeaderPrefix + "-Request"
	ResultHeader       = HeaderPrefix + "-Result"
	ProtocolVersion    = "v1"
	MaxHeaderBytes     = 4096
	MaxImageCount      = 32
	MaxResultBodyBytes = 128 << 20
	MaxClockSkew       = 30 * time.Second
	MaxRequestAge      = 2 * time.Minute
	MaxResultAge       = 2 * time.Minute
)

var (
	ErrMissingHeader   = errors.New("image relay header is missing")
	ErrInvalidEnvelope = errors.New("invalid image relay envelope")
	ErrInvalidBinding  = errors.New("invalid image relay request binding")
	ErrInvalidMetadata = errors.New("invalid image relay result metadata")
	ErrKeyConfig       = errors.New("invalid image relay signing key configuration")
	ErrExpired         = errors.New("image relay envelope is outside its time window")

	identityPattern  = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$")
	operationPattern = regexp.MustCompile("^[a-f0-9]{32}$")
	digestPattern    = regexp.MustCompile("^[a-f0-9]{64}$")
	sizePattern      = regexp.MustCompile("^([1-9][0-9]{0,4})x([1-9][0-9]{0,4})$")
)

// Binding describes the final outbound image request and its destination key.
// BodySHA256 covers the bytes sent after model remapping. ContentType includes
// the final boundary for multipart edits.
type Binding struct {
	OperationID  string `json:"operation_id"`
	Source       string `json:"source"`
	Destination  string `json:"destination"`
	MainAPIKeyID int64  `json:"main_key_id"`
	Method       string `json:"method"`
	Path         string `json:"path"`
	ContentType  string `json:"content_type"`
	BodySHA256   string `json:"body_sha256"`
}

type Request struct {
	Binding
	IssuedAt int64 `json:"issued_at"`
}

// Usage carries observed token counts for the receiving site's own pricing.
type Usage struct {
	InputTokens                     int  `json:"input_tokens,omitempty"`
	ImageInputTokens                int  `json:"image_input_tokens,omitempty"`
	ImageCacheReadTokens            int  `json:"image_cache_read_tokens,omitempty"`
	OutputTokens                    int  `json:"output_tokens,omitempty"`
	CacheCreationInputTokens        int  `json:"cache_creation_input_tokens,omitempty"`
	CacheCreationInputTokensPresent bool `json:"cache_creation_input_tokens_present,omitempty"`
	CacheCreation5mTokens           int  `json:"cache_creation_5m_tokens,omitempty"`
	CacheCreation1hTokens           int  `json:"cache_creation_1h_tokens,omitempty"`
	CacheCreation30mInputTokens     int  `json:"cache_creation_30m_input_tokens,omitempty"`
	CacheCreation12hInputTokens     int  `json:"cache_creation_12h_input_tokens,omitempty"`
	CacheReadInputTokens            int  `json:"cache_read_input_tokens,omitempty"`
	ImageOutputTokens               int  `json:"image_output_tokens,omitempty"`
}

// Metadata binds the exact encoded response to the observed image data.
// ImageCount is the observed count before either site applies a billing fallback.
type Metadata struct {
	BodyBytes  int64    `json:"body_bytes"`
	BodySHA256 string   `json:"body_sha256"`
	ImageCount int      `json:"image_count"`
	ImageSizes []string `json:"image_sizes,omitempty"`
	Model      string   `json:"model"`
	Usage      Usage    `json:"usage"`
}

type result struct {
	Request  Request  `json:"request"`
	IssuedAt int64    `json:"issued_at"`
	Metadata Metadata `json:"metadata"`
}

// Signer can be used concurrently. All configured keys can verify envelopes.
// New envelopes use the configured current key.
type Signer struct {
	current string
	secrets map[string][]byte
}

func NewSigner(current string, keys map[string][]byte) (*Signer, error) {
	if !identityPattern.MatchString(current) || len(keys) == 0 || len(keys) > 4 {
		return nil, ErrKeyConfig
	}
	secrets := make(map[string][]byte, len(keys))
	for id, key := range keys {
		if !identityPattern.MatchString(id) || strings.Contains(id, ".") || len(key) < 32 || len(key) > 128 {
			return nil, ErrKeyConfig
		}
		secrets[id] = bytes.Clone(key)
	}
	if _, ok := secrets[current]; !ok {
		return nil, ErrKeyConfig
	}
	return &Signer{current: current, secrets: secrets}, nil
}

func NewOperationID() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

func SHA256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// SignRequest sets the negotiation header after the caller has fixed the
// outbound HTTP method, route, content type and body.
func (s *Signer) SignRequest(header http.Header, binding Binding, now time.Time) (Request, error) {
	if !validBinding(binding) || now.Unix() <= 0 {
		return Request{}, ErrInvalidBinding
	}
	req := Request{Binding: binding, IssuedAt: now.Unix()}
	if err := s.seal(header, RequestHeader, req); err != nil {
		return Request{}, err
	}
	return req, nil
}

// VerifyRequest checks the authenticated request against the expected
// identities, destination key ID and actual HTTP request. Leave
// expected.OperationID empty to accept any valid operation ID. Claim the
// returned operation separately before sending it to a provider.
func (s *Signer) VerifyRequest(header http.Header, expected Binding, now time.Time) (Request, error) {
	var req Request
	if err := s.open(header, RequestHeader, &req); err != nil {
		return Request{}, err
	}
	if expected.OperationID == "" {
		expected.OperationID = req.OperationID
	}
	if !validBinding(req.Binding) || req.Binding != expected {
		return Request{}, ErrInvalidBinding
	}
	if !fresh(req.IssuedAt, now, MaxRequestAge) {
		return Request{}, ErrExpired
	}
	return req, nil
}

// SignResult sets the response header after the final JSON bytes and observed
// usage are known. The receipt attests the result independently of settlement.
func (s *Signer) SignResult(header http.Header, req Request, meta Metadata, now time.Time) error {
	if !validBinding(req.Binding) || !validOrder(req, now.Unix()) {
		return ErrInvalidBinding
	}
	if !validMetadata(meta) {
		return ErrInvalidMetadata
	}
	return s.seal(header, ResultHeader, result{Request: req, IssuedAt: now.Unix(), Metadata: meta})
}

// VerifyResult authenticates the receipt. The caller verifies HTTP status,
// framing and the body byte count and digest during delivery.
func (s *Signer) VerifyResult(header http.Header, req Request, now time.Time) (Metadata, error) {
	var got result
	if err := s.open(header, ResultHeader, &got); err != nil {
		return Metadata{}, err
	}
	if !validBinding(req.Binding) || got.Request != req || !validOrder(req, got.IssuedAt) {
		return Metadata{}, ErrInvalidBinding
	}
	if !fresh(got.IssuedAt, now, MaxResultAge) {
		return Metadata{}, ErrExpired
	}
	if !validMetadata(got.Metadata) {
		return Metadata{}, ErrInvalidMetadata
	}
	return got.Metadata, nil
}

func fresh(issuedAt int64, now time.Time, maxAge time.Duration) bool {
	return issuedAt > 0 && issuedAt >= now.Add(-maxAge).Unix() && issuedAt <= now.Add(MaxClockSkew).Unix()
}

func validOrder(req Request, resultIssuedAt int64) bool {
	return resultIssuedAt > 0 && req.IssuedAt > 0 && req.IssuedAt-int64(MaxClockSkew/time.Second) <= resultIssuedAt
}

func validBinding(b Binding) bool {
	if !operationPattern.MatchString(b.OperationID) || !identityPattern.MatchString(b.Source) ||
		!identityPattern.MatchString(b.Destination) || strings.EqualFold(b.Source, b.Destination) ||
		b.MainAPIKeyID <= 0 || b.Method != http.MethodPost || !digestPattern.MatchString(b.BodySHA256) ||
		!visibleASCII(b.ContentType, 256) {
		return false
	}
	if b.Path != "/v1/images/generations" && b.Path != "/v1/images/edits" {
		return false
	}
	format, params, err := mime.ParseMediaType(b.ContentType)
	if err != nil {
		return false
	}
	switch format {
	case "application/json":
		return true
	case "multipart/form-data":
		boundary := params["boundary"]
		return b.Path == "/v1/images/edits" && boundary != "" && len(boundary) <= 70
	default:
		return false
	}
}

func validMetadata(m Metadata) bool {
	if m.BodyBytes <= 0 || m.BodyBytes > MaxResultBodyBytes || !digestPattern.MatchString(m.BodySHA256) ||
		m.ImageCount < 0 || m.ImageCount > MaxImageCount || len(m.ImageSizes) > m.ImageCount ||
		!visibleASCII(m.Model, 128) {
		return false
	}
	for _, size := range m.ImageSizes {
		if !validSize(size) {
			return false
		}
	}
	for _, n := range []int{
		m.Usage.InputTokens, m.Usage.ImageInputTokens, m.Usage.ImageCacheReadTokens, m.Usage.OutputTokens,
		m.Usage.CacheCreationInputTokens, m.Usage.CacheCreation5mTokens, m.Usage.CacheCreation1hTokens,
		m.Usage.CacheCreation30mInputTokens, m.Usage.CacheCreation12hInputTokens, m.Usage.CacheReadInputTokens,
		m.Usage.ImageOutputTokens,
	} {
		if n < 0 || n > 1_000_000_000 {
			return false
		}
	}
	return true
}

func validSize(size string) bool {
	// These are the existing named output size tiers and the supported WxH form.
	if size == "1K" || size == "2K" || size == "4K" {
		return true
	}
	parts := sizePattern.FindStringSubmatch(size)
	if parts == nil {
		return false
	}
	width, _ := strconv.Atoi(parts[1])
	height, _ := strconv.Atoi(parts[2])
	return width <= 65535 && height <= 65535
}

func visibleASCII(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit || strings.TrimSpace(s) != s {
		return false
	}
	for i := range len(s) {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

func (s *Signer) seal(header http.Header, name string, payload any) error {
	if s == nil || len(s.secrets[s.current]) == 0 {
		return ErrKeyConfig
	}
	if header == nil {
		return ErrInvalidEnvelope
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrInvalidEnvelope
	}
	payloadPrefix := ProtocolVersion + "." + s.current + "." + base64.RawURLEncoding.EncodeToString(raw)
	value := payloadPrefix + "." + hex.EncodeToString(computeMAC(s.secrets[s.current], name, payloadPrefix))
	if len(value) > MaxHeaderBytes {
		return ErrInvalidEnvelope
	}
	for key := range header {
		if strings.EqualFold(key, name) {
			delete(header, key)
		}
	}
	header.Set(name, value)
	return nil
}

func (s *Signer) open(header http.Header, name string, payload any) error {
	if s == nil || len(s.secrets) == 0 {
		return ErrKeyConfig
	}
	value, err := oneHeader(header, name)
	if err != nil {
		return err
	}
	parts := strings.Split(value, ".")
	if len(parts) != 4 || parts[0] != ProtocolVersion || !digestPattern.MatchString(parts[3]) {
		return ErrInvalidEnvelope
	}
	key := s.secrets[parts[1]]
	if len(key) == 0 {
		return ErrInvalidEnvelope
	}
	tag, err := hex.DecodeString(parts[3])
	if err != nil || !hmac.Equal(tag, computeMAC(key, name, value[:len(value)-sha256.Size*2-1])) {
		return ErrInvalidEnvelope
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || json.Unmarshal(raw, payload) != nil {
		return ErrInvalidEnvelope
	}
	canonical, err := json.Marshal(payload)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ErrInvalidEnvelope
	}
	return nil
}

func computeMAC(key []byte, name, payloadPrefix string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(name))
	mac.Write([]byte{0})
	mac.Write([]byte(payloadPrefix))
	return mac.Sum(nil)
}

func oneHeader(h http.Header, name string) (string, error) {
	var value string
	for key, values := range h {
		if !strings.EqualFold(key, name) {
			continue
		}
		if value != "" || len(values) != 1 || len(values[0]) == 0 || len(values[0]) > MaxHeaderBytes {
			return "", ErrInvalidEnvelope
		}
		value = values[0]
	}
	if value == "" {
		return "", ErrMissingHeader
	}
	return value, nil
}

func hasHeader(h http.Header, name string) bool {
	for key := range h {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func HasRequest(h http.Header) bool { return hasHeader(h, RequestHeader) }
func HasResult(h http.Header) bool  { return hasHeader(h, ResultHeader) }

func IsReservedHeader(name string) bool {
	lower := strings.ToLower(name)
	prefix := strings.ToLower(HeaderPrefix)
	return lower == prefix || strings.HasPrefix(lower, prefix+"-")
}

// StripHeaders deletes the protocol namespace, including noncanonical spellings.
func StripHeaders(h http.Header) {
	for key := range h {
		if IsReservedHeader(key) {
			delete(h, key)
		}
	}
}
