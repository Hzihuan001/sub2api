#!/usr/bin/env python3
"""Validate the metadata used for a custom L1 release.

This is deliberately independent of the application runtime.  It catches the
most common release/merge mistakes before an image is published:

* the checked-out tree contains the declared official upstream baseline;
* the source VERSION, workflow version and custom release tag agree; and
* the Docker workflow uses that same version for the image tag and metadata.

The baseline is kept in ``docs/UPSTREAM_BASELINE`` so a future upstream merge
only needs to update one small, reviewable file.
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys
from typing import Dict, NoReturn, Optional


KEY_VALUE_RE = re.compile(r"^([A-Za-z0-9_.-]+)\s*=\s*(.*?)\s*$")
VERSION_RE = re.compile(r"^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$")
COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")


def fail(message: str) -> "NoReturn":
    print("release consistency check failed: " + message, file=sys.stderr)
    raise SystemExit(1)


def read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except OSError as exc:
        fail(f"cannot read {path}: {exc}")


def parse_key_values(path: Path) -> Dict[str, str]:
    values: Dict[str, str] = {}
    for line_number, raw_line in enumerate(read_text(path).splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        match = KEY_VALUE_RE.match(line)
        if not match:
            fail(f"invalid {path}:{line_number}; expected key=value")
        values[match.group(1)] = match.group(2)
    return values


def run_git(root: Path, *args: str) -> str:
    try:
        result = subprocess.run(
            ["git", *args],
            cwd=str(root),
            check=True,
            capture_output=True,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError) as exc:
        detail = getattr(exc, "stderr", "") or str(exc)
        fail(f"git {' '.join(args)} failed: {detail.strip()}")
    return result.stdout.strip()


def expected_version_from_tag(tag: str) -> Optional[str]:
    """Convert custom-l1-0.2.5.7 to the image version 0.2.5-l1.7."""
    if not tag:
        return None
    match = re.fullmatch(
        r"custom-l1-(\d+\.\d+\.\d+)(?:\.(\d+)|-l1\.(\d+))", tag
    )
    if not match:
        return None
    revision = match.group(2) or match.group(3)
    return f"{match.group(1)}-l1.{revision}"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", default=os.environ.get("GITHUB_REF_NAME", ""))
    parser.add_argument("--root", default=".")
    parser.add_argument("--version-file", default="backend/cmd/server/VERSION")
    parser.add_argument("--workflow-file", default=".github/workflows/custom-l1-release.yml")
    parser.add_argument("--baseline-file", default="docs/UPSTREAM_BASELINE")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    version_file = root / args.version_file
    workflow_file = root / args.workflow_file
    baseline_file = root / args.baseline_file

    baseline = parse_key_values(baseline_file)
    official_tag = baseline.get("official_tag", "")
    official_commit = baseline.get("official_commit", "")
    if not re.fullmatch(r"v\d+\.\d+\.\d+", official_tag):
        fail(f"official_tag must be a stable upstream tag, got {official_tag!r}")
    if not COMMIT_RE.fullmatch(official_commit):
        fail(f"official_commit must be a 40-character SHA, got {official_commit!r}")

    # A release checkout must contain the exact upstream tag and have it in
    # history.  This prevents publishing a custom image from an old branch.
    # ``official_commit`` may be either the peeled commit or the annotated tag
    # object recorded by the merge plan; normalize both forms before checking.
    tagged_commit = run_git(root, "rev-parse", "--verify", f"{official_tag}^{{commit}}")
    baseline_commit = run_git(root, "rev-parse", "--verify", f"{official_commit}^{{commit}}")
    if tagged_commit != baseline_commit:
        fail(
            f"{official_tag} resolves to {tagged_commit}, expected baseline "
            f"{official_commit} -> {baseline_commit}"
        )
    head = run_git(root, "rev-parse", "HEAD")
    try:
        subprocess.run(
            ["git", "merge-base", "--is-ancestor", baseline_commit, head],
            cwd=str(root),
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError:
        fail(f"HEAD {head} does not contain official baseline {official_tag} ({official_commit})")

    source_version = read_text(version_file).strip()
    if not VERSION_RE.fullmatch(source_version):
        fail(f"{args.version_file} contains invalid version {source_version!r}")

    workflow = read_text(workflow_file)
    workflow_match = re.search(r"(?m)^\s*CUSTOM_VERSION:\s*([^\s#]+)\s*$", workflow)
    if not workflow_match:
        fail(f"CUSTOM_VERSION is missing from {args.workflow_file}")
    workflow_version = workflow_match.group(1)
    if workflow_version != source_version:
        fail(
            f"source VERSION {source_version!r} differs from workflow CUSTOM_VERSION "
            f"{workflow_version!r}"
        )

    required_workflow_fragments = (
        "tags: ghcr.io/hzihuan001/sub2api:${{ env.CUSTOM_VERSION }}",
        "VERSION=${{ env.CUSTOM_VERSION }}",
        "org.opencontainers.image.version=${{ env.CUSTOM_VERSION }}",
    )
    for fragment in required_workflow_fragments:
        if fragment not in workflow:
            fail(f"workflow does not use CUSTOM_VERSION consistently: missing {fragment!r}")

    expected_from_tag = expected_version_from_tag(args.tag)
    if expected_from_tag and expected_from_tag != source_version:
        fail(f"release tag {args.tag!r} maps to {expected_from_tag!r}, not {source_version!r}")
    if args.tag and args.tag.startswith("custom-l1-") and not expected_from_tag:
        fail(f"invalid custom L1 release tag format: {args.tag!r}")

    print(
        "release consistency check passed: "
        f"baseline={official_tag}@{official_commit[:12]} "
        f"version={source_version} tag={args.tag or '<working tree>'}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
