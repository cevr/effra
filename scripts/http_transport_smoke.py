"""Drive examples/http-transport.ef over real sockets on the Go target and on
the JS target under Bun and Node.

Every case observes exact wire status/body/content-type. Cancellation is proved
causally: the example admits one active request, so the admission slot held by
/slow (a 60s handler) is released only after its request scope is cancelled and
closed.
"""
import os, pathlib, selectors, shutil, signal, socket, subprocess, tempfile, time

root = pathlib.Path(__file__).resolve().parents[1]
example = "examples/http-transport.ef"


def start(target, scratch):
    if target == "go":
        binary = subprocess.check_output([str(root / "bin/ef"), "build", example], cwd=root, text=True).strip()
        command = [str(root / binary)]
    else:
        module = subprocess.check_output([str(root / "bin/ef"), "build", example, "--target", "js", "--entry"], cwd=root, text=True).strip()
        # The module runs beside a link to the pinned node_modules, so Node
        # and Bun resolve the same Effect.
        hosted = pathlib.Path(scratch) / "http-transport.mjs"
        shutil.copyfile(root / module, hosted)
        if not (pathlib.Path(scratch) / "node_modules").exists():
            os.symlink(root / "node_modules", pathlib.Path(scratch) / "node_modules")
        host = shutil.which(target.removeprefix("js-"))
        assert host, target + " is required"
        command = [host, str(hosted)]
    process = subprocess.Popen(command, cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    selector = selectors.DefaultSelector()
    try:
        selector.register(process.stdout, selectors.EVENT_READ)
        assert selector.select(timeout=10), target + " server did not bind"
        line = process.stdout.readline().strip()
        assert line.startswith("listening http://127.0.0.1:"), line
        host, port = line.removeprefix("listening http://").rsplit(":", 1)
    except BaseException:
        # A server that is alive but never reports readiness must not outlive
        # the failed start.
        process.kill()
        process.communicate()
        raise
    finally:
        selector.close()
    return process, (host, int(port))


def connect(address):
    connection = socket.create_connection(address, timeout=5)
    connection.settimeout(5)
    return connection


def read_response(connection):
    """Return (status, headers, body), or None when the server closed the
    connection without sending any response bytes."""
    data = b""
    while b"\r\n\r\n" not in data:
        chunk = connection.recv(65536)
        if not chunk:
            assert data == b"", data
            return None
        data += chunk
    head, body = data.split(b"\r\n\r\n", 1)
    lines = head.decode("latin-1").split("\r\n")
    status = int(lines[0].split(" ")[1])
    headers = {}
    for line in lines[1:]:
        name, value = line.split(":", 1)
        headers[name.strip().lower()] = value.strip()
    length = int(headers.get("content-length", "0"))
    assert "transfer-encoding" not in headers, headers
    while len(body) < length:
        chunk = connection.recv(65536)
        assert chunk, "truncated body"
        body += chunk
    return status, headers, body


def exchange(address, raw):
    with connect(address) as connection:
        connection.sendall(raw)
        return read_response(connection)


def sequential(address, raw, deadline=3.0):
    """exchange() for a plain request that is not about admission. The
    admission slot is held until the previous response has been handed to the
    operating system (docs/runtime.md), so a client that reconnects inside
    that window may see 503; retry on a new connection until the deadline.
    Cases that assert saturation or shutdown 503 use exchange() and stay
    strict."""
    stop = time.monotonic() + deadline
    while True:
        response = exchange(address, raw)
        if response is None or response[0] != 503 or time.monotonic() >= stop:
            return response


def request(method, path, body=None, content_type=None, chunked=False):
    lines = [f"{method} {path} HTTP/1.1", "Host: effra"]
    if content_type is not None:
        lines.append("Content-Type: " + content_type)
    payload = b""
    if body is not None and chunked:
        lines.append("Transfer-Encoding: chunked")
        half = len(body) // 2
        for part in (body[:half], body[half:]):
            payload += f"{len(part):x}\r\n".encode() + part + b"\r\n"
        payload += b"0\r\n\r\n"
    elif body is not None:
        lines.append(f"Content-Length: {len(body)}")
        payload = body
    return ("\r\n".join(lines) + "\r\n\r\n").encode() + payload


def expect(response, status, body=b"", content_type=None, close=None):
    assert response is not None, f"expected {status}, connection closed without a response"
    actual, headers, actual_body = response
    assert actual == status and actual_body == body, (status, body, response)
    assert headers.get("content-type") == content_type, (content_type, headers)
    if close is not None:
        assert (headers.get("connection", "").lower() == "close") == close, headers


def hold_slot(address, deadline=3.0):
    """Occupy the single admission slot with /slow. A probe can briefly hold
    the slot itself, so a rejected /slow (it received a response) retries."""
    stop = time.monotonic() + deadline
    while True:
        slow = connect(address)
        slow.sendall(request("GET", "/slow"))
        while True:
            response = exchange(address, request("GET", "/health"))
            rejected = selectors.DefaultSelector()
            rejected.register(slow, selectors.EVENT_READ)
            answered = bool(rejected.select(timeout=0))
            rejected.close()
            if answered:
                slow.close()
                break
            if response is not None and response[0] == 503:
                return slow
            assert time.monotonic() < stop, f"/slow was not admitted: {response}"
        assert time.monotonic() < stop, "/slow was never admitted"


def poll_health(address, status, deadline=3.0):
    """Wait for /health to report status; it is 503 exactly while the single
    admission slot is held."""
    stop = time.monotonic() + deadline
    while True:
        response = exchange(address, request("GET", "/health"))
        if response is not None and response[0] == status:
            return
        assert time.monotonic() < stop, f"/health did not become {status}: {response}"


def check(target, scratch):
    process, address = start(target, scratch)
    try:
        text = "text/plain; charset=utf-8"
        octets = "application/octet-stream"
        expect(sequential(address, request("GET", "/health")), 200, b"ok", text)
        # The selected profile answers a wrong method on a known path with 404.
        expect(sequential(address, request("POST", "/health", b"")), 404)
        expect(sequential(address, request("GET", "/missing?x=/health")), 404)
        expect(sequential(address, request("POST", "/echo", b"\xff\x00binary", octets)), 200, b"\xff\x00binary", octets)
        expect(sequential(address, request("POST", "/echo", b"text", "text/plain")), 415)
        expect(sequential(address, request("GET", "/malformed")), 400)
        expect(sequential(address, request("GET", "/unavailable")), 500)
        # A response header value outside the shared policy (visible ASCII,
        # space, tab) fails closed, and the server keeps serving.
        expect(sequential(address, request("GET", "/invalid")), 500)
        expect(sequential(address, request("GET", "/health")), 200, b"ok", text)
        # Absolute-form targets: the query is not part of the path, even when
        # it contains a slash.
        expect(sequential(address, request("GET", "http://effra?next=/health")), 404)
        expect(sequential(address, request("GET", "http://effra/health?next=/echo")), 200, b"ok", text)
        # Body bounds: 16 bytes are admitted; 17 are rejected before the handler.
        expect(sequential(address, request("POST", "/echo", b"x" * 16, octets)), 200, b"x" * 16, octets)
        expect(sequential(address, request("POST", "/echo", b"x" * 17, octets)), 413, close=True)
        expect(sequential(address, request("POST", "/echo", b"y" * 16, octets, chunked=True)), 200, b"y" * 16, octets)
        expect(sequential(address, request("POST", "/echo", b"y" * 17, octets, chunked=True)), 413, close=True)
        malformed = b"POST /echo HTTP/1.1\r\nHost: effra\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\nab\r\n0\r\n\r\n"
        expect(sequential(address, malformed), 400, close=True)
        started = time.monotonic()
        stalled = b"POST /echo HTTP/1.1\r\nHost: effra\r\nContent-Type: application/octet-stream\r\nContent-Length: 4\r\n\r\nx"
        stalled_response = exchange(address, stalled)
        assert stalled_response is None, f"stalled body produced a response: {stalled_response}"
        assert time.monotonic() - started < 3, "body read timeout did not close the connection"
        # Client disconnect cancels the active handler and releases admission.
        slow = hold_slot(address)
        slow.close()
        poll_health(address, 200)
        # A client that keeps its connection open after a malformed body's
        # 400 does not hold shutdown open: the transport closes that
        # connection itself once the 400 has been written.
        held = connect(address)
        held.sendall(malformed)
        expect(read_response(held), 400, close=True)
        # Shutdown with active work: the in-flight request receives 503 after
        # its scope closed, then the server completes with interruption
        # within its idleMillis drain grace.
        slow = hold_slot(address)
        signalled = time.monotonic()
        process.send_signal(signal.SIGTERM)
        expect(read_response(slow), 503, close=True)
        slow.close()
        _, stderr = process.communicate(timeout=10)
        assert time.monotonic() - signalled < 5, f"shutdown took {time.monotonic() - signalled:.2f}s"
        assert process.returncode == 1 and "interrupt" in stderr.lower(), (process.returncode, stderr)
        held.close()
        with socket.socket() as listener:
            # Client connections may linger in TIME_WAIT; a live listener would
            # still refuse this bind.
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(address)
            listener.listen()
        print(f"{target}: HTTP transport statuses, limits, disconnect and shutdown: passed")
    finally:
        if process.poll() is None:
            process.kill()
            process.communicate()


with tempfile.TemporaryDirectory() as scratch:
    for target in ("go", "js-bun", "js-node"):
        check(target, scratch)
