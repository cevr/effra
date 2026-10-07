"""Drive the actual generated HTTP executable and verify cooperative signal shutdown."""
import pathlib, selectors, signal, subprocess, urllib.request, urllib.error

root = pathlib.Path(__file__).resolve().parents[1]
binary = subprocess.check_output([str(root / "bin/ef"), "build", "examples/http.ef"], cwd=root, text=True).strip()
process = subprocess.Popen([str(root / binary)], cwd=root, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
try:
    selector = selectors.DefaultSelector()
    selector.register(process.stdout, selectors.EVENT_READ)
    assert selector.select(timeout=10), "server did not bind"
    line = process.stdout.readline().strip()
    selector.close()
    assert line.startswith("listening http://"), line
    base = line.removeprefix("listening ")
    for path, expected in [("/health", "ok"), ("/users/42", "Hello, Ada"), ("/users/slow", "Hello, timed out"), ("/file", "scoped file read complete\n")]:
        with urllib.request.urlopen(base + path, timeout=5) as response:
            assert response.status == 200 and response.read().decode() == expected, path
            assert response.headers["Content-Type"] == "text/plain; charset=utf-8", (path, response.headers)
    # The managed transport keeps the target's percent-encoding: an encoded
    # /health is not the health route.
    with urllib.request.urlopen(base + "/%68ealth", timeout=5) as response:
        assert response.read().decode() == "Hello, Ada"
    # A declared request failure is an empty 500 without a content type.
    try:
        urllib.request.urlopen(base + "/users/missing", timeout=5)
        raise AssertionError("unhandled request failure did not produce HTTP 500")
    except urllib.error.HTTPError as error:
        assert error.code == 500 and error.read() == b"" and error.headers["Content-Type"] is None, error.headers
        error.close()
    # Routes take no body: a declared one beyond maxBodyBytes is refused unhandled.
    try:
        urllib.request.urlopen(urllib.request.Request(base + "/health", data=b"x"), timeout=5)
        raise AssertionError("oversized request body was admitted")
    except urllib.error.HTTPError as error:
        assert error.code == 413 and error.read() == b"", error.code
        error.close()
    process.send_signal(signal.SIGTERM)
    _, stderr = process.communicate(timeout=10)
    assert process.returncode == 1 and "context canceled" in stderr, stderr
    print("HTTP routes, encoded path, failure boundary, body bound, file scope, timeout and SIGTERM shutdown: passed")
finally:
    if process.poll() is None:
        process.terminate()
        try:
            process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate()
