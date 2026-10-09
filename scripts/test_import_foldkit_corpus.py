#!/usr/bin/env python3
"""Behavioral controls for the pinned Foldkit corpus importer.

These checks exercise the importer at its source, snapshot, and corpus-matrix
boundaries. They intentionally use only Python's standard library so the gate
can validate the reference corpus before any host package is installed.
"""

from __future__ import annotations

import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "import_foldkit_corpus", ROOT / "scripts" / "import_foldkit_corpus.py"
)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


class CorpusImporterControls(unittest.TestCase):
    def test_pinned_snapshot_and_complete_matrix_are_source_backed(self) -> None:
        snapshot = ROOT / MODULE.SNAPSHOT_RELATIVE
        manifest = MODULE.validate_snapshot(snapshot)
        MODULE.validate_corpus_manifest(
            ROOT / MODULE.CORPUS_RELATIVE / "manifest.json", snapshot, ROOT
        )
        self.assertEqual(manifest["source"]["commit"], MODULE.COMMIT)
        self.assertEqual(manifest["selection"]["exampleDirectoryCount"], 34)
        self.assertEqual(len(manifest["files"]), MODULE.EXPECTED_FILE_COUNT)

    def test_corpus_matrix_uses_the_passed_snapshot_location(self) -> None:
        with tempfile.TemporaryDirectory(prefix="foldkit-corpus-path-") as temporary:
            repository_root = pathlib.Path(temporary)
            snapshot = repository_root / "captured" / "foldkit"
            matrix = MODULE.corpus_manifest_for(snapshot, repository_root)
            self.assertEqual(
                matrix["snapshot"]["path"], "captured/foldkit"
            )
            self.assertEqual(
                matrix["coverage"][0]["evidence"],
                ["captured/foldkit/examples/api-cache/package.json"],
            )
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.corpus_manifest_for(
                    repository_root.parent / "outside", repository_root
                )
            self.assertIn("outside repository root", str(refusal.exception))

    def test_malformed_matrix_rows_have_actionable_diagnostics(self) -> None:
        with tempfile.TemporaryDirectory(prefix="foldkit-corpus-matrix-") as temporary:
            repository_root = pathlib.Path(temporary)
            snapshot = repository_root / MODULE.SNAPSHOT_RELATIVE
            for example in MODULE.EXAMPLE_IDS:
                target = snapshot / "examples" / example / "package.json"
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(
                    ROOT
                    / MODULE.SNAPSHOT_RELATIVE
                    / "examples"
                    / example
                    / "package.json",
                    target,
                )
            manifest_path = repository_root / MODULE.CORPUS_RELATIVE / "manifest.json"
            manifest_path.parent.mkdir(parents=True, exist_ok=True)
            original = json.loads(
                (ROOT / MODULE.CORPUS_RELATIVE / "manifest.json").read_text(
                    encoding="utf-8"
                )
            )

            cases = (
                (lambda value: value["coverage"].__setitem__(1, None), "row 1 must be an object"),
                (
                    lambda value: value["coverage"][1].pop("evidence"),
                    "row 1 has malformed fields (missing evidence)",
                ),
                (
                    lambda value: value["coverage"][1].__setitem__("status", "covered"),
                    "invalid corpus coverage status at row 1",
                ),
            )
            for mutate, expected_message in cases:
                with self.subTest(expected_message=expected_message):
                    manifest = json.loads(json.dumps(original))
                    mutate(manifest)
                    manifest_path.write_text(
                        json.dumps(manifest), encoding="utf-8"
                    )
                    with self.assertRaises(MODULE.CorpusImportError) as refusal:
                        MODULE.validate_corpus_manifest(
                            manifest_path, snapshot, repository_root
                        )
                    self.assertIn(expected_message, str(refusal.exception))

            manifest_path.write_text("[]\n", encoding="utf-8")
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.validate_corpus_manifest(manifest_path, snapshot, repository_root)
            self.assertIn("root must be an object", str(refusal.exception))

            false_claim = json.loads(json.dumps(original))
            false_row = next(
                row
                for row in false_claim["coverage"]
                if row["example"] == "counter" and row["host"] == "effra-effect-js"
            )
            false_row.update(
                status="implemented-and-verified",
                evidence=[
                    f"{MODULE.SNAPSHOT_RELATIVE.as_posix()}/examples/counter/src/main.ts"
                ],
                note="Only the immutable upstream source is present.",
            )
            manifest_path.write_text(json.dumps(false_claim), encoding="utf-8")
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.validate_corpus_manifest(manifest_path, snapshot, repository_root)
            self.assertIn("pinned upstream source-reference evidence", str(refusal.exception))

            authored_looking_claim = json.loads(json.dumps(false_claim))
            authored_looking_claim["coverage"][
                MODULE.EXAMPLE_IDS.index("counter") * len(MODULE.HOSTS)
                + MODULE.HOSTS.index("effra-effect-js")
            ]["evidence"] = ["conformance/framework-ports/fake-runner-output.txt"]
            fake_output = repository_root / "conformance" / "framework-ports" / "fake-runner-output.txt"
            fake_output.write_text("typecheck=passed\nexecute=passed\n", encoding="utf-8")
            manifest_path.write_text(
                json.dumps(authored_looking_claim), encoding="utf-8"
            )
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.validate_corpus_manifest(manifest_path, snapshot, repository_root)
            self.assertIn("not accepted before an executable runner", str(refusal.exception))

            snapshot_manifest = snapshot / "manifest.json"
            snapshot_manifest.write_text("[]\n", encoding="utf-8")
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.validate_snapshot(snapshot)
            self.assertIn("root must be an object", str(refusal.exception))

    def test_symlink_referent_must_be_listed_in_snapshot_manifest(self) -> None:
        with tempfile.TemporaryDirectory(prefix="foldkit-corpus-symlink-") as temporary:
            snapshot = pathlib.Path(temporary) / "snapshot"
            shutil.copytree(
                ROOT / MODULE.SNAPSHOT_RELATIVE,
                snapshot,
                symlinks=True,
            )
            manifest_path = snapshot / "manifest.json"
            manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
            manifest["files"] = [
                item for item in manifest["files"] if item["path"] != "AGENTS.md"
            ]
            manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.validate_snapshot(snapshot)
            self.assertIn(
                "snapshot symlink referent is absent from the manifest",
                str(refusal.exception),
            )

    def test_snapshot_modes_follow_the_git_executable_bit_only(self) -> None:
        with tempfile.TemporaryDirectory(prefix="foldkit-corpus-mode-") as temporary:
            snapshot = pathlib.Path(temporary) / "snapshot"
            shutil.copytree(
                ROOT / MODULE.SNAPSHOT_RELATIVE,
                snapshot,
                symlinks=True,
            )
            manifest = json.loads((snapshot / "manifest.json").read_text(encoding="utf-8"))
            regular = next(item for item in manifest["files"] if item["mode"] == "100644")
            target = snapshot.joinpath(*pathlib.PurePosixPath(regular["path"]).parts)
            os.chmod(target, 0o600)
            MODULE.validate_snapshot(snapshot)
            os.chmod(target, 0o700)
            with self.assertRaises(MODULE.CorpusImportError) as refusal:
                MODULE.validate_snapshot(snapshot)
            self.assertIn("snapshot executable mode mismatch", str(refusal.exception))

    def test_failed_capture_removes_unpublished_snapshot_and_stage(self) -> None:
        with tempfile.TemporaryDirectory(prefix="foldkit-corpus-capture-") as temporary:
            repository_root = pathlib.Path(temporary)
            source = repository_root / "fixture-source"
            source.mkdir()
            (source / "examples" / "toy" / "src").mkdir(parents=True)
            (source / "packages" / "foldkit").mkdir(parents=True)
            (source / "package.json").write_text(
                json.dumps(
                    {
                        "packageManager": MODULE.WORKSPACE_PACKAGE_MANAGER,
                        "devDependencies": {"effect": MODULE.WORKSPACE_EFFECT},
                    }
                ),
                encoding="utf-8",
            )
            (source / "packages" / "foldkit" / "package.json").write_text(
                json.dumps({"version": MODULE.RELEASE}), encoding="utf-8"
            )
            (source / "examples" / "toy" / "package.json").write_text(
                json.dumps({"name": "toy"}), encoding="utf-8"
            )
            (source / "examples" / "toy" / "src" / "main.ts").write_text(
                "export const value = 1\n", encoding="utf-8"
            )
            git_environment = os.environ.copy()
            git_environment.update(
                {
                    "GIT_CONFIG_GLOBAL": os.devnull,
                    "GIT_CONFIG_NOSYSTEM": "1",
                    "GIT_AUTHOR_NAME": "Foldkit fixture",
                    "GIT_AUTHOR_EMAIL": "fixture@example.invalid",
                    "GIT_COMMITTER_NAME": "Foldkit fixture",
                    "GIT_COMMITTER_EMAIL": "fixture@example.invalid",
                }
            )
            subprocess.run(
                ["git", "-C", str(source), "init", "--quiet"],
                check=True,
                env=git_environment,
            )
            subprocess.run(
                ["git", "-C", str(source), "add", "."],
                check=True,
                env=git_environment,
            )
            subprocess.run(
                [
                    "git",
                    "-C",
                    str(source),
                    "-c",
                    "commit.gpgsign=false",
                    "commit",
                    "--quiet",
                    "-m",
                    "fixture",
                ],
                check=True,
                env=git_environment,
            )
            commit = subprocess.check_output(
                ["git", "-C", str(source), "rev-parse", "HEAD"],
                text=True,
                env=git_environment,
            ).strip()
            tree = subprocess.check_output(
                ["git", "-C", str(source), "rev-parse", "HEAD^{tree}"],
                text=True,
                env=git_environment,
            ).strip()
            original_write_json = MODULE.write_json

            def fail_corpus_manifest(path: pathlib.Path, value: object) -> None:
                if path.name.endswith("-corpus-manifest.json"):
                    raise OSError("injected corpus manifest publication failure")
                original_write_json(path, value)

            with mock.patch.multiple(
                MODULE,
                COMMIT=commit,
                TREE=tree,
                SOURCE_URL="https://example.invalid/foldkit/fixture",
                EXAMPLE_IDS=("toy",),
                FOCUS={"toy": "Fixture example"},
                write_json=mock.Mock(side_effect=fail_corpus_manifest),
            ):
                with self.assertRaisesRegex(
                    OSError, "injected corpus manifest publication failure"
                ):
                    MODULE.capture(source, repository_root)
            self.assertFalse(
                (repository_root / MODULE.SNAPSHOT_RELATIVE).exists()
            )
            self.assertFalse(
                (
                    repository_root / MODULE.CORPUS_RELATIVE / "manifest.json"
                ).exists()
            )
            parent = (repository_root / MODULE.SNAPSHOT_RELATIVE).parent
            self.assertEqual(list(parent.glob(".foldkit-capture-*")), [])


if __name__ == "__main__":
    unittest.main()
