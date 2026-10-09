#!/usr/bin/env python3
"""Prepare a read-only migration from the local Wayfinder snapshot.

This module deliberately has no tracker client.  It turns the checked-in local
issues into deterministic issue, resolution-comment, and graph payloads.  A
later publisher can consume those payloads after reviewing them and resolving
the pending hosted identities.
"""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import re
import subprocess
import sys
import tarfile
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Any
from urllib.parse import urldefrag


SCHEMA_VERSION = 2
INPUT_MANIFEST_SCHEMA_VERSION = 1
HISTORICAL_INPUT_SNAPSHOT_ID = "hosted-run-binding-2026-10-08"
HISTORICAL_INPUT_SNAPSHOT_DESCRIPTOR_SHA256 = "cbf02a3830b39463986022c7be9838f1100fa6c4bb04f5c9b415b9f729e4cd5d"
HISTORICAL_INPUT_MANIFEST_SHA256 = "0373fa5b59890288ddc7de40cf817444a8f05c0a377af30132bfa8dadd7c16dc"
HISTORICAL_INPUT_CANONICAL_SHA256 = "82e5751d2a5e9ee4a2bcd3ec8ea55dbbe834ce8e716a66770189bc90804b661a"
CURRENT_IDENTITY_INTAKE_SNAPSHOT_ID = "github-wayfinder-current-intake-2026-10-08"
CURRENT_IDENTITY_INTAKE_DESCRIPTOR_SHA256 = "7f9ee47619a7a2f20e97d1666037c0d04105fbf7811728361dc2f73bcb29ebbe"
CURRENT_IDENTITY_RAW_RESPONSE_SHA256 = "c7a7d8a7be5f1e763363acddada990e3a8db926e77c391ccd94470c198d4b375"
CURRENT_IDENTITY_READ_BINDING_SHA256 = "ea23fc457d1825d3ecb5e03d68f61613e787a2404e4889b91ae85c8c004d4cf4"
CURRENT_IDENTITY_MAPPING_PATH = "docs/wayfinder/migration/current-hosted-identities-2026-10-09-source-reconciled.json"
PRESERVED_U2_CURRENT_IDENTITY_MAPPING_PATH = "docs/wayfinder/migration/current-hosted-identities-2026-10-08.json"
CURRENT_IDENTITY_OUTPUT_SNAPSHOT_ID = "current-wayfinder-map-2026-10-09-source-reconciled"
CURRENT_IDENTITY_RAW_RESPONSE_PATH = (
    "docs/wayfinder/migration/snapshots/github-wayfinder-current-intake-2026-10-08/all-issues.raw.json"
)
CURRENT_IDENTITY_READ_BINDING_PATH = (
    "docs/wayfinder/migration/snapshots/github-wayfinder-current-intake-2026-10-08/read-binding.json"
)
CURRENT_IDENTITY_DESCRIPTOR_PATH = (
    "docs/wayfinder/migration/snapshots/github-wayfinder-current-intake-2026-10-08/snapshot.json"
)
CURRENT_IDENTITY_DUPLICATE_NUMBER = 63
CURRENT_IDENTITY_DUPLICATE_OF = 62
CURRENT_IDENTITY_ISSUE_COUNT = 75
CURRENT_IDENTITY_CANONICAL_COUNT = 74
REPOSITORY = "cevr/effra"
HISTORICAL_LINK_COMMIT = "779dc58368149452b0fd42b0debb291e3001c8ac"
HISTORICAL_LINK_REVISION = {
    "repository": REPOSITORY,
    "commit": HISTORICAL_LINK_COMMIT,
    "role": "historical-link-target",
}
HOSTED_ROOT = f"https://github.com/{REPOSITORY}"
METADATA_PREFIX = "<!-- "
METADATA_SUFFIX = " -->"
LOCAL_ISSUE_MARKER = "local-wayfinder://"

# These are the four identities already published by the root agent.  They are
# intentionally the only hosted numbers known to the preparer.  Local snapshots
# may be added for a known hosted identity, but no new hosted number or URL is
# inferred here; rerun this read-only preparer after a reviewed mapping update.
KNOWN_HOSTED: dict[str, dict[str, Any]] = {
    "map": {
        "localId": "map",
        "hostedIssueNumber": 1,
        "hostedIssueId": 5764998176,
        "hostedUrl": f"{HOSTED_ROOT}/issues/1",
        "hostedTitle": "Effra language, runtimes, and interoperability map",
        "hostedState": "open",
        "hostedBodySha256": "dd445c84a7cc2be67369ca2b80c5ff95d84562241bcf2262de94e61860393ba1",
        "identityKind": "local-snapshot",
    },
    "state-machines": {
        "localId": "state-machines",
        "hostedIssueNumber": 2,
        "hostedIssueId": 5765048895,
        "hostedUrl": f"{HOSTED_ROOT}/issues/2",
        "hostedTitle": "Checked machines and machine-backed actors",
        "hostedState": "open",
        "hostedBodySha256": "2ec776a2736dff4947f532ad4287aaf3e14d6dd93d5f967cdfa02e958a241fe0",
        "identityKind": "local-snapshot",
    },
    "machine-provider-contract": {
        "localId": "machine-provider-contract",
        "hostedIssueNumber": 3,
        "hostedIssueId": 5765049263,
        "hostedUrl": f"{HOSTED_ROOT}/issues/3",
        "hostedTitle": "Runtime-independent machine plans and userland provider contract",
        "hostedState": "open",
        "hostedBodySha256": "1296408463e472e2077a6d7d4db802b000c63833b7b9c3a9ae0cd226888281db",
        "identityKind": "local-snapshot",
    },
    "machine-provider-conformance": {
        "localId": "machine-provider-conformance",
        "hostedIssueNumber": 4,
        "hostedIssueId": 5765142474,
        "hostedUrl": f"{HOSTED_ROOT}/issues/4",
        "hostedTitle": "Machine execution across independent userland runtime providers",
        "hostedState": "open",
        "hostedBodySha256": "71996a63df66fcdb348222bb9d2552651cc4836cfc71b8a3b4d7f4187350a412",
        "identityKind": "local-snapshot",
    },
}

IMPLEMENTATION_LABEL_PREFIXES = ("implementation:",)
RESEARCH_LABELS = {"research:needed"}


class MigrationError(RuntimeError):
    """A source or mapping shape cannot be transformed safely."""


@dataclass(frozen=True)
class MigrationInputs:
    """One immutable selection of migration and hosted-evidence inputs."""

    snapshot_id: str
    issue_sources: tuple[tuple[str, bytes], ...]
    mapping_bytes: bytes | None
    preparer_bytes: bytes
    expected_canonical_sha256: str | None = None
    research_record_path: str | None = None
    research_record_bytes: bytes | None = None


@dataclass(frozen=True)
class CapturedHostedIdentity:
    """Identity fields projected from one captured hosted issue object."""

    local_id: str
    issue_number: int
    issue_id: int
    url: str
    title: str
    state: str
    body_sha256: str
    labels: tuple[str, ...]


@dataclass(frozen=True)
class CurrentIdentityIntake:
    """One immutable capture used to verify the current hosted identity map."""

    snapshot_id: str
    descriptor_path: str
    descriptor_sha256: str
    raw_response_path: str
    raw_response_sha256: str
    read_binding_path: str
    read_binding_sha256: str
    read_at: str
    endpoint: str
    issue_count: int
    duplicate_number: int
    duplicate_of: int
    identities: tuple[CapturedHostedIdentity, ...]


def canonical_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n"


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def sha256_text(value: str) -> str:
    return sha256_bytes(value.encode("utf-8"))


