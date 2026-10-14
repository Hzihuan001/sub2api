#!/usr/bin/env python3
"""Dependency-free custom boundary checks and executable contract-test inventory.

Static checks deliberately supplement, not replace, real router/component tests.
Only tracked production source participates in dependency rules; tests may use
the real host to prove a contribution is connected. No commands use a shell.
"""
from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
from pathlib import Path


class ContractError(ValueError):
    pass


def relative_path(root: Path, value: str) -> Path:
    candidate = (root / value).resolve()
    if not candidate.is_relative_to(root.resolve()):
        raise ContractError(f"path escapes repository: {value}")
    return candidate


def imports(source: str, suffix: str) -> list[str]:
    if suffix == ".go":
        blocks = re.findall(r'(?m)^import\s+(?:\((.*?)\)|([^\n]+))', source, re.S)
        return [item for block in blocks for item in re.findall(r'"([^"\n]+)"', ''.join(block))]
    return re.findall(r'''(?:\bfrom\s*|\bimport\s*\(\s*|\bimport\s*)["']([^"']+)["']''', source)


def production_sources(root: Path):
    for folder in ("backend/internal", "backend/cmd", "frontend/src"):
        for path in (root / folder).rglob("*"):
            if path.suffix not in {".go", ".ts", ".vue"}:
                continue
            if path.name.endswith(("_test.go", ".spec.ts", ".test.ts")) or "__tests__" in path.parts:
                continue
            yield path


def load_contracts(root: Path) -> dict:
    manifest = (root / "custom/manifest.yml").read_text(encoding="utf-8")
    match = re.search(r'^contracts:\s*(\S+)\s*$', manifest, re.M)
    if not match:
        raise ContractError("custom/manifest.yml must declare contracts")
    data = json.loads(relative_path(root, match[1]).read_text(encoding="utf-8"))
    if data.get("version") != 1:
        raise ContractError("unsupported contract manifest version")
    return data


def check(root: Path) -> dict:
    data = load_contracts(root)
    feature_ids = {item["id"] for item in json.loads((root / ".github/custom-features.json").read_text())["features"]}
    modules = data.get("modules", [])
    ids = [item["id"] for item in modules]
    if len(ids) != len(set(ids)) or set(ids) != feature_ids:
        raise ContractError("module IDs must uniquely match custom feature inventory")
    tests = data.get("tests", {})
    if not tests:
        raise ContractError("executable contract tests must not be empty")
    for module in modules:
        if not module.get("owner") or not module.get("entries") or not module.get("tests"):
            raise ContractError(f"incomplete module contract: {module['id']}")
        for entry in module["entries"]:
            if not relative_path(root, entry).exists():
                raise ContractError(f"missing module entry: {entry}")
        for test in module["tests"]:
            if test not in tests:
                raise ContractError(f"unknown test group: {test}")
        if not set(module.get("dependencies", [])) <= feature_ids:
            raise ContractError(f"unknown dependency in {module['id']}")
    for name, test in tests.items():
        if not relative_path(root, test["cwd"]).is_dir() or not test.get("argv"):
            raise ContractError(f"invalid test command: {name}")
        if not all(isinstance(arg, str) and arg for arg in test["argv"]):
            raise ContractError(f"invalid test arguments: {name}")

    rules = data.get("dependency_rules", [])
    if not rules or not data.get("host_entries"):
        raise ContractError("dependency rules and host entries cannot be empty")
    exceptions = {(e["file"], e["import"]): e for e in data.get("exceptions", [])}
    used_exceptions = set()
    sources = list(production_sources(root))
    for path in sources:
        rel = path.relative_to(root).as_posix()
        for dependency in imports(path.read_text(encoding="utf-8"), path.suffix):
            if path.suffix != ".go" and dependency.startswith("."):
                resolved = (path.parent / dependency).resolve()
                frontend = (root / "frontend/src").resolve()
                if resolved.is_relative_to(frontend):
                    dependency = "@/" + resolved.relative_to(frontend).as_posix()
            for rule in rules:
                if not any(rel.startswith(prefix) for prefix in rule["source_prefixes"]):
                    continue
                if not any(dependency.startswith(prefix) for prefix in rule["forbidden_import_prefixes"]):
                    continue
                key = (rel, dependency)
                exception = exceptions.get(key)
                if not exception or not exception.get("reason"):
                    raise ContractError(f"{rule['id']}: forbidden dependency {rel} -> {dependency}")
                used_exceptions.add(key)
            # Features must name dependencies on other frontend feature roots.
            if rel.startswith("frontend/src/features/") and dependency.startswith("@/features/"):
                own = rel.split('/')[3]
                other = dependency.split('/')[2]
                module = next((m for m in modules if m['id'] == own), None)
                if module and other != own and other not in module.get("dependencies", []):
                    raise ContractError(f"undeclared feature dependency: {own} -> {other}")
    stale = set(exceptions) - used_exceptions
    if stale:
        raise ContractError(f"remove obsolete dependency exceptions: {sorted(stale)}")

    for boundary in data["host_entries"]:
        allowed = set(boundary["files"])
        if not allowed:
            raise ContractError("host boundary must not have an empty file list")
        found = set()
        for path in sources:
            rel = path.relative_to(root).as_posix()
            if not any(rel.startswith(prefix) for prefix in boundary["host_prefixes"]):
                continue
            if any(any(dep.startswith(prefix) for prefix in boundary["import_prefixes"]) for dep in imports(path.read_text(encoding="utf-8"), path.suffix)):
                found.add(rel)
        if found != allowed:
            raise ContractError(f"{boundary['id']}: host entry mismatch; unregistered={sorted(found-allowed)}, disconnected={sorted(allowed-found)}")
    return data


def run_tests(root: Path, data: dict, selected: list[str]) -> None:
    for name in selected or list(data["tests"]):
        if name not in data["tests"]:
            raise ContractError(f"unknown test group: {name}")
        spec = data["tests"][name]
        argv = list(spec["argv"])
        argv[0] = shutil.which(argv[0]) or argv[0]
        print(f"custom contract [{name}]: {' '.join(argv)}", flush=True)
        subprocess.run(argv, cwd=relative_path(root, spec["cwd"]), check=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--run-tests", action="store_true")
    parser.add_argument("--group", action="append", default=[])
    args = parser.parse_args()
    try:
        data = check(args.root)
        print(f"custom architecture contracts: PASS ({len(data['modules'])} modules, {len(data['tests'])} executable test groups)")
        if args.run_tests:
            run_tests(args.root, data, args.group)
        return 0
    except (ContractError, OSError, ValueError, KeyError, subprocess.CalledProcessError) as exc:
        print(f"custom architecture contracts: FAIL: {exc}")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
