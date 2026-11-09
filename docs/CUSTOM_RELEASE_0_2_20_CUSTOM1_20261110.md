# 0.2.20-custom.1 Production Release Record (2026-11-10)

## Source and artifact

- Upstream base: official `v0.2.20`, commit `dab829a4e9b7d1cc307189aea8b0319eb29e29d0`.
- Branch: `upgrade/upstream-v0.2.20-20261109`.
- Runtime source commit: `eb069def5b0dbcdd8f85f4294fa20434a6537015`.
- Custom runtime tag: `custom-0.2.20.1`.
- Version: `0.2.20-custom.1`.
- Immutable image: `ghcr.io/hzihuan001/sub2api@sha256:f3a21007c4a9a2e5941435dffadaf5467b2dbd2a4db400baad0e165150578693` (`linux/amd64`).
- The `.17` Pelican changes were merged into this release; `.17` was not separately deployed. Production was still `.16` before this rollout.

The image was built from the tagged source, pushed, pulled back by digest and checked against its embedded version, commit and OCI labels. The exact published digest passed the operator container acceptance. No successful GitHub Actions run is claimed.

## Production rollout

Deployment completed in main → L1 → COS order on 2026-11-10 China time. Each site was rechecked immediately before its application update; account/user/API-key/live/WebSocket slots, wait queues, Redis async-image processing tasks and nonterminal batch-image jobs were all zero. No request or job was forcibly cancelled.

All sites run the same immutable digest and embedded commit. Application containers are healthy with restart count 0. PostgreSQL and Redis container identities remained unchanged. Only each app image reference was updated, using `docker compose up -d --no-deps --pull never`; volumes, networks, environment settings, data and reverse proxy configuration were retained. Startup scans found zero fatal/migration-failure matches.

| Site | Public host | Migration rows before → after | Health |
| --- | --- | ---: | --- |
| Main | `ai.moshu.cloud` | 334 → 335 | healthy, restart 0 |
| L1 | `api.tokenpulse.top` | 343 → 344 | healthy, restart 0 |
| COS | `cos.tokenpulse.top` | 343 → 344 | healthy, restart 0 |

For each site the only migration delta was `249_mcp_oauth.sql`. Every prior migration row and checksum remained unchanged, and all three new MCP OAuth tables exist. The migration checksum was verified using the runner's `strings.TrimSpace` behavior. No database rollback was performed.

## Backups

Fresh backups are stored on the production server at `/opt/sub2api-deploy-backups/custom20-20261109T191822Z/`. The private root directory is mode `0700`; backup files are mode `0600`. Its 20-entry SHA-256 manifest was verified with digest `24fc04054a8f65bfb038e45c5e60294b57f4cfcc220462f006b2ca318225e32f`.

Each PostgreSQL custom-format dump passed `pg_restore --list`; each application-data archive and the OpenResty configuration archive passed read/list checks. The backup includes per-site environment/Compose files, main `data.yml`, database dumps, app data, schema-migration ledger snapshots and the relevant OpenResty configuration. This was not a live restore drill.

## Public and browser checks

- On all three hosts, public `GET /health` returned HTTP 200 with JSON and `GET /pelican-showcase` returned HTTP 200 with HTML.
- The logged-in main-site browser rendered the Pelican result cards and embedded HTML/SVG artwork. Filtering to a group hid the other groups; restoring “全部分组” restored them. The Refresh action reloaded results successfully.
- The main-site test-management view listed three enabled plans. An existing plan displayed `超高 (xhigh)`; its editor exposed provider default, low, medium, high and `xhigh`. The editor was closed without saving.
- The browser had no L1/COS login session. Both routes correctly redirected to their login forms, which rendered. Authenticated workflows on those two hosts were not exercised.
- No new model request, paid generation, production fault injection or load test was performed. Visible showcase records predated this deployment.

## Migration and rollback boundary

Migration `249_mcp_oauth.sql` adds MCP OAuth storage and was the only schema change. The application rollback target is the previous production `.16` image `ghcr.io/hzihuan001/sub2api@sha256:a3f3a1e9cc5d51cfe262407faf3e32197a27b4b3f227eaaf39a4389d294aff30`. If an app-only rollback is needed, keep the new migration and all current data; never restore a pre-migration database over the live database.

The planned 30-minute and 24-hour observation windows begin after this release and have not yet elapsed.
