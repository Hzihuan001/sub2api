"""Offline execution contracts for the local upgrade baseline snapshot.

Set PWSH to a PowerShell 7 executable if it is not already on PATH.
The child fixture shadows every external command; it never invokes real Git.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest


ROOT = Path(__file__).resolve().parents[1]
PWSH = os.environ.get("PWSH") or shutil.which("pwsh")


@unittest.skipUnless(PWSH, "PowerShell 7 is required for offline sync script contracts")
class SyncUpstreamBaselineTests(unittest.TestCase):
    old = "1" * 40
    fresh = "2" * 40

    def run_scenario(self, scenario):
        result = subprocess.run(
            [PWSH, "-NoProfile", "-File", str(ROOT / "scripts/fixtures/sync-upstream-git-mock.ps1"),
             "-SyncScript", str(ROOT / "scripts/sync-upstream.ps1"), "-Scenario", scenario],
            cwd=ROOT, text=True, capture_output=True, check=True,
        )
        return json.loads(result.stdout.strip())

    def assert_frozen_base(self, result):
        self.assertIsNone(result["error"])
        calls = result["calls"]
        fetch = calls.index(["fetch", "origin", "custom/integration"])
        resolve = calls.index(["rev-parse", "origin/custom/integration"])
        self.assertLess(fetch, resolve)
        self.assertEqual(sum(c == ["fetch", "origin", "custom/integration"] for c in calls), 1)
        self.assertIn(["switch", "--create", "upgrade/offline-test", self.fresh], calls)
        self.assertIn(["merge-base", "--is-ancestor", "refs/tags/v9.9.9^{commit}", self.fresh], calls)
        self.assertIn(["diff", "--check", f"{self.fresh}..HEAD"], calls)
        self.assertIn(["range-diff", f"refs/tags/v9.9.9..{self.fresh}", "refs/tags/v9.9.9..HEAD"], calls)
        isolation = next(c for c in calls if c[0] == "python" and "--base-ref" in c)
        self.assertEqual(isolation[isolation.index("--base-ref") + 1], self.fresh)
        report = "\n".join(result["output"])
        self.assertIn(f"- Base: `custom/integration` ({self.fresh})", report)
        self.assertNotIn(self.old, report)

    def test_apply_fetches_before_reporting_and_creating_from_one_frozen_base(self):
        self.assert_frozen_base(self.run_scenario("apply"))

    def test_later_tracking_ref_change_cannot_change_the_frozen_start_commit(self):
        self.assert_frozen_base(self.run_scenario("advanced-ref"))

    def test_dry_run_keeps_original_metadata_only_behavior(self):
        result = self.run_scenario("dry-run")
        self.assertIsNone(result["error"])
        self.assertNotIn(["fetch", "origin", "custom/integration"], result["calls"])
        self.assertFalse(any(c[0] in {"switch", "merge", "commit", "push", "python"} for c in result["calls"]))
        self.assertIn(["merge-base", "--is-ancestor", "refs/tags/v9.9.9^{commit}", self.old], result["calls"])
        self.assertIn(f"({self.old})", "\n".join(result["output"]))

    def test_fetched_current_base_exits_without_creating_an_upgrade(self):
        result = self.run_scenario("already-current")
        self.assertIsNone(result["error"])
        self.assertIn(["fetch", "origin", "custom/integration"], result["calls"])
        self.assertFalse(any(c[0] in {"switch", "merge", "commit", "push", "python"} for c in result["calls"]))
        self.assertIn("No update:", "\n".join(result["output"]))

    def test_origin_fetch_failure_stops_before_baseline_resolution_or_mutation(self):
        result = self.run_scenario("fetch-failure")
        self.assertIn("fetch origin custom/integration failed", result["error"])
        self.assertNotIn(["rev-parse", "origin/custom/integration"], result["calls"])
        self.assertFalse(any(c[0] in {"switch", "merge", "commit", "push", "python"} for c in result["calls"]))


if __name__ == "__main__":
    unittest.main()
