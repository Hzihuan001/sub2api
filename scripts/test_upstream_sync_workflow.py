"""Bounded, offline execution of the real upstream-sync workflow Bash blocks.

Git and gh are shell mocks, and PATH is emptied before any workflow code runs.
Only explicit awk/sed/cp wrappers can execute local utilities. No repository,
credentials, network, remote branches, or pull requests are touched.
"""
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github/workflows/upstream-sync.yml"


def find_bash():
    candidates = [os.environ.get("BASH")]
    if os.name == "nt":
        candidates += ["G:/PC/Git/bin/bash.exe", "C:/Program Files/Git/bin/bash.exe"]
    candidates += [shutil.which("bash")]
    return next((candidate for candidate in candidates if candidate and Path(candidate).is_file()), None)


BASH = find_bash()


def extract_step(name):
    """Read this workflow's literal Bash block without requiring a YAML package.

    Indentation and block style are checked rather than guessed. A changed YAML
    shape fails the test instead of silently running a copied or empty script.
    """
    lines = WORKFLOW.read_text(encoding="utf-8").splitlines()
    starts = [i for i, line in enumerate(lines) if line == f"      - name: {name}"]
    if len(starts) != 1:
        raise AssertionError(f"Expected one workflow step named {name!r}")
    start = starts[0] + 1
    end = next((i for i in range(start, len(lines)) if lines[i].startswith("      - ")), len(lines))
    block = lines[start:end]
    if "        shell: bash" not in block:
        raise AssertionError(f"{name}: expected Bash shell")
    run = [i for i, line in enumerate(block) if line == "        run: |"]
    if len(run) != 1:
        raise AssertionError(f"{name}: expected one literal run block")
    script_lines = block[run[0] + 1:]
    if any(line and not line.startswith("          ") for line in script_lines):
        raise AssertionError(f"{name}: unsupported run block indentation")
    script = "\n".join(line[10:] if line else "" for line in script_lines).strip() + "\n"
    if not script.strip():
        raise AssertionError(f"{name}: empty run block")
    env = {}
    if "        env:" in block:
        for line in block[block.index("        env:") + 1:]:
            match = re.fullmatch(r"          ([A-Z_][A-Z_0-9]*):\s*(.*)", line)
            if not match:
                break
            env[match[1]] = match[2]
    condition = next((line.removeprefix("        if: ") for line in block if line.startswith("        if: ")), "")
    return script, env, condition


def render_expressions(value, context):
    def replace(match):
        key = match.group(1).strip()
        if key not in context:
            raise AssertionError(f"Workflow uses an unprovided fixture expression: {key}")
        return context[key]
    return re.sub(r"\$\{\{\s*(.*?)\s*\}\}", replace, value)


