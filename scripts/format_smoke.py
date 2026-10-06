#!/usr/bin/env python3
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile


root = pathlib.Path(__file__).resolve().parents[1]
ef = root / "bin" / "ef"


def run(*args, input_text=None, cwd=root):
    return subprocess.run(
        [str(ef), *args],
        cwd=cwd,
        input=input_text,
        text=True,
        capture_output=True,
        timeout=10,
    )


source = 'import go missing "example.invalid/no-such-package"\neffect fn main() -> string { "ok" }'
stdin = run("fmt", "--stdin", input_text=source)
assert stdin.returncode == 0, (stdin.stdout, stdin.stderr)
assert stdin.stderr == ""
assert "import go missing" in stdin.stdout and stdin.stdout.endswith("\n")

with tempfile.TemporaryDirectory(prefix="effra-format-") as directory:
    workspace = pathlib.Path(directory)
    path = workspace / "main.ef"
    path.write_text(source)
    original_mode = path.stat().st_mode & 0o777
    original_bytes = path.read_bytes()

    check = run("fmt", "--check", "--json", str(path))
    assert check.returncode == 1, (check.stdout, check.stderr)
    check_report = json.loads(check.stdout)
    assert check.stderr == "" and check_report["success"] and check_report["files"][0]["changed"]
    assert not check_report["files"][0]["written"]
    assert path.read_bytes() == original_bytes

    write = run("fmt", "--json", str(path))
    assert write.returncode == 0, (write.stdout, write.stderr)
    write_report = json.loads(write.stdout)
    assert write.stderr == "" and write_report["formatterVersion"]
    assert write_report["files"][0]["written"]
    assert path.stat().st_mode & 0o777 == original_mode
    formatted_bytes = path.read_bytes()
    formatted_mtime = path.stat().st_mtime_ns

    noop = run("fmt", str(path))
    assert noop.returncode == 0 and noop.stdout == "" and "already formatted" in noop.stderr
    assert path.read_bytes() == formatted_bytes and path.stat().st_mtime_ns == formatted_mtime

    valid = workspace / "valid.ef"
    invalid = workspace / "invalid.ef"
    valid_bytes = b'effect fn main() -> string { "valid" }'
    valid.write_bytes(valid_bytes)
    invalid.write_text('effect fn main() -> string { @ }')
    mixed = run("fmt", "--json", str(valid), str(invalid))
    assert mixed.returncode == 2 and mixed.stderr == ""
    mixed_report = json.loads(mixed.stdout)
    assert not mixed_report["success"] and not mixed_report.get("partial")
    assert mixed_report["failures"][0]["code"] == "EFMT_SYNTAX"
    assert valid.read_bytes() == valid_bytes

    alias = workspace / "alias.ef"
    os.link(valid, alias)
    aliases = run("fmt", "--json", str(valid), str(alias))
    assert aliases.returncode == 2 and aliases.stderr == ""
    assert json.loads(aliases.stdout)["failures"][0]["code"] == "EFMT_ALIAS"
    assert valid.read_bytes() == valid_bytes and alias.read_bytes() == valid_bytes

    symlink = workspace / "link.ef"
    symlink.symlink_to(path)
    rejected = run("fmt", str(symlink))
    assert rejected.returncode == 2 and "EFMT_SYMLINK" in rejected.stderr

    option_like = workspace / "--option.ef"
    option_like.write_text(source)
    option_run = run("fmt", "--", "--option.ef", cwd=workspace)
    assert option_run.returncode == 0, (option_run.stdout, option_run.stderr)

    incompatible = run("fmt", "--stdin", "--json", input_text=source)
    assert incompatible.returncode == 2 and incompatible.stderr == ""
    assert json.loads(incompatible.stdout)["failures"][0]["code"] == "EFMT_INVOCATION"

    digest = hashlib.sha256(source.encode()).hexdigest()
    messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "format-smoke", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
        {"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": source, "uri": "buffer://main.ef", "expectedDigest": digest}}},
        {"jsonrpc": "2.0", "id": 4, "method": "tools/call", "params": {"name": "code.format", "arguments": {"file": "main.ef"}}},
        {"jsonrpc": "2.0", "id": 5, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": source, "expectedDigest": "stale"}}},
        {"jsonrpc": "2.0", "id": 6, "method": "ping"},
    ]
    server = subprocess.run(
        [str(ef), "mcp", str(workspace)],
        cwd=root,
        input="\n".join(json.dumps(message) for message in messages) + "\n",
        text=True,
        capture_output=True,
        timeout=10,
    )
    assert server.returncode == 0, server.stderr
    replies = [json.loads(line) for line in server.stdout.splitlines()]
    assert [reply.get("id") for reply in replies] == [1, 2, 3, 4, 5, 6], replies
    listed = replies[1]["result"]["tools"]
    assert any(tool["name"] == "code.format" for tool in listed)
    buffer_result = replies[2]["result"]["structuredContent"]
    assert buffer_result["origin"] == "buffer" and buffer_result["uri"] == "buffer://main.ef"
    assert buffer_result["text"] == stdin.stdout
    disk_result = replies[3]["result"]["structuredContent"]
    assert disk_result["origin"] == "disk" and disk_result["text"] == stdin.stdout
    assert replies[4]["result"]["isError"] and "stale format source" in replies[4]["result"]["content"][0]["text"]
    assert replies[5]["result"] == {}

print("formatter CLI and MCP process adapters: passed")
