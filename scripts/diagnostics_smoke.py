#!/usr/bin/env python3
"""Public diagnostic snapshots, editor ranges and admission boundaries."""
import argparse
import hashlib
import json
import os
import pathlib
import signal
import subprocess
import sys
import tempfile
import time

from smoke_support import assert_report_parity

ROOT = pathlib.Path(__file__).resolve().parents[1]


def requests(arguments):
    messages = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
            "protocolVersion": "2025-11-25", "capabilities": {},
            "clientInfo": {"name": "diagnostics-smoke", "version": "1"}}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
    ]
    messages.extend({"jsonrpc": "2.0", "id": index + 2, "method": "tools/call",
                     "params": {"name": "project.diagnostics", "arguments": value}}
                    for index, value in enumerate(arguments))
    messages.append({"jsonrpc": "2.0", "id": "after", "method": "ping"})
    return "\n".join(map(json.dumps, messages)) + "\n"


def mcp(binary, directory, arguments):
    process = subprocess.run([binary, "mcp", str(directory)], input=requests(arguments),
                             text=True, capture_output=True, timeout=30)
    assert process.returncode == 0, process.stderr
    replies = list(map(json.loads, process.stdout.splitlines()))
    assert len(replies) == len(arguments) + 2, replies
    assert replies[-1] == {"jsonrpc": "2.0", "id": "after", "result": {}}, replies[-1]
    return replies[1:-1]


def cli(binary, path, strict=False):
    arguments = [binary, "diagnostics", str(path), "--json"]
    if strict:
        arguments.append("--strict")
    process = subprocess.run(arguments, text=True, capture_output=True, timeout=30)
    assert process.returncode in (0, 1), (process.returncode, process.stderr)
    report = json.loads(process.stdout)
    assert process.returncode == (0 if report["policyPassed"] else 1), report
    return report


def write(directory, name, source):
    path = directory / name
    path.write_bytes(source.encode("utf-8"))
    return path


