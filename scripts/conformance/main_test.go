package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// cliEnv makes the test binary act as the conformance command, so process
// tests drive the real CLI without building a separate binary.
const cliEnv = "EFFRA_CONFORMANCE_TEST_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(cliEnv) == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	// Keep every git process, fixture and verifier alike, independent of the
	// developer's global and system configuration.
	os.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	os.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	os.Exit(m.Run())
}

// repoRoot is the Effra repository holding this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// runCLI runs the conformance command in dir with env as its whole
// environment and returns its exit code, stdout and stderr.
func runCLI(t *testing.T, dir string, env []string, args ...string) (int, string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, args...)
	command.Dir = dir
	command.Env = append(env, cliEnv+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err = command.Run()
	if _, ok := err.(*exec.ExitError); err != nil && !ok {
		t.Fatalf("run conformance CLI: %v", err)
	}
	return command.ProcessState.ExitCode(), stdout.String(), stderr.String()
}
