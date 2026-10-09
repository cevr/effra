#!/usr/bin/env python3
"""Compare two checkers over the ownership corpus; execute the programs they disagree on.

E2 integrates only as a strict improvement: no program the base checker admits
and runs safely may be refused by the head. This script decides every program
with both checkers (refusals are EF107 and EF123) and executes each program
the two decide differently, with the erased checker, to say which side was
right. Output classes for a program decided differently:

  regression   base admits, head refuses, execution is safe (an over-refusal head introduced)
  improvement  base admits, head refuses, execution is unsafe or row-unsafe
  policy       base admits, head refuses, the row records "declared-row-policy": the refusal
               rests on what its declared rows admit, not on an executed failure (a raising
               twin witnesses the shape; see scripts/ownership_truth.py)
  twin         base admits, head refuses, the row is recorded unsafe but executes safe on its
               recorded schedule, and its raising twin (<row>_raising) witnesses the shape: a
               reviewed mismatch of internal/compiler/testdata/ownership_matrix/known_mismatches.json.
               It is labelled by its executed class, so it is not an improvement.
  unsound      base refuses, head admits, execution is unsafe or row-unsafe (head regressed)
  relaxed      base refuses, head admits, execution is safe

A matrix or probe row whose recorded class disagrees with its executed class (a known mismatch
which is neither a twin-witnessed row nor a row the erased checker cannot execute) fails the
sweep, as does a known mismatch whose recorded class is no longer the row's. Improvements which
rest on the oracle alone (rows with a later, non-erasable diagnostic) are counted and printed.

Matrix and probe rows are judged by their recorded truth (executed by
scripts/ownership_truth.py); --extra programs are executed here, three times
per target, and are safe only if every run is. A program executes one path, so a
refusal of a path-dependent program is not an over-refusal: --expect names
those in a JSON object {file name: reason}, reported as "expected" and not
counted as a regression. Programs the erased checker cannot run are
"unverified" (reported, not failing). A program either checker reports a
diagnostic other than an ownership refusal for, on a target, is not a
program of that target (ill-typed, or unsupported there): it is "skipped".

The result is reported twice: with the declared-row policy (a policy row is an
accepted refusal) and without it (a policy row counts as a regression).
Exit status 1 is a regression or an unsound program with the policy, or
without it under --strict.

Usage:
  scripts/ownership_sweep.py --base BIN --head BIN --erased BIN --dump DIR [--extra DIR ...] [--expect FILE]
(--extra and --expect default to internal/compiler/testdata/ownership_sweep) [--jobs N]

DIR holds programs.json (see scripts/ownership_truth.py) and a node_modules link
for JS runs. --extra adds every *.ef file below a directory (hand probes,
examples). Exit status is 1 when there is a regression or an unsound program.
"""
import argparse
import concurrent.futures as cf
import json
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from ownership_truth import POLICY, classify, env, execute, failure_labels  # noqa: E402

REFUSALS = ("EF123", "EF107")
TWIN = "twin-witnessed"
ORACLE = "+oracle"


