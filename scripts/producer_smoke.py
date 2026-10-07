#!/usr/bin/env python3
"""Producer image identity and stale-fact controls; run under validation flock."""
import hashlib
import json
import os
from pathlib import Path
import selectors
import shutil
import subprocess
import tempfile
import time

from smoke_support import adapter_semantic, assert_report_parity

root = Path(__file__).resolve().parents[1]


def run(*args, cwd=root):
    result = subprocess.run(args, cwd=cwd, capture_output=True, text=True, timeout=120)
    assert result.returncode == 0, (args, result.stdout, result.stderr)
    return result.stdout


def digest(path):
    with path.open("rb") as image:
        return "sha256:" + hashlib.file_digest(image, "sha256").hexdigest()


class Server:
    def __init__(self, binary, workspace):
        self.process = subprocess.Popen([str(binary), "mcp", str(workspace)], cwd=root,
                                        stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, bufsize=0)
        self.index = 0
        self.stdout_buffer = bytearray()
        self.request("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                    "clientInfo": {"name": "producer-control", "version": "1"}})
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def send(self, message):
        self.process.stdin.write((json.dumps(message) + "\n").encode("utf-8"))
        self.process.stdin.flush()

    def request(self, method, params=None):
        self.index += 1
        self.send({"jsonrpc": "2.0", "id": self.index, "method": method, "params": params or {}})
        response = self.receive()
        assert response["id"] == self.index and "error" not in response, response
        return response["result"]

    def receive(self):
        deadline = time.monotonic() + 15
        while True:
            newline = self.stdout_buffer.find(b"\n")
            if newline >= 0:
                line = bytes(self.stdout_buffer[:newline])
                del self.stdout_buffer[:newline + 1]
                return json.loads(line)
            with selectors.DefaultSelector() as selector:
                selector.register(self.process.stdout, selectors.EVENT_READ)
                remaining = deadline - time.monotonic()
                assert remaining > 0 and selector.select(remaining), "producer control response deadline"
            chunk = os.read(self.process.stdout.fileno(), 65536)
            assert chunk, "server ended before producer response"
            self.stdout_buffer.extend(chunk)

    def tool(self, name, **arguments):
        return self.request("tools/call", {"name": name, "arguments": arguments})

    def close(self):
        self.process.stdin.close()
        self.process.wait(timeout=15)
        error = self.process.stderr.read()
        assert self.process.returncode == 0 and not error, error


def facts(server, name="project.check", **args):
    result = server.tool(name, file="main.ef", **args)
    assert not result.get("isError"), result
    return result["structuredContent"]


def build_overlay(directory, name, source, replacement):
    modified = directory / (name + ".txt")
    modified.write_text(replacement)
    overlay = directory / (name + ".json")
    overlay.write_text(json.dumps({"Replace": {str(source): str(modified)}}))
    binary = directory / name
    run("go", "build", "-overlay", str(overlay), "-o", str(binary), "./cmd/ef")
    return binary


with tempfile.TemporaryDirectory(prefix="effra-producer-") as temporary:
    directory = Path(temporary)
    source = 'effect fn main() -> string { "ok" }\n'
    file = directory / "main.ef"
    file.write_text(source)
    main = root / "cmd/ef/main.go"
    original = main.read_text()
    assert "Effra prototype\\nusage:" in original
    first = build_overlay(directory, "compiler-A", main,
                          original.replace("Effra prototype\\nusage:", "Effra producer-A\\nusage:"))
    second = build_overlay(directory, "compiler-B", main,
                           original.replace("Effra prototype\\nusage:", "Effra producer-B\\nusage:"))
    first_digest, second_digest = digest(first), digest(second)
    assert first_digest != second_digest

    # First acquisition occurs after pathname replacement, so a pathname hash
    # cannot accidentally pass by having been cached before the replacement.
    installed = directory / "ef"
    shutil.copyfile(first, installed)
    installed.chmod(0o755)
    old = Server(installed, directory)
    try:
        replacement = directory / "replacement"
        shutil.copyfile(second, replacement)
        replacement.chmod(0o755)
        os.replace(replacement, installed)
        original_facts = facts(old)
        assert original_facts["producer"]["digest"] == first_digest
        assert original_facts["producer"]["strength"] == "executing-artifact"
        key, revision = original_facts["producer"]["qualifier"], original_facts["revision"]
        for target in ("go", "js"):
            selected = facts(old, "code.type", target=target, symbol="main",
                             expectedRevision=revision, expectedProducer=key)
            assert selected["producer"] == original_facts["producer"]
            assert selected["snapshot"] == {"schemaVersion": selected["schemaVersion"],
                                              "revision": revision, "target": target,
                                              "producer": key, "reuseScope": "artifact"}
            for name, args in [("project.check", {}), ("code.inspect", {"symbol": "main"}),
                               ("code.typeAt", {"offset": source.index('"ok"')}),
                               ("project.graph", {}), ("project.diagnostics", {}), ("project.lint", {})]:
                value = facts(old, name, target=target, expectedRevision=revision,
                              expectedProducer=key, **args)
                if name == "project.lint":
                    value = value["lint"]
                assert value["producer"] == original_facts["producer"]
                assert value["snapshot"] == {"revision": revision, "target": target,
                                              "schemaVersion": original_facts["schemaVersion"],
                                              "producer": key, "reuseScope": "artifact"}
            # CLI acquisition is an independent process of the same artifact;
            # adapter parity must follow content identity, not process ID.
            for command, args in [("check", []), ("inspect", ["main"]),
                                  ("query", [str(source.index('"ok"'))]),
                                  ("type", ["--symbol", "main"]),
                                  ("graph", []), ("diagnostics", ["--json"]), ("lint", [])]:
                encoded = run(str(first), command, str(file), *args, "--target", target)
                cli = json.loads(encoded)
                assert cli["producer"] == original_facts["producer"] and cli["snapshot"]["producer"] == key
                if command == "graph":
                    assert cli["typeProjectionUsage"]["responseBytes"] == len(encoded.rstrip("\n").encode("utf-8"))
                if command == "type":
                    assert cli["snapshot"] == selected["snapshot"]
                    assert cli["querySchemaVersion"] == selected["querySchemaVersion"] == 2
        profile_source = '''import Fns "effra/functions"
error MissingProfile
service Profiles { effect fn name(id: string) -> string raises {MissingProfile} }
effect fn load(id: string) -> string raises {MissingProfile} uses {Profiles} { run Profiles.name(id) }
effect fn greeting(id: string) -> string raises {MissingProfile} uses {Profiles} { run Fns.call(load, id) }
'''
        profile = directory / "profile.ef"
        profile.write_text(profile_source)
        for target in ("go", "js"):
            remote = old.tool("code.inspect", file="profile.ef", symbol="greeting", target=target,
                              expectedProducer=key)["structuredContent"]
            cli = json.loads(run(str(first), "inspect", str(profile), "greeting", "--target", target))
            assert_report_parity(remote, cli, target=target,
                                 ignored=("file", "timings"), project=adapter_semantic)
            assert remote["revision"] != revision
            assert remote["bundledInterfaces"] and remote["symbol"]["contract"]["requirements"] == ["Profiles"]
        formatted = old.tool("code.format", source=source, expectedProducer=key)["structuredContent"]
        assert formatted["producer"] == original_facts["producer"] and formatted["formatterVersion"]
        cli_format = subprocess.run([str(first), "fmt", str(file), "--check", "--json"], capture_output=True, text=True, timeout=15)
        assert cli_format.returncode in (0, 1)
        assert json.loads(cli_format.stdout)["producer"] == formatted["producer"]
        fresh = Server(installed, directory)
        try:
            current = facts(fresh)
            assert current["producer"]["digest"] == second_digest
            assert current["revision"] == revision
            assert current["producer"]["declaration"] == original_facts["producer"]["declaration"]
            assert current["producerIdentity"] == original_facts["producerIdentity"]
            queued = []
            for target in ("go", "js"):
                stale_id, ping_id = fresh.index + 1, fresh.index + 2
                fresh.index = ping_id
                fresh.send({"jsonrpc": "2.0", "id": stale_id, "method": "tools/call",
                            "params": {"name": "code.type", "arguments": {
                                "file": "main.ef", "target": target, "symbol": "main",
                                "expectedRevision": current["revision"], "expectedProducer": key}}})
                fresh.send({"jsonrpc": "2.0", "id": ping_id, "method": "ping"})
                queued.extend((stale_id, ping_id))
            responses = {response["id"]: response for response in
                         (fresh.receive(), fresh.receive(), fresh.receive(), fresh.receive())}
            for stale_id, ping_id in zip(queued[::2], queued[1::2]):
                refusal = responses[stale_id]["result"]
                assert refusal["isError"] and "stale producer" in refusal["content"][0]["text"]
                assert responses[ping_id]["result"] == {}
            rejected = fresh.tool("code.typeAt", file="main.ef", offset=source.index('"ok"'),
                                  expectedRevision=revision, expectedProducer=key)
            assert rejected["isError"] and "stale producer" in rejected["content"][0]["text"]
            assert fresh.request("ping") == {}
            assert facts(old)["producer"]["digest"] == first_digest
        finally:
            fresh.close()
    finally:
        old.close()

    # The production acquisition owner is copied into a tiny real Go host,
    # rather than duplicating a repository to create distinct dirty builds.
    fixture = directory / "host"
    package = fixture / "internal/producer"
    package.mkdir(parents=True)
    shutil.copyfile(root / "internal/producer/identity.go", package / "identity.go")
    (fixture / "go.mod").write_text("module effra.local/prototype\n\ngo 1.27\n")
    host = '''package main
import("encoding/json";"os";"effra.local/prototype/internal/producer")
const marker="baseline"
func main(){json.NewEncoder(os.Stdout).Encode(map[string]any{"marker":marker,"producer":producer.Current()})}
'''
    (fixture / "main.go").write_text(host)
    run("git", "init", "-q", cwd=fixture)
    run("git", "-c", "user.name=Effra fixture", "-c", "user.email=fixture@invalid", "add", ".", cwd=fixture)
    run("git", "-c", "user.name=Effra fixture", "-c", "user.email=fixture@invalid", "commit", "-qm",
        "test: retain producer fixture baseline", cwd=fixture)
    identities = []
    for marker in ("dirty-A", "dirty-B"):
        (fixture / "main.go").write_text(host.replace("baseline", marker))
        binary = directory / marker
        run("go", "build", "-o", str(binary), ".", cwd=fixture)
        result = json.loads(run(str(binary)))
        assert result["marker"] == marker
        identity = result["producer"]
        assert identity["digest"] == digest(binary) and identity["declaration"]["vcsModified"] == "true"
        identities.append(identity)
    assert identities[0]["declaration"] == identities[1]["declaration"]
    assert identities[0]["digest"] != identities[1]["digest"]

    # Explicit unavailable identity is stable within a process and refuses a
    # qualifier from another process. No installation-path fallback exists.
    owner = root / "internal/producer/identity.go"
    implementation = owner.read_text()
    unavailable = implementation.replace('\n\t"os"', '').replace('return os.Open("/proc/self/exe")', 'return nil, errUnsupported')
    assert unavailable != implementation
    fallback = build_overlay(directory, "unavailable", owner, unavailable)
    processes = [Server(fallback, directory), Server(fallback, directory)]
    try:
        before, other = [facts(server, target="go") for server in processes]
        assert_report_parity(before, other, target="go", ignored=("file", "timings"),
                             project=adapter_semantic)
        id_before, id_other = before["producer"], other["producer"]
        assert id_before["strength"] == "unavailable" and id_before["reuseScope"] == "process"
        assert "digest" not in id_before and id_before["qualifier"] != id_other["qualifier"]
        same = facts(processes[0], target="go", expectedProducer=id_before["qualifier"])
        assert same["producer"] == before["producer"], same
        assert same["snapshot"] == before["snapshot"], same
        assert_report_parity(same, before, target="go", ignored=("file", "timings"),
                             project=adapter_semantic)
        for target in ("go", "js"):
            first_report = facts(processes[0], target=target)
            second_report = facts(processes[1], target=target)
            assert_report_parity(first_report, second_report, target=target,
                                 ignored=("file", "timings"), project=adapter_semantic)
            cli_report = json.loads(run(str(fallback), "check", str(file), "--target", target))
            assert_report_parity(cli_report, first_report, target=target,
                                 ignored=("file", "timings"), project=adapter_semantic)
        refused = processes[1].tool("project.check", file="main.ef", expectedProducer=id_before["qualifier"])
        assert refused["isError"] and processes[1].request("ping") == {}
    finally:
        for server in processes:
            server.close()

    # A causal sentinel proves compile/emit/build/run do not acquire identity.
    lazy_source = implementation.replace('func Current() Identity { return current() }',
                                         'func Current() Identity { panic("producer acquisition forbidden") }')
    assert lazy_source != implementation
    lazy = build_overlay(directory, "lazy", owner, lazy_source)
    run(str(lazy), "build", str(file), "--target", "js", "-o", str(directory / "app.mjs"))
    assert run(str(lazy), "run", str(file), "--target", "go") == "ok\n"
    result = subprocess.run([str(lazy), "check", str(file)], capture_output=True, text=True, timeout=15)
    assert result.returncode != 0 and "producer acquisition forbidden" in result.stderr

print("producer artifact replacement, dirty hosts, unavailable reuse and lazy compilation: passed")
