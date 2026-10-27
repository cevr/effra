package compiler

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// scanTemplateByName is the lookup the name index replaced, kept as the
// precedence oracle: an alias-qualified name is a bundled type binding; in
// the checked file, the first generic record, then generic enum, of the name,
// then a bundled template of the file's module; elsewhere, the first bundled
// template of the scope's module.
func scanTemplateByName(p *Program, module, name string) *Record {
	if alias, member, qualified := strings.Cut(name, "."); qualified {
		return p.BundledTypeBindings[alias][member]
	}
	if module == "" || module == currentModuleIdentity {
		for _, r := range append(append([]*DataDeclaration{}, p.Records...), p.Enums...) {
			if len(r.Parameters) > 0 && r.Name == name {
				return r
			}
		}
	}
	for _, r := range p.BundledTemplates {
		if r.Module == module && r.Name == name {
			return r
		}
	}
	return nil
}

// templateInScope resolves name with the checker's function module set to
// module, as checking a function of that module does.
func templateInScope(c *checker, module, name string) *Record {
	previous := c.functionModule
	c.functionModule = module
	defer func() { c.functionModule = previous }()
	return c.templateByName(name)
}

// A local generic Codec beside the bundled witness, whose body names Codec
// unqualified inside ergo/conversions.
const templateAliasAgainstLocal = `import Convert "ergo/conversions"
import Data "ergo/data"
record Codec<T: type> { local: T }
enum Option<T: type> { Missing, Present { value: T } }
record User { name: string }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
fn local() -> Option<string> { Option.Present { value: "local" } }
fn bundled() -> Data.Option<string> { Data.Option<string>.None {} }
fn kept(value: Codec<string>) -> string { value.local }
effect fn main() -> string {
    let converter = Convert.witness(decode, encode)
    let user = run converter.decode("Ada")
    run converter.encode(user)
}
`

// Same-name declarations: a plain record ahead of a generic enum, and a
// generic record ahead of a generic enum.
const templateShadowing = `record Box { x: i64 }
enum Box<T: type> { Empty, Full { value: T } }
record Pair<T: type> { first: T }
enum Pair<T: type> { One { first: T } }
record Plain { x: i64 }
`

func TestTemplateLookupPrecedence(t *testing.T) {
	alias := Compile(templateAliasAgainstLocal)
	if !alias.Checked {
		t.Fatal("a local generic Codec must not leak into the bundled witness", alias.Diagnostics)
	}
	shadow := Compile(templateShadowing)
	c := alias.Program.semantic
	for _, want := range []struct {
		module, name, owner string
		variant             string
	}{
		{"", "Codec", currentModuleIdentity, ""},
		{currentModuleIdentity, "Codec", currentModuleIdentity, ""},
		{"ergo/conversions", "Codec", "ergo/conversions", ""},
		{"", "Convert.Codec", "ergo/conversions", ""},
		{"ergo/conversions", "Convert.Codec", "ergo/conversions", ""},
		{currentModuleIdentity, "Option", currentModuleIdentity, "Missing"},
		{"ergo/data", "Option", "ergo/data", "None"},
		{currentModuleIdentity, "Data.Option", "ergo/data", "None"},
	} {
		r := templateInScope(c, want.module, want.name)
		if r == nil || r.Module != want.owner || want.variant != "" && (len(r.Variants) == 0 || r.Variants[0].Name != want.variant) {
			t.Fatalf("%q in scope %q resolved to %+v, want a template of %s", want.name, want.module, r, want.owner)
		}
	}
	for _, missing := range []struct{ module, name string }{
		{currentModuleIdentity, "User"},
		{"ergo/conversions", "Option"},
		{"ergo/data", "Codec"},
		{"ergo/missing", "Codec"},
		{currentModuleIdentity, "Data.Option.Some"},
		{currentModuleIdentity, "Unknown.Codec"},
	} {
		if r := templateInScope(c, missing.module, missing.name); r != nil {
			t.Fatalf("%q in scope %q resolved to %+v, want none", missing.name, missing.module, r)
		}
	}
	box, pair := templateInScope(shadow.Program.semantic, "", "Box"), templateInScope(shadow.Program.semantic, "", "Pair")
	if box == nil || box.Kind != "enum" || pair == nil || pair.Kind != "record" {
		t.Fatalf("first generic declaration must win: Box %+v, Pair %+v", box, pair)
	}

	// Every name in every scope agrees with the scan, in both programs.
	for _, r := range []*Result{alias, shadow} {
		p := r.Program
		names := []string{"Missing", "Data.Option", "Convert.Codec", "Data.Option.Some", "Unknown.Codec"}
		modules := []string{"", currentModuleIdentity, "ergo/missing"}
		for _, list := range [][]*DataDeclaration{p.Records, p.Enums, p.BundledTemplates} {
			for _, d := range list {
				names = append(names, d.Name)
				modules = append(modules, d.Module)
			}
		}
		for _, module := range modules {
			for _, name := range names {
				if got, want := templateInScope(p.semantic, module, name), scanTemplateByName(p, module, name); got != want {
					t.Fatalf("%q in scope %q: index %p, scan %p", name, module, got, want)
				}
			}
		}
	}
}

