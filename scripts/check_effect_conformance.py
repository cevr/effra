#!/usr/bin/env python3
"""Validate selected upstream behavior mappings; optionally run their Go/JS evidence."""

from __future__ import annotations

import argparse
import json
import re
import subprocess
from pathlib import Path

import import_effect_conformance as corpus

ROOT = Path(__file__).resolve().parents[1]
MAPPING = ROOT / "conformance/effect-cases.json"
STATUSES = frozenset({"covered", "difference", "pending", "unsupported"})


def read_mapping(path: Path) -> dict:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError, RecursionError) as error:
        raise corpus.ImportError("mapping is not valid UTF-8 JSON") from error
    if not isinstance(value, dict):
        raise corpus.ImportError("mapping root must be an object")
    return value


def validate_mapping(mapping: dict, root: Path = ROOT) -> list[str]:
    manifest = corpus.validate_self_contained(root / corpus.OUTPUT_RELATIVE)
    if set(mapping) != {"schemaVersion", "sourceCommit", "cases"} or mapping["schemaVersion"] != 1 or mapping["sourceCommit"] != corpus.COMMIT:
        raise corpus.ImportError("mapping schema or upstream pin is invalid")
    cases = mapping["cases"]
    if not isinstance(cases, list) or not cases:
        raise corpus.ImportError("mapping cases must be a nonempty list")
    references = {entry["path"] for entry in manifest["files"] if entry["kind"] == "reference"}
    ids: set[str] = set()
    upstream_cases: set[tuple[str, int]] = set()
    tests: set[str] = set()
    for case in cases:
        if not isinstance(case, dict) or set(case) != {"id", "status", "upstream", "behavior", "limits", "evidence"}:
            raise corpus.ImportError("mapping case fields are invalid")
        identity = case["id"]
        if not isinstance(identity, str) or re.fullmatch(r"effect\.[a-z0-9.-]+", identity) is None or identity in ids:
            raise corpus.ImportError("mapping case id is invalid or duplicated")
        ids.add(identity)
        status = case["status"]
        if not isinstance(status, str) or status not in STATUSES:
            raise corpus.ImportError(f"invalid status for {identity}")
        if any(not isinstance(case[key], str) or not case[key].strip() for key in ("behavior", "limits")):
            raise corpus.ImportError(f"behavior and limits are required for {identity}")
        upstream = case["upstream"]
        if not isinstance(upstream, dict) or set(upstream) != {"file", "line", "label"}:
            raise corpus.ImportError(f"invalid upstream case for {identity}")
        path = corpus.safe_relative_path(upstream["file"])
        line, label = upstream["line"], upstream["label"]
        if path not in references or not isinstance(line, int) or isinstance(line, bool) or line < 1 or not isinstance(label, str) or not label:
            raise corpus.ImportError(f"upstream target is invalid for {identity}")
        lines = (root / corpus.OUTPUT_RELATIVE / path).read_text(encoding="utf-8").splitlines()
        if line > len(lines) or re.match(r'\s*it(?:\.(?:effect|live))?\("' + re.escape(label) + r'",', lines[line - 1]) is None:
            raise corpus.ImportError(f"upstream case anchor does not match for {identity}")
        if (path, line) in upstream_cases:
            raise corpus.ImportError(f"duplicate upstream case for {identity}")
        upstream_cases.add((path, line))
        evidence = case["evidence"]
        if not isinstance(evidence, list) or bool(evidence) != (status in {"covered", "difference"}):
            raise corpus.ImportError(f"status/evidence misrepresentation for {identity}")
        for pointer in evidence:
            if not isinstance(pointer, dict) or set(pointer) != {"file", "test", "targets"}:
                raise corpus.ImportError(f"invalid evidence for {identity}")
            file = corpus.safe_relative_path(pointer["file"])
            test = pointer["test"]
            if not file.startswith("internal/compiler/") or not file.endswith("_test.go") or not isinstance(test, str) or re.fullmatch(r"Test[A-Za-z0-9_]+", test) is None or pointer["targets"] != ["go", "js"]:
                raise corpus.ImportError(f"evidence must name an existing shared Go/JS test for {identity}")
            try:
                contents = (root / file).read_text(encoding="utf-8")
            except (OSError, UnicodeError) as error:
                raise corpus.ImportError(f"evidence file is unavailable for {identity}") from error
            if re.search(r"^func " + re.escape(test) + r"\(t \*testing\.T\) \{", contents, re.MULTILINE) is None:
                raise corpus.ImportError(f"evidence test is unavailable for {identity}")
            tests.add(test)
    return sorted(tests)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mapping", type=Path, default=MAPPING)
    parser.add_argument("--run", action="store_true", help="execute the existing selected tests, including real generated Go and JS")
    args = parser.parse_args()
    mapping = read_mapping(args.mapping)
    tests = validate_mapping(mapping)
    if args.run:
        result = subprocess.run(["go", "test", "./internal/compiler", "-run", "^(" + "|".join(tests) + ")$", "-count=1"], cwd=ROOT, check=False)
        if result.returncode:
            return result.returncode
    print(f"effect mapping: {len(mapping['cases'])} selected behaviors; {len(tests)} shared Go/JS evidence tests; 746 files remain reference-only")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except corpus.ImportError as error:
        raise SystemExit(f"effect_conformance: {error}")
