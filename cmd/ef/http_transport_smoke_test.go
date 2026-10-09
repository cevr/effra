package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Ported from scripts/http_transport_smoke.py: examples/http-transport.ef
// over real sockets on the Go target and on the JS target under Bun and Node.
//
// Every case observes exact wire status/body/content-type. Cancellation is
// proved causally: the example admits one active request, so the admission
// slot held by /slow (a 60s handler) is released only after its request scope
// is cancelled and closed.
//
// The polls below are bounded retries, never sleeps. The admission slot is
// held until the previous response has been handed to the operating system
// (docs/runtime.md), so plain sequential requests retry a 503 on a new
// connection for up to three seconds; cases that assert saturation or
// shutdown 503 stay strict.
func TestHTTPTransportSmokeStatusesLimitsDisconnectAndShutdown(t *testing.T) {
	t.Parallel()
	binary := buildTestCLI(t)
	inputs := []string{"examples/http-transport.ef", "go.mod", "effra.bindings.json"}
	t.Run("go", func(t *testing.T) {
		t.Parallel()
		workspace := smokeWorkspace(t, inputs...)
		stdout, stderr, code := runTestCLIDir(t, binary, workspace, "", "build", "examples/http-transport.ef")
		if code != 0 {
			t.Fatalf("build: exit %d\n%s%s", code, stdout, stderr)
		}
		httpTransportSmokeCheck(t, workspace, httpSmokeArtifact(workspace, stdout))
	})
	t.Run("js", func(t *testing.T) {
		t.Parallel()
		// The module runs beside the workspace's link to the pinned
		// node_modules, so Node and Bun resolve the same Effect.
		workspace := smokeWorkspace(t, inputs...)
		stdout, stderr, code := runTestCLIDir(t, binary, workspace, "", "build", "examples/http-transport.ef", "--target", "js", "--entry")
		if code != 0 {
			t.Fatalf("build: exit %d\n%s%s", code, stdout, stderr)
		}
		module := httpSmokeArtifact(workspace, stdout)
		for _, host := range []string{"bun", "node"} {
			t.Run(host, func(t *testing.T) {
				t.Parallel()
				path, err := exec.LookPath(host)
				if err != nil {
					t.Fatalf("%s is required", host)
				}
				httpTransportSmokeCheck(t, workspace, path, module)
			})
		}
	})
}

// httpTransportSmokeResponse is one parsed HTTP/1.1 response.
type httpTransportSmokeResponse struct {
	status  int
	headers map[string]string
	body    []byte
}

const httpTransportSmokeTimeout = 5 * time.Second

func httpTransportSmokeConnect(t *testing.T, address string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, httpTransportSmokeTimeout)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func httpTransportSmokeSend(t *testing.T, connection net.Conn, raw []byte) {
	t.Helper()
	if err := connection.SetWriteDeadline(time.Now().Add(httpTransportSmokeTimeout)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(raw); err != nil {
		t.Fatal(err)
	}
}

// httpTransportSmokeRead returns the response, or nil when the server closed
// the connection without sending any response bytes. Each read waits at most
// five seconds.
func httpTransportSmokeRead(t *testing.T, connection net.Conn) *httpTransportSmokeResponse {
	t.Helper()
	buffer := make([]byte, 65536)
	receive := func() (int, bool) {
		if err := connection.SetReadDeadline(time.Now().Add(httpTransportSmokeTimeout)); err != nil {
			t.Fatal(err)
		}
		n, err := connection.Read(buffer)
		if n > 0 {
			return n, true
		}
		if errors.Is(err, io.EOF) {
			return 0, false
		}
		t.Fatalf("read response: %v", err)
		return 0, false
	}
	var data []byte
	for !bytes.Contains(data, []byte("\r\n\r\n")) {
		n, ok := receive()
		if !ok {
			if len(data) != 0 {
				t.Fatalf("connection closed inside the response head: %q", data)
			}
			return nil
		}
		data = append(data, buffer[:n]...)
	}
	head, body, _ := bytes.Cut(data, []byte("\r\n\r\n"))
	lines := strings.Split(string(head), "\r\n")
	fields := strings.Split(lines[0], " ")
	if len(fields) < 2 {
		t.Fatalf("status line %q", lines[0])
	}
	status, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("status line %q", lines[0])
	}
	headers := map[string]string{}
	for _, line := range lines[1:] {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			t.Fatalf("header line %q", line)
		}
		headers[strings.ToLower(strings.TrimSpace(name))] = strings.TrimSpace(value)
	}
	length := 0
	if value, ok := headers["content-length"]; ok {
		if length, err = strconv.Atoi(value); err != nil {
			t.Fatalf("content-length %q", value)
		}
	}
	if _, chunked := headers["transfer-encoding"]; chunked {
		t.Fatalf("unexpected transfer-encoding: %v", headers)
	}
	for len(body) < length {
		n, ok := receive()
		if !ok {
			t.Fatal("truncated body")
		}
		body = append(body, buffer[:n]...)
	}
	return &httpTransportSmokeResponse{status: status, headers: headers, body: body}
}

