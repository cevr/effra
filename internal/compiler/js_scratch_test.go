package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// jsModuleDir returns a fresh directory, removed when the test ends, where a
// generated JavaScript or TypeScript module resolves the repository's pinned
// packages through a node_modules link. It lives outside the repository:
// tests then never race other processes over dist/, and the package's test
// cache does not depend on dist/'s changing listing.
func jsModuleDir(t *testing.T, prefix string) string {
	t.Helper()
	modules, err := filepath.Abs(filepath.Join("..", "..", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("pinned JavaScript packages: %v", err)
	}
	dir, err := os.MkdirTemp(t.TempDir(), prefix)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return dir
}