def refusals(binary, path, target, tmp):
    """The ownership refusals of a program, or ["ERR"] if the checker failed or
    the program has any other diagnostic (it is not a program of this target)."""
    p = subprocess.run([binary, "check", os.path.basename(path), "--target", target], capture_output=True, text=True,
                       env=env(tmp), timeout=300, cwd=os.path.dirname(path))
    try:
        report = json.loads(p.stdout.strip().splitlines()[-1])
    except Exception:
        return ["ERR"]
    codes = {d["code"] for d in report.get("diagnostics", [])}
    if codes - set(REFUSALS):
        return ["ERR"]
    return sorted(codes)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", required=True)
    ap.add_argument("--head", required=True)
    ap.add_argument("--erased", required=True)
    ap.add_argument("--dump", required=True)
    ap.add_argument("--extra", action="append", default=[], help="directory of *.ef programs (default: internal/compiler/testdata/ownership_sweep and examples)")
    ap.add_argument("--expect")
    ap.add_argument("--known", help="reviewed mismatches between recorded and executed truth (default: the matrix known_mismatches.json)")
    ap.add_argument("--jobs", type=int, default=8)
    ap.add_argument("--strict", action="store_true", help="fail on the policy rows too")
    a = ap.parse_args()
    repo = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
    if not a.extra:
        a.extra = [os.path.join(repo, "internal/compiler/testdata/ownership_sweep"), os.path.join(repo, "examples")]
    if a.expect is None:
        a.expect = os.path.join(repo, "internal/compiler/testdata/ownership_sweep/expect.json")
    tmp = os.path.join(a.dump, "tmp")
    work = os.path.join(a.dump, "sweep")
    os.makedirs(tmp, exist_ok=True)
    os.makedirs(work, exist_ok=True)
    link = os.path.join(a.dump, "node_modules")
    if os.path.exists(link) and not os.path.exists(os.path.join(work, "node_modules")):
        os.symlink(os.path.realpath(link), os.path.join(work, "node_modules"))
    corpus = []
    for r in json.load(open(os.path.join(a.dump, "programs.json"))):
        path = os.path.join(work, r["set"] + "__" + r["name"].replace("@", "_") + ".ef")
        open(path, "w").write(r["source"])
        corpus.append((r["set"] + "/" + r["name"], path, {t: r[t] for t in ("go", "js") if t in r}))
    fixture = os.path.join(os.path.realpath(a.dump), "fixture.txt")
    for extra in a.extra:
        for root, _, files in os.walk(extra):
            if "node_modules" in root:
                continue
            for f in sorted(files):
                if f.endswith(".ef"):
                    source = os.path.join(root, f)
                    key = os.path.relpath(source, extra)
                    # A program names the fixture file as @FIXTURE@.
                    path = os.path.join(work, "extra__" + key.replace(os.sep, "_"))
                    open(path, "w").write(open(source).read().replace("@FIXTURE@", fixture))
                    corpus.append(("extra/" + key, path, {"go": None, "js": None}))

    corpus_targets = {key: targets for key, _, targets in corpus}
    expect = json.load(open(a.expect)) if a.expect else {}
    if a.known is None:
        a.known = os.path.join(repo, "internal/compiler/testdata/ownership_matrix/known_mismatches.json")
    known = json.load(open(a.known)) if os.path.exists(a.known) else {}
    recorded_by_row = {key + " [" + t + "]": c for key, _, targets in corpus for t, c in targets.items() if c}
    stale = sorted(row for row, mismatch in known.items() if row in recorded_by_row and recorded_by_row[row] != mismatch["recorded"])
    for row in stale:
        print(f"STALE        {row}: recorded {recorded_by_row[row]}, known mismatch says {known[row]['recorded']}")

    def one(item):
        key, path, targets = item
        row = {"key": key}
        for target in targets:
            row[target] = (refusals(a.base, path, target, tmp), refusals(a.head, path, target, tmp))
        return row, path

    with cf.ThreadPoolExecutor(a.jobs) as pool:
        decided = list(pool.map(one, corpus))
    different = []
    for row, path in decided:
        for target in [t for t in ("go", "js") if t in row]:
            base, head = row[target]
            if bool(base) != bool(head) and "ERR" not in base + head:
                different.append((row["key"], path, target, bool(base), corpus_targets[row["key"]][target]))
    skipped = sorted({row["key"] + " [" + t + "]" for row, _ in decided for t in ("go", "js") if t in row and "ERR" in row[t][0] + row[t][1]})
    print(f"{len(decided)} programs, {len(different)} decisions differ, {len(skipped)} skipped as ill-typed or unsupported on a target")
    for item in skipped:
        print(f"skipped      {item}")

    def labelled(key, target, recorded):
        """The class of a recorded row: its executed class where the reviewed
        mismatches say the two differ. A twin-witnessed row executes safe; a row
        the erased checker cannot run keeps its recorded class (the oracle's)."""
        mismatch = known.get(key + " [" + target + "]")
        if mismatch is None:
            return recorded
        if mismatch["recorded"] != recorded:
            return "stale:" + mismatch["recorded"]
        if mismatch["executed"] == "safe" and key.startswith("matrix/") and key + "_raising" in corpus_targets:
            return TWIN
        if mismatch["executed"].startswith("unknown:diagnostics"):
            return recorded + ORACLE
        return "disagree:" + mismatch["executed"]

    def execute_one(item):
        key, path, target, base_refuses, recorded = item
        if recorded is not None:
            return key, target, base_refuses, labelled(key, target, recorded)
        # A race decides some programs (a failing child with no sleep): a
        # program is safe only if every run is.
        labels = failure_labels(path)
        runs = [classify(*execute(a.erased, path, target, tmp), labels) for _ in range(3)]
        run = next((r for r in runs if r != "safe"), "safe")
        return key, target, base_refuses, run

    counts = {"regression": 0, "improvement": 0, "policy": 0, "twin": 0, "unsound": 0, "relaxed": 0, "expected": 0, "unverified": 0}
    oracle_only = 0
    disagree = len(stale)
    with cf.ThreadPoolExecutor(a.jobs) as pool:
        for key, target, base_refuses, run in pool.map(execute_one, different):
            if run.startswith(("stale:", "disagree:")):
                disagree += 1
                print(f"DISAGREE     {key} [{target}] {run}")
                continue
            if run.endswith(ORACLE):
                run = run[: -len(ORACLE)]
                oracle_only += 1
            if run == TWIN:
                kind = "unsound" if base_refuses else "twin"
            elif run == "safe":
                kind = "relaxed" if base_refuses else "regression"
                if kind == "regression" and os.path.basename(key) in expect:
                    kind = "expected"
            elif run == POLICY:
                kind = "unsound" if base_refuses else "policy"
            elif run in ("unsafe", "row-unsafe"):
                kind = "unsound" if base_refuses else "improvement"
            else:
                kind = "unverified"
            counts[kind] += 1
            print(f"{kind:12} {key} [{target}] {'executed' if key.startswith('extra/') else 'recorded'} {run}")
    print("with the declared-row policy:    " + json.dumps(counts))
    strict = dict(counts, regression=counts["regression"] + counts["policy"], policy=0)
    print("without the declared-row policy: " + json.dumps(strict))
    print(f"{oracle_only} decisions rest on the recorded oracle alone (the row has no executable witness); {disagree} recorded/executed disagreements")
    worst = strict if a.strict else counts
    sys.exit(1 if worst["regression"] or worst["unsound"] or disagree else 0)


if __name__ == "__main__":
    main()
