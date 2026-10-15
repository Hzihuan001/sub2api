# Modularization performance acceptance (local only)

## User disposition — 2026-10-15

After clarification that the INFO/CBBC difference was not reproduced by the bounded DEBUG run and did not establish a stable regression, the user said “那就不管” (stop pursuing this). Stop further SSE diagnostics and load tests; record this item as an accepted unresolved performance risk, no longer a current blocker. Preserve all measurements, thresholds and the mixed technical conclusion below; this is not a performance pass or proof of measurement noise. That single performance disposition did not itself authorize publication or waive other release restrictions.

The user subsequently approved the separate local build → GHCR → pull-back acceptance → backups → main/L1/COS sequential application-only deployment. Release `0.2.16-custom.6`, runtime commit `655a16868`, has now been published and deployed to all three sites. The identical pulled artifact passed isolated acceptance; backups and per-site health/version/log checks passed. The main site's active request drained before its update, with no forced interruption. Production did not receive another performance run, paid model call, full authenticated role matrix or long-term monitoring. Follow the [release report](../../docs/CUSTOM_RELEASE_0_2_16_CUSTOM6_20261015.md) for exact checks. Actions dispatch still returned HTTP 422; this is not a CI pass. The 15 baseline lint findings, incomplete P4 full old/new accounting matrix and unclosed real upgrade PR workflow remain explicit. None of these release steps reran or reclassified this report's measurements.

All sections below retain the experiments, source scope and decisions at their recorded stages. Historical references to a next diagnostic or deferred publication are not instructions to restart SSE testing; the disposition and release outcome above supersede those pending-action descriptions.

## Fixed inputs and safety

- Published runtime: `00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b`, source/document baseline `989594099c1bd10db911c737449d15d2bb6149ed`.
- Initial candidate runtime: `57721bba97234054d1e4a9c3bd13aa078967da2d`. Later acceptance-only commit `a44618924` does not change runtime.
- Published local image ID: `sha256:3a2f925574266819adc3cddad9595170cd71404519f315326f83d61098b6a80b`.
- Initial candidate image ID: `sha256:264242a7a7664b8e285cebd5c464825d3dafd8e709e230123ee30f61270424eb` (`sub2api:modular-57721-local`).
- Final candidate image ID: `sha256:a11840c20fa0e546ec1518f44762cb51ce448382a38beece0214f46c0e116a11` (`sub2api:modular-53fd17-local`), runtime commit `53fd17b5b46be47a216f55381beaac24eefe667f`.
- No production calls, remote mutations, paid providers, unrelated-volume reuse, image deletion or production credentials. Follow-up diagnostics reuse only this report's explicitly owned synthetic fixture.
- Full acceptance requires **both** microbenchmarks and equal-load HTTP/SSE evidence. The unchanged P0 classifier is a control, not proof that new policy wrappers are inexpensive.

## Microbenchmark method

Run `deploy/tests/modular-microbenchmark.ps1 -Execute` with PowerShell 7. It uses Go 1.27.2, `GOMAXPROCS=2`, `GOTOOLCHAIN=local`, offline modules, fixed cache/temp directories on D:, read-only Go overlays, and two serial baseline/candidate rounds. Each round uses `-count=5 -benchtime=100ms`. Both sources execute the same `modular-perf-benchmark_test.go` fixture through existing HTTP, passthrough, stream-failed, stream-error and replay host functions. No repository, cache or provider I/O occurs in these benchmarks.

The baseline export was verified against its Git blobs before use; the runner rechecks the three policy/wrapper SHA-256 fingerprints. The P0 classifier fixture must retain Git blob `3f85a17acf7adba0bdcfc573dd72d28a42d66e2f`.

P0 acceptance rule remains: investigate median time regression above 20% **or any increased allocation count**, rather than changing the threshold or rerunning until green.

### Initial result, 2026-10-15: **not passed**

Raw files: `D:/projects/go-tmp/modular-microbenchmark-20261015/{baseline,candidate}-round{1,2}.log`; provenance in the same directory. All four benchmark runs completed successfully. Values below are medians across the ten samples, not the best sample.