func httpTransportSmokeExchange(t *testing.T, address string, raw []byte) *httpTransportSmokeResponse {
	t.Helper()
	connection := httpTransportSmokeConnect(t, address)
	defer connection.Close()
	httpTransportSmokeSend(t, connection, raw)
	return httpTransportSmokeRead(t, connection)
}

// httpTransportSmokeSequential is exchange for a plain request that is not
// about admission. The admission slot is held until the previous response has
// been handed to the operating system (docs/runtime.md), so a client that
// reconnects inside that window may see 503; retry on a new connection until
// the deadline. Cases that assert saturation or shutdown 503 use exchange and
// stay strict.
func httpTransportSmokeSequential(t *testing.T, address string, raw []byte) *httpTransportSmokeResponse {
	t.Helper()
	stop := time.Now().Add(3 * time.Second)
	for {
		response := httpTransportSmokeExchange(t, address, raw)
		if response == nil || response.status != 503 || !time.Now().Before(stop) {
			return response
		}
	}
}

func httpTransportSmokeRequest(method, path string, body []byte, contentType string, chunked bool) []byte {
	lines := []string{method + " " + path + " HTTP/1.1", "Host: effra"}
	if contentType != "" {
		lines = append(lines, "Content-Type: "+contentType)
	}
	var payload []byte
	if body != nil && chunked {
		lines = append(lines, "Transfer-Encoding: chunked")
		half := len(body) / 2
		for _, part := range [][]byte{body[:half], body[half:]} {
			payload = append(payload, fmt.Sprintf("%x\r\n", len(part))...)
			payload = append(payload, part...)
			payload = append(payload, "\r\n"...)
		}
		payload = append(payload, "0\r\n\r\n"...)
	} else if body != nil {
		lines = append(lines, fmt.Sprintf("Content-Length: %d", len(body)))
		payload = body
	}
	return append([]byte(strings.Join(lines, "\r\n")+"\r\n\r\n"), payload...)
}

// httpTransportSmokeExpect checks the exact status and body. An empty
// contentType requires the header to be absent; mustClose requires
// "Connection: close".
func httpTransportSmokeExpect(t *testing.T, response *httpTransportSmokeResponse, status int, body, contentType string, mustClose bool) {
	t.Helper()
	if response == nil {
		t.Fatalf("expected %d, connection closed without a response", status)
	}
	if response.status != status || string(response.body) != body {
		t.Fatalf("expected %d %q, got %d %q %v", status, body, response.status, response.body, response.headers)
	}
	if actual, ok := response.headers["content-type"]; ok != (contentType != "") || actual != contentType {
		t.Fatalf("expected content type %q, got %v", contentType, response.headers)
	}
	if mustClose && strings.ToLower(response.headers["connection"]) != "close" {
		t.Fatalf("expected Connection: close, got %v", response.headers)
	}
}

