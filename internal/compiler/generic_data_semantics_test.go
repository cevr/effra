package compiler

import (
	"testing"
)

func assertGenericChecked(t *testing.T, source string) *Result {
	t.Helper()
	r := Compile(source)
	if !r.Checked {
		t.Fatal("generic source rejected", r.Diagnostics)
	}
	return r
}

func TestGenericDataConstructorsAndMatch(t *testing.T) {
	assertGenericChecked(t, `record User { name: string }
record Box<T: type> { value: T; label: string }
enum Presence<T: type> { None; Some { value: T } }
fn box() -> Box<User> { Box { value: User { name: "Ada" }; label: "user" } }
fn empty() -> Presence<User> { Presence<User>.None {} }
fn present() -> Presence<User> { Presence.Some { value: box().value } }
fn read(value: Presence<User>) -> string {
 match value { Presence.None => "missing"; Presence.Some {value: user} => user.name }
}
fn nested() -> Box<Presence<User>> { Box { value: empty(); label: "nested" } }
fn choose(flag: bool) -> Presence<User> {
 if flag { present() } else { empty() }
}`)
}

func TestGenericDataCallableVariantEvidence(t *testing.T) {
	assertGenericChecked(t, `record Callback<F: callable fn(A) -> A, A: type> { invoke: F }
enum Action<F: callable fn(A) -> A, A: type> { None; Some { value: Callback<F,A> } }
fn identity(value: string) -> string { value }
fn selected() -> Action<fn(string) -> string,string> {
 Action<fn(string) -> string,string>.Some { value: Callback<fn(string) -> string,string> { invoke: identity } }
}
fn invoke(value: Action<fn(string) -> string,string>) -> string {
 match value { Action.None => "none"; Action.Some {value: callback} => callback.invoke("value") }
}
fn use() -> string { invoke(selected()) }`)
}

func TestGenericDataConstructorControls(t *testing.T) {
	const declarations = `record User { name: string }
record Other { name: string }
record Box<T: type> { value: T }
enum Presence<T: type> { None; Some { value: T } }
`
	for _, source := range []string{
		`fn bad() -> Presence<User> { Presence.None {} }`,
		`fn bad() -> Presence<User> { Presence<User>.Some { value: Other {name: "x"} } }`,
		`fn bad() -> Box<User> { Box<User> {} }`,
		`fn bad() -> Presence<User> { Presence.Some<User> {value: User {name:"x"}} }`,
		`enum Duplicate<T: type> { Same; Same }`,
		`fn bad(value: Presence<User>) -> string { match value { Presence.None => "x" } }`,
		`fn bad(value: Presence<User>) -> string { match value { Box.Some {value} => "x"; Presence.None => "y" } }`,
	} {
		r := Compile(declarations + source)
		if len(r.Diagnostics) == 0 || r.Checked {
			t.Fatal("invalid generic semantics escaped refusal", source, r.Diagnostics)
		}
	}
}

func TestGenericConstructorFormattingAndRefusalBoundary(t *testing.T) {
	const source = `fn read(value: Remote.Presence<string>) -> string {
 match value { Remote.Presence.None => "missing"; Remote.Presence.Some {value} => value }
}
fn empty() -> Remote.Presence<string> { Remote.Presence<string>.None {} }`
	formatted, err := FormatSource(source)
	if err != nil {
		t.Fatal(err)
	}
	second, err := FormatSource(formatted.Text)
	if err != nil || second.Text != formatted.Text || FormatterIdentity != "effra/formatter-8" {
		t.Fatal(formatted, second, err)
	}
	for _, source := range []string{
		`fn identity(value: string)->string {value} fn bad()->string { identity<string>("x") }`,
		`fn identity(value: string)->string {value} fn bad()->fn(string)->string { identity<string> }`,
		`record Box<T: type>{value:T} fn bad()->Box<string>{Box<string><bool>{value:"x"}}`,
	} {
		r := Compile(source)
		if r.Checked || len(r.Diagnostics) == 0 {
			t.Fatal("unchecked explicit application syntax", source, r.Diagnostics)
		}
	}
}

