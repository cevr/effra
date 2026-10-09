#!/usr/bin/env python3
"""Execute the ownership matrix and hand probes to witness their recorded truth.

Truth is what running a program does, never what the checker says. The
checker is erased for the run (EF123 and EF107 refusals dropped, so every
program executes) and the same runtime executes it. Output classes:

  safe        exit 0, output, no line "closed"
  unsafe      exit 0 and a line "closed": a handle was used after its owner closed
  row-unsafe  non-zero exit naming an uncaught typed failure that escaped every declared row
  defect      any other non-zero exit (a runtime defect, not a typed failure)

Usage:
  scripts/ownership_truth.py [--erased BIN | --build] --dump DIR [--only REGEX] [--jobs N] [--results FILE]
                             [--known FILE] [--update-known] [--write-truth PATH]

  DIR holds programs.json, written by
    EFFRA_OWNERSHIP_DUMP=DIR EFFRA_OWNERSHIP_FIXTURE=DIR/fixture.txt \\
      go test ./internal/compiler -run TestOwnershipMatrixDump -count=1
  and a node_modules link for JS runs. Without --erased, BIN is built from the
  current tree with the two refusals erased (scripts/ownership_truth.py --build).

A matrix row records one of those classes, or "declared-row-policy": a row
refused for what its declared rows admit, whose recorded schedule executes
safe. It is not executed truth. It matches an executed "safe" only if its
raising twin (<row>_raising, whose children actually fail) executed
"row-unsafe"; the policy rows are counted separately.

Every other row whose executed class differs from its recorded one is a
mismatch. The mismatches are a reviewed set, committed in --known
(internal/compiler/testdata/ownership_matrix/known_mismatches.json, each with
its reason). A run fails on drift in either direction: a mismatch which is not
in the set, or a recorded one which no longer happens. --update-known rewrites
the set from an unfiltered run, for review as a diff.

--write-truth PATH writes truth.json (internal/compiler/testdata/ownership_matrix/):
the recorded class of every matrix row, in generator order. It needs an
unfiltered run without drift, so every row is either executed truth, a policy
row with its twin, or a known mismatch.

Each program is executed once per target (the sweep repeats the programs it executes itself). The
raising twins of the policy rows need Files, which the JS host lacks, so the 64 policy programs and
their twins are Go-only; the shape is executed on both targets by the portable controls
(cc_joins_in_order, cc_joins_in_order_first_caught and the cc_alt_* rows).

A probe with a "witness" in probes.json is not executed itself: its source
never runs the shape, or its failure races the exit. The named twin (the
same shape made deterministic) is executed and must show the probe's truth.
"""
import argparse
import concurrent.futures as cf
import json
import os
import re
import subprocess
import sys

BUILTIN_FAILURES = ("IoError", "Timeout", "GoError", "AssertionFailed", "WithFile")
POLICY = "declared-row-policy"
ERASE = '\tif code == "EF123" || code == "EF107" { // PROBE ONLY: erase ownership and row refusals to obtain the runtime truth\n\t\treturn\n\t}\n'


def env(tmp):
    e = dict(os.environ)
    e.update(GOCACHE=os.environ.get("GOCACHE", os.path.expanduser("~/.cache/go-build")), TMPDIR=tmp, GOPROXY="off")
    return e


def build_erased(out, root, scratch):
    """Build an erased checker from the current tree (a copy: the tree is untouched)."""
    import shutil
    import tempfile
    tmp = tempfile.mkdtemp(prefix="erased-", dir=scratch)
    for name in subprocess.check_output(["git", "-C", root, "ls-files", "-z", "--", "go.mod", "cmd", "internal", "runtime"], text=True).split("\0"):
        if name and os.path.exists(os.path.join(root, name)):
            os.makedirs(os.path.dirname(os.path.join(tmp, name)), exist_ok=True)
            shutil.copy(os.path.join(root, name), os.path.join(tmp, name))
    path = os.path.join(tmp, "internal/compiler/semantic.go")
    src = open(path).read()
    head = "func (c *checker) diagnostic(code, message string, span Span) {\n"
    assert head in src, "diagnostic function not found"
    open(path, "w").write(src.replace(head, head + ERASE, 1))
    built = subprocess.run(["go", "build", "-o", out, "./cmd/ef"], cwd=tmp, env=env(os.path.join(scratch, "tmp")), capture_output=True, text=True)
    if built.returncode:
        sys.exit("erased build failed: " + built.stderr)
    shutil.rmtree(tmp, ignore_errors=True)


