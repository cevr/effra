#!/usr/bin/env python3
"""Actual Content-Length framed ef processes and shared diagnostic parity."""
import argparse
import json
import os
import pathlib
import subprocess
import tempfile

from diagnostics_smoke import cli, editor_position, mcp
from smoke_support import assert_report_parity

ROOT = pathlib.Path(__file__).resolve().parents[1]
BINARY = str(ROOT / "bin/ef")


def call(method, params=None, identifier=None):
    value = {"jsonrpc": "2.0", "method": method}
    if params is not None:
        value["params"] = params
    if identifier is not None:
        value["id"] = identifier
    return value


def frame(value):
    body = json.dumps(value, ensure_ascii=False).encode()
    return f"Content-Length: {len(body)}\r\n\r\n".encode() + body


def decode(data):
    messages = []
    while data:
        header, data = data.split(b"\r\n\r\n", 1)
        assert header.startswith(b"Content-Length: "), header
        count = int(header.removeprefix(b"Content-Length: "))
        assert len(data) >= count
        messages.append(json.loads(data[:count]))
        data = data[count:]
    return messages


INIT = call("initialize", {"capabilities": {}}, 1)
READY = call("initialized", {})
STOP = call("shutdown", identifier="stop")
EXIT = call("exit")


def exchange(calls, target="go", fragmented=False, expected=0, raw_prefix=b"", environment=None):
    payload = raw_prefix + b"".join(map(frame, calls))
    process = subprocess.Popen([BINARY, "lsp", "--target", target], stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               env=None if environment is None else {**os.environ, **environment})
    try:
        if fragmented:
            # Split both header and Unicode body bytes. The suffix coalesces
            # several complete messages in one write without timing assumptions.
            # Include the entire open notification, not just initialization.
            split = sum(len(frame(value)) for value in calls[:3])
            prefix, payload = payload[:split], payload[split:]
            for byte in prefix:
                process.stdin.write(bytes([byte]))
                process.stdin.flush()
        stdout, stderr = process.communicate(payload, timeout=40)
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate()
    assert process.returncode == expected, (process.returncode, stderr)
    if expected == 0:
        assert not stderr, stderr
    else:
        assert stderr, "abnormal termination must be explicit"
    return decode(stdout)


def opened(path, text, version=1):
    return call("textDocument/didOpen", {"textDocument": {
        "uri": path.as_uri(), "languageId": "effra", "version": version, "text": text}})


def changed(path, text, version):
    return call("textDocument/didChange", {"textDocument": {
        "uri": path.as_uri(), "version": version}, "contentChanges": [{"text": text}]})


def publications(messages):
    return [m["params"] for m in messages if m.get("method") == "textDocument/publishDiagnostics"]


def parity(directory):
    warning = ('effect fn task() -> string { "ok" }\r\n'
               'effect fn main() -> string { let s = "𐐀é"; '
               'let forgotten = task(); run task().provide<Console>(Stdout) }\r\n')
    fixtures = [('unicode.ef', 'effect fn main() -> () { "𐐀é" @ }'),
                ('crlf.ef', '// comment\r\neffect fn main() -> () { () }\r\n@'),
                ('eof.ef', 'effect fn main() -> () {\r\n'), ('warning.ef', warning),
                ('lint-error.ef', '// effra-lint-disable-next-line bogus -- reason\r\n'
                 'effect fn main() -> () { () }')]
    for name, text in fixtures:
        path = directory / name
        path.write_bytes(text.encode())
        report = cli(BINARY, path)
        remote = mcp(BINARY, directory, [{"file": name}])[0]["result"]["structuredContent"]
        assert_report_parity(report, remote, report_schema=1, snapshot_schema=6)
        messages = exchange([INIT, READY, opened(path, text, 4), STOP, EXIT], fragmented=True)
        capabilities = messages[0]["result"]["capabilities"]
        assert capabilities == {"positionEncoding": "utf-16", "textDocumentSync": {"openClose": True, "change": 1}}
        published = publications(messages)
        assert published == [{"uri": path.as_uri(), "version": 4,
                              "diagnostics": [f["lsp"] for f in report["diagnostics"]]}]
        for finding in report["diagnostics"]:
            span = finding["span"]
            assert finding["lsp"]["range"] == {
                "start": editor_position(text, span["offset"]),
                "end": editor_position(text, span["offset"] + span["length"])}


