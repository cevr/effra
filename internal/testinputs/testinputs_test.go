package testinputs

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeclaredInputsInvalidateCachedResults drives cmd/go over a fixture
// module whose TestMain uses this package's source, proving that editing a
// declared dependency or the tool digest misses Go's test cache.
func TestDeclaredInputsInvalidateCachedResults(t *testing.T) {
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is unavailable")
	}
	source, err := os.ReadFile("testinputs.go")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module fixture\n\ngo 1.22\n")
	write("testinputs/testinputs.go", string(source))
	write("probe/probe_test.go", `package probe

import (
	"os"
	"testing"

	"fixture/testinputs"
)

func TestMain(m *testing.M) { os.Exit(testinputs.Run(m, "..")) }

func TestProbe(t *testing.T) {}
`)
	write("node_modules/.store/dep@1/index.js", "export const one = 1\n")
	if err := os.Symlink(".store/dep@1", filepath.Join(root, "node_modules/dep")); err != nil {
		t.Fatal(err)
	}
	cached := func(digest string) bool {
		t.Helper()
		command := exec.Command(gobin, "test", "./probe")
		command.Dir = root
		command.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off", "GOPROXY=off", "GOTOOLCHAIN=local", ToolDigest+"="+digest)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("go test: %v\n%s", err, output)
		}
		return strings.Contains(string(output), "(cached)")
	}
	cached("one")
	if !cached("one") {
		t.Fatal("an unchanged run was not cached; the fixture cannot observe invalidation")
	}
	write("node_modules/.store/dep@1/index.js", "export const one = 2 // edited\n")
	if cached("one") {
		t.Fatal("an edited dependency behind a symlink replayed the cached result")
	}
	write("node_modules/.store/dep@1/added.js", "export {}\n")
	if cached("one") {
		t.Fatal("an added dependency file replayed the cached result")
	}
	if cached("two") {
		t.Fatal("a changed tool digest replayed the cached result")
	}
	if !cached("two") {
		t.Fatal("an unchanged run was not cached after invalidation")
	}
}

func TestEntriesDeclareEachResolvedDirectoryOnce(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "node_modules/.store/a/lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".store/a", filepath.Join(root, "node_modules/a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(root, "node_modules/.store/a/lib/cycle")); err != nil {
		t.Fatal(err)
	}
	entries, err := Entries(filepath.Join(root, "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	opens := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry, "open ") {
			opens++
		}
	}
	// node_modules, .store, .store/a and .store/a/lib: the link to a and the
	// cycle back to it resolve to directories already declared.
	if opens != 4 || entries[0] != "getenv "+ToolDigest+"\n" {
		t.Fatalf("entries = %q", entries)
	}
	absent, err := Entries(filepath.Join(root, "missing"))
	if err != nil || len(absent) != 2 || !strings.HasPrefix(absent[1], "stat ") {
		t.Fatalf("absent tree entries = %q, %v", absent, err)
	}
}
