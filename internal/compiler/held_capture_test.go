package compiler

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// heldCaptureCase is one consumer of a recipe that holds a managed handle.
// Refused rows must be refused by the shared checker on both targets with the
// named EF123; admitted rows must check on Go and carry only the Go-only File
// diagnostics on JS. A non-empty native output also runs the Go program.
type heldCaptureCase struct {
	name     string
	source   string
	refusal  string
	expected string
}

func heldCaptureFixture(t *testing.T) string {
	t.Helper()
	fixture, err := filepath.Abs(filepath.Join("..", "..", "examples", "fixture.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strconv.Quote(fixture)
}

func requireHeldCaptureCases(t *testing.T, cases []heldCaptureCase) {
	t.Helper()
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				r := CompileFor(row.source, target)
				if row.refusal != "" {
					found := false
					for _, d := range r.Diagnostics {
						found = found || (d.Code == "EF123" && strings.Contains(d.Message, row.refusal))
					}
					if r.Checked || !found {
						t.Fatalf("%s: expected EF123 %q: %+v", target, row.refusal, r.Diagnostics)
					}
					continue
				}
				for _, d := range r.Diagnostics {
					if target == "go" || d.Code != "EF110" {
						t.Fatalf("%s: expected admission: %+v", target, r.Diagnostics)
					}
				}
			}
			if row.expected != "" {
				runGenericDataNative(t, row.source, row.expected)
			}
		})
	}
}

const closedExecution = "recipe holds a value owned by a closed scope and cannot be executed"

// A recipe's held handles belong to the owner that executed the recipe which
// produced it. Timeout and child owners close before their result is
// observable, so a produced recipe holding their handles cannot run later,
// whichever form (recovery, plain factory, nested recovery, branch join,
// fork with or without join) carried it there. Outer-owner borrows and runs
// inside the owner stay admitted.
func TestHeldCapturesFollowExecutionOwners(t *testing.T) {
	fixture := heldCaptureFixture(t)
	prefix := `error WithFile { file: File }
error Busy
effect fn readFile(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn rejected(file: File) -> Effect<string, { IoError }> raises { WithFile } {
    fail WithFile { file: file }
}
service Work {
    effect fn job() -> Effect<string, { IoError }> raises { WithFile, Busy, IoError }
}
impl LiveWork for Work {
    effect fn job() -> Effect<string, { IoError }> raises { WithFile, Busy, IoError } {
        let file = run Files.openRead(` + fixture + `).provide<Files>(LiveFiles)
        run rejected(file)
    }
}
fn retrieve(failure: WithFile) -> Effect<string, { IoError }> { readFile(failure.file) }
fn idle(failure: Busy) -> Effect<string, { IoError }> { constant() }
effect fn constant() -> string raises { IoError } { "constant" }
effect fn produce() -> Effect<string, { IoError }> raises { IoError } {
    let file = run Files.openRead(` + fixture + `).provide<Files>(LiveFiles)
    readFile(file)
}
effect fn wrap(file: File) -> Effect<string, { IoError }> { readFile(file) }
effect fn inside() -> string raises { IoError } {
    let task = run produce()
    run task
}
`
	recovered := "Work.job().provide<Work>(LiveWork).recover<WithFile>(retrieve)"
	consumer := func(body string) string {
		return prefix + `effect fn consume(flag: bool) -> string raises { IoError, Timeout, Busy } uses { Scheduler } {
    scope {
` + body + `
    }
}
effect fn main() -> void raises { IoError, Timeout, Busy } {
    run Console.log(run consume(true).provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)
}
`
	}
	const outer = "        let file = run Files.openRead(%s).provide<Files>(LiveFiles)\n"
	borrow := strings.Replace(outer, "%s", fixture, 1)
	requireHeldCaptureCases(t, []heldCaptureCase{
		{name: "timeout/recovery", refusal: closedExecution, source: consumer(`        let task = run ` + recovered + `.timeout(5000)
        run task.catch<IoError>("closed")`)},
		{name: "timeout/factory", refusal: closedExecution, source: consumer(`        let task = run produce().timeout(5000)
        run task.catch<IoError>("closed")`)},
		{name: "timeout/nested-recovery", refusal: closedExecution, source: consumer(`        let task = run ` + recovered + `.recover<Busy>(idle).timeout(5000)
        run task.catch<IoError>("closed")`)},
		{name: "timeout/branch-join", refusal: closedExecution, source: consumer(`        let closed = run produce().timeout(5000)
        let open = run produce()
        let task = if flag { closed } else { open }
        run task.catch<IoError>("closed")`)},
		{name: "fork-join/recovery", refusal: closedExecution, source: consumer(`        let child = fork ` + recovered + `
        let task = run child.join()
        run task.catch<IoError>("closed")`)},
		{name: "fork-join/factory", refusal: closedExecution, source: consumer(`        let child = fork produce()
        let task = run child.join()
        run task.catch<IoError>("closed")`)},
		{name: "fork-without-join", refusal: closedExecution, source: consumer(`        let task = run produce().timeout(5000)
        let child = fork task.catch<IoError>("closed")
        "forked"`)},
		{name: "control/run-in-scope", expected: "scoped file read complete\n\n", source: consumer(`        let task = run ` + recovered + `
        run task.catch<IoError>("closed")`)},
		{name: "control/inside-timeout", expected: "scoped file read complete\n\n", source: consumer(`        run inside().timeout(5000)`)},
		{name: "control/inside-child", expected: "scoped file read complete\n\n", source: consumer(`        let child = fork inside()
        run child.join()`)},
		{name: "control/outer-borrow-timeout", expected: "scoped file read complete\n\n", source: consumer(borrow + `        let task = run wrap(file).timeout(5000)
        run task.catch<IoError>("closed")`)},
		{name: "control/outer-borrow-join", expected: "scoped file read complete\n\n", source: consumer(borrow + `        let child = fork wrap(file)
        let task = run child.join()
        run task.catch<IoError>("closed")`)},
	})
}

