"""Negative tests for the pinned, reference-only Effect snapshot importer."""

from __future__ import annotations

import importlib.util
import json
import os
import pathlib
import shutil
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "import_effect_conformance", ROOT / "scripts" / "import_effect_conformance.py"
)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class SnapshotIntegrityTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="effect-conformance-test-")
        self.root = pathlib.Path(self.temporary.name)
        self.source = self.root / "source"
        (self.source / "packages/effect/test").mkdir(parents=True)
        (self.source / "packages/other/test").mkdir(parents=True)
        (self.source / "LICENSE").write_bytes(b"root-license\n")
        (self.source / "packages/effect/LICENSE").write_bytes(b"effect-license\n")
        (self.source / "packages/effect/test/one.test.ts").write_bytes(b"export const one = 1\n")
        (self.source / "packages/other/test/two.test.ts").write_bytes(b"export const two = 2\n")
        git_env = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
        git = ["git", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + os.devnull, "-c", "init.templateDir="]
        template = self.root / "empty-template"
        template.mkdir()
        subprocess.run([*git, "init", "--template=" + str(template), "-q", str(self.source)], check=True, env=git_env)
        subprocess.run([*git, "-C", str(self.source), "config", "user.email", "test@example.invalid"], check=True, env=git_env)
        subprocess.run([*git, "-C", str(self.source), "config", "user.name", "Snapshot Test"], check=True, env=git_env)
        subprocess.run([*git, "-C", str(self.source), "add", "."], check=True, env=git_env)
        subprocess.run([*git, "-C", str(self.source), "commit", "-qm", "fixture"], check=True, env=git_env)
        self.commit = subprocess.check_output([*git, "-C", str(self.source), "rev-parse", "HEAD"], text=True, env=git_env).strip()
        self.custom_tag = f"custom:{self.commit}"
        self.output = self.root / "snapshot"
        MODULE.import_snapshot(self.source, self.output, self.commit, self.custom_tag)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def manifest(self, output: pathlib.Path) -> dict[str, object]:
        return json.loads((output / "manifest.json").read_text(encoding="utf-8"))

    def assert_invalid(self, output: pathlib.Path) -> None:
        with self.assertRaises(MODULE.ImportError):
            MODULE.validate(self.source, output, self.manifest(output))

    def copy_snapshot(self, name: str) -> pathlib.Path:
        output = self.root / name
        shutil.copytree(self.output, output)
        return output

    def test_generated_snapshot_is_source_backed_and_maps_nearest_licenses(self) -> None:
        manifest = self.manifest(self.output)
        self.assertEqual(manifest["selection"]["count"], 2)
        self.assertEqual(manifest["licenses"]["mapping"]["packages/effect/test/one.test.ts"], "packages/effect/LICENSE")
        self.assertEqual(manifest["licenses"]["mapping"]["packages/other/test/two.test.ts"], "LICENSE")
        MODULE.validate(self.source, self.output, manifest)

    def test_custom_snapshot_is_not_release_eligible(self) -> None:
        with self.assertRaises(MODULE.ImportError):
            MODULE.validate_self_contained(self.output)
        MODULE.validate_self_contained(self.output, allow_custom=True)

    def test_missing_license_fails(self) -> None:
        output = self.copy_snapshot("missing-license")
        (output / "packages/effect/LICENSE").unlink()
        self.assert_invalid(output)

    def test_wrong_schema_fails(self) -> None:
        output = self.copy_snapshot("wrong-schema")
        manifest = self.manifest(output)
        manifest["schemaVersion"] = 1
        (output / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.assert_invalid(output)

    def test_wrong_pin_fails(self) -> None:
        output = self.copy_snapshot("wrong-pin")
        manifest = self.manifest(output)
        manifest["source"]["commit"] = "0" * 40
        manifest["source"]["url"] = "https://github.com/Effect-TS/effect/tree/" + "0" * 40
        (output / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.assert_invalid(output)

    def test_duplicate_path_fails(self) -> None:
        output = self.copy_snapshot("duplicate-path")
        manifest = self.manifest(output)
        manifest["files"].append(dict(manifest["files"][0]))
        (output / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.assert_invalid(output)

    def test_changed_bytes_fail(self) -> None:
        output = self.copy_snapshot("changed-bytes")
        (output / "packages/effect/test/one.test.ts").write_bytes(b"changed\n")
        self.assert_invalid(output)

    def test_malformed_types_are_import_errors(self) -> None:
        output = self.copy_snapshot("malformed-types")
        manifest = self.manifest(output)
        manifest["files"][0]["kind"] = []
        (output / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.assert_invalid(output)

        output = self.copy_snapshot("malformed-license-mapping")
        manifest = self.manifest(output)
        reference_path = next(item["path"] for item in manifest["files"] if item["kind"] == "reference")
        manifest["licenses"]["mapping"][reference_path] = []
        (output / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.assert_invalid(output)

    def test_special_files_are_rejected_and_preserved(self) -> None:
        if not hasattr(os, "mkfifo"):
            self.skipTest("FIFO creation is unavailable on this platform")
        output = self.copy_snapshot("special-file")
        fifo = output / "unlisted.fifo"
        os.mkfifo(fifo)
        self.assert_invalid(output)
        with self.assertRaises(MODULE.ImportError):
            MODULE.validate_self_contained(output, allow_custom=True)
        self.assertFalse(MODULE.owned_snapshot(output))
        self.assertTrue(fifo.exists())

        staged = self.copy_snapshot("special-file-staged")
        staged_fifo = staged / "unlisted.fifo"
        os.mkfifo(staged_fifo)
        with self.assertRaises(MODULE.ImportError):
            MODULE.replace_owned_snapshot(staged, self.output)
        self.assertTrue(staged_fifo.exists())
        self.assertTrue((self.output / "manifest.json").is_file())

    def test_jointly_changed_source_and_manifest_still_fails(self) -> None:
        output = self.copy_snapshot("joint-change")
        target = output / "packages/effect/test/one.test.ts"
        target.write_bytes(b"changed-and-rehashed\n")
        manifest = self.manifest(output)
        entry = next(item for item in manifest["files"] if item["path"] == "packages/effect/test/one.test.ts")
        entry["bytes"] = target.stat().st_size
        entry["sha256"] = MODULE.sha256_file(target)
        (output / "manifest.json").write_text(json.dumps(manifest), encoding="utf-8")
        self.assert_invalid(output)

    def test_non_owned_output_is_not_replaced(self) -> None:
        output = self.root / "non-owned"
        output.mkdir()
        (output / "README.md").write_text("unrelated", encoding="utf-8")
        (output / "manifest.json").write_text('{"name":"unrelated"}', encoding="utf-8")
        (output / "keep.txt").write_text("keep", encoding="utf-8")
        with self.assertRaises(MODULE.ImportError):
            MODULE.import_snapshot(self.source, output, self.commit, self.custom_tag)
        self.assertEqual((output / "keep.txt").read_text(encoding="utf-8"), "keep")

    def test_invalid_utf8_readme_and_deep_json_are_structured_refusals(self) -> None:
        for name, filename, contents in (
            ("invalid-readme", "README.md", b"\xff"),
            ("deep-manifest", "manifest.json", b"[" * 2000 + b"0" + b"]" * 2000),
        ):
            with self.subTest(name=name):
                output = self.copy_snapshot(name)
                (output / filename).write_bytes(contents)
                with self.assertRaises(MODULE.ImportError):
                    MODULE.validate_self_contained(output, allow_custom=True)
                self.assertFalse(MODULE.owned_snapshot(output))
                staged = self.copy_snapshot(name + "-staged")
                with self.assertRaises(MODULE.ImportError):
                    MODULE.replace_owned_snapshot(staged, output)
                self.assertEqual((output / filename).read_bytes(), contents)
                result = subprocess.run(
                    ["python3", "-B", str(ROOT / "scripts/import_effect_conformance.py"), "--self-check", "--output", str(output)],
                    capture_output=True, text=True, check=False,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("import_effect_conformance:", result.stderr)
                self.assertNotIn("Traceback", result.stderr)


class ReleaseSnapshotIntegrityTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="effect-release-conformance-test-")
        self.root = pathlib.Path(self.temporary.name)
        self.source = ROOT / "conformance" / "upstream" / "effect-4.0.1"
        self.output = self.root / "snapshot"
        shutil.copytree(self.source, self.output)

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def manifest(self) -> dict[str, object]:
        return json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))

    def write_manifest(self, manifest: dict[str, object]) -> None:
        (self.output / "manifest.json").write_text(
            json.dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8"
        )

    def assert_invalid(self) -> None:
        with self.assertRaises(MODULE.ImportError):
            MODULE.validate_self_contained(self.output)

    def test_committed_release_snapshot_is_exactly_pinned(self) -> None:
        manifest = MODULE.validate_self_contained(self.source)
        self.assertEqual((self.source / "manifest.json").read_text(encoding="utf-8"), json.dumps(manifest, indent=2, sort_keys=True) + "\n")
        self.assertEqual(manifest["selection"]["count"], 746)
        self.assertEqual(len(manifest["licenses"]["paths"]), 26)
        self.assertEqual(manifest["source"]["commit"], MODULE.COMMIT)
        self.assertEqual(manifest["source"]["tag"], MODULE.TAG)

    def test_offline_release_negatives_reject_metadata_and_payload_together(self) -> None:
        original_manifest = self.manifest()

        manifest = self.manifest()
        manifest["source"]["commit"] = "0" * 40
        manifest["source"]["url"] = "https://github.com/Effect-TS/effect/tree/" + "0" * 40
        self.write_manifest(manifest)
        self.assert_invalid()
        self.write_manifest(original_manifest)

        manifest = self.manifest()
        manifest["selection"]["count"] = 745
        self.write_manifest(manifest)
        self.assert_invalid()
        self.write_manifest(original_manifest)

        manifest = self.manifest()
        manifest["files"][0]["path"] = "../escape"
        self.write_manifest(manifest)
        self.assert_invalid()
        self.write_manifest(original_manifest)

        manifest = self.manifest()
        manifest["files"].append(dict(manifest["files"][0]))
        self.write_manifest(manifest)
        self.assert_invalid()
        self.write_manifest(original_manifest)

        manifest = self.manifest()
        reference = next(item for item in manifest["files"] if item["kind"] == "reference")
        reference["license"] = "missing-license"
        manifest["licenses"]["mapping"][reference["path"]] = "missing-license"
        self.write_manifest(manifest)
        self.assert_invalid()
        self.write_manifest(original_manifest)

        manifest = self.manifest()
        manifest["files"][0]["bytes"] = "not-an-integer"
        self.write_manifest(manifest)
        self.assert_invalid()
        self.write_manifest(original_manifest)

        target = self.output / "packages/ai/anthropic/test/AnthropicClient.test.ts"
        original_bytes = target.read_bytes()
        target.write_bytes(b"jointly changed payload\n")
        manifest = self.manifest()
        entry = next(item for item in manifest["files"] if item["path"] == "packages/ai/anthropic/test/AnthropicClient.test.ts")
        entry["bytes"] = target.stat().st_size
        entry["sha256"] = MODULE.sha256_file(target)
        manifest["integrity"] = MODULE.integrity_for_manifest(manifest)
        self.write_manifest(manifest)
        with self.assertRaises(MODULE.ImportError) as error:
            MODULE.validate_self_contained(self.output)
        self.assertIn("independently pinned", str(error.exception))
        target.write_bytes(original_bytes)

        target.write_bytes(b"changed payload only\n")
        self.write_manifest(original_manifest)
        self.assert_invalid()

    def test_offline_rejects_fully_rehashed_payload_mutation(self) -> None:
        target = self.output / "packages/ai/anthropic/test/AnthropicClient.test.ts"
        original_bytes = target.read_bytes()
        original_manifest = self.manifest()
        try:
            target.write_bytes(b"fully changed payload with rehashed metadata\n")
            manifest = self.manifest()
            entry = next(item for item in manifest["files"] if item["path"] == "packages/ai/anthropic/test/AnthropicClient.test.ts")
            entry["bytes"] = target.stat().st_size
            entry["sha256"] = MODULE.sha256_file(target)
            manifest["integrity"] = MODULE.integrity_for_manifest(manifest)
            self.write_manifest(manifest)
            with self.assertRaises(MODULE.ImportError) as error:
                MODULE.validate_self_contained(self.output)
            self.assertIn("independently pinned", str(error.exception))
        finally:
            target.write_bytes(original_bytes)
            self.write_manifest(original_manifest)

    def test_offline_malformed_types_are_import_errors(self) -> None:
        original_manifest = self.manifest()
        try:
            manifest = self.manifest()
            manifest["files"][0]["kind"] = None
            self.write_manifest(manifest)
            self.assert_invalid()

            manifest = self.manifest()
            reference_path = next(item["path"] for item in manifest["files"] if item["kind"] == "reference")
            manifest["licenses"]["mapping"][reference_path] = {}
            self.write_manifest(manifest)
            self.assert_invalid()
        finally:
            self.write_manifest(original_manifest)


if __name__ == "__main__":
    unittest.main()
