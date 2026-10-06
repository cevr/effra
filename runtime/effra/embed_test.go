package effra

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRuntimeCatalogCoversEmbeddedSourcesAndDeclaredImports(t *testing.T) {
	entries, err := sources.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	embedded := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("embedded runtime directory was not selectable: %s", entry.Name())
		}
		embedded[entry.Name()] = struct{}{}
	}
	wantFiles := make(map[string]struct{}, len(runtimeSourceFiles))
	for _, name := range runtimeSourceFiles {
		if _, duplicate := wantFiles[name]; duplicate {
			t.Fatalf("runtime source listed more than once: %s", name)
		}
		wantFiles[name] = struct{}{}
	}
	if !reflect.DeepEqual(embedded, wantFiles) {
		t.Fatalf("embed list and authoritative source list differ: embedded=%v listed=%v", sortedKeys(embedded), sortedKeys(wantFiles))
	}
	all := Sources()
	if !reflect.DeepEqual(sourceNames(all), sortedKeys(wantFiles)) {
		t.Fatalf("Sources returned the wrong files: %v", sourceNames(all))
	}

	fileOwners := map[string][]RuntimeModule{}
	for module, spec := range runtimeModuleCatalog {
		for _, dependency := range spec.dependencies {
			if _, ok := runtimeModuleCatalog[dependency]; !ok {
				t.Fatalf("module %q names unknown dependency %q", module, dependency)
			}
		}
		for _, name := range spec.files {
			fileOwners[name] = append(fileOwners[name], module)
		}
		actualImports := sourceImports(t, sourcesForFiles(t, all, spec.files))
		if !reflect.DeepEqual(actualImports, stringSet(spec.imports)) {
			t.Fatalf("module %q declared imports differ: actual=%v declared=%v", module, sortedKeys(actualImports), sortedKeys(stringSet(spec.imports)))
		}
		selected, err := SelectSources(module)
		if err != nil {
			t.Fatalf("select %q: %v", module, err)
		}
		if err := typecheckRuntimeSources(selected); err != nil {
			t.Fatalf("typecheck %q closure: %v", module, err)
		}
	}
	for _, name := range runtimeSourceFiles {
		owners := fileOwners[name]
		if len(owners) != 1 {
			t.Fatalf("runtime file %q belongs to %d modules: %v", name, len(owners), owners)
		}
	}
	for name, owners := range fileOwners {
		if _, ok := wantFiles[name]; !ok {
			t.Fatalf("catalog contains unembedded runtime file %q in %v", name, owners)
		}
	}
}

func TestRuntimeModuleSelectionIsBoundedAndOrderIndependent(t *testing.T) {
	empty, err := SelectSources()
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty roots must select an empty non-nil snapshot: %#v", empty)
	}
	if _, err := SelectSources(RuntimeModule("unknown")); err == nil || !strings.Contains(err.Error(), "unknown runtime module") {
		t.Fatalf("unknown module was not rejected usefully: %v", err)
	}

	first, err := SelectSources(RuntimeModuleFiles, RuntimeModuleCore, RuntimeModuleInterop)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SelectSources(RuntimeModuleInterop, RuntimeModuleFiles, RuntimeModuleCore, RuntimeModuleCore)
	if err != nil {
		t.Fatal(err)
	}
	assertSameSources(t, first, second)
	if _, ok := first["http.go"]; ok {
		t.Fatal("file/interop selection unexpectedly retained HTTP")
	}
	if _, ok := first["effect.go"]; !ok {
		t.Fatal("file/interop selection omitted core dependency")
	}

	mutated := first["files.go"]
	mutated[0] ^= 0xff
	delete(first, "files.go")
	again, err := SelectSources(RuntimeModuleFiles)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again["files.go"]; !ok {
		t.Fatal("mutating one result changed a later selection")
	}
	if bytes.Equal(mutated, again["files.go"]) {
		t.Fatal("later selection reused a caller-mutated source buffer")
	}
}

