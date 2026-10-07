package compiler

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const targetFixture = `import Fns "effra/functions"
import Data "effra/data"
error Missing { id: string }
record Box { value: string }
record Tag { value: string }
enum Shape { Circle { radius: i64 }, Square }
record Pair<A: type> { left: A, right: A }
service Users { effect fn get(id: string) -> string raises {Missing} }
impl Fixed for Users { effect fn get(id: string) -> string raises {Missing} { id } }
impl Prefix(prefix: string) for Users { effect fn get(id: string) -> string raises {Missing} { prefix + id } }
// helper is a comment, not a reference
fn helper() -> string { "helper" }
fn shadow(helper: string) -> string { helper }
fn boxed(value: string) -> Box { Box { value: value } }
fn tagged(value: string) -> string { let tag = Tag { value }; tag.value }
fn area(shape: Shape) -> i64 { match shape { Shape.Circle { radius: r } => r, Shape.Square => 0 } }
fn pick(o: Data.Option<string>) -> string { match o { Data.Option.Some { value: v } => v, Data.Option.None => "none" } }
fn wrap(text: string) -> Data.Option<string> { Data.Option.Some { value: text } }
fn pair(text: string) -> string { let p = Pair { left: text, right: text }; p.left }
fn same(input: string) -> string { Fns.identity(input) }
effect fn load(id: string) -> string raises {Missing} uses {Users} {
    if id == "" { fail Missing { id: id } } else { run Users.get(id) }
}
effect fn fixed(id: string) -> string raises {Missing} { run load(id).provide<Users>(Fixed) }
effect fn prefixed(id: string) -> string raises {Missing} {
    let users = run Prefix("p")
    run load(id).provide<Users>(users)
}
effect fn safe(id: string) -> string { run fixed(id).catch<Missing>("none") }
`

// at locates name inside the first occurrence of context.
func at(t *testing.T, source, context, name string) int {
	t.Helper()
	start := strings.Index(source, context)
	within := strings.Index(context, name)
	if start < 0 || within < 0 {
		t.Fatalf("fixture lacks %q in %q", name, context)
	}
	return start + within
}

