#!/usr/bin/env python3
"""Check invariants that keep the custom layer easy to rebase.

This is intentionally a dependency-free, read-only policy check apart from
the optional ``go generate`` verification.  It is used by the upstream sync
workflow after the official merge and before an upgrade PR is opened.

The check enforces four rules:

* a clean checkout must remain clean after ``go generate ./...``;
* already-published migration files are append-only;
* a new custom migration uses the ``9000_custom_*.sql`` namespace (official
  migrations present in the selected upstream ref are allowed to keep their
  upstream name); and
* the backend feature manifest and the checked-in manifest have valid,
  unique IDs and settings namespaces.

The script does not resolve conflicts, rewrite files, or update the database.
Run it from a clean checkout.  If ``--run-go-generate`` is used, any generated
diff is left in place for inspection and causes a non-zero exit status.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from pathlib import Path
from typing import Iterable


FEATURE_ID_RE = re.compile(r"^[a-z][a-z0-9]*(?:[-_][a-z0-9]+)*$")
CUSTOM_MIGRATION_RE = re.compile(r"^9000_custom_[a-z0-9][a-z0-9_-]*\.sql$")
GO_FEATURE_RE = re.compile(r"^\s*(Feature\w+)\s+FeatureID\s*=\s*\"([^\"]+)\"\s*$", re.MULTILINE)
GO_MANIFEST_RE = re.compile(
    r"ID:\s*(Feature\w+)\s*,\s*SettingsNamespace:\s*SettingNamespace\(\s*(Feature\w+)\s*\)",
)


class CheckFailure(RuntimeError):
    pass


def run_git(root: Path, *args: str, check: bool = True) -> str:
    completed = subprocess.run(
        ["git", *args],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    if check and completed.returncode != 0:
        raise CheckFailure(
            f"git {' '.join(args)} failed ({completed.returncode}): "
            f"{completed.stderr.strip()}"
        )
    return completed.stdout


def ref_exists(root: Path, ref: str) -> bool:
    return subprocess.run(
        ["git", "rev-parse", "--verify", ref],
        cwd=root,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    ).returncode == 0


def resolve_ref(root: Path, requested: str | None, fallbacks: Iterable[str]) -> str | None:
    if requested:
        if not ref_exists(root, requested):
            raise CheckFailure(f"git ref does not exist: {requested}")
        return requested
    for candidate in fallbacks:
        if ref_exists(root, candidate):
            return candidate
    return None


def repository_root() -> Path:
    try:
        return Path(run_git(Path.cwd(), "rev-parse", "--show-toplevel").strip())
    except CheckFailure as exc:
        raise CheckFailure("run this check from inside a git repository") from exc


def assert_clean_before_generation(root: Path) -> None:
    status = run_git(root, "status", "--porcelain", "--untracked-files=all")
    if status.strip():
        raise CheckFailure(
            "working tree must be clean before --run-go-generate; "
            "commit or stash local changes first:\n" + status
        )


def check_generated_sources(root: Path, run_generate: bool) -> None:
    backend = root / "backend"
    if not (backend / "go.mod").is_file():
        print("generated sources: skipped (backend/go.mod not found)")
        return

    if run_generate:
        assert_clean_before_generation(root)
        try:
            completed = subprocess.run(
                ["go", "generate", "./..."],
                cwd=backend,
                text=True,
                capture_output=True,
                check=False,
            )
        except FileNotFoundError as exc:
            raise CheckFailure(
                "go executable was not found; install the pinned Go toolchain "
                "or run with --skip-generated after a CI generation step"
            ) from exc
        if completed.returncode != 0:
            raise CheckFailure(
                "go generate ./... failed:\n"
                + (completed.stdout + completed.stderr).strip()
            )

    status = run_git(root, "status", "--porcelain", "--untracked-files=all")
    if status.strip():
        raise CheckFailure(
            "generated source check failed: go generate left working-tree changes:\n"
            + status
        )
    print("generated sources: clean")


def changed_migrations(root: Path, base_ref: str | None) -> list[tuple[str, str]]:
    if not base_ref:
        # A parent is a safe local fallback for a newly-created topic branch.
        base_ref = "HEAD^"
    output = run_git(
        root,
        "diff",
        "--name-status",
        "--no-renames",
        f"{base_ref}...HEAD",
        "--",
        "backend/migrations",
    )
    changes: list[tuple[str, str]] = []
    for line in output.splitlines():
        if not line.strip():
            continue
        status, path = line.split("\t", 1)
        changes.append((status, path))
    return changes


def path_exists_at(root: Path, ref: str | None, path: str) -> bool:
    if not ref:
        return False
    return subprocess.run(
        ["git", "cat-file", "-e", f"{ref}:{path}"],
        cwd=root,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    ).returncode == 0


def blob_at(root: Path, ref: str, path: str) -> bytes:
    completed = subprocess.run(
        ["git", "show", f"{ref}:{path}"],
        cwd=root,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        raise CheckFailure(f"cannot read {path} at {ref}: {completed.stderr.decode(errors='replace')}")
    return completed.stdout


def blob_at_head(root: Path, path: str) -> bytes:
    return blob_at(root, "HEAD", path)


def check_migrations(root: Path, base_ref: str | None, upstream_ref: str | None) -> None:
    failures: list[str] = []
    for status, path in changed_migrations(root, base_ref):
        filename = Path(path).name
        existed_in_base = bool(base_ref and path_exists_at(root, base_ref, path))
        exists_in_upstream = bool(upstream_ref and path_exists_at(root, upstream_ref, path))

        if status == "A":
            # Official migrations are allowed to retain the upstream numbering.
            if not exists_in_upstream and not CUSTOM_MIGRATION_RE.fullmatch(filename):
                failures.append(
                    f"new migration {path} is not official and must match "
                    "9000_custom_<feature>.sql"
                )
        elif status == "D":
            if existed_in_base:
                failures.append(f"published migration cannot be deleted: {path}")
        elif status == "M":
            if not existed_in_base:
                continue
            # A merge may carry an upstream edit of a file that already exists
            # locally.  It is safe only when HEAD is byte-for-byte the upstream
            # version; a custom edit to a published file is never accepted.
            if not upstream_ref or not exists_in_upstream:
                failures.append(f"published migration cannot be modified: {path}")
            elif blob_at_head(root, path) != blob_at(root, upstream_ref, path):
                failures.append(
                    f"published migration {path} differs from upstream; "
                    "append a new migration instead"
                )

    if failures:
        raise CheckFailure("migration immutability check failed:\n- " + "\n- ".join(failures))
    print("migrations: append-only policy passed")


def validate_feature_id(value: str, origin: str) -> None:
    if not FEATURE_ID_RE.fullmatch(value):
        raise CheckFailure(f"{origin}: invalid feature id {value!r}")


def load_feature_manifest(root: Path) -> list[dict[str, str]]:
    path = root / ".github" / "custom-features.json"
    if not path.is_file():
        raise CheckFailure(f"missing custom feature manifest: {path}")
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise CheckFailure(f"invalid JSON in {path}: {exc}") from exc
    features = data.get("features") if isinstance(data, dict) else None
    if not isinstance(features, list):
        raise CheckFailure(f"{path}: expected a top-level features array")
    normalized: list[dict[str, str]] = []
    seen: set[str] = set()
    namespaces: set[str] = set()
    for index, item in enumerate(features):
        if not isinstance(item, dict) or not isinstance(item.get("id"), str) or not isinstance(item.get("settingsNamespace"), str):
            raise CheckFailure(f"{path}: feature #{index + 1} must contain string id and settingsNamespace")
        feature_id = item["id"].strip()
        namespace = item["settingsNamespace"].strip()
        validate_feature_id(feature_id, f"{path} feature #{index + 1}")
        if namespace != f"custom.{feature_id}":
            raise CheckFailure(
                f"{path}: feature {feature_id!r} must use settings namespace custom.{feature_id!s}"
            )
        if feature_id in seen:
            raise CheckFailure(f"{path}: duplicate feature id {feature_id!r}")
        if namespace in namespaces:
            raise CheckFailure(f"{path}: duplicate settings namespace {namespace!r}")
        seen.add(feature_id)
        namespaces.add(namespace)
        normalized.append({"id": feature_id, "settingsNamespace": namespace})
    return sorted(normalized, key=lambda item: item["id"])


def check_feature_manifests(root: Path) -> None:
    checked_in = load_feature_manifest(root)
    source_path = root / "backend" / "internal" / "custom" / "features.go"
    if not source_path.is_file():
        raise CheckFailure(f"missing backend feature manifest source: {source_path}")
    source = source_path.read_text(encoding="utf-8")
    constants = dict(GO_FEATURE_RE.findall(source))
    if not constants:
        raise CheckFailure(f"{source_path}: no FeatureID constants found")
    for identifier, feature_id in constants.items():
        validate_feature_id(feature_id, str(source_path))

    entries = GO_MANIFEST_RE.findall(source)
    if len(entries) != len(constants):
        raise CheckFailure(
            f"{source_path}: every FeatureID must appear exactly once in BuiltInFeatureManifests "
            f"(found {len(entries)}, constants {len(constants)})"
        )
    backend_features: list[dict[str, str]] = []
    seen_ids: set[str] = set()
    for constant_name, namespace_constant in entries:
        if constant_name != namespace_constant or constant_name not in constants:
            raise CheckFailure(
                f"{source_path}: manifest entry {constant_name}/{namespace_constant} "
                "does not reference the same declared feature"
            )
        feature_id = constants[constant_name]
        if feature_id in seen_ids:
            raise CheckFailure(f"{source_path}: duplicate feature id {feature_id!r}")
        seen_ids.add(feature_id)
        backend_features.append({"id": feature_id, "settingsNamespace": f"custom.{feature_id}"})

    if sorted(backend_features, key=lambda item: item["id"]) != checked_in:
        raise CheckFailure(
            "custom feature manifest is out of sync with backend/internal/custom/features.go"
        )
    print("custom feature manifests: valid and in sync")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-ref", help="custom base ref used for migration comparison")
    parser.add_argument("--upstream-ref", help="official tag/ref used to identify official migrations")
    parser.add_argument(
        "--run-go-generate",
        action="store_true",
        help="run go generate ./... before asserting the checkout stayed clean",
    )
    parser.add_argument(
        "--skip-generated",
        action="store_true",
        help="skip the generated-source check (use only when an earlier workflow step ran it)",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        root = repository_root()
        base_ref = resolve_ref(
            root,
            args.base_ref or os.environ.get("CUSTOM_BASE_REF"),
            ("origin/custom/integration", "custom/integration"),
        )
        upstream_ref = resolve_ref(root, args.upstream_ref or os.environ.get("UPSTREAM_REF"), ("upstream/main", "upstream/mainline"))
        if not args.skip_generated:
            check_generated_sources(root, run_generate=args.run_go_generate)
        check_migrations(root, base_ref, upstream_ref)
        check_feature_manifests(root)
        print("custom isolation checks: PASS")
        return 0
    except CheckFailure as exc:
        print(f"custom isolation checks: FAIL\n{exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
