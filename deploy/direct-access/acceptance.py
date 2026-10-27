"""Run isolated Nginx/auth_request/PostgreSQL acceptance with zero model fees.

Run from any directory: python deploy/direct-access/acceptance.py
The project uses only synthetic fixture keys, localhost:18390, an internal Docker
network and its own named volume. It never reads production settings or keys.
The long tests run concurrently (~130 s). The environment is retained for review.
Stop it with the printed `docker compose ... down` command (no volume deletion).
"""
import argparse
import concurrent.futures
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
GENERATED = HERE / ".acceptance"
PROJECT = "moshu-direct-access-acceptance"
COMPOSE = ["docker", "compose", "-p", PROJECT, "-f", str(HERE / "compose.acceptance.yml")]
ALLOWED = "sk-direct-allowed-acceptance-only"
ORDINARY = "sk-direct-ordinary-acceptance-only"
RESULTS = []


def compose(*args, check=True, input_text=None):
    return subprocess.run(COMPOSE + list(args), check=check, input=input_text,
                          text=True, encoding="utf-8", errors="replace", capture_output=True, cwd=HERE)


def sql(statement):
    result = compose("exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1",
                     "-U", "acceptance", "-d", "acceptance", "-At", input_text=statement)
    return result.stdout.strip()


def request(path="/v1/models", key=ALLOWED, headers=None, method="GET", body=None, timeout=20):
    conn = http.client.HTTPConnection("127.0.0.1", 18390, timeout=timeout)
    sent = dict(headers or {})
    if key is not None:
        sent.setdefault("Authorization", "Bearer " + key)
    try:
        conn.request(method, path, body=body, headers=sent)
        response = conn.getresponse()
        raw = response.read()
        try:
            parsed = json.loads(raw)
        except (ValueError, UnicodeDecodeError):
            parsed = None
        return response.status, parsed, raw
    finally:
        conn.close()


def case(name, fn):
    start = time.monotonic()
    try:
        detail = fn()
        result = {"name": name, "passed": True, "seconds": round(time.monotonic() - start, 3),
                  "detail": detail}
    except Exception as exc:
        result = {"name": name, "passed": False, "seconds": round(time.monotonic() - start, 3),
                  "error": str(exc)}
    RESULTS.append(result)
    print(json.dumps(result, ensure_ascii=False), flush=True)
    return result


def expect_status(expected, **kwargs):
    status, parsed, _ = request(**kwargs)
    assert status in expected, "expected %s; received %s" % (expected, status)
    if 200 <= status < 300:
        assert parsed and parsed.get("mock") is True, "request did not reach the isolated mock"
    return {"status": status}


def mutate_and_check(statement, restore, expected=(401, 403)):
    try:
        sql(statement)
        return expect_status(expected)
    finally:
        sql(restore)
        expect_status((200,))


def render_nginx():
    template = (HERE / "nginx.conf.template").read_text(encoding="utf-8")
    replacements = {"__DOMAIN__": "localhost", "__MAIN_ORIGIN__": "http://mock:8080",
                    "__AUTH_ORIGIN__": "http://auth:8080", "__CERT_DIR__": "/unused",
                    "__ACME_ROOT__": "/tmp/acme"}
    for old, new in replacements.items():
        template = template.replace(old, new)
    # The template owns routing/auth/proxy semantics. Only transport changes for
    # the isolated localhost harness: omit marked redirect and TLS directives.
    template = re.sub(r"(?ms)^\s*# HTTP_REDIRECT_START[^\n]*\n.*?^\s*# HTTP_REDIRECT_END\s*$", "", template)
    template = re.sub(r"(?m)^\s*listen\s+\[::\]:443[^;]*;\s*$", "", template)
    template = re.sub(r"(?m)^(\s*)listen\s+443[^;]*;", r"\1listen 80;", template)
    template = re.sub(r"(?m)^\s*(?:ssl_[\w]+|http2)\s+[^;]*;\s*$", "", template)
    assert "ssl_certificate" not in template and "return 301 https" not in template, "unmarked TLS/redirect block"
    assert "auth_request" in template, "must exercise the actual auth_request template"
    assert "__" not in template, "unresolved nginx template placeholder"
    (GENERATED / "nginx.conf").write_text(template, encoding="utf-8")
    (GENERATED / "dsn.txt").write_text(
        "postgres://moshu_direct_access_auth:acceptance-only-not-production@postgres:5432/acceptance?sslmode=disable\n",
        encoding="utf-8")
    (GENERATED / "30-password.sql").write_text(
        "ALTER ROLE moshu_direct_access_auth LOGIN PASSWORD 'acceptance-only-not-production';\n",
        encoding="utf-8")