MOCKS = r'''
set -euo pipefail
# Resolve only these harmless utilities before removing every executable from
# PATH. Thus command git, an accidental markdown substitution, or an unexpected
# new tool can never fall through to a real Git/gh/network binary.
offline_awk="$(command -v awk)"
offline_sed="$(command -v sed)"
offline_cp="$(command -v cp)"
export PATH=/__upstream_workflow_offline_no_executables__
awk() { "$offline_awk" "$@"; }
sed() { "$offline_sed" "$@"; }
cp() { "$offline_cp" "$@"; }
command_not_found_handle() {
  printf '%s\n' "$*" >> unexpected.log
  return 127
}
pending_merge=0
configured_name=''
configured_email=''
git() {
  printf 'git' >> calls.log
  printf '\t%s' "$@" >> calls.log
  printf '\n' >> calls.log
  case "$1" in
    config)
      case "$2" in
        user.name) configured_name="$3" ;;
        user.email) configured_email="$3" ;;
        *) printf 'Unexpected git config: %s\n' "$*" >> unexpected.log; return 99 ;;
      esac
      ;;
    remote) [[ "$2" == get-url || "$2" == add ]] || return 99 ;;
    fetch)
      if [[ "${4:-}" == "$FAIL_PATCH_FETCH" && -n "$FAIL_PATCH_FETCH" ]]; then
        printf 'offline simulated patch fetch failure\n'; return 1
      fi
      ;;
    ls-remote) return 1 ;;
    switch) [[ "$FAIL_SWITCH" == 0 ]] || return 79 ;;
    merge-base) [[ "$NOOP" == 1 ]] ;;
    merge)
      if [[ "$2" == --abort ]]; then pending_merge=0; return 0; fi
      if [[ "$pending_merge" != 0 ]]; then
        printf 'Nested merge before previous commit\n' >> unexpected.log; return 75
      fi
      if [[ "${4:-}" == "$CONTAINED_PATCH_TARGET" && -n "$CONTAINED_PATCH_TARGET" ]]; then
        printf 'Already up to date.\n'; return 0
      fi
      pending_merge=1
      if [[ "${4:-}" == "$FAIL_MERGE_TARGET" ]]; then
        printf 'offline simulated merge conflict\n'; return 1
      fi
      ;;
    rev-parse)
      [[ "$*" == 'rev-parse -q --verify MERGE_HEAD' ]] || return 99
      [[ "$pending_merge" == 1 ]]
      ;;
    diff)
      if [[ "$2" == --name-only ]]; then printf 'frontend/src/example.vue\n'; fi
      if [[ "$2" == --check && "$FAIL_CHECK" == 1 ]]; then return 1; fi
      ;;
    range-diff) printf 'offline range diff\n' ;;
    add) return 0 ;;
    commit)
      if [[ -z "$configured_name" || -z "$configured_email" ]]; then
        printf 'Commit attempted without configured bot identity\n' >> unexpected.log; return 78
      fi
      [[ "$FAIL_COMMIT" == 0 ]] || return 86
      pending_merge=0
      ;;
    push) return 0 ;;
    *) printf 'Unexpected git: %s\n' "$*" >> unexpected.log; return 99 ;;
  esac
}
gh() {
  [[ "$1 $2" == 'pr create' ]] || { printf 'Unexpected gh: %s\n' "$*" >> unexpected.log; return 99; }
  printf '%s\0' "$@" > gh-args.bin
}
'''


