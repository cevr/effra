#!/usr/bin/env python3
"""Causal controls for the durable hosted run-binding reconciliation."""

from __future__ import annotations

import copy
import hashlib
import json
import os
import shutil
import subprocess
import tempfile
from pathlib import Path
from typing import Any, Callable

import wayfinder_migration as migration


ROOT = Path(__file__).resolve().parents[1]
ARTIFACT_PATH = ROOT / "docs" / "wayfinder" / "hosted-run-reconciliation.json"
ACCEPTED_COMMIT = "4627f414414cc964840b53894b7161482687de69"
ACCEPTED_TREE = "ff2e8954a6063772b5fcd7dee9abc9929b58613c"
HISTORICAL_INPUT_SNAPSHOT_ID = migration.HISTORICAL_INPUT_SNAPSHOT_ID


def require(condition: bool, message: str) -> None:
    if not condition:
        raise AssertionError(message)


def sha256_bytes(value: bytes) -> str:
    return hashlib.sha256(value).hexdigest()


def read_verified_json(path: Path, expected_sha256: str) -> tuple[Any, str]:
    require(path.is_file(), f"missing durable evidence file: {path}")
    raw = path.read_bytes()
    actual_sha256 = sha256_bytes(raw)
    require(actual_sha256 == expected_sha256, f"evidence bytes changed: {path}")
    try:
        return json.loads(raw.decode("utf-8")), actual_sha256
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise AssertionError(f"evidence is not valid JSON: {path}") from error


def validate_native_receipt(receipt: Any, expected_endpoint: str) -> None:
    require(isinstance(receipt, dict), "native receipt must be an object")
    require(receipt.get("method") == "GET", "native receipt method changed")
    require(receipt.get("endpoint") == expected_endpoint, "native receipt endpoint changed")
    require(receipt.get("exit") == 0, "native GET did not exit successfully")
    require(receipt.get("sourceCommit") == ACCEPTED_COMMIT, "native receipt source commit changed")
    require(receipt.get("sourceTree") == ACCEPTED_TREE, "native receipt source tree changed")


def validate_hosted_evidence(
    artifact: dict[str, Any],
    issue_response: Any,
    issue_response_sha256: str,
    issue_receipt: Any,
    parent_response: Any,
    parent_response_sha256: str,
    parent_receipt: Any,
) -> None:
    hosted_issue = artifact["hosted"]["issue"]
    hosted_parent = artifact["hosted"]["parent"]
    validate_native_receipt(issue_receipt, "repos/cevr/effra/issues/73")
    validate_native_receipt(parent_receipt, "repos/cevr/effra/issues/1/sub_issues?per_page=100")
    require(
        issue_response_sha256 == issue_receipt["responseSha256"],
        "issue response bytes do not match native GET receipt",
    )
    require(
        parent_response_sha256 == parent_receipt["responseSha256"],
        "parent response bytes do not match native GET receipt",
    )
    require(issue_receipt["responseSha256"] == hosted_issue["nativeGET"]["responseSha256"], "issue response binding changed")
    require(parent_receipt["responseSha256"] == hosted_parent["nativeGET"]["responseSha256"], "parent response binding changed")
    require(isinstance(issue_response, dict), "issue GET response must be an object")
    require(issue_response.get("number") == 73, "issue GET number changed")
    require(issue_response.get("id") == 5767369413, "issue GET ID changed")
    require(issue_response.get("html_url") == hosted_issue["url"], "issue GET URL changed")
    require(issue_response.get("title") == hosted_issue["title"], "issue GET title changed")
    require(issue_response.get("state") == hosted_issue["state"], "issue GET state changed")
    require(isinstance(issue_response.get("body"), str), "issue GET body missing")
    require(sha256_bytes(issue_response["body"].encode("utf-8")) == hosted_issue["bodySha256"], "issue body binding changed")
    require(isinstance(parent_response, list), "parent GET response must be an array")
    members = [item for item in parent_response if isinstance(item, dict) and item.get("number") == 73]
    require(len(members) == 1, "parent GET must contain exactly one issue-73 member")
    member = members[0]
    require(member.get("id") == hosted_issue["issueId"], "parent member issue ID changed")
    require(member.get("html_url") == hosted_issue["url"], "parent member issue URL changed")
    require(member.get("parent_issue_url") == hosted_parent["apiUrl"], "parent inverse URL changed")
    require(member.get("number") == hosted_issue["number"], "parent member issue number changed")


