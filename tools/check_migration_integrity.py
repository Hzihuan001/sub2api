#!/usr/bin/env python3
"""Protect applied SQL migrations during upstream merges and releases.

The migration runner records checksums, so changing or removing an already
deployed migration is unsafe even when the SQL remains idempotent.  This gate
compares the current migration tree with the frozen custom baseline and also
rejects newly introduced numeric-prefix collisions.  The baseline itself
contains a few historical collisions (for example ``006`` and ``006b``); those
are reported as legacy and are not re-created by this check.
"""

from __future__ import annotations

import argparse
import hashlib
from pathlib import Path
import re
import subprocess
import sys
from collections import defaultdict
from typing import Iterable, NoReturn


KEY_VALUE_RE = re.compile(r"^([A-Za-z0-9_.-]+)\s*=\s*(.*?)\s*$")
MIGRATION_NAME_RE = re.compile(r"^(?P<number>\d+)(?P<suffix>[a-z]*)_(?P<name>.+)\.sql$")


def fail(message: str) -> NoReturn:
    print(f"migration integrity check failed: {message}", file=sys.stderr)
    raise SystemExit(1)


def run_git(root: Path, *args: str) -> str:
    try:
        result = subprocess.run(
            ["git", *args],
            cwd=root,
            check=True,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = getattr(exc, "stderr", "") or str(exc)
        fail(f"git {' '.join(args)} failed: {detail.strip()}")
    return result.stdout.strip()


def run_git_bytes(root: Path, *args: str) -> bytes:
    """Run git without decoding so migration checksums use exact bytes."""
    try:
        result = subprocess.run(
            ["git", *args],
            cwd=root,
            check=True,
            capture_output=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = getattr(exc, "stderr", b"") or str(exc)
        if isinstance(detail, bytes):
            detail = detail.decode("utf-8", errors="replace")
        fail(f"git {' '.join(args)} failed: {detail.strip()}")
    return result.stdout


def parse_key_values(path: Path) -> dict[str, str]:
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        fail(f"cannot read {path}: {exc}")
    values: dict[str, str] = {}
    for line_number, raw_line in enumerate(lines, 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        match = KEY_VALUE_RE.fullmatch(line)
        if not match:
            fail(f"invalid {path}:{line_number}; expected key=value")
        values[match.group(1)] = match.group(2)
    return values


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def baseline_files(root: Path, commit: str, directory: str) -> list[str]:
    prefix = directory.rstrip("/") + "/"
    output = run_git(root, "ls-tree", "-r", "--name-only", commit, "--", directory)
    return sorted(
        path
        for path in output.splitlines()
        if path.startswith(prefix) and path.endswith(".sql")
    )


def current_files(root: Path, directory: str) -> dict[str, Path]:
    path = root / directory
    if not path.is_dir():
        fail(f"migration directory does not exist: {path}")
    prefix = directory.rstrip("/") + "/"
    return {
        f"{prefix}{file.name}": file
        for file in path.glob("*.sql")
        if file.is_file()
    }


def migration_prefix(path: str) -> str:
    name = Path(path).name
    match = MIGRATION_NAME_RE.fullmatch(name)
    if not match:
        fail(
            f"invalid migration filename {name!r}; use NNN_description.sql "
            "(a letter suffix is allowed only for a documented legacy file)"
        )
    # Numeric prefix is intentionally normalized: 006, 006a and 006b all use
    # the same migration slot and must not be newly combined by an upgrade.
    return match.group("number")


def grouped_prefixes(paths: Iterable[str]) -> dict[str, list[str]]:
    groups: dict[str, list[str]] = defaultdict(list)
    for path in paths:
        groups[migration_prefix(path)].append(Path(path).name)
    return {prefix: sorted(names) for prefix, names in groups.items()}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=".")
    parser.add_argument("--baseline-file", default="docs/MIGRATION_BASELINE")
    parser.add_argument(
        "--migration-dir",
        default=None,
        help="override the directory from the baseline file (useful for tests)",
    )
    args = parser.parse_args()

    root = Path(args.root).resolve()
    baseline = parse_key_values(root / args.baseline_file)
    commit = baseline.get("custom_baseline_commit", "")
    directory = args.migration_dir or baseline.get("migration_directory", "")
    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        fail("custom_baseline_commit must be a 40-character commit SHA")
    if not directory or directory.startswith("/") or ".." in Path(directory).parts:
        fail(f"invalid migration_directory {directory!r}")

    baseline_commit = run_git(root, "rev-parse", "--verify", f"{commit}^{{commit}}")
    baseline_paths = baseline_files(root, baseline_commit, directory)
    if not baseline_paths:
        fail(
            f"baseline {baseline_commit} contains no SQL migrations under {directory!r}; "
            "check the baseline commit and migration_directory"
        )
    current = current_files(root, directory)
    current_paths = sorted(current)

    missing = sorted(set(baseline_paths) - set(current_paths))
    if missing:
        fail(
            "applied migrations were removed: "
            + ", ".join(Path(path).name for path in missing)
            + "; restore them and add a new migration for corrections"
        )

    for path in baseline_paths:
        old = run_git_bytes(root, "show", f"{baseline_commit}:{path}")
        try:
            new = current[path].read_bytes()
        except OSError as exc:
            fail(f"cannot read {root / path}: {exc}")
        old_hash = sha256(old)
        new_hash = sha256(new)
        if old_hash != new_hash:
            fail(
                f"applied migration {Path(path).name} was modified "
                f"(baseline {old_hash[:12]}, current {new_hash[:12]}); "
                "restore the file and create a new migration instead"
            )

    baseline_groups = grouped_prefixes(baseline_paths)
    current_groups = grouped_prefixes(current_paths)
    new_collisions: list[str] = []
    for prefix, names in sorted(current_groups.items()):
        baseline_count = len(baseline_groups.get(prefix, []))
        if len(names) > max(1, baseline_count) and len(names) > baseline_count:
            new_names = sorted(set(names) - set(baseline_groups.get(prefix, [])))
            new_collisions.append(
                f"{prefix}: {', '.join(names)} (new: {', '.join(new_names) or '<unknown>'})"
            )
    if new_collisions:
        fail(
            "new duplicate migration prefix detected: "
            + "; ".join(new_collisions)
            + "; choose an unused numeric prefix (do not rename an applied migration)"
        )

    legacy_collisions = {
        prefix: names
        for prefix, names in current_groups.items()
        if len(names) > 1 and len(baseline_groups.get(prefix, [])) > 1
    }
    legacy_note = ", ".join(sorted(legacy_collisions)) if legacy_collisions else "none"
    added = sorted(set(current_paths) - set(baseline_paths))
    print(
        "migration integrity check passed: "
        f"baseline={baseline_commit[:12]} frozen={len(baseline_paths)} "
        f"added={len(added)} legacy_duplicate_prefixes={legacy_note}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