def documents(directory):
    path = directory / "unsaved space.ef"
    bad = 'fn bad() -> string { true }'
    good = 'fn good() -> string { "good" }'
    path.write_text(good)
    calls = [INIT, READY, opened(path, bad, 7), changed(path, good, 8),
             changed(path, bad, 7), opened(path, bad),
             call("textDocument/didChange", {"textDocument": {"uri": path.as_uri(), "version": 9},
                  "contentChanges": [{"range": None, "text": bad}]}),
             call("textDocument/didClose", {"textDocument": {"uri": path.as_uri()}}),
             opened(path, bad, -2), call("$/cancelRequest", {"id": "already-complete"}),
             call("textDocument/hover", identifier="hover"), STOP, EXIT]
    messages = exchange(calls)
    published = publications(messages)
    assert [p.get("version") for p in published] == [7, 8, None, -2], published
    assert published[0]["diagnostics"] and published[1]["diagnostics"] == []
    assert published[2]["diagnostics"] == []
    assert len([m for m in messages if m.get("method") == "window/logMessage"]) == 3
    assert next(m for m in messages if m.get("id") == "hover")["error"]["code"] == -32601
    assert cli(BINARY, path)["diagnostics"] == [], "disk changed while analyzing buffer"
    new = directory / "never-created.ef"
    assert not new.exists()
    assert publications(exchange([INIT, READY, opened(new, good), STOP, EXIT]))[0]["diagnostics"] == []
    assert not new.exists()
    assert publications(exchange([INIT, READY, opened(new, good), STOP, EXIT], target="js"))[0]["diagnostics"] == []
    # The compiler used to panic on a valid effect factory carrying a pure
    # callback. A real document must publish, accept the next edit, and recover.
    factories = (ROOT / "examples/callables-factory.ef").read_text()
    invalid_timeout = factories.replace('let first = run pureFailure().catch<Missing>(keep)',
                                        'let first = keep.timeout(10)')
    assert invalid_timeout != factories
    for target in ("go", "js"):
        reports = publications(exchange([
            INIT, READY, opened(new, factories), changed(new, invalid_timeout, 2),
            changed(new, factories, 3), STOP, EXIT], target=target))
        assert [r["version"] for r in reports] == [1, 2, 3], reports
        assert reports[0]["diagnostics"] == reports[2]["diagnostics"] == [], reports
        assert any(d["code"] == "EF106" for d in reports[1]["diagnostics"]), reports


def imports(directory):
    project = directory / "imports"
    sdk = project / "sdk"
    sdk.mkdir(parents=True)
    (project / "go.mod").write_text("module example.local/lsp\n\ngo 1.27\n")
    (sdk / "sdk.go").write_text('package sdk\nfunc Name() string { return "ok" }\n')
    path = project / "new.ef"
    text = 'import go sdk "example.local/lsp/sdk"\neffect fn main() -> string uses {Foreign} { run sdk.Name() }\n'
    # No .ef file exists and the server starts elsewhere: module resolution
    # must still use the captured document's directory.
    result = publications(exchange([INIT, READY, opened(path, text), STOP, EXIT]))
    assert result and result[0]["diagnostics"] == [], result
    missing = 'import go sdk "effra.invalid/lsp-missing"\n'
    messages = exchange([INIT, READY, changed(path, missing, 2), opened(path, missing),
                         call("unknown", identifier="after"), STOP, EXIT])
    assert not publications(messages), messages
    assert any("EF111" in m.get("params", {}).get("message", "") for m in messages)
    assert next(m for m in messages if m.get("id") == "after")["error"]["code"] == -32601


