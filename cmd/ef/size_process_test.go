package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"effra.local/prototype/internal/gosyntax"
	"effra.local/prototype/internal/receipt"
	rt "effra.local/prototype/runtime/effra"
)

// sizeFixture is one row of the size-conformance matrix in
// conformance/size/fixtures. The deterministic retention facts are checked
// here, in the gate; byte comparisons against the Go and TypeScript/Effect
// controls belong to the matrix in conformance/size/matrix.
type sizeFixture struct {
	name    string
	modules []rt.RuntimeModule
	// absent packages appear neither in the transitive dependencies nor in
	// the executable's symbol table; present packages appear in both.
	absent  []string
	present []string
	// output is the program's stdout on both targets; empty means build only.
	output string
}

var platformPackages = []string{"net", "net/http", "crypto/tls", "encoding/json", "os/exec"}

var sizeFixtures = []sizeFixture{
	{name: "minimal", modules: []rt.RuntimeModule{rt.RuntimeModuleCore}, absent: platformPackages, output: "minimal\n"},
	{name: "minimal-unused", modules: []rt.RuntimeModule{rt.RuntimeModuleCore}, absent: platformPackages, output: "minimal\n"},
	{name: "managed", modules: []rt.RuntimeModule{rt.RuntimeModuleCore}, absent: platformPackages, output: "child joined; interrupted; timed out; recovered\n"},
	{name: "codec", modules: []rt.RuntimeModule{rt.RuntimeModuleCodec, rt.RuntimeModuleCore}, absent: []string{"net", "net/http", "crypto/tls", "os/exec"}, output: "{\"id\":\"7\",\"name\":\"Ada\"}\n"},
	// The HTTP row is the detector's negative control: the same receipt
	// fields that stay empty above must name the transport here.
	{name: "http", modules: []rt.RuntimeModule{rt.RuntimeModuleCore, rt.RuntimeModuleHTTP}, absent: []string{"os/exec"}, present: []string{"net", "net/http"}},
	{name: "direct", modules: []rt.RuntimeModule{rt.RuntimeModuleCore}, absent: platformPackages, output: "Hello, <ada!>\n"},
	{name: "pipe", modules: []rt.RuntimeModule{rt.RuntimeModuleCore}, absent: platformPackages, output: "Hello, <ada!>\n"},
}

type builtSize struct {
	native receipt.Application
}

func readReceipt(t *testing.T, path string) receipt.Application {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var measured receipt.Application
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&measured); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if measured.Schema != receipt.Schema {
		t.Fatalf("%s: schema %q", path, measured.Schema)
	}
	return measured
}

func symbolPackages(measured receipt.Application) map[string]receipt.SymbolPackage {
	packages := map[string]receipt.SymbolPackage{}
	for _, entry := range measured.Symbols.Packages {
		packages[entry.Package] = entry
	}
	return packages
}

