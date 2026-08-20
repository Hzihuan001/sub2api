# Sub2API Operator custom release runbook

## Fixed release coordinates

- Upstream baseline: `v0.1.179` / `75f88be5`
- Development branch: `custom/v0.1.179`
- Application version: `0.1.179-custom.1`
- Source tag: `custom-0.1.179.1`
- Container tag: `ghcr.io/<fork-owner>/sub2api:0.1.179-custom.1`
- Initial target platform: `linux/amd64`

Never create a `v*` tag for this custom build. Never put passwords, tokens, SSH
keys, server addresses, or the contents of `CODEX_HANDOFF.md` in Git.

## Local quality and isolated container gate

Use Go 1.26.6, Node 20.20.2, pnpm 9.15.9, and golangci-lint 2.9.0. Run:

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

## Fork and GHCR gate

1. Set the personal Fork as `origin`; keep the official repository as `upstream`.
2. Push only `custom/v0.1.179` and tag `custom-0.1.179.1`.
3. The `Custom CI and GHCR release` workflow runs all tests before publishing.
4. Record both the mutable tag and immutable `sha256` digest from the workflow summary.
5. Pull and run that exact digest through the local container test above.
6. If either server reports `arm64`, update the workflow to build
   `linux/amd64,linux/arm64` before publishing; do not emulate a production
   architecture decision after a digest has already been approved.

## Read-only server preflight

Run this separately on Tencent Cloud and OVH before making any change. Save the
output in the release report, redacting secret values.

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
Sub2API version is newer than `v0.1.179`; this release must never downgrade it.

## Backup gate

Before staging and again before production, create timestamped backups outside
the application volume and verify that each file is non-empty:

- PostgreSQL custom-format dump plus a restore/list verification;
- `.env` and all Compose/1Panel definitions;
- `/app/data` and every persistent directory or bind mount;
- current image reference and immutable digest;
- OpenResty configuration and relevant proxy/SSE settings.

Do not copy secrets into the report or repository. Do not delete/recreate
PostgreSQL, Redis, application volumes, networks, or proxy configuration.

## Tencent Cloud staging

1. Complete the read-only preflight and backup gates.
2. Change only the Sub2API service image to the approved GHCR digest.
3. Pull the digest and run `docker compose up -d --no-deps sub2api` (or the
   equivalent 1Panel operation that recreates only the application container).
4. Run health, role login, allow/deny matrix, user/API key, WebSocket, browser,
   OpenResty, and SSE checks.
5. Inspect application/PostgreSQL/Redis/OpenResty logs and observe for at least
   30 minutes.
6. Record commit, tag, digest, commands, results, timestamps, and issues. Any
   failure restores the old image digest and blocks production.

## OVH production approval gate

Production requires a separate human confirmation after staging passes. Reuse
the exact staging-approved digest; never rebuild. Repeat preflight and backups,
then change only the Sub2API image and recreate only that service. Preserve
JWT/TOTP keys, database, Redis, volumes, ports, networks, OpenResty, and
Cloudflare settings.

After deployment, check health, all three role logins, representative allowed
and denied endpoints, normal user API keys, SSE streaming, 5xx rates, database
and Redis logs, and the authorization audit trail. Observe for at least 30
minutes and perform a 24-hour error-rate/audit follow-up.

## Rollback

Set the application image back to the recorded previous digest and recreate
only Sub2API. This customization has no schema migration. An older binary will
treat residual `operator` rows as non-admin; those users temporarily lose
management access while admin remains available.

If any official irreversible migration ran during the release, restore the
verified pre-release PostgreSQL backup together with the old image. Never run
an old image against a database that has crossed an unverified irreversible
migration.

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
