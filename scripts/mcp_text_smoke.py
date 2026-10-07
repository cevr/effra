#!/usr/bin/env python3
"""Exercise lossless MCP text admission through the compiled stdio binary."""

import json
import pathlib
import subprocess


ROOT = pathlib.Path(__file__).resolve().parents[1]
BINARY = str(ROOT / "bin/ef")


def json_line(value):
    return json.dumps(value, separators=(",", ":")).encode("utf-8") + b"\n"


initialize = json_line(
    {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2025-11-25",
            "capabilities": {},
            "clientInfo": {"name": "mcp-text-smoke", "version": "1"},
        },
    }
)
ready = json_line({"jsonrpc": "2.0", "method": "notifications/initialized"})
ping = json_line({"jsonrpc": "2.0", "id": 3, "method": "ping"})
request_prefix = (
    b'{"jsonrpc":"2.0","id":2,"method":"tools/call",'
    b'"params":{"name":"code.format","arguments":{"source":'
)


def format_request(source_json):
    return request_prefix + source_json + b"}}}\n"


invalid_utf8 = b'"// ' + bytes([0xFF]) + b'"'
cases = [
    ("invalid-utf8", invalid_utf8, True),
    ("unpaired-high-surrogate", b'"// \\ud800"', True),
    ("unpaired-low-surrogate", b'"// \\udc00"', True),
    ("literal-replacement-character", json.dumps("// �", ensure_ascii=False).encode(), False),
    ("astral-surrogate-pair", b'"// \\ud83d\\ude00"', False),
    ("literal-backslash", json.dumps("// \\ud800").encode(), False),
    ("empty", b'""', False),
]

for name, source_json, malformed in cases:
    process = subprocess.run(
        [BINARY, "mcp", str(ROOT)],
        cwd=ROOT,
        input=initialize + ready + format_request(source_json) + ping,
        capture_output=True,
        timeout=10,
    )
    assert process.returncode == 0, (name, process.stdout, process.stderr)
    assert process.stderr == b"", (name, process.stderr)
    responses = [json.loads(line) for line in process.stdout.splitlines()]
    assert len(responses) == 3, (name, responses)
    if malformed:
        assert responses[1].get("error", {}).get("code") == -32700, (name, responses[1])
    else:
        assert responses[1].get("result", {}).get("isError") is not True, (name, responses[1])
    assert responses[2].get("result") == {}, (name, responses[2])

print("compiled stdio MCP lossless text admission and queued ping controls: passed")
