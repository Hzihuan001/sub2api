# Local image recovery acceptance

`../tests/image-studio-recovery.ps1` exercises the real app process, production
managed-fanout provider, PostgreSQL persistence, result volume, and Redis worker.
`mock-image-recovery-upstream.mjs` is a separate, controlled synthetic upstream.
Neither file changes production behavior or the existing image smoke test.

## Preparation and safety

This harness is opt-in. Without `-Execute` it prints a preparation notice and does
not call Docker. A prepared script is **not** passing recovery evidence.

Requirements:

- PowerShell 7, local Docker Engine, cached `node:24-alpine` image (no implicit pull).
- The candidate is already deployed by the operator to exactly
  `sub2api-operator-test-modular`, default app port `127.0.0.1:18089`.
- Supply the candidate's exact `sha256:` image ID, not a mutable image tag.
- App, Postgres, and Redis must all carry the expected Compose project/service
  labels and share its labeled network. The actual PostgreSQL `data_directory`
  must agree with resolved `PGDATA`; Redis `CONFIG GET dir` establishes its actual
  directory. Their effective data mounts and every nested mount must be writable
  named volumes labeled for this project and respectively `postgres_data`,
  `redis_data`, or `app_data`. Host binds, foreign volumes, and bind-backed named
  volumes are rejected before mutations. A shadowed ancestor mount is not the
  effective storage (notably Postgres 18's anonymous parent volume).
  Every published service port must be loopback-only.
- Default batch queue keys, Redis DB 0, and `/app/data/batch-images` storage.
  The harness checks actual active membership before lease acceleration.
- No other unfinished or queued batch jobs. The script rechecks this before
  **every** kill. Do not submit concurrent work during the acceptance run.
- Local synthetic administrator credentials from `docker-compose.operator-test.yml`.
  The fixture uses a newly created group, user, API key, and mock-only account.

After the operator has approved testing the fixed candidate, an example command is:

```powershell
pwsh -NoProfile -File deploy/tests/image-studio-recovery.ps1 `
  -Execute -ExpectedImageId sha256:<64-hex-image-id> `
  -Project sub2api-operator-test-modular -Port 18089 -MockPort 18090
```

The script does not build/redeploy, change restart policy, remove containers or
volumes, flush Redis, delete broad keys, rewrite job states, add reconciliation
APIs, or manufacture completed results. HTTP clients forbid redirects and only
contact loopback URLs; the app contacts the mock on the isolated Docker network.

## Three real process/queue scenarios

| Scenario | Deterministic cut point | Required recovery evidence |
| --- | --- | --- |
| Durable result before settlement | Mock receives one request; a separate PostgreSQL transaction locks only that job row; mock releases a real PNG; the app fsyncs its `.jsonl.partial`, updates item progress, and blocks updating the parent row. Harness verifies the actual file, successful item, blocked DB session, running job, and no capture/usage, then SIGKILLs the app. | Same container/image starts a new process; the persisted PNG is indexed/downloadable; one success, one capture, one usage row costing $0.01, balance $9.99, frozen $0; upstream still exactly one request. |
| Completed duplicate deliveries | After completion, inject two exact batch IDs into the real ready list and wait for worker acknowledgement; SIGKILL; inject three more copies while stopped; restart and await acknowledgement. | Full financial/result/usage snapshot remains identical; upstream remains exactly one call. This forces actual delivery, not just enqueue's inflight dedup check. |
| Sent but unknown | Mock records receipt but never sends any response; verify persisted managed_fanout dispatch claim and absence of both final/partial result files; SIGKILL the app with the HTTP request still open. | Recovery attempts and an explicit duplicate delivery requeue without another upstream call; dispatch claim unchanged, status running, original $0.01 hold retained, no capture/release/usage, no result files. Mock records the real connection interruption. |

The kill command is `docker stop --signal SIGKILL --timeout 0 <exact-container-id>`.
The explicit signal is not a graceful stop: the script verifies exit code 137,
stopped state, and a different process start timestamp after `docker start`.
`stop` suppresses `unless-stopped` auto-restart long enough to verify and prepare
the recovery boundary; it does not update the restart-policy configuration.

### Checkpoint and timing limitations

The DB row lock is an external test synchronization hook, not a runtime test
hook. It changes no result/job/billing data. The app's production order is:
`appendManagedLine` writes and fsyncs, then `persistManagedProgress` updates the
item, then `RefreshBatchImageJobCounts` needs the locked parent row. Observing the
successful item and blocked update proves the checkpoint is after fsync and
before indexing/settlement. Merely observing mock response release is insufficient.

Default stale-active recovery and lock expiry take minutes. Only after verifying
the exact app is stopped, the harness updates **that owned batch's** active score
to 0 and deletes **that owned batch's** lock key. The production stale-active
recovery loop and worker then perform recovery on startup. Evidence explicitly
records this time acceleration. It proves crash recovery logic, not natural
wall-clock expiry or unmodified TTL timing. No direct ready injection substitutes
for stale-active recovery in either in-flight crash scenario.

The durable case tests real in-flight kill after a persisted partial result, not
a DB-manufactured indexing/settling job. It does **not** create a crash in the tiny
capture-to-usage or usage-to-completion windows. Completed duplicates demonstrate
idempotent terminal handling, not those intermediate settlement boundaries.

The unknown case is intentionally unresolved. A hold retained without resubmission
is the success criterion; automatic reconciliation, late-result retrieval, and
automatic refunds are outside this test. A missing checkpoint is never treated
as success or replaced with a fake result.

## Evidence and retained fixtures

The printed temporary artifact directory contains:

- `evidence.json`: candidate image, exact container IDs, test resource IDs, batch
  IDs, true kill/restart timestamps, lease acceleration and duplicate injections,
  database/upstream snapshots, final pass/failure, and explicit limitations.
- `durable-before-kill.jsonl`: the app's actual persisted checkpoint.
- `durable-recovered.png`: result downloaded through the public batch API.
- `upstream-final.json`: every attempt, effective model, output count, idempotency
  key, response release time, and connection-interruption time.

Snapshot assertions inspect both live/archive billing-dedup tables, usage row
count/cost, user available/frozen balance, API key usage stability, immutable
dispatch state, and result state. The mock deliberately does not deduplicate:
an accidental second upstream attempt remains visible and fails acceptance.
Usage evidence also counts **all** rows/cost/request IDs for this run's unique
synthetic key: zero before the first result, exactly one durable capture after
completion, and still exactly that one row after the unknown case. An extra usage
row under a different request ID cannot evade the per-batch assertion.

Queue acknowledgement uses one read-only `EVAL_RO` snapshot of ready, active,
delayed, lock, and inflight membership. Completed work is drained only if all five
are absent; separate reads cannot miss a delayed-to-ready transfer. The unknown
case uses the same atomic snapshot but expects delayed and inflight to remain.

The script-level negative checks require no Docker, Redis, PostgreSQL, or network:

```powershell
pwsh -NoProfile -File deploy/tests/image-studio-recovery-contract-test.ps1
```

They import only function ASTs from the harness and stub transports with explicit
allowlists. They reject ready/active/delayed/lock/inflight residue, malformed queue
evidence, foreign/wrong-role/bind/nested data mounts, actual data-path overrides,
and alternate-request-ID usage. They pin a single `EVAL_RO` call and its exact
keys, not an execution of Redis Lua or real Docker recovery; passing these checks
does not substitute for the opt-in integration run.

On failure, the harness releases only its uniquely named PostgreSQL lock session
and attempts to restart only the exact app it stopped. It leaves all synthetic
fixtures, mock container, job records, and any unknown hold for inspection. It
does not refund or cancel unknown work. A rerun will refuse another run's
unfinished job; the operator must explicitly decide how to dispose of the
isolated test environment after collecting evidence. No cleanup command is
embedded in this test.
