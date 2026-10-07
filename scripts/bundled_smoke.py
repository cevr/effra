#!/usr/bin/env python3
"""Compare complete bundled semantic responses through CLI and stdio MCP."""
import json
import pathlib
import re
import subprocess
import tempfile

from smoke_support import adapter_semantic, assert_report_parity

root = pathlib.Path(__file__).resolve().parents[1]
ef = str(root / "bin/ef")
fixtures = [
    ((root / "examples/generic-users.ef").read_text(), "lookup", ".Ok {", None, None),
    ((root / "examples/generic-settings.ef").read_text(), "configuration", ".Ok {", None, None),
    ('''import Fns "effra/functions"
error MissingUser
service Users { effect fn name(id: string) -> string raises {MissingUser} }
effect fn load(id: string) -> string raises {MissingUser} uses {Users} { run Users.name(id) }
effect fn greeting(id: string) -> string raises {MissingUser} uses {Users} { run Fns.call(load, id) }
''', "Fns.call", "Fns.call", "MissingUser", "Users"),
    ('''import Other "effra/functions"
error MissingSetting
service Settings { effect fn read(key: string) -> string raises {MissingSetting} }
effect fn load(key: string) -> string raises {MissingSetting} uses {Settings} { run Settings.read(key) }
effect fn configuration(key: string) -> string raises {MissingSetting} uses {Settings} { run Other.call(load, key) }
''', "Other.call", "Other.call", "MissingSetting", "Settings"),
    ('''import Convert "effra/conversions"
record LocalCodec { local: string }
record User { name: string }
record Holder { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> }
enum Bundle { Value { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> } }
error Broken { converter: Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
fn make() -> Convert.Codec<User, string, effect fn(string) -> User, effect fn(User) -> string> { Convert.witness(decode, encode) }
effect fn main() -> string {
 let converter = make()
 let user = run converter.decode("Ada")
 run converter.encode(user)
}
''', "make", "Convert.witness", None, None),
    ('''import Fns "effra/functions"
effect fn echo(input: string) -> string { Fns.identity(input) }
effect fn local<E: raises, R: uses>(callback: effect fn(string) -> string raises {E} uses {R}, input: string) -> string raises {E} uses {R} { run callback(input) }
effect fn main() -> string { run local(echo, "ok") }
''', "local", "local(echo", None, None),
]


def cli(command, file, target, *args, success=True):
    result = subprocess.run([ef, command, str(file), *args, "--target", target],
                            cwd=root, text=True, capture_output=True)
    assert (result.returncode == 0) == success, (result.stdout, result.stderr)
    return json.loads(result.stdout)


