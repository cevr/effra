package source

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenReadsTheOpenFileAndNamesItsPhysicalPath(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "real", "main.ef")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	file, err := Open(filepath.Join(root, "alias", "main.ef"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if file.Path != target {
		t.Fatalf("physical path %s, want %s", file.Path, target)
	}
	// The open file is read, not the spelling: a replacement after the open
	// does not change the bytes.
	if err := os.Rename(target, target+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package q\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := file.Read(0); err != nil || string(data) != "package p\n" {
		t.Fatalf("read %q (%v), want the opened file's bytes", data, err)
	}
	if !file.Info().Mode().IsRegular() {
		t.Fatal("Info does not describe the open file")
	}
	if _, err := file.Read(4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("a bounded read of a larger file gave %v", err)
	}
	if _, err := file.Read(-1); err == nil {
		t.Fatal("a negative bound was accepted")
	}
}

func TestOpenRefusesDirectoriesAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "source must be a regular file") {
		t.Fatalf("a directory was admitted: %v", err)
	}
	if _, err := Open(filepath.Join(dir, "missing.ef")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a missing file gave %v", err)
	}
}
