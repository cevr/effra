package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Ported from scripts/diagnostics_smoke.py (identity group): replacing the
// selected symlink while the Go import runs must not mix documents. The
// report keeps the requested URI and the bytes and findings the check
// actually read. /proc child lists synchronize on the import's start, so this
// case is Linux-only; the static URI control in diagnostics_smoke_test.go is
// portable.
func TestDiagnosticsSmokeSymlinkReplacedDuringGoImport(t *testing.T) {
	binary := buildTestCLI(t)
	for _, surface := range []string{"cli", "mcp"} {
		t.Run(surface, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			changing, baseline := diagnosticsSmokeSymlinkFixture(t, binary, directory, "changing-"+surface+".ef")
			arguments := []string{"diagnostics", changing, "--json"}
			if surface == "mcp" {
				arguments = []string{"mcp", directory}
			}
			command := exec.Command(binary, arguments...)
			command.Dir = directory
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			stdin, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- command.Wait() }()
			exited := false
			t.Cleanup(func() {
				if exited {
					return
				}
				_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
					<-done
				}
			})
			if surface == "mcp" {
				if _, err := stdin.Write([]byte(diagnosticsSmokeRequests(t, []map[string]any{{"file": filepath.Base(changing)}}))); err != nil {
					t.Fatal(err)
				}
			}
			// Poll a causal child-start condition, not a correctness delay.
			deadline := time.After(15 * time.Second)
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for !diagnosticsSmokeHasChildren(command.Process.Pid) {
				select {
				case err := <-done:
					exited = true
					t.Fatalf("Go import never started; exited with %v: %s", err, stderr.Bytes())
				case <-deadline:
					t.Fatal("Go import never started")
				case <-ticker.C:
				}
			}
			replacement := filepath.Join(directory, "replacement-"+surface+".ef")
			if err := os.Symlink("two.ef", replacement); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(replacement, changing); err != nil {
				t.Fatal(err)
			}
			if err := stdin.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				exited = true
				if err != nil {
					t.Fatalf("exit %v: %s", err, stderr.Bytes())
				}
			case <-time.After(30 * time.Second):
				t.Fatal("check did not finish")
			}
			var report map[string]any
			if surface == "cli" {
				report = smokeJSON(t, stdout.Bytes())
			} else {
				replies := diagnosticsSmokeReplies(t, stdout.Bytes())
				if replies[len(replies)-1]["id"] != "after" {
					t.Fatalf("replies = %v", replies)
				}
				report = diagnosticsSmokeStructured(t, replies[1])
			}
			if report["checked"] != true {
				t.Fatalf("unchecked: %v", report)
			}
			diagnosticsSmokeAssertSelected(t, report, changing, baseline)
		})
	}
}

// diagnosticsSmokeHasChildren reports whether any thread of the process has
// started a child.
func diagnosticsSmokeHasChildren(pid int) bool {
	lists, _ := filepath.Glob("/proc/" + strconv.Itoa(pid) + "/task/*/children")
	for _, list := range lists {
		// An OS thread may finish while its child list is inspected.
		if content, err := os.ReadFile(list); err == nil && strings.TrimSpace(string(content)) != "" {
			return true
		}
	}
	return false
}
