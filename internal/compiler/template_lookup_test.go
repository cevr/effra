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
// unqualified inside effra/conversions.
const templateAliasAgainstLocal = `import Convert "effra/conversions"
import Data "effra/data"
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
		{"effra/conversions", "Codec", "effra/conversions", ""},
		{"", "Convert.Codec", "effra/conversions", ""},
		{"effra/conversions", "Convert.Codec", "effra/conversions", ""},
		{currentModuleIdentity, "Option", currentModuleIdentity, "Missing"},
		{"effra/data", "Option", "effra/data", "None"},
		{currentModuleIdentity, "Data.Option", "effra/data", "None"},
	} {
		r := templateInScope(c, want.module, want.name)
		if r == nil || r.Module != want.owner || want.variant != "" && (len(r.Variants) == 0 || r.Variants[0].Name != want.variant) {
			t.Fatalf("%q in scope %q resolved to %+v, want a template of %s", want.name, want.module, r, want.owner)
		}
	}
	for _, missing := range []struct{ module, name string }{
		{currentModuleIdentity, "User"},
		{"effra/conversions", "Option"},
		{"effra/data", "Codec"},
		{"effra/missing", "Codec"},
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
		modules := []string{"", currentModuleIdentity, "effra/missing"}
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

func TestTemplateLookupFollowsGrowingDeclarations(t *testing.T) {
	r := Compile(templateShadowing)
	c := r.Program.semantic
	if templateInScope(c, "", "Late") != nil {
		t.Fatal("undeclared template resolved")
	}
	late := &DataDeclaration{Kind: "record", Name: "Late", Parameters: []TemplateParameter{{Name: "T", Kind: "type"}}}
	r.Program.Records = append(r.Program.Records, late)
	if got := templateInScope(c, "", "Late"); got != late {
		t.Fatalf("index answered from a stale declaration set: %+v", got)
	}
	if got := templateInScope(c, "effra/data", "Late"); got != nil {
		t.Fatalf("a local template leaked into a bundled scope: %+v", got)
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