def editor_position(source, byte_offset):
    # Independent protocol oracle: decode the known byte prefix, normalize
    # editor line endings, then count UTF-16 code units on its final line.
    prefix = source.encode("utf-8")[:byte_offset].decode("utf-8")
    lines = prefix.replace("\r\n", "\n").replace("\r", "\n").split("\n")
    return {"line": len(lines) - 1,
            "character": len(lines[-1].encode("utf-16-le")) // 2}


def positions(binary, directory):
    warning = ('effect fn task() -> string { "ok" }\r\n'
               'effect fn main() -> string { let s = "𐐀é"; '
               'let forgotten = task(); run task().provide<Console>(Stdout) }\r\n')
    fixtures = [
        ("unicode.ef", 'effect fn main() -> () { "𐐀é" @ }', [("EF001", "@", 1, "error", 1)]),
        ("crlf.ef", '// comment\r\neffect fn main() -> () { () }\r\n@', [("EF001", "@", 1, "error", 1)]),
        ("eof.ef", 'effect fn main() -> () {\r\n', [("EF002", None, 0, "error", 1)]),
        ("warning.ef", warning, [("EFL001", "let forgotten", 3, "warning", 2),
                                ("EFL002", "provide", 7, "hint", 4)]),
        ("suppression.ef", '// effra-lint-disable-next-line bogus -- reason\r\n'
         'effect fn main() -> () { () }', [("EFL004", "//", None, "error", 1)]),
        ("cr-code.ef", 'effect fn main() -> () { () }\r@', [("EF001", "\r", 1, "error", 1)]),
        ("cr-comment.ef", '// comment\r@ effect fn main() -> () { () }\n', [("EF001", "\r", 1, "error", 1)]),
    ]
    for name, source, expected in fixtures:
        path = write(directory, name, source)
        report = cli(binary, path)
        remote = mcp(binary, directory, [{"file": name}])[0]["result"]["structuredContent"]
        assert_report_parity(report, remote, report_schema=1, snapshot_schema=6)
        assert report["revision"] == hashlib.sha256(source.encode()).hexdigest(), name
        assert report["source"] == {"uri": path.as_uri(), "origin": "disk"}, report
        findings = report["diagnostics"]
        assert len(findings) == len(expected), (name, findings)
        for finding, (code, anchor, length, severity, numeric) in zip(findings, expected):
            offset = len(source.encode()) if anchor is None else source.encode().index(anchor.encode())
            if length is None:
                length = source.encode().index(b"\r\n") - offset
            assert finding["code"] == code and finding["severity"] == severity, (name, finding)
            assert finding["origin"] == ("lint" if code.startswith("EFL") else "compiler"), finding
            assert finding["span"]["offset"] == offset and finding["span"]["length"] == length, (name, finding)
            assert finding["locationAvailable"], finding
            assert finding["lsp"]["severity"] == numeric, finding
            assert finding["lsp"]["range"] == {"start": editor_position(source, offset),
                                                 "end": editor_position(source, offset + length)}, (name, finding)
        if name.startswith("cr-"):
            assert "use LF or CRLF" in findings[0]["message"], findings
        if name == "warning.ef":
            strict = cli(binary, path, strict=True)
            assert report["policyPassed"] and not strict["policyPassed"], strict
            assert report["diagnostics"] == strict["diagnostics"], strict
            strict_remote = mcp(binary, directory, [{"file": name, "strict": True}])[0]["result"]["structuredContent"]
            assert_report_parity(strict_remote, strict, report_schema=1, snapshot_schema=6)

    for name, source, checked in [
        ("escaped-cr.ef", 'effect fn main() -> string { "\\r" }', True),
        ("hidden-error.ef", '// effra-lint-disable-next-line bogus -- reason\n'
         'effect fn main() -> () { run Console.log("x") }', False),
        ("suppressed.ef", 'effect fn task() -> string { "ok" }\n'
         'effect fn main() -> string {\n'
         '// effra-lint-disable-next-line unused-recipe -- intentional\n'
         'let forgotten = task()\n"ok"\n}', True),
    ]:
        report = cli(binary, write(directory, name, source), strict=True)
        remote = mcp(binary, directory, [{"file": name, "strict": True}])[0]["result"]["structuredContent"]
        assert_report_parity(report, remote, report_schema=1, snapshot_schema=6)
        assert report["checked"] == checked, report
        if checked:
            assert report["policyPassed"] and report["diagnostics"] == [], report
        else:
            assert not report["lintAvailable"] and report["lintUnavailableReason"], report
            assert any(f["code"] == "EF108" for f in report["diagnostics"]), report
            assert all(f["origin"] == "compiler" for f in report["diagnostics"]), report


def bounds(binary, directory):
    for count in (100, 101):
        write(directory, f"findings-{count}.ef", 'fn duplicate() -> () { () }\n' * (count + 1))
    limit = 2 * 1024 * 1024
    for size in (limit, limit + 1):
        write(directory, f"size-{size}.ef", "//" + "x" * (size - 2))
    (directory / "directory.ef").mkdir()
    arguments = [{"file": name} for name in (
        "findings-100.ef", "findings-101.ef", f"size-{limit}.ef", f"size-{limit+1}.ef", "directory.ef")]
    if hasattr(os, "mkfifo"):
        os.mkfifo(directory / "fifo.ef")
        arguments.append({"file": "fifo.ef"})
    replies = mcp(binary, directory, arguments)
    report = replies[0]["result"]["structuredContent"]
    assert report["returnedCount"] == report["totalCounts"]["errors"] == 100, report["totalCounts"]
    assert not report["policyPassed"] and not report["truncated"], "exact-limit result was incomplete or passed policy"
    assert replies[1]["result"].get("isError") and "limit" in replies[1]["result"]["content"][0]["text"], "101 findings were not rejected"
    assert replies[2]["result"]["structuredContent"]["returnedCount"] == 0, "exact source size was rejected or fabricated a finding"
    for argument, reply in zip(arguments[3:], replies[3:]):
        assert reply["result"].get("isError"), f"source admission accepted {argument['file']}"


def identity(binary, directory):
    # Lexical normalization must govern both the document identity and its bytes.
    dots = directory / "dots"
    (dots / "real" / "inner").mkdir(parents=True)
    (dots / "link").symlink_to("real/inner", target_is_directory=True)
    lexical = write(dots, "b.ef", 'effect fn main() -> () { () }')
    write(dots / "real", "b.ef", 'effect fn main() -> () { run Console.log("x") }')
    expected = cli(binary, lexical)
    requested = dots / "link" / ".." / "b.ef"
    reports = (cli(binary, requested), mcp(binary, directory, [{"file": "dots/link/../b.ef"}])[0]["result"]["structuredContent"])
    for report in reports:
        assert_report_parity(report, expected, target="go", report_schema=1, snapshot_schema=6)

    original = write(directory, "one.ef", 'import go fmt "fmt"\neffect fn main() -> () { () }')
    replacement = write(directory, "two.ef", 'effect fn main() -> () { run Console.log("x") }')
    link = directory / "selected.ef"
    link.symlink_to(original.name)
    baseline = cli(binary, original)
    for report in (cli(binary, link), mcp(binary, directory, [{"file": link.name}])[0]["result"]["structuredContent"]):
        assert report["source"]["uri"] == link.as_uri(), report
        assert report["revision"] == baseline["revision"] and report["diagnostics"] == baseline["diagnostics"], report

    if sys.platform != "linux":
        return  # /proc synchronization is test-only; the static URI control remains portable.
    for surface in ("cli", "mcp"):
        changing = directory / f"changing-{surface}.ef"
        changing.symlink_to(original.name)
        command = [binary, "diagnostics", str(changing), "--json"] if surface == "cli" else [binary, "mcp", str(directory)]
        with subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, text=True, start_new_session=True) as process:
            try:
                if surface == "mcp":
                    process.stdin.write(requests([{"file": changing.name}]))
                    process.stdin.flush()
                deadline = time.monotonic() + 15
                def has_children():
                    for children in pathlib.Path(f"/proc/{process.pid}/task").glob("*/children"):
                        try:
                            if children.read_text().strip():
                                return True
                        except FileNotFoundError:
                            pass  # An OS thread may finish while inspecting its child list.
                    return False

                while not has_children():
                    assert process.poll() is None and time.monotonic() < deadline, "Go import never started"
                    time.sleep(0.001)  # Poll a causal child-start condition, not a correctness delay.
                new_link = directory / f"replacement-{surface}.ef"
                new_link.symlink_to(replacement.name)
                os.replace(new_link, changing)
                stdout, stderr = process.communicate(timeout=30)
                assert process.returncode == 0, stderr
                if surface == "cli":
                    report = json.loads(stdout)
                else:
                    replies = list(map(json.loads, stdout.splitlines()))
                    assert replies[-1]["id"] == "after", replies
                    report = replies[1]["result"]["structuredContent"]
                assert report["source"]["uri"] == changing.as_uri(), report
                assert report["checked"] and report["revision"] == baseline["revision"], report
                assert report["diagnostics"] == baseline["diagnostics"], report
            finally:
                if process.poll() is None:
                    os.killpg(process.pid, signal.SIGTERM)
                    try:
                        process.communicate(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(process.pid, signal.SIGKILL)
                        process.communicate(timeout=5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", default=str(ROOT / "bin/ef"))
    parser.add_argument("--group", choices=("positions", "bounds", "identity"))
    args = parser.parse_args()
    for name, check in (("positions", positions), ("bounds", bounds), ("identity", identity)):
        if args.group and args.group != name:
            continue
        with tempfile.TemporaryDirectory(prefix="effra-diagnostics-") as temporary:
            check(args.binary, pathlib.Path(temporary))
        print(f"diagnostics {name}: passed")


if __name__ == "__main__":
    main()
