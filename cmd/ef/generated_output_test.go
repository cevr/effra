package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/compiler"
	rt "effra.local/prototype/runtime/effra"
)

func TestResolveSourceOriginMatchesAdmittedPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "jump")); err != nil {
		t.Fatal(err)
	}
	resolvedSource := `effect fn main() -> string { "resolved" }
`
	lexicalSource := `effect fn main() -> string { "lexical" }
`
	resolvedPath := filepath.Join(root, "real", "origin.ef")
	lexicalPath := filepath.Join(root, "origin.ef")
	if err := os.WriteFile(resolvedPath, []byte(resolvedSource), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lexicalPath, []byte(lexicalSource), 0600); err != nil {
		t.Fatal(err)
	}

	t.Chdir(root)
	r, origin, err := loadWithOrigin("jump/origin.ef", "go")
	if err != nil {
		t.Fatal(err)
	}
	wantOrigin, err := filepath.Abs(resolvedPath)
	if err != nil {
		t.Fatal(err)
	}
	if origin != wantOrigin || r.Revision != revisionForSource(resolvedSource) {
		t.Fatalf("symlinked admitted path resolved incorrectly: origin=%q want=%q revision=%q", origin, wantOrigin, r.Revision)
	}

	// The established loader admits filepath.Abs(path), which lexically cleans
	// this spelling before reading it. Origin identity must describe those
	// admitted bytes rather than the different file selected by raw-path OS
	// walking through the symlink.
	logicalPath := "jump/../origin.ef"
	r, origin, err = loadWithOrigin(logicalPath, "go")
	if err != nil {
		t.Fatal(err)
	}
	wantLexicalOrigin, err := filepath.Abs(lexicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if origin != wantLexicalOrigin || r.Revision != revisionForSource(lexicalSource) {
		t.Fatalf("logical admitted path resolved incorrectly: origin=%q want=%q revision=%q", origin, wantLexicalOrigin, r.Revision)
	}

	absPath := root + string(os.PathSeparator) + "jump" + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "origin.ef"
	_, absoluteOrigin, err := loadWithOrigin(absPath, "go")
	if err != nil {
		t.Fatal(err)
	}
	if absoluteOrigin != wantLexicalOrigin {
		t.Fatalf("absolute symlink/.. admitted path resolved incorrectly: origin=%q want=%q", absoluteOrigin, wantLexicalOrigin)
	}
}

