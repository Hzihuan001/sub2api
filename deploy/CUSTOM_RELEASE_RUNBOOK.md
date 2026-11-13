# Sub2API custom release runbook

## Current production release: custom.21.4 — rollout complete (2026-11-13)

Runtime source is locked to `548f4360c908a3f2b5ba18cbd47394ec6b68eaa5`, tag `custom-0.2.21.4`, version `0.2.21-custom.4`. The immutable artifact is `ghcr.io/hzihuan001/sub2api@sha256:2f0aa58a7091303d652aa7529cdccdf692a13309669bba2f4e4908a181c4d8b8`; main → L1 → COS completed with all applications healthy/restart 0. Release evidence is in [`CUSTOM_RELEASE_0_2_21_CUSTOM4_20261113.md`](../docs/CUSTOM_RELEASE_0_2_21_CUSTOM4_20261113.md).

This release automatically recovers completed image tasks when the result download is interrupted: it keeps the same task id, retries GET/status with bounded exponential backoff from 3 seconds to a 60-second maximum, and never replays the original POST or charges again. Browser task metadata survives refresh/unmount, and stopping the wait does not delete the task. The user-facing message now describes automatic recovery.

Verified backup: `/opt/sub2api-backups/v0.2.21-custom.4-20261113T104618Z/`; all three PostgreSQL dumps, restore lists, app/config archives and SHA-256 manifest passed. The exact published-image operator gate passed on port `18199`; resources were cleaned. Four public domains each returned 200 for health, keys, image-studio, docs and showcase pages. No paid production model call, real generation, fault injection or browser-authenticated interaction is claimed; previously accepted image-workbench/chat tests were not repeated.

No database migration was added: the production migration ledger remains main 339 and L1/COS 348. For application-only rollback, use the previous `.3` digest `sha256:a3aab14ed40f643babfb020c5f71caf431ec00aa8d5c9ca82b005a1a4a81c358`; retain current business data and migration rows, and never restore an older database over live data.

Runtime tag `custom-0.2.21.4` remains fixed at the source commit above. Documentation closure is a separate commit and must not move the runtime tag.

## Previous production release: custom.21.3 — rollout complete (2026-11-13)

Runtime source is locked to `6e72928b09967df5d92bb547758bce0e99ff5227`, tag `custom-0.2.21.3`, version `0.2.21-custom.3`. The immutable artifact is `ghcr.io/hzihuan001/sub2api@sha256:a3aab14ed40f643babfb020c5f71caf431ec00aa8d5c9ca82b005a1a4a81c358`; main → L1 → COS completed with all applications healthy/restart 0. Release evidence is in [`CUSTOM_RELEASE_0_2_21_CUSTOM3_20261113.md`](../docs/CUSTOM_RELEASE_0_2_21_CUSTOM3_20261113.md).

This release changes only the defaults for newly created key route failure policies. Consecutive failures are enabled by default with threshold 6 and 60-second cooldown. Status-code, TTFT, SLA, timeout and rate-limit rules are created only when explicitly added by the user; their default windows/samples and cooldowns are relaxed. Existing persisted policies are not rewritten. Multi-group cooldown polling and immediate return to an earlier available group remain enabled.

Verified backup: `/opt/sub2api-backups/v0.2.21-custom.3-20261112T165449Z/`; all three PostgreSQL dumps, restore lists, app/config archives and SHA-256 manifest passed. The exact published-image operator gate passed on port `18198`, and resources were cleaned. Four public domains each returned 200 for health, keys, image-studio, docs and showcase pages. No paid production model call, real generation, fault injection or browser-authenticated interaction is claimed; previously accepted image-workbench/chat tests were not repeated.

No database migration was added: the production migration ledger remains main 339 and L1/COS 348. For application-only rollback, use the previous `.2` digest `sha256:5d24cc7b12bb8da8ebc657a29a9e53041083b6d270e7e2b5075670f56430cd40`; retain current business data and migration rows, and never restore an older database over live data.

Runtime tag `custom-0.2.21.3` remains fixed at the source commit above. Documentation closure is a separate commit and must not move the runtime tag.

## Previous production release: custom.21.2 — rollout complete (2026-11-11)