def validate_original_verifier(artifact: dict[str, Any], verifier: Any) -> None:
    original = artifact["scope"]["originalWave"]
    require(isinstance(verifier, dict), "original verifier must be an object")
    require(verifier.get("sourceCommit") == ACCEPTED_COMMIT, "original verifier source commit changed")
    require(verifier.get("sourceTree") == ACCEPTED_TREE, "original verifier source tree changed")
    require(len(verifier.get("actualHostedIdentities", {})) == original["canonicalIssues"], "original identity count changed")
    require(verifier.get("nativeParentRelationships") == 70, "original parent edge count changed")
    require(verifier.get("nativeBlockerRelationships") == 113, "original blocker edge count changed")
    require(verifier.get("resolutionComments") == original["resolutionComments"], "original comment count changed")
    require(verifier.get("sourceClosedFiniteHistories") == original["finiteHistoricalClosures"], "original closure count changed")
    require(verifier.get("administrativeDuplicateClosedSeparately") == original["administrativeDuplicate"]["issueNumber"], "duplicate closure binding changed")
    require(verifier.get("pendingPublicSourcePathReferences") == 12, "pending source references changed")
    require(verifier.get("sourcePayloadRelationshipStatus") == "unverified; hosted verification evidence is separate", "original relationship boundary changed")
    require(verifier.get("benchmarks") == "not run", "original benchmark boundary changed")
    require(verifier.get("fullImplementationGoal") == "active", "original goal status changed")


def expect_failure(label: str, operation: Callable[[], None]) -> None:
    try:
        operation()
    except (AssertionError, KeyError, TypeError, ValueError, json.JSONDecodeError):
        return
    raise AssertionError(f"negative control unexpectedly passed: {label}")


def write_scratch_git_pointer(source_root: Path, scratch: Path) -> None:
    result = subprocess.run(
        ["git", "-C", str(source_root), "rev-parse", "--absolute-git-dir"],
        check=False,
        capture_output=True,
        text=True,
    )
    require(result.returncode == 0, f"source Git metadata unavailable: {result.stderr.strip()}")
    git_dir = Path(result.stdout.strip())
    require(git_dir.is_absolute() and git_dir.exists(), f"source Git directory unavailable: {git_dir}")
    (scratch / ".git").write_text(f"gitdir: {git_dir}\n", encoding="utf-8")


def expect_rehashed_response_rejection(side: str) -> None:
    response_relative = (
        "docs/wayfinder/receipts/issue-73-get.response.json"
        if side == "issue"
        else "docs/wayfinder/receipts/map-sub-issues-get.response.json"
    )
    label = f"rehashed {side} response with unchanged native receipt"
    with tempfile.TemporaryDirectory(prefix="wayfinder-hosted-reconciliation-") as temporary:
        scratch = Path(temporary)
        shutil.copytree(ROOT / "docs" / "wayfinder", scratch / "docs" / "wayfinder")
        (scratch / "scripts").mkdir()
        shutil.copy2(ROOT / "scripts" / "wayfinder_migration.py", scratch / "scripts" / "wayfinder_migration.py")
        write_scratch_git_pointer(ROOT, scratch)
        response_path = scratch / response_relative
        response = json.loads(response_path.read_text(encoding="utf-8"))
        if side == "issue":
            response["updated_at"] = "2099-01-01T00:00:00Z"
        else:
            member = next(item for item in response if item.get("number") == 73)
            member["updated_at"] = "2099-01-01T00:00:00Z"
        response_bytes = (json.dumps(response, ensure_ascii=False) + "\n").encode("utf-8")
        response_path.write_bytes(response_bytes)

        artifact_path = scratch / "docs" / "wayfinder" / "hosted-run-reconciliation.json"
        artifact = json.loads(artifact_path.read_text(encoding="utf-8"))
        evidence = next(
            item
            for item in artifact["localSource"]["portableEvidence"]["files"]
            if item["path"] == response_relative
        )
        evidence["sha256"] = sha256_bytes(response_bytes)
        artifact_path.write_text(json.dumps(artifact, indent=2) + "\n", encoding="utf-8")

        original_root = ROOT
        original_artifact_path = ARTIFACT_PATH
        globals()["ROOT"] = scratch
        globals()["ARTIFACT_PATH"] = artifact_path
        try:
            try:
                check(run_rehashed_controls=False, run_ordinary_clone=False)
            except AssertionError as error:
                require(
                    "bytes do not match native GET receipt" in str(error),
                    f"{label} rejected for unrelated reason: {error}",
                )
            else:
                raise AssertionError(f"negative control unexpectedly passed: {label}")
        finally:
            globals()["ROOT"] = original_root
            globals()["ARTIFACT_PATH"] = original_artifact_path


