#!/usr/bin/env python3
"""Actual selected lexical/type CLI and MCP workflows over shared snapshots."""
import copy
import json
import pathlib
import subprocess
import tempfile

from smoke_support import assert_report_parity, producer_snapshot

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
error Child
effect fn task() -> string raises {Child} { "ok" }
effect fn main() -> string raises {Child} {
 let child = fork task()
 run child.join()
}
effect fn fetch(item: string) -> string uses {Labels} { run Labels.read(item) }
effect fn guarded<E: raises>(notice: Notice, cb: effect fn(Notice) -> string raises {E}) -> string raises {E, Child} { "ok" }
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
    def table_ids(name):
        entries = value[name]
        assert isinstance(entries, list), (name, entries)
        identities = []
        for node in entries:
            assert isinstance(node, dict), (name, node)
            identity = node.get("id")
            assert type(identity) is str and identity, (name, identity)
            assert identity not in identities, (name, identity)
            identities.append(identity)
        return set(identities)

    types = table_ids("types")
    rows = table_ids("rows")

    def type_ref(identity, key):
        assert type(identity) is str and identity and identity in types, (key, identity)

    def row_ref(identity, key):
        assert type(identity) is str and identity and identity in rows, (key, identity)

    def visit(item):
        if isinstance(item, list):
            for child in item:
                visit(child)
        elif isinstance(item, dict):
            for key, child in item.items():
                if key == "args":
                    assert isinstance(child, list), (key, child)
                    for ref in child:
                        type_ref(ref, key)
                elif key in ("ref", "signature"):
                    type_ref(child, key)
                elif key == "result":
                    if isinstance(child, dict):
                        assert type(child.get("kind")) is str and child["kind"], (key, child)
                    else:
                        type_ref(child, key)
                elif key in ("failureRow", "serviceRow", "row"):
                    row_ref(child, key)
                visit(child)
    visit(value)

def assert_causal_wire_rejections(value, label):
    closure(value)

    def rejected(mutated, control):
        try:
            closure(mutated)
        except (AssertionError, KeyError, TypeError):
            return
        raise AssertionError(f"{label}: {control} was accepted")

    for malformed in ("", None, 123):
        empty_type = copy.deepcopy(value)
        empty_type["types"].append({"id": malformed, "kind": "primitive"})
        rejected(empty_type, f"malformed type ID {malformed!r}")
        empty_row = copy.deepcopy(value)
        empty_row["rows"].append({"id": malformed})
        rejected(empty_row, f"malformed row ID {malformed!r}")

    fiber = next(node for node in value["types"] if node.get("kind") == "fiber")
    child_type = fiber["args"][0]
    malformed_args = copy.deepcopy(value)
    malformed_args["types"].append({"id": "", "kind": "primitive"})
    malformed_fiber = next(node for node in malformed_args["types"] if node.get("kind") == "fiber")
    malformed_fiber["args"] = [""]
    rejected(malformed_args, "empty args reference")

    for malformed in ("", None, 123):
        malformed_ref = copy.deepcopy(value)
        malformed_ref["selection"]["ref"] = malformed
        rejected(malformed_ref, f"malformed ref {malformed!r}")
        malformed_result = copy.deepcopy(value)
        malformed_result["selection"]["result"] = malformed
        rejected(malformed_result, f"malformed result {malformed!r}")
    for malformed in ("", None):
        malformed_row = copy.deepcopy(value)
        malformed_row["selection"]["failureRow"] = malformed
        rejected(malformed_row, f"malformed failure row {malformed!r}")

    missing_argument = copy.deepcopy(value)
    missing_argument["types"] = [node for node in missing_argument["types"] if node["id"] != child_type]
    try:
        closure(missing_argument)
    except AssertionError:
        pass
    else:
        raise AssertionError(f"{label}: missing args child was accepted")
    duplicate_type = copy.deepcopy(value)
    duplicate_type["types"].append(copy.deepcopy(duplicate_type["types"][0]))
    try:
        closure(duplicate_type)
    except AssertionError:
        pass
    else:
        raise AssertionError(f"{label}: duplicate type was accepted")
    duplicate_row = copy.deepcopy(value)
    duplicate_row["rows"].append(copy.deepcopy(duplicate_row["rows"][0]))
    try:
        closure(duplicate_row)
    except AssertionError:
        pass
    else:
        raise AssertionError(f"{label}: duplicate row was accepted")
    missing_rows = copy.deepcopy(value)
    missing_rows["rows"] = []
    try:
        closure(missing_rows)
    except AssertionError:
        pass
    else:
        raise AssertionError(f"{label}: missing row was accepted")


