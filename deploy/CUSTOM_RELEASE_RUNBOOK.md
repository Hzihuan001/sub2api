# Sub2API custom release runbook

## Release coordinates and authorization

- Read [`CUSTOM_PROJECT_STATUS.md`](../docs/CUSTOM_PROJECT_STATUS.md) before each release and verify actual integration ancestry/contents. Release and integration branches include `.12` runtime and the separate documentation closure; resolve the exact current ref with Git, without moving the runtime tag.
- Current 2026-10-20 release: upstream `v0.2.16`, branch `codex/group-failover-rules-v0.2.16`, runtime `f42b02f9ea0f43c3e34d9bbf778cd9e450cb05b4`, version `0.2.16-custom.12`, tag `custom-0.2.16.12`, `linux/amd64`, built `2026-10-19T22:11:37Z`.
- Published image: `ghcr.io/hzihuan001/sub2api:0.2.16-custom.12`; immutable digest / Engine image ID `sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610`. Local and server pull-back version/commit/digest match the accepted build.
- The user has explicitly approved local build → GHCR → pull-back acceptance → backups → sequential main/L1/COS application-only deployment. This authorization covers the stated sequence; the former v0.2.13 Tencent-staging/separate-OVH-approval procedure is historical.

The `.12` release completed all three sites on 2026-10-20: main 06:23:33–06:23:41, L1 06:24:31–06:24:40 and COS 06:25:13–06:25:21 (Asia/Shanghai). All were healthy/zero restarts with matching version/commit/digest. Configuration fingerprints and PostgreSQL/Redis identities/start times remained unchanged; old migration rows were intact and only 9001/9002 were added. The 06:26:50 bounded follow-up also matched public JS bytes to the origin and found zero critical logs. See the [current release report](../docs/CUSTOM_RELEASE_0_2_16_CUSTOM12_20261020.md) for exact evidence and rollback constraints. Later `.6`/`.7` examples are historical, not current migration or rollback instructions.

The `.12` formal digest passed a fresh 13/13 main-site synthetic gateway/accounting matrix: ten successes each had one usage/dedup and exact actual-group balance/Key-quota charges. All three local apps were healthy, data services preserved; a limited `.11` startup rollback with 9001/9002 retained and restoration to `.12` passed. L1/COS full authenticated local matrices remain unexecuted because personal compliance consent was not supplied; production had no existing admin API Key. No paid production model, authenticated role matrix or long-term monitoring is claimed. The actual Actions dispatch returned HTTP 422 despite enabled permissions API, with no runtime CI run. Existing local-GHCR authorization applies, not CI success. The isolated project remains available on 18180/18181/18182; stop instructions are in the acceptance report.

Never create a `v*` tag for this custom build. Never put passwords, tokens, SSH
keys, server addresses, or the contents of `CODEX_HANDOFF.md` in Git.

## Local quality and isolated container gate

Use the toolchain recorded with the accepted source scope: `.7` local functional checks used portable Go 1.27.2, Node 22.13.0 and pnpm 10.33.0; the earlier `.6` gate used Node 20.20.2/pnpm 9.15.9 and golangci-lint 2.14.0. Build and test scopes are recorded separately in the release report. The commands below describe the quality gate, not a claim that every command was rerun. Reuse results only when source, dependencies, toolchain and fixture scope match.

Historical modularization gates retained 15 full-lint findings, an incomplete P4 full old/new task/accounting matrix and mixed SSE load results; user stopped further load tests. Do not relabel those as passing or rerun SSE diagnostics merely for a green summary. The [old gate report](../docs/CUSTOM_MODULARIZATION_FINAL_GATES_20261015.md) records that scope; `.12` additionally ran the complete service suite (17,311 including subtests), 137 critical backend cases, 147 affected frontend cases and formal-image matrix recorded in its current release report. Real Actions/upgrade-PR execution remains unclosed.

Quality commands:

```powershell
cd backend
go test -tags=unit ./...
go test -tags=integration ./...
golangci-lint run --timeout=30m ./...

cd ../frontend
pnpm install --frozen-lockfile
pnpm run lint:check
pnpm run typecheck
pnpm run test:run
pnpm run build

cd ..
git diff --check
docker compose -f deploy/docker-compose.operator-test.yml config --quiet
pwsh -File deploy/tests/operator-container-test.ps1
```