// generatedMainTree prints the syntax tree of an application's generated
// main without positions or comments, among them its //line directives.
func generatedMainTree(t *testing.T, measured receipt.Application) string {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(measured.Generation.Directory, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := gosyntax.Tree(source)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// symbolsWithoutAddresses lists an executable's symbols by name, kind and
// size, in name order. nm runs from the generated module, as the receipt's
// measurement does, so the module's toolchain reads its executable.
func symbolsWithoutAddresses(t *testing.T, measured receipt.Application, executable string) []string {
	t.Helper()
	command := exec.Command("go", "tool", "nm", "-size", "-type", "-sort", "name", executable)
	command.Dir = measured.Generation.Directory
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go tool nm %s: %v", executable, err)
	}
	symbols := []string{}
	for _, line := range strings.Split(strings.TrimRight(string(output), "\n"), "\n") {
		// nm prints address, size, kind, name and type, but leaves the
		// address blank for an undefined symbol (kind U).
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[1] != "U" {
			fields = fields[1:]
		}
		if len(fields) > 0 {
			symbols = append(symbols, strings.Join(fields, " "))
		}
	}
	return symbols
}

// symbolDifference lists the symbols of each sorted listing that the other
// lacks, counting repeats.
func symbolDifference(a, b []string) (onlyA, onlyB []string) {
	count := map[string]int{}
	for _, symbol := range a {
		count[symbol]++
	}
	for _, symbol := range b {
		count[symbol]--
	}
	for _, symbol := range a {
		if count[symbol] > 0 {
			onlyA, count[symbol] = append(onlyA, symbol), count[symbol]-1
		}
	}
	for _, symbol := range b {
		if count[symbol] < 0 {
			onlyB, count[symbol] = append(onlyB, symbol), count[symbol]+1
		}
	}
	return onlyA, onlyB
}

// firstSymbols renders up to ten symbols of a difference.
func firstSymbols(symbols []string) string {
	if len(symbols) > 10 {
		return fmt.Sprintf("%s, and %d more", strings.Join(symbols[:10], "; "), len(symbols)-10)
	}
	return strings.Join(symbols, "; ")
}

// A symbol-list mismatch names the symbols each side lacks, repeats counted.
func TestSymbolDifferenceNamesEachSidesExtraSymbols(t *testing.T) {
	onlyA, onlyB := symbolDifference([]string{"8 T a", "8 T a", "4 D b", "2 T c"}, []string{"8 T a", "4 D b", "3 T c"})
	if !slices.Equal(onlyA, []string{"8 T a", "2 T c"}) || !slices.Equal(onlyB, []string{"3 T c"}) {
		t.Errorf("difference %q / %q", onlyA, onlyB)
	}
}

func TestSizeFixturesRetainOnlyReachableRuntime(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	fixtures, err := filepath.Abs(filepath.Join("..", "..", "conformance", "size", "fixtures"))
	if err != nil {
		t.Fatal(err)
	}
	built := map[string]builtSize{}
	for _, fixture := range sizeFixtures {
		source := filepath.Join(fixtures, fixture.name+".ef")
		native := filepath.Join(root, "receipts", fixture.name+".go.json")
		executable := filepath.Join(root, "bin", fixture.name)
		if stdout, stderr, code := runTestCLIDir(t, binary, root, "", "build", source, "-o", executable, "--receipt", native); code != 0 {
			t.Fatalf("%s: go build failed: code=%d stdout=%q stderr=%q", fixture.name, code, stdout, stderr)
		}
		row := builtSize{native: readReceipt(t, native)}
		built[fixture.name] = row

		measured := row.native
		if !slices.Equal(measured.Plan.RuntimeModules, fixture.modules) {
			t.Errorf("%s: runtime modules = %v, want %v", fixture.name, measured.Plan.RuntimeModules, fixture.modules)
		}
		selected, err := rt.SelectSources(fixture.modules...)
		if err != nil {
			t.Fatal(err)
		}
		runtimeFiles := []string{}
		for _, file := range measured.Generation.Files {
			if name, found := strings.CutPrefix(file.Path, "runtime/"); found {
				runtimeFiles = append(runtimeFiles, name)
				if file.Bytes != len(selected[name]) {
					t.Errorf("%s: runtime/%s has %d bytes, want %d", fixture.name, name, file.Bytes, len(selected[name]))
				}
			}
		}
		if len(runtimeFiles) != len(selected) {
			t.Errorf("%s: generated runtime files %v, want the selected %d", fixture.name, runtimeFiles, len(selected))
		}
		if measured.Compiler.RuntimeSourceBytes <= measured.Generation.RuntimeBytes && fixture.name != "http" {
			t.Errorf("%s: application runtime %d bytes is not smaller than the distributed runtime %d", fixture.name, measured.Generation.RuntimeBytes, measured.Compiler.RuntimeSourceBytes)
		}
		if info, err := os.Stat(executable); err != nil || info.Size() != measured.Binary.Bytes {
			t.Errorf("%s: receipt binary bytes %d do not match the executable (%v)", fixture.name, measured.Binary.Bytes, err)
		}
		symbols := symbolPackages(measured)
		for _, name := range fixture.absent {
			if slices.Contains(measured.Deps.Transitive, name) {
				t.Errorf("%s: depends on %s", fixture.name, name)
			}
			if _, found := symbols[name]; found {
				t.Errorf("%s: executable retains %s symbols", fixture.name, name)
			}
		}
		for _, name := range fixture.present {
			if !slices.Contains(measured.Deps.Transitive, name) {
				t.Errorf("%s: does not depend on %s", fixture.name, name)
			}
			if _, found := symbols[name]; !found {
				t.Errorf("%s: executable has no %s symbols", fixture.name, name)
			}
		}
		if len(measured.Deps.NonStandard) != 2 || measured.Deps.NonStandard[0] != "effra.generated" || measured.Deps.NonStandard[1] != "effra.generated/runtime" {
			t.Errorf("%s: non-standard dependencies %v", fixture.name, measured.Deps.NonStandard)
		}

		if fixture.output == "" {
			continue
		}
		if output, err := exec.Command(executable).Output(); err != nil || string(output) != fixture.output {
			t.Errorf("%s: native output %q (%v), want %q", fixture.name, output, err, fixture.output)
		}
	}

	// The HTTP transport is visible to every detector the minimal rows use.
	if minimal, transport := symbolPackages(built["minimal"].native)["effra.generated/runtime"], symbolPackages(built["http"].native)["effra.generated/runtime"]; transport.TextBytes <= minimal.TextBytes {
		t.Errorf("HTTP runtime symbols (%d text bytes) do not exceed the minimal core (%d)", transport.TextBytes, minimal.TextBytes)
	}

	// Unused declarations and equivalent spellings retain exactly the same
	// program: identical runtime, generated syntax tree, imports,
	// dependencies and symbols by name, kind and size. Only source positions differ: the
	// generated main's //line directives name each source file and line, and
	// so do the executable's position tables, which change its size and shift
	// symbol addresses (and so the receipt's listing digest) but no symbol's
	// name, kind or size.
	for _, pair := range [][2]string{{"minimal", "minimal-unused"}, {"direct", "pipe"}} {
		a, b := built[pair[0]], built[pair[1]]
		runtimeOnly := func(files []receipt.File) []receipt.File {
			return slices.DeleteFunc(slices.Clone(files), func(file receipt.File) bool { return !strings.HasPrefix(file.Path, "runtime/") })
		}
		if !slices.Equal(runtimeOnly(a.native.Generation.Files), runtimeOnly(b.native.Generation.Files)) {
			t.Errorf("%s/%s: generated runtime differs", pair[0], pair[1])
		}
		if generatedMainTree(t, a.native) != generatedMainTree(t, b.native) {
			t.Errorf("%s/%s: generated mains parse to different programs", pair[0], pair[1])
		}
		for pkg, imports := range a.native.Imports {
			if !slices.Equal(imports, b.native.Imports[pkg]) {
				t.Errorf("%s/%s: %s imports %v vs %v", pair[0], pair[1], pkg, imports, b.native.Imports[pkg])
			}
		}
		if !slices.Equal(a.native.Deps.Transitive, b.native.Deps.Transitive) {
			t.Errorf("%s/%s: dependencies differ", pair[0], pair[1])
		}
		if !slices.Equal(a.native.Symbols.Packages, b.native.Symbols.Packages) {
			t.Errorf("%s/%s: receipts retain different symbols per package: %+v vs %+v", pair[0], pair[1], a.native.Symbols.Packages, b.native.Symbols.Packages)
		}
		if onlyA, onlyB := symbolDifference(symbolsWithoutAddresses(t, a.native, filepath.Join(root, "bin", pair[0])), symbolsWithoutAddresses(t, b.native, filepath.Join(root, "bin", pair[1]))); len(onlyA) > 0 || len(onlyB) > 0 {
			t.Errorf("%s/%s: executables retain different symbols (size kind name type)\nonly in %s: %s\nonly in %s: %s", pair[0], pair[1], pair[0], firstSymbols(onlyA), pair[1], firstSymbols(onlyB))
		}
	}
}
