//go:build windows

package lint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows starts the first of path.com, path.exe, ... for a manifest path
// without a PATHEXT extension; the runner hashes and starts exactly that
// file. A batch file is refused, since cmd.exe would parse its arguments
// with its own rules: selection refuses it as an invocation error, and a
// run fails as spawn-failed without starting anything. These run over real
// files through the production lookup.
func TestWindowsProgramResolution(t *testing.T) {
	dir := t.TempDir()
	program, err := os.ReadFile(fixturePack(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"policy.exe": program, "batch.BAT": []byte("@echo off\r\n"), "script.cmd": []byte("@echo off\r\n")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	manifest := func(path string) Manifest {
		t.Helper()
		manifest := fixtureManifest(t, "serve")
		manifest.Executable.Path = path
		return manifest
	}
	run := func(path string) Report {
		t.Helper()
		registry, err := NewRegistry(testBuiltins, manifest(path))
		if err != nil {
			t.Fatal(err)
		}
		report, err := Run(context.Background(), configure(t, registry, `{"version":1}`), "fixture", testSnapshot(), RunOptions{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}

	resolved := run("policy")
	if want := filepath.Join(dir, "policy.exe"); !resolved.Complete || !strings.EqualFold(resolved.Analysis.Execution.Program, want) {
		t.Fatalf("policy resolved to %+v, want %s", resolved.Analysis.Execution, want)
	}
	for _, path := range []string{"batch", "script.cmd"} {
		data, _ := json.Marshal(manifest(path))
		file := filepath.Join(dir, strings.TrimSuffix(path, ".cmd")+".json")
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(file); err == nil || !strings.Contains(err.Error(), "batch file") {
			t.Errorf("%s: selection admitted a batch file: %v", path, err)
		}
		if report := run(path); report.Failure == nil || report.Failure.Code != FailureSpawn || !strings.Contains(report.Failure.Message, "batch file") {
			t.Errorf("%s: a batch file ran: %+v", path, report)
		}
	}
}
