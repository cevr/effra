#!/usr/bin/env python3
"""Verify or refresh the pinned Effect test corpus used as reference-only conformance data.

The upstream bytes live in the git submodule at conformance/upstream/effect,
pinned by its gitlink. Effra owns only the manifest: the selection rule, the
license accounting and the sha256 identity of every selected file. Verification
requires the checkout at the pinned commit with no tracked modifications,
recomputes the manifest from the commit's immutable git objects and compares it
with the committed manifest. These files describe upstream behavior; they are
not claims that Effra executes or passes the upstream suite.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
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
REGULAR_FILE_MODES = frozenset({"100644", "100755"})
CHECKOUT_RELATIVE = Path("conformance/upstream/effect")
MANIFEST_RELATIVE = Path("conformance/effect-upstream.manifest.json")
INIT_COMMAND = "scripts/init_upstream.sh"


class ImportError(RuntimeError):
    """A checkout, pin, or manifest invariant failed."""


def git_process(repository: Path, *args: str, text: bool = True, stdin: bytes | None = None) -> subprocess.CompletedProcess:
    try:
        return subprocess.run(
            ["git", "-C", str(repository), *args],
            check=False,
            capture_output=True,
            text=text,
            input=stdin,
        )
    except OSError as error:
        raise ImportError(f"cannot execute git: {error}") from error


def run_git(repository: Path, *args: str, text: bool = True) -> str | bytes:
    result = git_process(repository, *args, text=text)
    if result.returncode != 0:
        detail = result.stderr if text else result.stderr.decode("utf-8", errors="replace")
        raise ImportError(f"git {' '.join(args)} failed: {detail.strip()}")
    return result.stdout


def safe_relative_path(value: object) -> str:
    if not isinstance(value, str) or not value or any(character in value for character in "\\\x00\n"):
        raise ImportError(f"unsafe upstream path: {value!r}")
    path = Path(value)
    if path.is_absolute() or ".." in path.parts or "." in path.parts:
        raise ImportError(f"unsafe upstream path: {value!r}")
    return value


def tree_modes(checkout: Path, commit: str) -> dict[str, str]:
    listing = str(run_git(checkout, "ls-tree", "-r", "-z", commit))
    modes = {}
    for entry in listing.split("\x00"):
        if entry:
            header, path = entry.split("\t", 1)
            modes[path] = header.split(" ", 1)[0]
    return modes


def selected_paths(tracked: dict[str, str]) -> list[str]:
    return sorted(path for path in tracked if any(component in COMPONENTS for component in Path(path).parts))


def nearest_license(source_paths: dict[str, str], relative: str) -> str:
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


def license_map(tracked: dict[str, str], paths: list[str]) -> dict[str, str]:
    result = {relative: nearest_license(tracked, relative) for relative in paths}
    result.update({license_path: license_path for license_path in set(result.values())})
    return result


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def canonical_sha256(value: object) -> str:
    encoded = json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return sha256_bytes(encoded)


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


def blob_bytes(checkout: Path, commit: str, paths: list[str]) -> dict[str, bytes]:
    """Read each path from the commit's immutable objects with one git process."""
    request = "".join(f"{commit}:{safe_relative_path(path)}\n" for path in paths).encode("utf-8")
    result = git_process(checkout, "cat-file", "--batch", text=False, stdin=request)
    if result.returncode != 0:
        raise ImportError(f"git cat-file failed: {result.stderr.decode('utf-8', errors='replace').strip()}")
    output = result.stdout
    blobs: dict[str, bytes] = {}
    offset = 0
    for path in paths:
        end = output.index(b"\n", offset)
        header = output[offset:end].split()
        if len(header) != 3 or header[1] != b"blob":
            raise ImportError(f"pinned tree has no file at {path}")
        size = int(header[2])
        blobs[path] = output[end + 1:end + 1 + size]
        offset = end + 2 + size
    return blobs