def ready():
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        try:
            if request()[0] == 200:
                return
        except (OSError, http.client.HTTPException):
            pass
        time.sleep(2)
    raise AssertionError("isolated acceptance ingress not healthy after 90 s")


def ip_headers():
    status, data, _ = request(headers={
        "X-Forwarded-For": "198.51.100.77", "X-Real-IP": "198.51.100.78",
        "CF-Connecting-IP": "198.51.100.79", "Forwarded": "for=198.51.100.80",
        "X-Original-URI": "/v1beta/models?key=" + ORDINARY,
        "X-Direct-Access-Authorized": "true", "X-Direct-User-ID": "999"})
    assert status == 200 and data.get("mock"), "valid key with spoofed headers was blocked"
    got = data["headers"]
    assert got["x-real-ip"] and got["x-real-ip"] != "198.51.100.78", "spoofed X-Real-IP survived"
    assert got["x-forwarded-for"] == got["x-real-ip"], "XFF was appended rather than overwritten"
    assert got["cf-connecting-ip"] == "", "spoofed Cloudflare client address survived"
    assert got["forwarded"] == "", "spoofed Forwarded survived"
    assert got["x-direct-access-authorized"] == "" and got["x-direct-user-id"] == "", "spoofed direct identity survived"
    return {"status": status, "ip_headers_overwritten": True}


def readonly_role():
    result = compose("exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1",
                     "-U", "acceptance", "-d", "acceptance", "-At", check=False,
                     input_text="SET ROLE moshu_direct_access_auth; DELETE FROM api_keys WHERE id = 1;")
    assert result.returncode != 0 and "permission denied" in result.stderr, "sidecar role can mutate keys"
    read = compose("exec", "-T", "postgres", "psql", "-v", "ON_ERROR_STOP=1",
                   "-U", "acceptance", "-d", "acceptance", "-At", check=False,
                   input_text="SET ROLE moshu_direct_access_auth; SELECT key FROM api_keys;")
    assert read.returncode != 0 and "permission denied" in read.stderr, "sidecar role can enumerate keys"
    return {"write_denied": True, "business_table_read_denied": True}


def duplicate_credentials():
    conn = http.client.HTTPConnection("127.0.0.1", 18390, timeout=15)
    try:
        conn.putrequest("GET", "/v1/models")
        conn.putheader("Authorization", "Bearer " + ALLOWED)
        conn.putheader("Authorization", "Bearer " + ORDINARY)
        conn.endheaders()
        response = conn.getresponse()
        response.read()
        assert response.status in (400, 401, 403), "duplicate credential headers were accepted"
        return {"status": response.status}
    finally:
        conn.close()


def large_upload():
    chunk = b"x" * (1 << 20)
    prefix = b'{"acceptance_padding":"'
    suffix = b'"}'
    size = 80 << 20
    conn = http.client.HTTPConnection("127.0.0.1", 18390, timeout=180)
    digest = hashlib.sha256()
    try:
        conn.putrequest("POST", "/v1/responses?mode=echo")
        conn.putheader("Authorization", "Bearer " + ALLOWED)
        conn.putheader("Content-Type", "application/json")
        conn.putheader("Content-Length", str(size))
        conn.endheaders()
        conn.send(prefix)
        digest.update(prefix)
        remaining = size - len(prefix) - len(suffix)
        while remaining:
            part = chunk[:min(len(chunk), remaining)]
            conn.send(part)
            digest.update(part)
            remaining -= len(part)
        conn.send(suffix)
        digest.update(suffix)
        response = conn.getresponse()
        data = json.loads(response.read())
        assert response.status == 200 and data["bytes"] == size, "large upload failed"
        assert data["sha256"] == digest.hexdigest(), "large body changed in transit"
        return {"status": response.status, "bytes": data["bytes"], "hash_matches": True}
    finally:
        conn.close()


def slow_upload():
    body = b'{"input":"slow-upload-acceptance"}'
    conn = http.client.HTTPConnection("127.0.0.1", 18390, timeout=180)
    try:
        conn.putrequest("POST", "/v1/responses?mode=echo")
        conn.putheader("Authorization", "Bearer " + ALLOWED)
        conn.putheader("Content-Type", "application/json")
        conn.putheader("Content-Length", str(len(body)))
        conn.endheaders()
        conn.send(body[:1])
        deadline = time.monotonic() + 127
        while time.monotonic() < deadline:
            time.sleep(min(10, max(0, deadline - time.monotonic())))
        conn.send(body[1:])
        response = conn.getresponse()
        data = json.loads(response.read())
        assert response.status == 200 and data["sha256"] == hashlib.sha256(body).hexdigest(), "slow upload was interrupted"
        return {"status": response.status, "upload_pause_seconds": 127, "hash_matches": True}
    finally:
        conn.close()


