#!/usr/bin/env python3
"""Size-conformance matrix: raw byte, symbol and dependency receipts.

Builds every fixture in conformance/size/fixtures on both targets through
`ef build --receipt`, the Go and TypeScript/Effect controls in
conformance/size/controls, and an all-source counterfactual of selected
fixtures, under one matched toolchain configuration. Every binary is measured
by the same functions here, and Effra's own receipts are cross-checked
against them. Outputs of each program are compared across cohorts.

This is the explicit, expensive size-conformance command named by
docs/specs/binary-reachability.md; the gate runs only the deterministic
retention checks (cmd/ef/size_process_test.go). Results are raw
measurements, never performance claims.

usage: python3 scripts/size_conformance.py [--out DIR] [--record PATH]
"""

import argparse
import hashlib
import json
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SIZE = ROOT / "conformance" / "size"
SCHEMA = "effra.size-conformance/1"

# Fixture rows: (name, expected stdout or None when built only).
FIXTURES = [
    ("minimal", "minimal\n"),
    ("minimal-unused", "minimal\n"),
    ("managed", "child joined; interrupted; timed out; recovered\n"),
    ("codec", '{"id":"7","name":"Ada"}\n'),
    ("http", None),
    ("direct", "Hello, <ada!>\n"),
    ("pipe", "Hello, <ada!>\n"),
]
# Rows with optimized idiomatic Go and TypeScript/Effect controls.
CONTROLS = ["minimal", "managed"]
# Rows rebuilt with every distributed runtime source, to measure what source
# selection removes beyond the Go linker's own dead-code elimination.
COUNTERFACTUAL = ["minimal", "managed", "codec"]

GO_FLAGS = ["-trimpath", "-mod=readonly"]
STRIP_FLAGS = ["-ldflags=-s -w"]
NM_LINE = re.compile(r"^\s*[0-9a-f]*\s+(\d+)\s+(\S)\s+(.*)$")


def run(*args, cwd=ROOT, env=None, check=True):
    result = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if check and result.returncode != 0:
        raise SystemExit(f"{' '.join(map(str, args))} failed ({result.returncode}):\n{result.stdout}{result.stderr}")
    return result


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def symbol_package(name):
    """Mirror of cmd/ef/receipt.go symbolPackage; cross-checked per binary."""
    prefix, sep, _ = name.partition(":")
    if sep and not any(c in prefix for c in "./[("):
        return prefix + ":"
    if name.startswith("$"):
        return "(constant)"
    cut = min([i for i in (name.find("["), name.find("(")) if i >= 0], default=-1)
    if cut >= 0:
        name = name[:cut]
    slash = name.rfind("/") + 1
    dot = name.find(".", slash)
    if dot > slash:
        return name[:dot]
    return "(unqualified)"


def measure_binary(path, env):
    listing = run("go", "tool", "nm", "-size", "-type", str(path), env=env).stdout
    packages = {}
    for line in listing.splitlines():
        match = NM_LINE.match(line)
        if not match:
            raise SystemExit(f"unexpected nm line {line!r}")
        size, kind, name = int(match[1]), match[2], match[3]
        entry = packages.setdefault(symbol_package(name), [0, 0, 0])
        entry[0] += 1
        entry[1 if kind in "Tt" else 2] += size
    return {
        "bytes": Path(path).stat().st_size,
        "sha256": sha256(path),
        "symbols": {
            "count": sum(entry[0] for entry in packages.values()),
            "listingSha256": hashlib.sha256(listing.encode()).hexdigest(),
            # [package, symbols, text bytes, data bytes]
            "packages": [[name, *packages[name]] for name in sorted(packages)],
        },
    }


def measure_dependencies(cwd, package, env):
    out = run("go", "list", "-mod=readonly", "-deps", "-f", "{{.ImportPath}} {{.Standard}}", package, cwd=cwd, env=env).stdout
    transitive, nonstandard = [], []
    for line in out.strip().splitlines():
        path, standard = line.split(" ")
        transitive.append(path)
        if standard != "true":
            nonstandard.append(path)
    return {"count": len(transitive), "standard": len(transitive) - len(nonstandard),
            "nonStandard": sorted(nonstandard), "transitive": sorted(transitive)}


