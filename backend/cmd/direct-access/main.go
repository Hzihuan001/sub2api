// direct-access is the optional, read-only Nginx auth_request gate for the
// dedicated direct ingress. It never proxies model requests or replaces the
// application's API key, quota, balance or IP checks.
package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

const (
	maxCredentialBytes = 128 // matches the main application's API key limit
	queryTimeout       = 2 * time.Second
)

// The gate role can execute this boolean-only function but cannot select any
// application tables. authorization.sql owns the exact permission semantics.
const permissionQuery = `SELECT public.moshu_direct_access_allowed($1)`

type accessStore interface {
	Allowed(context.Context, string) (bool, error)
	Ping(context.Context) error
}

type sqlStore struct{ db *sql.DB }

func (s sqlStore) Allowed(ctx context.Context, key string) (bool, error) {
	var allowed bool
	err := s.db.QueryRowContext(ctx, permissionQuery, key).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed, nil
}

func (s sqlStore) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

type accessHandler struct{ store accessStore }

func (h accessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path != "/auth" && r.URL.Path != "/healthz" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	if r.URL.Path == "/healthz" {
		if h.store == nil || h.store.Ping(ctx) != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
		return
	}
	key, err := requestCredential(r)
	if err != nil {
		http.Error(w, "direct access denied", http.StatusUnauthorized)
		return
	}
	if h.store == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	allowed, err := h.store.Allowed(ctx, key)
	if err != nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		http.Error(w, "direct access denied", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errCredential = errors.New("invalid credentials")

// Read only headers and the original URI supplied by the internal Nginx
// location. In particular, this must never read the potentially large body.
func requestCredential(r *http.Request) (string, error) {
	var chosen string
	add := func(value string) error {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > maxCredentialBytes {
			return errCredential
		}
		for _, b := range []byte(value) {
			if b < 0x21 || b > 0x7e || b == ',' {
				return errCredential
			}
		}
		if chosen != "" && chosen != value {
			return errCredential
		}
		chosen = value
		return nil
	}
	for _, name := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key"} {
		values := headerValues(r.Header, name)
		if len(values) > 1 {
			return "", errCredential
		}
		if len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if name == "Authorization" {
			parts := strings.Fields(value)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return "", errCredential
			}
			value = parts[1]
		}
		if err := add(value); err != nil {
			return "", err
		}
	}

	// Query credentials are accepted only where the main Google auth accepts
	// them. Never interpret the auth subrequest's own query as the original URL.
	originalURIs := headerValues(r.Header, "X-Original-URI")
	if len(originalURIs) > 1 {
		return "", errCredential
	}
	if len(originalURIs) == 1 {
		raw := originalURIs[0]
		if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
			return "", errCredential
		}
		u, err := url.ParseRequestURI(raw)
		if err != nil {
			return "", errCredential
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "", errCredential
		}
		if len(query["api_key"]) > 1 || len(query["key"]) > 1 {
			return "", errCredential
		}
		if strings.TrimSpace(query.Get("api_key")) != "" {
			return "", errCredential
		}
		if key := strings.TrimSpace(query.Get("key")); key != "" {
			if !strings.HasPrefix(u.Path, "/v1beta") && !strings.HasPrefix(u.Path, "/antigravity/v1beta") {
				return "", errCredential
			}
			if err := add(key); err != nil {
				return "", err
			}
		}
	}
	if chosen == "" {
		return "", errCredential
	}
	return chosen, nil
}

func headerValues(headers http.Header, name string) []string {
	var out []string
	for key, values := range headers {
		if strings.EqualFold(key, name) {
			out = append(out, values...)
		}
	}
	return out
}

func readDSN(path string) (string, error) {
	if path == "" {
		return "", errors.New("DIRECT_ACCESS_DSN_FILE is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("cannot open database secret file")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(b) > 65536 || strings.TrimSpace(string(b)) == "" {
		return "", errors.New("invalid database secret file")
	}
	return strings.TrimSpace(string(b)), nil
}

func run(ctx context.Context) error {
	dsn, err := readDSN(os.Getenv("DIRECT_ACCESS_DSN_FILE"))
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return errors.New("invalid database configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(time.Minute)
	addr := os.Getenv("DIRECT_ACCESS_LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18090"
	}
	srv := &http.Server{
		Addr: addr, Handler: accessHandler{store: sqlStore{db: db}},
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10,
	}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP server failed")
	case <-ctx.Done():
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(stop)
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
