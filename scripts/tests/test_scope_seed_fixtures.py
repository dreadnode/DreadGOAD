"""Consistency tests for the connected SCOPE-RANGE seed fixtures."""

from __future__ import annotations

import csv
import json
import pathlib
import re
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[2]
POSTGRES_SEED = ROOT / "ansible/roles/scope_range_data/files/init-postgres.sh"
MARIADB_SEED = ROOT / "ansible/roles/scope_range_data/files/init-mariadb.sql"
MONGO_SEED = ROOT / "ansible/roles/scope_range_data/files/init-mongo.js"
VALIDATION_MANIFEST = ROOT / "ad/SCOPE-RANGE/data/validation.json"
KALI_SMOKE_TEST = (
    ROOT / "ansible/roles/scope_range_kali/files/scope-range-smoke.sh"
)
VENDOR_CONTACTS = (
    ROOT / "ansible/roles/scope_range_storage/files/vendor-contacts.csv"
)


def sql_insert_rows(source: str, table: str) -> list[str]:
    """Return the rows in one declarative SQL INSERT block."""
    match = re.search(
        rf"INSERT INTO {re.escape(table)} \([^;]+?\) VALUES\n(?P<rows>.*?);",
        source,
        flags=re.DOTALL,
    )
    if match is None:
        raise AssertionError(f"missing INSERT block for {table}")
    return [
        line.strip().rstrip(",")
        for line in match["rows"].splitlines()
        if line.lstrip().startswith("(")
    ]


class ScopeSeedFixtureTests(unittest.TestCase):
    """Keep fixture volume, story, and live assertions synchronized."""

    def test_postgresql_fixture_volume(self) -> None:
        """The business database retains the expanded deep-sea dataset."""
        source = POSTGRES_SEED.read_text(encoding="utf-8")
        customers = sql_insert_rows(source, "customers")
        projects = sql_insert_rows(source, "projects")
        invoices = sql_insert_rows(source, "invoices")
        self.assertEqual(len(customers), 12)
        self.assertEqual(len(projects), 16)
        self.assertEqual(len(invoices), 24)
        self.assertEqual(len(sql_insert_rows(source, "authorization_records")), 6)
        invoice_customer_ids = [int(re.match(r"\((\d+),", row).group(1)) for row in invoices]  # type: ignore[union-attr]
        self.assertEqual(invoice_customer_ids, [item for item in range(1, 13) for _ in range(2)])
        self.assertIn("Dreadnode Biology Division", source)
        self.assertIn("'KRAKEN'", source)

        sql_companies = {
            re.match(r"\('([^']+)'", row).group(1)  # type: ignore[union-attr]
            for row in customers
        }
        with VENDOR_CONTACTS.open(encoding="utf-8", newline="") as stream:
            storage_companies = {row["company"] for row in csv.DictReader(stream)}
        self.assertEqual(storage_companies, sql_companies)

    def test_mongodb_fixture_volume(self) -> None:
        """The research database retains all four seeded collections."""
        source = MONGO_SEED.read_text(encoding="utf-8")
        expected = {
            "experiment": 10,
            "source": 12,
            "specimen_id": 8,
            "dive_id": 8,
        }
        for field, count in expected.items():
            with self.subTest(field=field):
                records = re.findall(rf"(?m)^\s*\{{ {field}:", source)
                self.assertEqual(len(records), count)
        self.assertIn("specimen_id: 'KRA-003'", source)
        self.assertIn("project: 'KRAKEN'", source)

        postgres = POSTGRES_SEED.read_text(encoding="utf-8")
        project_rows = sql_insert_rows(postgres, "projects")
        project_names = {
            re.match(r"\(\d+, '([^']+)'", row).group(1)  # type: ignore[union-attr]
            for row in project_rows
        }
        mongo_project_names = set(re.findall(r"project: '([^']+)'", source))
        self.assertLessEqual(mongo_project_names, project_names)

    def test_mariadb_fixture_volume_matches_kali_smoke_test(self) -> None:
        """The foundational smoke gate expects every seeded WordPress note."""
        seed = MARIADB_SEED.read_text(encoding="utf-8")
        notes = sql_insert_rows(seed, "wordpress.range_notes")
        smoke_test = KALI_SMOKE_TEST.read_text(encoding="utf-8")

        self.assertEqual(len(notes), 6)
        self.assertIn("grep -qx '6'", smoke_test)

    def test_live_validator_expects_expanded_fixtures(self) -> None:
        """Live checks must reject partial or stale version-one data."""
        manifest = json.loads(VALIDATION_MANIFEST.read_text(encoding="utf-8"))
        checks = {
            check["name"]: check["command"]
            for host in manifest["hosts"]
            for check in host["checks"]
        }
        self.assertIn("= 12", checks["synthetic business records are seeded"])
        self.assertIn("= 16", checks["synthetic business records are seeded"])
        self.assertIn("= 24", checks["synthetic business records are seeded"])
        mongo_check = checks["research collections are seeded"]
        for collection in ("experiments", "telemetry", "specimens", "dive_logs"):
            self.assertIn(f"db.{collection}.countDocuments", mongo_check)
        self.assertIn("scope-seed-v2", VALIDATION_MANIFEST.read_text(encoding="utf-8"))


if __name__ == "__main__":
    unittest.main()
