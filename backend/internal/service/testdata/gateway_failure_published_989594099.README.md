# Published gateway failure baseline

`gateway_failure_published_989594099.json` contains observations from commit
`989594099c1bd10db911c737449d15d2bb6149ed`, the published `.5` baseline. It is not
generated from `decideOpenAIGatewayFailure` or `decideOpenAIFailureReplay`.

The same `gateway_failure_published_baseline_test.go` source is used to capture
the old wrappers and to compare the current wrappers. Capture verifies SHA-256
fingerprints of the old classifier and HTTP/passthrough implementations and
requires `gateway_failure_decision.go` to be absent. Ordinary test execution
only reads the frozen JSON; it never updates expectations.

## Source verification and capture

Captured on 2026-10-15 with Go 1.27.2 windows/amd64. The existing source export
`D:/projects/go-tmp/cancel-accounting-baseline-989594099-46dd295b/backend` was
verified against `git ls-tree -r 989594099 backend`: all 1,452 non-test Go files
matched their Git blob hashes. No additional runtime Go files were present.
`go.mod` and `go.sum` differed only in line endings; normalizing through Git's
path filters yielded the exact baseline blobs
`e8af7899206ac3f64af86fd769336390cb661e66` and
`c29ade22438b97c863e6949bb90ec183e1e3609a` respectively.

An external Go overlay maps only the export's nonexistent
`internal/service/gateway_failure_published_baseline_test.go` to this test file.
No baseline runtime or checkout files were modified. The separate
`D:/projects/sub2api-baseline-check` checkout was not used because it was at
`4cbcaafef`, not the required published commit.

From the verified export's `backend` directory, the exact capture command was:

```powershell
$env:GOCACHE='D:/projects/go-cache'
$env:GOTMPDIR='D:/projects/go-tmp'
$env:GOTOOLCHAIN='local'
$env:GOPROXY='off'
$env:GOSUMDB='off'
$env:SUB2API_CAPTURE_PUBLISHED_FAILURES='989594099c1bd10db911c737449d15d2bb6149ed'
& 'C:/Users/a/AppData/Local/Temp/codex-go1.27.2-runtime/go/bin/go.exe' test -overlay=D:/projects/go-tmp/gateway-published-baseline-capture-overlay.json -tags=unit ./internal/service -run '^TestGatewayFailurePublishedBaselineCapture$' -count=1 -v
```

The fixture is the JSON record following `PUBLISHED_FAILURE_BASELINE_JSON=` in
that successful test's output, reformatted for review without changing values.
For another machine, export the same commit and adapt only the overlay and
runtime paths. Never refresh this fixture from the current checkout.

The representative inputs cover request rejection, credential/access state,
quota, missing model, overload, rate limit, body/context limits, malformed and
unknown errors, and gateway/edge statuses. Each records classification,
`response.failed` and error-event semantic status/failover, eight account
contexts (HTTP with and without a repository, plus passthrough), and replay
metadata with the same-account flag both false and true. Next-account actions
retain their baseline numeric enum values. These fixtures do not validate
runtime cooldown writes, retry timing, or already-committed output; those remain
the responsibility of the existing attempt/lifecycle tests.