def select_current_identity_intake(root: Path, snapshot_id: str) -> CurrentIdentityIntake:
    """Select the one retained complete issue-list capture for current mapping.

    This is a separate current identity input.  It does not select or modify
    the historical migration bundle, whose input selector remains unchanged.
    """

    if snapshot_id != CURRENT_IDENTITY_INTAKE_SNAPSHOT_ID:
        raise MigrationError(f"unknown current identity intake snapshot: {snapshot_id}")

    descriptor_path = root / CURRENT_IDENTITY_DESCRIPTOR_PATH
    raw_path = root / CURRENT_IDENTITY_RAW_RESPONSE_PATH
    binding_path = root / CURRENT_IDENTITY_READ_BINDING_PATH
    try:
        descriptor_bytes = descriptor_path.read_bytes()
    except OSError as error:
        raise MigrationError(f"missing current identity intake descriptor: {descriptor_path}") from error
    if sha256_bytes(descriptor_bytes) != CURRENT_IDENTITY_INTAKE_DESCRIPTOR_SHA256:
        raise MigrationError("current identity intake descriptor bytes changed")
    try:
        descriptor = json.loads(descriptor_bytes.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise MigrationError(f"invalid current identity intake descriptor: {error}") from error
    if (
        descriptor.get("schemaVersion") != 1
        or descriptor.get("kind") != "wayfinder-current-hosted-identity-intake"
        or descriptor.get("snapshotId") != snapshot_id
        or descriptor.get("repository") != REPOSITORY
        or descriptor.get("rawResponse")
        != {
            "path": CURRENT_IDENTITY_RAW_RESPONSE_PATH,
            "sha256": CURRENT_IDENTITY_RAW_RESPONSE_SHA256,
            "issueCount": CURRENT_IDENTITY_ISSUE_COUNT,
        }
        or descriptor.get("readBinding", {}).get("path") != CURRENT_IDENTITY_READ_BINDING_PATH
        or descriptor.get("readBinding", {}).get("sha256") != CURRENT_IDENTITY_READ_BINDING_SHA256
        or descriptor.get("readBinding", {}).get("rawExit") != 0
        or descriptor.get("identityProjection")
        != {
            "canonicalIssueCount": CURRENT_IDENTITY_CANONICAL_COUNT,
            "excludedIssues": [
                {
                    "number": CURRENT_IDENTITY_DUPLICATE_NUMBER,
                    "duplicateOf": CURRENT_IDENTITY_DUPLICATE_OF,
                    "marker": "effra-wayfinder-migration-duplicate-of",
                }
            ],
        }
    ):
        raise MigrationError("current identity intake descriptor identity changed")

    try:
        raw_response_bytes = raw_path.read_bytes()
    except OSError as error:
        raise MigrationError(f"missing current identity intake raw response: {raw_path}") from error
    if sha256_bytes(raw_response_bytes) != CURRENT_IDENTITY_RAW_RESPONSE_SHA256:
        raise MigrationError("current identity intake raw response bytes changed")
    try:
        read_binding_bytes = binding_path.read_bytes()
    except OSError as error:
        raise MigrationError(f"missing current identity intake read binding: {binding_path}") from error
    if sha256_bytes(read_binding_bytes) != CURRENT_IDENTITY_READ_BINDING_SHA256:
        raise MigrationError("current identity intake read binding bytes changed")
    try:
        pages = json.loads(raw_response_bytes.decode("utf-8"))
        read_binding = json.loads(read_binding_bytes.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise MigrationError(f"invalid current identity intake JSON: {error}") from error

    if not isinstance(pages, list) or len(pages) != 1 or not isinstance(pages[0], list):
        raise MigrationError("current identity intake must contain one captured issues page")
    issues = pages[0]
    if len(issues) != CURRENT_IDENTITY_ISSUE_COUNT:
        raise MigrationError("current identity intake issue count changed")
    issue_numbers: set[int] = set()
    issue_ids: set[int] = set()
    state_counts = {"open": 0, "closed": 0}
    identities: list[CapturedHostedIdentity] = []
    excluded: list[tuple[int, int]] = []
    seen_local_ids: set[str] = set()
    marker_pattern = re.compile(r"\A<!-- effra-wayfinder-id: ([A-Za-z0-9][A-Za-z0-9._-]*) -->\r?\n")
    duplicate_pattern = re.compile(r"\A<!-- effra-wayfinder-migration-duplicate-of: ([1-9][0-9]*) -->\r?\n\r?\n")
    for issue in issues:
        if not isinstance(issue, dict) or "pull_request" in issue:
            raise MigrationError("current identity intake contains a non-issue row")
        number = issue.get("number")
        issue_id = issue.get("id")
        if type(number) is not int or number <= 0 or number in issue_numbers:
            raise MigrationError("current identity intake has an invalid or duplicate issue number")
        if type(issue_id) is not int or issue_id <= 0 or issue_id in issue_ids:
            raise MigrationError("current identity intake has an invalid or duplicate hosted issue ID")
        issue_numbers.add(number)
        issue_ids.add(issue_id)
        url = issue.get("html_url")
        title = issue.get("title")
        state = issue.get("state")
        body = issue.get("body")
        labels = issue.get("labels")
        if url != f"{HOSTED_ROOT}/issues/{number}":
            raise MigrationError(f"current issue #{number} URL differs from its canonical issue identity")
        if not isinstance(title, str) or not title or state not in state_counts or not isinstance(body, str):
            raise MigrationError(f"current issue #{number} lacks captured title, state, or body")
        if not isinstance(labels, list) or any(not isinstance(label, dict) or not isinstance(label.get("name"), str) for label in labels):
            raise MigrationError(f"current issue #{number} has invalid captured labels")
        label_names = tuple(label["name"] for label in labels)
        if len(label_names) != len(set(label_names)):
            raise MigrationError(f"current issue #{number} has duplicate captured label names")
        state_counts[state] += 1

        duplicate = duplicate_pattern.match(body)
        if number == CURRENT_IDENTITY_DUPLICATE_NUMBER:
            if duplicate is None or int(duplicate.group(1)) != CURRENT_IDENTITY_DUPLICATE_OF or state != "closed":
                raise MigrationError("current identity duplicate exclusion marker changed")
            excluded.append((number, int(duplicate.group(1))))
            continue
        if duplicate is not None:
            raise MigrationError(f"current issue #{number} has an unexpected duplicate exclusion marker")
        marker = marker_pattern.match(body)
        if marker is None:
            raise MigrationError(f"current issue #{number} lacks a leading canonical local identity marker")
        local_id = marker.group(1)
        if local_id in seen_local_ids:
            raise MigrationError(f"current intake contains duplicate local marker: {local_id}")
        seen_local_ids.add(local_id)
        identities.append(
            CapturedHostedIdentity(
                local_id=local_id,
                issue_number=number,
                issue_id=issue_id,
                url=url,
                title=title,
                state=state,
                body_sha256=sha256_bytes(body.encode("utf-8")),
                labels=label_names,
            )
        )

    if issue_numbers != set(range(1, CURRENT_IDENTITY_ISSUE_COUNT + 1)):
        raise MigrationError("current identity intake issue number set changed")
    if len(issue_ids) != CURRENT_IDENTITY_ISSUE_COUNT:
        raise MigrationError("current identity intake hosted issue IDs are not unique")
    if excluded != [(CURRENT_IDENTITY_DUPLICATE_NUMBER, CURRENT_IDENTITY_DUPLICATE_OF)]:
        raise MigrationError("current identity intake duplicate exclusion set changed")
    if len(identities) != CURRENT_IDENTITY_CANONICAL_COUNT:
        raise MigrationError("current identity intake canonical identity count changed")
    if (
        read_binding.get("endpoint") != descriptor.get("readBinding", {}).get("endpoint")
        or read_binding.get("readAt") != descriptor.get("readBinding", {}).get("readAt")
        or read_binding.get("rawSHA256") != CURRENT_IDENTITY_RAW_RESPONSE_SHA256
        or read_binding.get("rawExit") != 0
        or read_binding.get("issueCount") != CURRENT_IDENTITY_ISSUE_COUNT
        or read_binding.get("numbers") != list(range(1, CURRENT_IDENTITY_ISSUE_COUNT + 1))
        or read_binding.get("states") != state_counts
    ):
        raise MigrationError("current identity intake read binding does not match its raw response")

    return CurrentIdentityIntake(
        snapshot_id=snapshot_id,
        descriptor_path=CURRENT_IDENTITY_DESCRIPTOR_PATH,
        descriptor_sha256=CURRENT_IDENTITY_INTAKE_DESCRIPTOR_SHA256,
        raw_response_path=CURRENT_IDENTITY_RAW_RESPONSE_PATH,
        raw_response_sha256=CURRENT_IDENTITY_RAW_RESPONSE_SHA256,
        read_binding_path=CURRENT_IDENTITY_READ_BINDING_PATH,
        read_binding_sha256=CURRENT_IDENTITY_READ_BINDING_SHA256,
        read_at=read_binding["readAt"],
        endpoint=read_binding["endpoint"],
        issue_count=CURRENT_IDENTITY_ISSUE_COUNT,
        duplicate_number=CURRENT_IDENTITY_DUPLICATE_NUMBER,
        duplicate_of=CURRENT_IDENTITY_DUPLICATE_OF,
        identities=tuple(sorted(identities, key=lambda item: item.local_id)),
    )


def build_current_identity_mapping(
    intake: CurrentIdentityIntake,
    rows: list[dict[str, Any]],
) -> dict[str, Any]:
    """Build the current mapping from captured hosted IDs and current local markers."""

    rows_by_id = {row["metadata"]["id"]: row for row in rows}
    if len(rows_by_id) != len(rows):
        raise MigrationError("current local issue markers are not unique")
    captured_by_id = {identity.local_id: identity for identity in intake.identities}
    if set(rows_by_id) != set(captured_by_id):
        missing = sorted(set(rows_by_id) - set(captured_by_id))
        extra = sorted(set(captured_by_id) - set(rows_by_id))
        raise MigrationError(f"current local markers differ from captured identities (missing={missing}, extra={extra})")

    identities: list[dict[str, Any]] = []
    for local_id in sorted(captured_by_id):
        captured = captured_by_id[local_id]
        row = rows_by_id[local_id]
        identities.append(
            {
                "localId": local_id,
                "localTitle": row["metadata"]["title"],
                "localSourceSha256": row["sourceSha256"],
                "hostedStatus": "verified",
                "hostedIssueNumber": captured.issue_number,
                "hostedIssueId": captured.issue_id,
                "hostedUrl": captured.url,
                "hostedTitle": captured.title,
                "hostedState": captured.state,
                "hostedBodySha256": captured.body_sha256,
                "hostedLabels": list(captured.labels),
                "identityKind": "captured-current-issue-list",
            }
        )
    return {
        "schemaVersion": SCHEMA_VERSION,
        "repository": REPOSITORY,
        "historicalLinkRevision": dict(HISTORICAL_LINK_REVISION),
        "mapLocalId": "map",
        "migrationStatus": "read-only-current-identity-capture",
        "currentIdentityIntake": {
            "snapshotId": intake.snapshot_id,
            "descriptorPath": intake.descriptor_path,
            "descriptorSha256": intake.descriptor_sha256,
            "rawResponsePath": intake.raw_response_path,
            "rawResponseSha256": intake.raw_response_sha256,
            "readBindingPath": intake.read_binding_path,
            "readBindingSha256": intake.read_binding_sha256,
            "readAt": intake.read_at,
            "endpoint": intake.endpoint,
            "issueCount": intake.issue_count,
            "canonicalIdentityCount": len(intake.identities),
            "excludedDuplicate": {"number": intake.duplicate_number, "duplicateOf": intake.duplicate_of},
        },
        "localIssueCount": len(rows),
        "verifiedHostedCount": len(identities),
        "pendingHostedCount": 0,
        "identities": identities,
    }


def git_object_type(root: Path, revision: str, relative: str) -> str | None:
    """Return the Git object type at ``revision:path`` when it is present.

    The migration is deliberately offline.  This asks only the repository that
    owns the input snapshot; it never consults a hosted repository or a source
    cache.  A missing path is meaningful evidence for an unresolved link, but
    an unavailable revision or object store is a preparation error: otherwise
    the same nominal input snapshot could produce a different payload merely
    because its historical evidence was unavailable.
    """

    try:
        revision_check = subprocess.run(
            ["git", "-C", str(root), "cat-file", "-e", f"{revision}^{{commit}}"],
            check=False,
            capture_output=True,
            text=True,
        )
        if revision_check.returncode != 0:
            raise MigrationError(f"historical link revision unavailable: {revision}")
        result = subprocess.run(
            ["git", "-C", str(root), "ls-tree", "-z", revision, "--", relative],
            check=False,
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            raise MigrationError(f"historical link object store unavailable: {revision}:{relative}")
    except OSError as error:
        raise MigrationError(f"historical link Git metadata unavailable: {root}") from error

    for entry in result.stdout.split("\0"):
        if not entry:
            continue
        header, _, entry_path = entry.partition("\t")
        if entry_path != relative:
            continue
        fields = header.split()
        if len(fields) >= 2 and fields[1] in {"blob", "tree"}:
            return fields[1]
    return None


def canonical_input_manifest(
    root: Path,
    rows: list[dict[str, Any]],
    mapping: dict[str, Any],
    *,
    preparer_bytes: bytes | None = None,
    mapping_path: str | None = None,
    mapping_bytes: bytes | None = None,
    current_identity_intake: CurrentIdentityIntake | None = None,
) -> dict[str, Any]:
    """Describe every non-generated input used to prepare the payloads.

    The digest covers issue bytes and parsed metadata, the exact preparer and
    the canonical hosted mapping.  Generated payloads are intentionally absent
    so the source digest cannot become circular.
    """

    if current_identity_intake is None and (mapping_path is not None or mapping_bytes is not None):
        raise MigrationError("selected current mapping path and bytes require explicit current identity intake")
    if current_identity_intake is not None:
        if mapping_path is None or mapping_bytes is None:
            raise MigrationError("current identity manifest requires the exact selected mapping path and bytes")
        selected_mapping_path = PurePosixPath(mapping_path)
        if (
            selected_mapping_path.is_absolute()
            or ".." in selected_mapping_path.parts
            or selected_mapping_path.as_posix() != mapping_path
            or mapping_path == "docs/wayfinder/hosted-identities.json"
        ):
            raise MigrationError("current identity manifest requires a distinct repository-relative mapping path")

    issue_inputs: list[dict[str, Any]] = []
    for row in rows:
        metadata_text = canonical_json(row["metadata"])
        issue_inputs.append(
            {
                "path": str(row["path"].relative_to(root)),
                "sourceSha256": row["sourceSha256"],
                "metadata": row["metadata"],
                "metadataSha256": sha256_text(metadata_text),
            }
        )
    preparer_path = root / "scripts" / "wayfinder_migration.py"
    if preparer_bytes is None and not preparer_path.is_file():
        raise MigrationError(f"missing preparer input: {preparer_path.relative_to(root)}")
    if preparer_bytes is None:
        preparer_bytes = preparer_path.read_bytes()
    mapping_text = canonical_json(mapping)
    hosted_mapping_path = mapping_path or "docs/wayfinder/hosted-identities.json"
    record = {
        "schemaVersion": INPUT_MANIFEST_SCHEMA_VERSION,
        "kind": "wayfinder-input-snapshot",
        "repository": REPOSITORY,
        "historicalLinkRevision": dict(HISTORICAL_LINK_REVISION),
        "inputs": {
            "issues": issue_inputs,
            "preparer": {
                "path": "scripts/wayfinder_migration.py",
                "sha256": sha256_bytes(preparer_bytes),
            },
            "hostedMapping": {
                "path": hosted_mapping_path,
                "sha256": sha256_bytes(mapping_bytes) if mapping_bytes is not None else sha256_text(mapping_text),
                "localIssueCount": mapping["localIssueCount"],
                "verifiedHostedCount": mapping["verifiedHostedCount"],
                "pendingHostedCount": mapping["pendingHostedCount"],
            },
        },
        "counts": {
            "issueFiles": len(issue_inputs),
            "verifiedHostedIdentities": mapping["verifiedHostedCount"],
            "pendingHostedIdentities": mapping["pendingHostedCount"],
        },
    }
    if current_identity_intake is not None:
        record["inputs"]["currentIdentityIntake"] = {
            "snapshotId": current_identity_intake.snapshot_id,
            "descriptor": {
                "path": current_identity_intake.descriptor_path,
                "sha256": current_identity_intake.descriptor_sha256,
            },
            "rawResponse": {
                "path": current_identity_intake.raw_response_path,
                "sha256": current_identity_intake.raw_response_sha256,
                "issueCount": current_identity_intake.issue_count,
            },
            "readBinding": {
                "path": current_identity_intake.read_binding_path,
                "sha256": current_identity_intake.read_binding_sha256,
                "readAt": current_identity_intake.read_at,
                "endpoint": current_identity_intake.endpoint,
            },
            "canonicalIdentityCount": len(current_identity_intake.identities),
            "excludedDuplicate": {
                "number": current_identity_intake.duplicate_number,
                "duplicateOf": current_identity_intake.duplicate_of,
            },
        }
    return {
        "record": record,
        "sha256": sha256_text(canonical_json(record)),
    }


def parse_metadata(path: Path, source: str | None = None) -> dict[str, Any]:
    """Parse the metadata line from already-decoded issue source.

    ``issue_snapshot`` passes the text decoded from its one raw-byte read.  A
    path-only call remains useful to small callers, but it also decodes bytes
    explicitly instead of using text-mode newline translation.
    """

    if source is None:
        try:
            source = path.read_bytes().decode("utf-8")
        except UnicodeDecodeError as error:
            raise MigrationError(f"{path}: issue source is not valid UTF-8") from error
    lines = source.splitlines(keepends=True)
    if not lines or not lines[0].startswith(METADATA_PREFIX) or not lines[0].rstrip().endswith(METADATA_SUFFIX):
        raise MigrationError(f"{path}: first line is not Wayfinder metadata")
    try:
        metadata = json.loads(lines[0].strip()[len(METADATA_PREFIX) : -len(METADATA_SUFFIX)])
    except json.JSONDecodeError as error:
        raise MigrationError(f"{path}: invalid metadata JSON: {error}") from error
    expected = {"id", "title", "status", "labels", "parent", "assignee", "blocked_by"}
    if set(metadata) != expected:
        raise MigrationError(f"{path}: metadata keys differ from {sorted(expected)}")
    if not isinstance(metadata["id"], str) or not metadata["id"]:
        raise MigrationError(f"{path}: issue id must be a non-empty string")
    if not isinstance(metadata["title"], str) or not metadata["title"]:
        raise MigrationError(f"{path}: issue title must be a non-empty string")
    if metadata["status"] not in {"open", "closed"}:
        raise MigrationError(f"{path}: invalid status {metadata['status']!r}")
    if not isinstance(metadata["labels"], list) or not all(isinstance(label, str) for label in metadata["labels"]):
        raise MigrationError(f"{path}: labels must be a string list")
    if metadata["parent"] is not None and not isinstance(metadata["parent"], str):
        raise MigrationError(f"{path}: parent must be null or a local issue ID")
    if not isinstance(metadata["blocked_by"], list) or not all(isinstance(ref, str) for ref in metadata["blocked_by"]):
        raise MigrationError(f"{path}: blocked_by must be a string list")
    return metadata


def issue_snapshot(root: Path, inputs: MigrationInputs | None = None) -> list[dict[str, Any]]:
    directory = root / "docs" / "wayfinder" / "issues"
    rows: list[dict[str, Any]] = []
    seen: set[str] = set()
    if inputs is None:
        source_items = tuple((path.name, path.read_bytes()) for path in sorted(directory.glob("*.md")))
    else:
        source_items = inputs.issue_sources
    for name, raw_source in source_items:
        if Path(name).name != name or not name.endswith(".md"):
            selected = inputs.snapshot_id if inputs is not None else "live"
            raise MigrationError(f"invalid issue input path in {selected} snapshot: {name}")
        path = directory / name
        try:
            decoded_source = raw_source.decode("utf-8")
        except UnicodeDecodeError as error:
            raise MigrationError(f"{path}: issue source is not valid UTF-8") from error
        # Preserve raw_source for identity while making the semantic body
        # newline-stable from that same read.  A second filesystem read here
        # could bind a changed body to the original byte hash.
        source = decoded_source.replace("\r\n", "\n").replace("\r", "\n")
        metadata = parse_metadata(path, source)
        local_id = metadata["id"]
        if local_id in seen:
            raise MigrationError(f"duplicate local issue identity: {local_id}")
        seen.add(local_id)
        rows.append(
            {
                "metadata": metadata,
                "path": path,
                "source": source,
                "sourceSha256": sha256_bytes(raw_source),
            }
        )
    if not rows:
        raise MigrationError(f"no local issues found under {directory}")
    by_id = {row["metadata"]["id"]: row for row in rows}
    for row in rows:
        metadata = row["metadata"]
        refs = list(metadata["blocked_by"])
        if metadata["parent"] is not None:
            refs.append(metadata["parent"])
        for ref in refs:
            if ref not in by_id:
                raise MigrationError(f"{metadata['id']}: unknown local reference {ref}")
    visit_state: dict[str, int] = {}

    def visit(local_id: str) -> None:
        state = visit_state.get(local_id, 0)
        if state == 1:
            raise MigrationError(f"local dependency cycle includes {local_id}")
        if state == 2:
            return
        visit_state[local_id] = 1
        for ref in by_id[local_id]["metadata"]["blocked_by"]:
            visit(ref)
        visit_state[local_id] = 2

    for local_id in sorted(by_id):
        visit(local_id)
    return rows


def bootstrap_mapping(rows: list[dict[str, Any]]) -> dict[str, Any]:
    identities: list[dict[str, Any]] = []
    local_ids = {row["metadata"]["id"] for row in rows}
    for row in rows:
        metadata = row["metadata"]
        local_id = metadata["id"]
        known = KNOWN_HOSTED.get(local_id)
        if known is None:
            identities.append(
                {
                    "localId": local_id,
                    "localTitle": metadata["title"],
                    "hostedStatus": "pending",
                    "hostedIssueNumber": None,
                    "hostedIssueId": None,
                    "hostedUrl": None,
                    "hostedTitle": None,
                    "hostedState": None,
                    "hostedBodySha256": None,
                    "identityKind": "local-snapshot",
                }
            )
        else:
            identity = dict(known)
            identity["localTitle"] = metadata["title"]
            identity["hostedStatus"] = "verified"
            identities.append(identity)
    for local_id, known in sorted(KNOWN_HOSTED.items()):
        if local_id in local_ids:
            continue
        if known["identityKind"] != "hosted-only":
            raise MigrationError(f"known hosted identity {local_id} has no local snapshot")
        identity = dict(known)
        identity["localTitle"] = None
        identity["hostedStatus"] = "verified"
        identities.append(identity)
    identities.sort(key=lambda item: (item["localId"] is None, item["localId"] or item["hostedUrl"]))
    return {
        "schemaVersion": SCHEMA_VERSION,
        "repository": REPOSITORY,
        "historicalLinkRevision": dict(HISTORICAL_LINK_REVISION),
        "mapLocalId": "map",
        "migrationStatus": "pending-first-hosted-publication",
        "localIssueCount": len(rows),
        "verifiedHostedCount": sum(identity["hostedStatus"] == "verified" for identity in identities),
        "pendingHostedCount": sum(identity["hostedStatus"] == "pending" for identity in identities),
        "identities": identities,
    }


def load_mapping(
    path: Path,
    rows: list[dict[str, Any]],
    *,
    mapping_bytes: bytes | None = None,
    current_identity_intake: CurrentIdentityIntake | None = None,
) -> dict[str, Any]:
    if mapping_bytes is None:
        if not path.exists():
            if current_identity_intake is not None:
                raise MigrationError(f"missing current identity mapping: {path}")
            return bootstrap_mapping(rows)
        mapping_bytes = path.read_bytes()
    try:
        mapping = json.loads(mapping_bytes.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise MigrationError(f"{path}: invalid mapping JSON: {error}") from error
    if not isinstance(mapping, dict) or mapping.get("schemaVersion") != SCHEMA_VERSION:
        raise MigrationError(f"{path}: unsupported identity mapping schema")
    if mapping.get("repository") != REPOSITORY or mapping.get("historicalLinkRevision") != HISTORICAL_LINK_REVISION:
        raise MigrationError(f"{path}: repository or historical link revision differs from the accepted snapshot")
    identities = mapping.get("identities")
    if not isinstance(identities, list):
        raise MigrationError(f"{path}: identities must be a list")
    if current_identity_intake is not None:
        expected_mapping = build_current_identity_mapping(current_identity_intake, rows)
        if canonical_json(mapping) != canonical_json(expected_mapping):
            expected_by_id = {identity["localId"]: identity for identity in expected_mapping["identities"]}
            actual_by_id: dict[str, Any] = {}
            duplicate_ids: set[str] = set()
            for identity in identities:
                if not isinstance(identity, dict) or not isinstance(identity.get("localId"), str):
                    raise MigrationError(f"{path}: current mapping contains an identity without a local marker")
                local_id = identity["localId"]
                if local_id in actual_by_id:
                    duplicate_ids.add(local_id)
                actual_by_id[local_id] = identity
            if duplicate_ids:
                raise MigrationError(f"{path}: duplicate current local identity {sorted(duplicate_ids)[0]}")
            if set(actual_by_id) != set(expected_by_id):
                missing = sorted(set(expected_by_id) - set(actual_by_id))
                extra = sorted(set(actual_by_id) - set(expected_by_id))
                raise MigrationError(f"{path}: current identity set differs (missing={missing}, extra={extra})")
            for local_id, expected_identity in expected_by_id.items():
                actual_identity = actual_by_id[local_id]
                for field, expected_value in expected_identity.items():
                    if canonical_json(actual_identity.get(field)) != canonical_json(expected_value):
                        raise MigrationError(
                            f"{path}: current identity {local_id} field {field} differs from captured inputs"
                        )
                if set(actual_identity) != set(expected_identity):
                    raise MigrationError(f"{path}: current identity {local_id} has unexpected fields")
            for field, expected_value in expected_mapping.items():
                if field == "identities":
                    continue
                if canonical_json(mapping.get(field)) != canonical_json(expected_value):
                    raise MigrationError(f"{path}: current mapping field {field} differs from captured inputs")
            if set(mapping) != set(expected_mapping):
                raise MigrationError(f"{path}: current mapping has unexpected fields")
        return mapping
    local_ids = {row["metadata"]["id"] for row in rows}
    mapped_local_ids = {identity.get("localId") for identity in identities if identity.get("localId") is not None}
    if mapped_local_ids != local_ids:
        # A previously generated mapping may contain a verified hosted-only
        # identity that now has a checked-in local snapshot. Promote only when
        # the hosted issue number and every immutable hosted field match the
        # known identity; never infer a new number or URL from local ordering.
        known_by_number = {
            known["hostedIssueNumber"]: (local_id, known)
            for local_id, known in KNOWN_HOSTED.items()
            if known["identityKind"] == "local-snapshot" and local_id in local_ids
        }
        promoted: list[dict[str, Any]] = []
        for identity in identities:
            if identity.get("localId") is None and identity.get("hostedStatus") == "verified":
                candidate = known_by_number.get(identity.get("hostedIssueNumber"))
                if candidate is not None:
                    local_id, known = candidate
                    immutable_fields = (
                        "hostedIssueNumber",
                        "hostedIssueId",
                        "hostedUrl",
                        "hostedTitle",
                        "hostedState",
                        "hostedBodySha256",
                    )
                    if all(identity.get(field) == known.get(field) for field in immutable_fields):
                        promoted_identity = dict(identity)
                        promoted_identity["localId"] = local_id
                        promoted_identity["localTitle"] = next(
                            row["metadata"]["title"] for row in rows if row["metadata"]["id"] == local_id
                        )
                        promoted_identity["identityKind"] = "local-snapshot"
                        promoted.append(promoted_identity)
                        continue
            promoted.append(identity)
        identities = promoted
        mapped_local_ids = {identity.get("localId") for identity in identities if identity.get("localId") is not None}
    if mapped_local_ids - local_ids:
        extra = sorted(mapped_local_ids - local_ids)
        raise MigrationError(f"{path}: local identity set differs (extra={extra})")
    for row in rows:
        local_id = row["metadata"]["id"]
        if local_id in mapped_local_ids:
            continue
        known = KNOWN_HOSTED.get(local_id)
        if known is not None:
            identity = dict(known)
            identity["localTitle"] = row["metadata"]["title"]
            identity["hostedStatus"] = "verified"
        else:
            identity = {
                "localId": local_id,
                "localTitle": row["metadata"]["title"],
                "hostedStatus": "pending",
                "hostedIssueNumber": None,
                "hostedIssueId": None,
                "hostedUrl": None,
                "hostedTitle": None,
                "hostedState": None,
                "hostedBodySha256": None,
                "identityKind": "local-snapshot",
            }
        identities.append(identity)
        mapped_local_ids.add(local_id)
    if mapped_local_ids != local_ids:
        missing = sorted(local_ids - mapped_local_ids)
        extra = sorted(mapped_local_ids - local_ids)
        raise MigrationError(f"{path}: local identity set differs (missing={missing}, extra={extra})")
    mapping["identities"] = identities
    mapping["localIssueCount"] = len(rows)
    mapping["verifiedHostedCount"] = sum(identity.get("hostedStatus") == "verified" for identity in identities)
    mapping["pendingHostedCount"] = sum(identity.get("hostedStatus") == "pending" for identity in identities)
    identities.sort(key=lambda item: (item.get("localId") is None, item.get("localId") or item.get("hostedUrl")))
    seen_hosted: set[int] = set()
    seen_local: set[str] = set()
    for identity in identities:
        local_id = identity.get("localId")
        if local_id is not None:
            if local_id in seen_local:
                raise MigrationError(f"{path}: duplicate local identity {local_id}")
            seen_local.add(local_id)
        number = identity.get("hostedIssueNumber")
        hosted_status = identity.get("hostedStatus")
        if hosted_status not in {"pending", "verified"}:
            raise MigrationError(f"{path}: invalid hostedStatus for {local_id}: {hosted_status!r}")
        if hosted_status == "pending":
            if any(identity.get(field) is not None for field in ("hostedIssueNumber", "hostedIssueId", "hostedUrl", "hostedTitle", "hostedState", "hostedBodySha256")):
                raise MigrationError(f"{path}: pending identity {local_id} contains hosted data")
        else:
            if not isinstance(number, int) or number <= 0:
                raise MigrationError(f"{path}: verified identity {local_id} lacks a hosted issue number")
            if number in seen_hosted:
                raise MigrationError(f"{path}: duplicate hosted issue number {number}")
            seen_hosted.add(number)
            if not isinstance(identity.get("hostedIssueId"), int) or not isinstance(identity.get("hostedUrl"), str):
                raise MigrationError(f"{path}: verified identity {local_id} lacks exact hosted identity")
    return mapping


def select_migration_inputs(
    root: Path,
    snapshot_id: str | None = None,
    mapping_path: Path | None = None,
) -> MigrationInputs:
    """Select current inputs or validate the explicitly named historical bundle.

    Historical selection never falls back to issue, mapping, or preparer bytes
    from the live checkout. The descriptor, bundle, and original generated
    input manifest are all pinned before the returned byte snapshot is used.
    """

    issue_directory = root / "docs" / "wayfinder" / "issues"
    preparer_path = root / "scripts" / "wayfinder_migration.py"
    selected_mapping_path = mapping_path or root / "docs" / "wayfinder" / "hosted-identities.json"
    if snapshot_id is None:
        if not issue_directory.is_dir():
            raise MigrationError(f"missing live issue input directory: {issue_directory}")
        try:
            issue_sources = tuple((path.name, path.read_bytes()) for path in sorted(issue_directory.glob("*.md")))
            preparer_bytes = preparer_path.read_bytes()
            mapping_bytes = selected_mapping_path.read_bytes() if selected_mapping_path.is_file() else None
        except OSError as error:
            raise MigrationError(f"could not read current migration inputs: {error}") from error
        return MigrationInputs("live", issue_sources, mapping_bytes, preparer_bytes)

    if snapshot_id != HISTORICAL_INPUT_SNAPSHOT_ID:
        raise MigrationError(f"unknown historical input snapshot: {snapshot_id}")
    if mapping_path is not None:
        raise MigrationError("--mapping cannot override a historical input snapshot")

    snapshot_directory = root / "docs" / "wayfinder" / "migration" / "snapshots" / snapshot_id
    descriptor_path = snapshot_directory / "snapshot.json"
    try:
        descriptor_bytes = descriptor_path.read_bytes()
    except OSError as error:
        raise MigrationError(f"missing historical input snapshot descriptor: {descriptor_path}") from error
    if sha256_bytes(descriptor_bytes) != HISTORICAL_INPUT_SNAPSHOT_DESCRIPTOR_SHA256:
        raise MigrationError("historical input snapshot descriptor bytes changed")
    try:
        descriptor = json.loads(descriptor_bytes.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise MigrationError(f"invalid historical input snapshot descriptor: {error}") from error

    expected_input_record = descriptor.get("historicalInputManifest", {})
    manifest_path = root / expected_input_record.get("path", "")
    try:
        manifest_bytes = manifest_path.read_bytes()
    except OSError as error:
        raise MigrationError(f"missing historical input manifest: {manifest_path}") from error
    if sha256_bytes(manifest_bytes) != HISTORICAL_INPUT_MANIFEST_SHA256:
        raise MigrationError("historical input manifest bytes changed")
    try:
        input_manifest = json.loads(manifest_bytes.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise MigrationError(f"invalid historical input manifest: {error}") from error
    if (
        expected_input_record.get("fileSha256") != HISTORICAL_INPUT_MANIFEST_SHA256
        or expected_input_record.get("canonicalContentSha256") != HISTORICAL_INPUT_CANONICAL_SHA256
        or input_manifest.get("canonicalContentSha256") != HISTORICAL_INPUT_CANONICAL_SHA256
    ):
        raise MigrationError("historical input manifest identity changed")

    bundle_record = descriptor.get("bundle", {})
    bundle_path = root / bundle_record.get("path", "")
    try:
        bundle_bytes = bundle_path.read_bytes()
    except OSError as error:
        raise MigrationError(f"missing historical input bundle: {bundle_path}") from error
    if sha256_bytes(bundle_bytes) != bundle_record.get("sha256"):
        raise MigrationError("historical input bundle bytes changed")

    expected_issues = input_manifest.get("inputs", {}).get("issues", [])
    if len(expected_issues) != descriptor.get("issues", {}).get("count"):
        raise MigrationError("historical input issue count differs from its descriptor")
    expected_names: dict[str, str] = {}
    for issue in expected_issues:
        source_path = PurePosixPath(issue.get("path", ""))
        if source_path.parent != PurePosixPath("docs/wayfinder/issues") or source_path.suffix != ".md":
            raise MigrationError(f"invalid historical issue path: {source_path}")
        expected_names[f"issues/{source_path.name}"] = issue["sourceSha256"]
    mapping_record = descriptor.get("hostedMapping", {})
    preparer_record = descriptor.get("preparer", {})
    research_record = descriptor.get("researchRecord", {})
    expected_names[mapping_record.get("pathInBundle", "")] = mapping_record.get("sha256", "")
    expected_names[preparer_record.get("pathInBundle", "")] = preparer_record.get("sha256", "")
    expected_names[research_record.get("pathInBundle", "")] = research_record.get("sha256", "")
    try:
        with tarfile.open(fileobj=io.BytesIO(bundle_bytes), mode="r:") as archive:
            members = archive.getmembers()
            names = [member.name for member in members]
            if (
                len(names) != len(set(names))
                or len(names) != bundle_record.get("entries")
                or set(names) != set(expected_names)
            ):
                raise MigrationError("historical input bundle entries differ from the bound input set")
            if any(not member.isfile() for member in members):
                raise MigrationError("historical input bundle contains a non-file entry")
            sources: dict[str, bytes] = {}
            for member in members:
                source = archive.extractfile(member)
                if source is None:
                    raise MigrationError(f"historical input bundle entry is unreadable: {member.name}")
                sources[member.name] = source.read()
    except (tarfile.TarError, OSError) as error:
        raise MigrationError(f"invalid historical input bundle: {error}") from error
    for name, expected_sha256 in expected_names.items():
        if sha256_bytes(sources[name]) != expected_sha256:
            raise MigrationError(f"historical input bytes changed: {name}")

    issue_sources = tuple(
        (PurePosixPath(issue["path"]).name, sources[f"issues/{PurePosixPath(issue['path']).name}"])
        for issue in expected_issues
    )
    inputs = MigrationInputs(
        snapshot_id,
        issue_sources,
        sources[mapping_record["pathInBundle"]],
        sources[preparer_record["pathInBundle"]],
        HISTORICAL_INPUT_CANONICAL_SHA256,
        research_record.get("sourcePath"),
        sources[research_record["pathInBundle"]],
    )
    rows = issue_snapshot(root, inputs)
    mapping = load_mapping(selected_mapping_path, rows, mapping_bytes=inputs.mapping_bytes)
    canonical = canonical_input_manifest(root, rows, mapping, preparer_bytes=inputs.preparer_bytes)
    if canonical["sha256"] != inputs.expected_canonical_sha256:
        raise MigrationError("historical input canonical binding changed")
    if canonical_json({**canonical["record"], "canonicalContentSha256": canonical["sha256"]}).encode("utf-8") != manifest_bytes:
        raise MigrationError("historical input manifest does not match its retained inputs")
    return inputs


def identity_index(mapping: dict[str, Any]) -> dict[str, dict[str, Any]]:
    result: dict[str, dict[str, Any]] = {}
    for identity in mapping["identities"]:
        local_id = identity.get("localId")
        if local_id is not None:
            result[local_id] = identity
    return result


def hosted_url(mapping: dict[str, Any], local_id: str, anchor: str = "") -> str | None:
    identity = identity_index(mapping).get(local_id)
    if identity is None or identity.get("hostedStatus") != "verified":
        return None
    return f"{identity['hostedUrl']}{anchor}"


def issue_id_for_path(root: Path, path: Path) -> str | None:
    issue_root = root / "docs" / "wayfinder" / "issues"
    try:
        relative = path.relative_to(issue_root)
    except ValueError:
        return None
    if relative.suffix != ".md" or len(relative.parts) != 1:
        return None
    return relative.stem


def lexical_relative_path(root: Path, source_path: Path, path_text: str) -> PurePosixPath | None:
    """Resolve a Markdown relative path without consulting the current tree.

    Historical link identity is the lexical source location plus the path in
    the Markdown document.  Normalizing ``..`` is necessary for containment,
    but following symlinks or checking the current filesystem would make the
    result depend on an unpublished working tree rather than the selected Git
    revision.
    """

    try:
        source_relative = source_path.relative_to(root)
    except ValueError as error:
        raise MigrationError(f"source path is outside the input snapshot: {source_path}") from error
    components = list(PurePosixPath(source_relative.as_posix()).parent.parts)
    components.extend(PurePosixPath(path_text).parts)
    normalized: list[str] = []
    for component in components:
        if component in {"", "."}:
            continue
        if component == "..":
            if not normalized:
                return None
            normalized.pop()
            continue
        normalized.append(component)
    return PurePosixPath(*normalized)


LINK_PATTERN = re.compile(r"(?P<open>\]\()(?P<target>[^)\s]+)(?P<close>\))")


def rewrite_links(
    markdown: str,
    *,
    root: Path,
    source_path: Path,
    mapping: dict[str, Any],
    unresolved: list[dict[str, str]],
    historical_revision: str = HISTORICAL_LINK_COMMIT,
) -> str:
    def replace(match: re.Match[str]) -> str:
        target = match.group("target")
        if target.startswith(("http://", "https://", "mailto:", "#", "/")):
            return match.group(0)
        path_text, anchor = urldefrag(target)
        target_relative = lexical_relative_path(root, source_path, path_text)
        if target_relative is None:
            unresolved.append(
                {
                    "kind": "path",
                    "source": target,
                    "status": "unresolved",
                    "reason": "outside-input-snapshot",
                }
            )
            return f"](local-reference:{target})"
        target_path = root.joinpath(*target_relative.parts)
        local_id = issue_id_for_path(root, target_path)
        if local_id is not None:
            resolved = hosted_url(mapping, local_id, f"#{anchor}" if anchor else "")
            if resolved is not None:
                return f"]({resolved})"
            unresolved.append({"kind": "issue", "localId": local_id, "source": target})
            unresolved_target = f"{LOCAL_ISSUE_MARKER}{local_id}"
            if anchor:
                unresolved_target += f"#{anchor}"
            return f"]({unresolved_target})"
        relative = target_relative
        kind = git_object_type(root, historical_revision, relative.as_posix())
        if kind is None:
            unresolved.append(
                {
                    "kind": "path",
                    "source": target,
                    "path": relative.as_posix(),
                    "status": "unresolved",
                    "reason": "absent-at-historical-link-revision",
                    "historicalLinkCommit": historical_revision,
                }
            )
            return f"](local-reference:{relative.as_posix()})"
        rewritten = f"{HOSTED_ROOT}/{kind}/{historical_revision}/{relative.as_posix()}"
        if anchor:
            rewritten += f"#{anchor}"
        return f"]({rewritten})"

    return LINK_PATTERN.sub(replace, markdown)


def strip_metadata_and_title(source: str, title: str) -> str:
    lines = source.splitlines(keepends=True)
    if not lines:
        raise MigrationError("empty issue source")
    body = lines[1:]
    if body and body[0].startswith("# "):
        expected_heading = f"# {title}"
        if body[0].rstrip("\n") != expected_heading:
            raise MigrationError(f"title heading differs from metadata: {body[0].rstrip()!r} != {expected_heading!r}")
        body = body[1:]
    return "".join(body).strip() + "\n"


HEADING_PATTERN = re.compile(r"^## .+$")


def sections(markdown: str) -> list[tuple[str, str]]:
    lines = markdown.splitlines(keepends=True)
    starts = [index for index, line in enumerate(lines) if HEADING_PATTERN.match(line.rstrip("\n"))]
    result: list[tuple[str, str]] = []
    if not starts:
        return [("", markdown)] if markdown.strip() else []
    if starts[0] > 0 and "".join(lines[: starts[0]]).strip():
        result.append(("", "".join(lines[: starts[0]])))
    for position, start in enumerate(starts):
        end = starts[position + 1] if position + 1 < len(starts) else len(lines)
        heading = lines[start].strip()[3:].strip()
        result.append((heading, "".join(lines[start:end])))
    return result


def first_paragraph_split(markdown: str) -> tuple[str, str | None]:
    """Split an unheaded closed answer after its first paragraph."""
    lines = markdown.splitlines(keepends=True)
    for index in range(1, len(lines)):
        if not lines[index - 1].strip() and lines[index].strip():
            body = "".join(lines[:index]).strip() + "\n"
            remainder = "".join(lines[index:]).strip()
            return body, (remainder + "\n" if remainder else None)
    return markdown.strip() + "\n", None


def question_paragraph_split(markdown: str) -> tuple[str, str | None]:
    """Keep the first paragraph of a question section in the issue body."""
    lines = markdown.splitlines(keepends=True)
    content_start = 1 if lines and lines[0].lstrip().startswith("## ") else 0
    while content_start < len(lines) and not lines[content_start].strip():
        content_start += 1
    for index in range(content_start + 1, len(lines)):
        if not lines[index - 1].strip() and lines[index].strip():
            body = "".join(lines[:index]).strip() + "\n"
            remainder = "".join(lines[index:]).strip()
            return body, (remainder + "\n" if remainder else None)
    return markdown.strip() + "\n", None


def split_issue_content(row: dict[str, Any]) -> tuple[str, list[dict[str, Any]]]:
    metadata = row["metadata"]
    local_id = metadata["id"]
    content = strip_metadata_and_title(row["source"], metadata["title"])
    if metadata["status"] == "open":
        return content, []

    parsed = sections(content)
    question = next((body for heading, body in parsed if heading.lower() == "question"), None)
    if question is not None:
        question_index = next(index for index, (heading, _) in enumerate(parsed) if heading.lower() == "question")
        body = question
        comments = [
            {"ordinal": ordinal, "body": section_body, "sourceHeading": heading, "syntheticHeading": False}
            for ordinal, (heading, section_body) in enumerate(parsed[question_index + 1 :], 1)
            if section_body.strip()
        ]
        # A few early closed records put the resolution paragraph directly in
        # the Question section.  Keep its actual question paragraph in the
        # issue body and move the remainder into a named resolution comment.
        if len(parsed) == question_index + 1:
            body, remainder = question_paragraph_split(question)
            if remainder is not None:
                comments = [
                    {
                        "ordinal": 1,
                        "body": f"## Resolution\n\n{remainder}",
                        "sourceHeading": "Resolution",
                        "syntheticHeading": True,
                    }
                ]
        return body.strip() + "\n", comments

    resolution_index = next(
        (
            index
            for index, (heading, _) in enumerate(parsed)
            if heading.lower() == "resolution" or "resolution" in heading.lower()
        ),
        None,
    )
    if resolution_index is None:
        body, remainder = first_paragraph_split(content)
        comments = []
        if remainder is not None:
            comments.append(
                {
                    "ordinal": 1,
                    "body": f"## Resolution\n\n{remainder}",
                    "sourceHeading": "Resolution",
                    "syntheticHeading": True,
                }
            )
        return body, comments
    body = "".join(section_body for _, section_body in parsed[:resolution_index]).strip() + "\n"
    comments = [
        {"ordinal": ordinal, "body": section_body, "sourceHeading": heading, "syntheticHeading": False}
        for ordinal, (heading, section_body) in enumerate(parsed[resolution_index:], 1)
        if section_body.strip()
    ]
    return body, comments


def migrated_labels(labels: list[str]) -> list[str]:
    result = list(dict.fromkeys(labels))
    if any(label.startswith(IMPLEMENTATION_LABEL_PREFIXES) for label in labels) and "wayfinder:task" not in result:
        result.append("wayfinder:task")
    if any(label in RESEARCH_LABELS for label in labels) and "wayfinder:research" not in result:
        result.append("wayfinder:research")
    return result


def payload_header(input_snapshot: dict[str, Any], input_manifest_path: str) -> dict[str, Any]:
    return {
        "schemaVersion": SCHEMA_VERSION,
        "repository": REPOSITORY,
        "historicalLinkRevision": dict(HISTORICAL_LINK_REVISION),
        "inputSnapshot": {
            "path": input_manifest_path,
            "sha256": input_snapshot["sha256"],
        },
    }


def build_payloads(
    root: Path,
    rows: list[dict[str, Any]],
    mapping: dict[str, Any],
    input_snapshot: dict[str, Any],
    *,
    input_manifest_path: str = "docs/wayfinder/migration/input-manifest.json",
) -> dict[str, Any]:
    identity_by_local = identity_index(mapping)
    issues: list[dict[str, Any]] = []
    comments: list[dict[str, Any]] = []
    edges: list[dict[str, Any]] = []
    unresolved_refs: list[dict[str, str]] = []
    for row in rows:
        metadata = row["metadata"]
        local_id = metadata["id"]
        body, raw_comments = split_issue_content(row)
        body_unresolved: list[dict[str, str]] = []
        rewritten_body = rewrite_links(
            body,
            root=root,
            source_path=row["path"],
            mapping=mapping,
            unresolved=body_unresolved,
            historical_revision=HISTORICAL_LINK_COMMIT,
        )
        rewritten_comments: list[dict[str, Any]] = []
        for comment in raw_comments:
            comment_unresolved: list[dict[str, str]] = []
            rewritten = rewrite_links(
                comment["body"],
                root=root,
                source_path=row["path"],
                mapping=mapping,
                unresolved=comment_unresolved,
                historical_revision=HISTORICAL_LINK_COMMIT,
            )
            rewritten_comments.append({**comment, "body": rewritten, "unresolvedReferences": comment_unresolved})
            unresolved_refs.extend({"sourceLocalId": local_id, **ref} for ref in comment_unresolved)
        unresolved_refs.extend({"sourceLocalId": local_id, **ref} for ref in body_unresolved)
        identity = identity_by_local[local_id]
        is_verified = identity["hostedStatus"] == "verified"
        issues.append(
            {
                "localId": local_id,
                "title": metadata["title"],
                "operation": "preserve-existing" if is_verified else "create",
                "publishable": not is_verified,
                "desiredState": metadata["status"],
                "labels": migrated_labels(metadata["labels"]),
                "originalLabels": metadata["labels"],
                "historicalAssignee": metadata["assignee"],
                "hostedAssignees": [],
                "parentLocalId": metadata["parent"],
                "blockedByLocalIds": metadata["blocked_by"],
                "body": rewritten_body,
                "sourcePath": str(row["path"].relative_to(root)),
                "sourceSha256": row["sourceSha256"],
                "unresolvedReferences": body_unresolved,
                "hostedIdentity": {
                    "status": identity["hostedStatus"],
                    "number": identity.get("hostedIssueNumber"),
                    "issueId": identity.get("hostedIssueId"),
                    "url": identity.get("hostedUrl"),
                },
            }
        )
        comments.append(
            {
                "localId": local_id,
                "title": metadata["title"],
                "desiredState": metadata["status"],
                "comments": rewritten_comments,
                "sourceSha256": row["sourceSha256"],
            }
        )
        if metadata["parent"] is not None:
            edges.append(edge_payload("parent", local_id, metadata["parent"], identity_by_local, mapping))
        for blocker in metadata["blocked_by"]:
            edges.append(edge_payload("blocked-by", local_id, blocker, identity_by_local, mapping))
    if len(issues) != len(rows) or len(comments) != len(rows):
        raise MigrationError("issue and comment payload counts do not match the local snapshot")
    if len({issue["localId"] for issue in issues}) != len(issues):
        raise MigrationError("duplicate issue payload identity")
    if len({comment["localId"] for comment in comments}) != len(comments):
        raise MigrationError("duplicate comment payload identity")
    unresolved_refs = sorted(
        {json.dumps(reference, ensure_ascii=False, sort_keys=True): reference for reference in unresolved_refs}.values(),
        key=lambda reference: json.dumps(reference, ensure_ascii=False, sort_keys=True),
    )
    return {
        "issues": {**payload_header(input_snapshot, input_manifest_path), "items": issues},
        "resolutionComments": {**payload_header(input_snapshot, input_manifest_path), "items": comments},
        "edges": {
            **payload_header(input_snapshot, input_manifest_path),
            "items": sorted(edges, key=lambda edge: (edge["kind"], edge["childLocalId"], edge["parentLocalId"])),
        },
        "unresolvedReferences": {**payload_header(input_snapshot, input_manifest_path), "items": unresolved_refs},
    }


def edge_payload(
    kind: str,
    child_local_id: str,
    parent_local_id: str,
    identities: dict[str, dict[str, Any]],
    mapping: dict[str, Any],
) -> dict[str, Any]:
    child = identities[child_local_id]
    parent = identities[parent_local_id]
    child_verified = child["hostedStatus"] == "verified"
    parent_verified = parent["hostedStatus"] == "verified"
    publishable = child_verified and parent_verified
    return {
        "kind": kind,
        "childLocalId": child_local_id,
        "parentLocalId": parent_local_id,
        "childHostedIssueNumber": child.get("hostedIssueNumber"),
        "parentHostedIssueNumber": parent.get("hostedIssueNumber"),
        "childHostedUrl": child.get("hostedUrl"),
        "parentHostedUrl": parent.get("hostedUrl"),
        "publishable": publishable,
        "status": "ready-to-publish" if publishable else "pending-hosted-identities",
        "relationshipStatus": "unverified",
        "relationshipReceipt": None,
        "pendingLocalIds": sorted(
            local_id
            for local_id, identity in ((child_local_id, child), (parent_local_id, parent))
            if identity["hostedStatus"] != "verified"
        ),
    }


def expected_files(
    root: Path,
    mapping: dict[str, Any],
    input_snapshot: dict[str, Any],
    payloads: dict[str, Any],
    *,
    output_directory: Path | None = None,
    mapping_output_path: Path | None = None,
    include_mapping_output: bool = True,
    output_snapshot_id: str | None = None,
) -> dict[Path, str]:
    output = output_directory or root / "docs" / "wayfinder" / "migration"
    input_manifest_path = output / "input-manifest.json"
    manifest_path = output / "manifest.json"
    issues_path = output / "issues.json"
    comments_path = output / "resolution-comments.json"
    edges_path = output / "edges.json"
    unresolved_path = output / "unresolved-references.json"
    if mapping_output_path is None and include_mapping_output:
        mapping_output_path = root / "docs" / "wayfinder" / "hosted-identities.json"
    mapping_path_text = (
        str(mapping_output_path.relative_to(root))
        if mapping_output_path is not None
        else "docs/wayfinder/hosted-identities.json"
    )
    input_manifest_path_text = str(input_manifest_path.relative_to(root))
    identity_text = canonical_json(mapping)
    issues_text = canonical_json(payloads["issues"])
    comments_text = canonical_json(payloads["resolutionComments"])
    edges_text = canonical_json(payloads["edges"])
    unresolved_text = canonical_json(payloads["unresolvedReferences"])
    input_manifest = {
        **input_snapshot["record"],
        "canonicalContentSha256": input_snapshot["sha256"],
    }
    manifest = {
        "schemaVersion": SCHEMA_VERSION,
        "repository": REPOSITORY,
        "historicalLinkRevision": dict(HISTORICAL_LINK_REVISION),
        "mappingPath": mapping_path_text,
        "inputSnapshot": {
            "path": input_manifest_path_text,
            "sha256": input_snapshot["sha256"],
        },
        "payloads": {
            "issues": {"path": str(issues_path.relative_to(root)), "sha256": sha256_text(issues_text)},
            "resolutionComments": {
                "path": str(comments_path.relative_to(root)),
                "sha256": sha256_text(comments_text),
            },
            "edges": {"path": str(edges_path.relative_to(root)), "sha256": sha256_text(edges_text)},
            "unresolvedReferences": {
                "path": str(unresolved_path.relative_to(root)),
                "sha256": sha256_text(unresolved_text),
            },
        },
        "counts": {
            "localIssues": len(payloads["issues"]["items"]),
            "closedIssues": sum(item["desiredState"] == "closed" for item in payloads["issues"]["items"]),
            "resolutionComments": sum(len(item["comments"]) for item in payloads["resolutionComments"]["items"]),
            "graphEdges": len(payloads["edges"]["items"]),
            "publishableIssues": sum(item["publishable"] for item in payloads["issues"]["items"]),
            "pendingEdges": sum(not item["publishable"] for item in payloads["edges"]["items"]),
            "unresolvedReferences": len(payloads["unresolvedReferences"]["items"]),
        },
    }
    if output_snapshot_id is not None:
        manifest["outputSnapshotId"] = output_snapshot_id
    files = {
        input_manifest_path: canonical_json(input_manifest),
        issues_path: issues_text,
        comments_path: comments_text,
        edges_path: edges_text,
        unresolved_path: unresolved_text,
        manifest_path: canonical_json(manifest),
    }
    if mapping_output_path is not None:
        files[mapping_output_path] = identity_text
    return files


def validate_payloads(
    root: Path,
    rows: list[dict[str, Any]],
    mapping: dict[str, Any],
    input_snapshot: dict[str, Any],
    payloads: dict[str, Any],
) -> None:
    if mapping["localIssueCount"] != len(rows):
        raise MigrationError("identity mapping localIssueCount is stale")
    identity_by_local = identity_index(mapping)
    if set(identity_by_local) != {row["metadata"]["id"] for row in rows}:
        raise MigrationError("identity mapping does not cover exactly the local snapshot")
    for payload_name in ("issues", "resolutionComments", "edges", "unresolvedReferences"):
        payload = payloads[payload_name]
        if payload.get("schemaVersion") != SCHEMA_VERSION:
            raise MigrationError(f"{payload_name}: unsupported payload schema")
        if payload.get("repository") != REPOSITORY:
            raise MigrationError(f"{payload_name}: repository differs from the accepted snapshot")
        if payload.get("historicalLinkRevision") != HISTORICAL_LINK_REVISION:
            raise MigrationError(f"{payload_name}: historical link revision differs from the accepted snapshot")
        if payload.get("inputSnapshot", {}).get("sha256") != input_snapshot["sha256"]:
            raise MigrationError(f"{payload_name}: input snapshot hash differs from the source inputs")
    issues = payloads["issues"]["items"]
    comments = payloads["resolutionComments"]["items"]
    edges = payloads["edges"]["items"]
    if len(issues) != len(rows) or len(comments) != len(rows):
        raise MigrationError("payload issue/comment set does not cover all local IDs")
    if {item["localId"] for item in issues} != set(identity_by_local):
        raise MigrationError("issue payload identities differ from the local set")
    by_local = {item["localId"]: item for item in issues}
    comment_by_local = {item["localId"]: item for item in comments}
    if any(item["localId"] not in comment_by_local for item in issues):
        raise MigrationError("missing resolution-comment payload")
    for row in rows:
        metadata = row["metadata"]
        payload = by_local[metadata["id"]]
        if payload["desiredState"] != metadata["status"] or payload["parentLocalId"] != metadata["parent"]:
            raise MigrationError(f"{metadata['id']}: status/parent changed during migration")
        if payload["blockedByLocalIds"] != metadata["blocked_by"]:
            raise MigrationError(f"{metadata['id']}: blocker list changed during migration")
        if metadata["status"] == "closed" and not comment_by_local[metadata["id"]]["comments"]:
            raise MigrationError(f"{metadata['id']}: closed issue lost its resolution record")
        if metadata["status"] == "open" and comment_by_local[metadata["id"]]["comments"]:
            raise MigrationError(f"{metadata['id']}: open issue unexpectedly gained resolution comments")
    local_ids = set(identity_by_local)
    for edge in edges:
        if edge["childLocalId"] not in local_ids or edge["parentLocalId"] not in local_ids:
            raise MigrationError(f"dangling local graph edge: {edge}")
        if edge["publishable"] and (edge["childHostedUrl"] is None or edge["parentHostedUrl"] is None):
            raise MigrationError(f"publishable edge has no hosted endpoints: {edge}")
        if not edge["publishable"] and not edge["pendingLocalIds"]:
            raise MigrationError(f"pending edge has no pending identity: {edge}")
        if edge["status"] not in {"ready-to-publish", "pending-hosted-identities"}:
            raise MigrationError(f"edge status is not endpoint readiness: {edge}")
        if edge.get("relationshipStatus") != "unverified" or edge.get("relationshipReceipt") is not None:
            raise MigrationError(f"edge claims relationship evidence without a retained receipt: {edge}")
    if any("](" + "docs/" in item["body"] for item in issues):
        raise MigrationError("issue payload contains an unreconciled repository-relative link")
    unresolved_items = payloads["unresolvedReferences"]["items"]
    unresolved_by_local = defaultdict(list)
    for reference in unresolved_items:
        unresolved_by_local[reference["sourceLocalId"]].append(reference)

    def validate_links(local_id: str, markdown: str, declared: list[dict[str, Any]]) -> None:
        declared_keys = {json.dumps(reference, ensure_ascii=False, sort_keys=True) for reference in declared}
        available_keys = {
            json.dumps(
                {key: value for key, value in reference.items() if key != "sourceLocalId"},
                ensure_ascii=False,
                sort_keys=True,
            )
            for reference in unresolved_by_local[local_id]
        }
        for match in LINK_PATTERN.finditer(markdown):
            target = match.group("target")
            if target.startswith(f"{HOSTED_ROOT}/blob/") or target.startswith(f"{HOSTED_ROOT}/tree/"):
                path_text, _ = urldefrag(target)
                kind, revision, relative = path_text[len(f"{HOSTED_ROOT}/") :].split("/", 2)
                if revision != HISTORICAL_LINK_COMMIT:
                    raise MigrationError(f"{local_id}: repository link is bound to an unaccepted revision: {target}")
                if git_object_type(root, revision, relative) != kind:
                    raise MigrationError(f"{local_id}: repository link target is absent or has the wrong type: {target}")
            elif target.startswith("local-reference:") or target.startswith(LOCAL_ISSUE_MARKER):
                kind = "issue" if target.startswith(LOCAL_ISSUE_MARKER) else "path"
                if kind == "issue":
                    local_reference = target[len(LOCAL_ISSUE_MARKER) :].split("#", 1)[0]
                    expected = {"kind": "issue", "localId": local_reference}
                else:
                    expected = {"kind": "path", "path": target[len("local-reference:") :]}
                if not any(
                    all(reference.get(key) == value for key, value in expected.items())
                    for reference in unresolved_by_local[local_id]
                ):
                    raise MigrationError(f"{local_id}: unresolved link has no retained source record: {target}")
        if not declared_keys.issubset(available_keys):
            raise MigrationError(f"{local_id}: declared unresolved reference is absent from the global record")

    for item in issues:
        validate_links(item["localId"], item["body"], item["unresolvedReferences"])
    for item in payloads["resolutionComments"]["items"]:
        for comment in item["comments"]:
            validate_links(item["localId"], comment["body"], comment["unresolvedReferences"])
    expected_comments = sum(len(item["comments"]) for item in comments)
    if expected_comments < sum(item["desiredState"] == "closed" for item in issues):
        raise MigrationError("not every closed issue has a separate resolution comment")


def run(
    root: Path,
    mapping_path: Path | None,
    write: bool,
    check: bool,
    input_snapshot_id: str | None = None,
    output_snapshot_id: str | None = None,
    current_identity_intake_id: str | None = None,
) -> int:
    if input_snapshot_id is not None and output_snapshot_id is not None:
        raise MigrationError("historical input and current output snapshots cannot be selected together")
    if input_snapshot_id is not None and current_identity_intake_id is not None:
        raise MigrationError("historical migration and current identity inputs cannot be selected together")
    if current_identity_intake_id is not None and mapping_path is None:
        raise MigrationError("--current-identity-intake requires an explicit --mapping")
    if current_identity_intake_id is not None and output_snapshot_id is None:
        raise MigrationError("--current-identity-intake requires a distinct --output-snapshot")
    if current_identity_intake_id is not None:
        expected_mapping_path = (root / CURRENT_IDENTITY_MAPPING_PATH).resolve()
        if mapping_path is None or mapping_path.resolve() != expected_mapping_path:
            raise MigrationError(
                f"--current-identity-intake requires --mapping {CURRENT_IDENTITY_MAPPING_PATH}"
            )
    elif mapping_path is not None:
        for guarded_mapping_path in (
            CURRENT_IDENTITY_MAPPING_PATH,
            PRESERVED_U2_CURRENT_IDENTITY_MAPPING_PATH,
        ):
            if mapping_path.resolve() == (root / guarded_mapping_path).resolve():
                raise MigrationError(
                    f"--mapping {guarded_mapping_path} requires --current-identity-intake"
                )
    if write and output_snapshot_id is None:
        raise MigrationError("--write requires a distinct --output-snapshot identity")
    if check and input_snapshot_id is None and output_snapshot_id is None:
        raise MigrationError("--check requires --input-snapshot or --output-snapshot")
    if output_snapshot_id is not None:
        valid_output_id = re.fullmatch(r"[a-z0-9][a-z0-9._-]{0,63}", output_snapshot_id)
        if output_snapshot_id == HISTORICAL_INPUT_SNAPSHOT_ID or not valid_output_id:
            raise MigrationError(f"invalid or reserved output snapshot identity: {output_snapshot_id}")
        snapshot_root = root / "docs" / "wayfinder" / "migration" / "snapshots" / output_snapshot_id
        if (snapshot_root / "snapshot.json").exists():
            raise MigrationError(f"output snapshot identity is already used by an input snapshot: {output_snapshot_id}")
    inputs = select_migration_inputs(root, input_snapshot_id, mapping_path)
    if inputs.snapshot_id != "live" and not check:
        raise MigrationError("historical input snapshots are read-only and support --check only")
    current_identity_intake = (
        select_current_identity_intake(root, current_identity_intake_id)
        if current_identity_intake_id is not None
        else None
    )
    rows = issue_snapshot(root, inputs)
    selected_mapping_path = mapping_path or root / "docs" / "wayfinder" / "hosted-identities.json"
    mapping = load_mapping(
        selected_mapping_path,
        rows,
        mapping_bytes=inputs.mapping_bytes,
        current_identity_intake=current_identity_intake,
    )
    input_snapshot = canonical_input_manifest(
        root,
        rows,
        mapping,
        preparer_bytes=inputs.preparer_bytes,
        mapping_path=(str(selected_mapping_path.relative_to(root)) if current_identity_intake is not None else None),
        mapping_bytes=(inputs.mapping_bytes if current_identity_intake is not None else None),
        current_identity_intake=current_identity_intake,
    )
    if inputs.expected_canonical_sha256 is not None and input_snapshot["sha256"] != inputs.expected_canonical_sha256:
        raise MigrationError("selected historical input canonical binding changed")
    if inputs.snapshot_id == "live" and output_snapshot_id is None and (write or check):
        raise MigrationError("current migration artifacts require a distinct --output-snapshot identity")
    if inputs.snapshot_id != "live":
        output_directory = root / "docs" / "wayfinder" / "migration"
    elif output_snapshot_id is not None:
        output_directory = root / "docs" / "wayfinder" / "migration" / "snapshots" / output_snapshot_id / "outputs"
    else:
        output_directory = root / "docs" / "wayfinder" / "migration"
    output_manifest_path = output_directory / "input-manifest.json"
    output_mapping_path = output_directory / "hosted-identities.json" if inputs.snapshot_id == "live" and output_snapshot_id is not None else None
    payloads = build_payloads(
        root,
        rows,
        mapping,
        input_snapshot,
        input_manifest_path=str(output_manifest_path.relative_to(root)),
    )
    validate_payloads(root, rows, mapping, input_snapshot, payloads)
    files = expected_files(
        root,
        mapping,
        input_snapshot,
        payloads,
        output_directory=output_directory,
        mapping_output_path=output_mapping_path,
        include_mapping_output=inputs.snapshot_id == "live" and output_snapshot_id is None,
        output_snapshot_id=output_snapshot_id,
    )
    stale: list[str] = []
    for path, expected in files.items():
        if not path.exists() or path.read_text(encoding="utf-8") != expected:
            stale.append(str(path.relative_to(root)))
    if check:
        if stale:
            print("stale migration artifacts:")
            for path in stale:
                print(f"- {path}")
            return 1
        print(
            f"wayfinder migration: {len(rows)} local issues, "
            f"{sum(item['desiredState'] == 'closed' for item in payloads['issues']['items'])} closed, "
            f"{sum(len(item['comments']) for item in payloads['resolutionComments']['items'])} resolution comments, "
            f"{len(payloads['edges']['items'])} graph edges"
        )
        return 0
    if not write:
        print(canonical_json(payloads["issues"]), end="")
        return 0
    if output_directory.exists():
        expected_paths = {path.relative_to(output_directory) for path in files if path.is_relative_to(output_directory)}
        existing_paths = {path.relative_to(output_directory) for path in output_directory.rglob("*") if path.is_file()}
        if existing_paths != expected_paths or stale:
            raise MigrationError(f"output snapshot identity already exists with different bytes: {output_snapshot_id}")
        print(f"current migration snapshot already matches immutable output identity {output_snapshot_id}")
        return 0
    for path, expected in files.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(expected, encoding="utf-8")
    print(
        f"wrote {len(files)} read-only migration artifacts for {len(rows)} local issues "
        f"({sum(item['desiredState'] == 'closed' for item in payloads['issues']['items'])} closed)"
    )
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--mapping", type=Path, default=None)
    parser.add_argument(
        "--input-snapshot",
        choices=(HISTORICAL_INPUT_SNAPSHOT_ID,),
        default=None,
        help="check one pinned historical input bundle instead of the current checkout",
    )
    parser.add_argument(
        "--current-identity-intake",
        choices=(CURRENT_IDENTITY_INTAKE_SNAPSHOT_ID,),
        default=None,
        help="bind the current mapping to one retained raw hosted issue-list capture",
    )
    parser.add_argument(
        "--output-snapshot",
        default=None,
        help="immutable identity/path for a new current-state migration output",
    )
    parser.add_argument("--write", action="store_true", help="write deterministic mapping and payload artifacts")
    parser.add_argument("--check", action="store_true", help="check deterministic artifacts without writing")
    args = parser.parse_args(argv)
    if args.write and args.check:
        parser.error("--write and --check are mutually exclusive")
    if args.input_snapshot is not None and args.mapping is not None:
        parser.error("--mapping cannot override an explicitly selected input snapshot")
    if args.current_identity_intake is not None and args.input_snapshot is not None:
        parser.error("--current-identity-intake cannot be combined with --input-snapshot")
    if args.current_identity_intake is not None and args.mapping is None:
        parser.error("--current-identity-intake requires --mapping")
    if args.current_identity_intake is not None and args.output_snapshot is None:
        parser.error("--current-identity-intake requires --output-snapshot")
    if args.input_snapshot is not None and not args.check:
        parser.error("--input-snapshot is read-only and requires --check")
    if args.input_snapshot is not None and args.output_snapshot is not None:
        parser.error("--input-snapshot and --output-snapshot are mutually exclusive")
    if args.check and args.input_snapshot is None and args.output_snapshot is None:
        parser.error("--check requires --input-snapshot or --output-snapshot")
    if args.write and args.output_snapshot is None:
        parser.error("--write requires a distinct --output-snapshot identity")
    root = args.root.resolve()
    mapping_path = args.mapping.resolve() if args.mapping is not None else None
    try:
        return run(
            root,
            mapping_path,
            args.write,
            args.check,
            args.input_snapshot,
            args.output_snapshot,
            args.current_identity_intake,
        )
    except MigrationError as error:
        print(f"wayfinder migration: error: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