def go_build(cwd, package, output, env, strip=False):
    flags = GO_FLAGS + (STRIP_FLAGS if strip else [])
    run("go", "build", *flags, "-o", str(output), package, cwd=cwd, env=env)


def check_output(label, command, expected, cwd=ROOT):
    actual = run(*command, cwd=cwd).stdout
    if actual != expected:
        raise SystemExit(f"{label}: output {actual!r}, want {expected!r}")


def bun_bundle(entry, output, external, cwd):
    args = ["bun", "build", str(entry), "--minify", "--target=node", "--outfile", str(output)]
    if external:
        args += ["--external", "effect"]
    run(*args, cwd=cwd)
    return {"bytes": Path(output).stat().st_size, "sha256": sha256(output)}


def runtime_catalog_files():
    text = (ROOT / "runtime" / "effra" / "embed.go").read_text()
    match = re.search(r"^//go:embed (.+)$", text, re.M)
    return match[1].split()


def identity(env):
    head = run("git", "rev-parse", "HEAD").stdout.strip()
    dirty = bool(run("git", "status", "--porcelain").stdout.strip())
    goenv = json.loads(run("go", "env", "-json", "GOVERSION", "GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", env=env).stdout)
    effect = json.loads((ROOT / "node_modules" / "effect" / "package.json").read_text())["version"]
    tsc = shutil.which("tsc")
    return {
        "commit": head,
        "dirty": dirty,
        "go": goenv,
        "goFlags": GO_FLAGS,
        "stripFlags": STRIP_FLAGS,
        "bun": run("bun", "--version").stdout.strip(),
        "bundle": "bun build --minify --target=node [--external effect]",
        "node": run("node", "--version").stdout.strip(),
        "effect": effect,
        "tsc": run(tsc, "--version").stdout.strip() if tsc else None,
        "machine": platform.machine(),
        "symbolTool": "go tool nm -size -type",
        "sizeMethod": "file size in bytes (os.stat); stripped companions add only -ldflags=-s -w",
        "fixtures": {p.name: sha256(p) for p in sorted((SIZE / "fixtures").glob("*.ef"))},
        "controls": {str(p.relative_to(SIZE)): sha256(p) for p in sorted((SIZE / "controls").rglob("*")) if p.is_file()},
        "runtimeSources": {name: sha256(ROOT / "runtime" / "effra" / name) for name in runtime_catalog_files()},
    }


