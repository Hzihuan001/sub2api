# Local Docker Acceptance Report — v0.2.13 isolation refactor

Date: 2026-10-03 (Asia/Shanghai)

## Build

- Source commit: `aad71b52b26a4d43c9ab6ee7aeaf32b3bcd32363`
- Image: `ghcr.io/hzihuan001/sub2api:0.2.13-custom.5`
- Image digest: `sha256:f2ffb09e4ce386a0141eb22043e82365ace1cbd950a81461396d5e03fcba98e7`
- Platform: `linux/amd64`
- Compose project: `moshu-isolation-refactor`
- Compose file: `deploy/acceptance/docker-compose.isolation.yml`

## Runtime checks

| Site | URL | Result |
| --- | --- | --- |
| Main | `http://127.0.0.1:18180` | `/health` 200; login 200 |
| L1 | `http://127.0.0.1:18181` | `/health` 200 |
| COS | `http://127.0.0.1:18182` | `/health` 200 |

- All three PostgreSQL containers healthy and isolated.
- All three Redis containers healthy and isolated.
- `/`, `/admin/dashboard`, and `/image-studio` returned 200 on all sites.
- Main application restart preserved database state; health and admin login remained successful.
- Main administrator accepted the isolated compliance acknowledgement; dashboard statistics,
  usage records, and operator permission policy endpoints returned 200.
- All three instances reported version `0.2.13-custom.5`; the image-studio route and the
  management system/version, dashboard, usage, and operator-permission endpoints returned 200.
- A temporary `acceptance-mock` provider was attached only to the isolated network. The main
  site passed synchronous `/v1/chat/completions` (200), streaming chat (`text/event-stream` and
  `[DONE]`), and streaming `/v1/responses` (complete response events). Usage, total cost,
  actual cost, and balance deduction were observed. The temporary group, account, API key,
  balance, and mock container were removed after the test; the database is back to one default
  group with no active accounts or keys.
- Image generation/batch downloads, settlement/reconciliation replay, failover/timeout recovery,
  and cross-site reseller protocol were not covered by this fixture and remain release evidence
  gaps. L1/COS credentialed management login was not attempted because their initialized
  credentials are not present in container environment variables.
- No `panic`, fatal startup error, migration failure, database error, or Redis error appeared in the last five minutes of application logs.

## Quality gates

- GitHub Actions CI for PR #1: backend tests, `golangci-lint`, frontend build/typecheck, security checks and shell checks passed.
- `scripts/check-custom-isolation.py --skip-generated --base-ref custom/integration --upstream-ref v0.2.13`: passed.
- Compose config validation: passed.
- `custom-release.yml` quality and publish run `37141659216` passed; the GHCR digest above was
  pulled back into this Compose project and all three application containers reported healthy.
- Local Windows release-helper tests: `10` tests passed, `3` platform-specific tests skipped
  (POSIX mode bits/runnable bash). The Linux CI release-helper job remains the authoritative shell
  coverage.

No production data was used during this acceptance run. Production deployment of the same
immutable digest was separately completed and recorded in the release report.
