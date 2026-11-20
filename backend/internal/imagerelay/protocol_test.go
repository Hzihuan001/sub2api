package imagerelay

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

var fixtureNow = time.Unix(1_795_290_000, 0)

func fixtureBinding() Binding {
	return Binding{
		OperationID:  "0123456789abcdef0123456789abcdef",
		Source:       "l1",
		Destination:  "main",
		MainAPIKeyID: 23,
		Method:       http.MethodPost,
		Path:         "/v1/images/edits",
		ContentType:  "multipart/form-data; boundary=relay-test",
		BodySHA256:   SHA256Hex([]byte("multipart bytes")),
	}
}

func fixtureMetadata() Metadata {
	body := []byte("normalized response body")
	return Metadata{
		BodyBytes: int64(len(body)), BodySHA256: SHA256Hex(body),
		ImageCount: 2, ImageSizes: []string{"4096x2160", "1024x1024"},
		Model: "gpt-image-2", Usage: Usage{InputTokens: 12, ImageInputTokens: 4, ImageOutputTokens: 31},
	}
}

func newFixtureSigner(t *testing.T) *Signer {
	t.Helper()
	signer, err := NewSigner("current", map[string][]byte{"current": bytes.Repeat([]byte{0xa2}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestEnvelopeRoundTripLongRunningAndKeyRotation(t *testing.T) {
	newKey := bytes.Repeat([]byte{0x94}, 32)
	keys := map[string][]byte{"old": bytes.Repeat([]byte{0x13}, 32), "new": newKey}
	l1, err := NewSigner("old", keys)
	if err != nil {
		t.Fatal(err)
	}
	main, err := NewSigner("new", keys)
	if err != nil {
		t.Fatal(err)
	}
	newKey[0] ^= 0xff
	delete(keys, "old")

	headers := make(http.Header)
	sent, err := l1.SignRequest(headers, fixtureBinding(), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	expected := fixtureBinding()
	expected.OperationID = ""
	req, err := main.VerifyRequest(headers, expected, fixtureNow.Add(time.Second))
	if err != nil || req != sent {
		t.Fatalf("VerifyRequest = %v, %v", req, err)
	}
	response := make(http.Header)
	meta := fixtureMetadata()
	later := fixtureNow.Add(20 * time.Minute)
	if err := main.SignResult(response, req, meta, later); err != nil {
		t.Fatal(err)
	}
	actual, err := l1.VerifyResult(response, sent, later.Add(time.Second))
	if err != nil || !reflect.DeepEqual(actual, meta) {
		t.Fatalf("VerifyResult = %+v, %v", actual, err)
	}
	if _, err := l1.VerifyResult(response, req, later.Add(MaxResultAge+time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired receipt: %v", err)
	}
}

func TestEnvelopeRejectsRequestMismatch(t *testing.T) {
	s := newFixtureSigner(t)
	header := make(http.Header)
	req, err := s.SignRequest(header, fixtureBinding(), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, mismatched := range []func(*Binding){
		func(b *Binding) { b.OperationID = "1123456789abcdef0123456789abcdef" },
		func(b *Binding) { b.Source = "other-l1" },
		func(b *Binding) { b.Destination = "other-main" },
		func(b *Binding) { b.MainAPIKeyID++ },
		func(b *Binding) { b.Method = http.MethodGet },
		func(b *Binding) { b.Path = "/v1/images/generations" },
		func(b *Binding) { b.ContentType = "multipart/form-data; boundary=other" },
		func(b *Binding) { b.BodySHA256 = SHA256Hex([]byte("different")) },
	} {
		expected := req.Binding
		mismatched(&expected)
		if _, err := s.VerifyRequest(header, expected, fixtureNow); !errors.Is(err, ErrInvalidBinding) {
			t.Fatalf("mismatching request accepted: %v", err)
		}
	}
	for _, when := range []time.Time{
		fixtureNow.Add(MaxRequestAge + time.Second),
		fixtureNow.Add(-MaxClockSkew - time.Second),
	} {
		if _, err := s.VerifyRequest(header, req.Binding, when); !errors.Is(err, ErrExpired) {
			t.Fatalf("request outside time window: %v", err)
		}
	}
	resp := make(http.Header)
	if err := s.SignResult(resp, req, fixtureMetadata(), fixtureNow); err != nil {
		t.Fatal(err)
	}
	req.OperationID = "1123456789abcdef0123456789abcdef"
	if _, err := s.VerifyResult(resp, req, fixtureNow); !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("receipt for other operation: %v", err)
	}
}

func TestEnvelopeRejectsAmbiguousAndTamperedValues(t *testing.T) {
	s := newFixtureSigner(t)
	h := make(http.Header)
	req, err := s.SignRequest(h, fixtureBinding(), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	tampered := h.Get(RequestHeader)
	replacement := byte('0')
	if tampered[len(tampered)-1] == replacement {
		replacement = '1'
	}
	good := h.Get(RequestHeader)

	// Even a valid MAC must not allow loosely parsed JSON or cross-header reuse.
	noncanonical := string(raw[:len(raw)-1]) + ",\"issued_at\":" + string(raw[strings.LastIndex(string(raw), ":")+1:])
	prefix := "v1.current." + base64.RawURLEncoding.EncodeToString([]byte(noncanonical))
	for _, headers := range []http.Header{
		{RequestHeader: {tampered[:len(tampered)-1] + string(replacement)}},
		{RequestHeader: {good, good}},
		{RequestHeader: {good}, strings.ToLower(RequestHeader): {good}},
		{RequestHeader: {strings.Repeat("a", MaxHeaderBytes+1)}},
		{RequestHeader: {prefix + "." + hex.EncodeToString(computeMAC(s.secrets["current"], RequestHeader, prefix))}},
	} {
		if _, err := s.VerifyRequest(headers, fixtureBinding(), fixtureNow); !errors.Is(err, ErrInvalidEnvelope) {
			t.Fatalf("invalid/ambiguous envelope error = %v", err)
		}
	}
	if _, err := s.VerifyResult(http.Header{ResultHeader: {good}}, req, fixtureNow); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("cross-header envelope error = %v", err)
	}
}

func TestMetadataValidationAndHeaderBoundary(t *testing.T) {
	s := newFixtureSigner(t)
	h := make(http.Header)
	req, err := s.SignRequest(h, fixtureBinding(), fixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalidate := range []func(*Metadata){
		func(m *Metadata) { m.BodyBytes = MaxResultBodyBytes + 1 },
		func(m *Metadata) { m.BodySHA256 = "" },
		func(m *Metadata) { m.ImageCount = -1 },
		func(m *Metadata) { m.ImageCount = MaxImageCount + 1 },
		func(m *Metadata) { m.ImageCount = 0 },
		func(m *Metadata) { m.ImageSizes = []string{"65536x1200"} },
		func(m *Metadata) { m.Usage.OutputTokens = -1 },
		func(m *Metadata) { m.Model = "foo\r\nbar" },
	} {
		meta := fixtureMetadata()
		invalidate(&meta)
		if err := s.SignResult(h, req, meta, fixtureNow); !errors.Is(err, ErrInvalidMetadata) {
			t.Fatalf("invalid metadata accepted: %+v (%v)", meta, err)
		}
	}
	h.Set("Content-Type", "application/json")
	h.Set(HeaderPrefix, "reserved")
	h[strings.ToLower(ResultHeader)] = []string{"reserved"}
	h["X-Sub2api-Image-Relay-Unknown"] = []string{"reserved"}
	if !HasRequest(h) || !HasResult(h) {
		t.Fatal("reserved headers should be found regardless of case")
	}
	StripHeaders(h)
	if len(h) != 1 || h.Get("Content-Type") != "application/json" || HasRequest(h) || HasResult(h) {
		t.Fatalf("unexpected headers after stripping: %v", h)
	}
}

func TestRejectInvalidSignerAndOperationIDs(t *testing.T) {
	for _, key := range []string{"missing", ".", "old.key"} {
		if _, err := NewSigner(key, map[string][]byte{"old.key": bytes.Repeat([]byte{5}, 32)}); !errors.Is(err, ErrKeyConfig) {
			t.Fatalf("invalid key %q: %v", key, err)
		}
	}
	if _, err := NewSigner("short", map[string][]byte{"short": []byte("too short")}); !errors.Is(err, ErrKeyConfig) {
		t.Fatalf("short key: %v", err)
	}
	one, err := NewOperationID()
	if err != nil || !operationPattern.MatchString(one) {
		t.Fatalf("invalid operation ID, %v", err)
	}
	two, err := NewOperationID()
	if err != nil || !operationPattern.MatchString(two) || one == two {
		t.Fatalf("invalid second operation ID, %v", err)
	}
}
