package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"effra.local/prototype/internal/compiler"
)

const httpApplicationSource = `effect fn route(path: string) -> string {
    "served " + path
}
effect fn main() -> void raises { IoError } {
    run Http.serve("127.0.0.1:0", route).provide<Http>(GoHttp)
}
`

const minimalApplicationSource = `effect fn main() -> string {
    "minimal"
}
`

// generationCommits maps each commit record of one application to its
// published generation directory.
func generationCommits(t *testing.T, application string) map[string]string {
	t.Helper()
	commits := filepath.Join(application, "commits")
	entries, err := os.ReadDir(commits)
	if err != nil {
		t.Fatal(err)
	}
	generations := map[string]string{}
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
		generations[entry.Name()] = filepath.Join(application, "generations", commit.Directory)
	}
	return generations
}

// newGeneration returns the one generation published since before.
func newGeneration(t *testing.T, application string, before map[string]string) (string, string) {
	t.Helper()
	added := []string{}
	after := generationCommits(t, application)
	for commit := range after {
		if _, found := before[commit]; !found {
			added = append(added, commit)
		}
	}
	if len(added) != 1 {
		t.Fatalf("expected one new generation, got %v", added)
	}
	return added[0], after[added[0]]
}

func generationImports(t *testing.T, generation string) []string {
	t.Helper()
	imports := map[string]bool{}
	for _, pattern := range []string{"*.go", "runtime/*.go"} {
		files, err := filepath.Glob(filepath.Join(generation, pattern))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				imports[path] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(imports))
}

// generationDependencies lists the transitive Go build dependencies of a
// generated module as resolved by the Go toolchain itself.
func generationDependencies(t *testing.T, generation string) []string {
	t.Helper()
	command := exec.Command("go", "list", "-deps", "-mod=readonly", ".")
	command.Dir = generation
	command.Env = replaceEnv(replaceEnv(os.Environ(), "GOWORK", "off"), "GOFLAGS", "")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, output)
	}
	return strings.Fields(string(output))
}

func generationBytes(t *testing.T, generation string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for path, state := range generationFileStates(t, generation) {
		files[path] = state.bytes
	}
	return files
}