func TestSelectedNamesPublishCheckerResolvedDeclarationTargets(t *testing.T) {
	type expectation struct {
		context, name   string
		kind            string
		targetKind      string
		owner           string
		declaration     string // context of the declaration name; "" if not in this source
		declarationName string
		presentation    string
	}
	expectations := []expectation{
		{"import Fns", "Fns", "declaration", "module", "", "import Fns", "Fns", `import Fns "effra/functions"`},
		{"Fns.identity", "Fns", "reference", "module", "", "import Fns", "Fns", `import Fns "effra/functions"`},
		{"Fns.identity", "identity", "expression", "function", "", "", "", "fn identity(input: string) -> string"},
		{"error Missing", "Missing", "declaration", "error", "", "error Missing", "Missing", "error Missing { id: string }"},
		{"fail Missing", "Missing", "reference", "error", "", "error Missing", "Missing", "error Missing { id: string }"},
		{"Missing { id: id", "id", "reference", "field", "Missing", "Missing { id: string", "id", "field Missing.id: string"},
		{"catch<Missing>", "Missing", "reference", "error", "", "error Missing", "Missing", "error Missing { id: string }"},
		{"Box { value: value }", "Box", "expression", "record", "", "record Box", "Box", "record Box { value: string }"},
		{"Box { value: value }", "value", "reference", "field", "Box", "Box { value: string", "value", "field Box.value: string"},
		{"tag.value", "value", "expression", "field", "Tag", "Tag { value: string", "value", "field Tag.value: string"},
		{"Shape.Circle {", "Shape", "reference", "enum", "", "enum Shape", "Shape", "enum Shape { Circle { radius: i64 }, Square }"},
		{"Shape.Circle {", "Circle", "reference", "variant", "Shape", "Circle { radius: i64 }", "Circle", "variant Shape.Circle { radius: i64 }"},
		{"radius: r", "radius", "reference", "field", "Shape.Circle", "radius: i64", "radius", "field Shape.Circle.radius: i64"},
		{"Shape.Square =>", "Square", "reference", "variant", "Shape", "Square }", "Square", "variant Shape.Square"},
		{"Data.Option.Some { value: v }", "Data", "reference", "module", "", "import Data", "Data", `import Data "effra/data"`},
		{"Data.Option.Some { value: v }", "Option", "reference", "enum", "", "", "", "enum Option<T: type> { None, Some { value: T } }"},
		{"Data.Option.Some { value: v }", "Some", "reference", "variant", "Option", "", "", "variant Option.Some { value: T }"},
		{"Data.Option.Some { value: text }", "Some", "expression", "variant", "Option", "", "", "variant Option.Some { value: T }"},
		{"Pair { left: text", "left", "reference", "field", "Pair", "left: A", "left", "field Pair.left: A"},
		{"p.left", "left", "expression", "field", "Pair", "left: A", "left", "field Pair.left: string"},
		{"service Users", "Users", "declaration", "service", "", "service Users", "Users", "service Users"},
		{"impl Fixed for Users", "Users", "reference", "service", "", "service Users", "Users", "service Users"},
		{"run Users.get", "Users", "reference", "service", "", "service Users", "Users", "service Users"},
		{"run Users.get", "get", "expression", "operation", "Users", "effect fn get", "get", "effect fn Users.get(id: string) -> string raises {Missing}"},
		{"provide<Users>(Fixed)", "Users", "reference", "service", "", "service Users", "Users", "service Users"},
		{"provide<Users>(Fixed)", "Fixed", "expression", "provider", "Users", "impl Fixed", "Fixed", "impl Fixed for Users"},
		{`run Prefix("p")`, "Prefix", "expression", "provider", "Users", "impl Prefix", "Prefix", "impl Prefix for Users"},
		{"Fixed for Users { effect fn get", "get", "declaration", "method", "Fixed", "Fixed for Users { effect fn get", "get", "effect fn Fixed.get(id: string) -> string raises {Missing}"},
		{"run load(id).provide<Users>(Fixed)", "load", "expression", "function", "", "effect fn load", "load", "effect fn load(id: string) -> string raises {Missing} uses {Users}"},
		{"fn shadow(helper", "helper", "bindingDeclaration", "parameter", "", "fn shadow(helper", "helper", "parameter helper: string"},
		{"{ helper }", "helper", "bindingUse", "parameter", "", "fn shadow(helper", "helper", "parameter helper: string"},
		{"Tag { value }", "value", "bindingUse", "parameter", "", "fn tagged(value", "value", "parameter value: string"},
		{"let p =", "p", "bindingDeclaration", "let", "", "let p =", "p", "let p: Pair<string>"},
		{": v }", "v", "bindingDeclaration", "pattern", "", ": v }", "v", "pattern v: string"},
		{"=> v,", "v", "bindingUse", "pattern", "", ": v }", "v", "pattern v: string"},
	}
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(targetFixture, target)
			if !r.Checked {
				t.Fatalf("fixture: %+v", r.Diagnostics)
			}
			for _, want := range expectations {
				offset := at(t, targetFixture, want.context, want.name)
				// Every byte of the selected token selects the same target.
				for _, probe := range []int{offset, offset + len(want.name) - 1} {
					response, err := r.SelectType(TypeSelection{Offset: &probe})
					if err != nil {
						t.Fatalf("%s %q: %v", want.context, want.name, err)
					}
					selected := response["selection"].(SelectedType)
					label := fmt.Sprintf("%q in %q at %d", want.name, want.context, probe)
					if selected.Kind != want.kind || selected.Target == nil || selected.Target.Kind != want.targetKind || selected.Target.Name != want.name || selected.Target.Owner != want.owner {
						t.Fatalf("%s: selection %+v target %+v", label, selected, selected.Target)
					}
					if selected.Span.Offset != offset || selected.Span.Length != len(want.name) || !selected.LocationAvailable {
						t.Fatalf("%s: selected token span %+v", label, selected.Span)
					}
					if want.declaration == "" {
						if selected.Target.LocationAvailable || selected.Target.Source == userSourceID {
							t.Fatalf("%s: bundled target claimed a location in this snapshot: %+v", label, selected.Target)
						}
					} else if declared := at(t, targetFixture, want.declaration, want.declarationName); !selected.Target.LocationAvailable || selected.Target.Source != userSourceID || selected.Target.Span.Offset != declared || selected.Target.Span.Length != len(want.declarationName) {
						t.Fatalf("%s: target %+v, want declaration at %d", label, selected.Target, declared)
					}
					if selected.Presentation != want.presentation {
						t.Fatalf("%s: presentation %q, want %q", label, selected.Presentation, want.presentation)
					}
				}
			}
		})
	}
}

