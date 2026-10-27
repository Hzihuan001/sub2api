package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestRequestCredential(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers http.Header
		uri     string
		want    string
	}{
		{"bearer", http.Header{"Authorization": {"Bearer sk-test"}}, "/v1/responses", "sk-test"},
		{"case insensitive bearer", http.Header{"Authorization": {"bEaReR sk-test"}}, "", "sk-test"},
		{"x api", http.Header{"X-Api-Key": {"sk-test"}}, "", "sk-test"},
		{"google", http.Header{"X-Goog-Api-Key": {"sk-test"}}, "", "sk-test"},
		{"same sources", http.Header{"Authorization": {"Bearer sk-test"}, "X-Api-Key": {"sk-test"}}, "", "sk-test"},
		{"native query", nil, "/v1beta/models/gemini:generateContent?key=sk-test", "sk-test"},
		{"antigravity query", nil, "/antigravity/v1beta/models/x:generateContent?key=sk-test", "sk-test"},
		{"query agrees", http.Header{"X-Api-Key": {"sk-test"}}, "/v1beta/models/x?key=sk-test", "sk-test"},
		{"missing", nil, "", ""},
		{"bad bearer", http.Header{"Authorization": {"Basic sk-test"}}, "", ""},
		{"empty", http.Header{"X-Api-Key": {" "}}, "", ""},
		{"header conflict", http.Header{"Authorization": {"Bearer sk-test"}, "X-Goog-Api-Key": {"sk-other"}}, "", ""},
		{"duplicate header", http.Header{"Authorization": {"Bearer sk-test", "Bearer sk-test"}}, "", ""},
		{"mixed case duplicate", http.Header{"X-Api-Key": {"sk-test"}, "x-api-key": {"sk-test"}}, "", ""},
		{"combined header", http.Header{"X-Api-Key": {"sk-test, sk-test"}}, "", ""},
		{"oversized", http.Header{"X-Api-Key": {strings.Repeat("a", 129)}}, "", ""},
		{"control character", http.Header{"X-Api-Key": {"sk-\x00test"}}, "", ""},
		{"header query conflict", http.Header{"X-Api-Key": {"sk-test"}}, "/v1beta/models/x?key=sk-other", ""},
		{"duplicate query", nil, "/v1beta/models/x?key=sk-test&key=sk-test", ""},
		{"deprecated query", nil, "/v1beta/models/x?api_key=sk-test", ""},
		{"ordinary query forbidden", nil, "/v1/responses?key=sk-test", ""},
		{"absolute URI forbidden", http.Header{"X-Api-Key": {"sk-test"}}, "https://evil.test/v1beta?key=sk-test", ""},
		{"invalid URI", http.Header{"X-Api-Key": {"sk-test"}}, "/v1beta?key=%zz", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/auth", nil)
			if tc.headers != nil {
				r.Header = tc.headers.Clone()
			}
			if tc.uri != "" {
				r.Header.Set("X-Original-URI", tc.uri)
			}
			got, err := requestCredential(r)
			if tc.want == "" {
				if err == nil {
					t.Fatal("expected rejection")
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
	r := httptest.NewRequest(http.MethodGet, "/auth?key=sk-test", nil)
	if _, err := requestCredential(r); err == nil {
		t.Fatal("auth URL query must not supply original credentials")
	}
	r.Header["X-Original-Uri"] = []string{"/v1beta?key=sk-test", "/v1beta?key=sk-test"}
	if _, err := requestCredential(r); err == nil {
		t.Fatal("duplicate original URI accepted")
	}
}

type fakeStore struct {
	allowed   bool
	err       error
	calls     int
	pingCalls int
	lastKey   string
	deadline  time.Duration
}

func (s *fakeStore) Allowed(ctx context.Context, key string) (bool, error) {
	s.calls++
	s.lastKey = key
	if deadline, ok := ctx.Deadline(); ok {
		s.deadline = time.Until(deadline)
	}
	return s.allowed, s.err
}
func (s *fakeStore) Ping(context.Context) error { s.pingCalls++; return s.err }

type panicBody struct{}

func (panicBody) Read([]byte) (int, error) { panic("auth gate must not read request body") }
func (panicBody) Close() error             { return nil }

func authRequest(h http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/auth", nil)
	r.Header.Set("Authorization", "Bearer secret-test-key")
	r.Body = panicBody{}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHandlerRevocationAndFailures(t *testing.T) {
	s := &fakeStore{allowed: true}
	h := accessHandler{store: s}
	if w := authRequest(h); w.Code != http.StatusNoContent {
		t.Fatalf("first request: %d", w.Code)
	}
	s.allowed = false // The next request must recheck the database, not an allow cache.
	if w := authRequest(h); w.Code != http.StatusForbidden {
		t.Fatalf("revoked request: %d", w.Code)
	}
	s.allowed, s.err = true, errors.New("database error containing secret-test-key")
	w := authRequest(h)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret-test-key") {
		t.Fatalf("unsafe failure: %d %q", w.Code, w.Body.String())
	}
	if s.calls != 3 || s.lastKey != "secret-test-key" || s.deadline <= 0 || s.deadline > queryTimeout {
		t.Fatalf("unexpected calls/deadline: %+v", s)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authorization responses must not be cached")
	}
	if w := authRequest(accessHandler{}); w.Code != http.StatusServiceUnavailable {
		t.Fatal("missing store did not fail closed")
	}
}

func TestHandlerRoutesAndNoBodyRead(t *testing.T) {
	s := &fakeStore{allowed: true}
	h := accessHandler{store: s}
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", 200},
		{http.MethodPost, "/auth", 405},
		{http.MethodHead, "/auth", 405},
		{http.MethodGet, "/not-found", 404},
		{http.MethodGet, "/auth", 401},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Body = panicBody{}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s: got %d", tc.method, tc.path, w.Code)
		}
	}
	if s.calls != 0 || s.pingCalls != 1 {
		t.Fatal("invalid request reached permission database")
	}
	s.err = errors.New("offline")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != 503 {
		t.Fatal("unhealthy database reported healthy")
	}
}

func TestSQLStoreParameterizedAndStrict(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := sqlStore{db: db}
	for _, allowed := range []bool{true, false} {
		mock.ExpectQuery(regexp.QuoteMeta(permissionQuery)).WithArgs("sk-' secret").
			WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(allowed))
		got, err := s.Allowed(context.Background(), "sk-' secret")
		if err != nil || got != allowed {
			t.Fatalf("permission %t: %v %v", allowed, got, err)
		}
	}
	mock.ExpectQuery(regexp.QuoteMeta(permissionQuery)).WithArgs("null").WillReturnRows(sqlmock.NewRows([]string{"allowed"}).AddRow(nil))
	if ok, err := s.Allowed(context.Background(), "null"); ok || err == nil {
		t.Fatal("unexpected NULL must fail closed")
	}
	mock.ExpectQuery(regexp.QuoteMeta(permissionQuery)).WithArgs("offline").WillReturnError(io.ErrUnexpectedEOF)
	if ok, err := s.Allowed(context.Background(), "offline"); ok || err == nil {
		t.Fatal("database error must fail closed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReadDSNOnlyFromFile(t *testing.T) {
	if _, err := readDSN(""); err == nil {
		t.Fatal("missing secret path accepted")
	}
	if _, err := readDSN(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte("  postgres://test-secret@db/test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := readDSN(path); err != nil || got != "postgres://test-secret@db/test" {
		t.Fatal("valid secret file rejected")
	}
	for _, raw := range []string{" \n", strings.Repeat("a", 65537)} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readDSN(path); err == nil {
			t.Fatal("invalid secret file accepted")
		}
	}
}
