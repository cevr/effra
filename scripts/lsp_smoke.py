#!/usr/bin/env python3
"""Actual Content-Length framed ef processes and shared diagnostic parity."""
import json
import pathlib
import subprocess
import tempfile

from diagnostics_smoke import cli, editor_position, mcp

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


def exchange(calls, target="go", fragmented=False, expected=0, raw_prefix=b""):
    payload = raw_prefix + b"".join(map(frame, calls))
    process = subprocess.Popen([BINARY, "lsp", "--target", target], stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE)
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
        assert report == remote
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


def main():
    with tempfile.TemporaryDirectory(prefix="effra-lsp-smoke-") as temporary:
        directory = pathlib.Path(temporary)
        parity(directory)
        documents(directory)
        imports(directory)
        bounds_and_protocol(directory)
    print("LSP framed-process diagnostics, documents, imports and protocol checks passed")


if __name__ == "__main__":
    main()
