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
for target in ("go", "js"):
    assert run("run", "examples/data.ef", "--target", target).stdout == "running 42\n"
data_inspection = json.loads(run("inspect", "examples/data.ef", "State").stdout)
assert data_inspection["declaration"]["kind"] == "enum"
assert data_inspection["declaration"]["variants"][1]["fields"][0]["type"] == "string"
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
    {"jsonrpc":"2.0", "id":8, "method":"tools/call", "params":{"name":"code.inspect", "arguments":{"file":"examples/data.ef", "symbol":"State"}}},
    {"jsonrpc":"2.0", "id":4, "method":"tools/call", "params":{"name":"project.check", "arguments":{"file":"examples/main.ef", "expectedRevision":"old"}}},
    {"jsonrpc":"2.0", "id":5, "method":"tools/call", "params":{"name":"code.inspect", "arguments":{"file":"examples/imports.ef", "symbol":"main"}}},
    {"jsonrpc":"2.0", "id":6, "method":"tools/call", "params":{"name":"project.describe", "arguments":{}}},
    {"jsonrpc":"2.0", "id":7, "method":"tools/call", "params":{"name":"project.tests", "arguments":{"file":"examples/testing.ef"}}},
]
server = subprocess.run([ef, "mcp", str(root)], input="\n".join(map(json.dumps, messages))+"\n", text=True, capture_output=True)
assert server.returncode == 0, server.stderr
responses = [json.loads(line) for line in server.stdout.splitlines()]
assert len(responses) == 8
assert len(responses[1]["result"]["tools"]) == 9
mcp = responses[2]["result"]["structuredContent"]
assert mcp["symbol"] == inspected["symbol"] and mcp["revision"] == inspected["revision"]
data_mcp = responses[3]["result"]["structuredContent"]
assert data_mcp["declaration"]["name"] == "State" and data_mcp["declaration"]["kind"] == "enum"
assert responses[4]["result"]["isError"]
assert responses[5]["result"]["structuredContent"]["bindings"] == bindings["bindings"]
assert responses[5]["result"]["structuredContent"]["revision"] == bindings["revision"]
assert "Go imports" in responses[6]["result"]["structuredContent"]["guardrails"]["targetCapabilities"]
catalog=responses[7]["result"]["structuredContent"]
assert len(catalog["tests"])==3 and not catalog["liveRequired"]
for target in ("go","js"):
    suite=json.loads(run("test","examples/testing.ef","--target",target).stdout)
    assert suite["passed"] and len(suite["tests"])==3 and not suite["watchdogExpired"]
with tempfile.TemporaryDirectory(prefix="effra-tests-") as tmp:
    file=pathlib.Path(tmp)/"cases.ef"
    file.write_text('effect fn test_bad() -> () throws {AssertionFailed} uses {Assert} {run Assert.equalText("actual","expected")} effect fn test_after() -> () throws {AssertionFailed} uses {Assert} {run Assert.check(true,"ok")}')
    for target in ("go","js"):
        suite=json.loads(run("test",str(file),"--target",target,success=False).stdout)
        assert not suite["passed"] and len(suite["tests"])==2
        assert suite["tests"][0]["reasons"][0]["tag"]=="AssertionFailed" and suite["tests"][1]["passed"]
        assert 'expected "expected"; received "actual"' in suite["tests"][0]["reasons"][0]["message"]
    # A real watchdog is separate from program time and must disclaim cleanup.
    file.write_text('effect fn test_slow() -> () {run Clock.sleep(10000).provide<Clock>(LiveClock)}')
    for target in ("go","js"):
        assert "--live" in run("test",str(file),"--target",target,success=False).stderr
        suite=json.loads(run("test",str(file),"--target",target,"--live","--timeout-ms","100",success=False).stdout)
        assert suite["watchdogExpired"] and not suite["cleanupCompleted"]
rules=json.loads(run("lint","rules").stdout)
assert {r["name"] for r in rules} == {"unused-recipe","redundant-provision","unused-go-import","invalid-suppression"}
graph=json.loads(run("graph","examples/workflow.ef").stdout)
assert any(e["kind"]=="requires" and e["from"]=="function:welcome" and e["service"]=="Directory" for e in graph["edges"])
with tempfile.TemporaryDirectory(prefix="effra-tooling-") as tmp:
    source="effect fn task() -> string { \"ok\" } effect fn main() -> () { let forgotten = task(); () }"
    file=pathlib.Path(tmp)/"main.ef"
    file.write_text(source)
    lint=json.loads(run("lint",str(file),"--strict",success=False).stdout)
    assert lint["checked"] and not lint["lintPassed"] and lint["warnings"]==1
    assert json.loads(run("lint",str(file)).stdout)["lintPassed"]
    offset=source.index("task();")
    query=json.loads(run("query",str(file),str(offset)).stdout)
    assert query["expression"]["type"]["effect"] and query["expression"]["type"]["success"]=="string"
    calls=messages[:2]+[
        {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.lint","arguments":{"file":"main.ef","strict":True}}},
        {"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"code.typeAt","arguments":{"file":"main.ef","offset":offset,"expectedRevision":lint["revision"]}}},
        {"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"project.graph","arguments":{"file":"main.ef"}}},
        {"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"lint.rules","arguments":{}}},
        {"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"code.typeAt","arguments":{"file":"main.ef","offset":1.5}}},
    ]
    server=subprocess.run([ef,"mcp",tmp],input="\n".join(map(json.dumps,calls))+"\n",text=True,capture_output=True)
    assert server.returncode==0,server.stderr
    replies=[json.loads(line) for line in server.stdout.splitlines()]
    assert replies[1]["result"]["structuredContent"]["lint"]==lint
    assert replies[2]["result"]["structuredContent"]["expression"]==query["expression"]
    assert replies[3]["result"]["structuredContent"]["nodes"]
    assert replies[4]["result"]["structuredContent"]==rules
    assert replies[5]["error"]["code"]==-32602

    source="""effect fn task() -> string { \"ok\" }
effect fn main() -> () {
// effra-lint-disable-next-line unused-recipe -- intentional deferred hook
let forgotten = task();
()}"""
    file.write_text(source)
    suppressed=json.loads(run("lint",str(file),"--strict").stdout)
    assert suppressed["lintPassed"] and suppressed["errors"]==0 and suppressed["lintDiagnostics"]==[]
    suppression_calls=messages[:2]+[
        {"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"project.lint","arguments":{"file":"main.ef","strict":True}}},
    ]
    server=subprocess.run([ef,"mcp",tmp],input="\n".join(map(json.dumps,suppression_calls))+"\n",text=True,capture_output=True)
    assert server.returncode==0,server.stderr
    suppression_reply=[json.loads(line) for line in server.stdout.splitlines()]
    assert suppression_reply[1]["result"]["structuredContent"]["lint"]==suppressed
print("native Go executable, JS module, CLI lint/query/graph and stdio MCP: passed")
