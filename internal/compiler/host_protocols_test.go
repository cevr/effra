package compiler

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"effra.local/prototype/examples/hosttypes"
)

// Methods follow Go's method sets: a value-receiver method is callable on a
// value, a pointer-receiver method mutates the original pointer rather than a
// copy, and a method call is a lazy recipe that captured its receiver.
func TestHostMethodSetsSelectPointerAndValueReceivers(t *testing.T) {
	r := compileHostTypes(t, `effect fn program() -> void uses { Console, Foreign } {
    let point = run host.Offset(run host.Origin(), 4)
    run Console.log(run strconv.FormatInt(run point.Sum(), 10))
    match run host.Find("known") {
        Data.Option.None => void,
        Data.Option.Some { value: counter } => {
            let step = counter.Increment()
            let before = run host.Count(counter)
            let first = run step
            let second = run step
            run Console.log(run strconv.Itoa(before) + " " + run strconv.Itoa(first) + " " + run strconv.Itoa(second) + " " + run strconv.Itoa(run host.Count(counter)))
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "4\n3 4 5 5\n" {
		t.Fatalf("method sets: %q", output)
	}
	var increment Binding
	for _, binding := range r.Bindings {
		if binding.Symbol == "(*host.Counter).Increment" {
			increment = binding
		}
	}
	want := HostComponent{Native: "*effra.local/prototype/examples/hosttypes.Counter", Type: "*host.Counter", Adaptation: hostAdaptReceiver}
	if len(increment.HostParameters) != 1 || increment.HostParameters[0] != want || increment.Return != "int" {
		t.Fatalf("method binding inspection: %+v", increment)
	}
	for _, tc := range []struct{ name, body, code, message string }{
		{"pointer receiver on a value", `let point = run host.Origin()
    run point.Grow()`, "EF112", "pointer receiver and a host.Point value is not addressable"},
		{"missing method", `let point = run host.Origin()
    run point.Missing()`, "EF112", "host.Point has no method Missing"},
		{"argument type", `match run host.NewSink() {
        Data.Option.None => void,
        Data.Option.Some { value: sink } => {
            let written = run sink.ReadFrom("text")
            void
        }
    }`, "EF106", "Go argument must be io.Reader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compileHostTypes(t, "effect fn program() -> void uses { Console, Foreign } {\n    "+tc.body+"\n}")
			if r.Checked || !hasCode(r, tc.code) || !hasDiagnosticContaining(r, tc.message) {
				t.Fatalf("expected %s %q: %+v", tc.code, tc.message, r.Diagnostics)
			}
		})
	}
}

const hostProtocolsImport = `import go host "effra.local/prototype/examples/hosttypes"
import go io "io"
import go strconv "strconv"
import Data "effra/data"
`

// A concrete host value is passed to a native interface directly, so
// io.Copy sees the original object's optional methods: WriterTo on the source
// before ReaderFrom on the destination. A native Go control fixes the
// expected dispatch, and an incompatible value is refused.
func TestHostInterfaceAssignmentPreservesOptionalMethods(t *testing.T) {
	r := CompileAt(hostProtocolsImport+`effect fn copied(n: i64, source: string, sink: *host.Sink) -> string uses { Foreign } {
    run strconv.FormatInt(n, 10) + " " + source + " " + run sink.Report()
}
effect fn fast(source: *host.Source, sink: *host.Sink) -> string uses { Foreign } {
    let result = run io.Copy(sink, source)
    run copied(result.value, run source.Report(), sink)
}
effect fn plain(source: *host.Plain, sink: *host.Sink) -> string uses { Foreign } {
    let result = run io.Copy(sink, source)
    run copied(result.value, "plain", sink)
}
effect fn program() -> void uses { Console, Foreign } {
    match run host.NewSink() {
        Data.Option.None => void,
        Data.Option.Some { value: sink } => match run host.NewSource("abc") {
            Data.Option.None => void,
            Data.Option.Some { value: source } => run Console.log(run fast(source, sink))
        }
    }
    match run host.NewSink() {
        Data.Option.None => void,
        Data.Option.Some { value: sink } => match run host.NewPlain("xyz") {
            Data.Option.None => void,
            Data.Option.Some { value: source } => run Console.log(run plain(source, sink))
        }
    }
}`+hostTypesEntry, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	source, sink := hosttypes.NewSource("abc"), hosttypes.NewSink()
	n, err := io.Copy(sink, source)
	if err != nil {
		t.Fatal(err)
	}
	plainSink := hosttypes.NewSink()
	m, err := io.Copy(plainSink, hosttypes.NewPlain("xyz"))
	if err != nil {
		t.Fatal(err)
	}
	want := strconv.FormatInt(n, 10) + " " + source.Report() + " " + sink.Report() + "\n" + strconv.FormatInt(m, 10) + " plain " + plainSink.Report() + "\n"
	if !strings.Contains(want, "read=0 writeTo=1") || !strings.Contains(want, "write=0 readFrom=1") {
		t.Fatalf("native control did not take the optional-method paths: %q", want)
	}
	if output := runGeneratedGo(t, r); output != want {
		t.Fatalf("io.Copy dispatch:\n%s\nnative:\n%s", output, want)
	}
	refused := CompileAt(hostProtocolsImport+`effect fn program() -> void uses { Console, Foreign } {
    match run host.Find("known") {
        Data.Option.None => void,
        Data.Option.Some { value: counter } => match run host.NewSink() {
            Data.Option.None => void,
            Data.Option.Some { value: sink } => {
                let result = run io.Copy(sink, counter)
                void
            }
        }
    }
}`+hostTypesEntry, "go", "../..")
	if refused.Checked || !hasDiagnosticContaining(refused, "Go argument must be io.Reader") {
		t.Fatalf("counter accepted as io.Reader: %+v", refused.Diagnostics)
	}
}

func hasDiagnosticContaining(r *Result, message string) bool {
	for _, diagnostic := range r.Diagnostics {
		if strings.Contains(diagnostic.Message, message) {
			return true
		}
	}
	return false
}

// value.as<T>() is Go's comma-ok assertion with the match status kept beside
// the adapted value: a matched typed-nil pointer is (None, true), a failed
// match (None, false), and a matched present value is Some with the original
// native object. A non-nullable target is absent only on a failed match.
func TestHostAssertionKeepsMatchStatusSeparateFromTypedNil(t *testing.T) {
	r := compileHostTypes(t, `fn status(found: bool) -> string {
    if found { "matched" } else { "unmatched" }
}
fn problem(err: error) -> string {
    let asserted = err.as<*host.Problem>()
    match asserted.v0 {
        Data.Option.None => "none " + status(asserted.v1),
        Data.Option.Some { value } => "some " + status(asserted.v1)
    }
}
effect fn failure(typed: bool) -> string uses { Foreign } {
    let result = if typed { run host.Typed() } else { run host.Missing() }
    match result.error {
        Data.Option.None => "no error",
        Data.Option.Some { value: native } => problem(native)
    }
}
effect fn square() -> string uses { Foreign } {
    match run host.MakeShape("square") {
        Data.Option.None => "nil interface",
        Data.Option.Some { value: shape } => {
            let asserted = shape.as<*host.Square>()
            match asserted.v0 {
                Data.Option.None => "none " + status(asserted.v1),
                Data.Option.Some { value: present } => run present.Kind() + " " + status(asserted.v1)
            }
        }
    }
}
effect fn boxed() -> string uses { Foreign } {
    match run host.Boxed() {
        Data.Option.None => "nil any",
        Data.Option.Some { value: box } => {
            let text = match box.as<string>().v0 {
                Data.Option.None => "none",
                Data.Option.Some { value } => value
            }
            let number = box.as<int>()
            text + " " + status(number.v1)
        }
    }
}
effect fn program() -> void uses { Console, Foreign } {
    run Console.log(run failure(true) + "; " + run failure(false))
    run Console.log(run square() + "; " + run boxed())
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "none matched; none unmatched\nsquare matched; text unmatched\n" {
		t.Fatalf("assertions: %q", output)
	}
	for _, tc := range []struct{ name, body, message string }{
		{"impossible", `match run host.MakeShape("square") {
        Data.Option.None => void,
        Data.Option.Some { value: shape } => {
            let asserted = shape.as<host.Point>()
            void
        }
    }`, "impossible assertion: host.Point does not implement host.Shape"},
		{"concrete receiver", `let point = run host.Origin()
    let asserted = point.as<host.Point>()`, "as<host.Point> requires a native interface value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compileHostTypes(t, "effect fn program() -> void uses { Console, Foreign } {\n    "+tc.body+"\n}")
			if r.Checked || !hasDiagnosticContaining(r, tc.message) {
				t.Fatalf("expected %q: %+v", tc.message, r.Diagnostics)
			}
		})
	}
}

// Native int widens to i64 totally; i64 narrows to Option<int>, checked
// against the platform int, and neither conversion is implicit.
func TestHostIntegerConversionsAreExplicitAndChecked(t *testing.T) {
	r := compileHostTypes(t, `effect fn program() -> void uses { Console, Foreign } {
    match run host.Find("known") {
        Data.Option.None => void,
        Data.Option.Some { value: counter } => {
            let wide = i64(run host.Count(counter))
            let narrowed = match int(wide) {
                Data.Option.None => "too wide",
                Data.Option.Some { value } => run strconv.Itoa(value)
            }
            run Console.log(run strconv.FormatInt(wide, 10) + " " + narrowed)
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "3 3\n" {
		t.Fatalf("conversions: %q", output)
	}
	for _, tc := range []struct{ body, message string }{
		{`run Console.log(run strconv.Itoa(i64(4)))`, "i64 conversion requires a native int"},
		{`let n = int("4")`, "int conversion requires an i64"},
		{`run Console.log(run strconv.FormatInt(int(4), 10))`, "Go argument must be i64"},
	} {
		r := compileHostTypes(t, "effect fn program() -> void uses { Console, Foreign } {\n    "+tc.body+"\n}")
		if r.Checked || !hasDiagnosticContaining(r, tc.message) {
			t.Fatalf("%s: expected %q: %+v", tc.body, tc.message, r.Diagnostics)
		}
	}
	if r := CompileAt("effect fn main() -> void {\n    let n = i64(4)\n}", "go", "../.."); r.Checked {
		t.Fatal("i64 conversion admitted without a Go import")
	}
}
