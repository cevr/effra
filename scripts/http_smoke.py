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
    try:
        urllib.request.urlopen(base + "/users/missing", timeout=5)
        raise AssertionError("unhandled request failure did not produce HTTP 500")
    except urllib.error.HTTPError as error:
        assert error.code == 500
        error.close()
    process.send_signal(signal.SIGTERM)
    _, stderr = process.communicate(timeout=10)
    assert process.returncode == 1 and "context canceled" in stderr, stderr
    print("HTTP routes, failure boundary, file scope, timeout and SIGTERM shutdown: passed")
finally:
    if process.poll() is None:
        process.terminate()
        try:
            process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            process.kill()
            process.communicate()
