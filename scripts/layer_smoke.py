#!/usr/bin/env python3
"""Exercise checked layer provision through the actual CLI and MCP processes."""
import json
import pathlib
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[1]
ef = root / "bin" / "ef"
source = '''import Fns "effra/functions"
service Store { effect fn label() -> string }
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
effect fn labels(input: string) -> string uses {Account,Invoice} {
 let account=run Account.label()
 let invoice=run Invoice.label()
 account+":"+invoice
}
effect fn main() -> string { run Fns.call(labels,"input").provide(Fixture) }
'''


def cli(*args, success=True, input_text=None):
    result = subprocess.run([str(ef), *args], cwd=root, text=True,
                            capture_output=True, input=input_text, timeout=30)
    assert (result.returncode == 0) == success, (args, result.stdout, result.stderr)
    return result


def tool(identifier, name, arguments):
    return {"jsonrpc": "2.0", "id": identifier, "method": "tools/call",
            "params": {"name": name, "arguments": arguments}}


def semantic(value):
    result = {key: item for key, item in value.items() if key not in ("file", "timings")}
    if "typeProjectionUsage" in result:
        result["typeProjectionUsage"] = {key: item for key, item in result["typeProjectionUsage"].items()
                                         if key not in ("compatibilityBytes", "nameBytes", "responseBytes")}
    return result


