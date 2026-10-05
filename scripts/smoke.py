#!/usr/bin/env python3
"""Exercise the public binary and stdio MCP, not a second compiler model."""
import json, pathlib, subprocess, tempfile
root = pathlib.Path(__file__).resolve().parents[1]
ef = str(root / "bin/ef")
def run(*args, success=True):
    result = subprocess.run([ef, *args], cwd=root, text=True, capture_output=True)
    assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
    return result
checked = json.loads(run("check", "examples/main.ef").stdout)
assert checked["checked"]
inspected = json.loads(run("inspect", "examples/main.ef", "greeting").stdout)
assert inspected["symbol"]["contract"]["requirements"] == ["Users"]
assert inspected["symbol"]["contract"]["failures"] == ["NotFound"]
invalid = json.loads(run("check", "examples/missing-service.ef", success=False).stdout)
assert any(d["code"] == "EF108" for d in invalid["diagnostics"])
assert run("run", "examples/main.ef").stdout == "Hello, Ada\nUnknown user\n"
assert run("run", "examples/main.ef", "--target", "js").stdout == "Hello, Ada\nUnknown user\n"
assert run("run", "examples/imports.ef").stdout == "ADA\npartial result retained\ntimed out\n"
for target in ("go", "js"):
    assert run("run", "examples/concurrency.ef", "--target", target).stdout == "child joined\ntimed out\nrecovered\n"
for example, expected in (("workflow", "queued: Welcome, Ada\naccess denied\n"), ("latest-task", "result: new\n")):
    for target in ("go", "js"):
        assert run("run", "examples/" + example + ".ef", "--target", target).stdout == expected
workflow = json.loads(run("inspect", "examples/workflow.ef", "welcome").stdout)["symbol"]["contract"]
assert workflow["requirements"] == ["Access", "Delivery", "Directory"]
assert workflow["failures"] == ["DeliveryFailed", "Denied", "UserMissing"]
life = run("run", "examples/lifecycle.ef").stdout
snapshot = json.loads(life.splitlines()[0])
assert snapshot["state"] == "Open" and snapshot["resourceCount"] == 1
assert "scoped file read complete" in life and "label: " in life
js_life = json.loads(run("check", "examples/lifecycle.ef", "--target", "js", success=False).stdout)
assert any(d["code"] == "EF110" for d in js_life["diagnostics"])
native_path=run("build", "examples/main.ef").stdout.strip()
native=subprocess.run([str(root/native_path)], text=True, capture_output=True, cwd="/")
assert native.returncode==0 and native.stdout=="Hello, Ada\nUnknown user\n", native.stderr
run("build", "examples/main.ef", "--target", "js")
# The emitted library must expose service keys for external provision.
consumer = """import { Effect } from 'effect';
import { greeting, Users, MemoryUsers } from './dist/main.mjs';
console.log(await Effect.runPromise(Effect.provideService(greeting('42'), Users, MemoryUsers)));
"""
public = subprocess.run(["bun", "--eval", consumer], cwd=root, text=True, capture_output=True)
assert public.returncode == 0 and public.stdout == "Hello, Ada\n", (public.stdout, public.stderr)
with tempfile.TemporaryDirectory(prefix="effra-cli-") as tmp:
    failure = pathlib.Path(tmp) / "failed.ef"
    failure.write_text("error Bad\neffect fn main() -> string throws {Bad} { fail Bad }\n")
    assert "Bad" in run("run", str(failure), success=False).stderr
bindings = json.loads(run("inspect", "examples/imports.ef", "main").stdout)
assert any(b["forwardContext"] and b["cancellation"] == "cooperative" for b in bindings["bindings"])
messages = [
    {"jsonrpc":"2.0", "id":1, "method":"initialize", "params":{"protocolVersion":"2025-11-25", "capabilities":{}, "clientInfo":{"name":"smoke", "version":"1"}}},
    {"jsonrpc":"2.0", "method":"notifications/initialized"},
    {"jsonrpc":"2.0", "id":2, "method":"tools/list"},
    {"jsonrpc":"2.0", "id":3, "method":"tools/call", "params":{"name":"code.inspect", "arguments":{"file":"examples/main.ef", "symbol":"greeting"}}},
    {"jsonrpc":"2.0", "id":4, "method":"tools/call", "params":{"name":"project.check", "arguments":{"file":"examples/main.ef", "expectedRevision":"old"}}},
    {"jsonrpc":"2.0", "id":5, "method":"tools/call", "params":{"name":"code.inspect", "arguments":{"file":"examples/imports.ef", "symbol":"main"}}},
    {"jsonrpc":"2.0", "id":6, "method":"tools/call", "params":{"name":"project.describe", "arguments":{}}},
]
server = subprocess.run([ef, "mcp", str(root)], input="\n".join(map(json.dumps, messages))+"\n", text=True, capture_output=True)
assert server.returncode == 0, server.stderr
responses = [json.loads(line) for line in server.stdout.splitlines()]
assert len(responses) == 6
assert len(responses[1]["result"]["tools"]) == 4
mcp = responses[2]["result"]["structuredContent"]
assert mcp["symbol"] == inspected["symbol"] and mcp["revision"] == inspected["revision"]
assert responses[3]["result"]["isError"]
assert responses[4]["result"]["structuredContent"]["bindings"] == bindings["bindings"]
assert responses[4]["result"]["structuredContent"]["revision"] == bindings["revision"]
assert "Go imports" in responses[5]["result"]["structuredContent"]["guardrails"]["targetCapabilities"]
print("native Go executable, JS module, CLI, and stdio MCP: passed")
