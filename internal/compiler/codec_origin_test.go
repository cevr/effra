package compiler

import (
	"strings"
	"testing"
)

const codecOriginInvoiceSource = `import Json "effra/json"

error Rejected
service Policy {
    effect fn suffix() -> string raises { Rejected }
}
impl Allow for Policy {
    effect fn suffix() -> string raises { Rejected } { "!" }
}
record Invoice { id: i64, memo: string }
record ArchivedInvoice { id: i64, memo: string }
derive Object = Json.codec<Invoice>(maxDepth: 1, maxBodyBytes: 128)
derive mirror = Json.codec<Invoice>(maxBodyBytes: 128, maxDepth: 1)
derive tiny = Json.codec<Invoice>(maxBodyBytes: 16, maxDepth: 1)
derive archive = Json.codec<ArchivedInvoice>(maxBodyBytes: 128, maxDepth: 1)

effect fn decorate(value: Invoice) -> Invoice raises { Rejected } uses { Policy } {
    let suffix = run Policy.suffix()
    Invoice { id: value.id, memo: value.memo + suffix }
}
effect fn process(body: string) -> string raises { JsonDecodeFailure, Rejected, JsonEncodeFailure } uses { Policy } {
    let input = run body |> Object.decode()
    let decorated = run input |> decorate()
    run decorated |> mirror.encode()
}
effect fn named(body: string) -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let value = run Object.decode(input: body)
    run mirror.encode(value: value)
}
effect fn main() -> string raises { JsonDecodeFailure, Rejected, JsonEncodeFailure } {
    run process(body: "{\"id\":\"0007\",\"memo\":\"paid\",\"extra\":true}").provide<Policy>(Allow)
}
`

const codecOriginSettingsSource = `import Json "effra/json"
record Window { title: string, visible: bool }
enum Mode { Idle, Ready { window: Window, marker: void } }
record State { mode: Mode, revision: i64 }
record Unrelated { id: string }
derive Effect = Json.codec<State>(maxDepth: 3, maxBodyBytes: 160)
derive spare = Json.codec<Unrelated>(maxDepth: 1, maxBodyBytes: 64)
fn closed() -> Unrelated { Unrelated { id: "unused" } }
effect fn main() -> string raises { JsonEncodeFailure } {
    let state = State { mode: Mode.Ready { window: Window { title: "restored", visible: true }, marker: void }, revision: 9 }
    run state |> Effect.encode()
}
`