def expect_historical_snapshot_rejection(mode: str) -> None:
    require(mode in {"missing", "tampered"}, f"unknown historical snapshot control: {mode}")
    with tempfile.TemporaryDirectory(prefix="wayfinder-hosted-historical-input-") as temporary:
        scratch = Path(temporary)
        shutil.copytree(ROOT / "docs" / "wayfinder", scratch / "docs" / "wayfinder")
        (scratch / "scripts").mkdir()
        shutil.copy2(ROOT / "scripts" / "wayfinder_migration.py", scratch / "scripts" / "wayfinder_migration.py")
        shutil.copy2(ROOT / "scripts" / "test_wayfinder_hosted_reconciliation.py", scratch / "scripts" / "test_wayfinder_hosted_reconciliation.py")
        write_scratch_git_pointer(ROOT, scratch)
        descriptor_path = (
            scratch
            / "docs"
            / "wayfinder"
            / "migration"
            / "snapshots"
            / HISTORICAL_INPUT_SNAPSHOT_ID
            / "snapshot.json"
        )
        descriptor = json.loads(descriptor_path.read_text(encoding="utf-8"))
        bundle_path = scratch / descriptor["bundle"]["path"]
        if mode == "missing":
            bundle_path.unlink()
            expected_reason = "missing historical input bundle"
        else:
            bundle_path.write_bytes(bundle_path.read_bytes() + b"tampered")
            expected_reason = "historical input bundle bytes changed"

        original_root = ROOT
        original_artifact_path = ARTIFACT_PATH
        globals()["ROOT"] = scratch
        globals()["ARTIFACT_PATH"] = scratch / "docs" / "wayfinder" / "hosted-run-reconciliation.json"
        try:
            try:
                check(run_rehashed_controls=False, run_ordinary_clone=False)
            except migration.MigrationError as error:
                require(expected_reason in str(error), f"{mode} historical snapshot rejected for unrelated reason: {error}")
            else:
                raise AssertionError(f"negative control unexpectedly passed: {mode} historical snapshot")
        finally:
            globals()["ROOT"] = original_root
            globals()["ARTIFACT_PATH"] = original_artifact_path


def expect_historical_research_selection_controls() -> None:
    with tempfile.TemporaryDirectory(prefix="wayfinder-hosted-research-drift-") as temporary:
        scratch = Path(temporary)
        shutil.copytree(ROOT / "docs" / "wayfinder", scratch / "docs" / "wayfinder")
        (scratch / "scripts").mkdir()
        shutil.copy2(ROOT / "scripts" / "wayfinder_migration.py", scratch / "scripts" / "wayfinder_migration.py")
        shutil.copy2(ROOT / "scripts" / "test_wayfinder_hosted_reconciliation.py", scratch / "scripts" / "test_wayfinder_hosted_reconciliation.py")
        write_scratch_git_pointer(ROOT, scratch)
        inputs = migration.select_migration_inputs(ROOT, HISTORICAL_INPUT_SNAPSHOT_ID)
        require(inputs.research_record_path is not None, "historical research record path is missing")
        require(inputs.research_record_bytes is not None, "historical research record bytes are missing")
        research_path = scratch / inputs.research_record_path
        research_path.parent.mkdir(parents=True, exist_ok=True)
        research_path.write_bytes(inputs.research_record_bytes + b"\ncurrent-only research drift\n")

        original_root = ROOT
        original_artifact_path = ARTIFACT_PATH
        globals()["ROOT"] = scratch
        globals()["ARTIFACT_PATH"] = scratch / "docs" / "wayfinder" / "hosted-run-reconciliation.json"
        try:
            check(run_rehashed_controls=False, run_ordinary_clone=False)
            research_path.unlink()
            shutil.rmtree(research_path.parent)
            require(not research_path.parent.exists(), "live research directory unexpectedly remains")
            check(run_rehashed_controls=False, run_ordinary_clone=False)
        finally:
            globals()["ROOT"] = original_root
            globals()["ARTIFACT_PATH"] = original_artifact_path


