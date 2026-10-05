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
run("build", "examples/main.ef")
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
messages = [
    {"jsonrpc":"2.0", "id":1, "method":"initialize", "params":{"protocolVersion":"2025-11-25", "capabilities":{}, "clientInfo":{"name":"smoke", "version":"1"}}},
    {"jsonrpc":"2.0", "method":"notifications/initialized"},
    {"jsonrpc":"2.0", "id":2, "method":"tools/list"},
    {"jsonrpc":"2.0", "id":3, "method":"tools/call", "params":{"name":"code.inspect", "arguments":{"file":"examples/main.ef", "symbol":"greeting"}}},
    {"jsonrpc":"2.0", "id":4, "method":"tools/call", "params":{"name":"project.check", "arguments":{"file":"examples/main.ef", "expectedRevision":"old"}}},
]
server = subprocess.run([ef, "mcp", str(root)], input="\n".join(map(json.dumps, messages))+"\n", text=True, capture_output=True)
assert server.returncode == 0, server.stderr
responses = [json.loads(line) for line in server.stdout.splitlines()]
assert len(responses) == 4
assert len(responses[1]["result"]["tools"]) == 4
mcp = responses[2]["result"]["structuredContent"]
assert mcp["symbol"] == inspected["symbol"] and mcp["revision"] == inspected["revision"]
assert responses[3]["result"]["isError"]
print("public CLI, JS module, and stdio MCP: passed")