Runtime source is locked to `dff2cb433fe219c60686a9dad3460c4e5a6cdba6`, version `0.2.21-custom.2`. The immutable artifact is `ghcr.io/hzihuan001/sub2api@sha256:5d24cc7b12bb8da8ebc657a29a9e53041083b6d270e7e2b5075670f56430cd40`; main → L1 → COS completed with all applications healthy/restart 0. Release evidence is in [`CUSTOM_RELEASE_0_2_21_CUSTOM2_20261111.md`](../docs/CUSTOM_RELEASE_0_2_21_CUSTOM2_20261111.md).

This release keeps the image studio implementation already accepted in `.1` and adds idempotent migration `9009_custom_gpt_image_26_group_models.sql`. It appends GPT Image 2.6 Flare/Sunburst to existing OpenAI image groups so the group-filtered `/v1/models` response and user workbench expose the models. No existing allowlist entries are removed.

Verified backup: `/opt/sub2api-deploy-backups/custom211-20261111T072120Z/`; PostgreSQL dumps, restore lists, app-data/Compose/environment archives, OpenResty config archive and SHA-256 manifest passed. The exact published-image operator gate passed on port `18092`. Four public domains each returned 200 for health, keys, image-studio, docs and showcase pages. Browser-authenticated generation and paid production model calls remain outside the gate.

For application-only rollback, use the previous `.20.2` digest `sha256:6d89fe835bee13987c48a60406f93b270046a4be4fa35d1813efda622f234b76`; retain migration `9009`, current business data and all existing migration rows. Do not restore an older database over live data.

> **Superseded intermediate**: `0.2.21-custom.1` contained the image-task recovery and 1800-second image transport changes and was briefly started on the main site during rollout. Production group data then showed that the existing image allowlist still needed the GPT Image 2.6 entries, so `.2` added the idempotent `9009` migration and superseded `.1` before L1/COS rollout. Keep the `.2` artifact above as the production baseline.

Runtime source is locked to `a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac`, tag `custom-0.2.20.2`, version `0.2.20-custom.2`. All source gates and exact-published-image acceptance passed; source branch and tag are pushed. Main → L1 → COS production rollout is complete on the exact digest below, all healthy/restart 0. Local/public HTTP, anonymous authorization, final public CSP/static-SHA/alias and migration-count checks passed. Browser visual/interaction and paid-model tests remain unverified. Record evidence in [the release report](../docs/CUSTOM_RELEASE_0_2_20_CUSTOM2_20261111.md).

**Published artifact:** `ghcr.io/hzihuan001/sub2api@sha256:6d89fe835bee13987c48a60406f93b270046a4be4fa35d1813efda622f234b76` (immutable index digest), `linux/amd64`, built `2026-11-10T19:06:27Z` from a `git archive` export of the runtime HEAD using the root `Dockerfile`. Published-image pull-back matched version/commit/OCI revision; `pg_dump` and `psql` are 18.6. Build/push logs are `.tmp/custom202-image-build.log` and `.tmp/custom202-image-push.log`. Exact-digest operator acceptance exited 0 on project `sub2api-operator-test-custom202-20261111`, port 18197; scoped resources were cleaned, log `.tmp/custom202-operator-test.log`. `gh run list --commit a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac` returned `[]`: no Actions run, so this is a verified local release, not a claim of Actions success.

Scope now includes bounded same-account image retries with the original concurrency slot; correct DeepSeek Pro pricing after the official changelog superseded the planned Flash downgrade; Terra fallback/cache/priority pricing; `image_count` precedence on dedicated image endpoints; privacy effects after account commit; matching lease cleanup for disabled scheduled tests; and restoration of the Grok model editor, diagnostics and shared sync entry lost during the upstream merge. Retain upstream 0.2.20/Kiro features. Transient image errors do not penalize account status or route to another account/group, while explicit auth/quota/tool failures retain existing handling. DNS NXDOMAIN is not retried; replaying image POSTs may duplicate upstream work/cost.

**Source gates passed:** backend `go test -tags=unit ./... -count=1` (67 tested packages; service 248.257s), `go test -p=1 -tags=integration ./... -count=1` (service 155.170s), and `golangci-lint run --timeout=15m --max-issues-per-linter=0 --max-same-issues=0 ./...` all exited 0. Logs are root `.tmp-full-unit-final.log`, `.tmp-full-integration-final.log`, and `.tmp-full-lint-final2.log`. Lint uses 2.15 compiled with Go 1.27.3 and default `backend/.golangci.yml`, with no build-tag override; CI is pinned to 2.15. Frontend Node 20.20.2 / pnpm 9.15.9 lint (two existing warnings), typecheck, all 441 files / 3677 tests, and build (including i18n 4/4) all exited 0; the restored Grok suite passed 62/62.

