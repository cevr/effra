#!/usr/bin/env python3
"""Keep README.md truthful: every code block it shows compiles, is a verbatim excerpt
of a gated file, or is labelled as a sketch. The three checkout programs print the same
thing, and the comparison table quotes real diagnostics."""
import json, pathlib, re, shutil, subprocess, tempfile

root = pathlib.Path(__file__).resolve().parents[1]
ef = str(root / "bin/ef")
readme = (root / "README.md").read_text()


def process(*command, success=True, input_text=None):
    result = subprocess.run(command, cwd=root, text=True, capture_output=True, input=input_text)
    assert (result.returncode == 0) == success, (command, result.stdout, result.stderr)
    return result


def run(*args, success=True):
    return process(ef, *args, success=success)


showcase = (root / "examples/checkout.ef").read_text()
blocks = re.findall(r"^```(rust|ts|go)\n(.*?)^```$", readme, flags=re.S | re.M)
counts = {"checked": 0, "excerpt": 0, "sketch": 0, "showcase": 0}
with tempfile.TemporaryDirectory() as scratch:
    for index, (language, body) in enumerate(blocks):
        first, _, rest = body.partition("\n")
        if language == "rust" and first.startswith("// Sketch"):
            counts["sketch"] += 1
            continue
        excerpt = re.fullmatch(r"// From (\S+)", first)
        if excerpt:
            source = (root / excerpt.group(1)).read_text()
            assert rest in source, f"README block {index} is not a verbatim excerpt of {excerpt.group(1)}"
            counts["excerpt"] += 1
            continue
        assert language == "rust", f"README {language} block {index} must name its gated source with // From"
        snippet = pathlib.Path(scratch) / f"snippet{index}.ef"
        snippet.write_text(body)
        report = json.loads(run("check", str(snippet)).stdout)
        assert report["checked"], (index, report["diagnostics"])
        if "effect fn checkout(" in body:
            # The checkout program is the README's headline claim: after the
            # canonical formatter it must be a verbatim part of the gated
            # showcase, so the README cannot drift from examples/checkout.ef.
            formatted = process(ef, "fmt", "--stdin", input_text=body).stdout.strip()
            assert formatted in showcase, f"README checkout block {index} is not a substring of examples/checkout.ef after ef fmt"
            counts["showcase"] += 1
        counts["checked"] += 1
assert counts["checked"] >= 3 and counts["excerpt"] >= 6 and counts["sketch"] >= 1 and counts["showcase"] == 1, counts

# README: "All three versions are checked in and print the same thing." Run all three.
outputs = {f"ef run --target {target}": run("run", "examples/checkout.ef", "--target", target).stdout
           for target in ("go", "js")}
outputs["go run"] = process("go", "run", "./examples/compare/go").stdout
outputs["bun run"] = process("bun", "run", "--no-install", "examples/compare/checkout.ts").stdout
assert set(outputs.values()) == {"paid auth-7\nno such order\n"}, outputs
# checkout.ts claims its Effect type is inferred; a strict typecheck keeps that claim honest.
# The repository has no TypeScript dependency; the gate environment supplies tsc on PATH.
tsc = shutil.which("tsc")
if tsc:
    process(tsc, "--noEmit", "--strict", "--exactOptionalPropertyTypes", "--module", "nodenext",
            "--moduleResolution", "nodenext", "--target", "es2022", "--lib", "es2022,dom,esnext.disposable",
            "examples/compare/checkout.ts")
counts["typescript"] = "strict" if tsc else "unchecked: no tsc on PATH"
contract = json.loads(run("inspect", "examples/checkout.ef", "checkout").stdout)["symbol"]["contract"]
assert contract["failures"] == ["GatewayDown", "OrderNotFound", "Timeout"]
assert contract["requirements"] == ["Gateway", "Orders", "Scheduler"]

# Each row of "Same mistakes, three compilers" applies one edit to the showcase and
# quotes the resulting diagnostic; both the edit's effect and the quote are checked.
mistakes = [
    ('        Payment.Declined { reason } => "declined: " + reason\n', "",
     "EF117: missing match arm for Payment.Declined"),
    ("raises { OrderNotFound, GatewayDown, Timeout }", "raises { OrderNotFound, GatewayDown }",
     "EF107: undeclared failures: Timeout"),
    ('        .catch<Timeout>("gateway timed out")\n', "",
     "EF107: undeclared failures: Timeout"),
    ('    Gateway = FakeGateway("auth-7")\n', "",
     "EF108: missing service requirements: Gateway"),
    ("                id,\n                total: 1999\n", "                id\n",
     "EF114: missing payload field total"),
    ("        Payment.Authorized {\n            authId\n        }",
     "        Payment.Authorized {\n            auth: authId\n        }",
     "EF114: unknown payload field auth"),
]
with tempfile.TemporaryDirectory() as scratch:
    for index, (before, after, quoted) in enumerate(mistakes):
        assert showcase.count(before) == 1, before
        mutated = pathlib.Path(scratch) / f"mistake{index}.ef"
        mutated.write_text(showcase.replace(before, after))
        output = run("diagnostics", str(mutated), success=False).stdout
        assert f"error {quoted}" in output, (quoted, output)
        assert f"`{quoted}`" in readme, f"README does not quote {quoted}"
print("readme smoke ok", counts)