// serveOnce starts a built HTTP application, requests one path and stops it
// through its managed SIGINT shutdown.
func serveOnce(t *testing.T, binary, path string) string {
	t.Helper()
	command := exec.Command(binary)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Signal(syscall.SIGINT)
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = command.Process.Kill()
			t.Errorf("HTTP application did not stop after SIGINT: %s", stderr.String())
		}
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "listening http://") {
		t.Fatalf("HTTP application did not announce its address: %q %v %s", line, err, stderr.String())
	}
	client := http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(strings.TrimSpace(strings.TrimPrefix(line, "listening ")) + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestGoBuildSelectsRuntimeAndReconcilesShrinkingRebuild(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	source := filepath.Join(root, "app", "main.ef")
	other := filepath.Join(root, "other", "main.ef")
	for path, text := range map[string]string{source: httpApplicationSource, other: httpApplicationSource} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	build := func(directory, path, output string) {
		t.Helper()
		stdout, stderr, code := runTestCLIDir(t, binary, directory, "", "build", path, "-o", output)
		if code != 0 {
			t.Fatalf("build %s failed: code=%d stdout=%q stderr=%q", path, code, stdout, stderr)
		}
	}
	apps := filepath.Join(root, "dist", "go", "apps")

	// A separately generated HTTP application.
	build(root, other, filepath.Join("dist", "other"))
	otherApplications := generatedApplicationDirectories(t, apps)
	if len(otherApplications) != 1 {
		t.Fatalf("expected one application, got %v", otherApplications)
	}
	otherApplication := otherApplications[0]
	_, otherGeneration := newGeneration(t, otherApplication, map[string]string{})
	otherFiles := generationFileStates(t, otherGeneration)

	// HTTP first, into the managed output of one source origin.
	build(root, source, filepath.Join("dist", "app-http"))
	var application string
	for _, directory := range generatedApplicationDirectories(t, apps) {
		if directory != otherApplication {
			application = directory
		}
	}
	httpCommit, httpGeneration := newGeneration(t, application, map[string]string{})
	httpFiles := generationFileStates(t, httpGeneration)
	if got, want := generationRuntimeFiles(t, httpGeneration), []string{"effect.go", "fiber.go", "http.go", "managed.go", "scheduler.go", "scope.go"}; !slices.Equal(got, want) {
		t.Fatalf("HTTP runtime = %v, want %v", got, want)
	}
	if !slices.Contains(generationDependencies(t, httpGeneration), "net/http") {
		t.Fatal("HTTP generation does not depend on net/http")
	}
	if body := serveOnce(t, filepath.Join(root, "dist", "app-http"), "/hello"); body != "served /hello" {
		t.Fatalf("HTTP application served %q", body)
	}

	// Then the same origin shrinks to a minimal managed program.
	if err := os.WriteFile(source, []byte(minimalApplicationSource), 0o600); err != nil {
		t.Fatal(err)
	}
	before := generationCommits(t, application)
	build(root, source, filepath.Join("dist", "app-minimal"))
	minimalCommit, minimalGeneration := newGeneration(t, application, before)
	if got, want := generationRuntimeFiles(t, minimalGeneration), []string{"effect.go", "fiber.go", "managed.go", "scheduler.go", "scope.go"}; !slices.Equal(got, want) {
		t.Fatalf("minimal runtime after HTTP = %v, want the core selection %v", got, want)
	}
	if imports := generationImports(t, minimalGeneration); slices.Contains(imports, "net/http") {
		t.Fatalf("minimal generation imports net/http: %v", imports)
	}
	if dependencies := generationDependencies(t, minimalGeneration); slices.Contains(dependencies, "net/http") {
		t.Fatalf("minimal generation still depends on net/http: %v", dependencies)
	}
	run := exec.Command(filepath.Join(root, "dist", "app-minimal"))
	if output, err := run.CombinedOutput(); err != nil || string(output) != "minimal\n" {
		t.Fatalf("minimal rebuild did not run: %v %q", err, output)
	}

	// A fresh minimal build of the same origin publishes the identical
	// generation: same identity, files and bytes.
	fresh := t.TempDir()
	build(fresh, source, filepath.Join("dist", "app-minimal"))
	freshApplications := generatedApplicationDirectories(t, filepath.Join(fresh, "dist", "go", "apps"))
	if len(freshApplications) != 1 || filepath.Base(freshApplications[0]) != filepath.Base(application) {
		t.Fatalf("fresh build application identity = %v, want %s", freshApplications, filepath.Base(application))
	}
	freshCommits := generationCommits(t, freshApplications[0])
	freshGeneration, found := freshCommits[minimalCommit]
	if len(freshCommits) != 1 || !found {
		t.Fatalf("fresh build generation = %v, want %s", slices.Collect(maps.Keys(freshCommits)), minimalCommit)
	}
	if got, want := generationBytes(t, freshGeneration), generationBytes(t, minimalGeneration); !maps.EqualFunc(got, want, bytes.Equal) {
		t.Fatalf("fresh minimal generation differs from the rebuilt one: %v vs %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}

	// Previously published generations stay immutable, and the separate HTTP
	// application keeps its own coherent runtime and is reused unchanged.
	assertGenerationFileStatesUnchanged(t, httpGeneration, httpFiles)
	if commits := generationCommits(t, application); commits[httpCommit] != httpGeneration {
		t.Fatalf("HTTP generation commit was replaced: %v", commits)
	}
	build(root, other, filepath.Join("dist", "other-again"))
	if commits := generationCommits(t, otherApplication); len(commits) != 1 {
		t.Fatalf("unchanged separate application published again: %v", commits)
	}
	assertGenerationFileStatesUnchanged(t, otherGeneration, otherFiles)
	if body := serveOnce(t, filepath.Join(root, "dist", "other-again"), "/still"); body != "served /still" {
		t.Fatalf("separate HTTP application served %q", body)
	}
}

func TestGoBuildReportsExhaustedApplicationPlanAsDiagnostic(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	// Every statement costs one unit of plan work, so this checked source
	// exceeds the 2^20 application closure budget.
	source := filepath.Join(root, "exhausted.ef")
	if err := os.WriteFile(source, []byte("effect fn main() -> void {\n"+strings.Repeat("1\n", 1<<20+64)+"void\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"build"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := runTestCLIDir(t, binary, root, "", command, source)
			var report struct {
				Checked     bool                  `json:"checked"`
				Diagnostics []compiler.Diagnostic `json:"diagnostics"`
			}
			if err := json.Unmarshal(stdout, &report); err != nil {
				t.Fatalf("no JSON report: %v stdout=%q stderr=%q", err, stdout, stderr)
			}
			if !report.Checked {
				t.Fatalf("exhaustion fixture did not check: %+v", report.Diagnostics)
			}
			diagnostics := report.Diagnostics
			if code == 0 || !strings.Contains(string(stderr), "EF136") {
				t.Fatalf("exhausted build succeeded: code=%d stderr=%q", code, stderr)
			}
			if len(diagnostics) != 1 || diagnostics[0].Code != "EF136" {
				t.Fatalf("%s did not report EF136: %+v", command, diagnostics)
			}
		})
	}
	t.Cleanup(func() {
		if _, err := os.Stat(filepath.Join(root, "dist", "go", "apps")); !os.IsNotExist(err) {
			t.Errorf("refused application published output: %v", err)
		}
	})
}
