#!/usr/bin/env python3
import hashlib
import json
import os
import pathlib
import subprocess
import tempfile


root = pathlib.Path(__file__).resolve().parents[1]
ef = root / "bin" / "ef"


def run(*args, input_text=None, cwd=root, timeout=10):
    return subprocess.run(
        [str(ef), *args],
        cwd=cwd,
        input=input_text,
        text=True,
        capture_output=True,
        timeout=timeout,
    )


def run_bytes(*args, input_bytes=None, cwd=root, timeout=120):
    return subprocess.run(
        [str(ef), *args],
        cwd=cwd,
        input=input_bytes,
        capture_output=True,
        timeout=timeout,
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
    assert write.stderr == "" and write_report["formatterVersion"] == "effra/formatter-5"
    assert write_report["schemaVersion"] == 1
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

    duplicate = run("fmt", "--json", "main.ef", "./main.ef", cwd=workspace)
    duplicate_report = json.loads(duplicate.stdout)
    assert duplicate.returncode == 0 and duplicate.stderr == ""
    assert len(duplicate_report["files"]) == 1 and duplicate_report["files"][0]["requestedPaths"] == ["main.ef", "./main.ef"]

    input_limit = 2 * 1024 * 1024
    input_paths = []
    for index in range(4):
        input_path = workspace / f"input-{index}.ef"
        input_path.write_bytes(b" " * input_limit)
        input_paths.append(input_path)
    exact_input = run("fmt", "--check", "--json", *(path.name for path in input_paths), cwd=workspace, timeout=120)
    assert exact_input.returncode == 1 and json.loads(exact_input.stdout)["success"]
    aggregate_extra = workspace / "aggregate-extra.ef"
    aggregate_extra.write_bytes(b" ")
    aggregate_input_over = run("fmt", "--json", *(path.name for path in input_paths), aggregate_extra.name, cwd=workspace, timeout=120)
    aggregate_input_report = json.loads(aggregate_input_over.stdout)
    assert aggregate_input_over.returncode == 2 and aggregate_input_report["failures"][0]["code"] == "EFMT_INPUT_LIMIT" and "aggregate input limit" in aggregate_input_report["failures"][0]["message"]
    aggregate_extra.write_bytes(b" " * (input_limit + 1))
    per_file_input_over = run("fmt", "--json", aggregate_extra.name, cwd=workspace, timeout=120)
    assert per_file_input_over.returncode == 2 and json.loads(per_file_input_over.stdout)["failures"][0]["code"] == "EFMT_INPUT_LIMIT"

    expansion = b"effect fn main() -> void { " + b"scope { " * 64 + b"void;\n" * 15000 + b" }" * 64 + b" }"
    formatted_expansion = run_bytes("fmt", "--stdin", input_bytes=expansion)
    assert formatted_expansion.returncode == 0

    overflow_expansion = b"effect fn main() -> void { " + b"scope { " * 64 + b"void;\n" * 20000 + b" }" * 64 + b" }"
    expanded_output = run_bytes("fmt", "--stdin", input_bytes=overflow_expansion)
    assert expanded_output.returncode == 2 and expanded_output.stdout == b"" and b"EFMT_OUTPUT_LIMIT" in expanded_output.stderr

    output_limit = 4 * 1024 * 1024
    padding = output_limit - len(formatted_expansion.stdout) - 3
    assert padding >= 0
    exact_output_source = b"//" + b"x" * padding + b"\n" + expansion
    exact_output = run_bytes("fmt", "--stdin", input_bytes=exact_output_source)
    assert exact_output.returncode == 0 and len(exact_output.stdout) == output_limit
    one_over_output_source = b"//x" + exact_output_source[2:]
    one_over_output = run_bytes("fmt", "--stdin", input_bytes=one_over_output_source)
    assert one_over_output.returncode == 2 and one_over_output.stdout == b"" and b"EFMT_OUTPUT_LIMIT" in one_over_output.stderr
    output_paths = []
    for index in range(4):
        output_path = workspace / f"output-{index}.ef"
        output_path.write_bytes(exact_output_source)
        output_paths.append(output_path)
    exact_aggregate_output = run("fmt", "--check", "--json", *(path.name for path in output_paths), cwd=workspace, timeout=120)
    assert exact_aggregate_output.returncode == 1 and json.loads(exact_aggregate_output.stdout)["success"]
    aggregate_empty = workspace / "aggregate-empty.ef"
    aggregate_empty.write_bytes(b"")
    exact_with_empty = run("fmt", "--check", "--json", *(path.name for path in output_paths), aggregate_empty.name, cwd=workspace, timeout=120)
    assert exact_with_empty.returncode == 1 and json.loads(exact_with_empty.stdout)["success"]
    aggregate_extra.write_bytes(b"//extra")
    aggregate_output_over = run("fmt", "--json", *(path.name for path in output_paths), aggregate_extra.name, cwd=workspace, timeout=120)
    aggregate_output_report = json.loads(aggregate_output_over.stdout)
    assert aggregate_output_over.returncode == 2 and aggregate_output_report["failures"][0]["code"] == "EFMT_OUTPUT_LIMIT" and "aggregate output limit" in aggregate_output_report["failures"][0]["message"]
    assert all(path.read_bytes() == exact_output_source for path in output_paths)

    invalid_utf8 = run_bytes("fmt", "--stdin", input_bytes=b"// invalid \xff\neffect fn main() -> void { void }\n")
    assert invalid_utf8.returncode == 2 and invalid_utf8.stdout == b"" and b"not valid UTF-8" in invalid_utf8.stderr

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
    assert buffer_result["formatterVersion"] == write_report["formatterVersion"]
    assert buffer_result["schemaVersion"] == write_report["schemaVersion"]
    assert buffer_result["origin"] == "buffer" and buffer_result["uri"] == "buffer://main.ef"
    assert buffer_result["text"] == stdin.stdout
    disk_result = replies[3]["result"]["structuredContent"]
    assert disk_result["origin"] == "disk" and disk_result["text"] == stdin.stdout
    assert replies[4]["result"]["isError"] and "stale format source" in replies[4]["result"]["content"][0]["text"]
    assert replies[5]["result"] == {}

    expansion_messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "format-expansion", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": overflow_expansion.decode()}}},
        {"jsonrpc": "2.0", "id": 3, "method": "ping"},
    ]
    expansion_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input="\n".join(json.dumps(message) for message in expansion_messages) + "\n", text=True, capture_output=True, timeout=60)
    expansion_replies = [json.loads(line) for line in expansion_server.stdout.splitlines()]
    expansion_result = expansion_replies[1]["result"]
    assert expansion_server.returncode == 0 and [reply.get("id") for reply in expansion_replies] == [1, 2, 3]
    assert expansion_result["isError"] and "structuredContent" not in expansion_result and "MCP output limit" in expansion_result["content"][0]["text"]

    target_messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "format-target", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": "", "target": "go"}}},
        {"jsonrpc": "2.0", "id": 3, "method": "ping"},
    ]
    target_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input="\n".join(json.dumps(message) for message in target_messages) + "\n", text=True, capture_output=True, timeout=10)
    target_replies = [json.loads(line) for line in target_server.stdout.splitlines()]
    assert target_server.returncode == 0 and target_replies[2]["result"] == {} and target_replies[1].get("error")

    compact_format = json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}}, separators=(",", ":"))
    init_line = json.dumps(messages[0], separators=(",", ":"))
    ready_line = json.dumps(messages[1], separators=(",", ":"))
    ping_line = json.dumps({"jsonrpc": "2.0", "id": 3, "method": "ping"}, separators=(",", ":"))
    frame_limit = 16 * 1024 * 1024
    for size in (frame_limit - 1, frame_limit, frame_limit + 1, frame_limit + 1024 * 1024):
        raw = compact_format + " " * (size - len(compact_format))
        frame_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n" + raw + "\n" + ping_line + "\n").encode(), capture_output=True, timeout=60)
        frame_replies = [json.loads(line) for line in frame_server.stdout.splitlines()]
        assert frame_server.returncode == 0 and frame_server.stderr == b""
        if size <= frame_limit:
            assert [reply.get("id") for reply in frame_replies] == [1, 2, 3]
        else:
            assert [reply.get("id") for reply in frame_replies] == [1, None, 3]

    malformed_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n{bad json}\n" + ping_line + "\n").encode(), capture_output=True, timeout=10)
    malformed_replies = [json.loads(line) for line in malformed_server.stdout.splitlines()]
    assert malformed_server.returncode == 0 and [reply.get("id") for reply in malformed_replies] == [1, None, 3]
    eof_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n{bad json}").encode(), capture_output=True, timeout=10)
    eof_replies = [json.loads(line) for line in eof_server.stdout.splitlines()]
    assert eof_server.returncode == 0 and eof_replies[-1]["error"]["code"] == -32700

    # Quotes remain escaped by JSON even when the bounded response encoder
    # leaves HTML characters alone. Fill the admitted request close enough to
    # the frame cap that the response envelope itself crosses the cap.
    large_uri_length = frame_limit // 2
    while True:
        large_uri = '"' * large_uri_length
        response_message = {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": "", "uri": large_uri}}}
        response_line = json.dumps(response_message, separators=(",", ":"))
        if len(response_line.encode()) <= frame_limit:
            break
        large_uri_length -= 1
    response_request = init_line + "\n" + ready_line + "\n" + response_line + "\n" + ping_line + "\n"
    response_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=response_request.encode(), capture_output=True, timeout=60)
    response_replies = [json.loads(line) for line in response_server.stdout.splitlines()]
    assert response_server.returncode == 0 and response_replies[-1]["result"] == {}
    assert response_replies[1]["result"]["isError"] and "encoded bytes" in response_replies[1]["result"]["content"][0]["text"]

    raw_id = "<" * (3 * 1024 * 1024)
    raw_id_message = {"jsonrpc": "2.0", "id": raw_id, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}}
    raw_id_line = json.dumps(raw_id_message, separators=(",", ":"))
    id_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n" + raw_id_line + "\n" + ping_line + "\n").encode(), capture_output=True, timeout=60)
    id_replies = [json.loads(line) for line in id_server.stdout.splitlines()]
    assert id_server.returncode == 0 and id_replies[-1]["result"] == {}
    assert id_replies[1]["id"] == raw_id and "\\u003c" not in id_server.stdout.decode()

    near_limit_template = {"jsonrpc": "2.0", "id": "", "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}}
    near_limit_template_line = json.dumps(near_limit_template, separators=(",", ":"))
    near_limit_id = "a" * (frame_limit - len(near_limit_template_line.encode()))
    near_limit_message = {"jsonrpc": "2.0", "id": near_limit_id, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}}
    near_limit_line = json.dumps(near_limit_message, separators=(",", ":"))
    assert len(near_limit_line.encode()) == frame_limit
    near_limit_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n" + near_limit_line + "\n" + ping_line + "\n").encode(), capture_output=True, timeout=60)
    near_limit_replies = [json.loads(line) for line in near_limit_server.stdout.splitlines()]
    assert near_limit_server.returncode == 0 and [reply.get("id") for reply in near_limit_replies] == [1, near_limit_id, 3]
    assert near_limit_replies[1]["error"]["code"] == -32000 and near_limit_replies[1]["error"]["message"] == "too big"

    def exact_scalar_id_line(prefix, suffix, scalar):
        available = frame_limit - len((prefix + suffix).encode())
        scalar_width = len(scalar.encode())
        repetitions = available // scalar_width
        remainder = available - repetitions * scalar_width
        identifier = scalar * repetitions + "a" * remainder
        line = prefix + identifier + suffix
        assert len(line.encode()) == frame_limit
        return line, identifier

    valid_prefix = '{"jsonrpc":"2.0","id":"'
    valid_suffix = '","method":"tools/call","params":{"name":"code.format","arguments":{"source":""}}}'
    for separator in ("\u2028", "\u2029"):
        separator_line, separator_id = exact_scalar_id_line(valid_prefix, valid_suffix, separator)
        separator_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n" + separator_line + "\n" + ping_line + "\n").encode(), capture_output=True, timeout=60)
        separator_replies = [json.loads(line) for line in separator_server.stdout.splitlines()]
        assert separator_server.returncode == 0 and [reply.get("id") for reply in separator_replies] == [1, separator_id, 3]
        assert separator_replies[1]["error"]["code"] == -32000 and separator_replies[1]["error"]["message"] == "too big"
        assert all(len(line) <= frame_limit for line in separator_server.stdout.splitlines())

    missing_prefix = '{"jsonrpc":"2.0","id":"'
    missing_suffix = '","method":"tools/call","params":{"name":"code.format"}}'
    missing_line, missing_id = exact_scalar_id_line(missing_prefix, missing_suffix, "a")
    missing_minus_line = missing_prefix + missing_id[:-1] + missing_suffix
    for candidate, candidate_id in ((missing_line, missing_id), (missing_minus_line, missing_id[:-1])):
        missing_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n" + candidate + "\n" + ping_line + "\n").encode(), capture_output=True, timeout=60)
        missing_replies = [json.loads(line) for line in missing_server.stdout.splitlines()]
        assert missing_server.returncode == 0 and [reply.get("id") for reply in missing_replies] == [1, candidate_id, 3]
        assert missing_replies[1]["error"]["code"] == -32602 and missing_replies[1]["error"]["message"] == "too big"

    unknown_prefix = '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"code.format","arguments":{"source":"","'
    unknown_suffix = '":""}}}'
    unknown_key_bytes = frame_limit - len((unknown_prefix + unknown_suffix).encode())
    unknown_key = "\u2028" * (unknown_key_bytes // 3) + "a" * (unknown_key_bytes % 3)
    unknown_line = unknown_prefix + unknown_key + unknown_suffix
    assert len(unknown_line.encode()) == frame_limit
    unknown_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input=(init_line + "\n" + ready_line + "\n" + unknown_line + "\n" + ping_line + "\n").encode(), capture_output=True, timeout=60)
    unknown_replies = [json.loads(line) for line in unknown_server.stdout.splitlines()]
    assert unknown_server.returncode == 0 and [reply.get("id") for reply in unknown_replies] == [1, 2, 3]
    assert unknown_replies[1]["error"]["code"] == -32602 and unknown_replies[1]["error"]["message"] == "too big"

    variant_messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "format-id-variants", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": -0.0, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}},
        {"jsonrpc": "2.0", "id": None, "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}},
        {"jsonrpc": "2.0", "id": 'quote"\u0001', "method": "tools/call", "params": {"name": "code.format", "arguments": {"source": ""}}},
        {"jsonrpc": "2.0", "id": 3, "method": "ping"},
    ]
    variant_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input="\n".join(json.dumps(message) for message in variant_messages) + "\n", text=True, capture_output=True, timeout=10)
    variant_replies = [json.loads(line) for line in variant_server.stdout.splitlines()]
    assert variant_server.returncode == 0 and [reply.get("id") for reply in variant_replies] == [1, 0.0, None, 'quote"\u0001', 3]
    assert all(reply.get("result", {}).get("structuredContent", {}).get("origin") == "buffer" for reply in variant_replies[1:4])

    invalid_disk = workspace / "invalid-utf8.ef"
    invalid_disk.write_bytes(b"// invalid \xff\neffect fn main() -> void { void }\n")
    invalid_messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "format-utf8", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": {"name": "code.format", "arguments": {"file": "invalid-utf8.ef"}}},
        {"jsonrpc": "2.0", "id": 3, "method": "ping"},
    ]
    invalid_server = subprocess.run([str(ef), "mcp", str(workspace)], cwd=root, input="\n".join(json.dumps(message) for message in invalid_messages) + "\n", text=True, capture_output=True, timeout=10)
    invalid_replies = [json.loads(line) for line in invalid_server.stdout.splitlines()]
    assert invalid_server.returncode == 0 and invalid_replies[1]["result"]["isError"] and "not valid UTF-8" in invalid_replies[1]["result"]["content"][0]["text"] and invalid_replies[2]["result"] == {}

print("formatter CLI and MCP process adapters: passed")
