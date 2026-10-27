//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// Only use an isolated PostgreSQL server, with its administrator URI in the
// named file. Fixture tables live in a new database; preexisting gate roles
// abort the test so it cannot mutate an existing infrastructure installation.
func TestPostgresPermissionGateWithExecuteOnlyRole(t *testing.T) {
	path := os.Getenv("DIRECT_ACCESS_TEST_DSN_FILE")
	if path == "" {
		t.Skip("set DIRECT_ACCESS_TEST_DSN_FILE for isolated PostgreSQL acceptance")
	}
	dsn, err := readDSN(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("test DSN must be a PostgreSQL URI")
	}
	server, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal("open isolated server")
	}
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := server.PingContext(ctx); err != nil {
		t.Fatal("isolated PostgreSQL unavailable")
	}
	var existing bool
	if err := server.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname IN ('moshu_direct_access_owner','moshu_direct_access_auth'))`).Scan(&existing); err != nil || existing {
		t.Fatal("test requires isolated server without direct-access roles")
	}
	var token [12]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	database := "direct_gate_test_" + hex.EncodeToString(token[:6])
	password := hex.EncodeToString(token[:])
	if _, err := server.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(database)); err != nil {
		t.Fatalf("create fixture database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = server.Exec("DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(database) + " WITH (FORCE)")
		for _, role := range []string{"moshu_direct_access_auth", "moshu_direct_access_owner"} {
			if _, err := server.Exec("DROP ROLE IF EXISTS " + pq.QuoteIdentifier(role)); err != nil {
				t.Errorf("cleanup fixture role: %v", err)
			}
		}
	})
	u.Path = "/" + database
	admin, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("open fixture database")
	}
	t.Cleanup(func() { _ = admin.Close() })
	exec := func(statement string) {
		t.Helper()
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("fixture update: %v", err)
		}
	}
	exec(`CREATE TABLE public.users (id bigint PRIMARY KEY, status text NOT NULL, deleted_at timestamptz, password_hash text);
CREATE TABLE public.api_keys (key text UNIQUE NOT NULL, user_id bigint NOT NULL, status text NOT NULL, deleted_at timestamptz, expires_at timestamptz, name text);
CREATE TABLE public.user_attribute_definitions (id bigint PRIMARY KEY, key text UNIQUE NOT NULL, type text NOT NULL, enabled boolean NOT NULL, deleted_at timestamptz);
CREATE TABLE public.user_attribute_values (user_id bigint NOT NULL, attribute_id bigint NOT NULL, value text, UNIQUE(user_id,attribute_id));
INSERT INTO public.users VALUES (1,'active',NULL,'not-readable');
INSERT INTO public.api_keys VALUES ('secret-test-key',1,'active',NULL,NULL,'not-readable');
INSERT INTO public.user_attribute_definitions VALUES (1,'moshu_direct_access','multi_select',true,NULL);
INSERT INTO public.user_attribute_values VALUES (1,1,'["enabled"]');`)
	canonical, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "direct-access", "authorization.sql"))
	if err != nil {
		t.Fatal("read canonical authorization.sql")
	}
	exec(string(canonical))
	exec("ALTER ROLE moshu_direct_access_auth PASSWORD " + pq.QuoteLiteral(password))
	u.User = url.UserPassword("moshu_direct_access_auth", password)
	restricted, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal("open restricted fixture connection")
	}
	t.Cleanup(func() { _ = restricted.Close() })
	restricted.SetMaxOpenConns(2)
	s := sqlStore{db: restricted}
	h := accessHandler{store: s}
	check := func(want int) {
		t.Helper()
		if got := authRequest(h).Code; got != want {
			t.Fatalf("expected %d, got %d", want, got)
		}
	}
	check(http.StatusNoContent)
	for _, forbidden := range []string{
		"SELECT key FROM public.api_keys", "SELECT id FROM public.users",
		"SELECT value FROM public.user_attribute_values", "SELECT key FROM public.user_attribute_definitions",
		"SELECT password_hash FROM public.users", "UPDATE public.users SET status='disabled' WHERE id=1",
		"CREATE TABLE public.unwanted (id bigint)", "SET ROLE moshu_direct_access_owner",
	} {
		if _, err := restricted.ExecContext(ctx, forbidden); err == nil {
			t.Fatalf("execute-only gate role accepted forbidden SQL: %s", forbidden)
		}
	}
	for _, tc := range []struct{ name, change, restore string }{
		{"revoked immediately", `UPDATE public.user_attribute_values SET value='[]'`, `UPDATE public.user_attribute_values SET value='["enabled"]'`},
		{"definition disabled", `UPDATE public.user_attribute_definitions SET enabled=false`, `UPDATE public.user_attribute_definitions SET enabled=true`},
		{"definition deleted", `UPDATE public.user_attribute_definitions SET deleted_at=now()`, `UPDATE public.user_attribute_definitions SET deleted_at=NULL`},
		{"definition wrong type", `UPDATE public.user_attribute_definitions SET type='text'`, `UPDATE public.user_attribute_definitions SET type='multi_select'`},
		{"user disabled", `UPDATE public.users SET status='disabled'`, `UPDATE public.users SET status='active'`},
		{"user deleted", `UPDATE public.users SET deleted_at=now()`, `UPDATE public.users SET deleted_at=NULL`},
		{"key disabled", `UPDATE public.api_keys SET status='disabled'`, `UPDATE public.api_keys SET status='active'`},
		{"key deleted", `UPDATE public.api_keys SET deleted_at=now()`, `UPDATE public.api_keys SET deleted_at=NULL`},
		{"key expired", `UPDATE public.api_keys SET expires_at=now()-interval '1 minute'`, `UPDATE public.api_keys SET expires_at=NULL`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exec(tc.change)
			check(http.StatusForbidden)
			exec(tc.restore)
			check(http.StatusNoContent)
		})
	}
	for i, value := range []string{"", `null`, `[]`, `true`, `"enabled"`, `enabled`, `["enabled","enabled"]`, `["enabled","other"]`, `["Enabled"]`, `["enabled "]`, `[true]`, `[1]`, `{ "enabled": true }`, `["enabled"] junk`} {
		t.Run(fmt.Sprintf("strict option %d", i), func(t *testing.T) {
			if _, err := admin.ExecContext(ctx, "UPDATE public.user_attribute_values SET value=$1", value); err != nil {
				t.Fatal("set synthetic permission value")
			}
			check(http.StatusForbidden)
		})
	}
	exec(`UPDATE public.user_attribute_values SET value='  [ "enabled" ]  '`)
	check(http.StatusNoContent)
	if ok, err := s.Allowed(ctx, "secret-test-key' OR 1=1 --"); ok || err != nil {
		t.Fatal("unknown key/query injection accepted")
	}
	exec("REVOKE EXECUTE ON FUNCTION public.moshu_direct_access_allowed(text) FROM moshu_direct_access_auth")
	w := authRequest(h)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "secret-test-key") {
		t.Fatal("database failure did not fail closed safely")
	}
}