def git_output(root: Path, *arguments: str, input_bytes: bytes | None = None) -> bytes:
    result = subprocess.run(
        ["git", "-C", str(root), *arguments],
        check=False,
        capture_output=True,
        input=input_bytes,
    )
    require(result.returncode == 0, f"git {' '.join(arguments)} failed ({result.returncode}): {result.stderr.decode(errors='replace').strip()}")
    return result.stdout


def expect_ordinary_clone_check() -> None:
    with tempfile.TemporaryDirectory(prefix="wayfinder-hosted-reconciliation-clone-") as temporary:
        clone = Path(temporary) / "ordinary-clone"
        unstaged = subprocess.run(["git", "-C", str(ROOT), "diff", "--quiet"], check=False, capture_output=True)
        require(unstaged.returncode == 0, "ordinary clone source has unstaged tracked changes")
        untracked = git_output(ROOT, "ls-files", "--others", "--exclude-standard").decode().strip()
        require(not untracked, f"ordinary clone source has unstaged untracked inputs: {untracked}")
        parent = git_output(ROOT, "rev-parse", "HEAD").decode().strip()
        staged_tree = git_output(ROOT, "write-tree").decode().strip()
        staged_patch = git_output(ROOT, "diff", "--cached", "--binary", "--full-index", "HEAD")
        result = subprocess.run(
            ["git", "clone", "--no-local", "--no-checkout", str(ROOT), str(clone)],
            check=False,
            capture_output=True,
            text=True,
        )
        require(result.returncode == 0, f"ordinary clone failed: {result.stderr.strip()}")
        require((clone / ".git").is_dir(), "ordinary clone does not have a real .git directory")
        require(not (clone / ".git" / "objects" / "info" / "alternates").exists(), "ordinary clone depends on an alternate object store")
        git_output(clone, "-c", "core.hooksPath=/dev/null", "checkout", "--detach", parent)
        if staged_patch:
            git_output(clone, "apply", "--index", "--binary", "-", input_bytes=staged_patch)
        clone_tree = git_output(clone, "write-tree").decode().strip()
        require(clone_tree == staged_tree, f"ordinary clone tree differs from staged subject: {clone_tree} != {staged_tree}")
        environment = os.environ.copy()
        environment["EFFRA_SKIP_ORDINARY_CLONE"] = "1"
        result = subprocess.run(
            ["python3", "-B", "scripts/test_wayfinder_hosted_reconciliation.py"],
            cwd=clone,
            env=environment,
            check=False,
            capture_output=True,
            text=True,
        )
        require(
            result.returncode == 0,
            f"ordinary clone checker failed ({result.returncode}): {result.stderr.strip()}",
        )
        print(
            "ordinary clone: "
            f"parent={parent} stagedTree={staged_tree} patchSha256={sha256_bytes(staged_patch)} "
            "gitDirectory=present alternates=absent checker=passed"
        )