| Host wrapper/input | Baseline ns/op | Candidate ns/op | Change | allocs/op baseline → candidate |
| --- | ---: | ---: | ---: | ---: |
| HTTP / model | 3250.5 | 5903.5 | +81.62% | 6 → 7 |
| HTTP / overload | 3801.5 | 6538 | +71.98% | 10 → 11 |
| Stream failed / request | 2956 | 4523.5 | +53.03% | 6 → 9 |
| Stream error / request | 2115 | 3161 | +49.46% | 5 → 7 |
| Stream failed / safety | 2678.5 | 4134.5 | +54.36% | 5 → 8 |
| Stream error / safety | 1830 | 2675.5 | +46.20% | 4 → 6 |
| Replay / request | 6090 | 7374.5 | +21.09% | 12 → 16 |

Control classifier medians differ by -0.82% to +3.11%, with unchanged allocations. Overload stream-failed/error wrappers improve by 30.03%/23.72%, reducing ten allocations each; these improvements do not cancel the seven regressions above. Other measured cases remain below the threshold without added allocations.

Source attribution, not a runtime fix:

1. The shared HTTP decision performs the access-state JSON check before the cheap status check. The published ordinary HTTP host wrapper did not do this scan; passthrough had different semantics and did need it.
2. The shared stream decision computes semantic status before the request/safety class short circuit. Published stream wrappers returned early for those classes.
3. Replay facts are eagerly assembled. The request replay benchmark shows four additional allocations; exact short-circuit preservation must be checked against the published constructor before editing.

This is a failed performance gate, not a reason to discard the measurement or claim that a normal-path test proves error paths unchanged.

### Minimal correction, 2026-10-15: microbenchmark gate passed

Runtime commit `53fd17b5b46be47a216f55381beaac24eefe667f` restores the published short-circuit behavior while retaining one shared decision implementation. It restricts the access-state scan to passthrough, avoids discarded semantic-status work in boolean-only stream adapters, and lazily computes replay capacity metadata after the published early-return cases. This is a runtime change, not a new/easier benchmark.

Exactly one prescribed follow-up baseline/candidate round was run with `-Rounds 1 -OutputDirectory D:/projects/go-tmp/modular-microbenchmark-53fd17b5-20261015`. The Go version, `GOMAXPROCS=2`, five samples, 100 ms duration, input payloads and wrapper fixture were unchanged. Fixture SHA-256 remains `dcd4a213a255e40994cf8f6e862ea830306c789fd370d625ff98f2a96b2ada74`. Both runs terminated with exit code 0; old failure evidence remains intact.

| Previously failing host wrapper/input | Baseline ns/op | Fixed candidate ns/op | Change | allocs/op baseline → fixed |
| --- | ---: | ---: | ---: | ---: |
| HTTP / model | 3192 | 3179 | -0.41% | 6 → 6 |
| HTTP / overload | 3901 | 3828 | -1.87% | 10 → 10 |
| Stream failed / request | 2931 | 2897 | -1.16% | 6 → 6 |
| Stream error / request | 2101 | 2091 | -0.48% | 5 → 5 |
| Stream failed / safety | 2681 | 2685 | +0.15% | 5 → 5 |
| Stream error / safety | 1816 | 1792 | -1.32% | 4 → 4 |
| Replay / request | 6000 | 6313 | +5.22% | 12 → 12 |

All 25 host-wrapper medians meet the original threshold: the largest increase is +5.31% (`safety/passthrough`, 1790 → 1885 ns/op), with no added allocations or median bytes in any case. Overload stream-failed/error retain improvements of -30.31%/-23.74% and ten fewer allocations each. This is not a promise of equivalent end-to-end latency; the Docker gate remains separate.

| Unchanged P0 classifier control | Baseline ns/op | Candidate ns/op | Change | allocs/op, both |
| --- | ---: | ---: | ---: | ---: |
| overload | 3794 | 3710 | -2.21% | 10 |
| quota | 2813 | 2933 | +4.27% | 5 |
| model | 2700 | 2828 | +4.74% | 5 |
| request | 2082 | 2047 | -1.68% | 5 |
| safety | 1770 | 1807 | +2.09% | 4 |

