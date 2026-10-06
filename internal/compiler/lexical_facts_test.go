package compiler

import (
	"reflect"
	"strings"
	"testing"
)

const lexicalFixture = `enum Notice { Named { value: string, suffix: string } }
fn local(input: string) -> string {
 let label = input
 label
}
fn describe(label: string, notice: Notice) -> string {
 let rendered = match notice {
  Notice.Named { suffix: tail, value: label } => label + tail
 }
 rendered + label
}
service Labels { effect fn read(item: string) -> string }
impl Prefix(prefix: string) for Labels {
 effect fn read(item: string) -> string {
  let result = prefix + item
  result
 }
}
`

func TestLexicalFactsResolveActualEnvironmentEntries(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		t.Run(target, func(t *testing.T) {
			r := CompileFor(lexicalFixture, target)
			if !r.Checked {
				t.Fatalf("fixture: %+v", r.Diagnostics)
			}
			if !r.lexical.complete {
				t.Fatal("facts refused")
			}
			use := func(needle, name string) lexicalBinding {
				t.Helper()
				offset := strings.Index(lexicalFixture, needle) + strings.Index(needle, name)
				for e, id := range r.lexical.uses {
					if e.Span.Offset == offset {
						return r.lexical.bindings[id]
					}
				}
				t.Fatalf("no resolved use at %q %d", needle, offset)
				return lexicalBinding{}
			}
			assert := func(got lexicalBinding, kind, declaration string, name string) {
				t.Helper()
				offset := strings.Index(lexicalFixture, declaration) + strings.Index(declaration, name)
				if got.Kind != kind || got.Name != name || got.NameSpan.Offset != offset || got.NameSpan.Length != len(name) {
					t.Fatalf("binding=%+v want %s at %d", got, kind, offset)
				}
				if got.Checked.node() == nil || r.projector.displayChecked(got.Checked) != "string" {
					t.Fatalf("lost canonical checked type: %+v", got)
				}
			}
			assert(use("= input", "input"), "parameter", "local(input:", "input")
			assert(use("\n label\n", "label"), "let", "let label =", "label")
			assert(use("=> label +", "label"), "pattern", "value: label", "label")
			assert(use("rendered + label", "label"), "parameter", "describe(label:", "label")
			assert(use("= prefix +", "prefix"), "configuration", "Prefix(prefix:", "prefix")
			assert(use("prefix + item", "item"), "parameter", "\n effect fn read(item:", "item")
			// The service operation and implementation method use distinct
			// parameter declarations even when their spellings agree.
			methodParam := use("prefix + item", "item")
			if methodParam.NameSpan.Offset == strings.Index(lexicalFixture, "read(item:")+len("read(") {
				t.Fatal("method resolved to service signature")
			}
			if use("=> label +", "label").ID == use("rendered + label", "label").ID {
				t.Fatal("pattern shadowing erased actual declaration identity")
			}
			if use("\n label\n", "label").ID == use("= input", "input").ID {
				t.Fatal("alias initializer identity leaked through let")
			}
		})
	}
}

func TestOriginalSyntaxFactsSurviveCheckingAndRetainExtents(t *testing.T) {
	source := lexicalFixture + `error Broken { message: string }
record Box { value: string }
fn make() -> Box { Box("value") }
effect fn broken() -> () raises { Broken } { fail Broken("bad") }
`
	program, diagnostics := parse(source)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	original := captureOriginalSyntax(program)
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("fixture: %+v", r.Diagnostics)
	}
	if !reflect.DeepEqual(original.syntax, r.lexical.syntax) {
		t.Fatal("checked lowering rewrote original syntax facts")
	}
	broken := r.Program.Functions[len(r.Program.Functions)-1].Body.Statements[0]
	if broken.Payload.Kind != "payload" {
		t.Fatal("control did not exercise payload lowering")
	}
	if _, exists := r.lexical.expressions[broken.Payload]; exists {
		t.Fatal("generated wrapper claimed original syntax")
	}
	makeExpr := r.Program.Functions[len(r.Program.Functions)-2].Body.Statements[0].Value
	if len(makeExpr.Fields) != 1 {
		t.Fatal("control did not exercise constructor field lowering")
	}
	for _, fact := range r.lexical.syntax {
		if fact.Extent.Length <= 0 || fact.Extent.Offset < 0 || fact.Extent.Offset+fact.Extent.Length > len(source) {
			t.Fatalf("invalid original extent: %+v", fact)
		}
		for _, child := range fact.Children {
			if child < 0 || child >= len(r.lexical.syntax) {
				t.Fatalf("dangling original child: %+v", fact)
			}
		}
	}
	pattern := r.Program.Functions[1].Body.Statements[0].Value.Arms[0].Pattern
	if len(pattern.Names) != 2 || pattern.Names[0].Field != "suffix" || pattern.Names[1].Field != "value" {
		t.Fatalf("pattern order lost: %+v", pattern.Names)
	}
	alias := pattern.Names[1]
	if source[alias.NameSpan.Offset:alias.NameSpan.Offset+alias.NameSpan.Length] != "label" {
		t.Fatal("pattern alias does not navigate to name token")
	}
	let := r.Program.Functions[0].Body.Statements[0]
	if source[let.Extent.Offset:let.Extent.Offset+let.Extent.Length] != "let label = input" || source[let.NameSpan.Offset:let.NameSpan.Offset+let.NameSpan.Length] != "label" {
		t.Fatalf("let extent/name: %+v", let)
	}
	if let.Span.Length != len("let") {
		t.Fatal("diagnostic anchor changed")
	}
}

func TestLexicalObservationDoesNotBroadenShadowingAdmission(t *testing.T) {
	r := Compile(`fn duplicate(input: string) -> string { let input = "new" input }`)
	if r.Checked {
		t.Fatal("duplicate local was admitted")
	}
	found := false
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Code == "EF101" {
			found = true
		}
	}
	if !found {
		t.Fatalf("existing duplicate diagnostic lost: %+v", r.Diagnostics)
	}
}
