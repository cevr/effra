//go:build unix

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// A signal interrupts main on both targets, and `ef run` forwards it to the
// program and passes the program's report and status through unchanged.
func TestEntryFailureReportForSignalInterruption(t *testing.T) {
	binary := buildTestCLI(t)
	root := entryFailureRoot(t)
	file := filepath.Join(root, "wait.ef")
	source := `effect fn wait() -> void uses { Clock, Console } {
    run Console.log("ready")
    run Clock.sleep(60000)
}
effect fn main() -> void {
    run wait().provide<Clock>(LiveClock).provide<Console>(Stdout)
}
`
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
			stdout, stderr, code := runInterruptedEntry(t, binary, root, file, target, signal)
			if stdout != "ready\n" || stderr != "interrupt\n" || code != 130 {
				t.Errorf("%s %v: code=%d stdout=%q stderr=%q; want 130 and %q", target, signal, code, stdout, stderr, "interrupt\n")
			}
		}
	}
}

// runInterruptedEntry runs `ef run` in its own process group, signals only
// `ef` once the program is ready, and on a timeout kills the whole group so
// no descendant keeps the capture pipes open. WaitDelay bounds pipe draining.
func runInterruptedEntry(t *testing.T, binary, root, file, target string, signal syscall.Signal) (string, string, int) {
	t.Helper()
	command := exec.Command(binary, "run", file, "--target", target)
	command.Dir = root
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	stdout := &readyWriter{ready: make(chan struct{})}
	command.Stdout, command.Stderr = stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	killGroup := func() {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		<-done
	}
	select {
	case <-stdout.ready:
	case err := <-done:
		t.Fatalf("%s: program exited before ready: %v stderr=%q", target, err, stderr.String())
	case <-time.After(60 * time.Second):
		killGroup()
		t.Fatalf("%s: program did not report ready: stderr=%q", target, stderr.String())
	}
	if err := command.Process.Signal(signal); err != nil {
		killGroup()
		t.Fatal(err)
	}
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(30 * time.Second):
		killGroup()
		t.Fatalf("%s: %v did not interrupt main", target, signal)
	}
	code := 0
	var exited *exec.ExitError
	if errors.As(waitErr, &exited) {
		code = exited.ExitCode()
	} else if waitErr != nil {
		t.Fatal(waitErr)
	}
	return stdout.String(), stderr.String(), code
}

// readyWriter collects a program's stdout and reports its first line.
type readyWriter struct {
	mu    sync.Mutex
	data  bytes.Buffer
	ready chan struct{}
	seen  bool
}

func (w *readyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.data.Write(p)
	if !w.seen && strings.Contains(w.data.String(), "\n") {
		w.seen = true
		close(w.ready)
	}
	return len(p), nil
}

func (w *readyWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.data.String()
}
