#!/usr/bin/env python3
"""Actual selected lexical/type CLI and MCP workflows over shared snapshots."""
import json
import pathlib
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[1]
ef = root / "bin" / "ef"
source = '''import Fns "effra/functions"
enum Notice { Named { value: string } }
fn local(input: string) -> string { let label = input; label }
fn describe(label: string, notice: Notice) -> string {
 let rendered = match notice { Notice.Named { value: label } => label }
 rendered + label
}
service Labels { effect fn read(item: string) -> string }
impl Prefix(prefix: string) for Labels { effect fn read(item: string) -> string { prefix + item } }
fn unrelated(input: string) -> string { Fns.identity(input) }
'''


def cli(path, target, *selector, success=True):
    result = subprocess.run([str(ef), "type", str(path), "--target", target, *selector],
                            cwd=root, text=True, capture_output=True, timeout=30)
    assert (result.returncode == 0) == success, (selector, result.stdout, result.stderr)
    if success:
        return json.loads(result.stdout)
    assert not result.stdout


def tool(identifier, arguments):
    return {"jsonrpc": "2.0", "id": identifier, "method": "tools/call",
            "params": {"name": "code.type", "arguments": arguments}}


def closure(value):
    types = {node["id"] for node in value["types"]}
    rows = {node["id"] for node in value["rows"]}
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
                visit(child)
    visit(value)


def producer_snapshot(value, target):
    producer = value["producer"]
    snapshot = value["snapshot"]
    assert producer["reuseScope"] in ("artifact", "process", "none")
    if producer["reuseScope"] == "none":
        assert not producer["qualifier"]
    else:
        assert producer["qualifier"]
    assert snapshot == {"schemaVersion": value["schemaVersion"], "revision": value["revision"],
                       "target": target, "producer": producer["qualifier"],
                       "reuseScope": producer["reuseScope"]}
    return producer


def assert_process_parity(actual, expected, reuse_scope):
    if reuse_scope == "artifact":
        assert actual == expected
        return
    # An explicitly process-scoped fallback cannot be reused across adapters.
    # Compare semantic facts while retaining the source, target and schema guard.
    actual = dict(actual)
    expected = dict(expected)
    actual.pop("producer", None)
    expected.pop("producer", None)
    for value in (actual, expected):
        snapshot = dict(value["snapshot"])
        snapshot.pop("producer", None)
        snapshot.pop("reuseScope", None)
        value["snapshot"] = snapshot
    assert actual == expected