def delayed_response():
    return expect_status((200,), path="/v1/responses?mode=delayed", method="POST", body=b"{}", timeout=180)


def sse_response():
    conn = http.client.HTTPConnection("127.0.0.1", 18390, timeout=180)
    try:
        start = time.monotonic()
        conn.request("POST", "/v1/responses?mode=sse", body=b"{}", headers={"Authorization": "Bearer " + ALLOWED})
        response = conn.getresponse()
        assert response.status == 200 and response.getheader("Content-Type", "").startswith("text/event-stream")
        first = response.readline()
        first_seconds = time.monotonic() - start
        assert first.startswith(b": heartbeat") and first_seconds < 5, "SSE first heartbeat buffered"
        payload = first + response.read()
        assert b"response.completed" in payload and b"[DONE]" in payload, "SSE stream truncated"
        assert payload.count(b": heartbeat") == 14, "SSE heartbeats lost"
        return {"status": 200, "first_heartbeat_seconds": round(first_seconds, 3), "heartbeats": 14,
                "completed": True}
    finally:
        conn.close()


def dependency_failure():
    compose("stop", "postgres")
    try:
        status, data, _ = request(timeout=15)
        assert status in (500, 502, 503), "DB outage did not fail closed: %s" % status
        assert not (data and data.get("mock")), "DB outage reached model mock"
        return {"status": status, "failed_closed": True}
    finally:
        compose("start", "postgres")
        ready()


