//go:build unix

package lint

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// promptly runs admission in another goroutine: opening a FIFO with no
// writer would block it, so only a refusal before opening returns.
func promptly(t *testing.T, name string, admit func() error, want string) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- admit() }()
	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v, want %q", name, err, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: admission blocked on a FIFO", name)
	}
}

func mkfifo(t *testing.T, path string) string {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A configuration, a manifest or a pack executable that is a FIFO is
// refused before it is opened: admission never waits for a writer, and a
// pack's timeout and cancellation cannot help a runner that blocks before
// it starts the pack.
func TestFileAdmissionRefusesAFIFOBeforeOpeningIt(t *testing.T) {
	dir := t.TempDir()
	config := mkfifo(t, filepath.Join(dir, "lint.json"))
	promptly(t, "configuration", func() error { _, err := LoadConfig(config); return err }, "not a regular file")

	manifestFIFO := mkfifo(t, filepath.Join(dir, "fifo-manifest.json"))
	promptly(t, "manifest", func() error { _, err := LoadManifest(manifestFIFO); return err }, "not a regular file")
	selecting := filepath.Join(dir, "selecting.json")
	writeFile(t, selecting, `{"version":1,"packs":[{"manifest":"fifo-manifest.json"}]}`)
	promptly(t, "selected manifest", func() error { _, err := LoadConfig(selecting); return err }, "not a regular file")

	// A valid manifest whose executable is a FIFO: selecting it is refused,
	// and a run that is handed it directly fails to spawn, before the
	// timeout could apply.
	manifest := fixtureManifest(t)
	manifest.Executable.Path = "pack"
	mkfifo(t, filepath.Join(dir, "pack"))
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "manifest.json"), string(data))
	promptly(t, "executable at selection", func() error { _, err := LoadManifest(filepath.Join(dir, "manifest.json")); return err }, "not a regular file")
	registry, err := NewRegistry(testBuiltins, manifest)
	if err != nil {
		t.Fatal(err)
	}
	configuration := configure(t, registry, `{"version":1}`)
	promptly(t, "executable at run", func() error {
		report, err := Run(context.Background(), configuration, "fixture", testSnapshot(), RunOptions{Dir: dir, Limits: Limits{Timeout: time.Minute}})
		if err != nil {
			return err
		}
		if report.Failure == nil || report.Failure.Code != FailureSpawn {
			t.Errorf("run: %+v", report)
			return nil
		}
		return errorString(report.Failure.Message)
	}, "not a regular file")
}