with tempfile.TemporaryDirectory(prefix="effra-type-") as directory:
    workspace = pathlib.Path(directory)
    path = workspace / "facts.ef"
    path.write_text(source)
    invalid = workspace / "invalid.ef"
    invalid.write_text('fn invalid() -> string { missing }')
    selectors = [
        ("let", source.index("let label") + len("let ")),
        ("use", source.index("; label") + len("; ")),
        ("alias", source.index("value: label") + len("value: ")),
        ("alias-use", source.index("=> label") + len("=> ")),
        ("outer-use", source.index("rendered + label") + len("rendered + ")),
        ("config", source.index("Prefix(prefix") + len("Prefix(")),
        ("config-use", source.index("{ prefix +") + len("{ ")),
        ("method-use", source.index("prefix + item") + len("prefix + ")),
    ]
    for target in ("go", "js"):
        views = {name: cli(path, target, "--offset", str(offset)) for name, offset in selectors}
        artifact = producer_snapshot(views["use"], target)
        for view in views.values():
            assert view["checked"] and view["typeProjectionComplete"] and view["querySchemaVersion"] == 1
            assert view["producerIdentity"] and view["sources"] and view["bundledInterfaces"]
            assert view["selection"]["locationAvailable"]
            current = producer_snapshot(view, target)
            if artifact["reuseScope"] == "artifact":
                assert current == artifact
            closure(view)
        binding = lambda name: views[name]["selection"]["binding"]
        assert binding("let")["id"] == binding("use")["id"]
        assert binding("alias")["id"] == binding("alias-use")["id"] != binding("outer-use")["id"]
        assert binding("alias")["declarationSpan"]["offset"] == selectors[2][1]
        assert binding("config")["id"] == binding("config-use")["id"]
        assert binding("method-use")["kind"] == "parameter"
        unrelated = cli(path, target, "--symbol", "unrelated")
        nominal = cli(path, target, "--symbol", "Notice")
        assert unrelated["selection"]["kind"] == nominal["selection"]["kind"] == "declaration"
        assert nominal["selection"]["declaration"]["variants"][0]["fields"][0]["type"] == "string"
        type_id = views["use"]["selection"]["expression"]["type"]["type"]["ref"]
        revision = views["use"]["revision"]
        definition = cli(path, target, "--definition", type_id, "--revision", revision)
        definition_producer = producer_snapshot(definition, target)
        if artifact["reuseScope"] == "artifact":
            assert definition_producer == artifact
        assert not definition["selection"]["locationAvailable"]
        closure(definition)
        cli(path, target, "--definition", type_id, success=False)
        cli(path, target, "--definition", type_id, "--revision", "stale", success=False)
        cli(path, target, "--offset", str(len(source)), success=False)
        cli(invalid, target, "--symbol", "invalid", success=False)
        requests = [{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                        "protocolVersion": "2025-11-25", "capabilities": {},
                        "clientInfo": {"name": "type-smoke", "version": "1"}}},
                    {"jsonrpc": "2.0", "method": "notifications/initialized"}]
        producer_guard = {"expectedProducer": artifact["qualifier"]} if artifact["reuseScope"] == "artifact" else {}
        for index, (name, offset) in enumerate(selectors, 2):
            requests.append(tool(index, {"file": path.name, "target": target, "offset": offset,
                                         "expectedRevision": revision, **producer_guard}))
        requests.extend([
            tool(20, {"file": path.name, "target": target, "symbol": "unrelated", **producer_guard}),
            tool(21, {"file": path.name, "target": target, "definition": type_id,
                      "expectedRevision": revision, **producer_guard}),
            tool(22, {"file": path.name, "target": target, "offset": selectors[0][1],
                      "expectedRevision": "stale", **producer_guard}),
            tool(23, {"file": invalid.name, "target": target, "symbol": "invalid"}),
            tool(24, {"file": path.name, "target": target, "offset": selectors[0][1], "symbol": "local"}),
        ])
        if artifact["reuseScope"] == "artifact":
            stale_qualifier = "sha256:" + "0" * 64
            if stale_qualifier == artifact["qualifier"]:
                stale_qualifier = "sha256:" + "1" * 64
            requests.append(tool(25, {"file": path.name, "target": target, "symbol": "unrelated",
                                      "expectedRevision": revision, "expectedProducer": stale_qualifier}))
            requests.append({"jsonrpc": "2.0", "id": 26, "method": "ping"})
        else:
            requests.append({"jsonrpc": "2.0", "id": 25, "method": "ping"})
        process = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, text=True, capture_output=True,
                                 input="".join(json.dumps(request)+"\n" for request in requests), timeout=60)
        assert process.returncode == 0, process.stderr
        replies = {reply["id"]: reply for reply in map(json.loads, process.stdout.splitlines())}
        for index, (name, _) in enumerate(selectors, 2):
            assert "result" in replies[index], replies[index]
            assert_process_parity(replies[index]["result"]["structuredContent"], views[name], artifact["reuseScope"])
        assert_process_parity(replies[20]["result"]["structuredContent"], unrelated, artifact["reuseScope"])
        assert_process_parity(replies[21]["result"]["structuredContent"], definition, artifact["reuseScope"])
        assert replies[22]["result"]["isError"] and replies[23]["result"]["isError"]
        assert replies[24]["error"]["code"] == -32602
        if artifact["reuseScope"] == "artifact":
            assert replies[25]["result"]["isError"] and "stale producer" in replies[25]["result"]["content"][0]["text"]
            assert replies[26]["result"] == {}
        else:
            assert replies[25]["result"] == {}
print("selected lexical declarations, shadowing, canonical definitions and CLI/MCP parity: passed")
