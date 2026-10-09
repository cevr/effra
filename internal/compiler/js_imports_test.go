package compiler

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// jsDynamicImport finds import() calls with a literal specifier. It is only
// applied to compiler-owned chunk text and to examples without such strings.
var jsDynamicImport = regexp.MustCompile(`\bimport\(\s*'([^']+)'\s*\)`)

// Each prelude chunk declares exactly the host modules its own text loads,
// so a module's declared imports cannot drift from the chunks it emits.
func TestJSPreludeChunksDeclareTheirHostModules(t *testing.T) {
	declared := 0
	for _, chunk := range jsPrelude {
		loaded := []string{}
		for _, match := range jsDynamicImport.FindAllStringSubmatch(chunk.source, -1) {
			loaded = append(loaded, match[1])
		}
		slices.Sort(loaded)
		want := slices.Sorted(slices.Values(chunk.hostModules))
		if !slices.Equal(loaded, want) {
			t.Errorf("chunk %s loads %v but declares %v", chunk.name, loaded, want)
		}
		if strings.Contains(chunk.source, "\nimport ") || strings.HasPrefix(chunk.source, "import ") {
			t.Errorf("chunk %s has a static import; only the prelude renders the effect import", chunk.name)
		}
		declared += len(chunk.hostModules)
	}
	if declared == 0 {
		t.Fatal("no chunk declares a host module; the HTTP transport loads node:http")
	}
}

// EmitModule's import set is exactly what the emitted text imports: the
// rendered effect names, and the dynamic host modules of selected chunks.
func TestJSModuleImportsAreTheEmittedImports(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil {
		t.Fatal(err)
	}
	sawHost := false
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r := CompileAt(string(source), "js", "../../examples")
		if !r.Checked || r.Program.GoOnly {
			continue
		}
		surfaces := []bool{false}
		if r.Entry() == nil {
			surfaces = append(surfaces, true)
		}
		for _, entry := range surfaces {
			module, err := r.EmitModule(entry)
			if err != nil {
				t.Fatal(path, err)
			}
			if js, decl, _ := r.Emit(entry); js != module.Source || decl != module.Declaration {
				t.Fatalf("%s: Emit and EmitModule disagree", path)
			}
			rendered := []string{}
			for _, match := range jsEffectImportLine.FindAllStringSubmatch(module.Source, -1) {
				rendered = append(rendered, strings.Split(match[1], ", ")...)
			}
			hosts := []string{}
			for _, match := range jsDynamicImport.FindAllStringSubmatch(module.Source, -1) {
				if !slices.Contains(hosts, match[1]) {
					hosts = append(hosts, match[1])
				}
			}
			slices.Sort(hosts)
			gotEffect, gotHosts := []string{}, []string{}
			for _, imported := range module.Imports {
				switch {
				case imported.Specifier == "effect" && !imported.Dynamic:
					gotEffect = imported.Names
				case imported.Dynamic && len(imported.Names) == 0:
					gotHosts = append(gotHosts, imported.Specifier)
				default:
					t.Errorf("%s: unexpected import %+v", path, imported)
				}
			}
			if !slices.Equal(gotEffect, rendered) || !slices.Equal(gotHosts, hosts) {
				t.Errorf("%s entry=%v: declared effect %v hosts %v, emitted effect %v hosts %v", path, entry, gotEffect, gotHosts, rendered, hosts)
			}
			sawHost = sawHost || len(hosts) > 0
		}
	}
	if !sawHost {
		t.Fatal("no example exercises a dynamic host import")
	}
}

// Program text is not an import: a string that spells an import() call
// adds no module to the declared set.
func TestJSModuleImportsIgnoreProgramStrings(t *testing.T) {
	r := CompileFor("effect fn main() -> string {\n    \"import('node:http') and import(\"\n}\n", "js")
	if !r.Checked {
		t.Fatalf("check failed: %+v", r.Diagnostics)
	}
	module, err := r.EmitModule(true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(module.Source, "import('node:http')") {
		t.Fatal("the string is not in the emitted module")
	}
	for _, imported := range module.Imports {
		if imported.Specifier != "effect" || imported.Dynamic {
			t.Errorf("program string produced import %+v", imported)
		}
	}
}