def credential_safe_logs():
    access = compose("exec", "-T", "nginx", "cat", "/var/log/nginx/nocf-access.log").stdout
    logs = compose("logs", "--no-color", "auth", "nginx", "mock").stdout
    for value in (ALLOWED, ORDINARY):
        assert value not in access and value not in logs, "credential appeared in ingress/sidecar/mock logs"
    assert "?key=" not in access and "?api_key=" not in access, "raw query logged by ingress"
    return {"access_log_omits_query": True, "fixture_credentials_absent": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--no-build", action="store_true", help="reuse the current isolated sidecar image")
    parser.add_argument("--skip-long", action="store_true", help="development only; evidence is incomplete")
    args = parser.parse_args()
    GENERATED.mkdir(parents=True, exist_ok=True)
    render_nginx()
    # Named fixture volume is retained. Re-seed its synthetic rows before tests.
    result = compose("up", "-d", *( [] if args.no_build else ["--build"] ))
    print(result.stdout, flush=True)
    ready()
    sql("UPDATE users SET status='active',deleted_at=NULL; UPDATE api_keys SET status='active',expires_at=NULL,deleted_at=NULL; "
        "UPDATE user_attribute_definitions SET enabled=TRUE,deleted_at=NULL,type='multi_select'; "
        "INSERT INTO user_attribute_values(id,user_id,attribute_id,value) VALUES(1,1,1,'[\"enabled\"]') "
        "ON CONFLICT(id) DO UPDATE SET value=EXCLUDED.value;")
    case("allow_permitted_key", lambda: expect_status((200,)))
    case("deny_missing_key", lambda: expect_status((401, 403), key=None))
    case("deny_unknown_key", lambda: expect_status((401, 403), key="sk-not-a-fixture-key"))
    case("deny_ordinary_user", lambda: expect_status((401, 403), key=ORDINARY))
    case("x_api_key", lambda: expect_status((200,), key=None, headers={"X-Api-Key": ALLOWED}))
    case("x_goog_api_key", lambda: expect_status((200,), key=None, headers={"X-Goog-Api-Key": ALLOWED}))
    case("google_query_key", lambda: expect_status((200,), key=None, path="/v1beta/models?key=" + ALLOWED))
    case("deny_openai_query_key", lambda: expect_status((400, 401, 403), key=None, path="/v1/models?key=" + ALLOWED))
    case("deny_query_api_key", lambda: expect_status((400, 401, 403), path="/v1beta/models?api_key=" + ALLOWED))
    case("deny_conflicting_credentials", lambda: expect_status((401, 403), headers={"X-Api-Key": ORDINARY}))
    case("deny_conflicting_google_query_key", lambda: expect_status((401, 403), path="/v1beta/models?key=" + ORDINARY))
    case("deny_duplicate_authorization_headers", duplicate_credentials)
    case("deny_ambiguous_scheme", lambda: expect_status((401, 403), key=None,
         headers={"Authorization": "Basic acceptance", "X-Api-Key": ALLOWED}))
    case("deny_repeated_query_key", lambda: expect_status((400, 401, 403), key=None,
         path="/v1beta/models?key=" + ALLOWED + "&key=" + ORDINARY))
    mutations = [
        ("permission_revoked", "UPDATE user_attribute_values SET value='[]' WHERE id=1", "UPDATE user_attribute_values SET value='[\"enabled\"]' WHERE id=1"),
        ("permission_malformed", "UPDATE user_attribute_values SET value='enabled' WHERE id=1", "UPDATE user_attribute_values SET value='[\"enabled\"]' WHERE id=1"),
        ("permission_unknown_option", "UPDATE user_attribute_values SET value='[\"enabled\",\"unknown\"]' WHERE id=1", "UPDATE user_attribute_values SET value='[\"enabled\"]' WHERE id=1"),
        ("permission_duplicate_option", "UPDATE user_attribute_values SET value='[\"enabled\",\"enabled\"]' WHERE id=1", "UPDATE user_attribute_values SET value='[\"enabled\"]' WHERE id=1"),
        ("permission_null", "UPDATE user_attribute_values SET value=NULL WHERE id=1", "UPDATE user_attribute_values SET value='[\"enabled\"]' WHERE id=1"),
        ("permission_removed", "DELETE FROM user_attribute_values WHERE id=1", "INSERT INTO user_attribute_values(id,user_id,attribute_id,value) VALUES(1,1,1,'[\"enabled\"]')"),
        ("attribute_disabled", "UPDATE user_attribute_definitions SET enabled=FALSE WHERE id=1", "UPDATE user_attribute_definitions SET enabled=TRUE WHERE id=1"),
        ("attribute_deleted", "UPDATE user_attribute_definitions SET deleted_at=NOW() WHERE id=1", "UPDATE user_attribute_definitions SET deleted_at=NULL WHERE id=1"),
        ("attribute_wrong_type", "UPDATE user_attribute_definitions SET type='text' WHERE id=1", "UPDATE user_attribute_definitions SET type='multi_select' WHERE id=1"),
        ("key_disabled", "UPDATE api_keys SET status='disabled' WHERE id=1", "UPDATE api_keys SET status='active' WHERE id=1"),
        ("key_expired", "UPDATE api_keys SET expires_at=NOW()-INTERVAL '1 second' WHERE id=1", "UPDATE api_keys SET expires_at=NULL WHERE id=1"),
        ("key_deleted", "UPDATE api_keys SET deleted_at=NOW() WHERE id=1", "UPDATE api_keys SET deleted_at=NULL WHERE id=1"),
        ("user_disabled", "UPDATE users SET status='disabled' WHERE id=1", "UPDATE users SET status='active' WHERE id=1"),
        ("user_deleted", "UPDATE users SET deleted_at=NOW() WHERE id=1", "UPDATE users SET deleted_at=NULL WHERE id=1"),
    ]
    for name, statement, restore in mutations:
        case(name, lambda statement=statement, restore=restore: mutate_and_check(statement, restore))
    for path in ("/", "/login", "/admin/users", "/api/v1/admin/users", "/api/v1/auth/login",
                 "/_direct_auth", "/_nocf_auth", "/auth", "/healthz", "/v1/../admin/users"):
        case("protect_path_" + path, lambda path=path: expect_status((401, 403, 404), path=path))
    case("no_options_auth_bypass", lambda: expect_status((401, 403), method="OPTIONS", key=None))
    def public_health():
        status, data, _ = request(path="/health", key=None)
        assert status == 200 and data == {"status": "ok", "entry": "direct"}, "public health reached application/mock"
        return {"status": status, "application_not_accessed": True}
    case("public_ingress_health_is_local_only", public_health)
    case("overwrite_untrusted_ip_headers", ip_headers)
    case("readonly_database_role", readonly_role)
    case("large_upload_80_MiB", large_upload)
    if not args.skip_long:
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
            futures = [executor.submit(case, name, fn) for name, fn in (
                ("slow_upload_127s", slow_upload), ("silent_response_130s", delayed_response),
                ("sse_130s_heartbeat", sse_response))]
            for future in futures:
                future.result()
    case("database_outage_fails_closed_and_recovers", dependency_failure)
    case("credential_safe_ingress_logs", credential_safe_logs)
    evidence = {"project": PROJECT, "production_accessed": False, "model_fees": 0,
                "complete": not args.skip_long, "passed": all(item["passed"] for item in RESULTS),
                "checks": RESULTS, "compose_status": compose("ps", "--format", "json").stdout,
                "stop_command": " ".join(COMPOSE + ["down"])}
    (GENERATED / "evidence.json").write_text(json.dumps(evidence, indent=2, ensure_ascii=False), encoding="utf-8")
    print("Evidence: " + str(GENERATED / "evidence.json"))
    print("Stop: " + evidence["stop_command"])
    return 0 if evidence["passed"] and evidence["complete"] else 1


if __name__ == "__main__":
    sys.exit(main())
