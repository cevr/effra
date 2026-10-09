#!/usr/bin/env python3
"""Capture and verify the pinned Foldkit workspace as reference-only source."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import posixpath
import shutil
import stat
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any


COMMIT = "d21db423ac0ba676b22aa7a7b4041ba409ec2c52"
TREE = "ccbd07912275e62385bd9fdccaf2e1e695cadb4b"
REPOSITORY = "foldkit/foldkit"
RELEASE = "0.167.0"
WORKSPACE_EFFECT = "4.0.0"
WORKSPACE_PACKAGE_MANAGER = "pnpm@11.8.0"
SOURCE_URL = f"https://github.com/{REPOSITORY}/tree/{COMMIT}"
DEFAULT_SOURCE = Path("/home/exedev/.cache/repo/foldkit/foldkit")
SNAPSHOT_RELATIVE = Path("conformance/upstream/foldkit-d21db423")
CORPUS_RELATIVE = Path("conformance/framework-ports")
CANONICAL_ENCODING = "json-sorted-compact-utf8-v1"

EXAMPLE_IDS = (
    "api-cache",
    "api-cache-query",
    "auth",
    "canvas-art",
    "charting",
    "counter",
    "counters",
    "crash-view",
    "embedding",
    "form",
    "generative-art",
    "interrupting-commands",
    "job-application",
    "kanban",
    "managed-resource-layer",
    "map",
    "personal-blog",
    "pixel-art",
    "query-sync",
    "route-transitions",
    "routing",
    "shopping-cart",
    "slow-warnings",
    "snake",
    "ssg",
    "ssr",
    "state-machine",
    "stopwatch",
    "todo",
    "ui-showcase",
    "view-transitions",
    "weather",
    "web-components",
    "websocket-chat",
)

HOSTS = (
    "foldkit-reference",
    "react-dom",
    "solid-2-dom",
    "solid-yield",
    "effra-go",
    "effra-effect-js",
)

FOCUS = {
    "api-cache": "Shared API cache and tabbed data reads",
    "api-cache-query": "Typed query cache with explicit data consumers",
    "auth": "Session restoration, routing, and login",
    "canvas-art": "Canvas commands and generated drawing state",
    "charting": "Chart and data-visualization interactions",
    "counter": "Minimal model, messages, update, and view",
    "counters": "Multiple independent counter models",
    "crash-view": "Failure presentation at a view boundary",
    "embedding": "Embedding an application through host ports",
    "form": "Field validation and asynchronous submission",
    "generative-art": "Procedural artwork and generated commands",
    "interrupting-commands": "Keyed cancellation of concurrent uploads",
    "job-application": "Calendar and job-application form flow",
    "kanban": "Persisted board state and task movement",
    "managed-resource-layer": "Layer acquisition through managed resource lifetime",
    "map": "Map and geolocation host integration",
    "personal-blog": "Routed article pages and content loading",
    "pixel-art": "Pixel editing and persisted drawing state",
    "query-sync": "URL query state synchronized with model state",
    "route-transitions": "Route changes with explicit transition state",
    "routing": "Routed application state and navigation commands",
    "shopping-cart": "Cart state and derived checkout information",
    "slow-warnings": "Slow work observation and warning UI",
    "snake": "Tick-driven interactive game state",
    "ssg": "Static-site generation over route data",
    "ssr": "Server rendering and initial application state",
    "state-machine": "Guarded checkout state machine",
    "stopwatch": "Time commands and subscriptions",
    "todo": "Persisted task editing",
    "ui-showcase": "Reusable UI controls and routing",
    "view-transitions": "Browser view-transition lifecycle",
    "weather": "Typed HTTP requests and async result state",
    "web-components": "Custom-element definition and host boundary",
    "websocket-chat": "Socket resource and streaming messages",
}

EXCLUDED_PREFIXES = ("repos/",)
EXCLUDED_DIRECTORY_NAMES = frozenset(
    {
        ".git",
        "node_modules",
        ".cache",
        ".vite",
        ".turbo",
        "coverage",
        ".next",
        ".output",
        ".vercel",
        ".nyc_output",
    }
)
EXCLUDED_FILE_SUFFIXES = (".tsbuildinfo",)

# Set from the first verified capture. These constants make offline validation
# reject edits that change both a captured byte and its manifest hash.
EXPECTED_FILE_COUNT = 2795
EXPECTED_EXAMPLE_INVENTORY_SHA256 = "7cca6e854630d8ad539801f5914a995590358294f2ee64086b4376e0923f57c8"
EXPECTED_ROOT_SHA256 = "69d0606c683d1175bf9c2f7975686113379ae11687cdb29ec9cc3c4cfaf55152"

NOTICE_CONTENT = (
    "This snapshot preserves the tracked Foldkit workspace at the commit in "
    "manifest.json, with the upstream LICENSE retained byte-for-byte. The "
    "tracked examples, packages, internal sources, scripts, package-manager "
    "files, and workspace configuration are included. The vendored repos/ "
    "reference trees and generated or installed dependency/cache directories "
    "are excluded; repository guidance states that repos/ is not imported by "
    "package or example source. No dependencies were installed and no source "
    "was executed as part of this capture. This is immutable comparison data, "
    "not Effra coverage or a claim that the snapshot currently builds.\n"
).encode("utf-8")


class CorpusImportError(RuntimeError):
    """A source, path, snapshot, or manifest invariant failed."""


@dataclass(frozen=True)
class TreeEntry:
    path: str
    mode: str
    object_type: str
    oid: str
    size: int


def run_git(source: Path, *args: str, input_bytes: bytes | None = None) -> bytes:
    result = subprocess.run(
        ["git", "-C", str(source), *args],
        check=False,
        capture_output=True,
        input=input_bytes,
    )
    if result.returncode != 0:
        detail = result.stderr.decode("utf-8", errors="replace").strip()
        raise CorpusImportError(f"git {' '.join(args)} failed: {detail}")
    return result.stdout


def safe_relative_path(value: str) -> PurePosixPath:
    if not value or "\\" in value or "\x00" in value:
        raise CorpusImportError(f"unsafe snapshot path: {value!r}")
    path = PurePosixPath(value)
    if path.is_absolute() or any(part in {"", ".", ".."} for part in path.parts):
        raise CorpusImportError(f"unsafe snapshot path: {value!r}")
    return path


def excluded_reason(path: str) -> str | None:
    normalized = path.replace("\\", "/")
    if any(normalized.startswith(prefix) for prefix in EXCLUDED_PREFIXES):
        return "vendored-reference-tree"
    parts = PurePosixPath(normalized).parts
    if any(part in EXCLUDED_DIRECTORY_NAMES for part in parts):
        return "installed-or-generated-directory"
    if any(part.endswith(suffix) for part in parts for suffix in EXCLUDED_FILE_SUFFIXES):
        return "generated-typescript-build-info"
    return None


def tree_entries(source: Path, commit: str) -> tuple[list[TreeEntry], dict[str, dict[str, int]]]:
    raw = run_git(source, "ls-tree", "-r", "-l", "-z", "--full-tree", commit)
    included: list[TreeEntry] = []
    excluded: dict[str, dict[str, int]] = {}
    for record in raw.split(b"\x00"):
        if not record:
            continue
        header, raw_path = record.split(b"\t", 1)
        mode, object_type, oid, raw_size = header.decode("ascii").split()
        path = raw_path.decode("utf-8", errors="strict")
        safe_relative_path(path)
        size = int(raw_size)
        reason = excluded_reason(path)
        if reason is not None:
            current = excluded.setdefault(reason, {"files": 0, "bytes": 0})
            current["files"] += 1
            current["bytes"] += size
            continue
        if object_type != "blob" or mode not in {"100644", "100755", "120000"}:
            raise CorpusImportError(f"unsupported tracked entry: {mode} {object_type} {path}")
        included.append(TreeEntry(path, mode, object_type, oid, size))
    included.sort(key=lambda entry: entry.path.encode("utf-8"))
    return included, excluded


def example_inventory(entries: list[TreeEntry]) -> tuple[str, ...]:
    result = {
        PurePosixPath(entry.path).parts[1]
        for entry in entries
        if len(PurePosixPath(entry.path).parts) >= 3
        and PurePosixPath(entry.path).parts[0] == "examples"
    }
    return tuple(sorted(result))


def require_inventory(entries: list[TreeEntry]) -> tuple[str, ...]:
    actual = example_inventory(entries)
    if actual != tuple(sorted(EXAMPLE_IDS)):
        missing = sorted(set(EXAMPLE_IDS) - set(actual))
        unexpected = sorted(set(actual) - set(EXAMPLE_IDS))
        raise CorpusImportError(
            f"pinned example inventory mismatch; missing={missing}, unexpected={unexpected}"
        )
    return actual


def validate_pin(source: Path) -> None:
    resolved = run_git(source, "rev-parse", f"{COMMIT}^{{commit}}").decode().strip()
    tree = run_git(source, "rev-parse", f"{COMMIT}^{{tree}}").decode().strip()
    if resolved != COMMIT:
        raise CorpusImportError(f"source commit mismatch: expected {COMMIT}, got {resolved}")
    if tree != TREE:
        raise CorpusImportError(f"source tree mismatch: expected {TREE}, got {tree}")


def read_blob_data(source: Path, entries: list[TreeEntry]) -> dict[str, bytes]:
    if not entries:
        raise CorpusImportError("source selection is empty")
    process = subprocess.run(
        ["git", "-C", str(source), "cat-file", "--batch"],
        check=False,
        capture_output=True,
        input=b"".join(entry.oid.encode("ascii") + b"\n" for entry in entries),
    )
    if process.returncode != 0:
        raise CorpusImportError(
            "git cat-file --batch failed: "
            + process.stderr.decode("utf-8", errors="replace").strip()
        )
    output = process.stdout
    offset = 0
    result: dict[str, bytes] = {}
    for entry in entries:
        end = output.find(b"\n", offset)
        if end < 0:
            raise CorpusImportError(f"truncated cat-file header for {entry.path}")
        header = output[offset:end].decode("ascii").split()
        if len(header) != 3:
            raise CorpusImportError(f"invalid cat-file header for {entry.path}")
        oid, object_type, raw_size = header
        size = int(raw_size)
        if oid != entry.oid or object_type != "blob" or size != entry.size:
            raise CorpusImportError(f"cat-file identity mismatch for {entry.path}")
        start = end + 1
        body_end = start + size
        if body_end >= len(output) or output[body_end : body_end + 1] != b"\n":
            raise CorpusImportError(f"truncated cat-file body for {entry.path}")
        result[entry.path] = output[start:body_end]
        offset = body_end + 1
    if offset != len(output):
        raise CorpusImportError("unexpected trailing cat-file output")
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


def source_versions(data: dict[str, bytes]) -> dict[str, str]:
    try:
        root = json.loads(data["package.json"])
        foldkit = json.loads(data["packages/foldkit/package.json"])
    except (KeyError, json.JSONDecodeError, TypeError) as error:
        raise CorpusImportError(f"could not read pinned workspace package versions: {error}") from error
    actual = {
        "workspacePackageManager": root.get("packageManager"),
        "effect": root.get("devDependencies", {}).get("effect"),
        "foldkit": foldkit.get("version"),
    }
    expected = {
        "workspacePackageManager": WORKSPACE_PACKAGE_MANAGER,
        "effect": WORKSPACE_EFFECT,
        "foldkit": RELEASE,
    }
    if actual != expected:
        raise CorpusImportError(f"pinned workspace versions mismatch: {actual!r}")
    return {key: str(value) for key, value in actual.items()}


def file_identity(files: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return [
        {
            "path": item["path"],
            "mode": item["mode"],
            "gitBlob": item["gitBlob"],
            "bytes": item["bytes"],
            "sha256": item["sha256"],
        }
        for item in files
    ]


def selection_identity(
    versions: dict[str, str],
    inventory: tuple[str, ...],
    files: list[dict[str, Any]],
    excluded: dict[str, dict[str, int]],
) -> dict[str, Any]:
    return {
        "schemaVersion": 1,
        "status": "source-reference-only-not-executed",
        "source": {
            "repository": REPOSITORY,
            "commit": COMMIT,
            "tree": TREE,
            "url": SOURCE_URL,
            **versions,
        },
        "selection": {
            "kind": "all-tracked-workspace-files-except-vendored-reference-and-generated-paths",
            "exampleDirectories": list(inventory),
            "exampleDirectoryCount": len(inventory),
            "includedFileCount": len(files),
            "excludedPrefixes": list(EXCLUDED_PREFIXES),
            "excludedDirectoryNames": sorted(EXCLUDED_DIRECTORY_NAMES),
            "excludedFileSuffixes": list(EXCLUDED_FILE_SUFFIXES),
            "excludedCounts": excluded,
        },
        "files": file_identity(files),
    }


def manifest_for(
    entries: list[TreeEntry],
    data: dict[str, bytes],
    inventory: tuple[str, ...],
    excluded: dict[str, dict[str, int]],
) -> dict[str, Any]:
    versions = source_versions(data)
    files = [
        {
            "path": entry.path,
            "mode": entry.mode,
            "gitBlob": entry.oid,
            "bytes": len(data[entry.path]),
            "sha256": sha256_bytes(data[entry.path]),
        }
        for entry in entries
    ]
    identity = selection_identity(versions, inventory, files, excluded)
    return {
        **identity,
        "integrity": {
            "algorithm": "sha256",
            "canonicalEncoding": CANONICAL_ENCODING,
            "exampleInventorySha256": canonical_sha256(list(inventory)),
            "fileIdentitySha256": canonical_sha256(file_identity(files)),
            "rootSha256": canonical_sha256(identity),
        },
    }


def safe_symlink_target(path: str, target: bytes) -> str:
    try:
        value = target.decode("utf-8", errors="strict")
    except UnicodeDecodeError as error:
        raise CorpusImportError(f"symlink target is not UTF-8: {path}") from error
    if not value or "\\" in value or "\x00" in value or posixpath.isabs(value):
        raise CorpusImportError(f"unsafe symlink target in source: {path} -> {value!r}")
    resolved = posixpath.normpath(posixpath.join(posixpath.dirname(path), value))
    if resolved == ".." or resolved.startswith("../"):
        raise CorpusImportError(f"symlink escapes snapshot: {path} -> {value!r}")
    return value


def write_tree(stage: Path, entries: list[TreeEntry], data: dict[str, bytes]) -> None:
    for entry in entries:
        relative = safe_relative_path(entry.path)
        destination = stage.joinpath(*relative.parts)
        destination.parent.mkdir(parents=True, exist_ok=True)
        body = data[entry.path]
        if entry.mode == "120000":
            os.symlink(safe_symlink_target(entry.path, body), destination)
            continue
        destination.write_bytes(body)
        destination.chmod(0o755 if entry.mode == "100755" else 0o644)


def write_json(path: Path, value: object) -> None:
    path.write_text(
        json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + "\n",
        encoding="utf-8",
    )


def corpus_manifest_for(snapshot: Path, repository_root: Path) -> dict[str, Any]:
    try:
        snapshot_reference = snapshot.relative_to(repository_root).as_posix()
    except ValueError as error:
        raise CorpusImportError(
            f"corpus snapshot is outside repository root: {snapshot}"
        ) from error
    if not snapshot_reference or snapshot_reference == ".":
        raise CorpusImportError("corpus snapshot path is empty")
    coverage = []
    for example in EXAMPLE_IDS:
        evidence = f"{snapshot_reference}/examples/{example}/package.json"
        for host in HOSTS:
            if host == "foldkit-reference":
                status = "reference-only"
                row_evidence = [evidence]
                note = "Pinned upstream source only; not Effra execution coverage."
            else:
                status = "pending"
                row_evidence = []
                note = "No executable port or Effra fixture is present in unit 1."
            coverage.append(
                {
                    "example": example,
                    "focus": FOCUS[example],
                    "host": host,
                    "status": status,
                    "evidence": row_evidence,
                    "note": note,
                }
            )
    return {
        "schemaVersion": 1,
        "status": "unit-1-reference-capture; host-ports-pending",
        "source": {
            "repository": REPOSITORY,
            "commit": COMMIT,
            "tree": TREE,
            "url": SOURCE_URL,
            "foldkitVersion": RELEASE,
            "workspaceEffectVersion": WORKSPACE_EFFECT,
        },
        "snapshot": {
            "path": snapshot_reference,
            "manifest": f"{snapshot_reference}/manifest.json",
            "status": "reference-only-not-executed",
        },
        "hosts": list(HOSTS),
        "examples": [
            {"id": example, "focus": FOCUS[example]}
            for example in EXAMPLE_IDS
        ],
        "coverage": coverage,
    }


def validate_corpus_manifest(path: Path, snapshot: Path, repository_root: Path) -> None:
    try:
        manifest = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise CorpusImportError(f"invalid framework corpus manifest: {error}") from error
    if not isinstance(manifest, dict):
        raise CorpusImportError("invalid framework corpus manifest: root must be an object")
    expected = corpus_manifest_for(snapshot, repository_root)
    for key in ("schemaVersion", "status", "source", "snapshot", "hosts", "examples"):
        if manifest.get(key) != expected[key]:
            raise CorpusImportError(f"framework corpus manifest has an unexpected {key} field")
    rows = manifest.get("coverage")
    if not isinstance(rows, list):
        raise CorpusImportError("framework corpus manifest coverage must be a list")
    if len(rows) != len(EXAMPLE_IDS) * len(HOSTS):
        raise CorpusImportError("framework corpus manifest has an incomplete example/host matrix")
    expected_rows = expected["coverage"]
    for index, row in enumerate(rows):
        if not isinstance(row, dict):
            raise CorpusImportError(f"framework corpus coverage row {index} must be an object")
        required = {"example", "focus", "host", "status", "evidence", "note"}
        missing = sorted(required - set(row))
        unexpected = sorted(set(row) - required)
        if missing or unexpected:
            detail = []
            if missing:
                detail.append(f"missing {', '.join(missing)}")
            if unexpected:
                detail.append(f"unexpected {', '.join(unexpected)}")
            raise CorpusImportError(
                f"framework corpus coverage row {index} has malformed fields ({'; '.join(detail)})"
            )
        expected_row = expected_rows[index]
        for key in ("example", "focus", "host"):
            if row.get(key) != expected_row[key]:
                raise CorpusImportError(
                    f"framework corpus coverage row {index} has unexpected {key}: {row.get(key)!r}"
                )
    pairs = [(row["example"], row["host"]) for row in rows]
    if len(set(pairs)) != len(pairs):
        raise CorpusImportError("framework corpus manifest contains duplicate example/host rows")
    for index, (row, expected_row) in enumerate(zip(rows, expected_rows, strict=True)):
        status = row.get("status")
        evidence = row.get("evidence")
        if status not in {"reference-only", "pending", "implemented-and-verified"}:
            raise CorpusImportError(
                f"invalid corpus coverage status at row {index} ({row['example']}:{row['host']}): {status!r}"
            )
        if not isinstance(evidence, list) or not all(isinstance(item, str) for item in evidence):
            raise CorpusImportError(
                f"invalid evidence list at row {index} ({row['example']}:{row['host']})"
            )
        if not isinstance(row.get("note"), str) or not row["note"].strip():
            raise CorpusImportError(
                f"missing coverage note at row {index} ({row['example']}:{row['host']})"
            )
        if row["host"] == "foldkit-reference":
            if status != "reference-only" or evidence != expected_row["evidence"]:
                raise CorpusImportError(
                    f"Foldkit source row {index} ({row['example']}:{row['host']}) must remain reference-only with its source evidence"
                )
        elif status == "pending" and evidence:
            raise CorpusImportError(
                f"pending host row has evidence at row {index}: {row['example']}:{row['host']}"
            )
        elif status == "reference-only" and not evidence:
            raise CorpusImportError(
                f"reference-only host row has no source evidence at row {index}: {row['example']}:{row['host']}"
            )
        elif status == "implemented-and-verified" and not evidence:
            raise CorpusImportError(
                f"implemented host row has no verification evidence at row {index}: {row['example']}:{row['host']}"
            )
        elif status == "implemented-and-verified":
            snapshot_prefix = f"{expected['snapshot']['path']}/"
            if any(
                item == expected["snapshot"]["path"] or item.startswith(snapshot_prefix)
                for item in evidence
            ):
                raise CorpusImportError(
                    f"implemented host row has pinned upstream source-reference evidence at row {index}: "
                    f"{row['example']}:{row['host']}"
                )
            raise CorpusImportError(
                f"implemented host row is not accepted before an executable runner verifies an authored target at row {index}: "
                f"{row['example']}:{row['host']}"
            )
        for item in evidence:
            relative = safe_relative_path(item)
            if not (repository_root.joinpath(*relative.parts).is_file()):
                raise CorpusImportError(f"missing corpus evidence file: {item}")


def snapshot_file_paths(root: Path) -> set[str]:
    result: set[str] = set()
    for current, directories, files in os.walk(root, followlinks=False):
        current_path = Path(current)
        for name in list(directories):
            candidate = current_path / name
            info = candidate.lstat()
            if stat.S_ISLNK(info.st_mode):
                raise CorpusImportError(f"directory symlink is not allowed in snapshot: {candidate}")
            if not stat.S_ISDIR(info.st_mode):
                raise CorpusImportError(f"unexpected directory entry: {candidate}")
        for name in files:
            candidate = current_path / name
            relative = candidate.relative_to(root).as_posix()
            info = candidate.lstat()
            if not stat.S_ISREG(info.st_mode) and not stat.S_ISLNK(info.st_mode):
                raise CorpusImportError(f"unsupported snapshot file type: {relative}")
            result.add(relative)
    return result


def git_blob_oid(data: bytes) -> str:
    header = f"blob {len(data)}\0".encode("ascii")
    return hashlib.sha1(header + data).hexdigest()


def validate_snapshot(snapshot: Path) -> dict[str, Any]:
    if snapshot.is_symlink() or not snapshot.is_dir():
        raise CorpusImportError(f"missing or unsafe source snapshot: {snapshot}")
    try:
        manifest = json.loads((snapshot / "manifest.json").read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise CorpusImportError(f"invalid Foldkit snapshot manifest: {error}") from error
    if not isinstance(manifest, dict):
        raise CorpusImportError("invalid Foldkit snapshot manifest: root must be an object")
    if manifest.get("schemaVersion") != 1 or manifest.get("status") != "source-reference-only-not-executed":
        raise CorpusImportError("unsupported Foldkit snapshot schema or status")
    source = manifest.get("source")
    if not isinstance(source, dict) or source.get("commit") != COMMIT or source.get("tree") != TREE:
        raise CorpusImportError("Foldkit snapshot does not identify the pinned commit and tree")
    if source.get("repository") != REPOSITORY or source.get("url") != SOURCE_URL:
        raise CorpusImportError("Foldkit snapshot repository provenance mismatch")
    files = manifest.get("files")
    if not isinstance(files, list):
        raise CorpusImportError("Foldkit snapshot files must be a list")
    expected_paths: set[str] = set()
    symlink_referents: list[tuple[str, str]] = []
    verified_files: list[dict[str, Any]] = []
    for item in files:
        if not isinstance(item, dict):
            raise CorpusImportError("Foldkit snapshot file entry must be an object")
        path = item.get("path")
        if not isinstance(path, str):
            raise CorpusImportError("Foldkit snapshot file path must be a string")
        safe_relative_path(path)
        if path in expected_paths or excluded_reason(path) is not None:
            raise CorpusImportError(f"duplicate or excluded path in snapshot manifest: {path}")
        expected_paths.add(path)
        candidate = snapshot.joinpath(*PurePosixPath(path).parts)
        try:
            info = candidate.lstat()
        except OSError as error:
            raise CorpusImportError(f"missing snapshot file: {path}") from error
        mode = item.get("mode")
        if mode == "120000":
            if not stat.S_ISLNK(info.st_mode):
                raise CorpusImportError(f"snapshot symlink mode mismatch: {path}")
            body = os.readlink(candidate).encode("utf-8")
            target = safe_symlink_target(path, body)
            referent = posixpath.normpath(posixpath.join(posixpath.dirname(path), target))
            symlink_referents.append((path, referent))
        elif mode in {"100644", "100755"}:
            if not stat.S_ISREG(info.st_mode):
                raise CorpusImportError(f"snapshot regular-file mode mismatch: {path}")
            body = candidate.read_bytes()
            # Git records only the owner executable bit (100644 or 100755); the
            # remaining permission bits follow the checkout umask.
            executable = bool(info.st_mode & stat.S_IXUSR)
            if executable != (mode == "100755"):
                raise CorpusImportError(f"snapshot executable mode mismatch: {path}")
        else:
            raise CorpusImportError(f"unsupported mode in snapshot manifest: {path}")
        if len(body) != item.get("bytes") or sha256_bytes(body) != item.get("sha256"):
            raise CorpusImportError(f"snapshot bytes or SHA-256 mismatch: {path}")
        if git_blob_oid(body) != item.get("gitBlob"):
            raise CorpusImportError(f"snapshot Git blob identity mismatch: {path}")
        verified_files.append(
            {
                "path": path,
                "mode": mode,
                "gitBlob": item["gitBlob"],
                "bytes": item["bytes"],
                "sha256": item["sha256"],
            }
        )
    for path, referent in symlink_referents:
        if referent not in expected_paths:
            raise CorpusImportError(
                f"snapshot symlink referent is absent from the manifest: {path} -> {referent}"
            )
    if verified_files != sorted(verified_files, key=lambda item: item["path"].encode("utf-8")):
        raise CorpusImportError("Foldkit snapshot file entries are not in canonical order")
    actual_paths = snapshot_file_paths(snapshot) - {"manifest.json", "NOTICE"}
    if actual_paths != expected_paths:
        raise CorpusImportError(
            f"snapshot file set mismatch; missing={sorted(expected_paths - actual_paths)[:4]}, "
            f"extra={sorted(actual_paths - expected_paths)[:4]}"
        )
    notice = (snapshot / "NOTICE").read_bytes()
    if notice != NOTICE_CONTENT:
        raise CorpusImportError("Foldkit NOTICE differs from the declared source-capture notice")
    if len(files) != EXPECTED_FILE_COUNT:
        raise CorpusImportError(
            f"Foldkit snapshot has {len(files)} files, expected pinned count {EXPECTED_FILE_COUNT}"
        )
    identity = {
        "schemaVersion": manifest["schemaVersion"],
        "status": manifest["status"],
        "source": manifest["source"],
        "selection": manifest["selection"],
        "files": file_identity(files),
    }
    integrity = manifest.get("integrity")
    expected_inventory_hash = canonical_sha256(list(EXAMPLE_IDS))
    if expected_inventory_hash != EXPECTED_EXAMPLE_INVENTORY_SHA256:
        raise CorpusImportError("built-in Foldkit inventory fingerprint is inconsistent")
    expected_integrity = {
        "algorithm": "sha256",
        "canonicalEncoding": CANONICAL_ENCODING,
        "exampleInventorySha256": canonical_sha256(manifest["selection"]["exampleDirectories"]),
        "fileIdentitySha256": canonical_sha256(file_identity(files)),
        "rootSha256": canonical_sha256(identity),
    }
    if integrity != expected_integrity:
        raise CorpusImportError("Foldkit snapshot integrity metadata mismatch")
    if integrity["exampleInventorySha256"] != EXPECTED_EXAMPLE_INVENTORY_SHA256:
        raise CorpusImportError("Foldkit example inventory digest differs from the pinned inventory")
    if integrity["rootSha256"] != EXPECTED_ROOT_SHA256:
        raise CorpusImportError("Foldkit file identity differs from the independently pinned snapshot")
    inventory = tuple(manifest["selection"]["exampleDirectories"])
    if inventory != tuple(sorted(EXAMPLE_IDS)) or manifest["selection"]["exampleDirectoryCount"] != 34:
        raise CorpusImportError("Foldkit snapshot example directories are incomplete")
    source_versions({entry["path"]: (snapshot / entry["path"]).read_bytes() for entry in files if entry["path"] in {"package.json", "packages/foldkit/package.json"}})
    return manifest


def capture(source: Path, repository_root: Path) -> None:
    validate_pin(source)
    entries, excluded = tree_entries(source, COMMIT)
    inventory = require_inventory(entries)
    data = read_blob_data(source, entries)
    manifest = manifest_for(entries, data, inventory, excluded)
    parent = (repository_root / SNAPSHOT_RELATIVE).parent
    output = repository_root / SNAPSHOT_RELATIVE
    corpus_path = repository_root / CORPUS_RELATIVE / "manifest.json"
    if output.exists() or output.is_symlink():
        raise CorpusImportError(f"refusing to replace existing snapshot: {output}")
    if corpus_path.exists() or corpus_path.is_symlink():
        raise CorpusImportError(f"refusing to replace existing corpus manifest: {corpus_path}")
    parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix=".foldkit-capture-", dir=parent))
    corpus_stage = stage.parent / f".{stage.name}-corpus-manifest.json"
    published_snapshot = False
    published_corpus = False
    try:
        write_tree(stage, entries, data)
        (stage / "NOTICE").write_bytes(NOTICE_CONTENT)
        write_json(stage / "manifest.json", manifest)
        write_json(corpus_stage, corpus_manifest_for(output, repository_root))
        os.replace(stage, output)
        published_snapshot = True
        corpus_path.parent.mkdir(parents=True, exist_ok=True)
        os.replace(corpus_stage, corpus_path)
        published_corpus = True
    except BaseException:
        if published_corpus:
            corpus_path.unlink(missing_ok=True)
        if published_snapshot:
            shutil.rmtree(output, ignore_errors=True)
        shutil.rmtree(stage, ignore_errors=True)
        raise
    finally:
        corpus_stage.unlink(missing_ok=True)
    print(f"captured {len(entries)} files across {len(inventory)} examples at {output}")
    print(f"ROOT_SHA256={manifest['integrity']['rootSha256']}")
    print(f"EXAMPLE_INVENTORY_SHA256={manifest['integrity']['exampleInventorySha256']}")
    print(f"FILE_COUNT={len(entries)}")


def check_source(source: Path, snapshot: Path) -> None:
    validate_pin(source)
    entries, excluded = tree_entries(source, COMMIT)
    inventory = require_inventory(entries)
    data = read_blob_data(source, entries)
    expected = manifest_for(entries, data, inventory, excluded)
    actual = validate_snapshot(snapshot)
    if expected != actual:
        raise CorpusImportError("captured Foldkit source differs from the immutable source checkout")
    print(f"source checkout matches {len(entries)} captured files at {COMMIT}")


def self_check(repository_root: Path) -> None:
    snapshot = repository_root / SNAPSHOT_RELATIVE
    manifest = validate_snapshot(snapshot)
    validate_corpus_manifest(repository_root / CORPUS_RELATIVE / "manifest.json", snapshot, repository_root)
    print(
        f"verified reference-only snapshot: {len(manifest['files'])} files, "
        f"{len(manifest['selection']['exampleDirectories'])} examples, "
        f"{manifest['integrity']['rootSha256']}"
    )


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", nargs="?", type=Path, help="read-only pinned Foldkit git checkout")
    actions = parser.add_mutually_exclusive_group(required=True)
    actions.add_argument("--capture", action="store_true", help="capture the pinned source into this worktree")
    actions.add_argument("--check", action="store_true", help="verify capture against the pinned source checkout")
    actions.add_argument("--self-check", action="store_true", help="verify committed snapshot and corpus matrix offline")
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(sys.argv[1:] if argv is None else argv)
    try:
        snapshot = args.root / SNAPSHOT_RELATIVE
        if args.capture:
            capture(args.source or DEFAULT_SOURCE, args.root)
            if EXPECTED_FILE_COUNT and EXPECTED_EXAMPLE_INVENTORY_SHA256 and EXPECTED_ROOT_SHA256:
                self_check(args.root)
        elif args.check:
            check_source(args.source or DEFAULT_SOURCE, snapshot)
            validate_corpus_manifest(
                args.root / CORPUS_RELATIVE / "manifest.json",
                snapshot,
                args.root,
            )
        else:
            if args.source is not None:
                raise CorpusImportError("--self-check does not accept a source checkout")
            self_check(args.root)
    except (CorpusImportError, OSError, ValueError, KeyError, TypeError) as error:
        print(f"Foldkit corpus import failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