def bounds_and_protocol(directory):
    path = directory / "bounded.ef"
    max_doc = 256 * 1024
    maximum = "//" + "x" * (max_doc - 2)
    messages = exchange([INIT, READY, opened(path, maximum), changed(path, maximum + "x", 2),
                         changed(path, "", 2), STOP, EXIT])
    assert [p["version"] for p in publications(messages)] == [1, 2]
    assert sum(m.get("method") == "window/logMessage" for m in messages) == 1
    messages = exchange([INIT, READY, opened(path, 'fn duplicate() -> () { () }\n' * 1002),
                         changed(path, "", 2), STOP, EXIT])
    assert [p["version"] for p in publications(messages)] == [2], messages
    assert any("limit" in m.get("params", {}).get("message", "") for m in messages)
    invalid_uris = ["file://remote/tmp/bad.ef", "file:///tmp/bad.ef?query",
                    "file:///tmp/a/../bad.ef", "untitled:bad.ef", "file:///tmp/bad%00.ef"]
    invalid_opens = [call("textDocument/didOpen", {"textDocument": {
        "uri": uri, "languageId": "effra", "version": 1, "text": ""}})
        for uri in invalid_uris]
    messages = exchange([INIT, READY, *invalid_opens, opened(path, ""), STOP, EXIT])
    assert len(publications(messages)) == 1
    assert sum(m.get("method") == "window/logMessage" for m in messages) == len(invalid_uris)
    # Invalid body/ID and unsupported method preserve the next frame boundary.
    messages = exchange([INIT, READY, {"jsonrpc": "2.0", "id": [], "method": "bad"},
                         call("unknown", identifier="after"), STOP, EXIT],
                        raw_prefix=b"Content-Length: 1\r\n\r\n{")
    assert messages[0]["error"]["code"] == -32700
    assert any(m.get("error", {}).get("code") == -32600 for m in messages)
    assert next(m for m in messages if m.get("id") == "after")["error"]["code"] == -32601
    messages = exchange([INIT, READY, STOP, call("unknown", identifier="late"), EXIT])
    assert next(m for m in messages if m.get("id") == "late")["error"]["code"] == -32600
    exchange([INIT, READY, STOP])  # EOF after shutdown releases the session cleanly.
    exchange([INIT, READY], expected=1)
    exchange([EXIT], expected=1)
    for data in (b"Content-Length: 2097153\r\n\r\n", b"Content-Length: 2\r\n\r\n{",
                 b"Content-Length: 1\r\nContent-Length: 1\r\n\r\n{",
                 b"X: " + b"x" * 8192 + b"\r\n\r\n"):
        exchange([], expected=1, raw_prefix=data)
    for target in ("bad",):
        run = subprocess.run([BINARY, "lsp", "--target", target], capture_output=True, timeout=5)
        assert run.returncode == 2 and not run.stdout and run.stderr


def uri_alias_regression(directory):
    path = directory / "c++" / "@scope" / "counter:one,x=y;z.ef"
    original = path.as_uri()
    alias = "FILE:" + str(path)
    good = 'fn good() -> string { "good" }'
    duplicate = opened(path, "bad", 2)
    duplicate["params"]["textDocument"]["uri"] = alias
    change = changed(path, good, 2)
    change["params"]["textDocument"]["uri"] = alias
    messages = exchange([INIT, READY, opened(path, 'fn bad() -> string { true }'),
                         duplicate, change,
                         call("textDocument/didClose", {"textDocument": {"uri": alias}}),
                         duplicate, STOP, EXIT])
    published = publications(messages)
    assert [p["uri"] for p in published] == [original, original, original, alias], messages
    assert [p.get("version") for p in published] == [1, 2, None, 2]
    assert published[0]["diagnostics"] and published[1]["diagnostics"] == []
    assert len([m for m in messages if m.get("method") == "window/logMessage"]) == 1
    # Existing encoders need not agree on reserved/unreserved bytes or hex case.
    for spelling in (original.replace("%2B", "%2b"), original.replace("counter", "%63ounter")):
        notification = opened(path, good)
        notification["params"]["textDocument"]["uri"] = spelling
        assert publications(exchange([INIT, READY, notification, STOP, EXIT]))[0]["uri"] == spelling


def operational_budget_regression(directory):
    project = directory / "large-errors"
    project.mkdir()
    (project / "go.mod").write_text("module example.local/lsp-errors\n\ngo 1.26\n")
    path = project / "unsaved.ef"
    # A real go-list operational failure has detail exceeding the old output
    # budget, while this captured source stays below the document text limit.
    text = "".join(f'import go absent{i} "example.invalid/absent{i:04d}"\n' for i in range(4000))
    assert len(text.encode()) < 256 * 1024
    messages = exchange([INIT, READY, opened(path, text, 19),
                         call("unknown", identifier="after-error"),
                         call("textDocument/didClose", {"textDocument": {"uri": path.as_uri()}}),
                         STOP, EXIT], environment={"GOPROXY": "off", "GOTOOLCHAIN": "local", "GOWORK": "off"})
    logs = [m["params"]["message"] for m in messages if m.get("method") == "window/logMessage"]
    assert len(logs) == 1 and "EF111" in logs[0] and "version 19" in logs[0] and path.as_uri() in logs[0], logs
    assert "bytes omitted" in logs[0] and len(logs[0].encode()) < 16 * 1024, logs[0][:100]
    assert next(m for m in messages if m.get("id") == "after-error")["error"]["code"] == -32601
    assert publications(messages) == [{"uri": path.as_uri(), "diagnostics": []}]


