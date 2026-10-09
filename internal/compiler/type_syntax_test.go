package compiler

import (
	"errors"
	"fmt"
	"testing"
)

const annotationFixture = `import Data "effra/data"
error Missing { id: string }
error E
record Box { value: string }
record Wrap<Box: type> { inner: Box, label: string }
enum Shape { Circle { size: Box }, Square(side: i64) }
service Users { effect fn get(id: string) -> Box raises {Missing} }
service Runner { effect fn run<R: uses>(task: effect fn() -> string uses {R}) -> string uses {R} }
service Labels { effect fn label(box: Box) -> string }
impl Cached(prefix: string) for Labels uses {Users} { effect fn label(box: Box) -> string { prefix + box.value } }
fn wrap(box: Box) -> Wrap<Box> { Wrap { inner: box, label: "w" } }
effect fn apply<E: raises>(box: Data.Option<Box>, cb: effect fn(Box) -> string raises {E}) -> string raises {E, Missing} { "x" }
effect fn fixed() -> string raises {E} { fail E }
`

// Annotation and row-label tokens take their declarations from the checked
// type and row the checker retained, so same-spelled template and row
// parameters shadow the module declarations exactly as checking does.
func TestTypeAnnotationsAndRowLabelsSelectCheckedDeclarations(t *testing.T) {
	type expectation struct {
		context, name, kind, targetKind, owner string
		declaration, declarationName           string // "" when the target is not in this source
		presentation                           string
	}
	expectations := []expectation{
		{"fn wrap(box: Box)", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		{"-> Wrap<Box>", "Wrap", "reference", "record", "", "record Wrap", "Wrap", "record Wrap<Box: type> { inner: Box, label: string }"},
		{"-> Wrap<Box>", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		{"size: Box", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		{"-> Box raises", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		{"Box raises {Missing}", "Missing", "reference", "error", "", "error Missing", "Missing", "error Missing { id: string }"},
		{"label(box: Box) -> string }", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		// A template parameter shadows the record of the same name.
		{"Wrap<Box: type>", "Box", "declaration", "typeParameter", "Wrap", "Wrap<Box", "Box", "type parameter Wrap.Box: type"},
		{"inner: Box", "Box", "reference", "typeParameter", "Wrap", "Wrap<Box", "Box", "type parameter Wrap.Box: type"},
		// Qualified bundled applications keep their alias and nested arguments.
		{"box: Data.Option<Box>", "Data", "reference", "module", "", "import Data", "Data", `import Data "effra/data"`},
		{"box: Data.Option<Box>", "Option", "reference", "enum", "", "", "", "enum Option<T: type> { None, Some { value: T } }"},
		{"box: Data.Option<Box>", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		// Callable annotations bind parameters, results and row labels.
		{"cb: effect fn(Box)", "Box", "reference", "record", "", "record Box", "Box", "record Box { value: string }"},
		{"apply<E: raises>", "E", "declaration", "rowParameter", "apply", "apply<E", "E", "row parameter apply.E: raises"},
		{"raises {E}) ->", "E", "reference", "rowParameter", "apply", "apply<E", "E", "row parameter apply.E: raises"},
		// A row parameter shadows the error of the same name.
		{"raises {E, Missing}", "E", "reference", "rowParameter", "apply", "apply<E", "E", "row parameter apply.E: raises"},
		{"raises {E, Missing}", "Missing", "reference", "error", "", "error Missing", "Missing", "error Missing { id: string }"},
		{"fixed() -> string raises {E}", "E", "reference", "error", "", "error E", "E", "error E"},
		{"run<R: uses>", "R", "declaration", "rowParameter", "Runner.run", "run<R", "R", "row parameter Runner.run.R: uses"},
		{"-> string uses {R}) ->", "R", "reference", "rowParameter", "Runner.run", "run<R", "R", "row parameter Runner.run.R: uses"},
		{"-> string uses {R} }", "R", "reference", "rowParameter", "Runner.run", "run<R", "R", "row parameter Runner.run.R: uses"},
		// A provider's captured construction services.
		{"for Labels uses {Users}", "Users", "reference", "service", "", "service Users", "Users", "service Users"},
	}
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(annotationFixture, target)
			if !r.Checked {
				t.Fatalf("fixture: %+v", r.Diagnostics)
			}
			for _, want := range expectations {
				offset := at(t, annotationFixture, want.context, want.name)
				for _, probe := range []int{offset, offset + len(want.name) - 1} {
					query, err := r.QueryType(TypeSelection{Offset: &probe})
					label := fmt.Sprintf("%q in %q at %d", want.name, want.context, probe)
					if err != nil {
						t.Fatalf("%s: %v", label, err)
					}
					selected := query.Selection
					if selected.Kind != want.kind || selected.Target == nil || selected.Target.Kind != want.targetKind || selected.Target.Name != want.name || selected.Target.Owner != want.owner {
						t.Fatalf("%s: selection %+v target %+v", label, selected, selected.Target)
					}
					if selected.Span.Offset != offset || selected.Span.Length != len(want.name) || !selected.LocationAvailable {
						t.Fatalf("%s: selected token span %+v", label, selected.Span)
					}
					if want.declaration == "" {
						if selected.Target.LocationAvailable || selected.Target.Source == userSourceID {
							t.Fatalf("%s: bundled target claimed a location: %+v", label, selected.Target)
						}
					} else if declared := at(t, annotationFixture, want.declaration, want.declarationName); !selected.Target.LocationAvailable || selected.Target.Source != userSourceID || selected.Target.Span.Offset != declared || selected.Target.Span.Length != len(want.declarationName) {
						t.Fatalf("%s: target %+v, want declaration at %d", label, selected.Target, declared)
					}
					if selected.Presentation != want.presentation {
						t.Fatalf("%s: presentation %q, want %q", label, selected.Presentation, want.presentation)
					}
				}
			}
			// Primitives, keywords and punctuation inside annotations name no
			// declaration and remain unselectable.
			for _, probe := range []struct{ context, name string }{
				{"Square(side: i64)", "i64"},
				{"get(id: string)", "string"},
				{"cb: effect fn(Box)", "effect"},
				{"cb: effect fn(Box)", "fn"},
				{"string raises {E}) ->", "raises"},
				{"string raises {E}) ->", "{"},
			} {
				offset := at(t, annotationFixture, probe.context, probe.name)
				if _, err := r.QueryType(TypeSelection{Offset: &offset}); !errors.Is(err, ErrNoSelection) {
					t.Fatalf("%q in %q: %v", probe.name, probe.context, err)
				}
			}
		})
	}
}

// A template or row parameter's identity is the checker's own, so a type
// definition query expands a template parameter's variable node.
func TestParameterTargetsPublishCheckedIdentities(t *testing.T) {
	r := Compile(annotationFixture)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	offset := at(t, annotationFixture, "inner: Box", "Box")
	query, err := r.QueryType(TypeSelection{Offset: &offset})
	if err != nil {
		t.Fatal(err)
	}
	target := query.Selection.Target
	if target.Identity != "type-parameter:"+r.Program.Records[1].Identity+":type:Box" || target.ParameterKind != "type" || target.Type == nil {
		t.Fatalf("template parameter target %+v", target)
	}
	definition, err := r.QueryType(TypeSelection{Definition: target.Type.ID, ExpectedRevision: r.Revision})
	if err != nil || definition.Selection.Presentation != "Box" {
		t.Fatalf("template parameter definition %q: %v", definition.Selection.Presentation, err)
	}
	offset = at(t, annotationFixture, "raises {E}) ->", "E")
	query, err = r.QueryType(TypeSelection{Offset: &offset})
	if err != nil {
		t.Fatal(err)
	}
	if target := query.Selection.Target; target.Identity != "row-parameter:"+r.checkedSymbols[r.Find("apply").Identity].declaration.Identity+":raises:E" || target.ParameterKind != "raises" {
		t.Fatalf("row parameter target %+v", target)
	}
}

// A qualified Go host type selects its import alias; the host member itself
// is not a declaration this snapshot can locate.
func TestHostTypeAnnotationSelectsItsImportAlias(t *testing.T) {
	source := hostTypesImport + "fn keep(point: host.Point) -> host.Point { point }\n" + `effect fn main() -> void {
    void
}`
	r := CompileAt(source, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	offset := at(t, source, "point: host.Point", "host")
	query, err := r.QueryType(TypeSelection{Offset: &offset})
	if err != nil {
		t.Fatal(err)
	}
	if target := query.Selection.Target; query.Selection.Kind != "reference" || target == nil || target.Kind != "hostModule" || target.Name != "host" || !target.LocationAvailable || target.Span.Offset != at(t, source, "import go host", "host") {
		t.Fatalf("host alias selection %+v target %+v", query.Selection, target)
	}
	offset = at(t, source, "point: host.Point", "Point")
	if _, err := r.QueryType(TypeSelection{Offset: &offset}); !errors.Is(err, ErrNoSelection) {
		t.Fatalf("host member: %v", err)
	}
}

// Annotation references are original syntax facts: the type spelling's
// interning shares one sourceType between occurrences, but every occurrence
// keeps its own tokens, so repeated spellings each select their own span.
func TestRepeatedAnnotationSpellingsKeepTheirOwnTokens(t *testing.T) {
	source := `record Box { value: string }
fn first(a: Box, b: Box) -> Box { a }
fn second(c: Box) -> Box { c }
`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	declared := at(t, source, "record Box", "Box")
	seen := 0
	for offset := 0; offset < len(source); offset++ {
		if source[offset] != 'B' || offset == declared {
			continue
		}
		query, err := r.QueryType(TypeSelection{Offset: &offset})
		if err != nil {
			t.Fatalf("Box at %d: %v", offset, err)
		}
		if query.Selection.Span.Offset != offset || query.Selection.Target == nil || query.Selection.Target.Span.Offset != declared {
			t.Fatalf("Box at %d: %+v", offset, query.Selection)
		}
		seen++
	}
	if seen != 5 {
		t.Fatalf("selected %d annotation occurrences", seen)
	}
}
