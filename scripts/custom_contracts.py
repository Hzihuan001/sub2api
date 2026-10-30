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


def go_host_call_count(source: str, function: str, call: str) -> int:
    """Count a local or qualified call inside one named Go host function.

    This checks the production call site, not just its package import. Ignore
    comments and literals so a deleted call cannot survive as documentation.
    Balanced delimiters keep calls in another function outside this contract.
    """
    if not re.fullmatch(r'[A-Za-z_]\w*', function) or not re.fullmatch(r'[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*', call):
        raise ContractError("invalid required host function or call")
    code = re.sub(r'''//[^\n]*|/\*[\s\S]*?\*/|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|`[^`]*`''', ' ', source)
    declaration = re.search(r'\bfunc\s+' + re.escape(function) + r'\s*\(', code)
    if not declaration:
        return 0
    depth = 1
    cursor = declaration.end()
    while cursor < len(code) and depth:
        depth += (code[cursor] == '(') - (code[cursor] == ')')
        cursor += 1
    opening = code.find('{', cursor)
    if depth or opening < 0:
        return 0
    depth = 1
    cursor = opening + 1
    while cursor < len(code) and depth:
        depth += (code[cursor] == '{') - (code[cursor] == '}')
        cursor += 1
    if depth:
        return 0
    pattern = r'(?<![\w.])' + r'\s*\.\s*'.join(re.escape(part) for part in call.split('.')) + r'\s*\('
    return len(re.findall(pattern, code[opening + 1:cursor - 1]))


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


def contract_entry_matches(rel: str, entry: str) -> bool:
    """Return whether an owned file or directory entry covers a path."""
    normalized = entry.rstrip("/")
    return rel == normalized or rel.startswith(normalized + "/")


def registered_contract_paths(data: dict) -> set[str]:
    registered: set[str] = set()
    for module in data.get("modules", []):
        registered.update(module.get("entries", []))
    for boundary in data.get("host_entries", []):
        registered.update(boundary.get("files", []))
    for extension in data.get("deployment_extensions", []):
        for key in ("source", "deployment"):
            value = extension.get(key)
            if value:
                registered.add(value)
        registered.update(extension.get("tests", []))
    return registered


def is_custom_candidate(rel: str) -> bool:
    lower = rel.lower()
    if "pelican" in lower:
        return True
    if re.fullmatch(r"backend/migrations/9[0-9]{3}_custom_[a-z0-9][a-z0-9_-]*\.sql", rel):
        return True
    if lower.startswith((
        "backend/internal/keyrouting/",
        "backend/internal/authz/",
        "backend/internal/securityaudit/",
        "backend/cmd/direct-access/",
        "deploy/direct-access/",
        "frontend/src/features/key-routing/",
        "frontend/src/features/gateway-resilience/",
        "frontend/src/features/image-studio/",
        "frontend/src/features/prompt-audit/",
    )):
        return True
    if lower.startswith("backend/internal/") and "batch_image" in lower:
        return True
    if lower.startswith("frontend/src/") and "batchimage" in lower:
        return True
    return False


def check_orphan_custom_sources(root: Path, data: dict) -> None:
    """Reject recognizable custom sources that are absent from ownership lists.

    This intentionally targets stable custom naming conventions rather than
    treating every modified host file as feature-owned. Host files remain
    governed by host_entries and required_host_calls.
    """
    registered = registered_contract_paths(data)
    orphaned: list[str] = []
    candidates: list[Path] = list(production_sources(root))
    for folder in ("backend/cmd/direct-access", "deploy/direct-access"):
        base = root / folder
        if base.exists():
            candidates.extend(path for path in base.rglob("*") if path.is_file() and path.suffix not in {".go", ".ts", ".vue"})
    migrations = root / "backend/migrations"
    if migrations.exists():
        candidates.extend(path for path in migrations.glob("*.sql") if path.is_file())
    for path in candidates:
        rel = path.relative_to(root).as_posix()
        if not is_custom_candidate(rel):
            continue
        if not any(contract_entry_matches(rel, entry) for entry in registered):
            orphaned.append(rel)
    if orphaned:
        raise ContractError("orphan custom sources not registered: " + ", ".join(sorted(orphaned)))


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
    check_orphan_custom_sources(root, data)
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
    required_calls = data.get("required_host_calls", [])
    if not required_calls:
        raise ContractError("required host calls cannot be empty")
    for contract in required_calls:
        path = relative_path(root, contract["file"])
        if path.suffix != ".go" or not path.is_file():
            raise ContractError(f"invalid required host call file: {contract['file']}")
        expected = contract.get("count", 1)
        if type(expected) is not int or expected < 1:
            raise ContractError("required host call count must be a positive integer")
        actual = go_host_call_count(path.read_text(encoding="utf-8"), contract["function"], contract["call"])
        if actual != expected:
            raise ContractError(f"{contract['id']}: required host call {contract['file']}:{contract['function']} -> {contract['call']} expected={expected}, actual={actual}")
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
