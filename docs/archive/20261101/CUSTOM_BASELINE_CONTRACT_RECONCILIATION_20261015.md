# Local Baseline Contract Reconciliation, 2026-10-15

Workspace: `C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom`.

This work edits eight test files only. No runtime, historical SQL, batch recovery,
WS identity implementation, commit, push, deployment, or business workflow change
was made by this task. Other agents' shared-worktree edits are not part of this
test-only change set.

## Baseline and Evidence

The original full unit baseline was not green. Its 14 top-level failures are in
`D:/projects/go-tmp/modular-full-unit-20261015.jsonl`; baseline reproduction is in
`D:/projects/go-tmp/modular-baseline-failures-20261015.jsonl`.

The released `.5` runtime is commit
`00e8f24a40eba481d1e61cfebb5f4ab3f9401a2b`, tag `custom-0.2.16.5`.
`docs/GATEWAY_RESILIENCE_V0_2_16_5_ACCEPTANCE.md` specifies cross-transport request
rejection precedence, no replay after output, credential ownership, and OAuth
5xx failover. Published source was checked as well as current modularized source.
The 245 constraint repair predates `.5` (`da5583598`) and is inherited by it.

## Expectation Changes

| Original failing contract | Correct deployed behavior and retained protection |
| --- | --- |
| Astra first ordinary `forbidden` expected two attempts and success; both ordinary `forbidden` expected two attempts | Ordinary forbidden is request-scoped, not credential evidence. Both standby-success and standby-rejection cases now require exactly account 1 only, original pro/max/model wire payload, masked 502/upstream_error, exact safe message, and no unused success response. Separate invalid_api_key-success test still requires both distinct accounts; structured workspace_suspended exhaustion still requires both, safe 502, exact safe message, and no upstream detail leakage. |
| Later migration may never restore dropped platform CHECKs | Deployed 245 deliberately restores four CHECKs with all 12 custom platforms. Test validates every later replacement is a parseable CHECK IN for the proper column and exactly that complete set. Existing 242-drop assertions and exact 245 repair test remain; no SQL was edited. |
| Generic stream nested status 500 expected fallback 502 | Explicit 5xx is preserved: 500 remains 500. Classification remains unknown, not credential; pre-output generic failure eligibility is checked for both terminal shapes. |
| Two explicit credential signals wrapped in 403 expected semantic 403 | invalid_api_key/authentication_error belongs to credential semantic 401. Ordinary permission and structured workspace state remain 403; their opposite replay decisions remain asserted. |
| Credential side effects wrapped in 403 expected semantic 403 | Semantic 401 is expected while disabled=true, exactly one SetError, and runtime account block are preserved. |
| Structured invalid_type under HTTP 401/403/429 expected failover | Concrete parameter rejection overrides misleading HTTP wrapper: no payload fallback/account replay. Exactly one request, no failover error, written response, exact upstream HTTP status and nonempty client error are asserted. |
| Generic bare server_error before Anthropic-compatible output expected client-visible failure immediately | `.5` permits next-account failover before output. Assert typed 502, retained diagnostic body, next-account retry, no downstream commit/body, and exactly one current-account send. Adjacent after-output test still requires visible error and partial output without false success. |
| OAuth passthrough 502/503/504 expected no failover | All three permit next-account failover. Assert exact status, next-account action, no downstream commit, exactly one send, no same-account retry flag/deadline/delay, unchanged rate-limit/overload side-effect expectations and ops status. |
| WS cyber_policy wrapped as rate_limit_error expected failover | Concrete safety rejection dominates rate limit. Require no failover, unchanged client error frame, safety code/mark and token usage. Both error and response.failed shapes remain covered. |
| Pool token_revoked expected account-state skip | `.5` explicitly excludes credential failures from pool skip. Require exactly one SetError and runtime block; unrelated bare401 pool skip, model/global cooldown scopes, and OAuth refresh cases remain checked. |

These cover the 11 stale top-level failures. Three PgDumper failures were a
Windows test environment issue: `sh` was absent. Adding `G:/PC/Git/bin` to PATH
makes those three tests pass without editing repository tests or runtime.

## Targeted Verification

Only the original failure families and adjacent request-safety, post-output,
credential response, semantic status, retry-window, migration, and lock-lifecycle
tests were run. No full suite was rerun.

Final result: 4 packages pass; 55 top-level tests pass; 239 pass events including
subtests; zero failed events. Package elapsed values: service 0.567s, handler
0.456s, migrations 0.091s, repository 0.741s. Full JSON event evidence:
`D:/projects/go-tmp/modular-contract-targeted-final-20261015.jsonl`.
`gofmt` and `git diff --check` passed for this test-only change set.

The exact selection and environment are reproducible with:

```powershell
& 'D:/projects/go-tmp/run-modular-contract-targeted-20261015.ps1'
```

The script writes a separate rerun log, preserving the final evidence above.

## Separate Existing Defect, Not Accepted as a Contract

Generic OpenAI invalid_api_key exhaustion is typed as
`openai_upstream_credential` with safe OpenAI status 502/message by the service,
but `credentialFailoverClientResponse` only honors the distinct OpenAI access-state
reason. Generic OpenAI credential failure falls through to Grok's fixed response:
503 and `No healthy Grok OAuth account is currently available`.

The same service reason/client metadata and handler fallback are present in the
published `.5` source, so this is not attributed to current modularization.
Runtime was not changed, and a passing gate was not made to accept that response.

Current references:

- `backend/internal/service/gateway_failure_attempt.go:52` sets generic credential metadata.
- `backend/internal/handler/openai_gateway_handler.go:3598` invokes the shared renderer.
- `backend/internal/handler/openai_gateway_handler.go:3657` defines it; line 3668 is the Grok fallback.
- `backend/internal/service/grok_credential_failure.go:24` defines the provider-specific message.

An independent overlay-only regression checks two distinct real handler attempts,
unchanged Astra wire shape, exact safe status/message, no secret leak, and no
wrong provider name. It intentionally fails on the existing defect and is not
part of the formal passing selection. No repro test exists inside the checkout.

```powershell
$env:GOCACHE = 'D:/projects/go-cache'
$env:GOTMPDIR = 'D:/projects/go-tmp'
$env:PATH = 'C:/Users/a/AppData/Local/Temp/codex-go1.27.2-runtime/go/bin;G:/PC/Git/bin;' + $env:PATH
Set-Location -LiteralPath 'C:/Users/a/.codex/worktrees/upgrade-v0-2-16-image-studio/sub2api-custom/backend'
& 'C:/Users/a/AppData/Local/Temp/codex-go1.27.2-runtime/go/bin/go.exe' test -overlay D:/projects/go-tmp/modular-generic-credential-exhaustion-overlay.json -tags=unit ./internal/handler -run '^TestReproOpenAIGenericCredentialExhaustionKeepsTypedSafeResponse$' -count=1 -v
```

Evidence/source:

- `D:/projects/go-tmp/modular-generic-credential-exhaustion-repro-20261015.log`
- `D:/projects/go-tmp/modular-generic-credential-exhaustion-repro_test.go`
- `D:/projects/go-tmp/modular-generic-credential-exhaustion-overlay.json`

This defect remains unresolved and should not be reported as a fully green
baseline or a release-ready conclusion.
