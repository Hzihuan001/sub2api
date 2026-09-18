#!/usr/bin/env python3
"""Regenerate Ent and Wire code and fail if generated files drift.

The generated ORM and dependency-injection files are part of the source tree,
but must never be hand-merged during an upstream update.  Running the same
generation commands used by the project and checking the resulting diff keeps
the checked-in output deterministic and makes merge mistakes visible before a
release is published.
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import sys


def run(root: Path, *args: str) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            list(args),
            cwd=root,
            check=True,
            text=True,
        )
    except OSError as exc:
        print(f"generated code check failed: cannot run {' '.join(args)}: {exc}", file=sys.stderr)
        raise SystemExit(1) from exc
    except subprocess.CalledProcessError as exc:
        print(f"generated code check failed: {' '.join(args)}", file=sys.stderr)
        raise SystemExit(exc.returncode or 1) from exc


def changed_generated_paths(root: Path) -> list[str]:
    tracked = subprocess.run(
        [
            "git",
            "diff",
            "--name-only",
            "--diff-filter=ACDMRTUXB",
            "--",
            "backend/ent",
            "backend/cmd/server/wire_gen.go",
        ],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    untracked = subprocess.run(
        [
            "git",
            "ls-files",
            "--others",
            "--exclude-standard",
            "--",
            "backend/ent",
            "backend/cmd/server/wire_gen.go",
        ],
        cwd=root,
        check=True,
        capture_output=True,
        text=True,
    ).stdout.splitlines()
    return sorted(set(path for path in tracked + untracked if path))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--root",
        default=None,
        help="repository root (defaults to the parent of this script)",
    )
    parser.add_argument(
        "--go",
        default=os.environ.get("GO", "go"),
        help="Go executable to use (also available through the GO environment variable)",
    )
    args = parser.parse_args()

    # Accepting an explicit root makes this check usable from a workspace
    # wrapper, a temporary checkout, and Windows paths without relying on the
    # process current directory.  CI continues to use the script directory as
    # the default, so this remains backwards compatible.
    root = Path(args.root).resolve() if args.root else Path(__file__).resolve().parents[1]
    backend = root / "backend"
    if not backend.is_dir():
        print(f"generated code check failed: missing {backend}", file=sys.stderr)
        return 1

    # Seed GOFLAGS with readonly for generator commands that do not explicitly
    # select a module mode.  The repository's Ent directive uses -mod=mod so
    # its generator can resolve tool-only transitive modules; those checksums
    # are committed and reviewed like any other dependency metadata.
    env = os.environ.copy()
    env.setdefault("GOFLAGS", "-mod=readonly")
    go = args.go
    for target in ("./ent", "./cmd/server"):
        try:
            subprocess.run(
                [go, "generate", target],
                cwd=backend,
                check=True,
                env=env,
            )
        except OSError as exc:
            print(f"generated code check failed: cannot run {go} generate {target}: {exc}", file=sys.stderr)
            return 1
        except subprocess.CalledProcessError as exc:
            print(f"generated code check failed: go generate {target}", file=sys.stderr)
            return exc.returncode or 1

    changed = changed_generated_paths(root)
    if changed:
        print("generated code is out of date; run 'make -C backend generate' and commit the result:", file=sys.stderr)
        print("\n".join(f"  {path}" for path in changed), file=sys.stderr)
        return 1

    print("generated code check passed (ent and wire)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