**Migration review passed:** `9008_custom_auth_invalidation_scope.sql` is an additive function-only migration, with no schema/data removal. Actual routing/security changes remain covered while revision-only/presentation-only events avoid redundant invalidation. The `.1` application is compatible with the retained 9008 migration, so the previous custom.20.1 digest remains the app-only rollback target. Preserve every old ledger row/checksum and the upgraded database; do not reverse 9008 or restore an older database during application rollback.

**Production evidence:** verified backup `/opt/sub2api-backups/v0.2.20-custom.2-20261110T184754Z`; all three `*-release-result.json` files report `passed: true`. Main updated 2026-11-10 UTC 19:32:27→19:32:42.506646, L1 19:34:49→19:35:21.609990, COS 19:35:59→19:36:09.544514 (2026-11-11 early morning in China). Only the application was recreated with `--no-deps --pull never`; configuration and PostgreSQL/Redis identities are unchanged. All old migration rows/checksums remain, only 9008 was added, and critical startup-log matches are 0. Final read-only evidence at `2026-11-10T19:39:58.627173Z` (2026-11-11 03:39:58 China time), `.tmp/custom202-final-evidence.json`, reconfirmed healthy/restart 0/exact digest and measured main/L1/COS ledger counts 335→336, 344→345, 344→345.

Main/L1 temporarily returned 503 for new inference ingress to assist natural drain, while existing streams finished without forced cancellation. Main fence: UTC 19:31:49.661551→19:32:44.062267 (54.4s); L1: 19:34:23.123466→19:35:23.536827 (60.4s), both on 2026-11-10. The original nginx file SHA values match, the temporary map is absent, and `nginx -t` passed after restoration. COS reached natural zero work without a fence. Retain this distinction when reporting final unchanged configuration.

Each site returned 200 locally/publicly for `/health`, `/keys`, `/image-studio`, `/docs/tools/codex`, `/pelican-showcase`; anonymous admin settings, group failure-policy and keys APIs returned 401. Final public verification exited 0 (`.tmp/custom202-public-postdeploy.log`): `ai.moshu.cloud`, `api.tokenpulse.top`, L1 alias `api.moshu.cloud` and `cos.tokenpulse.top` each returned 200 for 5 URLs (health plus 4 HTML pages), matched 7 JavaScript asset SHA values against origin, and matched the CSP nonce. CUA could not confirm the page URL and visual/interaction checks were stopped; no paid production model request, fault injection or application rollback is claimed.

Runtime tag `custom-0.2.20.2` stays fixed at `a90e15c1c1444c3f81b4ab9dc847b2a77ed60cac`; pre-closure `git fetch origin` showed branch left/right counts 0/0. Use a separate documentation closure commit and do not move that tag. For any later app-only rollback, naturally drain first and retain the current database, 9008 and new business data; never restore an older database over live data.

## Previous release: custom.20.2 — production rollout complete (2026-11-11)

Release report: [`custom.20` production record](../docs/CUSTOM_RELEASE_0_2_20_CUSTOM1_20261110.md). Runtime source/tag is `eb069def5b0dbcdd8f85f4294fa20434a6537015` / `custom-0.2.20.1`; all three applications previously ran `ghcr.io/hzihuan001/sub2api@sha256:f3a21007c4a9a2e5941435dffadaf5467b2dbd2a4db400baad0e165150578693`. The `.17` Pelican changes were merged into `.20`; `.17` was not separately deployed.

Main → L1 → COS finished with each app healthy and restart count 0. Only application containers were recreated; PostgreSQL, Redis, persistent data, networks and reverse proxy were retained. Fresh backup `/opt/sub2api-deploy-backups/custom20-20261109T191822Z/` passed SHA-256, PostgreSQL `pg_restore --list`, app-data and OpenResty archive verification. All pre-deploy request slots, waits, async image tasks and unfinished batch-image jobs were zero.

Migration `249_mcp_oauth.sql` is additive and was the only new ledger row: main 334→335, L1/COS 343→344. Preserve all old rows and current business data. The compatible app-only rollback target is production `.16`, digest `sha256:a3f3a1e9cc5d51cfe262407faf3e32197a27b4b3f227eaaf39a4389d294aff30`; never restore an older database over the migrated database. No production model calls or paid generation were used for acceptance, and no DNS/CDN/proxy changes were made.

