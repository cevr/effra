package lintpack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/lint"
)

// The built policy-lint program, run over the process protocol on real
// checked fixtures, reports exactly what in-process evaluation reports:
// statuses, findings and related locations. Its analysis identity is the
// in-process one qualified by the executable it ran.
func TestPolicyPackProcessMatchesInProcessEvaluation(t *testing.T) {
	dir := t.TempDir()
	// Windows starts only a file with a PATHEXT extension; the manifest's
	// extensionless path resolves to it.
	name := "policy-lint"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if output, err := exec.Command("go", "build", "-o", filepath.Join(dir, name), "./cmd/policy-lint").CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
	program, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(program)
	executable := "sha256:" + hex.EncodeToString(sum[:])
	// The manifest names the program relative to its own directory.
	manifest, err := Pack.Manifest(lint.Executable{Path: "policy-lint"})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := compiler.LintRegistry(manifest)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := func(failure, functions string) string {
		return `{"version":1,"rules":{"policy/provider-boundary":"off","policy/forbidden-failure":{"options":{"failure":"` + failure + `","functions":` + functions + `}}}}`
	}
	runs := []struct {
		fixture, config string
		findings        int
		complete        bool
	}{
		{"provider_boundary.ef", boundaryConfig, 2, true},
		{"provider_boundary.ef", `{"version":1,"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"severity":"warning","options":{"provider":"LiveMail","allow":["main","layered"]}}}}`, 1, true},
		{"provider_boundary.ef", `{"version":1,"rules":{"policy/forbidden-failure":"off","policy/provider-boundary":{"options":{"provider":"FakeMail","allow":["main"]}}}}`, 1, true},
		{"forbidden_failure.ef", forbidden("Denied", `["forwards","recovers","declaresOnly","mentions","main"]`), 1, true},
		{"forbidden_failure.ef", forbidden("Timeout", `["waits","main"]`), 1, true},
		{"forbidden_failure.ef", forbidden("GoError", `["parses","main"]`), 1, true},
		{"forbidden_failure.ef", forbidden("Missing", `["main"]`), 0, false},
	}
	for _, run := range runs {
		source, err := os.ReadFile("testdata/" + run.fixture)
		if err != nil {
			t.Fatal(err)
		}
		result := compiler.CompileAt(string(source), "go", "testdata")
		if !result.Checked {
			t.Fatal(result.Diagnostics)
		}
		parsed, err := lint.ParseConfig([]byte(run.config))
		if err != nil {
			t.Fatal(err)
		}
		configuration, problems := registry.Configure(parsed)
		if len(problems) > 0 {
			t.Fatal(problems)
		}
		want, err := lint.Evaluate(Pack, configuration, result.LintFacts())
		if err != nil {
			t.Fatal(err)
		}
		got, err := lint.Run(context.Background(), configuration, Pack.Namespace, result.LintFacts(), lint.RunOptions{Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		// The process ran with an empty selection, so it received only the
		// platform's required variables; in-process
		// evaluation is the authoring harness, never reusable.
		if execution := got.Analysis.Execution; execution == nil || execution.Executable != executable || !execution.Complete || len(execution.Variables) != len(lint.RequiredVariables(runtime.GOOS)) || want.Analysis.Execution != nil || !want.Analysis.InProcess || want.Analysis.ReuseScope != "none" {
			t.Fatalf("%s: execution identity %+v, in-process %+v", run.fixture, got.Analysis, want.Analysis)
		}
		qualified := got.Analysis
		qualified.Digest, qualified.Execution, qualified.InProcess, qualified.ReuseScope = want.Analysis.Digest, nil, true, "none"
		if !reflect.DeepEqual(qualified, want.Analysis) || got.Analysis.Digest == want.Analysis.Digest {
			t.Fatalf("%s: analysis %+v, in-process %+v", run.fixture, got.Analysis, want.Analysis)
		}
		got.Analysis = want.Analysis
		if !reflect.DeepEqual(got, want) || got.Complete != run.complete || len(got.Findings) != run.findings || got.Failure != nil {
			t.Fatalf("%s %s:\nprocess    %+v\nin-process %+v", run.fixture, run.config, got, want)
		}
	}
}
