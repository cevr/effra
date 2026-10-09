package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"effra.local/prototype/internal/receipt"
)

func TestBuildReceiptIsBuildOnlyAndRefusedBuildsWriteNone(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source := filepath.Join(root, "main.ef")
	if err := os.WriteFile(source, []byte(minimalApplicationSource), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(root, "receipt.json")
	if _, stderr, code := runTestCLIDir(t, binary, root, "", "check", source, "--receipt", receipt); code == 0 || !strings.Contains(string(stderr), "--receipt is only supported by build") {
		t.Fatalf("check accepted --receipt: code=%d stderr=%q", code, stderr)
	}
	broken := filepath.Join(root, "broken.ef")
	if err := os.WriteFile(broken, []byte("effect fn main() -> string {\n    missing()\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		if _, _, code := runTestCLIDir(t, binary, root, "", "build", broken, "--target", target, "--receipt", receipt); code == 0 {
			t.Fatalf("%s build of unchecked source succeeded", target)
		}
		if _, err := os.Stat(receipt); !os.IsNotExist(err) {
			t.Fatalf("%s: refused build wrote a receipt: %v", target, err)
		}
	}
}

func TestBuildReceiptRefusesEmptyPath(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source := filepath.Join(root, "main.ef")
	if err := os.WriteFile(source, []byte(minimalApplicationSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := runTestCLIDir(t, binary, root, "", "build", source, "--receipt", ""); code == 0 || !strings.Contains(string(stderr), "--receipt requires a non-empty path") {
		t.Fatalf("empty receipt path accepted: code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "dist")); !os.IsNotExist(err) {
		t.Fatalf("refused invocation built output: %v", err)
	}
}

// A receipt never replaces the build's source or artifacts, through any
// spelling or alias of their paths, and never lands in the managed
// generated-module tree. Each refusal happens before anything is built.
func TestBuildReceiptRefusesInputsArtifactsAndAliases(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source := filepath.Join(root, "main.ef")
	if err := os.WriteFile(source, []byte(minimalApplicationSource), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(root, "source-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, filepath.Join(root, "source-hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(root, "dir-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "parent", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("parent", "sub"), filepath.Join(root, "jump")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("late", filepath.Join(root, "late-link")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		args    []string
		receipt string
	}{
		{"go executable", []string{"-o", "result"}, "result"},
		{"go executable spelled differently", []string{"-o", "out/result"}, "out/../out/result"},
		{"go default executable", nil, "dist/main"},
		{"absent go executable through a link and ..", []string{"-o", "parent/result"}, "jump/../result"},
		{"go executable below a missing directory and ..", []string{"-o", "result"}, "missing/../result"},
		{"absent go executable through a dangling link", []string{"-o", "late/result"}, "late-link/result"},
		{"source", nil, "main.ef"},
		{"source through a directory link", nil, "dir-link/main.ef"},
		{"source symlink", nil, "source-link"},
		{"source hard link", nil, "source-hardlink"},
		{"managed generations", nil, "dist/go/apps/receipt.json"},
		{"js module", []string{"--target", "js", "-o", "out/app.mjs"}, "out/app.mjs"},
		{"js declaration", []string{"--target", "js", "-o", "out/app.mjs"}, "out/app.d.mts"},
		{"js default module", []string{"--target", "js"}, "dist/main.mjs"},
	}
	for _, c := range cases {
		args := append([]string{"build", source}, c.args...)
		args = append(args, "--receipt", c.receipt)
		_, stderr, code := runTestCLIDir(t, binary, root, "", args...)
		if code == 0 || !strings.Contains(string(stderr), "receipt path") {
			t.Errorf("%s: receipt %s accepted: code=%d stderr=%q", c.name, c.receipt, code, stderr)
		}
	}
	if data, err := os.ReadFile(source); err != nil || string(data) != string(original) {
		t.Fatalf("source changed: %v %q", err, data)
	}
	for _, built := range []string{"dist", "out", "result", "parent/result", "late", "missing"} {
		if _, err := os.Stat(filepath.Join(root, built)); !os.IsNotExist(err) {
			t.Errorf("refused build wrote %s: %v", built, err)
		}
	}

	// A distinct path is published whole, and an existing symbolic link at
	// that path is replaced rather than written through.
	victim := filepath.Join(root, "victim.txt")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "receipt.json")); err != nil {
		t.Fatal(err)
	}
	// The build root is a repository: its revision must not reach the
	// generated module's executable.
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, output)
	}
	if stdout, stderr, code := runTestCLIDir(t, binary, root, "", "build", source, "-o", "result", "--receipt", "receipt.json"); code != 0 {
		t.Fatalf("build failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if data, err := os.ReadFile(victim); err != nil || string(data) != "keep" {
		t.Fatalf("receipt wrote through a symbolic link: %v %q", err, data)
	}
	written := readReceipt(t, filepath.Join(root, "receipt.json"))
	if written.Binary == nil || written.Binary.Build == nil || written.Binary.Build.Settings["-trimpath"] != "true" {
		t.Fatalf("receipt lacks the executable's build information: %+v", written.Binary)
	}
	if _, stamped := written.Binary.Build.Settings["vcs"]; stamped {
		t.Fatalf("executable carries VCS stamping: %+v", written.Binary.Build.Settings)
	}
	if written.Toolchain == nil || written.Toolchain.Env["GOVERSION"] == "" || written.Binary.Build.GoVersion != written.Toolchain.Env["GOVERSION"] {
		t.Fatalf("receipt toolchain %+v does not match the executable's Go version", written.Toolchain)
	}
	if _, found := written.Toolchain.Env["GOFLAGS"]; !found {
		t.Fatalf("receipt does not record effective GOFLAGS: %+v", written.Toolchain.Env)
	}
}

// The JavaScript receipt lists the imports emission declared: program text
// that spells an import adds nothing and refuses nothing, and the HTTP
// transport's host module is listed.
func TestJSReceiptListsDeclaredImportsOnly(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	programs := map[string]string{
		"spelled.ef": "effect fn main() -> string {\n    \"import('node:http')\"\n}\n",
		"partial.ef": "effect fn main() -> string {\n    \"import(\"\n}\n",
		"http.ef":    httpApplicationSource,
	}
	for name, text := range programs {
		source := filepath.Join(root, name)
		if err := os.WriteFile(source, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, name+".receipt.json")
		if stdout, stderr, code := runTestCLIDir(t, binary, root, "", "build", source, "--target", "js", "--entry", "-o", filepath.Join(root, "out", name+".mjs"), "--receipt", path); code != 0 {
			t.Fatalf("%s: build failed: code=%d stdout=%q stderr=%q", name, code, stdout, stderr)
		}
		specifiers := []string{}
		for _, imported := range readReceipt(t, path).External {
			specifiers = append(specifiers, imported.Specifier)
		}
		want := "effect"
		if name == "http.ef" {
			want = "effect node:http"
		}
		if got := strings.Join(specifiers, " "); got != want {
			t.Errorf("%s: external modules %q, want %q", name, got, want)
		}
	}
}

// runReceiptCLI runs the CLI in directory with extra environment entries.
func runReceiptCLI(t *testing.T, binary, directory string, environment []string, args ...string) ([]byte, []byte, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Env = append(os.Environ(), environment...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout.Bytes(), stderr.Bytes(), exit.ExitCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	return stdout.Bytes(), stderr.Bytes(), 0
}

// Embedded build information omits CGO flags under -trimpath, so the
// receipt records the C toolchain inputs of a cgo build itself: differing
// CGO flags differ in the receipt, the cgo packages are listed and the C
// compiler is identified. With cgo disabled none of that applies.
func TestBuildReceiptRecordsCgoInputs(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("no C compiler")
	}
	binary := buildTestCLI(t)
	root := t.TempDir()
	source := filepath.Join(root, "http.ef")
	if err := os.WriteFile(source, []byte(httpApplicationSource), 0o600); err != nil {
		t.Fatal(err)
	}
	build := func(name string, environment ...string) receipt.Application {
		t.Helper()
		path := filepath.Join(root, name+".json")
		if stdout, stderr, code := runReceiptCLI(t, binary, root, append([]string{"CC=gcc", "GOFLAGS="}, environment...), "build", source, "-o", filepath.Join(root, name), "--receipt", path); code != 0 {
			t.Fatalf("%s: build failed: code=%d stdout=%q stderr=%q", name, code, stdout, stderr)
		}
		return readReceipt(t, path)
	}
	first := build("first", "CGO_ENABLED=1", "CGO_CFLAGS=-O2 -g -DEFFRA_RECEIPT_FIRST")
	second := build("second", "CGO_ENABLED=1", "CGO_CFLAGS=-O2 -g -DEFFRA_RECEIPT_SECOND")
	pure := build("pure", "CGO_ENABLED=0")

	if _, embedded := first.Binary.Build.Settings["CGO_CFLAGS"]; embedded {
		t.Fatalf("premise changed: build information records CGO_CFLAGS under -trimpath: %+v", first.Binary.Build.Settings)
	}
	if !strings.Contains(first.Toolchain.Env["CGO_CFLAGS"], "EFFRA_RECEIPT_FIRST") || !strings.Contains(second.Toolchain.Env["CGO_CFLAGS"], "EFFRA_RECEIPT_SECOND") {
		t.Fatalf("CGO flags not recorded: %q %q", first.Toolchain.Env["CGO_CFLAGS"], second.Toolchain.Env["CGO_CFLAGS"])
	}
	for _, key := range []string{"CC", "CXX", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS"} {
		if _, found := first.Toolchain.Env[key]; !found {
			t.Errorf("receipt does not record %s", key)
		}
	}
	if !slices.Contains(first.Deps.Cgo, "net") || !slices.Contains(first.Deps.Cgo, "runtime/cgo") {
		t.Fatalf("cgo packages not listed: %v", first.Deps.Cgo)
	}
	if len(first.Toolchain.CCompilers) == 0 {
		t.Fatal("C compiler not identified")
	}
	compiler := first.Toolchain.CCompilers[0]
	if compiler.Variable != "CC" || compiler.Command != "gcc" || len(compiler.Arguments) != 0 || !filepath.IsAbs(compiler.Program.Path) || len(compiler.Program.SHA256) != 64 || compiler.Underlying != nil || compiler.Version == "" {
		t.Fatalf("incomplete C compiler identity: %+v", compiler)
	}

	// The go command splits compiler settings with its quoting rules and
	// runs any leading arguments: a quoted program is gcc itself, and a
	// wrapper is recorded beside the compiler it runs.
	quoted := build("quoted", "CGO_ENABLED=1", `CC="gcc"`)
	if got := quoted.Toolchain.CCompilers; len(got) != 1 || got[0].Command != `"gcc"` || got[0].Program != compiler.Program || got[0].Version != compiler.Version {
		t.Fatalf("quoted CC measured as %+v, want program %+v", got, compiler.Program)
	}
	wrapped := build("wrapped", "CGO_ENABLED=1", "CC=env gcc")
	if got := wrapped.Toolchain.CCompilers; len(got) != 1 || got[0].Program.Name != "env" || !slices.Equal(got[0].Arguments, []string{"gcc"}) || got[0].Underlying == nil || *got[0].Underlying != compiler.Program || got[0].Version != compiler.Version {
		t.Fatalf("wrapped CC measured as %+v (underlying %+v), want env running %+v", got, got[0].Underlying, compiler.Program)
	}
	if pure.Toolchain.Env["CGO_ENABLED"] != "0" || len(pure.Deps.Cgo) != 0 || len(pure.Toolchain.CCompilers) != 0 {
		t.Fatalf("pure build reports cgo inputs: enabled=%q packages=%v compilers=%+v", pure.Toolchain.Env["CGO_ENABLED"], pure.Deps.Cgo, pure.Toolchain.CCompilers)
	}
}
