package compiler

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// A composite cause cannot be produced deterministically from source: scope
// close interrupts unobserved children, so which of them still fail depends on
// scheduling. This test replaces only the computation the generated entry
// runs with one that exits with a fixed composite cause; the generated entry
// (signal context, report plan, renderer and exit status) is unchanged. The
// payload mismatch and the label outside main's row are negative controls:
// the entry never guesses a rendering from a value's shape. Each mutant
// breaks the generated entry and must make the test fail.
const entryReportCompositeSource = `error Bad
error Worse {
    reason: string
}
effect fn bad() -> void raises { Bad } {
    fail Bad
}
effect fn worse() -> void raises { Worse } {
    fail Worse { reason: "first" }
}
effect fn main() -> void raises { Bad, Worse } {
    run bad()
    run worse()
}
`

const entryReportCompositeWant = "failure: Bad\n" +
	"failure: Worse { reason: \"second\" }\n" +
	"failure: Worse { reason: <opaque> }\n" +
	"failure: Stranger\n" +
	"defect: \"boom\\nline\"\n" +
	"interrupt\n"

// entryMutant replaces from, which must occur exactly once, with to.
type entryMutant struct{ name, from, to string }

func (m entryMutant) apply(t *testing.T, program string) string {
	t.Helper()
	if strings.Count(program, m.from) != 1 {
		t.Fatalf("%s: %q does not occur exactly once in the generated program", m.name, m.from)
	}
	return strings.Replace(program, m.from, m.to, 1)
}

func TestEntryReportRendersACompositeCauseIdenticallyOnBothTargets(t *testing.T) {
	stderr, code := runGoEntryReportComposite(t, nil)
	if stderr != entryReportCompositeWant || code != 1 {
		t.Fatalf("code=%d stderr=%q\nwant code=1 stderr=%q", code, stderr, entryReportCompositeWant)
	}
	for _, mutant := range []entryMutant{
		{"truncated cause", "efEntryReport.Report(cause)", "efEntryReport.Report(cause[:1])"},
		{"wrong mixed-cause status", "os.Exit(er.EntryExitCode(cause))", "os.Exit(130)"},
	} {
		stderr, code := runGoEntryReportComposite(t, []entryMutant{mutant})
		if stderr == entryReportCompositeWant && code == 1 {
			t.Errorf("mutant %q survived: the test does not observe the generated entry", mutant.name)
		}
	}
}

// runGoEntryReportComposite builds the generated program with main's
// computation replaced by one exiting with the composite cause.
func runGoEntryReportComposite(t *testing.T, mutants []entryMutant) (string, int) {
	t.Helper()
	r := CompileAt(entryReportCompositeSource, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	inject := entryMutant{"composite computation", "efFunction_main()(efContext{Runtime: fc})", "efProbeComposite()"}
	code = inject.apply(t, code)
	for _, mutant := range mutants {
		code = mutant.apply(t, code)
	}
	probe := `package main
import ("errors"; er ` + strconv.Quote(generatedModulePath+"/runtime") + `)
func efProbeComposite() er.Exit[struct{}] {
	return er.FromCause[struct{}](er.Cause{
		{Kind: "failure", Failure: &er.Failure{Tag: "Bad"}},
		{Kind: "failure", Failure: &er.Failure{Tag: "Worse", Payload: efType_Worse{` + goFieldName("reason") + `: "second"}}},
		{Kind: "failure", Failure: &er.Failure{Tag: "Worse", Payload: "not a Worse"}},
		{Kind: "failure", Failure: &er.Failure{Tag: "Stranger"}},
		{Kind: "defect", Err: errors.New("boom\nline")},
		{Kind: "interrupt"},
	})
}
`
	dir := t.TempDir()
	if err := application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string][]byte{"go.mod": r.ModuleFile(), "main.go": []byte(code), "probe.go": []byte(probe)} {
		if err := os.WriteFile(filepath.Join(dir, name), contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return entryReportOutcome(t, exec.Command(buildGoModule(t, dir)))
}

func entryReportOutcome(t *testing.T, command *exec.Cmd) (string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var exited *exec.ExitError
	if err != nil && !errors.As(err, &exited) {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout=%q, want empty", stdout.String())
	}
	return stderr.String(), command.ProcessState.ExitCode()
}