// httpTransportSmokeAnswered reports, without waiting, whether the connection
// is readable: response bytes, end of stream or an error are pending.
func httpTransportSmokeAnswered(t *testing.T, connection net.Conn) bool {
	t.Helper()
	raw, err := connection.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	answered := false
	peek := make([]byte, 1)
	if err := raw.Read(func(fd uintptr) bool {
		_, _, err := syscall.Recvfrom(int(fd), peek, syscall.MSG_PEEK|syscall.MSG_DONTWAIT)
		answered = !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	return answered
}

// httpTransportSmokeHoldSlot occupies the single admission slot with /slow.
// A probe can briefly hold the slot itself, so a rejected /slow (it received
// a response) retries.
func httpTransportSmokeHoldSlot(t *testing.T, address string) net.Conn {
	t.Helper()
	stop := time.Now().Add(3 * time.Second)
	for {
		slow := httpTransportSmokeConnect(t, address)
		httpTransportSmokeSend(t, slow, httpTransportSmokeRequest("GET", "/slow", nil, "", false))
		for {
			response := httpTransportSmokeExchange(t, address, httpTransportSmokeRequest("GET", "/health", nil, "", false))
			if httpTransportSmokeAnswered(t, slow) {
				slow.Close()
				break
			}
			if response != nil && response.status == 503 {
				return slow
			}
			if !time.Now().Before(stop) {
				t.Fatalf("/slow was not admitted: %+v", response)
			}
		}
		if !time.Now().Before(stop) {
			t.Fatal("/slow was never admitted")
		}
	}
}

// httpTransportSmokeHoldMalformed sends raw on a connection that stays open
// after its 400. The slot of the previous response may still be held
// (docs/runtime.md), so a 503 closes that socket and retries; the accepted
// connection is returned open.
func httpTransportSmokeHoldMalformed(t *testing.T, address string, raw []byte) net.Conn {
	t.Helper()
	stop := time.Now().Add(3 * time.Second)
	for {
		held := httpTransportSmokeConnect(t, address)
		httpTransportSmokeSend(t, held, raw)
		response := httpTransportSmokeRead(t, held)
		if response != nil && response.status == 503 && time.Now().Before(stop) {
			held.Close()
			continue
		}
		httpTransportSmokeExpect(t, response, 400, "", "", true)
		return held
	}
}

// httpTransportSmokePollHealth waits for /health to report status; it is 503
// exactly while the single admission slot is held.
func httpTransportSmokePollHealth(t *testing.T, address string, status int) {
	t.Helper()
	stop := time.Now().Add(3 * time.Second)
	for {
		response := httpTransportSmokeExchange(t, address, httpTransportSmokeRequest("GET", "/health", nil, "", false))
		if response != nil && response.status == status {
			return
		}
		if !time.Now().Before(stop) {
			t.Fatalf("/health did not become %d: %+v", status, response)
		}
	}
}

// httpTransportSmokeCheck runs the whole transcript against one server. The
// cases share the single admission slot, so they run in order.
func httpTransportSmokeCheck(t *testing.T, dir string, command ...string) {
	server := httpSmokeStart(t, dir, command...)
	location, err := url.Parse(server.address)
	if err != nil || location.Hostname() != "127.0.0.1" || location.Port() == "" {
		t.Fatalf("readiness address %q", server.address)
	}
	address := location.Host
	const text = "text/plain; charset=utf-8"
	const octets = "application/octet-stream"
	request := httpTransportSmokeRequest
	sequential := func(raw []byte) *httpTransportSmokeResponse {
		t.Helper()
		return httpTransportSmokeSequential(t, address, raw)
	}
	expect := func(response *httpTransportSmokeResponse, status int, body, contentType string, mustClose bool) {
		t.Helper()
		httpTransportSmokeExpect(t, response, status, body, contentType, mustClose)
	}
	expect(sequential(request("GET", "/health", nil, "", false)), 200, "ok", text, false)
	// The selected profile answers a wrong method on a known path with 404.
	expect(sequential(request("POST", "/health", []byte{}, "", false)), 404, "", "", false)
	expect(sequential(request("GET", "/missing?x=/health", nil, "", false)), 404, "", "", false)
	expect(sequential(request("POST", "/echo", []byte("\xff\x00binary"), octets, false)), 200, "\xff\x00binary", octets, false)
	expect(sequential(request("POST", "/echo", []byte("text"), "text/plain", false)), 415, "", "", false)
	expect(sequential(request("GET", "/malformed", nil, "", false)), 400, "", "", false)
	expect(sequential(request("GET", "/unavailable", nil, "", false)), 500, "", "", false)
	// A response header value outside the shared policy (visible ASCII,
	// space, tab) fails closed, and the server keeps serving.
	expect(sequential(request("GET", "/invalid", nil, "", false)), 500, "", "", false)
	expect(sequential(request("GET", "/health", nil, "", false)), 200, "ok", text, false)
	// Absolute-form targets: the query is not part of the path, even when it
	// contains a slash.
	expect(sequential(request("GET", "http://effra?next=/health", nil, "", false)), 404, "", "", false)
	expect(sequential(request("GET", "http://effra/health?next=/echo", nil, "", false)), 200, "ok", text, false)
	// Body bounds: 16 bytes are admitted; 17 are rejected before the handler.
	expect(sequential(request("POST", "/echo", bytes.Repeat([]byte("x"), 16), octets, false)), 200, strings.Repeat("x", 16), octets, false)
	expect(sequential(request("POST", "/echo", bytes.Repeat([]byte("x"), 17), octets, false)), 413, "", "", true)
	expect(sequential(request("POST", "/echo", bytes.Repeat([]byte("y"), 16), octets, true)), 200, strings.Repeat("y", 16), octets, false)
	expect(sequential(request("POST", "/echo", bytes.Repeat([]byte("y"), 17), octets, true)), 413, "", "", true)
	malformed := []byte("POST /echo HTTP/1.1\r\nHost: effra\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\nab\r\n0\r\n\r\n")
	expect(sequential(malformed), 400, "", "", true)
	// A body that stalls after one of its four declared bytes is closed
	// without a response by the 300 ms readBodyMillis timer; waiting on that
	// timer is the behavior under test.
	started := time.Now()
	stalled := []byte("POST /echo HTTP/1.1\r\nHost: effra\r\nContent-Type: application/octet-stream\r\nContent-Length: 4\r\n\r\nx")
	if response := sequential(stalled); response != nil {
		t.Fatalf("stalled body produced a response: %+v", response)
	}
	if elapsed := time.Since(started); elapsed >= 3*time.Second {
		t.Fatalf("body read timeout did not close the connection (%v)", elapsed)
	}
	// Client disconnect cancels the active handler and releases admission.
	slow := httpTransportSmokeHoldSlot(t, address)
	slow.Close()
	httpTransportSmokePollHealth(t, address, 200)
	// A client that keeps its connection open after a malformed body's 400
	// does not hold shutdown open: the transport closes that connection
	// itself once the 400 has been written.
	held := httpTransportSmokeHoldMalformed(t, address, malformed)
	// The deferred close also keeps held reachable: an unreachable net.Conn
	// may be closed by its finalizer, which would end the case early.
	defer held.Close()
	// Shutdown with active work: the in-flight request receives 503 after its
	// scope closed, then the server completes with interruption within its
	// idleMillis drain grace.
	slow = httpTransportSmokeHoldSlot(t, address)
	signalled := time.Now()
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	expect(httpTransportSmokeRead(t, slow), 503, "", "", true)
	slow.Close()
	exit, stderr := server.httpSmokeWait(t)
	if elapsed := time.Since(signalled); elapsed >= 5*time.Second {
		t.Fatalf("shutdown took %v", elapsed)
	}
	if exit != 1 || !strings.Contains(strings.ToLower(stderr), "interrupt") {
		t.Fatalf("shutdown: exit %d\n%s", exit, stderr)
	}
	held.Close()
	// Client connections may linger in TIME_WAIT; a live listener would still
	// refuse this bind. Go listeners set SO_REUSEADDR.
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("server still holds %s after shutdown: %v", address, err)
	}
	listener.Close()
}
