#!/usr/bin/env python3
"""Import the pinned Effect test corpus as reference-only conformance data.

The cache checkout is read through git archive/show and is never modified. The
import is intentionally separate from the runnable benchmark fixtures: these
files describe upstream behavior and are not claims that Effra executes or
passes the upstream suite.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import os
import re
import shutil
import stat
import subprocess
import tarfile
import tempfile
from pathlib import Path
from typing import Iterable


COMMIT = "460272d30457f4697d8b8c52cad41caccbcace08"
TAG = "effect@4.0.1"
COMPONENTS = frozenset({"test", "tests", "typetest", "test-dts"})
CANONICAL_ENCODING = "json-sorted-compact-utf8-v1"
RELEASE_REFERENCE_COUNT = 746
RELEASE_LICENSE_COUNT = 26
RELEASE_REFERENCE_IDENTITY_SHA256 = "9b9a09038a2b3d5e15256aed9d37401a06e911569d8fcc4ac3f1368fbbe8e001"
RELEASE_LICENSE_IDENTITY_SHA256 = "5d9c07de330f6f9de1cdf0306d7c25fde646691edcaba92fd740fdeeb1663b90"
RELEASE_ROOT_IDENTITY_SHA256 = "48f6287ce974209b0860671f0e62b0ef0a2f5436836f7e7c2aa935c4eb95c422"
LICENSE_NAMES = ("LICENSE", "LICENCE", "COPYING", "LICENSE.md", "LICENCE.md", "COPYING.md")
OUTPUT_RELATIVE = Path("conformance/upstream/effect-4.0.1")
MANIFEST_NAME = "manifest.json"
README_NAME = "README.md"
README_CONTENT = (
    "# Effect 4.0.1 upstream conformance reference\n\n"
    "This directory is a byte-preserved reference snapshot from the pinned\n"
    "Effect source commit recorded in `manifest.json`. It is not executed by\n"
    "Effra's gate and does not claim that Effra passes the upstream suite.\n"
    "The applicable package license for each copied file is recorded in\n"
    "`manifest.json`; all copied license texts are preserved byte-for-byte.\n"
)


class ImportError(RuntimeError):
    """A source, archive, or manifest invariant failed."""


def run_git(source: Path, *args: str, text: bool = True) -> str | bytes:
    result = subprocess.run(
        ["git", "-C", str(source), *args],
        check=False,
        capture_output=True,
        text=text,
    )
    if result.returncode != 0:
        detail = result.stderr if text else result.stderr.decode("utf-8", errors="replace")
        raise ImportError(f"git {' '.join(args)} failed: {detail.strip()}")
    return result.stdout


def selected_paths(source: Path, commit: str) -> list[str]:
    listing = str(run_git(source, "ls-tree", "-r", "--name-only", commit))
    paths = []
    for line in listing.splitlines():
        path = line.strip()
        if path and any(component in COMPONENTS for component in Path(path).parts):
            paths.append(path)
    return sorted(paths)


def safe_relative_path(value: object) -> str:
    if not isinstance(value, str) or not value or "\\" in value or "\x00" in value:
        raise ImportError(f"unsafe snapshot path: {value!r}")
    path = Path(value)
    if path.is_absolute() or ".." in path.parts or "." in path.parts:
        raise ImportError(f"unsafe snapshot path: {value!r}")
    return value


def tracked_paths(source: Path, commit: str) -> set[str]:
    listing = str(run_git(source, "ls-tree", "-r", "--name-only", commit))
    return {path for path in listing.splitlines() if path}


def nearest_license(source_paths: set[str], relative: str) -> str:
    parts = Path(relative).parts
    for depth in range(len(parts) - 1, 0, -1):
        directory = Path(*parts[:depth])
        for name in LICENSE_NAMES:
            candidate = str(directory / name)
            if candidate in source_paths:
                return candidate
    for name in LICENSE_NAMES:
        if name in source_paths:
            return name
    raise ImportError(f"no applicable license for selected file: {relative}")


def license_map(source: Path, commit: str, paths: list[str]) -> dict[str, str]:
    source_paths = tracked_paths(source, commit)
    result = {relative: nearest_license(source_paths, relative) for relative in paths}
    result.update({license_path: license_path for license_path in set(result.values())})
    return result


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def canonical_sha256(value: object) -> str:
    encoded = json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return sha256_bytes(encoded)


def canonical_content_paths(files: list[dict[str, object]]) -> list[str]:
    return [
        str(item["path"])
        for item in sorted(
            files,
            key=lambda item: (str(item.get("path")) != "LICENSE", str(item.get("path"))),
        )
    ]


def manifest_identity(manifest: dict[str, object]) -> dict[str, object]:
    source = manifest["source"]
    selection = manifest["selection"]
    files = manifest["files"]
    assert isinstance(source, dict)
    assert isinstance(selection, dict)
    assert isinstance(files, list)
    references = [
        {
            "path": item["path"],
            "license": item["license"],
            "bytes": item["bytes"],
            "sha256": item["sha256"],
        }
        for item in files
        if isinstance(item, dict) and item.get("kind") == "reference"
    ]
    licenses = [
        {
            "path": item["path"],
            "bytes": item["bytes"],
            "sha256": item["sha256"],
        }
        for item in files
        if isinstance(item, dict) and item.get("kind") == "license"
    ]
    return {
        "schemaVersion": manifest["schemaVersion"],
        "source": {
            "repository": source["repository"],
            "tag": source["tag"],
            "commit": source["commit"],
            "url": source["url"],
        },
        "selection": {
            "kind": selection["kind"],
            "components": selection["components"],
            "count": selection["count"],
        },
        "references": references,
        "licenses": licenses,
    }


def integrity_for_manifest(manifest: dict[str, object]) -> dict[str, str]:
    identity = manifest_identity(manifest)
    references = identity["references"]
    licenses = identity["licenses"]
    assert isinstance(references, list)
    assert isinstance(licenses, list)
    return {
        "algorithm": "sha256",
        "canonicalEncoding": CANONICAL_ENCODING,
        "referenceIdentitySha256": canonical_sha256(references),
        "licenseIdentitySha256": canonical_sha256(licenses),
        "rootSha256": canonical_sha256(identity),
    }


def _is_sha256(value: object) -> bool:
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{64}", value) is not None


def _is_byte_count(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value >= 0


def git_blob_bytes(source: Path, commit: str, relative: str) -> bytes:
    safe_relative_path(relative)
    return bytes(run_git(source, "show", f"{commit}:{relative}", text=False))


def archive_paths(source: Path, commit: str, paths: Iterable[str], destination: Path) -> None:
    names = list(dict.fromkeys(safe_relative_path(path) for path in paths))
    if not names:
        raise ImportError("snapshot selection is empty")
    archive = subprocess.run(
        ["git", "-C", str(source), "archive", "--format=tar", commit, *names],
        check=False,
        capture_output=True,
    )
    if archive.returncode != 0:
        raise ImportError(
            f"git archive failed: {archive.stderr.decode('utf-8', errors='replace').strip()}"
        )
    destination.mkdir(parents=True, exist_ok=True)
    with tarfile.open(fileobj=io.BytesIO(archive.stdout), mode="r:") as stream:
        for member in stream.getmembers():
            relative = Path(member.name)
            if relative.is_absolute() or ".." in relative.parts:
                raise ImportError(f"archive contains unsafe path: {member.name!r}")
            target = destination / relative
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            if not member.isfile():
                raise ImportError(f"archive contains unsupported entry: {member.name!r}")
            target.parent.mkdir(parents=True, exist_ok=True)
            extracted = stream.extractfile(member)
            if extracted is None:
                raise ImportError(f"could not read archive member: {member.name!r}")
            target.write_bytes(extracted.read())


def manifest_for(root: Path, paths: list[str], commit: str, tag: str, licenses: dict[str, str]) -> dict[str, object]:
    content_paths = sorted(set(licenses) | set(paths), key=lambda value: (value != "LICENSE", value))
    files = []
    for relative in content_paths:
        is_license = relative in set(licenses.values())
        files.append({
            "path": relative,
            "kind": "license" if is_license else "reference",
            "license": relative if is_license else licenses[relative],
            "bytes": (root / relative).stat().st_size,
            "sha256": sha256_file(root / relative),
        })
    manifest: dict[str, object] = {
        "schemaVersion": 2,
        "status": "reference-only-not-executed",
        "source": {
            "repository": "effect-ts/effect",
            "url": f"https://github.com/Effect-TS/effect/tree/{commit}",
            "commit": commit,
            "tag": tag
        },
        "selection": {
            "kind": "tracked-files-with-path-component",
            "components": sorted(COMPONENTS),
            "count": len(paths)
        },
        "licenses": {
            "paths": sorted(set(licenses.values()), key=lambda value: (value != "LICENSE", value)),
            "mapping": {relative: licenses[relative] for relative in paths},
        },
        "files": files
    }
    manifest["provenance"] = {
        "kind": "release-pinned" if commit == COMMIT and tag == TAG else "custom-self-consistent",
        "canonicalEncoding": CANONICAL_ENCODING,
    }
    manifest["integrity"] = integrity_for_manifest(manifest)
    return manifest


def expected_extra_files() -> set[str]:
    return {MANIFEST_NAME, README_NAME}


def validate_source_pin(source: Path, commit: str, tag: str) -> None:
    if tag.startswith("custom:"):
        if len(commit) != 40:
            raise ImportError("custom source pin must use a full 40-character commit")
        resolved = str(run_git(source, "rev-parse", f"{commit}^{{commit}}" )).strip()
        if resolved != commit:
            raise ImportError(f"custom source pin does not resolve to {commit}")
        return
    resolved = str(run_git(source, "rev-parse", f"{tag}^{{commit}}")).strip()
    if resolved != commit:
        raise ImportError(f"tag {tag!r} resolves to {resolved}, not pinned commit {commit}")


def read_manifest(output: Path) -> dict[str, object]:
    if output.is_symlink() or not output.is_dir():
        raise ImportError(f"snapshot output is not a regular directory: {output}")
    manifest_path = output / MANIFEST_NAME
    if manifest_path.is_symlink() or not manifest_path.is_file():
        raise ImportError(f"manifest is missing: {manifest_path}")
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as error:
        raise ImportError(f"manifest is not valid UTF-8 JSON: {manifest_path}") from error
    if not isinstance(manifest, dict):
        raise ImportError("manifest root is not an object")
    return manifest


def validate_manifest_structure(output: Path, manifest: dict[str, object]) -> dict[str, object]:
    expected_keys = {"files", "integrity", "licenses", "provenance", "schemaVersion", "selection", "source", "status"}
    if set(manifest) != expected_keys:
        raise ImportError("manifest schema keys are invalid")
    if manifest.get("schemaVersion") != 2:
        raise ImportError("manifest schemaVersion must be 2")
    if manifest.get("status") != "reference-only-not-executed":
        raise ImportError("manifest status is not reference-only-not-executed")

    source = manifest.get("source")
    if not isinstance(source, dict) or set(source) != {"commit", "repository", "tag", "url"}:
        raise ImportError("manifest source is invalid")
    commit = source.get("commit")
    tag = source.get("tag")
    if not isinstance(commit, str) or re.fullmatch(r"[0-9a-f]{40}", commit) is None:
        raise ImportError("manifest source commit is invalid")
    if not isinstance(tag, str) or (tag != TAG and not tag.startswith("custom:")):
        raise ImportError("manifest source tag is invalid")
    if tag.startswith("custom:") and len(commit) != 40:
        raise ImportError("custom source pin must use a full 40-character commit")
    if source.get("repository") != "effect-ts/effect" or source.get("url") != f"https://github.com/Effect-TS/effect/tree/{commit}":
        raise ImportError("manifest source URL or repository is invalid")

    selection = manifest.get("selection")
    if not isinstance(selection, dict) or set(selection) != {"components", "count", "kind"}:
        raise ImportError("manifest selection is invalid")
    if selection.get("kind") != "tracked-files-with-path-component" or selection.get("components") != sorted(COMPONENTS):
        raise ImportError("manifest selection kind or components are invalid")
    if not _is_byte_count(selection.get("count")):
        raise ImportError("manifest selection count is invalid")

    provenance = manifest.get("provenance")
    expected_provenance_kind = "release-pinned" if commit == COMMIT and tag == TAG else "custom-self-consistent"
    if not isinstance(provenance, dict) or set(provenance) != {"canonicalEncoding", "kind"} or provenance.get("kind") != expected_provenance_kind or provenance.get("canonicalEncoding") != CANONICAL_ENCODING:
        raise ImportError("manifest provenance declaration is invalid")

    files = manifest.get("files")
    if not isinstance(files, list) or not files:
        raise ImportError("manifest files is missing or empty")
    entries: dict[str, dict[str, object]] = {}
    for item in files:
        if not isinstance(item, dict) or set(item) != {"bytes", "kind", "license", "path", "sha256"}:
            raise ImportError("manifest file entry is invalid")
        relative = safe_relative_path(item.get("path"))
        if relative in entries:
            raise ImportError(f"manifest contains duplicate path: {relative}")
        kind = item.get("kind")
        if not isinstance(kind, str) or kind not in {"license", "reference"} or not isinstance(item.get("license"), str):
            raise ImportError(f"manifest file classification is invalid: {relative}")
        if not _is_byte_count(item.get("bytes")) or not _is_sha256(item.get("sha256")):
            raise ImportError(f"manifest file metadata is invalid: {relative}")
        if item.get("kind") == "license" and item.get("license") != relative:
            raise ImportError(f"license entry is invalid: {relative}")
        entries[relative] = item
    if [str(item["path"]) for item in files] != canonical_content_paths(files):
        raise ImportError("manifest files are not in canonical order")

    reference_paths = [relative for relative, item in entries.items() if item["kind"] == "reference"]
    license_paths_from_files = [relative for relative, item in entries.items() if item["kind"] == "license"]
    if selection["count"] != len(reference_paths):
        raise ImportError("manifest selection count does not match reference files")
    if not reference_paths or not license_paths_from_files:
        raise ImportError("manifest must contain references and licenses")
    license_info = manifest.get("licenses")
    if not isinstance(license_info, dict) or set(license_info) != {"mapping", "paths"}:
        raise ImportError("manifest license index is invalid")
    license_paths = license_info.get("paths")
    mapping = license_info.get("mapping")
    if not isinstance(license_paths, list) or any(not isinstance(path, str) for path in license_paths):
        raise ImportError("manifest license paths are invalid")
    if not isinstance(mapping, dict) or any(
        not isinstance(path, str) or not isinstance(target, str)
        for path, target in mapping.items()
    ):
        raise ImportError("manifest license mapping is invalid")
    expected_license_paths = sorted(license_paths_from_files, key=lambda value: (value != "LICENSE", value))
    if license_paths != expected_license_paths or list(mapping) != reference_paths:
        raise ImportError("manifest license ordering or mapping is invalid")
    if set(mapping.values()) != set(license_paths_from_files):
        raise ImportError("manifest license targets do not match license entries")
    for relative in reference_paths:
        if mapping[relative] not in entries or entries[mapping[relative]]["kind"] != "license":
            raise ImportError(f"reference license target is missing: {relative}")
        if entries[relative]["license"] != mapping[relative]:
            raise ImportError(f"reference license mapping is invalid: {relative}")

    integrity = manifest.get("integrity")
    expected_integrity = integrity_for_manifest(manifest)
    if integrity != expected_integrity:
        raise ImportError("manifest canonical identity digest is invalid")

    if output.joinpath(README_NAME).is_symlink() or not output.joinpath(README_NAME).is_file() or output.joinpath(README_NAME).read_text(encoding="utf-8") != README_CONTENT:
        raise ImportError("snapshot README is not the importer-owned README")
    expected_files = set(entries) | expected_extra_files()
    actual_files: set[str] = set()
    actual_directories: set[str] = set()
    for path in output.rglob("*"):
        mode = path.lstat().st_mode
        if stat.S_ISLNK(mode):
            raise ImportError(f"snapshot contains a symlink: {path}")
        if stat.S_ISDIR(mode):
            actual_directories.add(str(path.relative_to(output)))
            continue
        if stat.S_ISREG(mode):
            relative = safe_relative_path(str(path.relative_to(output)))
            actual_files.add(relative)
            continue
        raise ImportError(f"snapshot contains unsupported filesystem entry: {path}")
    if actual_files != expected_files:
        extras = sorted(actual_files - expected_files)
        missing = sorted(expected_files - actual_files)
        raise ImportError(f"snapshot file set differs; extras={extras}, missing={missing}")
    expected_directories: set[str] = set()
    for relative in expected_files:
        path = Path(relative).parent
        while str(path) != ".":
            expected_directories.add(str(path))
            path = path.parent
    if actual_directories != expected_directories:
        extras = sorted(actual_directories - expected_directories)
        missing = sorted(expected_directories - actual_directories)
        raise ImportError(f"snapshot directory set differs; extras={extras}, missing={missing}")
    for relative, item in entries.items():
        target = output / relative
        if target.is_symlink() or not target.is_file():
            raise ImportError(f"snapshot entry is not a regular file: {relative}")
        if target.stat().st_size != item["bytes"] or sha256_file(target) != item["sha256"]:
            raise ImportError(f"hash mismatch for imported file: {relative}")
    return manifest_identity(manifest)


def validate_release_identity(manifest: dict[str, object]) -> None:
    source = manifest["source"]
    selection = manifest["selection"]
    integrity = manifest["integrity"]
    identity = manifest_identity(manifest)
    assert isinstance(source, dict)
    assert isinstance(selection, dict)
    assert isinstance(integrity, dict)
    if source.get("commit") != COMMIT or source.get("tag") != TAG or manifest["provenance"]["kind"] != "release-pinned":
        raise ImportError("manifest source is not the pinned Effect 4.0.1 release")
    if selection.get("count") != RELEASE_REFERENCE_COUNT or len(identity["licenses"]) != RELEASE_LICENSE_COUNT:
        raise ImportError("manifest release selection counts are invalid")
    if integrity.get("referenceIdentitySha256") != RELEASE_REFERENCE_IDENTITY_SHA256 or integrity.get("licenseIdentitySha256") != RELEASE_LICENSE_IDENTITY_SHA256 or integrity.get("rootSha256") != RELEASE_ROOT_IDENTITY_SHA256:
        raise ImportError("manifest release identity digest is not the independently pinned corpus")


def validate(source: Path, output: Path, manifest: dict[str, object]) -> None:
    """Validate a source-backed snapshot using immutable git blobs."""
    validate_manifest_structure(output, manifest)
    source_info = manifest["source"]
    assert isinstance(source_info, dict)
    commit = source_info["commit"]
    tag = source_info["tag"]
    assert isinstance(commit, str)
    assert isinstance(tag, str)
    validate_source_pin(source, commit, tag)
    paths = selected_paths(source, commit)
    expected_licenses = license_map(source, commit, paths)
    expected_content = sorted(set(expected_licenses) | set(expected_licenses.values()), key=lambda value: (value != "LICENSE", value))
    selection = manifest["selection"]
    assert isinstance(selection, dict)
    if selection.get("kind") != "tracked-files-with-path-component" or selection.get("components") != sorted(COMPONENTS) or selection.get("count") != len(paths):
        raise ImportError("manifest selection metadata differs from the pinned git tree")
    files = manifest["files"]
    assert isinstance(files, list)
    entries = {str(item["path"]): item for item in files if isinstance(item, dict)}
    if [str(item["path"]) for item in files] != expected_content:
        raise ImportError("manifest file selection does not match the pinned git tree")
    expected_license_paths = sorted(set(expected_licenses.values()), key=lambda value: (value != "LICENSE", value))
    license_info = manifest["licenses"]
    assert isinstance(license_info, dict)
    expected_mapping = {relative: expected_licenses[relative] for relative in paths}
    if license_info.get("paths") != expected_license_paths or license_info.get("mapping") != expected_mapping:
        raise ImportError("manifest license mapping differs from the source tree")
    for relative in expected_content:
        item = entries[relative]
        source_bytes = git_blob_bytes(source, commit, relative)
        if len(source_bytes) != item["bytes"] or sha256_bytes(source_bytes) != item["sha256"]:
            raise ImportError(f"source blob hash mismatch for manifest file: {relative}")
        if relative in set(expected_licenses.values()):
            if item["kind"] != "license" or item["license"] != relative:
                raise ImportError(f"license entry is invalid: {relative}")
        elif item["kind"] != "reference" or item["license"] != expected_licenses[relative]:
            raise ImportError(f"reference license mapping is invalid: {relative}")
    if commit == COMMIT and tag == TAG:
        validate_release_identity(manifest)


def validate_self_contained(output: Path, *, allow_custom: bool = False) -> dict[str, object]:
    """Verify an imported snapshot without trusting a source working tree."""
    manifest = read_manifest(output)
    validate_manifest_structure(output, manifest)
    source = manifest["source"]
    assert isinstance(source, dict)
    if source.get("commit") == COMMIT and source.get("tag") == TAG:
        validate_release_identity(manifest)
    elif allow_custom and isinstance(source.get("tag"), str) and source["tag"].startswith("custom:"):
        pass
    else:
        raise ImportError("custom snapshot is self-consistency-only and is not release eligible")
    return manifest


def write_readme(root: Path) -> None:
    root.joinpath(README_NAME).write_text(README_CONTENT, encoding="utf-8")


def owned_snapshot(output: Path) -> bool:
    if output.is_symlink() or not output.is_dir():
        return False
    try:
        validate_self_contained(output, allow_custom=True)
    except (ImportError, OSError, UnicodeError, json.JSONDecodeError):
        return False
    return True


def replace_owned_snapshot(staged: Path, output: Path) -> None:
    output.parent.mkdir(parents=True, exist_ok=True)
    backup: Path | None = None
    if not owned_snapshot(staged):
        raise ImportError(f"refusing to replace with an unverified staged snapshot: {staged}")
    if output.is_symlink() or output.exists():
        if not owned_snapshot(output):
            raise ImportError(f"refusing to replace non-owned output path: {output}")
        backup = Path(tempfile.mkdtemp(prefix=f".{output.name}.backup-", dir=output.parent))
        backup.rmdir()
        output.rename(backup)
    try:
        os.replace(staged, output)
    except BaseException:
        if backup is not None and not output.exists():
            backup.rename(output)
        raise
    if backup is not None:
        shutil.rmtree(backup)


def import_snapshot(source: Path, output: Path, commit: str, tag: str) -> None:
    paths = selected_paths(source, commit)
    if not (source / ".git").exists() and not (source / "HEAD").exists():
        # Worktrees can use a .git file; this check produces a clearer failure
        # for accidental package directories.
        raise ImportError(f"source is not a git checkout: {source}")
    licenses = license_map(source, commit, paths)
    content_paths = sorted(set(licenses) | set(licenses.values()), key=lambda value: (value != "LICENSE", value))
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = Path(tempfile.mkdtemp(prefix="effect-conformance-", dir=output.parent))
    staged = temporary / "snapshot"
    try:
        archive_paths(source, commit, content_paths, staged)
        write_readme(staged)
        manifest = manifest_for(staged, paths, commit, tag, licenses)
        staged.joinpath(MANIFEST_NAME).write_text(
            json.dumps(manifest, indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        validate(source, staged, manifest)
        replace_owned_snapshot(staged, output)
    finally:
        shutil.rmtree(temporary, ignore_errors=True)


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("source", type=Path, nargs="?", help="read-only Effect git checkout")
    result.add_argument("--commit", default=COMMIT)
    result.add_argument("--tag", default=TAG)
    result.add_argument("--output", type=Path, default=Path(OUTPUT_RELATIVE))
    result.add_argument("--check", action="store_true", help="validate an existing import")
    result.add_argument("--self-check", action="store_true", help="validate committed files without the source checkout")
    return result


def main() -> int:
    args = parser().parse_args()
    output = args.output.resolve()
    if args.self_check:
        validate_self_contained(output)
        print(output)
        return 0
    if args.source is None:
        raise ImportError("source is required unless --self-check is used")
    source = args.source.resolve()
    if args.check:
        manifest = read_manifest(output)
        validate(source, output, manifest)
    else:
        import_snapshot(source, output, args.commit, args.tag)
    print(output)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ImportError as error:
        raise SystemExit(f"import_effect_conformance: {error}")
