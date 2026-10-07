package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
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
effect fn test_generation() -> void raises {AssertionFailed} uses {Assert} {
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
	conflictingWorkspace := filepath.Join(root, "conflicting")
	if err := os.MkdirAll(conflictingWorkspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflictingWorkspace, "go.mod"), []byte("module effra.generated\n\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.work"), []byte("go 1.27\n\nuse ./conflicting\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOWORK", "auto")
	t.Setenv("GOFLAGS", "-mod=mod")

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
	ordinaryApplication := appDirectories[0]
	ordinaryGeneration := generatedGenerationDirectory(t, ordinaryApplication)
	ordinaryFiles := generationFileStates(t, ordinaryGeneration)
	ordinaryCommitCount := generationCommitCount(t, filepath.Join(ordinaryApplication, "commits"))
	withoutWorkspaceOverride := exec.Command("go", "build", "-trimpath", "-mod=readonly", "-o", filepath.Join(root, "dist", "without-workspace-override"), ".")
	withoutWorkspaceOverride.Dir = ordinaryGeneration
	withoutWorkspaceOverride.Env = os.Environ()
	if err := withoutWorkspaceOverride.Run(); err == nil {
		t.Fatal("conflicting inherited go.work did not exercise the GOWORK=off child boundary")
	}
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
	if got := generationCommitCount(t, filepath.Join(ordinaryApplication, "commits")); got != ordinaryCommitCount {
		t.Fatalf("unchanged build created another commit marker: %d", got)
	}
	if got := generatedGenerationDirectory(t, ordinaryApplication); got != ordinaryGeneration {
		t.Fatalf("unchanged build selected a different generation: got=%q want=%q", got, ordinaryGeneration)
	}
	assertGenerationFileStatesUnchanged(t, ordinaryGeneration, ordinaryFiles)

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

func TestNativeGoBuildRejectsModuleMutationDespiteInheritedFlags(t *testing.T) {
	root := t.TempDir()
	dependency := filepath.Join(root, "dependency")
	if err := os.Mkdir(dependency, 0700); err != nil {
		t.Fatal(err)
	}
	module := []byte("module effra.generated\n\ngo 1.27\n\nreplace example.test/dependency => ./dependency\n")
	for path, data := range map[string][]byte{
		"go.mod":                   module,
		"main.go":                  []byte("package main\nimport \"example.test/dependency\"\nfunc main() { dependency.Value() }\n"),
		"dependency/go.mod":        []byte("module example.test/dependency\n\ngo 1.27\n"),
		"dependency/dependency.go": []byte("package dependency\nfunc Value() {}\n"),
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOFLAGS", "-mod=mod")
	before := generationFileStates(t, root)
	output := filepath.Join(t.TempDir(), "program")
	child := nativeGoBuildCommand(root, output)
	if data, err := child.CombinedOutput(); err == nil || !strings.Contains(string(data), "replaced but not required") {
		t.Fatalf("native child admitted a missing module requirement: err=%v output=%s", err, data)
	}
	assertGenerationFileStatesUnchanged(t, root, before)
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("refused native build wrote an executable: %v", err)
	}

	// The same graph is buildable with the inherited flags alone, which adds
	// the missing requirement. This makes the explicit readonly flag causal.
	unprotected := exec.Command("go", "build", "-trimpath", "-o", output, ".")
	unprotected.Dir = root
	unprotected.Env = replaceEnv(os.Environ(), "GOWORK", "off")
	if data, err := unprotected.CombinedOutput(); err != nil {
		t.Fatalf("unprotected control did not build the local dependency: err=%v output=%s", err, data)
	}
	after, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(module, after) || !bytes.Contains(after, []byte("require example.test/dependency")) {
		t.Fatalf("unprotected control did not mutate the module requirement: %s", after)
	}
}

func generationCommitCount(t *testing.T, commits string) int {
	t.Helper()
	entries, err := os.ReadDir(commits)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".commit") {
			count++
		}
	}
	return count
}

type generatedFileState struct {
	bytes   []byte
	modTime int64
}

func generationFileStates(t *testing.T, directory string) map[string]generatedFileState {
	t.Helper()
	states := map[string]generatedFileState{}
	var walk func(string, string)
	walk = func(current, relative string) {
		entries, err := os.ReadDir(current)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				t.Fatalf("unexpected generation symlink %s", entry.Name())
			}
			path := entry.Name()
			if relative != "" {
				path = filepath.ToSlash(filepath.Join(relative, entry.Name()))
			}
			fullPath := filepath.Join(current, entry.Name())
			if entry.IsDir() {
				walk(fullPath, path)
				continue
			}
			info, err := entry.Info()
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() {
				t.Fatalf("unexpected generation file type %s", fullPath)
			}
			data, err := os.ReadFile(fullPath)
			if err != nil {
				t.Fatal(err)
			}
			states[path] = generatedFileState{bytes: data, modTime: info.ModTime().UnixNano()}
		}
	}
	walk(directory, "")
	return states
}

func assertGenerationFileStatesUnchanged(t *testing.T, directory string, before map[string]generatedFileState) {
	t.Helper()
	after := generationFileStates(t, directory)
	if len(after) != len(before) {
		t.Fatalf("generation file count changed: before=%d after=%d", len(before), len(after))
	}
	for path, expected := range before {
		actual, ok := after[path]
		if !ok {
			t.Fatalf("generation file disappeared: %s", path)
		}
		if !bytes.Equal(actual.bytes, expected.bytes) || actual.modTime != expected.modTime {
			t.Fatalf("generation file changed: %s", path)
		}
	}
}