The exact default prompt remains `创建一个HTML，内容是SVG绘制一个鹈鹕骑自行车的2D动画，你不需要任何测试。`; scheduled test plans support provider default, low, medium, high and `xhigh`. The 30-minute and 24-hour observation windows have not yet elapsed.

## Partial release: custom-0.2.19.12 (2026-11-03)

Source branch: `codex/v0.2.19-merge-20261029`; runtime commit `73348ea7793b7b58780e36a825eb5a3e6d480ff0`, tag `custom-0.2.19.12`, digest `sha256:6cb01b9bdf763526c0c3dfe2e86cc3e2a67cd949f785933abe90a14627f2179a`; official base remains v0.2.19. The verified artifact reached main only. L1 rolled back to `.11` after its public CDN TLS check failed; COS remained `.11`. This release changed isolated showcase preview behavior and bounded empty transient read-failure retry; no DB migration or site configuration change. The inherited parent CSP issue was subsequently reproduced and is handled by the published `.13` artifact. Evidence and limitations are in `docs/CUSTOM_RELEASE_0_2_19_CUSTOM12_20261103.md` and `docs/CUSTOM_RELEASE_0_2_19_CUSTOM13_20261103.md`. Preserve previous application digests and all databases, Redis data and volumes.

## Historical release: custom-0.2.18.4 (2026-10-27; superseded)

- Runtime source/tag: `1524aa0985f432df19c9144c726224d2852308d6` / `custom-0.2.18.4`; authoritative branch/workspace: `codex/image-batch-16refs-20261026` / `D:/projects/sub2api-image-batch-16refs-20261026`. Official base remains `v0.2.18`. Later documentation commits do not move the runtime tag.
- Published and pulled image: `ghcr.io/hzihuan001/sub2api@sha256:80adc97b50213ba840e68d6c13b932701cff294f04fb5dcd291ffe12b63f49e6`. Root-Dockerfile linux/amd64 build, embedded version/full commit, OCI revision and PostgreSQL18.6 client tools passed. Frontend99, explicit-unit backend shared-reference/provider/binding regressions, exact-digest operator permission/restart and mock16-reference 4K-parameter fan-out acceptance passed. The mock does not establish real-model native4K output.
- Shared references upload once with capability negotiation and old-server fallback; each ordinary GPT-image fan-out request retains16 images. Specific JSON/body/Base64/read errors and expanded-total preflight checks replace ambiguous binding failures. Reference bytes, dimensions, quality, settlement and existing Gemini/Vertex/Nano paths remain. No database migration or production configuration change; the exact original user request failure was not uniquely established from old logs.
- Main → L1 → COS application-only deployment completed after verified backups and natural read-only drain. All are healthy/restart0; database/Redis identities, configuration, volumes, namespaces and full migration rows are preserved (329/338/338). No forced cancellation, production paid-model call or successful Actions CI is claimed.
- Verified backup: `/opt/sub2api-backups/v0.2.18-custom.4-20261027T024304Z`; compatible application rollback: `.18.3` / `ghcr.io/hzihuan001/sub2api@sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`. Retain current business data and all migration rows; do not restore an old database for an application rollback.
- [Full `.18.4` release evidence](../docs/CUSTOM_RELEASE_0_2_18_CUSTOM4_20261027.md). Preserve other sessions' dirty work and recheck remote integration before the next release.

## Historical release: custom-0.2.18.3 (2026-10-26; superseded by custom.4)

