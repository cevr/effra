package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	cases := []struct {
		name    string
		args    []string
		receipt string
	}{
		{"go executable", []string{"-o", "result"}, "result"},
		{"go executable spelled differently", []string{"-o", "out/result"}, "out/../out/result"},
		{"go default executable", nil, "dist/main"},
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
	for _, built := range []string{"dist", "out", "result"} {
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