The same control source and raw published baseline fixtures remain reusable for later comparisons. Reuse must still match runtime source scope, fixture hash and toolchain; do not substitute the control result for full-wrapper or HTTP/SSE coverage. Docker measurements must use a new image built from `53fd17b5`, not the older `57721bba9` image listed as the initial candidate above.

## Equal-load Docker method

Prepared runner: `deploy/tests/modular-performance.ps1`. Default invocation is read-only. Execution requires `-Execute -CandidateImage <local image> -ExpectedCandidateImageId sha256:<exact ID> -ExpectedCandidateCommit <40-character runtime commit>`. Runtime revisions are checked from each running binary's `--version`, not optional OCI labels (local builds may have no revision label).

- Dedicated project `sub2api-operator-test-perf20261015`, requested loopback application port `18092`. Docker Desktop does not publish a port from this internal-only network: `18092` is retained as a Compose mapping/evidence field, not a browser-accessible address. Control and measured clients used the fixed in-network service name `sub2api:8080`; no external endpoint was used.
- Exact project resource names must be absent before creation; after startup, real app/PostgreSQL/Redis mounts and Compose labels must match newly created volumes. Existing `18089`, `18086`, `18080` environments are not touched.
- Internal Docker network prevents external provider/price connections. Synthetic upstream never forwards requests. Only local synthetic accounts, keys, groups and balances are used.
- Same database, Redis, account and key across two baseline/candidate rounds. App-only replacement changes the image; resource limits remain 2 CPUs / 2 GiB.
- Each variant/round: 30 HTTP + 30 SSE warmup requests, followed by five windows of 100 HTTP and five windows of 100 SSE requests at concurrency two. Total measured requests: 4000; warmup: 240.
- Fixed upstream delay: 5 ms before JSON or first SSE frame, another 5 ms before SSE completion. TTFB measures the first body bytes, not just headers.
- Every window checks all upstream calls, including unexpected probes and malformed calls; total delta must be exactly 100, with no extra probe/rejected calls. The measured client requires HTTP 200, natural body EOF, expected text and (for SSE) the `response.completed` marker. It does not independently parse the JSON/SSE grammar: a truncated body containing those strings could pass. The fixed local mock emits complete JSON/SSE, but that fact must not be confused with a full client-side completeness validator.
- Record p50/p95 TTFB and total time, app-container cgroup CPU/window/request, and end-of-window process RSS. CPU includes background tasks, health checks and sampling; RSS is not peak RSS. Container ID/image/start time/restart count must stay fixed within a window and CPU counters must remain monotonic.
- Evidence is retained on D: and the isolated project remains available. Cleanup is deliberately not automatic.

## Equal-load Docker result, 2026-10-15

The final candidate was measured once under the prescribed two serial rounds. Each variant/round completed 30 HTTP + 30 SSE warmup requests and five 100-request HTTP + five 100-request SSE windows. All 40 measurement windows passed the implemented HTTP-status/EOF/marker, identity, restart, monotonic CPU and upstream-call checks (not independent JSON/SSE completeness parsing). The retained raw evidence is:

- `D:/projects/go-tmp/modular-performance-53fd17b5-cgroup1-20261015/results.json`
- `D:/projects/go-tmp/modular-performance-53fd17b5-cgroup1-20261015/windows.jsonl`
- `D:/projects/go-tmp/modular-performance-53fd17b5-cgroup1-20261015/warmups.jsonl`
- `D:/projects/go-tmp/modular-performance-53fd17b5-cgroup1-20261015/provenance.json`

The table reports the median across the ten windows for each protocol. TTFB and total are milliseconds; CPU is app-container cgroup microseconds per request (including background work, health checks and sampling); RSS is the end-of-window sample in KiB, not peak RSS.