const scopeEscape = "value owned by closing scope cannot escape"

// An opaque producer (a callable parameter, or a local callable value) may
// acquire a handle while it runs and return a recipe holding it. Its produced
// recipe is held by the owner that runs the invocation: usable there, refused
// at that owner's scope edge, and refused when run after a timeout owner
// closed. Recovery handlers and direct callable calls share this summary.
func TestHeldCapturesOfOpaqueProducersBelongToTheRunner(t *testing.T) {
	fixture := heldCaptureFixture(t)
	prefix := `error Missing
record Reader { task: Effect<string, { IoError }> }
effect fn readFile(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn job() -> Effect<string, { IoError }> raises { Missing } { fail Missing }
effect fn acquire(failure: Missing) -> Effect<string, { IoError }> raises { IoError } {
    let file = run Files.openRead(` + fixture + `).provide<Files>(LiveFiles)
    readFile(file)
}
effect fn acquireNoPayload() -> Effect<string, { IoError }> raises { IoError } {
    let file = run Files.openRead(` + fixture + `).provide<Files>(LiveFiles)
    readFile(file)
}
`
	recoverHandler := "handler: effect fn(Missing) -> Effect<string, { IoError }> raises { IoError }"
	callHandler := "handler: effect fn() -> Effect<string, { IoError }> raises { IoError }"
	escape := func(parameter, produce, argument string) string {
		return prefix + `effect fn escape(` + parameter + `) -> Reader raises { IoError } {
    scope {
` + produce + `
        Reader { task: task }
    }
}
effect fn main() -> void raises { IoError } {
    let reader = run escape(` + argument + `)
    run Console.log(run reader.task.catch<IoError>("closed")).provide<Console>(Stdout)
}
`
	}
	consume := func(parameter, produce, argument string) string {
		return prefix + `effect fn consume(` + parameter + `) -> string raises { IoError } {
    scope {
` + produce + `
        run task
    }
}
effect fn main() -> void raises { IoError } {
    run Console.log(run consume(` + argument + `)).provide<Console>(Stdout)
}
`
	}
	const recovered = "        let task = run job().recover<Missing>(handler)"
	const called = "        let task = run handler()"
	const aliased = "        let local = acquireNoPayload\n        let task = run local()"
	requireHeldCaptureCases(t, []heldCaptureCase{
		{name: "recover/parameter-handler", refusal: scopeEscape, source: escape(recoverHandler, recovered, "acquire")},
		{name: "call/parameter", refusal: scopeEscape, source: escape(callHandler, called, "acquireNoPayload")},
		{name: "call/local-value", refusal: scopeEscape, source: escape("", aliased, "")},
		{name: "call/parameter-timeout", refusal: closedExecution, source: prefix + `effect fn late(` + callHandler + `) -> string raises { IoError, Timeout } uses { Scheduler } {
    scope {
        let task = run handler().timeout(5000)
        run task.catch<IoError>("closed")
    }
}
effect fn main() -> void { void }
`},
		{name: "control/recover-in-scope", expected: "scoped file read complete\n\n", source: consume(recoverHandler, recovered, "acquire")},
		{name: "control/call-in-scope", expected: "scoped file read complete\n\n", source: consume(callHandler, called, "acquireNoPayload")},
		{name: "control/local-value-in-scope", expected: "scoped file read complete\n\n", source: consume("", aliased, "")},
	})
}

