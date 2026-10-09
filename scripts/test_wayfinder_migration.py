#!/usr/bin/env python3
"""Causal controls for the offline Wayfinder migration preparer."""

from __future__ import annotations

import copy
import json
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import wayfinder_migration as migration


class WayfinderMigrationTests(unittest.TestCase):
    def test_historical_link_revision_rejects_current_only_target(self) -> None:
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-git-") as directory:
            root = Path(directory)
            source_path = root / "docs" / "wayfinder" / "issues" / "sample.md"
            source_path.parent.mkdir(parents=True)
            (root / "README.md").write_text("baseline\n", encoding="utf-8")
            run_git(root, "init", "-q")
            run_git(root, "add", "README.md")
            run_git(
                root,
                "-c",
                "user.name=Wayfinder test",
                "-c",
                "user.email=wayfinder-test@example.invalid",
                "commit",
                "-qm",
                "baseline",
            )
            revision = run_git(root, "rev-parse", "HEAD").strip()
            (root / "README.md").write_text("changed after baseline\n", encoding="utf-8")
            (root / "new.md").write_text("added after baseline\n", encoding="utf-8")

            unresolved: list[dict[str, str]] = []
            rendered = migration.rewrite_links(
                "[baseline](../../../README.md) [new](../../../new.md)",
                root=root,
                source_path=source_path,
                mapping={"identities": []},
                unresolved=unresolved,
                historical_revision=revision,
            )

            self.assertIn(f"{migration.HOSTED_ROOT}/blob/{revision}/README.md", rendered)
            self.assertIn("local-reference:new.md", rendered)
            self.assertEqual(
                unresolved,
                [
                    {
                        "kind": "path",
                        "source": "../../../new.md",
                        "path": "new.md",
                        "status": "unresolved",
                        "reason": "absent-at-historical-link-revision",
                        "historicalLinkCommit": revision,
                    }
                ],
            )

    def test_historical_link_survives_current_deletion_and_symlink_changes(self) -> None:
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-history-") as directory:
            root = Path(directory)
            source_path = root / "docs" / "wayfinder" / "issues" / "sample.md"
            source_path.parent.mkdir(parents=True)
            (root / "README.md").write_text("historical README\n", encoding="utf-8")
            run_git(root, "init", "-q")
            run_git(root, "add", "README.md")
            run_git(
                root,
                "-c",
                "user.name=Wayfinder test",
                "-c",
                "user.email=wayfinder-test@example.invalid",
                "commit",
                "-qm",
                "baseline",
            )
            revision = run_git(root, "rev-parse", "HEAD").strip()
            markdown = "[Readme](../../../README.md)\n"

            before_unresolved: list[dict[str, str]] = []
            before = migration.rewrite_links(
                markdown,
                root=root,
                source_path=source_path,
                mapping={"identities": []},
                unresolved=before_unresolved,
                historical_revision=revision,
            )

            (root / "README.md").unlink()
            missing_unresolved: list[dict[str, str]] = []
            after_deletion = migration.rewrite_links(
                markdown,
                root=root,
                source_path=source_path,
                mapping={"identities": []},
                unresolved=missing_unresolved,
                historical_revision=revision,
            )
            self.assertEqual(after_deletion, before)
            self.assertEqual(missing_unresolved, [])

            (root / "current-target.md").write_text("current target\n", encoding="utf-8")
            (root / "README.md").symlink_to("current-target.md")
            symlink_unresolved: list[dict[str, str]] = []
            after_symlink = migration.rewrite_links(
                markdown,
                root=root,
                source_path=source_path,
                mapping={"identities": []},
                unresolved=symlink_unresolved,
                historical_revision=revision,
            )
            self.assertEqual(after_symlink, before)
            self.assertEqual(symlink_unresolved, [])

            (root / "current-target.md").write_text("changed current target\n", encoding="utf-8")
            changed_symlink_unresolved: list[dict[str, str]] = []
            after_symlink_target_change = migration.rewrite_links(
                markdown,
                root=root,
                source_path=source_path,
                mapping={"identities": []},
                unresolved=changed_symlink_unresolved,
                historical_revision=revision,
            )
            self.assertEqual(after_symlink_target_change, before)
            self.assertEqual(changed_symlink_unresolved, [])

    def test_unavailable_historical_revision_is_a_diagnostic(self) -> None:
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-revision-") as directory:
            root = Path(directory)
            source_path = root / "docs" / "wayfinder" / "issues" / "sample.md"
            source_path.parent.mkdir(parents=True)
            (root / "README.md").write_text("baseline\n", encoding="utf-8")
            run_git(root, "init", "-q")
            run_git(root, "add", "README.md")
            run_git(
                root,
                "-c",
                "user.name=Wayfinder test",
                "-c",
                "user.email=wayfinder-test@example.invalid",
                "commit",
                "-qm",
                "baseline",
            )

            with self.assertRaisesRegex(migration.MigrationError, "revision unavailable"):
                migration.rewrite_links(
                    "[Readme](../../../README.md)\n",
                    root=root,
                    source_path=source_path,
                    mapping={"identities": []},
                    unresolved=[],
                    historical_revision="0" * 40,
                )

    def test_input_manifest_uses_exact_issue_bytes_before_decoding(self) -> None:
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-bytes-") as directory:
            root = Path(directory)
            issue_directory = root / "docs" / "wayfinder" / "issues"
            issue_directory.mkdir(parents=True)
            (root / "scripts").mkdir()
            (root / "scripts" / "wayfinder_migration.py").write_text("fixture preparer\n", encoding="utf-8")
            write_issue(issue_directory / "one.md", "one", "One")
            mapping = fixture_mapping("one")

            lf = (issue_directory / "one.md").read_bytes()
            first_rows = migration.issue_snapshot(root)
            first_manifest = migration.canonical_input_manifest(root, first_rows, mapping)
            self.assertEqual(first_rows[0]["sourceSha256"], migration.sha256_bytes(lf))

            crlf = lf.replace(b"\n", b"\r\n")
            (issue_directory / "one.md").write_bytes(crlf)
            second_rows = migration.issue_snapshot(root)
            second_manifest = migration.canonical_input_manifest(root, second_rows, mapping)
            self.assertEqual(second_rows[0]["sourceSha256"], migration.sha256_bytes(crlf))
            self.assertNotEqual(first_rows[0]["sourceSha256"], second_rows[0]["sourceSha256"])
            self.assertNotEqual(first_manifest["sha256"], second_manifest["sha256"])

            with patch.object(Path, "read_text", side_effect=AssertionError("issue snapshot performed a second source read")):
                crlf_rows = migration.issue_snapshot(root)
                crlf_manifest = migration.canonical_input_manifest(root, crlf_rows, mapping)
                crlf_payloads = migration.build_payloads(root, crlf_rows, mapping, crlf_manifest)

            self.assertEqual(crlf_rows[0]["sourceSha256"], migration.sha256_bytes(crlf))
            self.assertNotIn("\r", crlf_rows[0]["source"])
            self.assertEqual(crlf_payloads["issues"]["items"][0]["body"], "Question\n")

    def test_input_manifest_changes_for_changed_and_added_inputs(self) -> None:
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-input-") as directory:
            root = Path(directory)
            issue_directory = root / "docs" / "wayfinder" / "issues"
            issue_directory.mkdir(parents=True)
            (root / "scripts").mkdir()
            (root / "scripts" / "wayfinder_migration.py").write_text("fixture preparer\n", encoding="utf-8")
            write_issue(issue_directory / "one.md", "one", "One")
            rows = migration.issue_snapshot(root)
            mapping = fixture_mapping("one")
            first = migration.canonical_input_manifest(root, rows, mapping)["sha256"]

            (issue_directory / "one.md").write_text(
                '<!-- {"id":"one","title":"One","status":"open","labels":[],"parent":null,"assignee":null,"blocked_by":[]} -->\n# One\nchanged\n',
                encoding="utf-8",
            )
            changed = migration.canonical_input_manifest(root, migration.issue_snapshot(root), mapping)["sha256"]
            self.assertNotEqual(first, changed)

            write_issue(issue_directory / "two.md", "two", "Two")
            added = migration.canonical_input_manifest(root, migration.issue_snapshot(root), mapping)["sha256"]
            self.assertNotEqual(changed, added)

    def test_explicit_historical_inputs_survive_live_input_drift(self) -> None:
        source_root = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-snapshot-") as directory:
            root = Path(directory)
            issue_target = root / "docs" / "wayfinder" / "issues"
            issue_target.parent.mkdir(parents=True)
            shutil.copytree(source_root / "docs" / "wayfinder" / "issues", issue_target)
            shutil.copy2(
                source_root / "docs" / "wayfinder" / "hosted-identities.json",
                root / "docs" / "wayfinder" / "hosted-identities.json",
            )
            scripts_target = root / "scripts"
            scripts_target.mkdir(parents=True)
            shutil.copy2(source_root / "scripts" / "wayfinder_migration.py", scripts_target / "wayfinder_migration.py")
            snapshot_source = source_root / "docs" / "wayfinder" / "migration" / "snapshots" / migration.HISTORICAL_INPUT_SNAPSHOT_ID
            snapshot_target = root / "docs" / "wayfinder" / "migration" / "snapshots" / migration.HISTORICAL_INPUT_SNAPSHOT_ID
            snapshot_target.parent.mkdir(parents=True)
            shutil.copytree(snapshot_source, snapshot_target)
            shutil.copy2(
                source_root / "docs" / "wayfinder" / "migration" / "input-manifest.json",
                root / "docs" / "wayfinder" / "migration" / "input-manifest.json",
            )
            live_issue = root / "docs" / "wayfinder" / "issues" / "run-binding.md"
            live_issue.write_bytes(live_issue.read_bytes() + b"\ncurrent-only drift\n")
            live_mapping_path = root / "docs" / "wayfinder" / "hosted-identities.json"
            live_mapping = json.loads(live_mapping_path.read_text(encoding="utf-8"))
            verified_identity = next(
                identity for identity in live_mapping["identities"] if identity["hostedStatus"] == "verified"
            )
            verified_identity["hostedTitle"] += " current-only drift"
            live_mapping_path.write_text(migration.canonical_json(live_mapping), encoding="utf-8")
            live_preparer = scripts_target / "wayfinder_migration.py"
            live_preparer.write_bytes(live_preparer.read_bytes() + b"\n# current-only drift\n")
            initial_inputs = migration.select_migration_inputs(root, migration.HISTORICAL_INPUT_SNAPSHOT_ID)
            self.assertIsNotNone(initial_inputs.research_record_path)
            self.assertIsNotNone(initial_inputs.research_record_bytes)
            live_research = root / initial_inputs.research_record_path
            live_research.parent.mkdir(parents=True, exist_ok=True)
            live_research.write_bytes(initial_inputs.research_record_bytes + b"\ncurrent-only research drift\n")

            live_rows = migration.issue_snapshot(root)
            historical_inputs = migration.select_migration_inputs(root, migration.HISTORICAL_INPUT_SNAPSHOT_ID)
            historical_rows = migration.issue_snapshot(root, historical_inputs)
            historical_mapping = migration.load_mapping(
                root / "docs" / "wayfinder" / "hosted-identities.json",
                historical_rows,
                mapping_bytes=historical_inputs.mapping_bytes,
            )
            historical_manifest = migration.canonical_input_manifest(
                root,
                historical_rows,
                historical_mapping,
                preparer_bytes=historical_inputs.preparer_bytes,
            )

            live_run_binding = next(row for row in live_rows if row["metadata"]["id"] == "run-binding")
            historical_run_binding = next(row for row in historical_rows if row["metadata"]["id"] == "run-binding")
            self.assertNotEqual(live_run_binding["sourceSha256"], historical_run_binding["sourceSha256"])
            self.assertNotEqual(historical_inputs.mapping_bytes, live_mapping_path.read_bytes())
            self.assertNotEqual(historical_inputs.preparer_bytes, live_preparer.read_bytes())
            self.assertIsNotNone(historical_inputs.research_record_bytes)
            self.assertNotEqual(historical_inputs.research_record_bytes, live_research.read_bytes())
            self.assertEqual(historical_inputs.research_record_path, "docs/research/generalized-run-binding.md")
            self.assertEqual(len(historical_rows), 72)
            self.assertEqual(historical_manifest["sha256"], migration.HISTORICAL_INPUT_CANONICAL_SHA256)

    def test_missing_or_tampered_historical_bundle_never_falls_back_to_live_inputs(self) -> None:
        source_root = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-snapshot-negative-") as directory:
            root = Path(directory)
            issue_target = root / "docs" / "wayfinder" / "issues"
            issue_target.parent.mkdir(parents=True)
            shutil.copytree(source_root / "docs" / "wayfinder" / "issues", issue_target)
            shutil.copy2(
                source_root / "docs" / "wayfinder" / "hosted-identities.json",
                root / "docs" / "wayfinder" / "hosted-identities.json",
            )
            scripts_target = root / "scripts"
            scripts_target.mkdir(parents=True)
            shutil.copy2(source_root / "scripts" / "wayfinder_migration.py", scripts_target / "wayfinder_migration.py")
            snapshot_source = source_root / "docs" / "wayfinder" / "migration" / "snapshots" / migration.HISTORICAL_INPUT_SNAPSHOT_ID
            snapshot_root = root / "docs" / "wayfinder" / "migration" / "snapshots" / migration.HISTORICAL_INPUT_SNAPSHOT_ID
            snapshot_root.parent.mkdir(parents=True)
            shutil.copytree(snapshot_source, snapshot_root)
            shutil.copy2(
                source_root / "docs" / "wayfinder" / "migration" / "input-manifest.json",
                root / "docs" / "wayfinder" / "migration" / "input-manifest.json",
            )
            bundle_path = snapshot_root / "inputs.tar"
            original_bundle = bundle_path.read_bytes()

            bundle_path.unlink()
            with self.assertRaisesRegex(migration.MigrationError, "missing historical input bundle"):
                migration.select_migration_inputs(root, migration.HISTORICAL_INPUT_SNAPSHOT_ID)

            bundle_path.write_bytes(original_bundle + b"tampered")
            with self.assertRaisesRegex(migration.MigrationError, "historical input bundle bytes changed"):
                migration.select_migration_inputs(root, migration.HISTORICAL_INPUT_SNAPSHOT_ID)

    def test_current_write_uses_a_new_immutable_output_identity(self) -> None:
        with tempfile.TemporaryDirectory(prefix="wayfinder-migration-current-output-") as directory:
            root = Path(directory)
            issue_directory = root / "docs" / "wayfinder" / "issues"
            issue_directory.mkdir(parents=True)
            scripts = root / "scripts"
            scripts.mkdir()
            (scripts / "wayfinder_migration.py").write_text("current preparer fixture\n", encoding="utf-8")
            issue = issue_directory / "one.md"
            write_issue(issue, "one", "One")
            mapping_path = root / "docs" / "wayfinder" / "hosted-identities.json"
            mapping_path.write_text(migration.canonical_json(fixture_mapping("one")), encoding="utf-8")
            historical_output = root / "docs" / "wayfinder" / "migration" / "issues.json"
            historical_output.parent.mkdir(parents=True)
            historical_output.write_bytes(b"historical output bytes\n")

            result = migration.run(root, mapping_path, write=True, check=False, output_snapshot_id="fixture-current-1")
            self.assertEqual(result, 0)
            output_directory = root / "docs" / "wayfinder" / "migration" / "snapshots" / "fixture-current-1" / "outputs"
            self.assertTrue((output_directory / "issues.json").is_file())
            self.assertTrue((output_directory / "hosted-identities.json").is_file())
            self.assertEqual(historical_output.read_bytes(), b"historical output bytes\n")
            self.assertEqual(
                migration.run(root, mapping_path, write=False, check=True, output_snapshot_id="fixture-current-1"),
                0,
            )

            issue.write_bytes(issue.read_bytes() + b"current body changed\n")
            with self.assertRaisesRegex(migration.MigrationError, "already exists with different bytes"):
                migration.run(root, mapping_path, write=True, check=False, output_snapshot_id="fixture-current-1")
            self.assertEqual(historical_output.read_bytes(), b"historical output bytes\n")

    def test_payload_validation_rejects_a_blob_target_at_the_wrong_revision(self) -> None:
        root = Path(__file__).resolve().parents[1]
        inputs = migration.select_migration_inputs(root, migration.HISTORICAL_INPUT_SNAPSHOT_ID)
        rows = migration.issue_snapshot(root, inputs)
        mapping = migration.load_mapping(root / "docs" / "wayfinder" / "hosted-identities.json", rows, mapping_bytes=inputs.mapping_bytes)
        input_snapshot = migration.canonical_input_manifest(root, rows, mapping, preparer_bytes=inputs.preparer_bytes)
        payloads = migration.build_payloads(root, rows, mapping, input_snapshot)
        mutated = copy.deepcopy(payloads)
        target = next(
            item
            for item in mutated["issues"]["items"]
            if f"{migration.HOSTED_ROOT}/blob/{migration.HISTORICAL_LINK_COMMIT}/" in item["body"]
        )
        target["body"] = target["body"].replace(
            f"{migration.HOSTED_ROOT}/blob/{migration.HISTORICAL_LINK_COMMIT}/",
            f"{migration.HOSTED_ROOT}/blob/{'0' * 40}/",
            1,
        )
        with self.assertRaises(migration.MigrationError):
            migration.validate_payloads(root, rows, mapping, input_snapshot, mutated)

    def test_edges_only_report_endpoint_readiness(self) -> None:
        root = Path(__file__).resolve().parents[1]
        inputs = migration.select_migration_inputs(root, migration.HISTORICAL_INPUT_SNAPSHOT_ID)
        rows = migration.issue_snapshot(root, inputs)
        mapping = migration.load_mapping(root / "docs" / "wayfinder" / "hosted-identities.json", rows, mapping_bytes=inputs.mapping_bytes)
        input_snapshot = migration.canonical_input_manifest(root, rows, mapping, preparer_bytes=inputs.preparer_bytes)
        payloads = migration.build_payloads(root, rows, mapping, input_snapshot)
        self.assertTrue(payloads["edges"]["items"])
        self.assertTrue(
            all(
                edge["status"] in {"ready-to-publish", "pending-hosted-identities"}
                and edge["relationshipStatus"] == "unverified"
                and edge["relationshipReceipt"] is None
                for edge in payloads["edges"]["items"]
            )
        )