// Every recorded name token spells its target's declared name. A name search
// would also satisfy this; the shadowing, owner and bundled controls above
// show the targets come from the checker's own resolutions instead.
func TestRecordedNamesAreOriginalTokensOfTheirTargets(t *testing.T) {
	r := Compile(targetFixture)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	names := append([]lexicalName{}, r.lexical.declarations...)
	for _, reference := range r.lexical.references {
		names = append(names, reference)
	}
	if len(r.lexical.references) < 30 {
		t.Fatalf("checker recorded too few references: %d", len(r.lexical.references))
	}
	for _, name := range names {
		target, _ := r.declarationTarget(name.Target)
		if target == nil {
			t.Fatalf("unresolvable recorded target at %+v", name.Span)
		}
		if token := targetFixture[name.Span.Offset : name.Span.Offset+name.Span.Length]; token != target.Name {
			t.Fatalf("token %q at %d names %+v", token, name.Span.Offset, target)
		}
	}
}

func TestSelectedNamesWithoutDeclarationsHaveNoTarget(t *testing.T) {
	r := Compile(targetFixture)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	for _, probe := range []struct{ context, name string }{
		{"// helper is", "helper"}, // comment
		{"// helper is", " "},      // whitespace
		{"fn helper()", "fn"},      // declaration keyword
		{"-> string { \"helper\" }", "string"},
		{`"helper" }`, "helper"}, // string literal
		{"if id ==", "if"},       // expression keyword
		{"if id ==", "=="},       // operator
		{"run fixed(id).catch", "catch"},
	} {
		offset := at(t, targetFixture, probe.context, probe.name)
		response, err := r.SelectType(TypeSelection{Offset: &offset})
		if err != nil {
			if !errors.Is(err, ErrNoSelection) {
				t.Fatalf("%q in %q: %v", probe.name, probe.context, err)
			}
			continue
		}
		if selected := response["selection"].(SelectedType); selected.Target != nil {
			t.Fatalf("%q in %q selected a declaration: %+v", probe.name, probe.context, selected.Target)
		}
	}
	unchecked := Compile(`fn broken() -> string { missing }`)
	offset := 0
	if _, err := unchecked.SelectType(TypeSelection{Offset: &offset}); !errors.Is(err, ErrUncheckedSource) {
		t.Fatalf("unchecked source: %v", err)
	}
}

func TestPresentationBoundsSharedTypeGraphs(t *testing.T) {
	types := []TypeNode{{ID: "t:0", Kind: "primitive", Name: "string"}}
	for i := 1; i <= 64; i++ {
		previous := fmt.Sprintf("t:%d", i-1)
		types = append(types, TypeNode{ID: fmt.Sprintf("t:%d", i), Kind: "application", Name: "Pair", Args: []string{previous, previous}})
	}
	projection := TypeProjection{Types: types, Complete: true}
	if got := presentSelection(&SelectedType{Definition: "t:2"}, projection); got != "Pair<Pair<string, string>, Pair<string, string>>" {
		t.Fatalf("small shared graph: %q", got)
	}
	// 2^64 leaves as text: the walk must stop at the byte budget, not expand.
	got := presentSelection(&SelectedType{Definition: "t:64"}, projection)
	if len(got) > maxPresentationBytes || !strings.HasSuffix(got, presentationElision) || !strings.HasPrefix(got, "Pair<Pair<") {
		t.Fatalf("unbounded presentation: %d bytes", len(got))
	}
}
