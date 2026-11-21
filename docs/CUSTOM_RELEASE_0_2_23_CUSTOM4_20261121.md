# 0.2.23-custom.4 image-task retention production release

On 2026-11-21, production main and L1 were updated sequentially to `0.2.23-custom.4`. The application containers are healthy with zero restarts. COS remains paused and was not included.

- Runtime commit `0c480893031841801e6cfc943676095a95707125`, tag `custom-0.2.23.4`.
- Immutable image `ghcr.io/hzihuan001/sub2api@sha256:e9440f9e868ca25d939e9b868d91b7d023171a9f990486efd59a273a078eb54a`.
- Main and L1 migration ledgers remain at 342 and 351 rows respectively, matching the pre-deployment counts. PostgreSQL and Redis containers were not recreated.
- Deployment backup `/opt/sub2api-backups/v0.2.23-custom.4-20261121T134059Z` is approximately 1.2 GB. PostgreSQL dump, restore list, data/configuration archives and SHA-256 checks passed.

## Retention behavior

Newly completed asynchronous image tasks and their server-hosted images expire one hour after completion. The workbench download becomes unavailable at the task expiry time. The local cleanup worker scans at startup and hourly afterward, so physical file deletion can occur up to just under one hour after expiry.

Tasks completed before this upgrade retain their stored `ExpiresAt` values. This release did not rewrite their Redis records or local recovery checkpoints, so some pre-upgrade tasks may remain available or occupy disk longer than one hour until their existing expiry is reached.

## Verification

| Check | Result |
| --- | --- |
| Application containers | Main and L1 healthy, restart count 0. |
| Database and cache | PostgreSQL migration ledgers unchanged at main 342 and L1 351. PostgreSQL and Redis containers were not recreated. |
| Backups | PostgreSQL dump/restore listing, data and configuration archives, and SHA-256 checks passed. |
| Ingress restoration | Temporary drain rule removed. Original OpenResty configuration hashes were restored and `nginx -t` passed. |
| Public endpoints | Main and L1 health/workbench pages returned HTTP 200. Anonymous model listing returned HTTP 401. |
| Source and artifact gates | Backend full unit/integration tests, frontend 445 test files/3,787 tests, lint, typecheck, build, and exact-digest isolated operator acceptance passed. |
| Lint baseline | Full golangci-lint reports the same 38 pre-existing `errcheck/staticcheck` findings as production `.2`; incremental diagnostics are 0. |

GitHub Actions are disabled for this repository. The release used the authorized local build, registry push and immutable-digest acceptance workflow. No paid production image generation was performed for this release.

## Rollback

Application-only rollback target is the previous `.2` image `ghcr.io/hzihuan001/sub2api@sha256:594fe3566ce73ed6ea2ebaecaca7f175b6a1616b44304f10d215ba03bed4e315`. No database migration was added. Keep the live PostgreSQL, Redis, persistent image data and configuration during rollback.