| Protocol / metric | Baseline | Candidate | Change |
| --- | ---: | ---: | ---: |
| HTTP p50 TTFB | 19.470 | 19.475 | +0.02% |
| HTTP p95 TTFB | 38.479 | 34.155 | -11.24% |
| HTTP p50 total | 19.554 | 19.564 | +0.06% |
| HTTP p95 total | 38.518 | 34.256 | -11.07% |
| HTTP CPU/request | 12,072.01 | 12,558.01 | +4.03% |
| HTTP RSS | 131,864 | 132,770 | +0.69% |
| SSE p50 TTFB | 16.510 | 16.735 | +1.36% |
| SSE p95 TTFB | 23.806 | 32.203 | +35.28% |
| SSE p50 total | 24.45 | 24.73 | +1.15% |
| SSE p95 total | 43.51 | 44.29 | +1.79% |
| SSE CPU/request | 11,627.75 | 12,164.53 | +4.62% |
| SSE RSS | 146,464 | 148,964 | +1.71% |

The arithmetic means of the ten window summaries are HTTP p50/p95 total 20.352/38.058 → 20.081/36.437 ms, CPU 12,349 → 12,705 µs/request (+2.88%), RSS 132,786 → 134,299 KiB (+1.14%); SSE p50/p95 total 24.790/44.035 → 25.993/45.129 ms, CPU 11,934 → 12,907 µs/request (+8.16%), RSS 146,464 → 148,884 KiB (+1.65%). These are descriptive results, not proof of resource equivalence. No independently calibrated cgroup CPU/RSS threshold was defined before the experiment.

### SSE-tail investigation: not yet cleared

The SSE p95-TTFB median is above the P0 investigation threshold in this small local sample. It must not be silently erased by substituting means, rerunning until green or changing the load. No second measured run was started. The following is read-only analysis of the original raw request timings, using the same nearest-rank percentile function as the client:

| SSE, all 1,000 measured requests per variant | Baseline ms | Candidate ms |
| --- | ---: | ---: |
| p50 TTFB | 16.612 | 16.856 |
| p95 TTFB | 28.237 | 39.141 |
| p99 TTFB | 65.816 | 64.388 |
| p50 total | 24.580 | 24.823 |
| p95 total | 49.554 | 53.086 |
| p50 first-body-to-completion gap | 7.937 | 7.902 |
| p95 first-body-to-completion gap | 14.256 | 12.691 |
| p95 headers-to-first-body gap | 0.188 | 0.211 |

The all-request SSE p95 TTFB increase is +38.61%, so it is not only an artifact of taking medians of window percentiles. Round 1 raw p95 TTFB is 38.875 → 41.766 ms (+7.44%); round 2 is 22.411 → 31.580 ms (+40.91%). p50 TTFB increases only +1.47% overall, p99 improves, and first-body-to-completion gaps do not increase. This is a tail-latency observation before/at the first body, not evidence of persistent whole-response buffering. There are 46 versus 75 SSE requests with TTFB above 30 ms; 20 versus 21 occur among each fresh client process's first four requests, so client startup alone does not explain the remaining difference. Raw windows vary substantially (candidate round-1 p95 TTFB ranges 18.81–52.25 ms), but scheduler/background attribution is still unproven without additional evidence. Source-path inspection is the next bounded diagnostic step; this report does not mark the overall performance gate passed.

This test is not a WAN, Cloudflare, WebSocket or long-duration saturation claim, and cgroup CPU/RSS values are not pure request cost.

Upstream mock counters are complete for the retained run: `total=4,241`, `http=2,120`, `sse=2,120`, `probe=1`, `rejected=0`. The 4,241 calls comprise one account/bootstrap probe, 240 warmups and 4,000 measured requests; every measured window had exactly 100 total upstream calls with zero extra probe and zero rejected calls. The probe is therefore not claimed to be absent from the entire run; it did not occur in any measured window. The static request-I/O audit remains the evidence for no new normal-request remote call.

Two preflight corrections are retained separately and produced no measurement windows: the internal network does not expose the requested host port, and this Docker Desktop uses cgroup v1 (`cpuacct.usage` converted from nanoseconds and `/proc/1/status` `VmRSS`). Their diagnostics are in `D:/projects/go-tmp/modular-performance-53fd17b5-20261015/initialization-diagnostic.md` and `D:/projects/go-tmp/modular-performance-53fd17b5-internal-20261015/cgroup-diagnostic.md`; neither was a “rerun until green.”

