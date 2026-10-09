package compiler

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shapeFixtureTypes is the number of distinct payload records the shape
// fixtures instantiate Option over.
const shapeFixtureTypes = 64

// shapeSharingSource declares n records with distinct layouts, each returned
// through an effect function as Data.Option<Rk> and then matched. Every Rk is
// its own Go GC shape, so any generic the runtime instantiates over the option
// is stenciled once per record unless the option's own representation is shared.
func shapeSharingSource(n int) string {
	var out strings.Builder
	out.WriteString("import Data \"effra/data\"\n\n")
	for k := 1; k <= n; k++ {
		fmt.Fprintf(&out, "record R%d {\n    name: string;\n    f%d: i64\n}\n", k, k)
		fmt.Fprintf(&out, "effect fn get%d(id: string) -> Data.Option<R%d> {\n    if id == \"x\" {\n        Data.Option.Some { value: R%d { name: \"r%d\"; f%d: %d } }\n    } else {\n        Data.Option<R%d>.None {}\n    }\n}\n", k, k, k, k, k, k, k)
		fmt.Fprintf(&out, "fn label%d(o: Data.Option<R%d>) -> string {\n    match o {\n        Data.Option.None => \"none\",\n        Data.Option.Some { value: r } => r.name\n    }\n}\n", k, k)
	}
	out.WriteString("effect fn main() -> string {\n")
	labels := []string{}
	for k := 1; k <= n; k++ {
		fmt.Fprintf(&out, "    let x%d = run get%d(\"x\")\n", k, k)
		labels = append(labels, fmt.Sprintf("label%d(x%d)", k, k))
	}
	out.WriteString("    " + strings.Join(labels, " + ") + "\n}\n")
	return out.String()
}

// stenciledRuntimeCopies counts the text symbols of the generated runtime
// package that Go stenciled for a GC shape. Stenciled instantiations are named
// pkg.Func[go.shape.T,...]; the linker keeps one per distinct shape tuple.
func stenciledRuntimeCopies(symbols string) []string {
	var names []string
	for _, line := range strings.Split(symbols, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || (fields[2] != "T" && fields[2] != "t") {
			continue
		}
		name := strings.Join(fields[3:], " ")
		if strings.HasPrefix(name, generatedModulePath+"/runtime.") && strings.Contains(name, "[go.shape.") {
			names = append(names, name)
		}
	}
	return names
}

// stenciledCopyCount builds the fixture with n payload types and returns the
// stenciled runtime symbols and the binary size. It fails when the oracle
// recognizes no symbol or misses the runtime generics every effect step uses,
// so a renamed runtime package or a changed nm format cannot pass vacuously.
func stenciledCopyCount(t *testing.T, n int) (copies int, size int64) {
	t.Helper()
	binary := buildShapeFixture(t, shapeSharingSource(n))
	output, err := runGoCommand(filepath.Dir(binary), "tool", "nm", "-size", binary)
	if err != nil {
		t.Fatalf("go tool nm: %v\n%s", err, output)
	}
	names := stenciledRuntimeCopies(string(output))
	if len(names) == 0 {
		t.Fatalf("no stenciled runtime symbols recognized for %d types: the symbol oracle is vacuous", n)
	}
	for _, want := range []string{".FromCause[", ".Propagate["} {
		found := false
		for _, name := range names {
			found = found || strings.Contains(name, want)
		}
		if !found {
			t.Fatalf("symbol oracle did not recognize a %s stenciled symbol among %v", want, names)
		}
	}
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatal(err)
	}
	return len(names), info.Size()
}

// goOnPathVersion reports the toolchain the backend actually invokes: the
// `go` on PATH, which is not necessarily the Go that built this test binary.
func goOnPathVersion(t *testing.T) string {
	t.Helper()
	output, err := runGoCommand(t.TempDir(), "env", "GOVERSION")
	if err != nil {
		t.Fatalf("go env GOVERSION: %v\n%s", err, output)
	}
	return strings.TrimSpace(string(output))
}

// buildShapeFixture compiles the fixture to a native binary with the same go
// build flags as `ef build` (trimpath, readonly modules, debug info kept).
func buildShapeFixture(t *testing.T, source string) (binary string) {
	t.Helper()
	r := CompileFor(source, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "main.go"), application.Main, 0644); err != nil {
		t.Fatal(err)
	}
	binary = filepath.Join(dir, "native")
	if output, err := runGoCommand(dir, "build", "-trimpath", "-mod=readonly", "-o", binary, "."); err != nil {
		t.Fatalf("native build: %v\n%s", err, output)
	}
	return binary
}