def assert_snapshot_schema_controls(value, target):
    for malformed in (999, True):
        broken = copy.deepcopy(value)
        broken["snapshot"]["schemaVersion"] = malformed
        try:
            producer_snapshot(broken, target)
        except AssertionError:
            continue
        raise AssertionError(f"snapshot schema {malformed!r} was self-certified")


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
        ("fiber", source.index("run child.join") + len("run ")),
        ("callee", source.index("fork task") + len("fork ")),
        ("operation", source.index("Labels.read(item)") + len("Labels.")),
        ("service", source.index("Labels.read(item)")),
        ("bundled", source.index("Fns.identity") + len("Fns.")),
        ("module-alias", source.index("Fns.identity")),
        ("variant", source.index("Notice.Named {") + len("Notice.")),
        ("annotation", source.index("notice: Notice)") + len("notice: ")),
        ("row-parameter", source.index("raises {E, Child}") + len("raises {")),
        ("row-label", source.index("raises {E, Child}") + len("raises {E, ")),
    ]
    for target in ("go", "js"):
        views = {name: cli(path, target, "--offset", str(offset)) for name, offset in selectors}
        artifact = producer_snapshot(views["use"], target)
        for view in views.values():
            assert view["checked"] and view["typeProjectionComplete"] and view["querySchemaVersion"] == 4
            assert view["producerIdentity"] and view["sources"] and view["bundledInterfaces"]
            assert view["selection"]["locationAvailable"]
            current = producer_snapshot(view, target)
            if artifact["reuseScope"] == "artifact":
                assert current == artifact
            closure(view)
        assert_snapshot_schema_controls(views["use"], target)
        binding = lambda name: views[name]["selection"]["binding"]
        assert binding("let")["id"] == binding("use")["id"]
        assert binding("alias")["id"] == binding("alias-use")["id"] != binding("outer-use")["id"]
        assert binding("alias")["declarationSpan"]["offset"] == selectors[2][1]
        assert binding("config")["id"] == binding("config-use")["id"]
        assert binding("method-use")["kind"] == "parameter"
        # Hover/definition facts: the selected token's declaration target and
        # the one presentation rendered from this response's own tables.
        declared = lambda name: views[name]["selection"]["target"]
        presentation = lambda name: views[name]["selection"]["presentation"]
        assert declared("let")["kind"] == "let" and presentation("let") == "let label: string"
        assert declared("use")["span"] == binding("use")["declarationSpan"]
        assert views["callee"]["selection"]["kind"] == "expression"
        assert declared("callee")["kind"] == "function" and declared("callee")["locationAvailable"]
        assert declared("callee")["span"]["offset"] == source.index("fn task") + len("fn ")
        assert presentation("callee") == "effect fn task() -> string raises {Child}"
        assert declared("operation")["kind"] == "operation" and declared("operation")["owner"] == "Labels"
        assert declared("operation")["span"]["offset"] == source.index("fn read") + len("fn ")
        assert presentation("operation") == "effect fn Labels.read(item: string) -> string"
        assert views["service"]["selection"]["kind"] == "reference"
        assert declared("service")["span"]["offset"] == source.index("service Labels") + len("service ")
        assert presentation("service") == "service Labels"
        bundled = declared("bundled")
        assert bundled["kind"] == "function" and not bundled["locationAvailable"]
        assert bundled["module"] == "effra/functions" and bundled["source"].startswith("source:effra/functions")
        assert declared("module-alias")["kind"] == "module" and declared("module-alias")["locationAvailable"]
        assert declared("module-alias")["span"]["offset"] == source.index('Fns "effra')
        assert presentation("module-alias") == 'import Fns "effra/functions"'
        assert views["variant"]["selection"]["kind"] == "reference"
        assert declared("variant")["kind"] == "variant" and declared("variant")["owner"] == "Notice"
        assert declared("variant")["span"]["offset"] == source.index("Named {")
        assert presentation("variant") == "variant Notice.Named { value: string }"
        # Annotation and row-label tokens resolve through the checked type and
        # row: a row parameter is the function's own, other labels are errors.
        assert views["annotation"]["selection"]["kind"] == "reference"
        assert declared("annotation")["kind"] == "enum" and declared("annotation")["span"]["offset"] == source.index("Notice {")
        assert declared("row-parameter")["kind"] == "rowParameter" and declared("row-parameter")["owner"] == "guarded"
        assert declared("row-parameter")["span"]["offset"] == source.index("guarded<E") + len("guarded<")
        assert presentation("row-parameter") == "row parameter guarded.E: raises"
        assert declared("row-label")["kind"] == "error" and presentation("row-label") == "error Child"
        named = {name: cli(path, target, "--symbol", name) for name in ("Labels", "Prefix", "Labels.read")}
        assert [named[name]["selection"]["target"]["kind"] for name in named] == ["service", "provider", "operation"]
        assert named["Labels.read"]["selection"]["presentation"] == "effect fn Labels.read(item: string) -> string"
        cli(path, target, "--symbol", "Labels.missing", success=False)
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
        fiber_id = views["fiber"]["selection"]["expression"]["type"]["type"]["ref"]
        fiber_definition = cli(path, target, "--definition", fiber_id, "--revision", revision)
        assert views["fiber"]["selection"]["expression"]["type"]["type"]["kind"] == "fiber"
        assert views["fiber"]["selection"]["expression"]["type"]["failureRow"]
        assert_causal_wire_rejections(views["fiber"], "selected Fiber")
        assert fiber_definition["selection"] == {"kind": "typeDefinition", "locationAvailable": False,
                                                   "span": {"offset": 0, "length": 0, "line": 0, "column": 0},
                                                   "extent": {"offset": 0, "length": 0, "line": 0, "column": 0},
                                                   "definition": fiber_id,
                                                   "presentation": "Fiber<string, {Child}>"}
        assert any(node["id"] == fiber_id and node["kind"] == "fiber"
                   and node["args"] and node["failureRow"] for node in fiber_definition["types"])
        assert_causal_wire_rejections(fiber_definition, "Fiber definition")
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
            tool(27, {"file": path.name, "target": target, "definition": fiber_id,
                      "expectedRevision": revision, **producer_guard}),
        ])
        for index, name in enumerate(named, 28):
            requests.append(tool(index, {"file": path.name, "target": target, "symbol": name, **producer_guard}))
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
        mcp_fiber = None
        for index, (name, _) in enumerate(selectors, 2):
            assert "result" in replies[index], replies[index]
            actual = replies[index]["result"]["structuredContent"]
            assert_report_parity(actual, views[name], target=target)
            closure(actual)
            if name == "fiber":
                mcp_fiber = actual
        assert_report_parity(replies[20]["result"]["structuredContent"], unrelated, target=target)
        assert_report_parity(replies[21]["result"]["structuredContent"], definition, target=target)
        for index, name in enumerate(named, 28):
            assert_report_parity(replies[index]["result"]["structuredContent"], named[name], target=target)
        actual_fiber_definition = replies[27]["result"]["structuredContent"]
        assert_report_parity(actual_fiber_definition, fiber_definition, target=target)
        assert mcp_fiber is not None
        assert_causal_wire_rejections(mcp_fiber, "MCP selected Fiber")
        assert_causal_wire_rejections(actual_fiber_definition, "MCP Fiber definition")
        assert replies[22]["result"]["isError"] and replies[23]["result"]["isError"]
        assert replies[24]["error"]["code"] == -32602
        if artifact["reuseScope"] == "artifact":
            assert replies[25]["result"]["isError"] and "stale producer" in replies[25]["result"]["content"][0]["text"]
            assert replies[26]["result"] == {}
        else:
            assert replies[25]["result"] == {}
print("selected lexical declarations, shadowing, canonical definitions and CLI/MCP parity: passed")