def run_git(root: Path, *arguments: str) -> str:
    result = subprocess.run(
        ["git", "-C", str(root), *arguments],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def write_issue(path: Path, local_id: str, title: str) -> None:
    metadata = (
        f'{{"id":"{local_id}","title":"{title}","status":"open","labels":[],"parent":null,'
        '"assignee":null,"blocked_by":[]}'
    )
    path.write_text(f"<!-- {metadata} -->\n# {title}\nQuestion\n", encoding="utf-8")


def fixture_mapping(local_id: str) -> dict[str, object]:
    return {
        "schemaVersion": migration.SCHEMA_VERSION,
        "repository": migration.REPOSITORY,
        "historicalLinkRevision": dict(migration.HISTORICAL_LINK_REVISION),
        "mapLocalId": local_id,
        "migrationStatus": "pending-first-hosted-publication",
        "localIssueCount": 1,
        "verifiedHostedCount": 0,
        "pendingHostedCount": 1,
        "identities": [
            {
                "localId": local_id,
                "localTitle": local_id.title(),
                "hostedStatus": "pending",
                "hostedIssueNumber": None,
                "hostedIssueId": None,
                "hostedUrl": None,
                "hostedTitle": None,
                "hostedState": None,
                "hostedBodySha256": None,
                "identityKind": "local-snapshot",
            }
        ],
    }


if __name__ == "__main__":
    unittest.main()