def complete_references(value):
    types = {item["id"] for item in value["types"]}
    rows = {item["id"] for item in value["rows"]}
    def visit(item):
        if isinstance(item, list):
            for child in item:
                visit(child)
        elif isinstance(item, dict):
            for key, child in item.items():
                if key in ("ref", "result") and isinstance(child, str) and child:
                    assert child in types, (key, child)
                if key in ("failureRow", "serviceRow") and child:
                    assert child in rows, (key, child)
                if key == "args":
                    for ref in child:
                        if isinstance(ref, str):
                            assert ref in types, ref
                visit(child)
    visit(value)
    if "edges" in value:
        nodes = {item["id"] for item in value["nodes"]}
        assert all(edge["from"] in nodes and edge["to"] in nodes for edge in value["edges"])


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
    unprovided_path = workspace / "unprovided.ef"
    unprovided_path.write_text(formatted.replace(".provide(Fixture)", ""))
    offset = formatted.index(".provide(Fixture)") + 1
    helper_offset = formatted.index("Fns.call") + len("Fns.")
    for target in ("go", "js"):
        checked = json.loads(cli("check", str(path), "--target", target).stdout)
        assert checked["checked"] and len(checked["layers"]) == 5
        inspected = json.loads(cli("inspect", str(path), "Fixture", "--target", target).stdout)
        explained = json.loads(cli("explain", str(path), "Fixture", "--target", target).stdout)
        assert inspected == explained
        plan = inspected["layer"]
        assert plan["provides"] == ["Account", "Invoice"] and not plan["requirements"]
        store = next(node for node in plan["nodes"] if node["service"] == "Store")
        assert not store["public"] and len(store["incoming"]) == 2 and len(store["replacements"]) == 1
        assert len(store["configurationParameters"]) == len(store["configurationArguments"]) == 1
        workflow = json.loads(cli("inspect", str(workflow_path), "Workflow", "--target", target).stdout)
        complete_references(workflow)
        delivery = next(node for node in workflow["layer"]["nodes"] if node["service"] == "Delivery")
        assert [argument["type"]["type"]["kind"] for argument in delivery["configurationArguments"]] == ["record", "enum"]
        graph = json.loads(cli("graph", str(path), "--target", target).stdout)
        assert next(layer for layer in graph["layers"] if layer["name"] == "Fixture") == plan
        assert any(edge["kind"] == "provides-layer" and edge["to"] == plan["id"] for edge in graph["edges"])
        queried = json.loads(cli("query", str(path), str(offset), "--target", target).stdout)
        helper = json.loads(cli("query", str(path), str(helper_offset), "--target", target).stdout)
        caller = json.loads(cli("inspect", str(path), "main", "--target", target).stdout)
        assert queried["expression"]["type"]["effect"]
        for view in (checked, inspected, graph, queried, helper, caller):
            assert view["producerIdentity"] and view["sources"]
            assert view["bundledBindings"] and view["bundledInterfaces"]
            assert view["revision"] == checked["revision"] and view["target"] == target
            assert view["typeProjectionComplete"]
            complete_references(view)
        assert cli("run", str(path), "--target", target).stdout == "fixture:fixture\n"
        assert cli("run", "examples/layers-workflow.ef", "--target", target).stdout == "queued:Ada|denied\n"
        invalid = json.loads(cli("check", str(invalid_path), "--target", target, success=False).stdout)
        missing = json.loads(cli("check", str(open_path), "--target", target, success=False).stdout)
        assert any(d["code"] == "EF108" and d["related"] for d in missing["diagnostics"])
        unprovided = json.loads(cli("check", str(unprovided_path), "--target", target, success=False).stdout)
        assert any(d["code"] == "EF108" and "Account" in d["message"] and "Invoice" in d["message"] for d in unprovided["diagnostics"])
        identity = json.loads(cli("check", str(identity_path), "--target", target, success=False).stdout)
        assert not identity["checked"] and any(d["code"] == "EF133" for d in identity["diagnostics"])
        refused_run = cli("run", str(identity_path), "--target", target, success=False)
        assert "EF133" in refused_run.stdout and "defect: invalid layer plan" not in refused_run.stderr
        duplicate = next(d for d in invalid["diagnostics"] if d["code"] == "EF130")
        assert duplicate["related"] and duplicate["span"]["length"] > 0
        assert cli("inspect", str(invalid_path), "App", "--target", target, success=False).stderr
        assert cli("inspect", str(path), "Absent", "--target", target, success=False).stderr
        refused = json.loads(cli("check", str(budget_path), "--target", target, success=False).stdout)
        assert not refused["checked"] and any(d["code"] == "EF133" for d in refused["diagnostics"])
        assert "layers" not in refused and not refused["typeProjectionComplete"]
        requests = [
            {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                "protocolVersion": "2025-11-25", "capabilities": {},
                "clientInfo": {"name": "layer-smoke", "version": "1"}}},
            {"jsonrpc": "2.0", "method": "notifications/initialized"},
            tool(2, "code.inspect", {"file": "invalid.ef", "symbol": "App", "target": target}),
            tool(3, "code.inspect", {"file": "app.ef", "symbol": "Absent", "target": target}),
            tool(8, "code.inspect", {"file": "budget.ef", "symbol": "App", "target": target}),
            {"jsonrpc": "2.0", "id": 4, "method": "ping"},
            tool(7, "code.format", {"source": formatted}),
            tool(10, "code.inspect", {"file": "workflow.ef", "symbol": "Workflow", "target": target}),
            tool(11, "project.check", {"file": "open.ef", "target": target}),
            tool(12, "project.check", {"file": "identity.ef", "target": target}),
            tool(13, "project.check", {"file": "unprovided.ef", "target": target}),
            tool(14, "code.typeAt", {"file": "app.ef", "offset": helper_offset, "target": target, "expectedRevision": "stale"}),
        ]
        comparisons = [(5, "code.inspect", {"symbol": "Fixture"}, inspected),
                       (6, "project.graph", {}, graph),
                       (9, "code.typeAt", {"offset": offset}, queried),
                       (15, "code.typeAt", {"offset": helper_offset}, helper),
                       (16, "code.inspect", {"symbol": "main"}, caller),
                       (17, "project.check", {}, checked)]
        for identifier, name, arguments, expected in comparisons:
            requests.append(tool(identifier, name, {"file": "app.ef", "target": target,
                                "expectedRevision": checked["revision"], **arguments}))
        server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root,
                                input="\n".join(map(json.dumps, requests)) + "\n",
                                text=True, capture_output=True, timeout=30)
        assert server.returncode == 0, server.stderr
        responses = {response["id"]: response for response in map(json.loads, server.stdout.splitlines())}
        assert all(responses[identifier]["result"]["isError"] for identifier in (2, 3, 8, 14))
        assert responses[4]["result"] == {}
        assert not responses[7]["result"].get("isError"), responses[7]
        for identifier, name, arguments, expected in comparisons:
            actual = responses[identifier]["result"]["structuredContent"]
            assert semantic(actual) == semantic(expected), (target, name, actual, expected)
            complete_references(actual)
        assert semantic(responses[10]["result"]["structuredContent"]) == semantic(workflow)
        complete_references(responses[10]["result"]["structuredContent"])
        for identifier, expected in ((11, missing), (12, identity), (13, unprovided)):
            assert semantic(responses[identifier]["result"]["structuredContent"]) == semantic(expected)
print("layer provision Go/JS, CLI/MCP metadata and formatter controls passed")