func codecOriginSpan(t *testing.T, source, name string) (Span, Span) {
	t.Helper()
	start := strings.Index(source, "derive "+name+" =")
	if start < 0 {
		t.Fatalf("derive %s not found", name)
	}
	nameOffset := start + len("derive ")
	lineEnd := strings.IndexByte(source[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(source) - start
	}
	position := func(offset int) (int, int) {
		line := 1
		column := 1
		for _, ch := range source[:offset] {
			if ch == '\n' {
				line++
				column = 1
			} else {
				column++
			}
		}
		return line, column
	}
	nameLine, nameColumn := position(nameOffset)
	extentLine, extentColumn := position(start)
	return Span{Offset: nameOffset, Length: len(name), Line: nameLine, Column: nameColumn}, Span{Offset: start, Length: lineEnd, Line: extentLine, Column: extentColumn}
}

func assertCodecDirectionOrigin(t *testing.T, r *Result, source, witness, direction string) {
	t.Helper()
	nameSpan, extent := codecOriginSpan(t, source, witness)
	member := witness + "." + direction
	memberOffset := strings.Index(source, member)
	if memberOffset < 0 {
		t.Fatalf("member %s not found", member)
	}
	memberOffset += len(witness) + 1
	query, err := r.QueryType(TypeSelection{Offset: &memberOffset})
	if err != nil {
		t.Fatalf("selected %s: %v", member, err)
	}
	target := query.Selection.Target
	if target == nil || target.Kind != "function" || target.Name != member || target.Identity != "function:module:file:module:"+member {
		t.Fatalf("direction target lost ordinary function identity: %+v", target)
	}
	if !target.LocationAvailable || target.Source != userSourceID || target.Span != nameSpan || target.Extent != extent {
		t.Fatalf("direction origin=%+v want name=%+v extent=%+v", target, nameSpan, extent)
	}
	if target.Callable == nil || len(target.Callable.Parameters) != 1 || target.Callable.Parameters[0].Span != (Span{}) || target.Callable.Parameters[0].Extent != (Span{}) {
		t.Fatalf("synthetic parameter acquired a source location: %+v", target.Callable)
	}
	if query.Selection.Span.Offset != memberOffset || query.Selection.Span.Length != len(direction) {
		t.Fatalf("selected direction span=%+v", query.Selection.Span)
	}

	deriveOffset := nameSpan.Offset
	deriveQuery, err := r.QueryType(TypeSelection{Offset: &deriveOffset})
	if err != nil {
		t.Fatalf("selected derive %s: %v", witness, err)
	}
	deriveTarget := deriveQuery.Selection.Target
	if deriveTarget == nil || deriveTarget.Kind != "codec" || deriveTarget.Name != witness || !deriveTarget.LocationAvailable || deriveTarget.Span != nameSpan || deriveTarget.Extent != extent {
		t.Fatalf("derive declaration target=%+v want name=%+v extent=%+v", deriveTarget, nameSpan, extent)
	}
	if deriveQuery.Selection.Presentation != "derive "+witness {
		t.Fatalf("derive presentation=%q", deriveQuery.Selection.Presentation)
	}
	directionFunction := r.Program.DerivedBindings[witness][direction]
	if directionFunction == nil || directionFunction.Body == nil || len(directionFunction.Body.Statements) != 1 {
		t.Fatalf("derived direction lost its generated body shape: %+v", directionFunction)
	}
	statement := directionFunction.Body.Statements[0]
	if statement.Span != (Span{}) || statement.Extent != (Span{}) || statement.Value == nil || statement.Value.Span != (Span{}) || statement.Value.Extent != (Span{}) || statement.Value.Left == nil || statement.Value.Left.Span != (Span{}) || statement.Value.Left.Extent != (Span{}) {
		t.Fatalf("synthetic codec body acquired a source location: %+v", statement)
	}
	if _, syntheticIndexed := r.lexical.functions[directionFunction]; syntheticIndexed {
		t.Fatal("synthetic codec direction was inserted into original function facts")
	}
	if _, original := r.lexical.items[r.Program.Codecs[0]]; !original {
		t.Fatal("original codec declaration was not retained in lexical items")
	}
}

func TestDerivedCodecDirectionsRetainOriginalDeclarationOrigins(t *testing.T) {
	cases := []struct {
		name    string
		source  string
		witness string
		dir     string
	}{
		{name: "invoice decode", source: codecOriginInvoiceSource, witness: "Object", dir: "decode"},
		{name: "settings encode", source: codecOriginSettingsSource, witness: "Effect", dir: "encode"},
	}
	for _, tc := range cases {
		for _, target := range []string{"go", "js"} {
			t.Run(tc.name+"/"+target, func(t *testing.T) {
				r := CompileFor(tc.source, target)
				if !r.Checked {
					t.Fatalf("source was rejected: %+v", r.Diagnostics)
				}
				assertCodecDirectionOrigin(t, r, tc.source, tc.witness, tc.dir)
			})
		}
	}
}

func TestCodecOriginRepairKeepsOrdinaryFunctionNavigation(t *testing.T) {
	r := CompileFor(codecOriginInvoiceSource, "go")
	if !r.Checked {
		t.Fatalf("invoice source was rejected: %+v", r.Diagnostics)
	}
	use := strings.LastIndex(codecOriginInvoiceSource, "process(body:")
	query, err := r.QueryType(TypeSelection{Offset: &use})
	if err != nil {
		t.Fatal(err)
	}
	target := query.Selection.Target
	if target == nil || target.Kind != "function" || target.Name != "process" || !target.LocationAvailable {
		t.Fatalf("ordinary function origin changed: %+v", target)
	}
	declaration := strings.Index(codecOriginInvoiceSource, "effect fn process") + len("effect fn ")
	if target.Span.Offset != declaration || target.Span.Length != len("process") {
		t.Fatalf("ordinary function span=%+v want offset=%d", target.Span, declaration)
	}
}
