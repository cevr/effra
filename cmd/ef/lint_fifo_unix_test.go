//go:build unix

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"effra.local/prototype/examples/lintpack"
	"effra.local/prototype/lint"
)

// A lint configuration, manifest, pack executable, fixture or expectation
// that is a FIFO is refused promptly as an invalid invocation (exit 2):
// admission never opens a special file, which would wait for a writer
// before any pack timeout applies.
func TestLintRefusesFIFOInputsPromptly(t *testing.T) {
	binary := buildTestCLI(t)
	dir := t.TempDir()
	fifo := func(name string) string {
		path := filepath.Join(dir, name)
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write := func(name, text string, mode os.FileMode) string {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	manifestFor := func(name, executable string) string {
		manifest, err := lintpack.Pack.Manifest(lint.Executable{Path: executable})
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(manifest)
		return write(name, string(data), 0o644)
	}
	config := func(name, manifest string) string {
		return write(name, `{"version":1,"packs":[{"manifest":"`+manifest+`"}],"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"options":{"provider":"LiveMail","allow":["main"]}}}}`, 0o644)
	}
	source := write("ok.ef", "effect fn main() -> void { void }\n", 0o644)
	// The program is a regular file; no case here may start it.
	write("program/policy-lint", "not a program", 0o755)
	regular := manifestFor("program/policy.json", "policy-lint")
	fifo("fifo-program")
	program := manifestFor("fifo-program.json", "fifo-program")
	fixtures := filepath.Join(dir, "fixtures")
	write("fixtures/fixture.ef", "effect fn main() -> void { void }\n", 0o644)
	if err := syscall.Mkfifo(filepath.Join(fixtures, "fixture.lint.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo("piped.ef")

	cases := []struct {
		name string
		args []string
	}{
		{"configuration", []string{"lint", "rules", "--lint-config", fifo("lint.json")}},
		{"manifest", []string{"lint", "rules", "--rules", fifo("manifest.json")}},
		{"selected manifest", []string{"lint", source, "--lint-config", config("selects-fifo.json", "manifest.json")}},
		{"executable", []string{"lint", source, "--lint-config", config("program.json", "fifo-program.json")}},
		{"executable under diagnostics", []string{"diagnostics", source, "--lint-config", config("program-diagnostics.json", program)}},
		{"expectation", []string{"lint", "test", filepath.Join(fixtures, "fixture.ef"), "--lint-config", config("fixtures.json", regular)}},
		{"fixture", []string{"lint", "test", filepath.Join(dir, "piped.ef"), "--lint-config", config("fixture.json", regular)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, c.args...)
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if ctx.Err() != nil {
				t.Fatalf("%v blocked on a FIFO", c.args)
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(stderr.String(), "not a regular file") {
				t.Fatalf("%v: %v %s %s", c.args, err, stdout.String(), stderr.String())
			}
		})
	}
}
