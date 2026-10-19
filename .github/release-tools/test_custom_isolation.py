import importlib.util
import io
from contextlib import redirect_stdout
from pathlib import Path
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "check_custom_isolation",
    Path(__file__).resolve().parents[2] / "scripts" / "check-custom-isolation.py",
)
isolation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(isolation)


class MigrationIsolationTest(unittest.TestCase):
    def check_changes(
        self, changes, *, base_paths=(), upstream_paths=(),
        head_blob=b"custom edit", upstream_blob=b"official version",
    ):
        files = {"base": set(base_paths), "upstream": set(upstream_paths)}
        diff = "".join(f"{status}\t{path}\n" for status, path in changes)
        with (
            patch.object(isolation, "run_git", return_value=diff),
            patch.object(
                isolation, "path_exists_at",
                side_effect=lambda root, ref, path: path in files.get(ref, set()),
            ),
            patch.object(isolation, "blob_at_head", return_value=head_blob),
            patch.object(isolation, "blob_at", return_value=upstream_blob),
            redirect_stdout(io.StringIO()),
        ):
            isolation.check_migrations(Path.cwd(), "base", "upstream")

    def test_go_tests_and_support_files_are_not_sql_migrations(self):
        self.check_changes(
            [
                ("A", "backend/migrations/batch_image_no_discount_migration_test.go"),
                ("M", "backend/migrations/migrations.go"),
                ("D", "backend/migrations/old_migration_test.go"),
                ("A", "backend/migrations/README.md"),
                ("A", "backend/migrations/9000_custom_batch_image_no_discount.sql"),
            ],
            base_paths=(
                "backend/migrations/migrations.go",
                "backend/migrations/old_migration_test.go",
            ),
        )

    def test_new_custom_sql_still_requires_custom_namespace(self):
        for filename in (
            "999_batch_image.sql",
            "9001_api_key_route_policies.sql",
            "9002_api_key_model_policies.sql",
        ):
            with self.subTest(filename=filename):
                with self.assertRaisesRegex(isolation.CheckFailure, "must match"):
                    self.check_changes([("A", f"backend/migrations/{filename}")])

    def test_new_custom_sql_accepts_reserved_range_and_existing_policy_names(self):
        self.check_changes(
            [("A", f"backend/migrations/{filename}") for filename in (
                "9000_custom_existing.sql",
                "9001_custom_api_key_route_policies.sql",
                "9002_custom_api_key_model_policies.sql",
                "9999_custom_last_extension.sql",
            )]
        )

    def test_new_custom_sql_rejects_numbers_outside_reserved_range(self):
        for number in (8999, 10000, 90001):
            with self.subTest(number=number):
                with self.assertRaisesRegex(isolation.CheckFailure, "must match"):
                    self.check_changes([("A", f"backend/migrations/{number}_custom_feature.sql")])

    def test_new_range_does_not_allow_modifying_or_deleting_published_policies(self):
        for filename in (
            "9001_custom_api_key_route_policies.sql",
            "9002_custom_api_key_model_policies.sql",
        ):
            path = f"backend/migrations/{filename}"
            for status, message in (("M", "cannot be modified"), ("D", "cannot be deleted")):
                with self.subTest(filename=filename, status=status):
                    with self.assertRaisesRegex(isolation.CheckFailure, message):
                        self.check_changes([(status, path)], base_paths=(path,))

    def test_new_official_sql_can_keep_its_upstream_name(self):
        path = "backend/migrations/247_official_feature.sql"
        self.check_changes([("A", path)], upstream_paths=(path,))

    def test_published_custom_sql_cannot_be_modified(self):
        path = "backend/migrations/9000_custom_existing.sql"
        with self.assertRaisesRegex(isolation.CheckFailure, "cannot be modified"):
            self.check_changes([("M", path)], base_paths=(path,))

    def test_published_sql_cannot_be_deleted_or_renamed_to_go(self):
        path = "backend/migrations/9000_custom_existing.sql"
        with self.assertRaisesRegex(isolation.CheckFailure, "cannot be deleted"):
            self.check_changes(
                [("D", path), ("A", "backend/migrations/existing_test.go")],
                base_paths=(path,),
            )

    def test_published_sql_modified_differently_from_upstream_is_rejected(self):
        path = "backend/migrations/001_init.sql"
        with self.assertRaisesRegex(isolation.CheckFailure, "differs from upstream"):
            self.check_changes(
                [("M", path)], base_paths=(path,), upstream_paths=(path,),
            )

    def test_published_sql_matching_upstream_bytes_is_allowed(self):
        path = "backend/migrations/001_init.sql"
        self.check_changes(
            [("M", path)], base_paths=(path,), upstream_paths=(path,),
            head_blob=b"official version", upstream_blob=b"official version",
        )


if __name__ == "__main__":
    unittest.main()
