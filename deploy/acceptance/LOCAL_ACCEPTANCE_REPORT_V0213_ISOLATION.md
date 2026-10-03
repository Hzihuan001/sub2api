# Local Docker Acceptance Report — v0.2.13 isolation refactor

Date: 2026-10-03 (Asia/Shanghai)

## Build

- Source commit: `be4d7f511` (`test: add isolated three-site docker acceptance`)
- Image: `sub2api:isolation-refactor-local`
- Image manifest: `sha256:6a1d4528bb6c3b68abb538bf34ea189380557fa1e0820f5700c915a2009ebaa7`
- Platform: `linux/amd64`
- Compose project: `moshu-isolation-refactor`
- Compose file: `deploy/acceptance/docker-compose.isolation.yml`

## Runtime checks

| Site | URL | Result |
| --- | --- | --- |
| Main | `http://127.0.0.1:18180` | `/health` 200; login 200 |
| L1 | `http://127.0.0.1:18181` | `/health` 200; login 200 |
| COS | `http://127.0.0.1:18182` | `/health` 200; login 200 |

- All three PostgreSQL containers healthy and isolated.
- All three Redis containers healthy and isolated.
- `/`, `/admin/dashboard`, and `/image-studio` returned 200 on all sites.
- Main application restart preserved database state; health and admin login remained successful.
- Main administrator accepted the isolated compliance acknowledgement; dashboard statistics,
  usage records, and operator permission policy endpoints returned 200.
- No `panic`, fatal startup error, migration failure, database error, or Redis error appeared in the last five minutes of application logs.

## Quality gates

- GitHub Actions CI for PR #1: backend tests, `golangci-lint`, frontend build/typecheck, security checks and shell checks passed.
- `scripts/check-custom-isolation.py --skip-generated --base-ref custom/integration --upstream-ref v0.2.13`: passed.
- Compose config validation: passed.
- Local Windows `go test ./...`: repository backup tests require `sh` and failed because the Windows host has no `sh` executable. The same full backend test job passed on Linux CI; no source failure was observed.

No production deployment was performed during this acceptance run.