// The index is derived from all three declaration lists, so growing any one
// of them is visible to the next lookup. Production fixes the lists before
// checking; this pins the derivation rule, one list at a time.
func TestTemplateLookupFollowsGrowingDeclarations(t *testing.T) {
	r := Compile(templateShadowing)
	p, c := r.Program, r.Program.semantic
	parameters := []TemplateParameter{{Name: "T", Kind: "type"}}
	for _, name := range []string{"LateEnum", "LateBundled", "Late"} {
		if templateInScope(c, "", name) != nil || templateInScope(c, "ergo/late", name) != nil {
			t.Fatalf("undeclared template %s resolved", name)
		}
	}

	// Enums alone grow.
	lateEnum := &DataDeclaration{Kind: "enum", Name: "LateEnum", Parameters: parameters}
	p.Enums = append(p.Enums, lateEnum)
	if got := templateInScope(c, "", "LateEnum"); got != lateEnum {
		t.Fatalf("index missed an enum appended after it was built: %+v", got)
	}

	// BundledTemplates alone grow.
	lateBundled := &DataDeclaration{Kind: "record", Name: "LateBundled", Module: "ergo/late", Parameters: parameters}
	p.BundledTemplates = append(p.BundledTemplates, lateBundled)
	if got := templateInScope(c, "ergo/late", "LateBundled"); got != lateBundled {
		t.Fatalf("index missed a bundled template appended after it was built: %+v", got)
	}
	if got := templateInScope(c, "", "LateBundled"); got != nil {
		t.Fatalf("a bundled template leaked into the file scope: %+v", got)
	}

	// Records grow with plain data of a generic enum's name, which must not
	// mask it, and then with a generic record.
	p.Records = append(p.Records, &DataDeclaration{Kind: "record", Name: "LateEnum"})
	if got := templateInScope(c, "", "LateEnum"); got != lateEnum {
		t.Fatalf("plain data masked a generic enum: %+v", got)
	}
	late := &DataDeclaration{Kind: "record", Name: "Late", Parameters: parameters}
	p.Records = append(p.Records, late)
	if got := templateInScope(c, "", "Late"); got != late {
		t.Fatalf("index missed a record appended after it was built: %+v", got)
	}
	if got := templateInScope(c, "ergo/data", "Late"); got != nil {
		t.Fatalf("a local template leaked into a bundled scope: %+v", got)
	}
}

