# Upstream upgrade PRs

The custom integration branch is intentionally separate from the plain
upstream-tracking branch. Create it once after the isolation refactor:

```powershell
git switch --create custom/integration <tested-custom-commit>
git push --set-upstream origin custom/integration
```

The canonical upstream mirror ref recorded in `custom/manifest.yml` is
`upstream/main`. The official remote currently exposes `main` (not
`mainline`); keep this metadata aligned if the upstream repository changes its
default branch. Release synchronization still selects immutable `vX.Y.Z` tags.

`.github/workflows/upstream-sync.yml` checks the official `v*` tags weekly
and can also be started manually. It creates `upgrade/upstream-vX.Y.Z`, merges
the selected official tag, applies the ordered `patch_branches` list from
`.github/upstream-sync-manifest.yml`, runs generated-code, migration and test
checks, then opens a draft pull request. A conflict or failed check produces a
blocked draft PR with logs. The job never builds, publishes, or deploys an
application image.

The workflow uses read-only token permissions by default and grants write
access only to the sync job that publishes the upgrade branch and draft PR.
It pins Go from `backend/go.mod`, Node from `.nvmrc`, and pnpm 9.15.9 before
running generation, backend tests, frontend typecheck, and lint. Git merge and
fetch failures are handled explicitly so an unrelated command failure cannot
be silently treated as a successful upgrade.

The default tag lookup selects only stable `vX.Y.Z` tags. A pre-release may be
selected only by supplying `upstream_tag` (or `-UpstreamTag`) explicitly. The
upstream merge is committed before the first custom patch, and every patch is
merged and committed independently. This keeps `MERGE_HEAD` out of the next
merge and makes each patch boundary visible in the draft PR history. Keep
`patch_branches` empty until a feature branch is recreated from
`custom/integration`, contains only that feature's commits, and has passed its
own Docker/CI acceptance; old historical feature branches must not be reused.

For an offline or PowerShell-driven run, the default is a metadata-only dry
run:

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1
```

Only an explicitly requested local upgrade creates a branch. Keep the working
tree clean and review the report before pushing:

```powershell
pwsh -NoProfile -File scripts/sync-upstream.ps1 `
  -UpstreamTag vX.Y.Z -Apply -RunChecks
pwsh -NoProfile -File scripts/sync-upstream.ps1 `
  -UpstreamTag vX.Y.Z -Apply -RunChecks -Push -CreatePr
```

The script has no deployment path. Production release remains a separate,
manually approved workflow.
