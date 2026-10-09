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
// scheduling. This test builds one cause at the entry boundary instead and
// hands it to each target's generated entry report, keeping the compiler's
// plan, the runtime renderer and the exit status on the tested path. The
// payload mismatch and the label outside main's row are negative controls:
// neither target guesses a rendering from a value's shape.
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

func TestEntryReportRendersACompositeCauseIdenticallyOnBothTargets(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			var stderr string
			var code int
			if target == "go" {
				stderr, code = runGoEntryReportComposite(t)
			} else {
				stderr, code = runJSEntryReportComposite(t)
			}
			if stderr != entryReportCompositeWant || code != 1 {
				t.Fatalf("code=%d stderr=%q\nwant code=1 stderr=%q", code, stderr, entryReportCompositeWant)
			}
		})
	}
}

func runGoEntryReportComposite(t *testing.T) (string, int) {
	t.Helper()
	r := CompileAt(entryReportCompositeSource, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	// The probe's init runs before main and exits with the entry's status.
	probe := `package main
import ("errors"; "fmt"; "os"; er ` + strconv.Quote(generatedModulePath+"/runtime") + `)
func init() {
	cause := er.Cause{
		{Kind: "failure", Failure: &er.Failure{Tag: "Bad"}},
		{Kind: "failure", Failure: &er.Failure{Tag: "Worse", Payload: efType_Worse{` + goFieldName("reason") + `: "second"}}},
		{Kind: "failure", Failure: &er.Failure{Tag: "Worse", Payload: "not a Worse"}},
		{Kind: "failure", Failure: &er.Failure{Tag: "Stranger"}},
		{Kind: "defect", Err: errors.New("boom\nline")},
		{Kind: "interrupt"},
	}
	fmt.Fprint(os.Stderr, efEntryReport.Report(cause))
	os.Exit(er.EntryExitCode(cause))
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

func runJSEntryReportComposite(t *testing.T) (string, int) {
	t.Helper()
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for backend conformance tests")
	}
	r := CompileFor(entryReportCompositeSource, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	js, _, err := r.Emit(true)
	if err != nil {
		t.Fatal(err)
	}
	entry := "__ef_runEntry(__ef_function_main(), __ef_entryReport);"
	if strings.Count(js, entry) != 1 {
		t.Fatalf("entry call not found:\n%s", js)
	}
	cause := `__ef_runEntry(Effect.failCause(__probe_Cause.fromReasons([
  __probe_Cause.makeFailReason({ _tag: "Bad" }),
  __probe_Cause.makeFailReason({ _tag: "Worse", reason: "second" }),
  __probe_Cause.makeFailReason({ _tag: "Worse", reason: 5n }),
  __probe_Cause.makeFailReason({ _tag: "Stranger" }),
  ...__probe_Cause.die(new Error("boom\nline")).reasons,
  ...__probe_Cause.interrupt().reasons,
])), __ef_entryReport);`
	js = "import { Cause as __probe_Cause } from 'effect';\n" + strings.Replace(js, entry, cause, 1)
	path := writeGeneratedJS(t, "entry-report-", js)
	return entryReportOutcome(t, exec.Command(bun, path))
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