func TestGoBuildOwnsCompleteGenerationAndPreservesLegacyOutput(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	sourcePath := filepath.Join(root, "one", "main.ef")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
		t.Fatal(err)
	}
	source := `effect fn main() -> string { "owned generation" }
effect fn test_generation() -> () raises {AssertionFailed} uses {Assert} {
    run Assert.check(true, "generation")
}
`
	if err := os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	secondSourcePath := filepath.Join(root, "two", "main.ef")
	if err := os.MkdirAll(filepath.Dir(secondSourcePath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondSourcePath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	legacyRuntime := filepath.Join(root, "dist", "go", "runtime")
	if err := os.MkdirAll(legacyRuntime, 0700); err != nil {
		t.Fatal(err)
	}
	legacyStdlib := []byte("package runtime\n// preserved legacy source\n")
	if err := os.WriteFile(filepath.Join(legacyRuntime, "stdlib.go"), legacyStdlib, 0600); err != nil {
		t.Fatal(err)
	}
	retiredLegacy := []byte("package runtime\n// retired legacy source\n")
	if err := os.WriteFile(filepath.Join(legacyRuntime, "retired_legacy.go"), retiredLegacy, 0600); err != nil {
		t.Fatal(err)
	}
	legacyModule := []byte("module legacy.generated\n\ngo 1.27\n")
	if err := os.WriteFile(filepath.Join(root, "dist", "go", "go.mod"), legacyModule, 0600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := runTestCLIDir(t, binary, root, "", "build", sourcePath, "-o", filepath.Join("dist", "one"))
	if code != 0 || len(stderr) != 0 || !strings.Contains(string(stdout), filepath.Join("dist", "one")) {
		t.Fatalf("owned Go build failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	runOutput, err := os.ReadFile(filepath.Join(root, "dist", "one"))
	if err != nil || string(runOutput) == "" {
		t.Fatalf("owned executable was not written: err=%v bytes=%d", err, len(runOutput))
	}
	if got, err := os.ReadFile(filepath.Join(legacyRuntime, "stdlib.go")); err != nil || !bytes.Equal(got, legacyStdlib) {
		t.Fatalf("legacy runtime was changed: err=%v bytes=%q", err, got)
	}
	if got, err := os.ReadFile(filepath.Join(root, "dist", "go", "go.mod")); err != nil || !bytes.Equal(got, legacyModule) {
		t.Fatalf("legacy module graph was changed: err=%v bytes=%q", err, got)
	}

	apps := filepath.Join(root, "dist", "go", "apps")
	appDirectories := generatedApplicationDirectories(t, apps)
	if len(appDirectories) != 1 {
		t.Fatalf("expected one ordinary application identity, got %d", len(appDirectories))
	}
	ordinaryGeneration := generatedGenerationDirectory(t, appDirectories[0])
	if _, err := os.Stat(filepath.Join(ordinaryGeneration, "runtime", "retired_legacy.go")); !os.IsNotExist(err) {
		t.Fatalf("retired legacy source remained in the new generation: %v", err)
	}
	for name := range rt.Sources() {
		if _, err := os.Stat(filepath.Join(ordinaryGeneration, "runtime", name)); err != nil {
			t.Fatalf("new runtime source set omitted %s: %v", name, err)
		}
	}

	stdout, stderr, code = runTestCLIDir(t, binary, root, "", "build", secondSourcePath, "-o", filepath.Join("dist", "two"))
	if code != 0 || len(stderr) != 0 || !strings.Contains(string(stdout), filepath.Join("dist", "two")) {
		t.Fatalf("same-basename second Go build failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if got := generatedApplicationDirectories(t, apps); len(got) != 2 {
		t.Fatalf("same-basename source origins shared generated identity: %d", len(got))
	}

	stdout, stderr, code = runTestCLIDir(t, binary, root, "", "build", sourcePath, "-o", filepath.Join("dist", "one-again"))
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("unchanged owned build was not reusable after a second origin: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if got := generatedApplicationDirectories(t, apps); len(got) != 2 {
		t.Fatalf("unchanged build created another application identity: %d", len(got))
	}

	stdout, stderr, code = runTestCLIDir(t, binary, root, "", "test", sourcePath)
	if code != 0 || len(stderr) != 0 || !strings.Contains(string(stdout), `"passed": true`) {
		t.Fatalf("test-mode generation failed: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if got := generatedApplicationDirectories(t, apps); len(got) != 3 {
		t.Fatalf("ordinary and test builds shared generated identity: %d", len(got))
	}

	modified := filepath.Join(ordinaryGeneration, "runtime", "effect.go")
	if err := os.WriteFile(modified, []byte("modified generated source"), 0600); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runTestCLIDir(t, binary, root, "", "build", sourcePath, "-o", filepath.Join("dist", "one-modified"))
	if code == 0 || !strings.Contains(string(stderr), "modified") {
		t.Fatalf("modified generation was admitted: code=%d stderr=%q", code, stderr)
	}
	if got, err := os.ReadFile(modified); err != nil || string(got) != "modified generated source" {
		t.Fatalf("modified generation was overwritten after refusal: err=%v bytes=%q", err, got)
	}
}

func revisionForSource(source string) string {
	return compiler.CompileAt(source, "go", ".").Revision
}

func generatedApplicationDirectories(t *testing.T, apps string) []string {
	t.Helper()
	entries, err := os.ReadDir(apps)
	if err != nil {
		t.Fatal(err)
	}
	directories := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			directories = append(directories, filepath.Join(apps, entry.Name()))
		}
	}
	return directories
}

func generatedGenerationDirectory(t *testing.T, application string) string {
	t.Helper()
	commits := filepath.Join(application, "commits")
	entries, err := os.ReadDir(commits)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".commit") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(commits, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var commit struct {
			Directory string `json:"directory"`
		}
		if err := json.Unmarshal(data, &commit); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(application, "generations", commit.Directory)
	}
	t.Fatalf("application has no committed generation: %s", application)
	return ""
}