def discard_build(directory, path):
    """Remove what `ef run` built beside the program (a native binary or a bundle of several MB per program)."""
    import glob
    import shutil
    stem = os.path.join(directory, "dist", os.path.splitext(os.path.basename(path))[0])
    # exactly this program's outputs: the file or directory named by the stem, and "<stem>.<ext>" files
    for built in [stem] + glob.glob(glob.escape(stem) + ".*"):
        if os.path.isdir(built):
            shutil.rmtree(built, ignore_errors=True)
        elif os.path.exists(built):
            os.remove(built)


def execute(binary, path, target, tmp):
    """Run one program on one target in a directory of its own: the Go and the JS
    run of a program (and its repeats) never share a dist directory, so one
    cannot discard the other's build output."""
    import shutil
    directory = os.path.join(os.path.dirname(path), "target-" + target)
    os.makedirs(directory, exist_ok=True)
    copy = os.path.join(directory, os.path.basename(path))
    shutil.copy(path, copy)
    try:
        p = subprocess.run([binary, "run", os.path.basename(path), "--target", target], capture_output=True, text=True,
                           env=env(tmp), timeout=300, cwd=directory)
    finally:
        discard_build(directory, path)
        os.remove(copy)
    out = [l for l in p.stdout.splitlines() if l.strip()]
    err = [l for l in p.stderr.splitlines() if l.strip()]
    return p.returncode, out, err


def failure_labels(path):
    """The failure labels a program can raise: its declared errors and the builtins."""
    declared = re.findall(r"^\s*error\s+(\w+)", open(path).read(), re.M)
    return re.compile(r"\b(" + "|".join(sorted(set(declared) | set(BUILTIN_FAILURES))) + r")\b")


def classify(rc, out, err, labels):
    if "source has diagnostics" in " ".join(out + err) or "refusing to emit source with diagnostics" in " ".join(out + err):
        return "unknown:diagnostics"
    if rc == 0 and "closed" in out:
        return "unsafe"
    if rc == 0 and out:
        return "safe"
    if rc != 0 and "fiber owner is closed" in " ".join(out + err):
        # The runtime refuses a fiber used after its owner closed: a use
        # after close, caught as a defect rather than printed as "closed".
        return "unsafe"
    if rc != 0 and labels.search(" ".join(out + err)):
        return "row-unsafe"
    if rc != 0 and ("interrupt: context canceled" in " ".join(out + err) or "All fibers interrupted without error" in " ".join(out + err)):
        # An uncaught interruption with no typed failure beside it: the
        # cancellation alone, which no row declares and no handler matches.
        return "interrupted"
    return "defect:" + " | ".join((out + err)[-2:])[:120]