func TestRuntimeModuleImportBoundariesAndSelectedCompileControls(t *testing.T) {
	for _, module := range []RuntimeModule{
		RuntimeModuleCore,
		RuntimeModuleSync,
		RuntimeModuleHTTP,
		RuntimeModuleFiles,
		RuntimeModuleConsole,
		RuntimeModuleEnv,
		RuntimeModuleInspect,
		RuntimeModuleInterop,
	} {
		t.Run("compile-"+string(module), func(t *testing.T) {
			selected, err := SelectSources(module)
			if err != nil {
				t.Fatal(err)
			}
			if output, err := compileSelectedRuntime(t, selected); err != nil {
				t.Fatalf("selected runtime did not compile: %v\n%s", err, output)
			}
		})
	}

	cases := []struct {
		name    string
		roots   []RuntimeModule
		require []string
		forbid  []string
	}{
		{name: "core", roots: []RuntimeModule{RuntimeModuleCore}, forbid: []string{"net/http", "encoding/json", "os", "io"}},
		{name: "inspect", roots: []RuntimeModule{RuntimeModuleInspect}, require: []string{"encoding/json"}, forbid: []string{"net/http", "os"}},
		{name: "http", roots: []RuntimeModule{RuntimeModuleHTTP}, require: []string{"net/http"}, forbid: []string{"encoding/json", "os"}},
		{name: "files-interop", roots: []RuntimeModule{RuntimeModuleFiles, RuntimeModuleInterop}, require: []string{"os", "io"}, forbid: []string{"net/http", "encoding/json"}},
		{name: "sync", roots: []RuntimeModule{RuntimeModuleSync}, forbid: []string{"net/http", "encoding/json", "os", "io"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			selected, err := SelectSources(testCase.roots...)
			if err != nil {
				t.Fatal(err)
			}
			imports := sourceImports(t, selected)
			for _, path := range testCase.require {
				if _, ok := imports[path]; !ok {
					t.Fatalf("selected closure omitted required import %q: %v", path, sortedKeys(imports))
				}
			}
			for _, path := range testCase.forbid {
				if _, ok := imports[path]; ok {
					t.Fatalf("selected closure retained unrelated import %q: %v", path, sortedKeys(imports))
				}
			}
			if output, err := compileSelectedRuntime(t, selected); err != nil {
				t.Fatalf("selected runtime did not compile: %v\n%s", err, output)
			}
		})
	}
}

func TestRuntimeModuleMissingDependencyControlFails(t *testing.T) {
	selected, err := SelectSources(RuntimeModuleFiles)
	if err != nil {
		t.Fatal(err)
	}
	delete(selected, "effect.go")
	if err := typecheckRuntimeSources(selected); err == nil {
		t.Fatal("removing the real core dependency still typechecked")
	}
	output, err := compileSelectedRuntime(t, selected)
	if err == nil || !strings.Contains(string(output), "undefined: Effect") {
		t.Fatalf("missing dependency compile control did not fail at the source seam: err=%v output=%s", err, output)
	}
}

func sourcesForFiles(t *testing.T, all map[string][]byte, names []string) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte, len(names))
	for _, name := range names {
		data, ok := all[name]
		if !ok {
			t.Fatalf("module refers to missing source %q", name)
		}
		out[name] = data
	}
	return out
}

func sourceNames(sources map[string][]byte) []string {
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func sourceImports(t *testing.T, sources map[string][]byte) map[string]struct{} {
	t.Helper()
	set := map[string]struct{}{}
	for name, data := range sources {
		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, importSpec := range file.Imports {
			path, err := strconv.Unquote(importSpec.Path.Value)
			if err != nil {
				t.Fatalf("unquote %s import: %v", name, err)
			}
			set[path] = struct{}{}
		}
	}
	return set
}

func typecheckRuntimeSources(sources map[string][]byte) error {
	fileSet := token.NewFileSet()
	files := make([]*ast.File, 0, len(sources))
	for _, name := range sourceNames(sources) {
		file, err := parser.ParseFile(fileSet, name, sources[name], parser.ParseComments)
		if err != nil {
			return err
		}
		files = append(files, file)
	}
	_, err := (&types.Config{Importer: importer.Default()}).Check("effra.local/runtime-probe", fileSet, files, nil)
	return err
}

func compileSelectedRuntime(t *testing.T, sources map[string][]byte) ([]byte, error) {
	t.Helper()
	directory := t.TempDir()
	runtimeDirectory := filepath.Join(directory, "runtime")
	if err := os.MkdirAll(runtimeDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range sources {
		if err := os.WriteFile(filepath.Join(runtimeDirectory, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module effra.local/runtime-probe\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		cache = filepath.Join(directory, "cache")
	}
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = filepath.Join(directory, "tmp")
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	env := replaceEnv(replaceEnv(os.Environ(), "GOCACHE", cache), "TMPDIR", tmp)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "go", "test", "-run", "^$", "./runtime")
	command.Dir = directory
	command.Env = env
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, fmt.Errorf("selected runtime compile timed out: %w", ctx.Err())
	}
	return output, err
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(env)+1)
	found := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !found {
				result = append(result, prefix+value)
				found = true
			}
			continue
		}
		result = append(result, entry)
	}
	if !found {
		result = append(result, prefix+value)
	}
	return result
}

func assertSameSources(t *testing.T, first, second map[string][]byte) {
	t.Helper()
	if !reflect.DeepEqual(sourceNames(first), sourceNames(second)) {
		t.Fatalf("source membership changed with root order: %v vs %v", sourceNames(first), sourceNames(second))
	}
	for name, data := range first {
		if !bytes.Equal(data, second[name]) {
			t.Fatalf("source bytes changed with root order: %s", name)
		}
	}
}