def build_manifest(checkout: Path, commit: str, tag: str) -> dict[str, object]:
    """Recompute the manifest of a commit from git objects, never the working tree."""
    tracked = tree_modes(checkout, commit)
    paths = selected_paths(tracked)
    if not paths:
        raise ImportError("upstream selection is empty")
    licenses = license_map(tracked, paths)
    license_paths = set(licenses.values())
    content_paths = sorted(set(licenses), key=lambda value: (value != "LICENSE", value))
    for relative in content_paths:
        if tracked[relative] not in REGULAR_FILE_MODES:
            raise ImportError(f"selected upstream entry is not a regular file: {relative}")
    blobs = blob_bytes(checkout, commit, content_paths)
    files = [
        {
            "path": relative,
            "kind": "license" if relative in license_paths else "reference",
            "license": licenses[relative],
            "bytes": len(blobs[relative]),
            "sha256": sha256_bytes(blobs[relative]),
        }
        for relative in content_paths
    ]
    manifest: dict[str, object] = {
        "schemaVersion": 2,
        "status": "reference-only-not-executed",
        "source": {
            "repository": "effect-ts/effect",
            "url": f"https://github.com/Effect-TS/effect/tree/{commit}",
            "commit": commit,
            "tag": tag,
        },
        "selection": {
            "kind": "tracked-files-with-path-component",
            "components": sorted(COMPONENTS),
            "count": len(paths),
        },
        "licenses": {
            "paths": sorted(license_paths, key=lambda value: (value != "LICENSE", value)),
            "mapping": {relative: licenses[relative] for relative in paths},
        },
        "files": files,
        "provenance": {
            "kind": "release-pinned" if commit == COMMIT and tag == TAG else "custom-self-consistent",
            "canonicalEncoding": CANONICAL_ENCODING,
        },
    }
    manifest["integrity"] = integrity_for_manifest(manifest)
    return manifest


def render_manifest(manifest: dict[str, object]) -> str:
    return json.dumps(manifest, indent=2, sort_keys=True) + "\n"


def first_difference(actual: object, expected: object, location: str = "") -> str | None:
    if isinstance(actual, dict) and isinstance(expected, dict):
        for key in sorted(set(actual) | set(expected)):
            if key not in actual or key not in expected:
                return f"{location}/{key}"
            found = first_difference(actual[key], expected[key], f"{location}/{key}")
            if found is not None:
                return found
        return None
    if isinstance(actual, list) and isinstance(expected, list):
        for index, (left, right) in enumerate(zip(actual, expected)):
            label = right.get("path", index) if isinstance(right, dict) else index
            found = first_difference(left, right, f"{location}[{label}]")
            if found is not None:
                return found
        return None if len(actual) == len(expected) else f"{location}[length]"
    if type(actual) is not type(expected) or actual != expected:
        return location or "/"
    return None


def pinned_checkout(root: Path, commit: str) -> Path:
    """Return the submodule checkout, failing closed unless it is exactly the pinned commit."""
    checkout = root / CHECKOUT_RELATIVE
    toplevel = git_process(checkout, "rev-parse", "--show-toplevel") if checkout.is_dir() else None
    if toplevel is None or toplevel.returncode != 0 or Path(toplevel.stdout.strip()) != checkout.resolve():
        raise ImportError(f"Effect upstream checkout {CHECKOUT_RELATIVE} is not initialized; run {INIT_COMMAND}")
    entry = str(run_git(root, "ls-files", "--stage", "--", str(CHECKOUT_RELATIVE))).split()
    if entry[:2] != ["160000", commit]:
        recorded = entry[1] if entry[:1] == ["160000"] else "no gitlink"
        raise ImportError(f"{CHECKOUT_RELATIVE} records {recorded}, not the pinned commit {commit}")
    head = str(run_git(checkout, "rev-parse", "HEAD")).strip()
    if head != commit:
        raise ImportError(f"Effect upstream checkout {CHECKOUT_RELATIVE} is at {head}, not the pinned commit {commit}; run {INIT_COMMAND}")
    modified = str(run_git(checkout, "status", "--porcelain=v1", "--untracked-files=no", "--ignore-submodules=all")).splitlines()
    if modified:
        raise ImportError(f"Effect upstream checkout {CHECKOUT_RELATIVE} has modified tracked files ({modified[0].strip()}); run {INIT_COMMAND} --force")
    return checkout