def refusal_recovery_regression(directory):
    path = directory / "recover.ef"
    invalid = ["file:///tmp/a%2Fb.ef", "file:///tmp/a%2fb.ef",
               "file:///tmp/a%2Fb c.ef", "file:///tmp/ü%2Fb.ef",
               'file:///tmp/a%2Fb".ef', "file:///tmp/a%2Fb{.ef",
               "file:///tmp/a/%2e%2e/b.ef", "file:///tmp//b.ef",
               "file:///tmp/%ff.ef", "file:///tmp/" + "x" * 4096 + ".ef"]
    notifications = [call("textDocument/didOpen", {"textDocument": {
        "uri": uri, "text": "", "languageId": "effra", "version": 1}}) for uri in invalid]
    messages = exchange([INIT, READY, *notifications, call("unknown", identifier="after-refusal"),
                         opened(path, ""), STOP, EXIT])
    assert sum(m.get("method") == "window/logMessage" for m in messages) == len(invalid)
    assert next(m for m in messages if m.get("id") == "after-refusal")["error"]["code"] == -32601
    assert len(publications(messages)) == 1
    # Raw editor spellings and one-decode literal escapes remain admitted.
    for uri in ("file:///tmp/a b.ef", "file:///tmp/ü.ef", "file:///tmp/%252F.ef"):
        notification = call("textDocument/didOpen", {"textDocument": {
            "uri": uri, "text": "", "languageId": "effra", "version": 1}})
        messages = exchange([INIT, READY, notification, STOP, EXIT])
        assert publications(messages) == [{"uri": uri, "version": 1, "diagnostics": []}], messages
        assert not any(m.get("method") == "window/logMessage" for m in messages), messages
    # Reject the encoded separator before decoded-path alias lookup.
    plain, encoded = "file:///tmp/a/b c.ef", "file:///tmp/a%2Fb c.ef"
    notifications = [call("textDocument/didOpen", {"textDocument": {
        "uri": uri, "text": "", "languageId": "effra", "version": 1}}) for uri in (plain, encoded)]
    messages = exchange([INIT, READY, *notifications, STOP, EXIT])
    assert publications(messages) == [{"uri": plain, "version": 1, "diagnostics": []}], messages
    logs = [m["params"]["message"] for m in messages if m.get("method") == "window/logMessage"]
    assert len(logs) == 1 and "must not encode path separators" in logs[0] and "duplicate" not in logs[0], logs
    # Located findings remain all-or-refuse. Refusing this projection must
    # still permit close and a following request on the same framed session.
    name = "d" * 200
    text = f'fn {name}() -> () {{ () }}\n' * 1001
    assert len(text.encode()) < 256 * 1024
    messages = exchange([INIT, READY, opened(path, text, 23),
                         call("textDocument/didClose", {"textDocument": {"uri": path.as_uri()}}),
                         call("unknown", identifier="after-publication"), STOP, EXIT])
    logs = [m["params"]["message"] for m in messages if m.get("method") == "window/logMessage"]
    assert len(logs) == 1 and "output" in logs[0] and "version 23" in logs[0], logs
    assert publications(messages) == [{"uri": path.as_uri(), "diagnostics": []}]
    assert next(m for m in messages if m.get("id") == "after-publication")["error"]["code"] == -32601


def oversized_uri_regression(directory):
    uri = "file:///nonexistent/loop-probe-x/" + "x" * 361837 + ".ef"
    messages = exchange([INIT, READY, call("textDocument/didOpen", {"textDocument": {
        "uri": uri, "text": "", "languageId": "effra", "version": 1}}),
        call("textDocument/didClose", {"textDocument": {"uri": uri}}),
        call("unknown", identifier="after-huge-uri"), opened(directory / "ordinary.ef", ""), STOP, EXIT])
    assert sum(m.get("method") == "window/logMessage" for m in messages) == 2
    assert len(publications(messages)) == 1
    assert next(m for m in messages if m.get("id") == "after-huge-uri")["error"]["code"] == -32601


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--review-case", choices=["uri", "operational", "recovery", "oversized"])
    arguments = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="effra-lsp-smoke-") as temporary:
        directory = pathlib.Path(temporary)
        if arguments.review_case:
            {"uri": uri_alias_regression, "operational": operational_budget_regression,
             "recovery": refusal_recovery_regression,
             "oversized": oversized_uri_regression}[arguments.review_case](directory)
        else:
            parity(directory)
            documents(directory)
            imports(directory)
            bounds_and_protocol(directory)
            uri_alias_regression(directory)
            operational_budget_regression(directory)
            refusal_recovery_regression(directory)
            oversized_uri_regression(directory)
    print("LSP framed-process diagnostics, documents, imports and protocol checks passed")


if __name__ == "__main__":
    main()
