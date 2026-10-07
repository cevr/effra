"""Controls for the pinned, reference-only Effect upstream submodule and its manifest."""

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
    "import_effect_conformance", ROOT / "scripts" / "import_effect_conformance.py"
)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)

GIT_ENV = dict(os.environ, GIT_CONFIG_GLOBAL=os.devnull, GIT_CONFIG_NOSYSTEM="1")
GIT = [
    "git", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + os.devnull, "-c", "init.templateDir=",
    "-c", "protocol.file.allow=always", "-c", "user.name=Snapshot Test", "-c", "user.email=test@example.invalid",
]


def git(repository: pathlib.Path, *args: str) -> str:
    return subprocess.run([*GIT, "-C", str(repository), *args], check=True, capture_output=True, text=True, env=GIT_ENV).stdout.strip()


def write(root: pathlib.Path, files: dict[str, bytes]) -> None:
    for relative, contents in files.items():
        (root / relative).parent.mkdir(parents=True, exist_ok=True)
        (root / relative).write_bytes(contents)


class SubmoduleReferenceTests(unittest.TestCase):
    """A fixture superproject whose submodule pins a fixture upstream commit."""

    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory(prefix="effect-upstream-test-")
        self.root = pathlib.Path(self.temporary.name).resolve()
        self.upstream = self.root / "upstream"
        write(self.upstream, {
            "LICENSE": b"root-license\n",
            "packages/effect/LICENSE": b"effect-license\n",
            "packages/effect/src/Effect.ts": b"export const source = 0\n",
            "packages/effect/test/one.test.ts": b"export const one = 1\n",
            "packages/other/test/two.test.ts": b"export const two = 2\n",
        })
        git(self.root, "init", "-q", str(self.upstream))
        git(self.upstream, "add", ".")
        git(self.upstream, "commit", "-qm", "pinned")
        self.commit = git(self.upstream, "rev-parse", "HEAD")
        write(self.upstream, {"packages/other/test/two.test.ts": b"export const two = 22\n"})
        git(self.upstream, "commit", "-qam", "later")
        self.later = git(self.upstream, "rev-parse", "HEAD")
        self.tag = f"custom:{self.commit}"

        self.effra = self.root / "effra"
        git(self.root, "init", "-q", str(self.effra))
        (self.effra / "scripts").mkdir()
        shutil.copy2(ROOT / "scripts" / "init_upstream.sh", self.effra / "scripts" / "init_upstream.sh")
        git(self.effra, "submodule", "add", "-q", str(self.upstream), str(MODULE.CHECKOUT_RELATIVE))
        self.checkout = self.effra / MODULE.CHECKOUT_RELATIVE
        git(self.checkout, "checkout", "-q", "--detach", self.commit)
        # Only an explicit mirror can satisfy initialization; the recorded URL does not exist.
        git(self.effra, "config", "-f", ".gitmodules", f"submodule.{MODULE.CHECKOUT_RELATIVE}.url", "file:///nonexistent/effect.git")
        git(self.effra, "add", ".")
        git(self.effra, "commit", "-qm", "pin upstream")
        MODULE.refresh(self.effra, self.commit, self.tag)
        git(self.effra, "add", str(MODULE.MANIFEST_RELATIVE))
        git(self.effra, "commit", "-qm", "record manifest")

    def tearDown(self) -> None:
        self.temporary.cleanup()

    def verify(self) -> dict[str, object]:
        return MODULE.verify(self.effra, self.commit, self.tag).manifest

    def assert_refused(self, *fragments: str) -> str:
        with self.assertRaises(MODULE.ImportError) as refusal:
            MODULE.verify(self.effra, self.commit, self.tag)
        for fragment in fragments:
            self.assertIn(fragment, str(refusal.exception))
        return str(refusal.exception)

    def run_printed_repair(self, message: str, mirror: pathlib.Path | None = None) -> None:
        """Run each printed repair command from outside the Effra checkout, as a user passing --root would."""
        env = dict(GIT_ENV, EFFRA_UPSTREAM_MIRROR=str(mirror) if mirror else "")
        for command in message.rpartition("; run ")[2].split(", then "):
            subprocess.run(["sh", "-c", command], cwd=self.root, check=True, capture_output=True, env=env)

    def edit_hidden_from_status(self, target: str) -> None:
        """Edit a selected file that index flags hide; git status alone would call the checkout clean."""
        (self.checkout / target).write_bytes(b"edited behind git status\n")
        self.assertEqual(git(self.checkout, "status", "--porcelain", "--untracked-files=no"), "")

    def assert_flag_refusal_then_repair(self, target: str) -> None:
        message = self.assert_refused(
            f"hides selected tracked files from git status with assume-unchanged or skip-worktree (1, first {target})",
            f"run git -C {self.checkout} read-tree HEAD, then {self.effra}/scripts/init_upstream.sh --force",
        )
        # Every integrity-bearing read comes from the pinned objects, never the edited file.
        self.assertEqual(MODULE.read_pinned(self.checkout, self.commit, self.tag).text(target), "export const one = 1\n")
        # The printed repair clears the flags; only then can a forced checkout restore the file.
        self.run_printed_repair(message)
        self.assertEqual((self.checkout / target).read_bytes(), b"export const one = 1\n")
        self.verify()

    def test_assume_unchanged_edit_is_refused_and_never_read(self) -> None:
        target = "packages/effect/test/one.test.ts"
        git(self.checkout, "update-index", "--assume-unchanged", "--", target)
        self.edit_hidden_from_status(target)
        self.assert_flag_refusal_then_repair(target)

    def test_skip_worktree_edit_is_refused_and_never_read(self) -> None:
        target = "packages/effect/test/one.test.ts"
        git(self.checkout, "update-index", "--skip-worktree", "--", target)
        self.edit_hidden_from_status(target)
        self.assert_flag_refusal_then_repair(target)

    def test_ignore_stat_checkout_is_refused_and_never_read(self) -> None:
        target = "packages/effect/test/one.test.ts"
        git(self.checkout, "config", "core.ignoreStat", "true")
        # With core.ignoreStat, git marks every file it writes assume-unchanged.
        (self.checkout / target).unlink()
        git(self.checkout, "checkout", "--", target)
        self.assertEqual(git(self.checkout, "ls-files", "-v", "--", target), f"h {target}")
        self.edit_hidden_from_status(target)
        message = self.assert_refused(
            f"Effect upstream checkout {MODULE.CHECKOUT_RELATIVE} enables core.ignoreStat, which hides edits from git status",
            f"run git -C {self.checkout} config core.ignoreStat false",
        )
        self.run_printed_repair(message)
        self.assert_flag_refusal_then_repair(target)

    def test_unparseable_ignore_stat_names_every_origin_and_the_printed_repair_restores_it(self) -> None:
        # git refuses to open the checkout while any core.ignoreStat entry is not a boolean; initializing cannot help.
        settings = self.root / "global.gitconfig"
        settings.write_text("[core]\n\tignoreStat = maybe\n", encoding="utf-8")
        git(self.checkout, "config", "core.ignoreStat", "sometimes")
        local = self.effra / ".git" / "modules" / MODULE.CHECKOUT_RELATIVE / "config"
        with mock.patch.dict(os.environ, GIT_CONFIG_GLOBAL=str(settings)):
            message = self.assert_refused(
                f"Effect upstream checkout {MODULE.CHECKOUT_RELATIVE} cannot be opened because git rejects its core.ignoreStat setting "
                "(fatal: bad boolean config value 'maybe' for 'core.ignorestat')",
                f"run git config --file {settings} --replace-all core.ignoreStat false, then git config --file {local} --replace-all core.ignoreStat false",
            )
            self.assertNotIn("not initialized", message)
            self.run_printed_repair(message)
            self.verify()
            with mock.patch.dict(os.environ, GIT_CONFIG_PARAMETERS="'core.ignorestat'='maybe'"):
                self.assert_refused("; remove core.ignoreStat from the command line configuration")

    def test_refresh_records_selection_and_nearest_licenses(self) -> None:
        manifest = self.verify()
        self.assertEqual(manifest["selection"]["count"], 2)
        self.assertEqual(manifest["licenses"]["mapping"], {
            "packages/effect/test/one.test.ts": "packages/effect/LICENSE",
            "packages/other/test/two.test.ts": "LICENSE",
        })
        self.assertEqual([item["path"] for item in manifest["files"]], [
            "LICENSE", "packages/effect/LICENSE", "packages/effect/test/one.test.ts", "packages/other/test/two.test.ts",
        ])
        self.assertEqual(manifest["provenance"]["kind"], "custom-self-consistent")
        with self.assertRaises(MODULE.ImportError):
            MODULE.validate_release_identity(manifest)

    def test_uninitialized_checkout_names_init_and_offline_mirror_restores_it(self) -> None:
        git(self.effra, "submodule", "deinit", "-q", "-f", str(MODULE.CHECKOUT_RELATIVE))
        shutil.rmtree(self.effra / ".git" / "modules")
        self.assert_refused(f"is not initialized; run {self.effra}/scripts/init_upstream.sh")
        # Run from outside the repository: the printed repair must not depend on the current directory.
        result = subprocess.run(
            [sys.executable, "-B", str(ROOT / "scripts" / "import_effect_conformance.py"), "--root", str(self.effra)],
            cwd=self.root, capture_output=True, text=True, check=False, env=GIT_ENV,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stderr.splitlines(), [
            f"import_effect_conformance: Effect upstream checkout {MODULE.CHECKOUT_RELATIVE} is not initialized; run {self.effra}/scripts/init_upstream.sh",
        ])

        self.run_printed_repair(result.stderr.strip(), mirror=self.upstream)
        self.verify()
        self.assertEqual(git(self.checkout, "rev-parse", "--is-shallow-repository"), "true")
        self.assertEqual(git(self.effra, "status", "--porcelain"), "")

    def test_checkout_at_another_commit_names_init_which_restores_the_pin(self) -> None:
        git(self.checkout, "checkout", "-q", "--detach", self.later)
        message = self.assert_refused(f"is at {self.later}, not the pinned commit {self.commit}; run {self.effra}/scripts/init_upstream.sh")
        self.run_printed_repair(message)
        self.verify()

    def test_modified_tracked_file_is_refused_even_with_a_rehashed_manifest(self) -> None:
        target = self.checkout / "packages/effect/test/one.test.ts"
        target.write_bytes(b"changed and rehashed\n")
        manifest_path = self.effra / MODULE.MANIFEST_RELATIVE
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        entry = next(item for item in manifest["files"] if item["path"] == "packages/effect/test/one.test.ts")
        entry["bytes"] = target.stat().st_size
        entry["sha256"] = MODULE.sha256_bytes(target.read_bytes())
        manifest["integrity"] = MODULE.integrity_for_manifest(manifest)
        manifest_path.write_text(MODULE.render_manifest(manifest), encoding="utf-8")
        message = self.assert_refused("has modified tracked files", f"run {self.effra}/scripts/init_upstream.sh --force")
        self.run_printed_repair(message)
        # The checkout is restored; hashes still come from the pinned objects.
        self.assert_refused("manifest differs from the pinned checkout at /files[packages/effect/test/one.test.ts]/sha256")

    def test_gitlink_must_record_the_pinned_commit(self) -> None:
        with self.assertRaises(MODULE.ImportError) as refusal:
            MODULE.verify(self.effra, self.later, f"custom:{self.later}")
        self.assertIn(f"records {self.commit}, not the pinned commit {self.later}", str(refusal.exception))

    def test_manifest_must_equal_the_recomputed_canonical_manifest(self) -> None:
        manifest_path = self.effra / MODULE.MANIFEST_RELATIVE
        original = manifest_path.read_text(encoding="utf-8")
        for mutate, location in (
            (lambda value: value["selection"].update(count=3), "/selection/count"),
            (lambda value: value["files"].append(dict(value["files"][0])), "/files[length]"),
            (lambda value: value["licenses"]["mapping"].update({"packages/other/test/two.test.ts": "packages/effect/LICENSE"}), "/licenses/mapping/packages/other/test/two.test.ts"),
            (lambda value: value["source"].update(tag="effect@4.0.1"), "/source/tag"),
            (lambda value: value.update(status="passing"), "/status"),
        ):
            with self.subTest(location=location):
                manifest = json.loads(original)
                mutate(manifest)
                manifest_path.write_text(MODULE.render_manifest(manifest), encoding="utf-8")
                self.assert_refused(f"manifest differs from the pinned checkout at {location}")
        manifest_path.write_text(json.dumps(json.loads(original)), encoding="utf-8")
        self.assert_refused("manifest is not in canonical form")
        for contents in (b"\xff", b"[" * 100000 + b"0" + b"]" * 100000):
            manifest_path.write_bytes(contents)
            self.assert_refused("manifest is not readable UTF-8 JSON")

    def test_missing_git_is_a_structured_cli_refusal(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            result = subprocess.run(
                [sys.executable, "-B", str(ROOT / "scripts" / "import_effect_conformance.py"), "--root", str(self.effra)],
                capture_output=True, text=True, check=False, env=dict(GIT_ENV, PATH=directory),
            )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("import_effect_conformance: cannot execute git:", result.stderr)
        self.assertNotIn("Traceback", result.stderr)


class ReleaseReferenceTests(unittest.TestCase):
    """The repository's own submodule at the Effect 4.0.1 release pin."""

    def setUp(self) -> None:
        self.checkout = ROOT / MODULE.CHECKOUT_RELATIVE
        self.manifest = MODULE.verify(ROOT).manifest

    def test_committed_manifest_is_exactly_the_pinned_release(self) -> None:
        self.assertEqual(self.manifest["source"]["commit"], MODULE.COMMIT)
        self.assertEqual(self.manifest["source"]["tag"], MODULE.TAG)
        self.assertEqual(self.manifest["selection"]["count"], 746)
        self.assertEqual(len(self.manifest["licenses"]["paths"]), 26)
        self.assertEqual(self.manifest["integrity"]["rootSha256"], MODULE.RELEASE_ROOT_IDENTITY_SHA256)
        self.assertEqual(self.manifest["integrity"]["referenceIdentitySha256"], MODULE.RELEASE_REFERENCE_IDENTITY_SHA256)
        self.assertEqual(git(ROOT, "ls-files", "--stage", "--", str(MODULE.CHECKOUT_RELATIVE)).split()[:2], ["160000", MODULE.COMMIT])

    def test_rehashed_manifest_entry_is_rejected(self) -> None:
        manifest = json.loads(json.dumps(self.manifest))
        entry = next(item for item in manifest["files"] if item["path"] == "packages/ai/anthropic/test/AnthropicClient.test.ts")
        entry["sha256"] = MODULE.sha256_bytes(b"fully changed payload with rehashed metadata\n")
        manifest["integrity"] = MODULE.integrity_for_manifest(manifest)
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory) / "manifest.json"
            path.write_text(MODULE.render_manifest(manifest), encoding="utf-8")
            with self.assertRaises(MODULE.ImportError) as refusal:
                MODULE.verify_manifest(MODULE.read_pinned(self.checkout, MODULE.COMMIT, MODULE.TAG).manifest, path, MODULE.COMMIT, MODULE.TAG)
        self.assertIn("/files[packages/ai/anthropic/test/AnthropicClient.test.ts]/sha256", str(refusal.exception))

    def test_independent_release_identity_pins_the_selection_rule(self) -> None:
        with mock.patch.object(MODULE, "COMPONENTS", MODULE.COMPONENTS - {"typetest"}):
            changed = MODULE.read_pinned(self.checkout, MODULE.COMMIT, MODULE.TAG).manifest
        with self.assertRaises(MODULE.ImportError):
            MODULE.validate_release_identity(changed)


if __name__ == "__main__":
    unittest.main()