- Runtime source/tag: `c2dbbe786e844ee1df77bfd710ebaf438f625126` / `custom-0.2.18.3`; source branch `codex/image-batch-16refs-20261026`, workspace `D:/projects/sub2api-image-batch-16refs-20261026`. Official base remains `v0.2.18`; later documentation commits do not move the runtime tag.
- Published deployment image: `ghcr.io/hzihuan001/sub2api@sha256:cb1c937d398989ccff2256b0bf7a7a51c7b63405f8dd5a267a1cd50f47e7b7ac`. Root-Dockerfile build, immutable pull-back, metadata, linux/amd64 and PostgreSQL18.6 client utilities passed. Frontend92, explicit `-tags=unit` backend reference/fan-out limits, operator permission/restart and mock16-reference batch acceptance passed. The prior `.18.2` missing-unit-tag test scope is corrected in its report.
- GPT-image batch shared references now allow16, matching per-prompt references. Model-specific caps and total output/attachment/inline-byte limits remain. No backend runtime, database migration or production configuration change. Main → L1 → COS application-only deployment completed after verified backups and zero read-only work counts; all healthy/restart0, data services/configuration/namespaces/migration rows preserved.
- Verified backup: `/opt/sub2api-backups/v0.2.18-custom.3-20261026T081840Z`; compatible application rollback: `.18.2` / `ghcr.io/hzihuan001/sub2api@sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`. Keep current business data and all migration rows; do not restore an old database as an application rollback.
- [Full `.18.3` release evidence](../docs/CUSTOM_RELEASE_0_2_18_CUSTOM3_20261026.md). No paid production model call, production rollback or successful Actions CI is claimed. Preserve other sessions' uncommitted work and recheck remote integration before the next release.

## Historical release: custom-0.2.18.2 (2026-10-26; superseded by custom.3)

- Version `0.2.18-custom.2`; immutable runtime source/tag `879a99a22e47a303604927104a6f67329dcd3b38` / `custom-0.2.18.2`; source branch `codex/image-studio-16refs-20261026` in `D:/projects/sub2api-image-studio-16refs-20261026`. Later commits update documentation only. The official base remains `v0.2.18`.
- Published and pulled deployment image: `ghcr.io/hzihuan001/sub2api@sha256:c90609bc4546ec309a980929ec8f28eef3cef236022dd698700920439ce78412`. The root Dockerfile, embedded version/commit, linux/amd64 and PostgreSQL 18.6 client tools were verified. Exact-digest operator permission/restart and mock single/batch image acceptance passed. The first isolated empty-database startup readiness failure was retained and the complete gate rerun after the database stabilized; full frontend Vitest retains one unrelated baseline failure. See the report instead of claiming universal CI success.
- GPT-image Studio references now allow16; Nano/Gemini14, Seedream10 and unknown4 remain. No backend runtime/schema/configuration change. Three production applications were updated main → L1 → COS after verified backups and zero read-only request/wait/image-task counts. All are healthy, restart0; config/mount/network fingerprints, PostgreSQL/Redis identities, namespaces and migration ledgers remain unchanged (main329, L1/COS338).
- Valid backup: `/opt/sub2api-backups/v0.2.18-custom.2-20261026T050710Z`. Earlier incomplete attempts are not restore points; final archives exclude only the exact active runtime-log directories and retain all business/configuration files. Dump lists, archive readability and SHA256 checks passed. No production paid-model call or rollback was performed during release.
- Compatible application rollback: `0.2.18-custom.1` / `ghcr.io/hzihuan001/sub2api@sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`. Retain current databases, data volumes and all migration rows. This supersedes older fallback examples below.
- Full evidence: [custom.2 release record](../docs/CUSTOM_RELEASE_0_2_18_CUSTOM2_20261026.md). Preserve other sessions' dirty worktrees; use the tested source branch/integration reference for later releases.

## Historical release: custom-0.2.18.1 (2026-10-24; superseded by custom.2)

- Source branch: `codex/v0.2.18-merge-20261023`; the immutable release source is `fc0d798bf2d6a08a074970d0485c567291e02660` and later branch commits only update release documentation.
- Source version: `0.2.18-custom.1`; immutable source tag: `custom-0.2.18.1`.
- Published image: `ghcr.io/hzihuan001/sub2api:0.2.18-custom.1`; immutable deployment reference: `ghcr.io/hzihuan001/sub2api@sha256:d7f365d1ba40b6c159d4155f7fbc2773d01f0fe6015af7906ebbece500ba1837`.
- The image was built from the root `Dockerfile`, pulled back by digest, and checked for embedded version/commit, `linux/amd64` architecture, `pg_dump`, and `psql`. The local three-site PostgreSQL/Redis Compose gate and restart persistence checks passed. Migration rollback remains forward-only: the old image must not overwrite the new database, and historical batch-discount normalization is not reversed by an application rollback.
- **Production deployment completed** on the validated OVH host: main → L1 → COS, application-only recreation, with PostgreSQL/Redis/volumes/networks/reverse proxy retained. Pre-deploy drain found no live slots, wait queues or image tasks; backup `/opt/sub2api-backups/v0.2.18-custom.1-20261024T000000Z` passed SHA256/gzip/dump verification. All three applications are healthy with restart 0 on the exact digest above; internal and public health checks returned 200 and post-start critical log matches were 0. Production migrations are main 329, L1/COS 338, with official `246` and custom `9000–9004` present. No paid model call, fault injection or long-duration capacity test was performed. Application rollback target is `.16` digest `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`; do not overwrite the upgraded database with an old backup.

