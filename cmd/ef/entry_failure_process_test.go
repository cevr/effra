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

// entryFailureCases pin the entry failure report: the stderr text and exit
// status of an unhandled cause at `main`, identical on both targets.
var entryFailureCases = []struct {
	name, source, stderr string
	code                 int
}{
	{"named failure with payload", `import Data "effra/data"
enum Shape {
    Dot
    Box { side: i64 }
}
record Point {
    x: i64
    y: i64
}
error Invalid {
    message: string
    code: i64
    shape: Shape
    at: Point
    hint: Data.Option<string>
    ok: bool
}
effect fn main() -> void raises { Invalid } {
    fail Invalid { message: "bad \"input\"\n\ttab é \\", code: 42, shape: Shape.Box { side: 2 }, at: Point { y: 2, x: 1 }, hint: Data.Option.Some { value: "h" }, ok: true }
}
`, `failure: Invalid { at: { x: 1, y: 2 }, code: 42, hint: Some { value: "h" }, message: "bad \"input\"\n\ttab é \\", ok: true, shape: Box { side: 2 } }` + "\n", 1},
	{"payload-less failure", `error Bad
effect fn main() -> void raises { Bad } {
    fail Bad
}
`, "failure: Bad\n", 1},
	{"built-in failure message", `effect fn main() -> void raises { AssertionFailed } {
    run Assert.equalText("a", "b").provide<Assert>(Assertions)
}
`, `failure: AssertionFailed { message: "expected \"b\"; received \"a\"" }` + "\n", 1},
	{"opaque and nested values", `record Holder {
    callback: fn() -> string
    nothing: void
}
fn name() -> string { "n" }
error Odd {
    holder: Holder
    data: bytes
}
effect fn main() -> void raises { Odd } {
    let data = run Http.text("abc").provide<Http>(LiveHttp)
    fail Odd { holder: Holder { callback: name, nothing: void }, data: data }
}
`, "failure: Odd { data: <bytes len=3>, holder: { callback: <fn>, nothing: void } }\n", 1},
	{"defect", `effect fn main() -> void {
    run Scheduler.advance(1).provide<Scheduler>(LiveScheduler)
}
`, `defect: "live scheduler cannot advance"` + "\n", 1},
	// Each child signals before failing; the sleep outlasts its last step, so
	// both failures are complete before the scope closes.
	{"composite cause from parallel children", `error Bad
error Worse {
    reason: string
}
effect fn bad(ready: Latch) -> void raises { Bad } uses { Sync } {
    run Sync.signal(ready)
    fail Bad
}
effect fn worse(ready: Latch) -> void raises { Worse } uses { Sync } {
    run Sync.signal(ready)
    fail Worse { reason: "second" }
}
effect fn program() -> void raises { Bad, Worse } uses { Clock, Sync } {
    let first = run Sync.latch()
    let second = run Sync.latch()
    scope {
        let a = fork bad(first)
        let b = fork worse(second)
        run Sync.await(first)
        run Sync.await(second)
        run Clock.sleep(100)
    }
}
effect fn main() -> void raises { Bad, Worse } {
    run program().provide<Clock>(LiveClock).provide<Sync>(TestSync)
}
`, "failure: Bad\nfailure: Worse { reason: \"second\" }\n", 1},
	{"interruption observed through join", `effect fn pending() -> void uses { Clock } {
    run Clock.sleep(60000)
}
effect fn program() -> void uses { Clock } {
    scope {
        let child = fork pending()
        run child.interrupt()
        run child.join()
    }
}
effect fn main() -> void {
    run program().provide<Clock>(LiveClock)
}
`, "interrupt\n", 130},
}

func entryFailureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEntryFailureReportIsIdenticalOnBothTargets(t *testing.T) {
	binary := buildTestCLI(t)
	root := entryFailureRoot(t)
	for index, test := range entryFailureCases {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(root, "case"+string(rune('a'+index))+".ef")
			if err := os.WriteFile(file, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"go", "js"} {
				stdout, stderr, code := runTestCLIDir(t, binary, root, "", "run", file, "--target", target)
				if string(stderr) != test.stderr || code != test.code || len(stdout) != 0 {
					t.Errorf("%s: entry report differs:\ncode=%d want %d\nstderr=%q\nwant   %q\nstdout=%q", target, code, test.code, stderr, test.stderr, stdout)
				}
			}
		})
	}
}

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
			command := exec.Command(binary, "run", file, "--target", target)
			command.Dir = root
			var stderr bytes.Buffer
			stdout := &readyWriter{ready: make(chan struct{})}
			command.Stdout, command.Stderr = stdout, &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-stdout.ready:
			case <-time.After(60 * time.Second):
				_ = command.Process.Kill()
				_ = command.Wait()
				t.Fatalf("%s: program did not report ready: stderr=%q", target, stderr.String())
			}
			if err := command.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			var waitErr error
			select {
			case waitErr = <-done:
			case <-time.After(30 * time.Second):
				_ = command.Process.Kill()
				<-done
				t.Fatalf("%s: %v did not interrupt main", target, signal)
			}
			code := 0
			var exited *exec.ExitError
			if errors.As(waitErr, &exited) {
				code = exited.ExitCode()
			} else if waitErr != nil {
				t.Fatal(waitErr)
			}
			if stdout.String() != "ready\n" || stderr.String() != "interrupt\n" || code != 130 {
				t.Errorf("%s %v: code=%d stderr=%q; want 130 and %q", target, signal, code, stderr.String(), "interrupt\n")
			}
		}
	}
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
