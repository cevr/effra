package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

// failingReader is an image whose bytes cannot be read.
type failingReader struct{}

func (failingReader) ReadAt([]byte, int64) (int, error) { return 0, errors.New("no image") }

// TestCompilerIdentity: the identity is the digest of the executable's
// bytes. Two processes of one binary share it, so an unchanged build
// reuses a module across processes. Two binaries the linker builds and
// signs from the same package, differing only in the value -X gives one
// string, have different identities, each its own digest; this runs on
// every platform, darwin/arm64 included, where an unsigned page is killed.
// On Linux a copy patched in exactly one byte also runs and differs. The
// open by os.Executable's path (the start-up open off Linux) gives the
// same identity as /proc/self/exe, and an unreadable image is an error,
// never a process-scoped key.
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

	dir := t.TempDir()
	built := map[string][]byte{}
	identities := map[string]string{}
	for _, marker := range []string{"ergo-identity-marker-0", "ergo-identity-marker-1"} {
		output := filepath.Join(dir, marker)
		link := exec.Command("go", "test", "-c", "-o", output,
			"-ldflags=-X=ergo.local/prototype/internal/sketch/build.identityMarker="+marker, ".")
		if out, err := link.CombinedOutput(); err != nil {
			t.Fatalf("link the %s variant: %v\n%s", marker, err, out)
		}
		printed, identity := identityOf(t, output)
		if printed != marker || identity != fileDigest(t, output) {
			t.Fatalf("variant %s printed marker %s and identity %s, want its digest %s", marker, printed, identity, fileDigest(t, output))
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		built[marker], identities[marker] = data, identity
	}
	if identities["ergo-identity-marker-0"] == identities["ergo-identity-marker-1"] {
		t.Fatalf("two linked variants share identity %s", identities["ergo-identity-marker-0"])
	}
	differing := 0
	a, b := built["ergo-identity-marker-0"], built["ergo-identity-marker-1"]
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			differing++
		}
	}
	t.Logf("linked variants: %d and %d bytes, %d differing bytes in the common prefix", len(a), len(b), differing)

	if runtime.GOOS == "linux" {
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
		patched := filepath.Join(dir, "patched")
		if err := os.WriteFile(patched, changed, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, other := identityOf(t, patched); other == want || other != fileDigest(t, patched) {
			t.Fatalf("a one-byte change gave identity %s (original %s)", other, want)
		}
	}

	byPath, err := openExecutable("darwin")
	if err != nil {
		t.Fatal(err)
	}
	defer byPath.Close()
	if got, err := imageIdentity(byPath); err != nil || got != want {
		t.Fatalf("identity by os.Executable's path %s %v, want %s", got, err, want)
	}
	if _, err := imageIdentity(failingReader{}); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("an unreadable image gave %v", err)
	}
}

// TestCompilerIdentitySurvivesAReinstall: a compiler process whose
// installed path is replaced by rename (go install, a package manager)
// keeps the identity of the bytes it runs, because the executable was
// opened at start. A late open of the path, the race the start-up open
// closes, would name the replacement instead.
func TestCompilerIdentitySurvivesAReinstall(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(t.TempDir(), "compiler")
	if err := os.WriteFile(installed, data, 0o755); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(installed, "-test.run=^$")
	child.Env = append(os.Environ(), helperVariable+"=replace")
	out, err := child.Output()
	if err != nil {
		t.Fatalf("reinstall helper: %v", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 4 {
		t.Fatalf("reinstall helper printed %q", out)
	}
	original, replacement, identity, late := fields[0], fields[1], fields[2], fields[3]
	if original == replacement || identity != original || late != replacement || original != fileDigest(t, binary) {
		t.Fatalf("after a reinstall: identity %s, original %s, replacement %s, late open %s", identity, original, replacement, late)
	}
}
