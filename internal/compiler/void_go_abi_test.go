package compiler

import (
	"go/ast"
	goformat "go/format"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const concreteVoidAndGenericSource = `
record Holder<T: type> { callback: fn() -> T }
fn finish() -> void { void }
fn selected() -> Holder<void> { Holder<void> { callback: finish } }
fn invokeDirect(callback: fn() -> void) -> void { callback() }
fn invoke(holder: Holder<void>) -> void { holder.callback() }
fn caller() -> void { invokeDirect(finish); invoke(selected()); void }
effect fn main() -> void { caller(); void }
`

func generatedGoFunction(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("generated function %s not found", name)
	return nil
}

func formatGoNode(t *testing.T, node ast.Node) string {
	t.Helper()
	var output strings.Builder
	if err := goformat.Node(&output, gotoken.NewFileSet(), node); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestConcreteVoidGoDeclarationsAndGenericCarrierBridge(t *testing.T) {
	r := CompileFor(concreteVoidAndGenericSource, "go")
	if !r.Checked {
		t.Fatalf("void ABI source rejected: %+v", r.Diagnostics)
	}
	generated, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "generated.go", generated, 0)
	if err != nil {
		t.Fatalf("generated Go did not parse: %v\n%s", err, generated)
	}

	finish := generatedGoFunction(t, file, "efFunction_finish")
	if finish.Type.Results != nil {
		t.Fatalf("concrete pure void function retained a Go result: %s", formatGoNode(t, finish.Type))
	}
	invoke := generatedGoFunction(t, file, "efFunction_invokeDirect")
	if invoke.Type.Results != nil || invoke.Type.Params == nil || len(invoke.Type.Params.List) != 1 {
		t.Fatalf("concrete callback declaration retained a result carrier: %s", formatGoNode(t, invoke.Type))
	}
	callbackType, ok := invoke.Type.Params.List[0].Type.(*ast.FuncType)
	if !ok || callbackType.Results != nil {
		t.Fatalf("void callback parameter was not a no-result Go function: %s", formatGoNode(t, invoke.Type.Params.List[0].Type))
	}
	main := generatedGoFunction(t, file, "efFunction_main")
	if main.Type.Results == nil || !strings.Contains(formatGoNode(t, main.Type), "efEffect[struct{}]") {
		t.Fatalf("effectful void function lost its runtime carrier: %s", formatGoNode(t, main.Type))
	}
	var holder *ast.StructType
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "type" {
			continue
		}
		for _, specification := range gen.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if ok && typeSpec.Name.Name == "efTemplate_Holder" {
				holder, _ = typeSpec.Type.(*ast.StructType)
			}
		}
	}
	if holder == nil || len(holder.Fields.List) != 1 {
		t.Fatalf("generic callable carrier declaration was not emitted: %s", generated)
	}
	genericCallback, ok := holder.Fields.List[0].Type.(*ast.FuncType)
	if !ok || genericCallback.Results == nil || len(genericCallback.Results.List) != 1 {
		t.Fatalf("generic callable field was eagerly specialized to no-result: %s", formatGoNode(t, holder.Fields.List[0].Type))
	}

	if !strings.Contains(generated, "func() struct{}") || !strings.Contains(generated, "efTemp1 := efFunction_finish") {
		t.Fatalf("generic void callback bridge was not emitted: %s", generated)
	}
	if strings.Contains(generated, "func efFunction_finish() struct{}") || strings.Contains(generated, "callback func() struct{}") {
		t.Fatalf("concrete void ABI regressed to a struct carrier: %s", generated)
	}

	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(generated), 0644); err != nil {
		t.Fatal(err)
	}
	probe := `package main

import "testing"

func TestConcreteVoidExecution(t *testing.T) {
	calls := 0
	efFunction_invokeDirect(func() { calls++ })
	if calls != 1 {
		t.Fatalf("no-result callback was not invoked exactly once: %d", calls)
	}
	efFunction_invoke(efFunction_selected())
}
`
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(probe), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := runGoCommand(dir, "test", "-race", "."); err != nil {
		t.Fatalf("generated concrete void ABI did not compile and execute: %v\n%s\n%s", err, output, generated)
	}
	binary := filepath.Join(dir, "native")
	if output, err := runGoCommand(dir, "build", "-o", binary, "."); err != nil {
		t.Fatalf("generated concrete void ABI did not build: %v\n%s\n%s", err, output, generated)
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("generated concrete void executable failed: %v\n%s", err, output)
	}
}