with tempfile.TemporaryDirectory(prefix="effra-bundled-parity-") as tmp:
    file = pathlib.Path(tmp) / "main.ef"
    for source, symbol, anchor, failure, service in fixtures:
        file.write_text(source)
        for target in ("go", "js"):
            offset = source.index(anchor) + (len(anchor.split(".")[0]) + 1 if "." in anchor else 0)
            operations = [("check", "project.check", {}, []),
                          ("inspect", "code.inspect", {"symbol": symbol}, [symbol]),
                          ("query", "code.typeAt", {"offset": offset}, [str(offset)]),
                          ("graph", "project.graph", {}, [])]
            expected = [cli(command, file, target, *args)
                        for command, _, _, args in operations]
            revision = expected[0]["revision"]
            messages = [{"jsonrpc": "2.0", "id": 1, "method": "initialize",
                         "params": {"protocolVersion": "2025-11-25", "capabilities": {},
                                    "clientInfo": {"name": "bundled-parity", "version": "1"}}},
                        {"jsonrpc": "2.0", "method": "notifications/initialized"}]
            for index, (_, tool, args, _) in enumerate(operations, start=2):
                messages.append({"jsonrpc": "2.0", "id": index, "method": "tools/call",
                                 "params": {"name": tool, "arguments": {
                                     "file": "main.ef", "target": target,
                                     "expectedRevision": revision, **args}}})
            messages.append({"jsonrpc": "2.0", "id": 6, "method": "tools/call",
                             "params": {"name": "project.check", "arguments": {
                                 "file": "main.ef", "target": target, "expectedRevision": "stale"}}})
            server = subprocess.run([ef, "mcp", tmp],
                                    input="\n".join(map(json.dumps, messages)) + "\n",
                                    text=True, capture_output=True, cwd=root)
            assert server.returncode == 0, server.stderr
            replies = [json.loads(line) for line in server.stdout.splitlines()]
            assert len(replies) == len(messages) - 1, replies
            for response, want in zip(replies[1:5], expected):
                assert "result" in response, response
                result = response["result"]
                assert not result.get("isError"), result
                actual = result["structuredContent"]
                assert_report_parity(actual, want, target=target,
                                     ignored=("file", "timings"), project=adapter_semantic)
                assert actual["producerIdentity"] and actual["sources"]
                assert actual["bundledInterfaces"] and actual["bundledBindings"]
                assert actual["revision"] == revision and actual["target"] == target
                assert actual["typeProjectionComplete"]
                declarations = {item["identity"]: item for item in actual.get("declarations") or []}
                type_ids = {item["id"] for item in actual.get("types", [])}
                row_ids = {item["id"] for item in actual.get("rows", [])}
                error_names = {item["name"] for item in actual.get("types", []) if item["kind"] == "error"}
                service_names = set(re.findall(r"\bservice\s+(\w+)\s*\{", source))
                failure_rows = {item["failureRow"] for item in actual.get("types", []) if item.get("failureRow")}
                service_rows = {item["serviceRow"] for item in actual.get("types", []) if item.get("serviceRow")}
                for row in actual.get("rows", []):
                    labels = row.get("labels", [])
                    parameters = {item["id"]: item for item in row.get("parameters", [])}
                    assert all(identity in labels for identity in parameters), (row, parameters)
                    for parameter in parameters.values():
                        assert parameter["id"] == "row-parameter:" + parameter["declaration"] + ":" + parameter["kind"] + ":" + parameter["name"], parameter
                    concrete = [label for label in labels if label not in parameters]
                    if labels:
                        assert row["id"] in failure_rows | service_rows, (row, failure_rows, service_rows)
                    if row["id"] in failure_rows:
                        assert all(label in error_names for label in concrete), (row, error_names)
                        assert all(parameter["kind"] == "raises" for parameter in parameters.values()), row
                    if row["id"] in service_rows:
                        assert all(label in service_names for label in concrete), (row, service_names)
                        assert all(parameter["kind"] == "uses" for parameter in parameters.values()), row
                        if "nodes" in actual:
                            nodes = {item["id"] for item in actual["nodes"]}
                            assert all("service:" + label in nodes for label in concrete), (row, nodes)
                for owner in declarations.values():
                    if owner["kind"] != "template":
                        continue
                    fields = owner.get("fields", []) + [field for variant in owner.get("variants", []) for field in variant.get("fields", [])]
                    references = [field["typeRef"]["ref"] for field in fields]
                    references += [parameter["variable"]["ref"] for parameter in owner["templateParameters"]]
                    assert all(reference in type_ids for reference in references), (owner, references, type_ids)
                for node in actual.get("types", []):
                    for reference in [*node.get("args", [])] + ([node["result"]] if node.get("result") else []):
                        assert reference in type_ids, (node, reference, type_ids)
                    for key in ("failureRow", "serviceRow"):
                        if node.get(key):
                            assert node[key] in row_ids, (node, row_ids)
                    if node["kind"] == "application":
                        owner = declarations.get(node["declaration"])
                        assert owner and owner["kind"] == "template", (node, declarations)
                        if node["declaration"] == "template:effra/conversions:module:Codec":
                            assert len(owner["fields"]) == 2 and len(owner["templateParameters"]) == 4
                            assert owner["source"] == "source:effra/conversions/Codec"
                        elif node["declaration"].startswith("template:effra/data:"):
                            assert owner["dataKind"] == "enum" and len(owner["variants"]) == 2
                            assert owner["source"] == "source:effra/data/" + owner["name"]
                            assert len(owner["templateParameters"]) == {"Option": 1, "Result": 2}[owner["name"]]
                            assert any(item["id"] == owner["source"] and item["module"] == "effra/data" for item in actual["sources"])
                        else:
                            assert owner["dataKind"] == "record" and owner["source"] == "source:user", owner
                if "edges" in actual:
                    nodes = {item["id"] for item in actual["nodes"]}
                    assert all(edge["from"] in nodes and edge["to"] in nodes
                               for edge in actual["edges"]), actual["edges"]
            assert replies[5]["result"]["isError"]
            if failure:
                # The public caller's missing rows must fail identically on both surfaces.
                signature = "-> string raises {" + failure + "} uses {" + service + "} { run " + anchor
                for row, code in ((" raises {" + failure + "}", "EF107"),
                                  (" uses {" + service + "}", "EF108")):
                    file.write_text(source.replace(signature, signature.replace(row, ""), 1))
                    rejected = cli("check", file, target, success=False)
                    assert any(item["code"] == code for item in rejected["diagnostics"]), rejected
                    invalid_messages = messages[:2] + [{
                        "jsonrpc": "2.0", "id": 2, "method": "tools/call",
                        "params": {"name": "project.check", "arguments": {
                            "file": "main.ef", "target": target}}}]
                    invalid = subprocess.run([ef, "mcp", tmp],
                                             input="\n".join(map(json.dumps, invalid_messages)) + "\n",
                                             text=True, capture_output=True, cwd=root)
                    assert invalid.returncode == 0, invalid.stderr
                    result = json.loads(invalid.stdout.splitlines()[1])["result"]["structuredContent"]
                    assert_report_parity(result, rejected, target=target,
                                         ignored=("file", "timings"), project=adapter_semantic)
                file.write_text(source)

    generic_prefix = 'import Data "effra/data" record User {name:string} '
    refusals = [(generic_prefix + body, None) for body in (
        'fn bad()->Data.Option<User>{nil}',
        'fn bad()->Data.Option<User>{null}',
        'fn bad()->Data.Option<User?>{Data.Option.Some {value:User {name:"x"}}}',
        'fn bad()->Data.Option<User>{Data.Option.Some {}}',
        'fn bad(value:Data.Option<User>)->User{value.value}',
        'fn bad()->Data.Option<User>{Foreign.Option<User>.None {}}',
    )]
    caller = (root / "examples/generic-users.ef").read_text()
    header = next(line for line in caller.splitlines() if line.startswith("effect fn lookup("))
    for row, code in ((" raises { Unavailable }", "EF107"), (" uses { Users }", "EF108")):
        assert row in header
        refusals.append((caller.replace(header, header.replace(row, ""), 1), code))
    for source, code in refusals:
        file.write_text(source)
        for target in ("go", "js"):
            rejected = cli("check", file, target, success=False)
            assert not rejected["checked"] and rejected["diagnostics"]
            if code:
                assert any(item["code"] == code for item in rejected["diagnostics"]), rejected
            messages = [{"jsonrpc": "2.0", "id": 1, "method": "initialize",
                         "params": {"protocolVersion": "2025-11-25", "capabilities": {},
                                    "clientInfo": {"name": "generic-data-refusals", "version": "1"}}},
                        {"jsonrpc": "2.0", "method": "notifications/initialized"},
                        {"jsonrpc": "2.0", "id": 2, "method": "tools/call",
                         "params": {"name": "project.check", "arguments": {"file": "main.ef", "target": target}}}]
            response = subprocess.run([ef, "mcp", tmp], input="\n".join(map(json.dumps, messages)) + "\n",
                                      text=True, capture_output=True, cwd=root)
            assert response.returncode == 0, response.stderr
            actual = json.loads(response.stdout.splitlines()[1])["result"]["structuredContent"]
            assert adapter_semantic(actual) == adapter_semantic(rejected), (actual, rejected)

print("bundled/generic CLI/MCP check, inspect, query, graph, absence/row refusals and stale revision parity: passed")