## Historical candidate: custom.16 (2026-10-23; superseded by custom-0.2.18.1)

This candidate was the predecessor of the current `custom-0.2.18.1` rollout. Its exact immutable GHCR image passed local pull-back and operator-container smoke acceptance, and it remains the application rollback target. The old deployment-blocked notes below are historical and do not describe the current production state.

- Source version: `0.2.16-custom.16`; fix commit `0ba89f7d13fa7acb639d450c84614e9c62d21b5f`; release commit `fb02af3ece9ebf2cd54633bbfb6b839c6ba0051b`.
- Source branch: `codex/key-account-routing-stability-20261022`; pushed runtime tag `custom-0.2.16.16` points to the release commit above.
- Scope: for Responses requests only, remove the relay-owned multi-group stateful-request rejection and add a narrow recovery path after an upstream explicitly rejects `previous_response_id`: do not count that deterministic pointer rejection as account health failure, preserve current request content, remove the rejected opaque pointer, and retry another eligible account within existing budgets. Ordinary account failures retain existing HTTP/WebSocket behavior. No database migration or persistent conversation-body storage.
- Known boundary: the relay cannot recreate history absent from the current request after stripping an opaque pointer. Upstream may return a genuine history/validation error; ambiguous timeout/disconnect after upstream side effects can cause duplicate execution/cost. This is not a guarantee of seamless continuation for every provider/protocol.
- Final `.16` source backend regression passed in Docker Go `1.27.2`: service `267.528s`, handler `42.874s`, middleware `0.135s`; the API-key passthrough handler test confirms account A is skipped and account B succeeds after explicit pointer rejection. `git diff --check` passed. Published image `ghcr.io/hzihuan001/sub2api:0.2.16-custom.16` has immutable digest `sha256:037d353afff1a4e18c88ff9ad59fad6735195465a1dbb60ab5ace7c5d33223e7`; digest pull-back and isolated operator-container smoke passed. This does not prove every provider/protocol can resume complete history. GitHub Actions are disabled; tag push started no job, and manual dispatch returned HTTP 422, so no successful CI run is claimed.
- Historical note: at the time of this candidate, production backup and rollout were pending because the workstation had no configured SSH alias. The subsequent `custom-0.2.18.1` release used a validated connection and completed the approved backup → main → L1 → COS procedure.
- Prior image/fallback is `.14`: `ghcr.io/hzihuan001/sub2api@sha256:61468c96ecbafb5e79f959e06a74291773fae77db235fff130ad9d67df4f2ca7`. Reconfirm this digest and live site state immediately before production change.

The immutable source tag `custom-0.2.16.15` was pushed before final review exposed two recovery-path defects; it was not built into a published image or deployed. It remains unchanged for traceability and is superseded by `.16`.

See [`custom.16 candidate/release record`](../docs/CUSTOM_RELEASE_0_2_16_CUSTOM16_20261023.md) for historical evidence and rollback details.

## Current production: custom.14 deployed to all three sites (2026-10-23)

This section overrides the historical `.13`/`.12` coordinates and rollback examples below. **The exact `.14` published image passed local pull-back acceptance and the main → L1 → COS production rollout. All three sites now run the version/commit/digest below.** No newer official release was merged in this round; the official base remains `v0.2.16`.

- Authoritative source: `codex/key-account-routing-stability-20261022` in `C:/Users/a/.codex/worktrees/group-routing-current`.
- Version `0.2.16-custom.14`; runtime commit `09e885d4eb2098a48758b9b5309e166bd85737ce`; runtime tag `custom-0.2.16.14`.
- Published image `ghcr.io/hzihuan001/sub2api:0.2.16-custom.14`; immutable deployment reference `ghcr.io/hzihuan001/sub2api@sha256:61468c96ecbafb5e79f959e06a74291773fae77db235fff130ad9d67df4f2ca7`.
- Previous application fallback: `ghcr.io/hzihuan001/sub2api@sha256:9d79c4378f3e4dc6dd4d1648cff7420947ccb7984a33d7bb2cbd11030c32c14f` (`0.2.16-custom.13`).
- Build the release from the repository **root `Dockerfile`**. Its PostgreSQL-client stage and runtime `pg_dump`/`psql` dependencies must be preserved; do not substitute a slimmer application-only Dockerfile that breaks backup operations. Verify both utilities in the final pulled image without connecting to production databases.
- Actions dispatch returned HTTP 422; no successful runtime CI run is claimed. Use the already authorized local build → authenticated GHCR push → immutable pull-back acceptance path. Publication does not establish automated upgrade-PR or CI success.