The dedicated Compose project remains intact for read-only attribution of the SSE-tail observation. No performance process is running: session 51414 terminated with exit code 0. Raw evidence and diagnostics remain on D:. The baseline and final candidate images, and all other stacks, were not removed. The complete measured run is available, but the SSE-tail investigation remains open; production release remains a parent-level decision.

## Prespecified bounded SSE crossover diagnostic

This section was recorded before the additional diagnostic load. Original measurements and the open SSE-tail observation above remain unchanged. The main task explicitly approved one limited local diagnostic after the separate 11-scenario test process terminated and its test project was removed. No production call or product-code change is included.

- Fixed order: candidate → baseline → baseline → candidate (CBBC), to balance a linear time/order effect instead of always placing baseline first.
- Each stage forcibly recreates only this project's app, including the middle baseline → baseline stage; it retains the exact same PostgreSQL, Redis, mock, synthetic account/key and resource limits.
- Each stage: 30 SSE warmups followed by exactly three 100-request SSE windows, concurrency 2. Total: 1,200 measured SSE requests and 120 warmups. No HTTP measurement and no additional samples after the prescribed run.
- Identical measured client/mock SHA-256: `b46b2da7968cc2b6236c30a7fa58cafe77ca98c1ede91376779a4ab001be1d4b`; same 5 ms + 5 ms response timing, models and body. This does not replace the original full run.
- Hypothesis H1: candidate-specific pre-first-byte overhead remains when candidate runs before and after baseline. H2: order/time/background variance changes the direction or magnitude across the balanced stages. Neither is assumed true in advance.
- Primary statistic: nearest-rank p95 TTFB from all 600 measured requests per variant. Secondary statistics: median of window p95 TTFB, p50 TTFB, total latency, cgroup CPU/RSS and first-four-request startup tails, without excluding observations from the primary statistic.
- Prespecified interpretation: if aggregate p95 and both paired comparisons (C1/B2 and C4/B3) exceed +20%, mark a consistent candidate regression requiring attribution. If aggregate is within 20% and paired effects are inconsistent/reversed, mark the original observation not reproduced with order/time confounding possible, not proven noise. Any mixed result remains unresolved. Do not append samples, alter the client, change thresholds, or erase original evidence to obtain a green result.
- Existing safety checks remain: exact runtime/image/fixture/container identity, internal network and volume ownership; each measured window must have exactly 100 upstream calls, no new probes/rejections, HTTP 200/natural EOF/expected response markers as implemented by the unchanged client, no restart or decreasing CPU counter. Independent grammar/completeness parsing is not part of this measured fixture.

Execution command (PowerShell 7), new evidence directory only:

```powershell
./deploy/tests/modular-performance.ps1 -Execute -SseCrossoverDiagnostic `
  -CandidateImage sub2api:modular-53fd17-local `
  -ExpectedCandidateImageId sha256:a11840c20fa0e546ec1518f44762cb51ce448382a38beece0214f46c0e116a11 `
  -ExpectedCandidateCommit 53fd17b5b46be47a216f55381beaac24eefe667f `
  -OutputDirectory D:/projects/go-tmp/modular-performance-sse-cbbc-20261015
```

### CBBC result: mixed, unresolved

The single approved diagnostic finished all twelve windows, with `results.json`, `windows.jsonl`, `warmups.jsonl` and `provenance.json` retained in `D:/projects/go-tmp/modular-performance-sse-cbbc-20261015`. The original process handle was 88564; after the interrupted chat its process was independently observed to have ended and all final artifacts were present. No third measured run was started.

| Prespecified raw-request p95 TTFB comparison | Baseline ms | Candidate ms | Change |
| --- | ---: | ---: | ---: |
| All 600 requests per variant | 39.921416 | 48.521648 | +21.54% |
| Candidate-first pair C1 / B2 (300 each) | 37.475425 | 44.797361 | +19.54% |
| Candidate-last pair C4 / B3 (300 each) | 41.289654 | 49.132992 | +19.00% |

