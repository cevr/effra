package compiler

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
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

// A receiver can be any executed host-valued expression. It is evaluated once
// when the method recipe is constructed, and the method runs on that original
// value each time the recipe executes.
func TestHostMethodReceiverExpressionsAreCapturedOnce(t *testing.T) {
	r := compileHostTypes(t, `effect fn pick(counter: *host.Counter) -> *host.Counter uses { Console } {
    run Console.log("picked")
    counter
}
effect fn program() -> void uses { Console, Foreign } {
    run Console.log(run strconv.FormatInt(run (run host.Offset(run host.Origin(), 4)).Sum(), 10))
    match run host.Find("known") {
        Data.Option.None => void,
        Data.Option.Some { value: counter } => {
            let step = (run pick(counter)).Increment()
            let first = run step
            let second = run step
            run Console.log(run strconv.Itoa(first) + " " + run strconv.Itoa(second))
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "4\npicked\n4 5\n" {
		t.Fatalf("receiver capture: %q", output)
	}
	for _, tc := range []struct{ body, message string }{
		{`run Console.log(run strconv.FormatInt(run host.Origin().Sum(), 10))`, "Go method Sum requires an executed receiver"},
		{`let point = run host.Origin()
    let sum = point.Sum`, "Go method Sum must be called"},
		{`let point = run host.Origin()
    let x = point.X`, "Go method X must be called"},
	} {
		r := compileHostTypes(t, "effect fn program() -> void uses { Console, Foreign } {\n    "+tc.body+"\n}")
		if r.Checked || !hasDiagnosticContaining(r, tc.message) {
			t.Fatalf("expected %q: %+v", tc.message, r.Diagnostics)
		}
	}
}

// effra.bindings.json attaches behavior to a method by its go/types full name,
// the declaring method's identity: a promoted method carries the contract of
// the method it promotes, and a context-forwarding method observes the managed
// fiber's cancellation. The timeout's deadline comes from a Scheduler provider
// whose sleep waits until the native method has entered, so the interruption
// is triggered only after entry, and the method reports that it entered with
// a live context and returned because that context was cancelled.
func TestHostMethodBehaviorContracts(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod": "module example.test/methods\n\ngo 1.27\n",
		"methods.go": `package methods

import (
	"context"
	"time"
)

type Client struct{}

func New() *Client { return &Client{} }

var (
	entered, returned = make(chan struct{}), make(chan struct{})
	entry, exit       string
)

func (c *Client) Lookup(ctx context.Context, id string) (string, error) {
	if id == "slow" {
		entry = "entered-dead"
		if ctx.Err() == nil {
			entry = "entered-live"
		}
		close(entered)
		defer close(returned)
		select {
		case <-ctx.Done():
			exit = "cancelled"
			return "partial", ctx.Err()
		case <-time.After(10 * time.Second):
			exit = "stuck"
			return "stuck", nil
		}
	}
	return "Ada", nil
}

// AwaitEntered is the deadline barrier: it returns once the slow Lookup has
// entered native code.
func AwaitEntered() bool {
	select {
	case <-entered:
		return true
	case <-time.After(10 * time.Second):
		return false
	}
}

// Entry reports how the slow Lookup entered and returned.
func Entry() string {
	select {
	case <-returned:
		return entry + "," + exit
	case <-time.After(10 * time.Second):
		return "not-returned"
	}
}

func (c *Client) Name() string { return "client" }

type Wrapped struct{ *Client }

func Wrap(c *Client) Wrapped { return Wrapped{c} }

type Generic[T any] struct{}

func (Generic[T]) Lookup(ctx context.Context, id string) (string, error) { return id, ctx.Err() }

type Instance struct{ Generic[int64] }

func NewInstance() Instance { return Instance{} }
`,
		"effra.bindings.json": `{"(*example.test/methods.Client).Lookup":{"context":"fiber","cancellation":"cooperative"},
			"(example.test/methods.Generic[T]).Lookup":{"context":"fiber","cancellation":"cooperative"}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := CompileAt(`import go m "example.test/methods"
import Data "effra/data"
impl AfterEntry for Scheduler {
    effect fn sleep(milliseconds: i64) -> void {
        if run m.AwaitEntered().provide<Foreign>(Host) { void } else { void }
    }
    effect fn advance(milliseconds: i64) -> void { void }
    effect fn awaitRegistration() -> void { void }
}
effect fn main() -> string raises {GoError} {
    match run m.New().provide<Foreign>(Host) {
        Data.Option.None => "none",
        Data.Option.Some { value: client } => {
            let timed = run client.Lookup("slow").orFail().timeout(1).catch<Timeout>("done").provide<Foreign>(Host).provide<Scheduler>(AfterEntry)
            let promoted = run (run m.Wrap(client).provide<Foreign>(Host)).Lookup("fast").orFail().provide<Foreign>(Host)
            let generic = run (run m.NewInstance().provide<Foreign>(Host)).Lookup("generic").orFail().provide<Foreign>(Host)
            timed + ":" + run m.Entry().provide<Foreign>(Host) + ":" + promoted + ":" + generic + ":" + run client.Name().provide<Foreign>(Host)
        }
    }
}`, "go", root)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	bindings := map[string]Binding{}
	for _, binding := range r.Bindings {
		bindings[binding.Symbol] = binding
	}
	// An instance of a generic declaration takes the declaration's contract,
	// keeps its instantiated signature and its own receiver identity.
	if generic := bindings["(m.Instance).Lookup"]; generic.Identity != "go:(example.test/methods.Instance).Lookup" || generic.Signature != "func(ctx context.Context, id string) (string, error)" {
		t.Fatalf("generic instance binding: %+v", generic)
	}
	for _, symbol := range []string{"(*m.Client).Lookup", "(m.Wrapped).Lookup", "(m.Instance).Lookup"} {
		b := bindings[symbol]
		if !b.Context || b.Cancellation != "cooperative" || len(b.HostParameters) != 3 || b.HostParameters[1].Adaptation != hostAdaptContext {
			t.Fatalf("%s contract: %+v", symbol, b)
		}
	}
	if name := bindings["(*m.Client).Name"]; name.Context || name.Cancellation != "unknown" {
		t.Fatalf("unclassified method acquired a contract: %+v", name)
	}
	if output := runGeneratedGo(t, r); output != "done:entered-live,cancelled:Ada:generic:client\n" {
		t.Fatalf("method contracts: %q", output)
	}
	if err := os.WriteFile(filepath.Join(root, "effra.bindings.json"), []byte(`{"(*example.test/methods.Client).Name":{"context":"fiber"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	refused := CompileAt(`import go m "example.test/methods"
import Data "effra/data"
effect fn main() -> string {
    match run m.New().provide<Foreign>(Host) {
        Data.Option.None => "none",
        Data.Option.Some { value: client } => run client.Name().provide<Foreign>(Host)
    }
}`, "go", root)
	if refused.Checked || !hasDiagnosticContaining(refused, "the context contract requires a first context.Context parameter") {
		t.Fatalf("context contract on a method without context accepted: %+v", refused.Diagnostics)
	}
	if err := os.WriteFile(filepath.Join(root, "effra.bindings.json"), []byte(`{"(*example.test/methods.Client).Name":{"cancellation":"eventually"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if invalid := CompileAt(`import go m "example.test/methods"
effect fn main() -> void {
    void
}`, "go", root); invalid.Checked || !hasCode(invalid, "EF111") {
		t.Fatalf("invalid method cancellation value accepted: %+v", invalid.Diagnostics)
	}
}

// Go spells a method of every unnamed interface as (interface).M, so such a
// key cannot name one declaration. It is refused when metadata loads and
// applies to no binding, so Lookup keeps its explicit context parameter.
func TestHostAmbiguousInterfaceContractKeyIsRefused(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod": "module example.test/anonymous\n\ngo 1.27\n",
		"anonymous.go": `package anonymous

import "context"

type FirstValue struct{}

func (FirstValue) Lookup(context.Context, string) (string, error) { return "first", nil }
func (FirstValue) FirstOnly()                                     {}

type SecondValue struct{}

func (SecondValue) Lookup(context.Context, string) (int64, error) { return 2, nil }
func (SecondValue) SecondOnly()                                    {}

func First() interface {
	FirstOnly()
	Lookup(context.Context, string) (string, error)
} {
	return FirstValue{}
}

func Second() interface {
	SecondOnly()
	Lookup(context.Context, string) (int64, error)
} {
	return SecondValue{}
}
`,
		"effra.bindings.json": `{"(interface).Lookup":{"context":"fiber","cancellation":"cooperative"}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := CompileAt(`import go a "example.test/anonymous"
import Data "effra/data"
effect fn main() -> void {
    match run a.First().provide<Foreign>(Host) {
        Data.Option.None => void,
        Data.Option.Some { value: first } => {
            let one = run first.Lookup("one").provide<Foreign>(Host)
            void
        }
    }
}`, "go", root)
	want := `effra.bindings.json key "(interface).Lookup" names a method of an unnamed interface, which Go spells this way whatever its package or signature, so it names no single declaration and carries no contract; pass an explicit context.Context argument, or declare a named Go interface with the method in the module and key that method`
	if r.Checked || len(r.Diagnostics) == 0 || r.Diagnostics[0].Code != "EF111" || r.Diagnostics[0].Message != want {
		t.Fatalf("ambiguous key: %+v", r.Diagnostics)
	}
	if !hasDiagnosticContaining(r, "incorrect Go argument count") {
		t.Fatalf("refused key still forwarded context: %+v", r.Diagnostics)
	}
}

// A method of an unnamed interface carries no contract, so the refusal names
// the two paths that remain. Passing a context.Context explicitly checks and
// runs with cancellation unknown. A named interface declared in the module,
// to which Go converts the value, is keyed by its own method and forwards the
// managed context.
func TestHostUnnamedInterfaceMethodRemedies(t *testing.T) {
	root := t.TempDir()
	for name, contents := range map[string]string{
		"go.mod": "module example.test/remedies\n\ngo 1.27\n",
		"remedies.go": `package remedies

import "context"

type value struct{}

func (value) Lookup(ctx context.Context, id string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "v:" + id, nil
}

func First() interface {
	Lookup(context.Context, string) (string, error)
} {
	return value{}
}

type Lookuper interface {
	Lookup(context.Context, string) (string, error)
}

func Named(v interface {
	Lookup(context.Context, string) (string, error)
}) Lookuper {
	return v
}
`,
		"effra.bindings.json": `{"(example.test/remedies.Lookuper).Lookup":{"context":"fiber","cancellation":"cooperative"}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r := CompileAt(`import go r "example.test/remedies"
import go gctx "context"
import Data "effra/data"
effect fn explicit() -> string raises {GoError} {
    let background = run gctx.Background().provide<Foreign>(Host)
    match run r.First().provide<Foreign>(Host) {
        Data.Option.None => "no value",
        Data.Option.Some { value: first } => match background {
            Data.Option.None => "no context",
            Data.Option.Some { value: ctx } => run first.Lookup(ctx, "explicit").orFail().provide<Foreign>(Host)
        }
    }
}
effect fn named() -> string raises {GoError} {
    match run r.First().provide<Foreign>(Host) {
        Data.Option.None => "no value",
        Data.Option.Some { value: first } => match run r.Named(first).provide<Foreign>(Host) {
            Data.Option.None => "no named value",
            Data.Option.Some { value: lookuper } => run lookuper.Lookup("named").orFail().provide<Foreign>(Host)
        }
    }
}
effect fn main() -> string raises {GoError} {
    run explicit() + " " + run named()
}`, "go", root)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "v:explicit v:named\n" {
		t.Fatalf("remedies: %q", output)
	}
	behaviors := map[string]string{}
	for _, binding := range r.Bindings {
		if strings.HasSuffix(binding.Symbol, ".Lookup") {
			behaviors[binding.Identity] = fmt.Sprintf("context=%v cancellation=%s", binding.Context, binding.Cancellation)
		}
	}
	want := map[string]string{
		"go:(interface{Lookup(context.Context, string) (string, error)}).Lookup": "context=false cancellation=unknown",
		"go:(example.test/remedies.Lookuper).Lookup":                             "context=true cancellation=cooperative",
	}
	if !maps.Equal(behaviors, want) {
		t.Fatalf("remedy bindings: %v", behaviors)
	}
}

// error.Error belongs to Go's universe, not a package. Calling it on a native
// error runs the original value's method: a typed-nil *Problem inside a
// non-nil error answers through its nil-tolerant receiver. The binding names
// no package and adds no import.
func TestHostUniverseErrorMethod(t *testing.T) {
	r := compileHostTypes(t, `effect fn show(err: error) -> string uses { Foreign } {
    run err.Error()
}
effect fn program() -> void uses { Console, Foreign } {
    let typed = run host.Typed()
    match typed.error {
        Data.Option.None => void,
        Data.Option.Some { value } => run Console.log(run show(value))
    }
    let wrapped = run host.Missing()
    match wrapped.error {
        Data.Option.None => void,
        Data.Option.Some { value } => run Console.log(run show(value))
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "nil problem\nwrapped: missing\n" {
		t.Fatalf("error.Error: %q", output)
	}
	var method Binding
	for _, binding := range r.Bindings {
		if binding.Symbol == "(error).Error" {
			method = binding
		}
	}
	if method.Package != "" || method.Identity != "go:(error).Error" || method.Return != "string" || len(method.HostParameters) != 1 || method.HostParameters[0].Adaptation != hostAdaptReceiver {
		t.Fatalf("universe method binding: %+v", method)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	requirePlanned(t, plan, RequiresForeign, "go:(error).Error")
}

// An unnamed interface is spelled through its method signatures and embedded
// types, so generated code imports every package they name although source
// never imports io and never calls the methods. The plan retains the same
// packages.
func TestHostUnnamedInterfaceClosesOverSignaturePackages(t *testing.T) {
	r := compileHostTypes(t, `effect fn program() -> void uses { Console, Foreign } {
    match run host.ProbeAnonymous() {
        Data.Option.None => void,
        Data.Option.Some { value } => run Console.log(run host.DynamicType(value))
    }
    match run host.ProbeEmbedded() {
        Data.Option.None => void,
        Data.Option.Some { value } => run Console.log(run host.DynamicType(value))
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	requireProvenance(t, plan, RequiresGoImport, "io", "go:interface{WriteTo(io.Writer) (int64, error)}", "host-type")
	requirePlanned(t, plan, RequiresHostType, "go:interface{Report() string; io.Reader}")
	if output := runGeneratedGo(t, r); output != "*hosttypes.Source\n*hosttypes.Source\n" {
		t.Fatalf("unnamed interfaces: %q", output)
	}
}

// Method bindings are keyed by the receiver's canonical type and the method,
// not by display: under the alias bytes, the fixture's *Buffer and the
// standard *bytes.Buffer both display as *bytes.Buffer, yet each String call
// keeps its own signature and inspection reports both receivers.
func TestHostMethodBindingsUseCanonicalReceiverIdentity(t *testing.T) {
	const imports = `import go bytes "effra.local/prototype/examples/hosttypes"
import go strconv "strconv"
import Data "effra/data"
`
	r := CompileAt(imports+`effect fn program() -> void uses { Console, Foreign } {
    match run bytes.NewProbeShadowBuffer() {
        Data.Option.None => void,
        Data.Option.Some { value: left } => run Console.log(run strconv.FormatInt(run left.String(), 10))
    }
    match run bytes.NewBuffer("x") {
        Data.Option.None => void,
        Data.Option.Some { value: right } => run Console.log(run right.String())
    }
}`+hostTypesEntry, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	identities := map[string]Binding{}
	for _, binding := range r.Bindings {
		identities[binding.Identity] = binding
	}
	shadow, standard := identities["go:(*effra.local/prototype/examples/hosttypes.Buffer).String"], identities["go:(*bytes.Buffer).String"]
	if shadow.Symbol != "(*bytes.Buffer).String" || standard.Symbol != "(*bytes.Buffer).String" || shadow.Return != "i64" || standard.Return != "string" || shadow.HostParameters[0].Native != "*effra.local/prototype/examples/hosttypes.Buffer" || standard.HostParameters[0].Native != "*bytes.Buffer" {
		t.Fatalf("colliding displays shared a binding: %+v", r.Bindings)
	}
	if output := runGeneratedGo(t, r); output != "7\nx\n" {
		t.Fatalf("colliding receivers: %q", output)
	}
	wrong := CompileAt(imports+`effect fn program() -> void uses { Console, Foreign } {
    match run bytes.NewProbeShadowBuffer() {
        Data.Option.None => void,
        Data.Option.Some { value: left } => run Console.log(run strconv.FormatInt(run left.String(), 10))
    }
    match run bytes.NewBuffer("x") {
        Data.Option.None => void,
        Data.Option.Some { value: right } => run Console.log(run strconv.FormatInt(run right.String(), 10))
    }
}`+hostTypesEntry, "go", "../..")
	if wrong.Checked || !hasDiagnosticContaining(wrong, "Go argument must be i64") {
		t.Fatalf("stdlib String accepted as i64: %+v", wrong.Diagnostics)
	}
}