def check(*, run_rehashed_controls: bool = True, run_ordinary_clone: bool = True) -> None:
    artifact = json.loads(ARTIFACT_PATH.read_text(encoding="utf-8"))
    require(artifact["sourceSubject"]["commit"] == ACCEPTED_COMMIT, "extension base commit changed")
    require(artifact["sourceSubject"]["tree"] == ACCEPTED_TREE, "extension base tree changed")
    require(artifact["sourceSubject"]["role"].startswith("accepted preparation/source base"), "extension base role missing")
    require(artifact["sourceSubject"]["containsExtensionBytes"] is False, "base incorrectly contains extension bytes")

    inputs = migration.select_migration_inputs(ROOT, HISTORICAL_INPUT_SNAPSHOT_ID)
    rows = migration.issue_snapshot(ROOT, inputs)
    mapping = migration.load_mapping(
        ROOT / "docs" / "wayfinder" / "hosted-identities.json",
        rows,
        mapping_bytes=inputs.mapping_bytes,
    )
    input_snapshot = migration.canonical_input_manifest(
        ROOT,
        rows,
        mapping,
        preparer_bytes=inputs.preparer_bytes,
    )
    require(input_snapshot["sha256"] == inputs.expected_canonical_sha256, "historical input manifest canonical binding changed")
    payloads = migration.build_payloads(ROOT, rows, mapping, input_snapshot)
    migration.validate_payloads(ROOT, rows, mapping, input_snapshot, payloads)

    require(len(rows) == 72, "local issue count changed")
    require(len(payloads["edges"]["items"]) == 184, "native edge count changed")
    run_row = next(row for row in rows if row["metadata"]["id"] == "run-binding")
    body, comments = migration.split_issue_content(run_row)
    require(comments == [], "open research issue unexpectedly has resolution comments")
    hosted_body = "<!-- effra-wayfinder-id: run-binding -->\n\n" + body
    require(sha256_bytes(hosted_body.encode("utf-8")) == artifact["hosted"]["issue"]["bodySha256"], "local body hash changed")
    require(run_row["sourceSha256"] == artifact["localSource"]["issueMirror"]["sha256"], "local mirror source bytes changed")

    identity = migration.identity_index(mapping)["run-binding"]
    hosted_issue = artifact["hosted"]["issue"]
    require(identity["hostedIssueNumber"] == hosted_issue["number"] == 73, "mapped issue number changed")
    require(identity["hostedIssueId"] == hosted_issue["issueId"] == 5767369413, "mapped issue ID changed")
    require(identity["hostedBodySha256"] == hosted_issue["bodySha256"], "mapped body hash changed")

    edge = next(
        item
        for item in payloads["edges"]["items"]
        if item["parentLocalId"] == "map" and item["childLocalId"] == "run-binding" and item["kind"] == "parent"
    )
    require(edge["relationshipStatus"] == "unverified", "preparer promoted relation status")
    require(edge["relationshipReceipt"] is None, "preparer fabricated relation receipt")
    require(artifact["transport"]["preparerEdge"]["relationshipReceipt"] is None, "artifact promoted relation receipt")
    require(artifact["transport"]["preparerEdge"]["relationshipStatus"] == "unverified", "artifact promoted relation status")

    counts = artifact["scope"]
    original = counts["originalWave"]
    require("portable original-hosted-verification-final.json" in original["stateClosureEdgeAuthority"], "original state authority missing")
    require(original["canonicalIssues"] == 71, "original wave issue count changed")
    require(original["nativeEdges"] == 183, "original wave edge count changed")
    require(original["finiteHistoricalClosures"] == 17, "original wave closure count changed")
    require(original["administrativeDuplicate"]["issueNumber"] == 63, "duplicate issue changed")
    require(not original["administrativeDuplicate"]["countsAsHistoricalCompletion"], "duplicate became completion")
    require(counts["extension"]["canonicalIssuesAfterAddition"] == len(rows), "extension issue count changed")
    require(counts["extension"]["nativeEdgesAfterAddition"] == len(payloads["edges"]["items"]), "extension edge count changed")
    require(counts["pendingPublicSourceReferences"] == 12, "pending source references changed")

    migration_files = artifact["localSource"]["migration"]
    require(inputs.mapping_bytes is not None, "historical mapping bytes are missing")
    require(sha256_bytes(inputs.mapping_bytes) == artifact["localSource"]["mapping"]["sha256"], "historical mapping bytes changed")
    research_record = artifact["localSource"]["researchRecord"]
    require(inputs.research_record_bytes is not None, "historical research record bytes are missing")
    require(inputs.research_record_path == research_record["path"], "historical research record path changed")
    require(sha256_bytes(inputs.research_record_bytes) == research_record["sha256"], "historical research record bytes changed")
    require(sha256_bytes((ROOT / migration_files["inputManifest"]["path"]).read_bytes()) == migration_files["inputManifest"]["fileSha256"], "input manifest bytes changed")
    require(input_snapshot["sha256"] == migration_files["inputManifest"]["canonicalContentSha256"], "input manifest canonical binding changed")
    require(sha256_bytes((ROOT / migration_files["manifest"]["path"]).read_bytes()) == migration_files["manifest"]["fileSha256"], "migration manifest bytes changed")
    for record in migration_files["payloads"].values():
        require(sha256_bytes((ROOT / record["path"]).read_bytes()) == record["sha256"], f"payload bytes changed: {record['path']}")

    portable = artifact["localSource"]["portableEvidence"]
    require(portable["safePublicJson"] is True, "portable evidence safety not recorded")
    evidence = {record["path"]: record for record in portable["files"]}
    issue_response_path = ROOT / "docs/wayfinder/receipts/issue-73-get.response.json"
    issue_receipt_path = ROOT / "docs/wayfinder/receipts/issue-73-get.receipt.json"
    parent_response_path = ROOT / "docs/wayfinder/receipts/map-sub-issues-get.response.json"
    parent_receipt_path = ROOT / "docs/wayfinder/receipts/map-sub-issues-get.receipt.json"
    verifier_path = ROOT / "docs/wayfinder/receipts/original-hosted-verification-final.json"
    issue_response, issue_response_sha256 = read_verified_json(issue_response_path, evidence[str(issue_response_path.relative_to(ROOT))]["sha256"])
    issue_receipt, _ = read_verified_json(issue_receipt_path, evidence[str(issue_receipt_path.relative_to(ROOT))]["sha256"])
    parent_response, parent_response_sha256 = read_verified_json(parent_response_path, evidence[str(parent_response_path.relative_to(ROOT))]["sha256"])
    parent_receipt, _ = read_verified_json(parent_receipt_path, evidence[str(parent_receipt_path.relative_to(ROOT))]["sha256"])
    verifier, _ = read_verified_json(verifier_path, evidence[str(verifier_path.relative_to(ROOT))]["sha256"])
    validate_hosted_evidence(
        artifact,
        issue_response,
        issue_response_sha256,
        issue_receipt,
        parent_response,
        parent_response_sha256,
        parent_receipt,
    )
    validate_original_verifier(artifact, verifier)

    current_intake = migration.select_current_identity_intake(
        ROOT,
        migration.CURRENT_IDENTITY_INTAKE_SNAPSHOT_ID,
    )
    current_rows = migration.issue_snapshot(ROOT)
    current_mapping_path = ROOT / migration.CURRENT_IDENTITY_MAPPING_PATH
    current_mapping_bytes = current_mapping_path.read_bytes()
    current_mapping = migration.load_mapping(
        current_mapping_path,
        current_rows,
        mapping_bytes=current_mapping_bytes,
        current_identity_intake=current_intake,
    )
    current_input_snapshot = migration.canonical_input_manifest(
        ROOT,
        current_rows,
        current_mapping,
        mapping_path=migration.CURRENT_IDENTITY_MAPPING_PATH,
        mapping_bytes=current_mapping_bytes,
        current_identity_intake=current_intake,
    )
    current_mapping_input = current_input_snapshot["record"]["inputs"]["hostedMapping"]
    require(current_mapping_input["path"] == migration.CURRENT_IDENTITY_MAPPING_PATH, "current mapping source path changed")
    require(current_mapping_input["sha256"] == sha256_bytes(current_mapping_bytes), "current mapping source bytes changed")
    current_intake_input = current_input_snapshot["record"]["inputs"]["currentIdentityIntake"]
    require(
        current_intake_input["rawResponse"]["sha256"] == migration.CURRENT_IDENTITY_RAW_RESPONSE_SHA256,
        "current raw identity intake binding changed",
    )
    require(len(current_rows) == 74, "current local issue count changed")
    require(current_mapping["verifiedHostedCount"] == 74, "current captured identity count changed")
    require(
        migration.run(
            ROOT,
            current_mapping_path,
            write=False,
            check=True,
            output_snapshot_id=migration.CURRENT_IDENTITY_OUTPUT_SNAPSHOT_ID,
            current_identity_intake_id=migration.CURRENT_IDENTITY_INTAKE_SNAPSHOT_ID,
        )
        == 0,
        "current identity output snapshot is stale",
    )

    # Each negative mutates evidence while leaving the expected counts and
    # source shape otherwise intact. A copied status/count flag is insufficient
    # proof when the bytes or endpoint relation no longer match.
    expect_failure("missing response", lambda: read_verified_json(issue_response_path.with_name("missing.json"), evidence[str(issue_response_path.relative_to(ROOT))]["sha256"]))
    tampered_response = copy.deepcopy(issue_response)
    tampered_response["body"] += "\nchanged"
    expect_failure(
        "tampered response body",
        lambda: validate_hosted_evidence(
            artifact,
            tampered_response,
            issue_response_sha256,
            issue_receipt,
            parent_response,
            parent_response_sha256,
            parent_receipt,
        ),
    )
    tampered_receipt = copy.deepcopy(issue_receipt)
    tampered_receipt["endpoint"] = "repos/cevr/effra/issues/72"
    expect_failure(
        "tampered receipt endpoint",
        lambda: validate_hosted_evidence(
            artifact,
            issue_response,
            issue_response_sha256,
            tampered_receipt,
            parent_response,
            parent_response_sha256,
            parent_receipt,
        ),
    )
    wrong_member = copy.deepcopy(parent_response)
    next(item for item in wrong_member if isinstance(item, dict) and item.get("number") == 73)["id"] = 5767369412
    expect_failure(
        "wrong parent member ID",
        lambda: validate_hosted_evidence(
            artifact,
            issue_response,
            issue_response_sha256,
            issue_receipt,
            wrong_member,
            parent_response_sha256,
            parent_receipt,
        ),
    )
    wrong_inverse = copy.deepcopy(parent_response)
    next(item for item in wrong_inverse if isinstance(item, dict) and item.get("number") == 73)["parent_issue_url"] = "https://github.com/cevr/effra/issues/2"
    expect_failure(
        "wrong inverse parent URL",
        lambda: validate_hosted_evidence(
            artifact,
            issue_response,
            issue_response_sha256,
            issue_receipt,
            wrong_inverse,
            parent_response_sha256,
            parent_receipt,
        ),
    )
    wrong_closure = copy.deepcopy(verifier)
    wrong_closure["sourceClosedFiniteHistories"] = 16
    expect_failure("changed original closure count", lambda: validate_original_verifier(artifact, wrong_closure))
    wrong_comments = copy.deepcopy(verifier)
    wrong_comments["resolutionComments"] = 23
    expect_failure("changed original comment count", lambda: validate_original_verifier(artifact, wrong_comments))
    wrong_source = copy.deepcopy(issue_receipt)
    wrong_source["sourceCommit"] = "0" * 40
    expect_failure(
        "changed source binding",
        lambda: validate_hosted_evidence(
            artifact,
            issue_response,
            issue_response_sha256,
            wrong_source,
            parent_response,
            parent_response_sha256,
            parent_receipt,
        ),
    )
    if run_rehashed_controls:
        expect_historical_research_selection_controls()
        expect_historical_snapshot_rejection("missing")
        expect_historical_snapshot_rejection("tampered")
        expect_rehashed_response_rejection("issue")
        expect_rehashed_response_rejection("parent")
    if run_ordinary_clone and os.environ.get("EFFRA_SKIP_ORDINARY_CLONE") != "1":
        expect_ordinary_clone_check()


if __name__ == "__main__":
    check()
    print("wayfinder hosted reconciliation: byte, identity, inverse, and negative controls passed")
