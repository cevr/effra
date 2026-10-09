package compiler

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// jsImportKeyword finds the import keyword in any form: a declaration, a
// call with either quote, or a call with space before the parenthesis.
var jsImportKeyword = regexp.MustCompile(`\bimport\b`)

// jsHostLoaderReference finds calls of rendered host loaders.
var jsHostLoaderReference = regexp.MustCompile(`\b__ef_host_[A-Za-z0-9_]+`)

// jsHostLoaderLine is the one rendering of a declared host import.
var jsHostLoaderLine = regexp.MustCompile(`(?m)^const (__ef_host_[A-Za-z0-9_]+) = \(\) => import\('([^']+)'\);$`)

// Chunk text never spells an import, so the prelude's rendered imports are
// the only ones a module can contain; each chunk references exactly the
// loaders of the host modules it declares.
func TestJSPreludeChunksDeclareTheirHostModules(t *testing.T) {
	declared := 0
	loaders := map[string]string{}
	for _, chunk := range jsPrelude {
		if jsImportKeyword.MatchString(chunk.source) {
			t.Errorf("chunk %s spells an import; declare a host module and call its loader", chunk.name)
		}
		referenced := []string{}
		for _, name := range jsHostLoaderReference.FindAllString(chunk.source, -1) {
			if !slices.Contains(referenced, name) {
				referenced = append(referenced, name)
			}
		}
		slices.Sort(referenced)
		want := []string{}
		for _, host := range chunk.hostModules {
			loader := jsHostLoader(host)
			if other, taken := loaders[loader]; taken && other != host {
				t.Errorf("host modules %s and %s share loader %s", other, host, loader)
			}
			loaders[loader] = host
			want = append(want, loader)
		}
		slices.Sort(want)
		if !slices.Equal(referenced, want) {
			t.Errorf("chunk %s calls loaders %v but declares %v", chunk.name, referenced, want)
		}
		declared += len(chunk.hostModules)
	}
	if declared == 0 {
		t.Fatal("no chunk declares a host module; the HTTP transport loads node:http")
	}
}

// The guard sees every spelling of an import, including the forms a
// single-quote call pattern would miss.
func TestJSImportGuardSeesEverySpelling(t *testing.T) {
	for _, text := range []string{`import("node:fs")`, `import ('node:fs')`, "import(`node:fs`)", `import fs from 'node:fs'`, `import{a}from'x'`} {
		if !jsImportKeyword.MatchString(text) {
			t.Errorf("guard misses %s", text)
		}
	}
	for _, text := range []string{`important`, `__ef_host_node_http()`, `reimported`} {
		if jsImportKeyword.MatchString(text) {
			t.Errorf("guard flags %s", text)
		}
	}
}

// EmitModule's import set is exactly what the emitted text imports: the
// rendered effect names, and the rendered loaders of the selected chunks'
// host modules.
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
			for _, match := range jsHostLoaderLine.FindAllStringSubmatch(module.Source, -1) {
				if match[1] != jsHostLoader(match[2]) {
					t.Errorf("%s: loader %s renders %s", path, match[1], match[2])
				}
				hosts = append(hosts, match[2])
			}
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
		t.Fatal("no example exercises a host import")
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