def validate_release_identity(manifest: dict[str, object]) -> None:
    source = manifest["source"]
    selection = manifest["selection"]
    integrity = manifest["integrity"]
    provenance = manifest["provenance"]
    identity = manifest_identity(manifest)
    assert isinstance(source, dict)
    assert isinstance(selection, dict)
    assert isinstance(integrity, dict)
    assert isinstance(provenance, dict)
    if source.get("commit") != COMMIT or source.get("tag") != TAG or provenance.get("kind") != "release-pinned":
        raise ImportError("manifest source is not the pinned Effect 4.0.1 release")
    if selection.get("count") != RELEASE_REFERENCE_COUNT or len(identity["licenses"]) != RELEASE_LICENSE_COUNT:
        raise ImportError("manifest release selection counts are invalid")
    if integrity.get("referenceIdentitySha256") != RELEASE_REFERENCE_IDENTITY_SHA256 or integrity.get("licenseIdentitySha256") != RELEASE_LICENSE_IDENTITY_SHA256 or integrity.get("rootSha256") != RELEASE_ROOT_IDENTITY_SHA256:
        raise ImportError("manifest release identity digest is not the independently pinned corpus")


def verify_manifest(checkout: Path, manifest_path: Path, commit: str, tag: str) -> dict[str, object]:
    """Compare a committed manifest with the one recomputed from the pinned commit."""
    expected = build_manifest(checkout, commit, tag)
    try:
        text = manifest_path.read_text(encoding="utf-8")
        actual = json.loads(text)
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise ImportError(f"manifest is not readable UTF-8 JSON: {manifest_path}") from error
    difference = first_difference(actual, expected)
    if difference is not None:
        raise ImportError(f"manifest differs from the pinned checkout at {difference}")
    if text != render_manifest(expected):
        raise ImportError("manifest is not in canonical form; run with --refresh")
    if commit == COMMIT and tag == TAG:
        validate_release_identity(expected)
    return expected


def verify(root: Path = ROOT, commit: str = COMMIT, tag: str = TAG) -> tuple[Path, dict[str, object]]:
    """Verify the pinned checkout and committed manifest; return both for consumers."""
    checkout = pinned_checkout(root, commit)
    return checkout, verify_manifest(checkout, root / MANIFEST_RELATIVE, commit, tag)


def refresh(root: Path = ROOT, commit: str = COMMIT, tag: str = TAG) -> dict[str, object]:
    """Rewrite the committed manifest from the pinned checkout."""
    manifest = build_manifest(pinned_checkout(root, commit), commit, tag)
    (root / MANIFEST_RELATIVE).write_text(render_manifest(manifest), encoding="utf-8")
    return manifest


def parser() -> argparse.ArgumentParser:
    result = argparse.ArgumentParser(description=__doc__)
    result.add_argument("--root", type=Path, default=ROOT, help="Effra repository root")
    result.add_argument("--refresh", action="store_true", help="rewrite the manifest from the pinned checkout")
    return result


def main() -> int:
    args = parser().parse_args()
    root = args.root.resolve()
    if args.refresh:
        manifest = refresh(root)
        print(json.dumps(manifest["integrity"], indent=2, sort_keys=True))
        return 0
    _, manifest = verify(root)
    integrity = manifest["integrity"]
    selection = manifest["selection"]
    assert isinstance(integrity, dict)
    assert isinstance(selection, dict)
    print(f"effect upstream: {selection['count']} reference files at {COMMIT[:12]} match {MANIFEST_RELATIVE} (root {integrity['rootSha256']})")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except ImportError as error:
        raise SystemExit(f"import_effect_conformance: {error}")