def reason(mismatch):
    if mismatch["executed"].startswith("unknown:diagnostics"):
        return "a later diagnostic is not erasable, so the row has no executed witness"
    return "the recorded schedule executes the non-raising path; the row's truth is that of its raising twin"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--erased")
    ap.add_argument("--build", action="store_true")
    ap.add_argument("--dump", required=True)
    ap.add_argument("--only")
    ap.add_argument("--jobs", type=int, default=8)
    ap.add_argument("--write-truth", metavar="PATH")
    ap.add_argument("--results")
    ap.add_argument("--known")
    ap.add_argument("--update-known", action="store_true")
    a = ap.parse_args()
    root = subprocess.check_output(["git", "rev-parse", "--show-toplevel"], text=True).strip()
    if a.known is None:
        a.known = os.path.join(root, "internal/compiler/testdata/ownership_matrix/known_mismatches.json")
    tmp = os.path.join(a.dump, "tmp")
    os.makedirs(tmp, exist_ok=True)
    erased = a.erased
    if erased is None:
        erased = os.path.join(a.dump, "ef-erased")
        build_erased(erased, root, a.dump)
    rows = json.load(open(os.path.join(a.dump, "programs.json")))
    pat = re.compile(a.only) if a.only else None
    work = os.path.join(a.dump, "src")
    os.makedirs(work, exist_ok=True)
    link = os.path.join(a.dump, "node_modules")
    if os.path.exists(link) and not os.path.exists(os.path.join(work, "node_modules")):
        os.symlink(os.path.realpath(link), os.path.join(work, "node_modules"))

    def one(r):
        key = r["set"] + "/" + r["name"]
        if pat and not pat.search(key):
            return None
        path = os.path.join(work, r["set"] + "__" + r["name"].replace("@", "_") + ".ef")
        open(path, "w").write(r["source"])
        res = {"key": key, "recorded": {"go": r["go"], "js": r.get("js", "")}}
        if r.get("witness"):
            # Witnessed by another probe's execution; resolved after the pool.
            res["witness"] = r["witness"]
            return res
        labels = failure_labels(path)
        res["go"] = classify(*execute(erased, path, "go", tmp), labels)
        if r.get("js"):
            res["js"] = classify(*execute(erased, path, "js", tmp), labels)
        return res

    with cf.ThreadPoolExecutor(a.jobs) as pool:
        results = [x for x in pool.map(one, rows) if x]
    executed = {x["key"]: x for x in results}
    for x in results:
        if "witness" not in x:
            continue
        twin = executed.get(x["witness"])
        if twin is None:
            sys.exit(f"{x['key']}: witness {x['witness']} was not executed (filtered out?)")
        x["go"] = twin["go"]
        if x["recorded"]["js"]:
            x["js"] = twin.get("js", "")
    # A policy row is executed safe by design; its raising twin executes the shape.
    policy, bad = 0, 0
    mismatches = {}
    for x in results:
        for target in ("go", "js"):
            want = x["recorded"][target]
            if not want:
                continue
            if want == POLICY:
                twin = executed.get(x["key"] + "_raising")
                if twin is None:
                    if pat:
                        continue
                    sys.exit(f"{x['key']}: policy row without a raising twin")
                if x.get(target) == "safe" and twin.get(target) == "row-unsafe":
                    policy += 1
                else:
                    bad += 1
                    print(f"POLICY {x['key']} [{target}]: executed {x.get(target)}, raising twin {twin.get(target)}; want safe and row-unsafe")
                continue
            if x.get(target) != want:
                mismatches[f"{x['key']} [{target}]"] = {"recorded": want, "executed": x.get(target)}
    known = json.load(open(a.known)) if a.known and os.path.exists(a.known) and not a.update_known else {}
    if a.update_known:
        if pat:
            sys.exit("--update-known needs an unfiltered run")
        known = {k: dict(v, reason=reason(v)) for k, v in sorted(mismatches.items())}
        with open(a.known, "w") as f:
            f.write("{\n" + ",\n".join(" %s: %s" % (json.dumps(k), json.dumps(v)) for k, v in known.items()) + "\n}\n")
        mismatches = {}
        known = {}
    for key, got in sorted(mismatches.items()):
        want = known.get(key)
        if want is None or want["recorded"] != got["recorded"] or want["executed"] != got["executed"]:
            bad += 1
            print(f"MISMATCH {key}: recorded {got['recorded']}, executed {got['executed']}" + ("" if want is None else f" (known: executed {want['executed']})"))
    if not pat:
        for key in sorted(set(known) - set(mismatches)):
            bad += 1
            print(f"STALE known mismatch {key}: it no longer happens")
    print(f"{len(results)} programs executed, {policy} policy rows witnessed by their raising twins, {len(set(known) & set(mismatches))} known mismatches, {bad} drifts")
    if a.results:
        json.dump(results, open(a.results, "w"), indent=1)
    if a.write_truth:
        if pat or bad:
            sys.exit("--write-truth needs an unfiltered run without drift")
        truth = []
        for x in results:
            if x["key"].startswith("matrix/"):
                row = {"name": x["key"][len("matrix/"):], "go": x["recorded"]["go"]}
                if x["recorded"]["js"]:
                    row["js"] = x["recorded"]["js"]
                truth.append(row)
        with open(a.write_truth, "w") as f:
            f.write("[\n" + ",\n".join("{\n" + ",\n".join('"%s": "%s"' % kv for kv in row.items()) + "\n}" for row in truth) + "\n]\n")
        print(f"wrote {len(truth)} rows to {a.write_truth}")
    sys.exit(1 if bad else 0)


if __name__ == "__main__":
    main()