// Every published template declaration carries its resolved layout, including
// fields whose type is not a template parameter.
func TestTemplateLayoutsReachEveryPublishedDeclaration(t *testing.T) {
	r := Compile(`import Data "ergo/data"
record A { x: i64 }
record Box<T: type> { value: T, count: i64 }
enum Choice<T: type> { Neither, Left { value: T, tag: string } }
record B { y: string }
record Pair<L: type, R: type> { left: L, right: R, label: string }
fn keep(value: Data.Option<Box<string>>) -> Data.Option<Box<string>> { value }
`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	templates := map[string]bool{}
	for _, d := range r.Declarations {
		if d.Kind != "template" {
			continue
		}
		templates[d.Name] = true
		fields := append([]Field{}, d.Fields...)
		for _, variant := range d.Variants {
			fields = append(fields, variant.Fields...)
		}
		for _, field := range fields {
			if field.TypeRef.Kind == "" {
				t.Fatalf("template %s field %s was published before its layout resolved", d.Name, field.Name)
			}
		}
	}
	for _, name := range []string{"Box", "Choice", "Pair", "Option"} {
		if !templates[name] {
			t.Fatalf("template %s was not published: %v", name, templates)
		}
	}
}

// Two generic declarations of one name share an identity and are published
// twice (the program is refused with EF101). The layout pass replaces every
// published copy, as the search it replaced did, so neither keeps fields
// that never resolved.
func TestTemplateLayoutsReachDuplicatePublishedIdentities(t *testing.T) {
	r := Compile(`record Pair<T: type> { first: T, count: i64 }
record Pair<T: type> { first: T, label: string }
`)
	if r.Checked || !hasCode(r, "EF101") {
		t.Fatal("duplicate generic declarations must be refused", r.Diagnostics)
	}
	copies := 0
	for _, d := range r.Declarations {
		if d.Kind != "template" || d.Name != "Pair" {
			continue
		}
		copies++
		for _, field := range d.Fields {
			if field.TypeRef.Kind == "" {
				t.Fatalf("published copy %d of Pair kept unresolved field %s", copies, field.Name)
			}
		}
	}
	if copies != 2 {
		t.Fatalf("Pair published %d times, want 2", copies)
	}
}

// A callable annotation's uses row admits a service the program declares,
// before or after the annotation, and refuses an undeclared one.
func TestCallableUsesRowsKnowDeclaredServices(t *testing.T) {
	const declared = `record Early { run: effect fn() -> string uses {Users} }
service Users { effect fn name() -> string }
record Late { run: effect fn() -> string uses {Users} }
fn call(value: Early) -> effect fn() -> string uses {Users} { value.run }
`
	if r := Compile(declared); !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	missing := Compile(strings.Replace(declared, "record Late { run: effect fn() -> string uses {Users} }", "record Late { run: effect fn() -> string uses {Missing} }", 1))
	if missing.Checked || !hasCode(missing, "EF102") {
		t.Fatal("an undeclared service entered a callable uses row", missing.Diagnostics)
	}
}

// templateLookupSource is issue #109's reproduction: n one-field records and
// one record whose n fields name them, so checking asks for a template by
// name about twice per field.
func templateLookupSource(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "record R%d { x: i64 }\n", i)
	}
	b.WriteString("record W {")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, " f%d: R%d", i, i)
	}
	b.WriteString(" }\n")
	return b.String()
}

// Checking charges allocation against source size. A name index keeps the
// shape near 370 bytes per source byte at every size; the copy-and-scan
// lookup it replaced allocated 2,594 B/B at n=2,500 and 4,766 B/B at n=5,000,
// growing with the declaration count.
const templateLookupBytesPerSourceByte = 512

func TestTemplateLookupAllocationIsLinearInSource(t *testing.T) {
	perByte := map[int]float64{}
	for _, n := range []int{2500, 5000} {
		source := templateLookupSource(n)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		r := Compile(source)
		runtime.ReadMemStats(&after)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		allocated := after.TotalAlloc - before.TotalAlloc
		perByte[n] = float64(allocated) / float64(len(source))
		t.Logf("n=%d: %d source bytes, %d bytes allocated (%.1f B/B)", n, len(source), allocated, perByte[n])
		if allocated > templateLookupBytesPerSourceByte*uint64(len(source)) {
			t.Fatalf("n=%d allocated %.1f bytes per source byte, over the %d B/B budget", n, perByte[n], templateLookupBytesPerSourceByte)
		}
	}
	// Doubling the program must not raise the allocation per source byte.
	if perByte[5000] > 1.25*perByte[2500] {
		t.Fatalf("allocation per source byte grew with size: %.1f -> %.1f B/B", perByte[2500], perByte[5000])
	}
}