// A service operation runs in whichever provider is in scope, so its result
// is possibly owned whatever the service or provider name: a handle it
// returns, or one held by a recipe it returns, is owned by the owner which
// runs it. A scope cannot leak one, while uses inside the scope and results
// returned from an invocation stay admitted.
func TestHeldCapturesOfServiceOperationsBelongToTheRunner(t *testing.T) {
	fixture := heldCaptureFixture(t)
	files := `effect fn read() -> string raises { IoError } uses { Files } {
    let h = scope { run Files.openRead(` + fixture + `) }
    run Files.readText(h)
}
effect fn main() -> void raises { IoError } {
    let t = run read().provide<Files>(LiveFiles)
    run Console.log(t).provide<Console>(Stdout)
}
`
	store := `service Store {
    effect fn open() -> File raises { IoError }
    effect fn reader() -> Effect<string, { IoError }> raises { IoError }
}
record Reader { task: Effect<string, { IoError }> }
impl LiveStore for Store {
    effect fn open() -> File raises { IoError } {
        run Files.openRead(` + fixture + `).provide<Files>(LiveFiles)
    }
    effect fn reader() -> Effect<string, { IoError }> raises { IoError } {
        let file = run Files.openRead(` + fixture + `).provide<Files>(LiveFiles)
        Files.readText(file).provide<Files>(LiveFiles)
    }
}
`
	requireHeldCaptureCases(t, []heldCaptureCase{
		{name: "files/unprovided-escape", refusal: scopeEscape, source: `effect fn f() -> string raises { IoError } uses { Files } {
    let h = scope { run Files.openRead("x") }
    run Files.readText(h)
}
`},
		{name: "files/provided-later-escape", refusal: scopeEscape, source: files},
		{name: "service/handle-escape", refusal: scopeEscape, source: store + `effect fn leak() -> File raises { IoError } {
    scope { run Store.open().provide<Store>(LiveStore) }
}
effect fn main() -> void { void }
`},
		{name: "service/recipe-escape", refusal: scopeEscape, source: store + `effect fn leak() -> Reader raises { IoError } {
    scope {
        let task = run Store.reader().provide<Store>(LiveStore)
        Reader { task: task }
    }
}
effect fn main() -> void { void }
`},
		{name: "control/files-in-scope", expected: "scoped file read complete\n\n", source: strings.Replace(files, "let h = scope { run Files.openRead("+fixture+") }\n    run Files.readText(h)", "scope {\n        let h = run Files.openRead("+fixture+")\n        run Files.readText(h)\n    }", 1)},
		{name: "control/service-in-scope", expected: "scoped file read complete\nscoped file read complete\n\n", source: store + `effect fn use() -> string raises { IoError } {
    scope {
        let file = run Store.open().provide<Store>(LiveStore)
        let task = run Store.reader().provide<Store>(LiveStore)
        run Files.readText(file).provide<Files>(LiveFiles) + run task
    }
}
effect fn main() -> void raises { IoError } {
    run Console.log(run use()).provide<Console>(Stdout)
}
`},
		{name: "control/service-returned", expected: "scoped file read complete\n\n", source: store + `effect fn open() -> File raises { IoError } {
    run Store.open().provide<Store>(LiveStore)
}
effect fn main() -> void raises { IoError } {
    let file = run open()
    run Console.log(run Files.readText(file).provide<Files>(LiveFiles)).provide<Console>(Stdout)
}
`},
	})
}

// A closed handle refused where it is passed is reported once: the recipe
// built from the refused argument does not report it again when it runs.
func TestHeldCapturesReportAClosedArgumentOnce(t *testing.T) {
	fixture := heldCaptureFixture(t)
	for name, body := range map[string]string{
		"timeout": `        let file = run Files.openRead(` + fixture + `).provide<Files>(LiveFiles).timeout(5000)`,
		"fork":    "        let child = fork Files.openRead(" + fixture + ").provide<Files>(LiveFiles)\n        let file = run child.join()",
	} {
		t.Run(name, func(t *testing.T) {
			source := `effect fn consume() -> string raises { IoError, Timeout } uses { Scheduler } {
    scope {
` + body + `
        run Files.readText(file).provide<Files>(LiveFiles).catch<IoError>("closed")
    }
}
`
			for _, target := range []string{"go", "js"} {
				r := CompileFor(source, target)
				if count := diagnosticCount(r, "EF123"); count != 1 || !hasDiagnosticMessage(r, "value owned by a closing scope cannot be used") {
					t.Fatalf("%s: want one argument refusal: %+v", target, r.Diagnostics)
				}
			}
		})
	}
}