@unittest.skipUnless(BASH, "Bash is required for offline upstream workflow contracts")
class UpstreamSyncWorkflowTests(unittest.TestCase):
    tag = "v9.9.9"
    sha = "a" * 40
    merge_step = "Create upgrade branch and merge upstream"
    check_step = "Run range-diff and source checks"
    pr_step = "Create upgrade PR"

    def run_steps(self, names, *, context=None, flags=None, patches=()):
        values = {
            "github.token": "offline-not-a-token",
            "steps.upstream.outputs.tag": self.tag,
            "steps.upstream.outputs.sha": self.sha,
            "steps.merge.outputs.branch": f"upgrade/upstream-{self.tag}",
            "steps.merge.outputs.blocked": "false",
            "steps.checks.outputs.failed": "false",
            "steps.merge.outcome": "success",
            "steps.checks.outcome": "success",
        }
        values.update(context or {})
        env = {
            "BASE_BRANCH": "custom/integration", "UPSTREAM_REMOTE": "upstream",
            "MANIFEST": "manifest.yml", "GITHUB_OUTPUT": "outputs.env", "GITHUB_RUN_ID": "123",
            "FAIL_SWITCH": "0", "FAIL_COMMIT": "0", "FAIL_CHECK": "0", "NOOP": "0",
            "FAIL_MERGE_TARGET": "", "FAIL_PATCH_FETCH": "", "CONTAINED_PATCH_TARGET": "",
        }
        env.update(flags or {})
        with tempfile.TemporaryDirectory(prefix="upstream-workflow-offline-") as directory:
            root = Path(directory)
            (root / "manifest.yml").write_text("patch_branches:\n" + "".join(f"  - {patch}\n" for patch in patches), encoding="utf-8")
            harness = MOCKS + "\n"
            harness += "\n".join(f"export {key}={shlex.quote(value)}" for key, value in env.items()) + "\n"
            for index, name in enumerate(names):
                source, step_env, _ = extract_step(name)
                source = render_expressions(source, values)
                # A hardcoded Git/gh executable must not bypass the mocks.
                if re.search(r'''(?:^|\s)["']?(?:[A-Za-z]:)?/[^\s"']*/(?:git|gh)(?:\.exe)?(?:["']|\s|$)''', source):
                    raise AssertionError(f"{name}: unexpected absolute Git/gh executable")
                (root / f"step-{index}.sh").write_text(source, encoding="utf-8", newline="\n")
                harness += "\n".join(f"export {key}={shlex.quote(render_expressions(value, values))}" for key, value in step_env.items()) + "\n"
                harness += f"source ./step-{index}.sh\n"
            (root / "harness.sh").write_text(harness, encoding="utf-8", newline="\n")
            process_env = os.environ.copy()
            # Do not inherit shell startup hooks from the caller.
            process_env.pop("BASH_ENV", None)
            process_env.pop("ENV", None)
            result = subprocess.run([BASH, "--noprofile", "--norc", "harness.sh"], cwd=root,
                                    env=process_env, capture_output=True, text=True, timeout=15)
            def read(name):
                path = root / name
                return path.read_text(encoding="utf-8") if path.exists() else ""
            outputs = dict(line.split("=", 1) for line in read("outputs.env").splitlines() if "=" in line)
            args_file = root / "gh-args.bin"
            args = args_file.read_bytes().decode("utf-8").split("\0")[:-1] if args_file.exists() else []
            return {"process": result, "outputs": outputs, "args": args,
                    "calls": [line.split("\t") for line in read("calls.log").splitlines()],
                    "unexpected": read("unexpected.log"), "report": read("sync-report.md"),
                    "files": sorted(path.name for path in root.iterdir())}

    def assert_clean(self, result):
        self.assertEqual(result["unexpected"], "")
        self.assertEqual(result["process"].returncode, 0, result["process"].stderr)

    def pr_argument(self, result, name):
        self.assertIn(name, result["args"])
        return result["args"][result["args"].index(name) + 1]

    def assert_blocked_draft(self, result):
        self.assert_clean(result)
        self.assertIn("--draft", result["args"])
        self.assertEqual(self.pr_argument(result, "--title"), f"[blocked] upstream sync {self.tag}")
        self.assertIn("`sync-report.md`", self.pr_argument(result, "--body"))

    def test_successful_pr_keeps_literal_markdown_tag_and_is_a_draft(self):
        result = self.run_steps([self.pr_step])
        self.assert_clean(result)
        self.assertEqual(self.pr_argument(result, "--title"), f"chore: sync upstream {self.tag}")
        self.assertIn(f"`{self.tag}`", self.pr_argument(result, "--body"))
        self.assertIn("--draft", result["args"])
        self.assertEqual(self.pr_argument(result, "--base"), "custom/integration")
        self.assertIn(["git", "push", "--set-upstream", "origin", f"upgrade/upstream-{self.tag}"], result["calls"])

    def test_failed_missing_or_non_success_results_can_never_get_a_success_title(self):
        for expression, value in [
            ("steps.merge.outputs.blocked", "true"), ("steps.merge.outputs.blocked", ""),
            ("steps.checks.outputs.failed", "true"), ("steps.checks.outputs.failed", ""),
            ("steps.merge.outcome", "failure"), ("steps.merge.outcome", "cancelled"),
            ("steps.merge.outcome", ""), ("steps.checks.outcome", "failure"),
            ("steps.checks.outcome", "skipped"), ("steps.checks.outcome", ""),
        ]:
            with self.subTest(expression=expression, value=value):
                self.assert_blocked_draft(self.run_steps([self.pr_step], context={expression: value}))

    def test_configures_bot_identity_before_committing_the_upstream_merge(self):
        result = self.run_steps(["Configure remotes", self.merge_step])
        self.assert_clean(result)
        calls = result["calls"]
        commit = calls.index(["git", "commit", "-m", f"chore: merge upstream {self.tag}"])
        self.assertLess(calls.index(["git", "config", "user.name", "github-actions[bot]"]), commit)
        self.assertLess(calls.index(["git", "config", "user.email", "41898282+github-actions[bot]@users.noreply.github.com"]), commit)
        self.assertEqual(result["outputs"], {"noop": "false", "branch": f"upgrade/upstream-{self.tag}", "blocked": "false"})

    def test_conflict_report_preserves_backticks_without_executing_tag_sha_or_branch(self):
        result = self.run_steps(["Configure remotes", self.merge_step], flags={"FAIL_MERGE_TARGET": self.tag})
        self.assert_clean(result)
        self.assertEqual(result["outputs"]["blocked"], "true")
        self.assertIn(f"- Upstream tag: `{self.tag}` (`{self.sha}`)", result["report"])
        self.assertIn("- Base branch: `custom/integration`", result["report"])
        self.assertIn("`frontend/src/example.vue`", result["report"])

    def test_switch_or_commit_failure_stops_before_emitting_success(self):
        for flag in ["FAIL_SWITCH", "FAIL_COMMIT"]:
            with self.subTest(flag=flag):
                result = self.run_steps(["Configure remotes", self.merge_step], flags={flag: "1"})
                self.assertEqual(result["unexpected"], "")
                self.assertNotEqual(result["process"].returncode, 0)
                self.assertNotEqual(result["outputs"].get("blocked"), "false")
                self.assertNotIn("noop", result["outputs"])
                if flag == "FAIL_SWITCH":
                    self.assertFalse(any(call[1] in {"merge", "commit"} for call in result["calls"]))

    def test_slash_patch_branches_merge_in_order_after_the_previous_merge_commits(self):
        patches = ["custom/feature-one", "custom/feature-two"]
        result = self.run_steps(["Configure remotes", self.merge_step], patches=patches)
        self.assert_clean(result)
        self.assertEqual(result["outputs"]["blocked"], "false")
        calls = result["calls"]
        previous = calls.index(["git", "commit", "-m", f"chore: merge upstream {self.tag}"])
        for patch in patches:
            fetch = calls.index(["git", "fetch", "--prune", "origin", patch])
            merge = calls.index(["git", "merge", "--no-ff", "--no-commit", f"origin/{patch}"])
            commit = calls.index(["git", "commit", "-m", f"chore: merge custom patch {patch}"])
            self.assertLess(previous, fetch)
            self.assertLess(fetch, merge)
            self.assertLess(merge, commit)
            previous = commit
        self.assertIn("patch-fetch.log", result["files"])

    def test_missing_slash_patch_produces_a_blocked_report(self):
        patch = "custom/missing-patch"
        result = self.run_steps(["Configure remotes", self.merge_step], patches=[patch], flags={"FAIL_PATCH_FETCH": patch})
        self.assert_clean(result)
        self.assertEqual(result["outputs"]["blocked"], "true")
        self.assertIn(f"patch branch not found: {patch}", result["report"])

    def test_already_contained_patch_skips_empty_commit_and_continues_to_next_patch(self):
        contained, next_patch = "custom/already-contained", "custom/next-patch"
        result = self.run_steps(["Configure remotes", self.merge_step], patches=[contained, next_patch],
                                flags={"CONTAINED_PATCH_TARGET": f"origin/{contained}"})
        self.assert_clean(result)
        self.assertEqual(result["outputs"], {"noop": "false", "branch": f"upgrade/upstream-{self.tag}", "blocked": "false"})
        calls = result["calls"]
        contained_merge = calls.index(["git", "merge", "--no-ff", "--no-commit", f"origin/{contained}"])
        next_fetch = calls.index(["git", "fetch", "--prune", "origin", next_patch])
        self.assertLess(contained_merge, next_fetch)
        self.assertIn(["git", "rev-parse", "-q", "--verify", "MERGE_HEAD"], calls[contained_merge + 1:next_fetch])
        self.assertEqual([call for call in calls if call[1] == "commit"], [
            ["git", "commit", "-m", f"chore: merge upstream {self.tag}"],
            ["git", "commit", "-m", f"chore: merge custom patch {next_patch}"],
        ])

    def test_patch_conflict_report_keeps_both_patch_and_tag_literals(self):
        patch = "custom/conflicting-patch"
        result = self.run_steps(["Configure remotes", self.merge_step], patches=[patch], flags={"FAIL_MERGE_TARGET": f"origin/{patch}"})
        self.assert_clean(result)
        self.assertEqual(result["outputs"]["blocked"], "true")
        self.assertIn(f"Patch branch `{patch}` conflicted after `{self.tag}`.", result["report"])

    def test_noop_skips_merge_commit_and_is_excluded_from_pr_creation(self):
        result = self.run_steps(["Configure remotes", self.merge_step], flags={"NOOP": "1"})
        self.assert_clean(result)
        self.assertEqual(result["outputs"]["noop"], "true")
        self.assertFalse(any(call[1] in {"merge", "commit", "push"} for call in result["calls"]))
        self.assertIn("steps.merge.outputs.noop != 'true'", extract_step(self.pr_step)[2])

    def test_a_real_failed_check_output_is_consumed_as_a_blocked_draft(self):
        checks = self.run_steps(["Configure remotes", self.check_step], flags={"FAIL_CHECK": "1"})
        self.assert_clean(checks)
        self.assertEqual(checks["outputs"].get("failed"), "true")
        self.assert_blocked_draft(self.run_steps([self.pr_step], context={"steps.checks.outputs.failed": checks["outputs"]["failed"]}))


if __name__ == "__main__":
    unittest.main()
