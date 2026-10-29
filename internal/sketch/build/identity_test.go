package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDefaultRoot: an absolute XDG_CACHE_HOME holds the cache; a relative
// one is ignored, as the XDG base directory specification requires, and
// the cache falls back to the platform default, which on Linux is
// $HOME/.cache.
func TestDefaultRoot(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", xdg)
	if got, err := DefaultRoot(); err != nil || got != filepath.Join(xdg, "ergo") {
		t.Fatalf("absolute XDG_CACHE_HOME: %s %v", got, err)
	}
	if runtime.GOOS != "linux" {
		t.Skip("the fallback below is Linux's")
	}
	for _, value := range []string{"relative/cache", ""} {
		t.Setenv("XDG_CACHE_HOME", value)
		if got, err := DefaultRoot(); err != nil || got != filepath.Join(home, ".cache", "ergo") {
			t.Fatalf("XDG_CACHE_HOME=%q: %s %v, want %s", value, got, err, filepath.Join(home, ".cache", "ergo"))
		}
	}
}

// identityOf runs binary as the identity helper and returns the marker and
// identity it prints.
func identityOf(t *testing.T, binary string) (string, string) {
	t.Helper()
	child := exec.Command(binary, "-test.run=^$")
	child.Env = append(os.Environ(), helperVariable+"=identity")
	out, err := child.Output()
	if err != nil {
		t.Fatalf("identity helper %s: %v", binary, err)
	}
	marker, identity, ok := strings.Cut(strings.TrimSpace(string(out)), " ")
	if !ok {
		t.Fatalf("identity helper printed %q", out)
	}
	return marker, identity
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestCompilerIdentity: the identity is the digest of the executable's
// bytes. Two processes of one binary share it, so an unchanged build
// reuses a module across processes; a copy that differs in one byte has
// another. Reading the file os.Executable names (the path off Linux) gives
// the same identity as /proc/self/exe, and an unreadable executable is an
// error, never a process-scoped key.
func TestCompilerIdentity(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := fileDigest(t, binary)
	_, first := identityOf(t, binary)
	_, second := identityOf(t, binary)
	inProcess, err := CompilerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if first != want || second != want || inProcess != want {
		t.Fatalf("identities %s, %s and %s, want the executable digest %s", first, second, inProcess, want)
	}

	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(data, []byte(identityMarker))
	if at < 0 {
		t.Fatal("the marker is not in the binary")
	}
	changed := bytes.Clone(data)
	changed[at+len(identityMarker)-1] = '1'
	copyPath := filepath.Join(t.TempDir(), "changed")
	if err := os.WriteFile(copyPath, changed, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, other := identityOf(t, copyPath); other == want || other != fileDigest(t, copyPath) {
		t.Fatalf("a one-byte change gave identity %s (original %s)", other, want)
	}

	byPath, err := executableIdentity(func() (io.ReadCloser, error) { return os.Open(binary) })
	if err != nil || byPath != want {
		t.Fatalf("identity by os.Executable path %s %v, want %s", byPath, err, want)
	}
	if _, err := executableIdentity(func() (io.ReadCloser, error) { return nil, errors.New("no image") }); err == nil {
		t.Fatal("an unreadable executable gave an identity")
	}
}
