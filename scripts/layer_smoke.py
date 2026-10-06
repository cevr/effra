#!/usr/bin/env python3
"""Exercise checked layer provision through the actual CLI and MCP processes."""
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
effect fn labels() -> string uses {Account,Invoice} {
 let account=run Account.label()
 let invoice=run Invoice.label()
 account+":"+invoice
}
effect fn main() -> string { run labels().provide(Fixture) }
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
    budget_path = workspace / "budget.ef"
    open_path = workspace / "open.ef"
    workflow_path = workspace / "workflow.ef"
    identity_path = workspace / "identity.ef"
    path.write_text(source)
    invalid_path.write_text(source + 'layer Bad { Store = Memory("other"); merge Shared }')
    open_path.write_text('service Store { effect fn label() -> string } service Account { effect fn label() -> string } impl AccountLive for Account uses {Store} { effect fn label() -> string { run Store.label() } } layer Open { Account=AccountLive } effect fn main() -> string { run Account.label().provide(Open) }')
    workflow_path.write_text((root / "examples/layers-workflow.ef").read_text())
    long_name = "App" + "x" * 5000
    identity_source = "\n".join(f'service S{i} {{ effect fn value() -> string }} impl P{i} for S{i} {{ effect fn value() -> string {{ "ok" }} }}' for i in range(20))
    identity_source += f"\nlayer {long_name} {{\n" + "\n".join(f"S{i}=P{i}" for i in range(20)) + f"\n}}\neffect fn main() -> string {{ run S0.value().provide({long_name}) }}\n"
    identity_path.write_text(identity_source)
    budget_source = "\n".join(
        f'service S{i} {{ effect fn value() -> string }} impl P{i} for S{i} {{ effect fn value() -> string {{ "value" }} }}'
        for i in range(50)
    )
    budget_source += "\nlayer Shared {\n" + "\n".join(f"S{i} = P{i}" for i in range(50)) + "\n}\n"
    budget_source += "layer App {\n" + "merge Shared\n" * 1000 + "}\n"
    budget_path.write_text(budget_source)
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
    assert len(store["configurationParameters"]) == len(store["configurationArguments"]) == 1
    workflow = json.loads(cli("inspect", str(workflow_path), "Workflow").stdout)
    delivery = next(node for node in workflow["layer"]["nodes"] if node["service"] == "Delivery")
    assert [argument["type"]["type"]["kind"] for argument in delivery["configurationArguments"]] == ["record", "enum"]
    graph = json.loads(cli("graph", str(path)).stdout)
    assert next(layer for layer in graph["layers"] if layer["name"] == "Fixture") == plan
    assert any(edge["kind"] == "provides-layer" and edge["to"] == plan["id"] for edge in graph["edges"])
    offset = formatted.index("labels().provide") + len("labels().")
    queried = json.loads(cli("query", str(path), str(offset)).stdout)
    assert queried["expression"]["type"]["effect"]
    for target in ("go", "js"):
        assert cli("run", str(path), "--target", target).stdout == "fixture:fixture\n"
        assert cli("run", "examples/layers-workflow.ef", "--target", target).stdout == "queued:Ada|denied\n"
    invalid = json.loads(cli("check", str(invalid_path), success=False).stdout)
    missing = json.loads(cli("check", str(open_path), success=False).stdout)
    assert any(d["code"] == "EF108" and d["related"] for d in missing["diagnostics"])
    for target in ("go", "js"):
        identity = json.loads(cli("check", str(identity_path), "--target", target, success=False).stdout)
        assert not identity["checked"] and any(d["code"] == "EF133" for d in identity["diagnostics"])
        refused_run = cli("run", str(identity_path), "--target", target, success=False)
        assert "EF133" in refused_run.stdout and "defect: invalid layer plan" not in refused_run.stderr
    duplicate = next(d for d in invalid["diagnostics"] if d["code"] == "EF130")
    assert duplicate["related"] and duplicate["span"]["length"] > 0
    assert cli("inspect", str(invalid_path), "App", success=False).stderr
    assert cli("inspect", str(path), "Absent", success=False).stderr
    refused = json.loads(cli("check", str(budget_path), success=False).stdout)
    assert not refused["checked"] and any(d["code"] == "EF133" for d in refused["diagnostics"])
    assert "layers" not in refused and not refused["typeProjectionComplete"]
    assert json.loads(cli("inspect", str(path), "Fixture").stdout)["layer"] == plan
    requests = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
            "protocolVersion": "2025-11-25", "capabilities": {},
            "clientInfo": {"name": "layer-smoke", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        tool(2, "code.inspect", {"file": "invalid.ef", "symbol": "App"}),
        tool(3, "code.inspect", {"file": "app.ef", "symbol": "Absent"}),
        tool(8, "code.inspect", {"file": "budget.ef", "symbol": "App"}),
        {"jsonrpc": "2.0", "id": 4, "method": "ping"},
        tool(5, "code.inspect", {"file": "app.ef", "symbol": "Fixture"}),
        tool(6, "project.graph", {"file": "app.ef"}),
        tool(7, "code.format", {"source": formatted}),
        tool(9, "code.typeAt", {"file": "app.ef", "offset": offset, "expectedRevision": checked["revision"]}),
        tool(10, "code.inspect", {"file": "workflow.ef", "symbol": "Workflow"}),
        tool(11, "project.check", {"file": "open.ef"}),
        tool(12, "project.check", {"file": "identity.ef"}),
    ]
    server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root,
                            input="\n".join(map(json.dumps, requests)) + "\n",
                            text=True, capture_output=True, timeout=30)
    assert server.returncode == 0, server.stderr
    responses = {response["id"]: response for response in map(json.loads, server.stdout.splitlines())}
    assert responses[2]["result"]["isError"] and responses[3]["result"]["isError"]
    assert responses[8]["result"]["isError"]
    assert responses[4]["result"] == {}
    mcp = responses[5]["result"]["structuredContent"]
    assert mcp["layer"] == plan and mcp["revision"] == inspected["revision"]
    assert mcp["types"] == inspected["types"] and mcp["rows"] == inspected["rows"]
    assert responses[6]["result"]["structuredContent"]["layers"] == graph["layers"]
    assert not responses[7]["result"].get("isError"), responses[7]
    assert responses[9]["result"]["structuredContent"]["expression"] == queried["expression"]
    assert responses[10]["result"]["structuredContent"]["layer"] == workflow["layer"]
    assert responses[11]["result"]["structuredContent"]["diagnostics"] == missing["diagnostics"]
    assert not responses[12]["result"]["structuredContent"]["checked"]
    assert any(d["code"] == "EF133" for d in responses[12]["result"]["structuredContent"]["diagnostics"])

print("layer provision Go/JS, CLI/MCP metadata and formatter controls passed")
