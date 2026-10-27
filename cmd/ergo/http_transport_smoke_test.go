package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The shared HTTP wire vectors (conformance/http/*-vectors.json) each name a
// public program, the exact limits it declares, plain exchanges and
// lifecycle cases. Every file runs over real sockets on each cohort it lists
// (the Go executable). Every exchange
// observes the exact status, body and Content-Type, and Connection: close
// when the vector records it.
//
// Cancellation is proved causally: the transport program admits one active
// request, so the admission slot held by /slow (a 60s handler) is released
// only after its request scope is cancelled and closed.
//
// The polls below are bounded retries, never sleeps. The admission slot is
// held until the previous response has been handed to the operating system
// (docs/runtime.md), so plain sequential requests retry a 503 on a new
// connection for up to three seconds; cases that assert saturation or
// shutdown 503 stay strict.
func TestHTTPWireVectors(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "conformance", "http", "*-vectors.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no HTTP wire vectors: %v", err)
	}
	binary := buildTestCLI(t)
	for _, file := range files {
		vectors := httpWireVectorsLoad(t, root, file)
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			inputs := []string{vectors.Program, "go.mod", "ergo.bindings.json"}
			for _, target := range vectors.Targets {
				if target != "go" {
					t.Fatalf("unknown wire cohort %q", target)
				}
				t.Run("go", func(t *testing.T) {
					t.Parallel()
					workspace := smokeWorkspace(t, inputs...)
					stdout, stderr, code := runTestCLIDir(t, binary, workspace, "", "build", vectors.Program)
					if code != 0 {
						t.Fatalf("build: exit %d\n%s%s", code, stdout, stderr)
					}
					httpWireVectorsRun(t, vectors, workspace, httpSmokeArtifact(workspace, stdout))
				})
			}
		})
	}
}

// httpWireVectors is one conformance/http/*-vectors.json file.
type httpWireVectors struct {
	Schema    string           `json:"schema"`
	Program   string           `json:"program"`
	Profile   string           `json:"profile"`
	Targets   []string         `json:"targets"`
	Limits    map[string]int64 `json:"limits"`
	Exchanges []struct {
		Name     string           `json:"name"`
		Request  httpWireRequest  `json:"request"`
		Response httpWireResponse `json:"response"`
	} `json:"exchanges"`
	Lifecycle []httpWireLifecycle `json:"lifecycle"`
}

// httpWireBody is exactly one of text, hex or a repeated string.
type httpWireBody struct {
	Text   *string `json:"text"`
	Hex    *string `json:"hex"`
	Repeat *string `json:"repeat"`
	Count  int     `json:"count"`
}

// httpWireRequest is either raw bytes (each rune one octet) or a request
// rendered with Content-Length or two-chunk chunked framing.
type httpWireRequest struct {
	Raw         *string       `json:"raw"`
	Method      string        `json:"method"`
	Target      string        `json:"target"`
	Body        *httpWireBody `json:"body"`
	ContentType *string       `json:"contentType"`
	Chunked     bool          `json:"chunked"`
}

// httpWireResponse is an expected response: an absent body is empty, an
// absent contentType requires the header to be absent, and close, when
// present, requires Connection: close to be present or absent.
type httpWireResponse struct {
	Status      int           `json:"status"`
	Body        *httpWireBody `json:"body"`
	ContentType *string       `json:"contentType"`
	Close       *bool         `json:"close"`
}

type httpWireLifecycle struct {
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Request      httpWireRequest  `json:"request"`
	Response     httpWireResponse `json:"response"`
	Hold         string           `json:"hold"`
	Probe        string           `json:"probe"`
	WithinMillis int              `json:"withinMillis"`
	ExitCode     int              `json:"exitCode"`
	Stderr       string           `json:"stderr"`
}

// httpWireVectorsLoad decodes one vector file and requires its program to
// declare exactly the recorded limits, so a drifted program or vector file
// fails rather than passing vacuously. Both are read in this process, so the
// Go test cache reruns the test when either changes.
func httpWireVectorsLoad(t *testing.T, root, file string) httpWireVectors {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var vectors httpWireVectors
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&vectors); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	if vectors.Schema != "ergo-http-wire-vectors/1" || len(vectors.Targets) == 0 || len(vectors.Exchanges) == 0 || len(vectors.Limits) == 0 {
		t.Fatalf("%s: schema %q, %d targets, %d exchanges, %d limits", file, vectors.Schema, len(vectors.Targets), len(vectors.Exchanges), len(vectors.Limits))
	}
	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(vectors.Program)))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range vectors.Limits {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `: ` + strconv.FormatInt(value, 10) + `\b`).Match(source) {
			t.Fatalf("%s does not declare %s: %d", vectors.Program, name, value)
		}
	}
	return vectors
}

func (b *httpWireBody) bytes(t *testing.T) []byte {
	t.Helper()
	switch {
	case b == nil:
		return nil
	case b.Text != nil:
		return []byte(*b.Text)
	case b.Hex != nil:
		decoded, err := hex.DecodeString(*b.Hex)
		if err != nil {
			t.Fatalf("vector body hex: %v", err)
		}
		return decoded
	case b.Repeat != nil:
		return bytes.Repeat([]byte(*b.Repeat), b.Count)
	}
	t.Fatalf("vector body has no text, hex or repeat: %+v", b)
	return nil
}

func (r httpWireRequest) raw(t *testing.T) []byte {
	t.Helper()
	if r.Raw != nil {
		out := make([]byte, 0, len(*r.Raw))
		for _, octet := range *r.Raw {
			if octet > 0xff {
				t.Fatalf("raw vector request holds a non-octet rune %q", octet)
			}
			out = append(out, byte(octet))
		}
		return out
	}
	contentType := ""
	if r.ContentType != nil {
		contentType = *r.ContentType
	}
	return httpTransportSmokeRequest(r.Method, r.Target, r.Body.bytes(t), contentType, r.Chunked)
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
	lines := []string{method + " " + path + " HTTP/1.1", "Host: ergo"}
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

// httpTransportSmokeExpect checks a response against a vector expectation.
func httpTransportSmokeExpect(t *testing.T, name string, response *httpTransportSmokeResponse, want httpWireResponse) {
	t.Helper()
	if response == nil {
		t.Fatalf("%s: expected %d, connection closed without a response", name, want.Status)
	}
	body := want.Body.bytes(t)
	if body == nil {
		body = []byte{}
	}
	if response.status != want.Status || !bytes.Equal(response.body, body) {
		t.Fatalf("%s: expected %d %q, got %d %q %v", name, want.Status, body, response.status, response.body, response.headers)
	}
	actual, typed := response.headers["content-type"]
	if typed != (want.ContentType != nil) || (typed && actual != *want.ContentType) {
		t.Fatalf("%s: expected content type %v, got %v", name, want.ContentType, response.headers)
	}
	if want.Close != nil && (strings.ToLower(response.headers["connection"]) == "close") != *want.Close {
		t.Fatalf("%s: expected close=%v, got %v", name, *want.Close, response.headers)
	}
}

// httpTransportSmokeHoldSlot occupies the single admission slot with a
// request for hold. A probe can briefly hold the slot itself, so a rejected
// hold (it received a response) retries.
func httpTransportSmokeHoldSlot(t *testing.T, address, hold, probe string) net.Conn {
	t.Helper()
	stop := time.Now().Add(3 * time.Second)
	for {
		slow := httpTransportSmokeConnect(t, address)
		httpTransportSmokeSend(t, slow, httpTransportSmokeRequest("GET", hold, nil, "", false))
		for {
			response := httpTransportSmokeExchange(t, address, httpTransportSmokeRequest("GET", probe, nil, "", false))
			if httpTransportSmokeAnswered(t, slow) {
				slow.Close()
				break
			}
			if response != nil && response.status == 503 {
				return slow
			}
			if !time.Now().Before(stop) {
				t.Fatalf("%s was not admitted: %+v", hold, response)
			}
		}
		if !time.Now().Before(stop) {
			t.Fatalf("%s was never admitted", hold)
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
		closing := true
		httpTransportSmokeExpect(t, "malformed request held open", response, httpWireResponse{Status: 400, Close: &closing})
		return held
	}
}

// httpTransportSmokePollHealth waits for probe to report status; it is 503
// exactly while the single admission slot is held.
func httpTransportSmokePollHealth(t *testing.T, address, probe string, status int) {
	t.Helper()
	stop := time.Now().Add(3 * time.Second)
	for {
		response := httpTransportSmokeExchange(t, address, httpTransportSmokeRequest("GET", probe, nil, "", false))
		if response != nil && response.status == status {
			return
		}
		if !time.Now().Before(stop) {
			t.Fatalf("%s did not become %d: %+v", probe, status, response)
		}
	}
}

// httpWireVectorsRun runs one vector file against one server: the exchanges
// in order, then the lifecycle cases in a fixed order (stalled body,
// disconnect, malformed request held open, shutdown with active work). The
// cases share the server's admission slots, so they run in order.
func httpWireVectorsRun(t *testing.T, vectors httpWireVectors, dir string, command ...string) {
	server := httpSmokeStart(t, dir, command...)
	location, err := url.Parse(server.address)
	if err != nil || location.Hostname() != "127.0.0.1" || location.Port() == "" {
		t.Fatalf("readiness address %q", server.address)
	}
	address := location.Host
	for _, exchange := range vectors.Exchanges {
		httpTransportSmokeExpect(t, exchange.Name, httpTransportSmokeSequential(t, address, exchange.Request.raw(t)), exchange.Response)
	}
	lifecycle := map[string]httpWireLifecycle{}
	for _, c := range vectors.Lifecycle {
		switch c.Kind {
		case "stalled-body", "disconnect-releases-admission", "malformed-held-open", "shutdown-with-active-work":
			if _, repeated := lifecycle[c.Kind]; repeated {
				t.Fatalf("lifecycle kind %q appears twice", c.Kind)
			}
			lifecycle[c.Kind] = c
		default:
			t.Fatalf("unknown lifecycle kind %q", c.Kind)
		}
	}
	if c, ok := lifecycle["stalled-body"]; ok {
		// A body that stalls after part of its declared length is closed
		// without a response by the readBodyMillis timer; waiting on that
		// timer is the behavior under test.
		started := time.Now()
		if response := httpTransportSmokeSequential(t, address, c.Request.raw(t)); response != nil {
			t.Fatalf("%s: stalled body produced a response: %+v", c.Name, response)
		}
		if elapsed := time.Since(started); elapsed >= time.Duration(c.WithinMillis)*time.Millisecond {
			t.Fatalf("%s: body read timeout did not close the connection (%v)", c.Name, elapsed)
		}
	}
	if c, ok := lifecycle["disconnect-releases-admission"]; ok {
		// Client disconnect cancels the active handler and releases admission.
		slow := httpTransportSmokeHoldSlot(t, address, c.Hold, c.Probe)
		slow.Close()
		httpTransportSmokePollHealth(t, address, c.Probe, 200)
	}
	var held net.Conn
	if c, ok := lifecycle["malformed-held-open"]; ok {
		// A client that keeps its connection open after a malformed body's
		// 400 does not hold shutdown open: the transport closes that
		// connection itself once the 400 has been written.
		held = httpTransportSmokeHoldMalformed(t, address, c.Request.raw(t))
		// The deferred close also keeps held reachable: an unreachable
		// net.Conn may be closed by its finalizer, which would end the case
		// early.
		defer held.Close()
	}
	c, ok := lifecycle["shutdown-with-active-work"]
	if !ok {
		return
	}
	// Shutdown with active work: the in-flight request receives its response
	// after its scope closed, then the server completes within its idleMillis
	// drain grace.
	slow := httpTransportSmokeHoldSlot(t, address, c.Hold, c.Probe)
	signalled := time.Now()
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	httpTransportSmokeExpect(t, c.Name, httpTransportSmokeRead(t, slow), c.Response)
	slow.Close()
	exit, stderr := server.httpSmokeWait(t)
	if elapsed := time.Since(signalled); elapsed >= time.Duration(c.WithinMillis)*time.Millisecond {
		t.Fatalf("%s: shutdown took %v", c.Name, elapsed)
	}
	if exit != c.ExitCode || stderr != c.Stderr {
		t.Fatalf("%s: exit %d stderr %q, want %d and %q", c.Name, exit, stderr, c.ExitCode, c.Stderr)
	}
	// Client connections may linger in TIME_WAIT; a live listener would still
	// refuse this bind. Go listeners set SO_REUSEADDR. The kernel must assign
	// the released port to no other socket before this bind (#112). The held
	// malformed connection provides that: the server closed it with a FIN
	// while this test keeps its end open, so the server's side outlives the
	// server in FIN_WAIT2 and holds the port for tcp_fin_timeout (60 s on
	// Linux by default), far longer than the case takes. Reading EOF on it
	// proves the FIN; a reset would have freed the port at once.
	if held == nil {
		t.Fatalf("%s: the release check needs the malformed-held-open case, whose connection holds the port until the check", c.Name)
	}
	if err := held.SetReadDeadline(time.Now().Add(httpTransportSmokeTimeout)); err != nil {
		t.Fatal(err)
	}
	if n, err := io.Copy(io.Discard, held); err != nil {
		t.Fatalf("%s: the held malformed connection ended with %v after %d more bytes, want EOF (a FIN)", c.Name, err, n)
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("%s: server still holds %s after shutdown: %v", c.Name, address, err)
	}
	listener.Close()
}
