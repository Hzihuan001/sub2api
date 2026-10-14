"""Negative fixtures: a surviving directory is not evidence of a wired feature."""
import json
import tempfile
import unittest
from pathlib import Path

from custom_contracts import ContractError, check, imports


class ContractTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.write("custom/manifest.yml", "contracts: custom/contracts.json\n")
        self.write(".github/custom-features.json", '{"features":[{"id":"probe"}]}')
        self.write("backend/internal/custom/probe.go", "package custom\n")
        self.write("backend/internal/server/router.go", 'package server\nimport "example/custom"\n')
        self.contract = {
            "version": 1,
            "modules": [{"id": "probe", "owner": "custom/probe", "entries": ["backend/internal/custom"], "tests": ["probe"]}],
            "tests": {"probe": {"cwd": ".", "argv": ["go", "test", "./..."]}},
            "dependency_rules": [{"id": "neutral", "source_prefixes": ["backend/internal/custom/"], "forbidden_import_prefixes": ["example/server"]}],
            "host_entries": [{"id": "host", "host_prefixes": ["backend/internal/server/"], "import_prefixes": ["example/custom"], "files": ["backend/internal/server/router.go"]}],
        }

    def write(self, path, contents):
        dest = self.root / path
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(contents, encoding="utf-8")

    def validate(self):
        self.write("custom/contracts.json", json.dumps(self.contract))
        return check(self.root)

    def test_valid_fixture(self):
        self.validate()

    def test_disconnected_host_is_rejected_even_if_file_survives(self):
        self.write("backend/internal/server/router.go", "package server\n")
        with self.assertRaisesRegex(ContractError, "disconnected"):
            self.validate()

    def test_unregistered_host_patch_is_rejected(self):
        self.write("backend/internal/server/extra.go", 'package server\nimport "example/custom"\n')
        with self.assertRaisesRegex(ContractError, "unregistered"):
            self.validate()

    def test_reverse_dependency_is_rejected(self):
        self.write("backend/internal/custom/probe.go", 'package custom\nimport (\n "example/server"\n)\n')
        with self.assertRaisesRegex(ContractError, "forbidden dependency"):
            self.validate()

    def test_duplicate_module_is_rejected(self):
        self.contract["modules"] *= 2
        with self.assertRaisesRegex(ContractError, "uniquely"):
            self.validate()

    def test_missing_entry_is_rejected(self):
        self.contract["modules"][0]["entries"] = ["missing.go"]
        with self.assertRaisesRegex(ContractError, "missing module entry"):
            self.validate()

    def test_removed_tests_cannot_turn_gate_into_noop(self):
        self.contract["tests"] = {}
        with self.assertRaisesRegex(ContractError, "must not be empty"):
            self.validate()

    def test_unused_exception_must_be_removed(self):
        self.contract["exceptions"] = [{"file":"backend/internal/custom/probe.go", "import":"example/server", "reason":"old adapter"}]
        with self.assertRaisesRegex(ContractError, "obsolete"):
            self.validate()

    def test_exact_exception_does_not_authorize_another_file(self):
        self.contract["exceptions"] = [{"file":"backend/internal/custom/probe.go", "import":"example/server", "reason":"old adapter"}]
        self.write("backend/internal/custom/probe.go", 'package custom\nimport "example/server"\n')
        self.validate()
        self.write("backend/internal/custom/other.go", 'package custom\nimport "example/server"\n')
        with self.assertRaisesRegex(ContractError, "forbidden dependency"):
            self.validate()

    def test_import_parser_covers_aliases_and_dynamic_imports(self):
        self.assertEqual(imports('import (\n alias "example/a"\n "example/b"\n)', '.go'), ['example/a', 'example/b'])
        self.assertEqual(imports("import { x } from '@/features/a'; const view = () => import('@/features/b')", '.ts'), ['@/features/a', '@/features/b'])

    def test_relative_cross_feature_import_is_rejected(self):
        self.write("frontend/src/features/probe/view.ts", "import '../other/private'")
        with self.assertRaisesRegex(ContractError, "undeclared feature dependency"):
            self.validate()

    def test_manifest_cannot_escape_checkout(self):
        self.contract["modules"][0]["entries"] = ["../../outside"]
        with self.assertRaisesRegex(ContractError, "escapes repository"):
            self.validate()


if __name__ == "__main__":
    unittest.main()