func TestGenericDataNestedApplicationInvariance(t *testing.T) {
	const prefix = `error Trouble
record Ops<F: callable effect fn(A)->A, A: type> { operation: F }
record Wrapper<T: type> { value: Ops<effect fn(string)->string raises {Trouble},string>; anchor: T }
effect fn narrow(value: string)->string {value}
effect fn wide(value: string)->string raises {Trouble} {value}
`
	assertGenericChecked(t, prefix+`fn good()->Wrapper<string>{Wrapper { value: Ops {operation:wide}; anchor:"x" } }`)
	assertGenericChecked(t, prefix+`fn direct()->Ops<effect fn(string)->string raises {Trouble},string>{
 Ops<effect fn(string)->string raises {Trouble},string> {operation:narrow}
}`)
	r := Compile(prefix + `fn bad()->Wrapper<string>{Wrapper { value: Ops {operation:narrow}; anchor:"x" } }`)
	found := false
	for _, d := range r.Diagnostics {
		found = found || d.Message == "template fields require initialized values"
	}
	if !found {
		t.Fatal("different nested application arguments were widened", r.Diagnostics)
	}
}

func TestGenericDataReservedVariantDiscriminator(t *testing.T) {
	r := Compile(`enum Presence<T:type> { Some {_tag:T} }`)
	if !hasCode(r, "EF120") {
		t.Fatal("generic variant payload can replace its discriminator", r.Diagnostics)
	}
	assertGenericChecked(t, `record Product<T:type> {_tag:T}`)
}

func TestGenericDataNestedHandleAndCallbackOwnership(t *testing.T) {
	const declarations = `record Box<T: type> { value: T }
enum Presence<T: type> { None; Some { value: T } }
enum Action<T: type> { None; Some { operation: effect fn(File)->File raises {IoError} uses {Files} } }
fn wrap(file: File) -> Box<Presence<File>> { Box { value: Presence.Some { value: file } } }
fn unwrap(value: Box<Presence<File>>, fallback: File) -> File {
 match value.value { Presence.None => fallback; Presence.Some {value: file} => file }
}
effect fn keep(file: File) -> File { file }
effect fn acquire(file: File) -> File raises {IoError} uses {Files} { run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles) }
effect fn invoke(action: Action<string>, file: File) -> File raises {IoError} uses {Files} {
 match action { Action.None => file; Action.Some {operation} => run operation(file) }
}
`
	for _, test := range []struct {
		name, body string
		rejected   bool
	}{
		{"borrowed nested", `scope { unwrap(wrap(file), file) }`, false},
		{"acquired nested", `scope { let owned = run acquire(file); unwrap(wrap(owned), file) }`, true},
		{"borrowed callback", `scope { run invoke(Action<string>.Some {operation: keep}, file) }`, false},
		{"acquired callback", `scope { run invoke(Action<string>.Some {operation: acquire}, file) }`, true},
		{"unresolved callback", `scope { run invoke(action, file) }`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := declarations + `effect fn outer(file: File, action: Action<string>) -> File raises {IoError} uses {Files} {` + test.body + `}`
			r := Compile(source)
			if hasCode(r, "EF123") != test.rejected {
				t.Fatal("generic ownership proof", r.Diagnostics)
			}
			for _, d := range r.Diagnostics {
				if d.Code != "EF123" {
					t.Fatal("unexpected diagnostic", d)
				}
			}
			if !test.rejected {
				found := false
				for _, symbol := range r.Symbols {
					if symbol.Name == "outer" {
						for _, fact := range symbol.Actual.Ownership {
							found = found || fact.Status == "borrowed"
						}
					}
				}
				if !found {
					t.Fatal("borrowed result evidence disappeared", source)
				}
			}
		})
	}
}
