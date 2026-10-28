package receipt_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"ergo.local/prototype/internal/receipt"
)

// physicalRoot returns a temporary directory without symbolic links in its
// own path, so expected locations can be compared literally.
func physicalRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// A `..` after a symbolic link applies to the link's target, as the file
// system walks it, so every spelling of one file locates the same path.
func TestLocateResolvesLinksBeforeParentSteps(t *testing.T) {
	root := physicalRoot(t)
	mkdir(t, filepath.Join(root, "out", "sub"))
	symlink(t, filepath.Join("out", "sub"), filepath.Join(root, "jump"))
	output := filepath.Join(root, "out", "result")
	for _, spelled := range []string{root + "/jump/../result", root + "/jump/../sub/../../out/result"} {
		location, err := receipt.Locate(spelled)
		if err != nil {
			t.Fatal(err)
		}
		if location.Path() != output || location.String() != spelled || location.Admitted() != "" {
			t.Errorf("%s located at %q (spelled %q, admitted %q), want %s", spelled, location.Path(), location.String(), location.Admitted(), output)
		}
	}
	// Missing components below the deepest existing directory are kept as
	// the writer will create them.
	nested, err := receipt.Locate(root + "/jump/new/deeper/file")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "out", "sub", "new", "deeper", "file"); nested.Path() != want {
		t.Fatalf("nested path located at %q, want %q", nested.Path(), want)
	}
}

// A path through a dangling link has no single location, so it is refused
// rather than replayed as a missing directory.
func TestLocateRefusesDanglingLinks(t *testing.T) {
	root := physicalRoot(t)
	symlink(t, "out", filepath.Join(root, "jump"))
	for _, path := range []string{root + "/jump/result", root + "/jump/deeper/result"} {
		if _, err := receipt.Locate(path); err == nil || !strings.Contains(err.Error(), "cannot be resolved") {
			t.Errorf("a path through a dangling link was located: %s: %v", path, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, "out")); err == nil {
		t.Fatal("locating a path created a directory")
	}
}

// A source is admitted as the loader admits it: `..` applies to the spelled
// name, and links in the admitted path are then resolved. An artifact path
// is walked as the file system walks it, and a `..` below a missing
// directory is refused rather than cleaned away.
func TestLocateSourceAndArtifactPaths(t *testing.T) {
	root := physicalRoot(t)
	mkdir(t, filepath.Join(root, "parent", "sub"))
	mkdir(t, filepath.Join(root, "real"))
	symlink(t, filepath.Join("parent", "sub"), filepath.Join(root, "jump"))
	symlink(t, "real", filepath.Join(root, "dir-link"))
	t.Chdir(root)

	spelled := "dir-link/../dir-link/main.ef"
	source, err := receipt.LocateSource("jump/../" + spelled)
	if err != nil {
		t.Fatal(err)
	}
	if source.String() != "jump/../"+spelled || source.Admitted() != filepath.Join(root, "dir-link", "main.ef") || source.Path() != filepath.Join(root, "real", "main.ef") {
		t.Fatalf("source located as spelled=%q admitted=%q path=%q", source, source.Admitted(), source.Path())
	}
	artifact, err := receipt.Locate("jump/../main.ef")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Path() != filepath.Join(root, "parent", "main.ef") || artifact.Admitted() != "" {
		t.Fatalf("artifact located at %q (admitted %q)", artifact.Path(), artifact.Admitted())
	}
	for _, path := range []string{"missing/../out", "missing/./out", "missing/.."} {
		if location, err := receipt.Locate(path); err == nil {
			t.Errorf("%s located at %s", path, location.Path())
		}
	}
	for _, locate := range []func(string) (receipt.Location, error){receipt.Locate, receipt.LocateSource} {
		if _, err := locate(""); err == nil {
			t.Error("an empty path was located")
		}
	}
}

// Every native build runs the go command outside a workspace with the
// fixed build flags, any extra flags, and then the output and package.
func TestGoBuildCommandUsesTheBuildFlagsOutsideAWorkspace(t *testing.T) {
	t.Setenv("GOWORK", "/nonexistent/loop-probe-x/go.work")
	command := receipt.GoBuildCommand("/nonexistent/loop-probe-x", "/nonexistent/loop-probe-x/app", ".", "-v")
	want := append(append([]string{"go", "build"}, receipt.GoBuildFlags...), "-v", "-o", "/nonexistent/loop-probe-x/app", ".")
	if !slices.Equal(command.Args, want) || command.Dir != "/nonexistent/loop-probe-x" {
		t.Fatalf("command %v in %q, want %v", command.Args, command.Dir, want)
	}
	var gowork []string
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "GOWORK=") {
			gowork = append(gowork, entry)
		}
	}
	if !slices.Equal(gowork, []string{"GOWORK=off"}) {
		t.Fatalf("GOWORK entries %v, want only GOWORK=off", gowork)
	}
}