The container test uses a project-scoped PostgreSQL database, Redis instance,
network, and three named volumes. It binds only `127.0.0.1:18080`, exercises
admin/operator/user login, the operator allow/deny matrix, privileged target
protection, atomic batch rejection, Admin API Key behavior, WebSocket access,
and restart persistence, then removes only its validated test project unless
`-Keep` is supplied.

To test an immutable GHCR image without changing production data:

```powershell
pwsh -File deploy/tests/operator-container-test.ps1 `
  -Project sub2api-operator-test-ghcr `
  -Image ghcr.io/<fork-owner>/sub2api@sha256:<digest> `
  -UsePublishedImage
```

`-UsePublishedImage` refuses mutable tags, pulls the requested digest, and
starts Compose with `--no-build`, so the GHCR acceptance cannot silently test a
locally rebuilt image.

For `.6`, this exact published-image path passed using project
`sub2api-operator-test-release6-20261015`, port `18097`, and `-UsePublishedImage`.
It covered the script's role, API Key, WebSocket, restart and log checks, exited
0, and automatically removed its own test-project resources. It is not evidence
of a fresh full performance run or complete production browser validation.

## Fork and GHCR gate

1. Set the personal Fork as `origin`; keep the official repository as `upstream`.
2. Push the release branch and custom tag; never create a `v*` tag for a custom build.
3. Use `Custom CI and GHCR release` when Actions can execute. The current `.12` actual dispatch returned HTTP 422 `Actions has been disabled for this repository`; no CI run was created, even though permissions API was enabled. Do not infer execution from configuration, repeatedly dispatch, or alter repository/account settings as part of the fallback.
4. For this explicitly approved fallback, build the committed release locally with exact version/commit metadata, verify the artifact, then push that artifact to GHCR. Record the mutable tag, immutable `sha256` digest and local image ID. A local release is not CI success.
5. Pull the immutable digest back, verify it resolves to the same accepted image ID and embedded version/commit, then run the published-image container gate above. A tag alone is not the acceptance identity.
6. Confirm each target's architecture before building. If a target requires another architecture, define and validate the necessary artifact before deployment; do not rebuild separately on a production site or silently change the approved digest.

## Read-only server preflight

Run this for main, L1 and COS before making any application change. Save the
output in the release report, redacting secret values. Recheck active requests,
long connections and unfinished batch jobs immediately before each site's update;
wait for existing work to finish rather than forcing it to complete.

```sh
uname -m
docker version
docker compose version
docker compose ps
docker compose images
docker inspect --format '{{.Config.Image}} {{.Image}}' <sub2api-container>
docker image inspect --format '{{json .RepoDigests}}' <current-image>
docker compose config --services
docker compose config --volumes
docker compose config --networks
docker compose exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c 'select version();'
docker compose exec -T redis redis-cli ping
```

Also record the Compose/1Panel ownership model, app version, current digest,
environment variable names (not values), bind mounts, named volumes, networks,
OpenResty upstream, and migration state. Stop immediately if the installed
application is newer than the approved release; this release must never
downgrade it.

## Backup gate

Before the three-site rollout, create timestamped backups for every site outside
the application volume. Verify non-empty files, readable archives and checksums;
record whether any later production changes require a fresh backup:

- PostgreSQL custom-format dump plus a restore/list verification;
- `.env` and all Compose/1Panel definitions;
- `/app/data` and every persistent directory or bind mount;
- current image reference and immutable digest;
- OpenResty configuration and relevant proxy/SSE settings.

Do not copy secrets into the report or repository. Do not delete/recreate
PostgreSQL, Redis, application volumes, networks, or proxy configuration.

For `.6`, `/opt/sub2api-backups/v0.2.16-custom.6-20261015T093931Z` contains the
three-site backups. PostgreSQL `pg_restore --list`, tar reads and SHA checks
passed. Backup verification is not a claim of a live database restore drill.

The current `.12` backup is `/opt/sub2api-backups/v0.2.16-custom.12-20261019T220600Z`;
all three dumps/archives/checksums and pg_restore lists were verified. This is not
a live database restore drill. Verify only two new migrations,
`9001_custom_api_key_route_policies.sql` and
`9002_custom_api_key_model_policies.sql`, with every old row unchanged. Actual
counts were main 324→326 and L1/COS 333→335. Do not use the historical `.7`
one-migration or `.6` unchanged-total assertion. New custom migration names must
match `^9[0-9]{3}_custom_[a-z0-9][a-z0-9_-]*\.sql$`; published files remain immutable.

## Main → L1 → COS application-only deployment

1. Complete read-only preflight, published-image acceptance and all three backup gates. Before each site, recheck the installed version and drain regular/live account/user/Key/WS slots, user/account waits, unfinished batch jobs and Redis `image_task:*` processing tasks. Read only a bounded status prefix, not base64 results; unknown states fail closed. `.12` waited for zero and rechecked immediately before the app change; it did not force active requests to end.
2. Change only the Sub2API service image to the approved GHCR digest.
3. Pull the digest and run `docker compose up -d --no-deps --pull never <application-service>`: `sub2api` for main, `api` for L1/COS (or the
   equivalent 1Panel operation that recreates only the application container).
4. Verify the container is healthy, its restart count, embedded version/commit, image ID/digest and Compose reference. Check internal/public health, relevant public entrypoints and startup logs before moving to the next site.
5. Record exactly which checks ran. The local role/API Key/WebSocket matrix does not become a production role/browser/SSE matrix by inference. A reachable login or image-studio URL is not a logged-in workflow or paid generation test. Production fault injection, paid model calls and extra SSE load are outside this approved sequence.
6. Record timestamps, commands and results separately for main, L1 and COS. Stop the sequence on failure and roll back only the failed site's application as appropriate; do not start the next site while a check is failing.

Use the same accepted digest at all three sites; never rebuild there. Preserve
JWT/TOTP keys, database, Redis, volumes, ports, networks, OpenResty and Cloudflare
settings. Record the actual post-deploy observation duration and any planned
30-minute or 24-hour follow-up separately; neither window is complete merely
because startup health checks pass.

The `.6` rollout completed main at 09:49:57–09:50:04 UTC, L1 at
09:50:36–09:50:44 and COS at 09:51:06–09:51:17 on 2026-10-15 (China time UTC+8).
All three sites were healthy with zero restarts and the expected version/commit/
image. Environment, ports, mounts and networks were unchanged; PostgreSQL and
Redis container IDs/start times and the migration count/full-row fingerprint
were unchanged. Internal/public health and image-studio endpoints returned 200;
anonymous settings and failure-policy requests returned 401; critical startup
log matches were zero. No production admin API Key was available, so no new
production authenticated role login was performed or fabricated. The full role
matrix is local published-image evidence. No paid model test or long-term
monitoring was performed.

A 09:54:31 UTC follow-up still found all three sites healthy with zero restarts,
the expected digest and zero critical log matches. Evidence is
`D:/projects/go-tmp/custom6-final-check-20261015.json`; this is a bounded follow-up
snapshot, not a completed 30-minute or 24-hour observation window.

## Rollback

Set the application image back to the recorded previous digest and recreate
only Sub2API. Record any newly applied migration in the release report. An
older binary must never be started after an unverified irreversible migration.
The current `.12` rollback target is `.11`, digest
`sha256:022704b184874c61ecd50587252867ad109f943afa75eed2b670fd3409aa916d`.
Keep 9001/9002 and all business data. The old binary ignores new Key model
restrictions, so check the enabled count in `custom_api_key_model_policies`
immediately before rollback. If any are enabled, veto automatic rollback and
first disable affected Keys or use an audited fix image; do not silently loosen
permissions. Local rollback verified startup/schema-read compatibility only;
production rollback was not triggered. Do not substitute an
official or much older image and assume it understands the same roles, settings
and batch states; that requires a separate compatibility review.

The historical `.6` scope added no migration, `.7` added 9000, and current `.12`
adds 9001/9002. For a compatible application rollback, do not
restore an old database backup over new business data. If a future release
crosses an unverified irreversible migration, stop and prepare a separately
reviewed recovery plan; never run the old image against that database or restore
the backup automatically.

## Release report fields

- Environment and UTC/China timestamps
- Baseline commit and custom commit
- Image tag and immutable digest
- Previous app version/tag/digest
- CPU architecture and Compose/1Panel owner
- Backup locations and verification results
- Quality, HTTP matrix, browser, WebSocket, SSE, proxy, and persistence results
- Log review and observation-window result
- Problems, mitigation, rollback result (if any), and final approver
