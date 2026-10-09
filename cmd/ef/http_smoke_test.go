package main

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// httpSmokeServer is one generated HTTP server process started from a smoke
// workspace. Its first stdout line is the readiness report.
type httpSmokeServer struct {
	command *exec.Cmd
	stderr  bytes.Buffer
	done    chan error
	// address is the readiness line's URL without the "listening " prefix.
	address string
}

// httpSmokeLineWriter receives the server's stdout and publishes its first
// line once.
type httpSmokeLineWriter struct {
	mu    sync.Mutex
	data  []byte
	sent  bool
	lines chan string
}

func (w *httpSmokeLineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.sent {
		w.data = append(w.data, p...)
		if index := bytes.IndexByte(w.data, '\n'); index >= 0 {
			w.sent = true
			w.lines <- strings.TrimSpace(string(w.data[:index]))
		}
	}
	return len(p), nil
}

// httpSmokeStart runs command in dir and waits up to ten seconds for its
// readiness line. A server that never reports readiness is killed. The test's
// cleanup kills a server that is still running.
func httpSmokeStart(t *testing.T, dir string, command ...string) *httpSmokeServer {
	t.Helper()
	server := &httpSmokeServer{command: exec.Command(command[0], command[1:]...), done: make(chan error, 1)}
	server.command.Dir = dir
	stdout := &httpSmokeLineWriter{lines: make(chan string, 1)}
	server.command.Stdout = stdout
	server.command.Stderr = &server.stderr
	if err := server.command.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { server.done <- server.command.Wait() }()
	t.Cleanup(func() {
		select {
		case <-server.done:
		default:
			_ = server.command.Process.Kill()
			<-server.done
		}
	})
	select {
	case line := <-stdout.lines:
		if !strings.HasPrefix(line, "listening http://") {
			t.Fatalf("readiness line = %q", line)
		}
		server.address = strings.TrimPrefix(line, "listening ")
	case err := <-server.done:
		server.done <- err // Leave the result for the cleanup.
		t.Fatalf("%s exited before binding: %v\n%s", command[0], err, server.stderr.String())
	case <-time.After(10 * time.Second):
		t.Fatalf("%s server did not bind", command[0])
	}
	return server
}

// httpSmokeStop sends SIGTERM and returns the exit code and stderr once the
// process exits within ten seconds.
func (server *httpSmokeServer) httpSmokeStop(t *testing.T) (int, string) {
	t.Helper()
	if err := server.command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	return server.httpSmokeWait(t)
}

// httpSmokeWait waits up to ten seconds for the process to exit.
func (server *httpSmokeServer) httpSmokeWait(t *testing.T) (int, string) {
	t.Helper()
	select {
	case err := <-server.done:
		server.done <- err // Leave the result for the cleanup.
		var exit *exec.ExitError
		if err != nil && !errors.As(err, &exit) {
			t.Fatal(err)
		}
		return server.command.ProcessState.ExitCode(), server.stderr.String()
	case <-time.After(10 * time.Second):
		t.Fatal("server did not exit after SIGTERM")
		return -1, ""
	}
}

// httpSmokeArtifact resolves the path `ef build` reports against the
// workspace it ran in.
func httpSmokeArtifact(workspace string, reported []byte) string {
	path := strings.TrimSpace(string(reported))
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

// Ported from scripts/http_smoke.py: the generated examples/http.ef
// executable serves its routes, keeps the target's percent-encoding, answers a
// declared failure with an empty 500, refuses a body beyond maxBodyBytes and
// shuts down cooperatively on SIGTERM.
func TestHTTPSmokeServesRoutesBoundariesAndSIGTERMShutdown(t *testing.T) {
	t.Parallel()
	binary := buildTestCLI(t)
	workspace := smokeWorkspace(t, "examples/http.ef", "examples/sdk", "examples/fixture.txt", "go.mod", "effra.bindings.json")
	stdout, stderr, code := runTestCLIDir(t, binary, workspace, "", "build", "examples/http.ef")
	if code != 0 {
		t.Fatalf("build: exit %d\n%s%s", code, stdout, stderr)
	}
	// The executable runs from the workspace, so its file scope reads the
	// workspace's examples/fixture.txt.
	server := httpSmokeStart(t, workspace, httpSmokeArtifact(workspace, stdout))
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(t *testing.T, method, path string, body io.Reader) (*http.Response, []byte) {
		t.Helper()
		request, err := http.NewRequest(method, server.address+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}
	t.Run("requests", func(t *testing.T) {
		for _, c := range []struct{ path, body string }{
			{"/health", "ok"},
			{"/users/42", "Hello, Ada"},
			// The SDK lookup outlives its 100 ms timeout.
			{"/users/slow", "Hello, timed out"},
			{"/file", "scoped file read complete\n"},
		} {
			t.Run(strings.TrimPrefix(c.path, "/"), func(t *testing.T) {
				t.Parallel()
				response, body := get(t, "GET", c.path, nil)
				if response.StatusCode != 200 || string(body) != c.body {
					t.Fatalf("%s: %d %q", c.path, response.StatusCode, body)
				}
				if got := response.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
					t.Fatalf("%s: content type %q", c.path, got)
				}
			})
		}
		// The managed transport keeps the target's percent-encoding: an encoded
		// /health is not the health route.
		t.Run("encoded-path", func(t *testing.T) {
			t.Parallel()
			if _, body := get(t, "GET", "/%68ealth", nil); string(body) != "Hello, Ada" {
				t.Fatalf("encoded path: %q", body)
			}
		})
		// A declared request failure is an empty 500 without a content type.
		t.Run("failure", func(t *testing.T) {
			t.Parallel()
			response, body := get(t, "GET", "/users/missing", nil)
			if _, typed := response.Header["Content-Type"]; response.StatusCode != 500 || len(body) != 0 || typed {
				t.Fatalf("unhandled request failure: %d %q %v", response.StatusCode, body, response.Header)
			}
		})
		// Routes take no body: a declared one beyond maxBodyBytes is refused
		// unhandled.
		t.Run("body-bound", func(t *testing.T) {
			t.Parallel()
			response, body := get(t, "POST", "/health", strings.NewReader("x"))
			if response.StatusCode != 413 || len(body) != 0 {
				t.Fatalf("oversized request body: %d %q", response.StatusCode, body)
			}
		})
	})
	if exit, stderr := server.httpSmokeStop(t); exit != 1 || !strings.Contains(stderr, "context canceled") {
		t.Fatalf("SIGTERM shutdown: exit %d\n%s", exit, stderr)
	}
}