The aggregate exceeds 20%, but neither paired comparison exceeds 20%. Under the recorded rule this is **mixed / unresolved**, not a pass and not proof of either a code regression or host noise. Source review found no new successful passthrough-SSE request-path buffering/flush/lock/remote call; both images' Go 1.27.2 build info and dependency/build flags were also compared. Those facts constrain attribution but do not override measured latency.

Mock counters went from `4,241/2,120/2,120/1/0` to `5,561/2,120/3,440/1/0` (total/HTTP/SSE/probe/rejected): exactly 1,200 measured SSE plus 120 SSE warmups, no new probes/rejections or HTTP calls. Each measured window retained the original 100-call check. The project and both old data sets remain available; no extra samples will be appended to make this result green.

## Post-measurement maintenance-tool hardening

These changes were made **after** the two measured runs. They were validated with local positive/negative contract cases only; no microbenchmark, HTTP/SSE load, image build or production deployment was rerun with the new tools.

- Exact historical measured fixture body: `deploy/acceptance/fixtures/modular-perf-fixture-20261015-b46b2da7.mjs`, SHA-256 `b46b2da7968cc2b6236c30a7fa58cafe77ca98c1ede91376779a4ab001be1d4b`. Both 4,000-request and CBBC evidence refer to this old body, not the revised client.
- Revised client validates complete JSON and SSE frames/terminal state **after** recording `totalMs`; it rejects truncated JSON, partial SSE frames, missing/failed completion and output after completion. `node deploy/tests/modular-perf-fixture.mjs selftest`: 4 positive and 9 negative cases passed without network calls. Revised fixture SHA-256 `1704f6609d7296f6d81f3ee43b4c79a1f60aad6c6b8727b1f88ee164b266fb0d` has not been performance-measured.
- Snapshot capture immediately rejects failed `docker inspect`, absent/ambiguous identity, missing start/restart state or unexpected image, before the following exec can replace the native exit status. This closes a fail-open edge in the earlier guard; the existing raw results do not retroactively gain this revised check.
- Microbenchmark maintenance runner requires a clean tracked candidate backend and no extra Go/module inputs (including ignored Go files), validates all baseline Go/module inputs against pinned Git blobs with Git checkout normalization, and checks candidate HEAD plus full raw-byte backend/fixture fingerprints before and after measurement. Untracked non-Go frontend build assets are not compiled by this service benchmark, but remain included in its before/after fingerprint. The original runs relied on separately verified baseline Go blobs and main-task source freezing; their historical provenance is not rewritten as if it contained the new guard. Initial maintenance preflight caught that an overbroad all-file raw-blob check rejected the export's noncompiled `.dockerignore`/CRLF checkout and current generated web assets; the current guard scopes Git identity checks to Go/module compilation inputs without changing any source/export files.
- `deploy/tests/modular-performance-contracts.ps1` extracts only the relevant functions for local tests, shadows Docker with a stub, and uses a disposable synthetic Git tree. All 18 checks passed: valid/missing/mismatched identity, inspect failure not reaching exec, source mutation, baseline mismatch/extra files and dirty/untracked/ignored candidate inputs. It does not run any benchmark or contact Docker/the network.

Actual read-only baseline preflight matched all 3,065 tracked Go/module Git blobs, then correctly rejected two additional pre-existing reproduction test files in that export: `internal/service/batch_image_cancel_accounting_repro_test.go` (SHA-256 `ae2992b9925b8a79aacb78e7769a0be423cecdd6a032f47ad117aada8a50ba39`) and `internal/service/gateway_failure_attempt_identity_repro_test.go` (`03e0fbb6159acaaffe2725de2985593d50300deca55fa8fdb64018d4d5c149f0`). They have no `init` function and their test functions were excluded by `-run '^$'`, but they are additional compilation inputs. Thus the original baseline is not retroactively described as an all-source-clean checkout. These files were not deleted or altered; a future strict microbenchmark requires a fresh clean baseline export (or separately reviewed explicit overlay). No measurement was launched by this guard check.

The crossover command above is a record of the one permitted diagnostic against its retained exact containers/counters and historical fixture; the post-measurement tool intentionally rejects trying to repeat it with changed inputs. No “clean” result replaces the unresolved measurement.