### custom.14 migration and rollback boundary

Relative to the previous production `.13`, this release adds no database migration. Keep every previously applied migration and checksum unchanged; do not modify old migrations, delete compatibility tables or restore a database backup as an application rollback. The safe Responses recovery adapter is code-only and keeps the existing routing/settlement schema.

- 9003 adds opt-in `account_v2` routing metadata/revisions and authorization fencing while retaining existing `group_v1` behavior. The `.12` binary does not implement the new scope's semantics: inspect active `account_v2` Keys before any rollback, and stop for an explicit compatibility decision rather than silently treating them as old group routing, deleting Keys or modifying balances.
- 9004 persists a site UUID namespace. Cloned sites sharing Redis must retain distinct namespaces; validate main/L1/COS identity and do not copy one site's namespace into another.
- Exact application fallback: `ghcr.io/hzihuan001/sub2api@sha256:e6a53ec608abd463bfa41ccc6e939dca0175756adeb40e957ebe31a6b68c3610` (`0.2.16-custom.12`). Retain additive schema and business data. Rollback must not conceal the `account_v2` compatibility limitation.

### custom.14 completed acceptance and production evidence

The formal immutable image passed the local three-site recovery matrix **12/12**, with no findings. Local evidence: `D:/projects/key-route-recovery-20261023-r2/evidence.json`. The earlier `.13` gateway/admin matrices remain historical evidence, not a claim that this release reran every prior paid or provider-specific test.

| Site | Deployment UTC, 2026-10-22 | Asia/Shanghai, 2026-10-22 | Migration rows after update |
| --- | --- | --- | --- |
| Main | 20:15:31–20:15:39 UTC | 04:15:31–04:15:39 | 328 |
| L1 | 20:16:03–20:16:14 UTC | 04:16:03–04:16:14 | 337 |
| COS | 20:16:35–20:16:43 UTC | 04:16:35–04:16:43 | 337 |

Every station reached zero in the read-only drain before only its application service was recreated. All became healthy with zero restarts; environment/mount/port/network fingerprints and PostgreSQL/Redis container identities/start times were preserved. Every migration ledger row remained unchanged. Origin and public health, Keys, image-studio and documentation-tool checks returned 200; the three protected anonymous-interface checks returned 401. Critical startup-log findings were zero.

The subsequent read-only comparison confirmed three distinct site namespaces. Across the three sites, all **18** origin/public resources (entry index plus KeysView and UsersView bundles) matched the accepted local formal artifact by SHA. No paid production model call or production rollback was performed. This bounded acceptance does not claim successful Actions CI, every provider/endpoint, sustained concurrency capacity or long-term monitoring.

### Checklist for the next release

1. Pull the exact published digest locally; verify version/commit, health, PostgreSQL utilities and bounded shared-Key-editor/protocol checks. Do not substitute earlier candidate results for the actual artifact.
2. Inspect each live deployment's Compose path/service, old image, migration ledger, site namespace and configuration. Take private, readable PostgreSQL/configuration/persistent-data/proxy backups before mutation; record checksums and verify archive/dump readability without exposing credentials.
3. Deploy **main → L1 → COS**, only after the previous site's checks pass. Drain using read-only account/user/Key/live/WebSocket slots, waiting counters, batch jobs and image tasks. A timeout does not authorize forced cancellation. Update only the application service; preserve PostgreSQL, Redis, volumes, environment, networks and reverse-proxy configuration. Do not make paid model or payment calls without separate authorization.
4. On a failed station gate, stop subsequent stations and apply the recorded compatible fallback only when migration and routing-scope guards allow it. Do not restore older business data. Record exact outcomes, limits and immutable coordinates.

Documentation-only closure commits must not move the runtime tag. The older release records below remain historical evidence, not current `.13` acceptance or rollback instructions.

## Historical custom.12 release coordinates and authorization

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
