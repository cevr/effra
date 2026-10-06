#!/usr/bin/env python3
"""Exercise static layer inspection through the actual CLI and MCP processes."""
import json
import pathlib
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[1]
ef = root / "bin" / "ef"
source = '''service Store { effect fn label() -> string }
service Account { effect fn label() -> string }
service Invoice { effect fn label() -> string }
impl Memory(label: string) for Store { effect fn label() -> string { label } }
impl AccountLive for Account uses {Store} { effect fn label() -> string { run Store.label() } }
impl InvoiceLive for Invoice uses {Store} { effect fn label() -> string { run Store.label() } }
layer Shared { Store = Memory("live") }
layer Accounts { merge Shared; Account = AccountLive }
layer Invoices { merge Shared; Invoice = InvoiceLive }
layer App provides {Account, Invoice} { merge Accounts, Invoices }
layer Fixture { merge App; replace Store = Memory("fixture") }
'''


def cli(*args, success=True, input_text=None):
    result = subprocess.run([str(ef), *args], cwd=root, text=True,
                            capture_output=True, input=input_text, timeout=30)
    assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
    return result


def tool(identifier, name, arguments):
    return {"jsonrpc": "2.0", "id": identifier, "method": "tools/call",
            "params": {"name": name, "arguments": arguments}}


with tempfile.TemporaryDirectory(prefix="effra-layers-") as directory:
    workspace = pathlib.Path(directory)
    path = workspace / "app.ef"
    invalid_path = workspace / "invalid.ef"
    path.write_text(source)
    invalid_path.write_text(source + 'layer Bad { Store = Memory("other"); merge Shared }')
    formatted = cli("fmt", "--stdin", input_text=source).stdout
    assert cli("fmt", "--stdin", input_text=formatted).stdout == formatted
    path.write_text(formatted)
    checked = json.loads(cli("check", str(path)).stdout)
    assert checked["checked"] and len(checked["layers"]) == 5
    inspected = json.loads(cli("inspect", str(path), "Fixture").stdout)
    explained = json.loads(cli("explain", str(path), "Fixture").stdout)
    assert inspected == explained
    plan = inspected["layer"]
    assert plan["provides"] == ["Account", "Invoice"] and not plan["requirements"]
    store = next(node for node in plan["nodes"] if node["service"] == "Store")
    assert not store["public"] and len(store["incoming"]) == 2 and len(store["replacements"]) == 1
    graph = json.loads(cli("graph", str(path)).stdout)
    assert next(layer for layer in graph["layers"] if layer["name"] == "Fixture") == plan
    invalid = json.loads(cli("check", str(invalid_path), success=False).stdout)
    duplicate = next(d for d in invalid["diagnostics"] if d["code"] == "EF130")
    assert duplicate["related"] and duplicate["span"]["length"] > 0
    assert cli("inspect", str(invalid_path), "App", success=False).stderr
    assert cli("inspect", str(path), "Absent", success=False).stderr
    assert json.loads(cli("inspect", str(path), "Fixture").stdout)["layer"] == plan
    requests = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
            "protocolVersion": "2025-11-25", "capabilities": {},
            "clientInfo": {"name": "layer-smoke", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        tool(2, "code.inspect", {"file": "invalid.ef", "symbol": "App"}),
        tool(3, "code.inspect", {"file": "app.ef", "symbol": "Absent"}),
        {"jsonrpc": "2.0", "id": 4, "method": "ping"},
        tool(5, "code.inspect", {"file": "app.ef", "symbol": "Fixture"}),
        tool(6, "project.graph", {"file": "app.ef"}),
        tool(7, "code.format", {"source": formatted}),
    ]
    server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root,
                            input="\n".join(map(json.dumps, requests)) + "\n",
                            text=True, capture_output=True, timeout=30)
    assert server.returncode == 0, server.stderr
    responses = {response["id"]: response for response in map(json.loads, server.stdout.splitlines())}
    assert responses[2]["result"]["isError"] and responses[3]["result"]["isError"]
    assert responses[4]["result"] == {}
    mcp = responses[5]["result"]["structuredContent"]
    assert mcp["layer"] == plan and mcp["revision"] == inspected["revision"]
    assert mcp["types"] == inspected["types"] and mcp["rows"] == inspected["rows"]
    assert responses[6]["result"]["structuredContent"]["layers"] == graph["layers"]
    assert not responses[7]["result"].get("isError"), responses[7]

print("static layer CLI/MCP and formatter controls passed")