def cross_check(name, receipt, measured, deps):
    if receipt["binary"]["bytes"] != measured["bytes"]:
        raise SystemExit(f"{name}: receipt binary bytes disagree with measurement")
    if receipt["symbols"]["listingSha256"] != measured["symbols"]["listingSha256"]:
        raise SystemExit(f"{name}: receipt symbol listing disagrees with measurement")
    ours = {row[0]: row[1:] for row in measured["symbols"]["packages"]}
    theirs = {p["package"]: [p["symbols"], p["textBytes"], p["dataBytes"]] for p in receipt["symbols"]["packages"]}
    if ours != theirs:
        raise SystemExit(f"{name}: receipt symbol packages disagree with measurement")
    if receipt["dependencies"]["transitive"] != deps["transitive"]:
        raise SystemExit(f"{name}: receipt dependencies disagree with measurement")


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--out", help="directory for binaries, bundles and full receipts (default: a temporary directory)")
    parser.add_argument("--record", help="write the summarized matrix JSON here")
    args = parser.parse_args()
    for tool in ("go", "bun", "node"):
        if not shutil.which(tool):
            raise SystemExit(f"size conformance requires {tool} on PATH")
    out = Path(args.out).resolve() if args.out else Path(tempfile.mkdtemp(prefix="effra-size-"))
    out.mkdir(parents=True, exist_ok=True)
    (out / "node_modules").unlink(missing_ok=True)
    (out / "node_modules").symlink_to(ROOT / "node_modules")
    env = dict(os.environ, CGO_ENABLED="0", GOWORK="off", GOFLAGS="")

    ef = out / "ef"
    go_build(ROOT, "./cmd/ef", ef, env)
    distribution = {
        "compilerExecutable": {"bytes": ef.stat().st_size},
        "runtimeSources": {"files": len(runtime_catalog_files()),
                           "bytes": sum((ROOT / "runtime" / "effra" / n).stat().st_size for n in runtime_catalog_files())},
    }

    rows = {}
    for name, expected in FIXTURES:
        source = SIZE / "fixtures" / f"{name}.ef"
        receipt_path = out / "receipts" / f"{name}.go.json"
        binary = out / "bin" / name
        run(str(ef), "build", str(source), "-o", str(binary), "--receipt", str(receipt_path), cwd=out, env=env)
        receipt = json.loads(receipt_path.read_text())
        generation = Path(receipt["generation"]["directory"])
        measured = measure_binary(binary, env)
        deps = measure_dependencies(generation, ".", env)
        cross_check(name, receipt, measured, deps)
        stripped = out / "bin" / f"{name}.stripped"
        go_build(generation, ".", stripped, env, strip=True)
        if expected is not None:
            check_output(f"{name} (go)", [str(binary)], expected)
            check_output(f"{name} (go, stripped)", [str(stripped)], expected)
        native = {
            "runtimeModules": receipt["plan"]["runtimeModules"],
            "requirements": receipt["plan"]["requirements"],
            "generated": {"mainBytes": receipt["generation"]["mainBytes"], "runtimeBytes": receipt["generation"]["runtimeBytes"],
                          "runtimeFiles": sorted(f["path"].removeprefix("runtime/") for f in receipt["generation"]["files"] if f["path"].startswith("runtime/"))},
            "imports": receipt["imports"],
            "dependencies": {k: deps[k] for k in ("count", "standard", "nonStandard")},
            "binary": measured,
            "stripped": {"bytes": stripped.stat().st_size, "sha256": sha256(stripped)},
        }

        js_receipt_path = out / "receipts" / f"{name}.js.json"
        module = out / "js" / f"{name}.mjs"
        run(str(ef), "build", str(source), "--target", "js", "--entry", "-o", str(module), "--receipt", str(js_receipt_path), cwd=out, env=env)
        js_receipt = json.loads(js_receipt_path.read_text())
        application = bun_bundle(module, out / "js" / f"{name}.app.min.mjs", True, out)
        deployment = bun_bundle(module, out / "js" / f"{name}.deploy.min.mjs", False, out)
        if expected is not None:
            check_output(f"{name} (js)", ["node", str(module)], expected, cwd=out)
            check_output(f"{name} (js bundle)", ["node", str(out / "js" / f"{name}.deploy.min.mjs")], expected, cwd=out)
        javascript = {
            "requirements": js_receipt["plan"]["requirements"],
            "module": {"bytes": js_receipt["module"]["bytes"], "sha256": js_receipt["module"]["sha256"]},
            "declarationBytes": js_receipt["declaration"]["bytes"],
            "externalRuntime": js_receipt["externalRuntime"],
            "applicationMinified": application,
            "deploymentMinified": deployment,
            "externalRuntimeMinifiedBytes": deployment["bytes"] - application["bytes"],
        }
        rows[name] = {"expectedOutput": expected, "go": native, "js": javascript}

    for name in COUNTERFACTUAL:
        receipt = json.loads((out / "receipts" / f"{name}.go.json").read_text())
        copy = out / "counterfactual" / name
        shutil.rmtree(copy, ignore_errors=True)
        shutil.copytree(receipt["generation"]["directory"], copy)
        for source in runtime_catalog_files():
            shutil.copyfile(ROOT / "runtime" / "effra" / source, copy / "runtime" / source)
        binary = out / "counterfactual" / f"{name}.bin"
        stripped = out / "counterfactual" / f"{name}.stripped"
        go_build(copy, ".", binary, env)
        go_build(copy, ".", stripped, env, strip=True)
        expected = rows[name]["expectedOutput"]
        check_output(f"{name} (all-source)", [str(binary)], expected)
        deps = measure_dependencies(copy, ".", env)
        rows[name]["go"]["allSourceCounterfactual"] = {
            "dependencies": {k: deps[k] for k in ("count", "standard", "nonStandard")},
            "platformDependencies": sorted(set(deps["transitive"]) & {"net", "net/http", "crypto/tls", "encoding/json", "os/exec"}),
            "binary": measure_binary(binary, env),
            "stripped": {"bytes": stripped.stat().st_size, "sha256": sha256(stripped)},
        }

    controls = {}
    tsc = shutil.which("tsc")
    if tsc:
        ts_files = [str(SIZE / "controls" / "ts" / "host.d.ts")] + [str(SIZE / "controls" / "ts" / f"{n}.ts") for n in CONTROLS]
        run(tsc, "--noEmit", "--strict", "--exactOptionalPropertyTypes", "--module", "nodenext", "--moduleResolution", "nodenext",
            "--target", "es2022", "--lib", "es2022,dom,esnext.disposable", *ts_files)
    for name in CONTROLS:
        expected = rows[name]["expectedOutput"]
        package = f"./conformance/size/controls/go/{name}"
        binary = out / "controls" / f"go-{name}"
        stripped = out / "controls" / f"go-{name}.stripped"
        go_build(ROOT, package, binary, env)
        go_build(ROOT, package, stripped, env, strip=True)
        check_output(f"{name} (go control)", [str(binary)], expected)
        deps = measure_dependencies(ROOT, package, env)
        ts = SIZE / "controls" / "ts" / f"{name}.ts"
        application = bun_bundle(ts, out / "controls" / f"ts-{name}.app.min.mjs", True, ROOT)
        deployment = bun_bundle(ts, out / "controls" / f"ts-{name}.deploy.min.mjs", False, ROOT)
        check_output(f"{name} (ts control)", ["node", str(out / "controls" / f"ts-{name}.deploy.min.mjs")], expected)
        controls[name] = {
            "go": {"dependencies": {k: deps[k] for k in ("count", "standard", "nonStandard")},
                   "binary": measure_binary(binary, env),
                   "stripped": {"bytes": stripped.stat().st_size, "sha256": sha256(stripped)}},
            "typescript": {"sourceBytes": ts.stat().st_size, "applicationMinified": application, "deploymentMinified": deployment,
                           "externalRuntimeMinifiedBytes": deployment["bytes"] - application["bytes"]},
        }

    matrix = {"schema": SCHEMA, "identity": identity(env), "distribution": distribution, "fixtures": rows, "controls": controls}
    (out / "matrix.json").write_text(json.dumps(matrix, indent=1) + "\n")
    if args.record:
        Path(args.record).write_text(json.dumps(matrix, indent=1) + "\n")
    print(f"size conformance: {len(rows)} fixtures, {len(controls)} controls, {len(COUNTERFACTUAL)} all-source counterfactuals")
    print(f"{'row':<16}{'go bytes':>11}{'stripped':>11}{'deps':>6}{'js module':>11}{'js app.min':>11}{'js deploy':>11}")
    for name, row in rows.items():
        go, js = row["go"], row["js"]
        print(f"{name:<16}{go['binary']['bytes']:>11}{go['stripped']['bytes']:>11}{go['dependencies']['count']:>6}"
              f"{js['module']['bytes']:>11}{js['applicationMinified']['bytes']:>11}{js['deploymentMinified']['bytes']:>11}")
        if "allSourceCounterfactual" in go:
            cf = go["allSourceCounterfactual"]
            print(f"{'  all-source':<16}{cf['binary']['bytes']:>11}{cf['stripped']['bytes']:>11}{cf['dependencies']['count']:>6}")
    for name, control in controls.items():
        go, ts = control["go"], control["typescript"]
        print(f"{'go ' + name:<16}{go['binary']['bytes']:>11}{go['stripped']['bytes']:>11}{go['dependencies']['count']:>6}")
        print(f"{'ts ' + name:<16}{'':>11}{'':>11}{'':>6}{ts['sourceBytes']:>11}{ts['applicationMinified']['bytes']:>11}{ts['deploymentMinified']['bytes']:>11}")
    print(f"out: {out}")


if __name__ == "__main__":
    sys.exit(main())