Follow-up checkout portability check: with `core.autocrlf=true`, Git would have transformed the archived LF fixture to CRLF and changed its SHA-256 from `b46b2da7…` to `79ef8cee…` on a fresh Windows checkout. Narrow `.gitattributes` rules now pin LF for the historical and current performance MJS files. `git cat-file --filters` under `core.autocrlf=true` reproduces the exact recorded hashes for both, without changing fixture bodies. The PowerShell contracts now also require these attributes and the immutable historical checksum: **21 checks passed**, with no Docker/network/model request.

## User-authorized finite DEBUG attribution (2026-10-15)

The user explicitly approved one additional diagnostic of exactly 100 synthetic SSE requests per version. This is not another performance acceptance run. Fixed order baseline → candidate, concurrency 2, no model warmups/retries, same app limits and owned PostgreSQL/Redis/mock. Only the app was recreated; the final candidate was restored. Runtime logging was temporarily set to DEBUG with sampling disabled via the existing local admin API, then all original functional settings were verified restored (INFO, access-log persistence false and unchanged retention). No business code, production configuration or paid-provider request changed.

- Evidence: `D:/projects/go-tmp/modular-sse-debug-20261015/` (`started.json`, both client/state/logging files, `usage.json`, `logs/sub2api.log`, `completed.json`). The separate diagnostic client saves both response correlation IDs and validates complete SSE frames, completed result and natural EOF. Its hash is recorded in `started.json`; it is not the historical performance client.
- Both versions: **100/100 valid SSE responses**, exactly 100 upstream calls. Mock counters moved from `5561/2120/3440/1/0` to `5761/2120/3640/1/0` (total/HTTP/SSE/probe/rejected); all 200 usage records are present. No requests were replayed.
- The orchestrator exited 1 only during final evidence validation: PowerShell parsed the multiline `psql json_agg` output line-by-line. Both load stages and logging restoration had already completed. Reading the existing `usage.json` with `Get-Content -Raw` verified exactly 200 records; a read-only metadata check produced `completed.json`. The syntax correction was saved, but the load script was **not rerun**.
- Final app is the exact `53fd17b5b` candidate image, healthy. PostgreSQL, Redis and mock container identities are unchanged. Existing recovery/unknown-result fixtures were untouched.

Analysis is offline, by exact request IDs, with per-request differences calculated before percentiles. DEBUG logging and the deliberately small, unwarmed sample change measurement conditions; these values cannot replace or clear the original INFO-level mixed performance result. Missing HTTP Forward start/end boundaries must remain explicit rather than be inferred from adjacent concurrent logs.

### DEBUG result and stopping point

All 200 client/usage records joined exactly, with seven unique correlated phase logs per request and zero malformed JSON logs. No outlier/startup request was removed. The offline script and detailed source-boundary explanation are retained as `correlate.mjs` / `diagnosis.md` in the evidence directory.

| DEBUG diagnostic, p95 (ms) | Baseline | Candidate |
| --- | ---: | ---: |
| Client first body | 48.869 | 41.849 |
| Forward first token | 13 | 9 |
| Audit done → account selecting | 14 | 13 |
| Account selecting → schedule decision | 23 | 23 |
| Selected → handler completed minus same-request Forward duration | 4 | 2 |

Forward TTFT p50 is 6 ms for both versions; visible account-selection wall p50 is 10 → 11 ms. The largest visible pre-Forward component is shared account selection/preparation, not evidence of candidate-only SSE buffering. The emitted schedule `latency_ms=0` is unpopulated on the legacy selection branch and must not be interpreted as zero scheduler cost. Existing logs cannot split that interval into Redis, database, scheduling or GC. The final residual also mixes account admission/release and usage submission; it is not pure queue time.

This finite DEBUG sample did not reproduce candidate-higher p95. It does **not** determine the cause of the original mixed observation or establish a technical performance pass. The approved diagnostic ended here with no extra samples, new instrumentation, production call or business-code optimization. Publication was still deferred at that diagnostic stage; the later risk disposition and separate release authorization are recorded at the top. Further performance experiments require a separately bounded scope and have not been started.
