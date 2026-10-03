# Upstream upgrade PRs

The custom integration branch is intentionally separate from the plain
upstream-tracking branch. Create it once after the isolation refactor:

```powershell
git switch --create custom/integration <tested-custom-commit>
git push --set-upstream origin custom/integration
```

`.github/workflows/upstream-sync.yml` checks the official `v*` tags weekly
and can also be started manually. It creates `upgrade/upstream-vX.Y.Z`, merges
the selected official tag, applies the ordered `patch_branches` list from
`.github/upstream-sync-manifest.yml`, runs generated-code, migration and test
checks, then opens a draft pull request. A conflict or failed check produces a
blocked draft PR with logs. The job never builds, publishes, or deploys an
application image.

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
