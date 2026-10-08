package lintpack

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/lint"
)

// The built policy-lint program, run over the process protocol on real
// checked fixtures, reports exactly what in-process evaluation reports:
// statuses, findings, related locations and analysis identity.
func TestPolicyPackProcessMatchesInProcessEvaluation(t *testing.T) {
	dir := t.TempDir()
	if output, err := exec.Command("go", "build", "-o", filepath.Join(dir, "policy-lint"), "./cmd/policy-lint").CombinedOutput(); err != nil {
		t.Fatal(string(output))
	}
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
		if !reflect.DeepEqual(got, want) || got.Complete != run.complete || len(got.Findings) != run.findings || got.Failure != nil {
			t.Fatalf("%s %s:\nprocess    %+v\nin-process %+v", run.fixture, run.config, got, want)
		}
	}
}