// TestVariantMarkersShareShapes is the SH1 control. A variant marker that
// mentions the template's type parameter makes Option[R1] and Option[R2]
// distinct method sets, so Go stencils every runtime generic instantiated over
// an option once per payload type (264 copies at 64 types). Markers without
// type arguments let those instantiations share a shape, leaving a constant
// number of copies independent of the record count.
//
// The count is stable across Go patch releases: stenciling is per GC shape and
// the shape of an interface type argument is fixed by the language, not by an
// optimization; only the Go minor version could change the constant, so the
// test skips outside the Go release line the ceiling was measured on.
func TestVariantMarkersShareShapes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 64-type native binary")
	}
	if version := goOnPathVersion(t); !strings.HasPrefix(version, "go1.27") {
		t.Skipf("shape-sharing ceiling measured on go1.27, the go on PATH is %s", version)
	}
	small, smallSize := stenciledCopyCount(t, shapeFixtureTypes/4)
	copies, size := stenciledCopyCount(t, shapeFixtureTypes)
	t.Logf("types=%d stenciled runtime copies=%d binary bytes=%d; types=%d copies=%d binary bytes=%d", shapeFixtureTypes/4, small, smallSize, shapeFixtureTypes, copies, size)
	const ceiling = 12
	if copies > ceiling {
		t.Fatalf("%d stenciled runtime generic copies for %d option payload types, want at most %d", copies, shapeFixtureTypes, ceiling)
	}
	if copies > small {
		t.Fatalf("stenciled runtime copies grew from %d at %d types to %d at %d: per-type growth is back", small, shapeFixtureTypes/4, copies, shapeFixtureTypes)
	}
}

const optionMismatchSource = `import Data "effra/data"

record R1 {
    name: string
}
record R2 {
    count: i64
}
fn want(o: Data.Option<R1>) -> string {
    match o {
        Data.Option.None => "none",
        Data.Option.Some { value: r } => r.name
    }
}
effect fn main() -> string {
    want(Data.Option.Some { value: R2 { count: 1 } })
}
`

// TestCheckerRefusesMismatchedOptionPayload pins the Effra checker as the type
// authority. Type-argument-free markers make the generated Go accept Some[R2]
// where Option[R1] is expected, so the refusal must come from Effra on both
// targets before any Go is emitted.
func TestCheckerRefusesMismatchedOptionPayload(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(optionMismatchSource, target)
		if r.Checked {
			t.Fatalf("%s: Some<R2> accepted where Option<R1> is expected", target)
		}
		found := false
		for _, d := range r.Diagnostics {
			found = found || (d.Code == "EF106" && strings.Contains(d.Message, "Data.Option<R1>"))
		}
		if !found {
			t.Fatalf("%s: want EF106 naming Data.Option<R1>, got %v", target, r.Diagnostics)
		}
	}
}

const optionTwoInstantiationsSource = `import Data "effra/data"

record R1 {
    name: string
}
record R2 {
    count: i64
}
fn first(o: Data.Option<R1>) -> string {
    match o {
        Data.Option.None => "none1",
        Data.Option.Some { value: r } => r.name
    }
}
fn second(o: Data.Option<R2>) -> string {
    match o {
        Data.Option.None => "none2",
        Data.Option.Some { value: r } => "count"
    }
}
effect fn main() -> void {
    run Console.log(first(Data.Option.Some { value: R1 { name: "a" } })).provide<Console>(Stdout)
    run Console.log(first(Data.Option<R1>.None {})).provide<Console>(Stdout)
    run Console.log(second(Data.Option.Some { value: R2 { count: 2 } })).provide<Console>(Stdout)
    run Console.log(second(Data.Option<R2>.None {})).provide<Console>(Stdout)
}
`

// TestOptionInstantiationsSelectTheirOwnArms runs two instantiations of one
// enum template through the native Go backend: sharing a marker must not blur
// which variant or which payload type a match selects.
func TestOptionInstantiationsSelectTheirOwnArms(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a native binary")
	}
	binary := buildShapeFixture(t, optionTwoInstantiationsSource)
	output, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, output)
	}
	if got, want := string(output), "a\nnone1\ncount\nnone2\n"; got != want {
		t.Fatalf("output %q, want %q", got, want)
	}
}

// TestUnmatchedDynamicVariantPanics pins the backstop the Go type system no
// longer provides once markers carry no type arguments: a dynamic value that
// implements the marker but is none of the enum's variants must not silently
// select the first arm. The value is unreachable from checked Effra, so it is
// injected at the Go level as an extra file of the generated main package.
func TestUnmatchedDynamicVariantPanics(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a native binary")
	}
	r := CompileFor(optionTwoInstantiationsSource, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	marker := regexp.MustCompile(`interface\{ (efVariantTemplate_\w+)\(\) \}`).FindSubmatch(application.Main)
	if marker == nil {
		t.Fatal("no variant marker interface in generated main.go")
	}
	dir := t.TempDir()
	if err = application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":   "module effra.generated\n\ngo 1.27\n",
		"main.go":  string(application.Main),
		"rogue.go": "package main\n\ntype efRogue struct{}\n\nfunc (efRogue) " + string(marker[1]) + "() {}\n\nfunc init() { efFunction_first(efRogue{}) }\n",
	}
	for name, content := range files {
		if err = os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	binary := filepath.Join(dir, "native")
	if output, err := runGoCommand(dir, "build", "-trimpath", "-mod=readonly", "-o", binary, "."); err != nil {
		t.Fatalf("native build: %v\n%s", err, output)
	}
	output, err := exec.Command(binary).CombinedOutput()
	if err == nil || !strings.Contains(string(output), "unreachable enum variant") {
		t.Fatalf("unmatched dynamic variant did not panic: err=%v output=%q", err, output)
	}
}
