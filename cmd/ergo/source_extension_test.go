package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestSourceExtensionRule (#221, #217): a 2026 source ends in .ergo, and the
// retired .ef spelling gets exactly the refusal of any other extension, from
// check and from fmt. The legacy edition keeps .ef until retire-ef (#205).
func TestSourceExtensionRule(t *testing.T) {
	root := t.TempDir()
	write := func(rel, text string) string {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write("new/ergo.json", `{"edition":"2026"}`)
	write("old/ergo.json", `{"edition":"legacy"}`)
	for _, c := range []struct {
		rel  string
		want error
	}{
		{"new/main.ergo", nil},
		{"new/main.ef", errSourceExtension},
		{"new/main.txt", errSourceExtension},
		{"new/main", errSourceExtension},
		{"old/main.ef", nil},
		{"old/main.ergo", nil},
		{"old/main.txt", errSourceExtension},
	} {
		path := write(c.rel, "func main() {}\n")
		err := checkSourceExtension(path, nil)
		if err == nil {
			var admitted interface{ Close() error }
			if admitted, err = admitSource(path); err == nil {
				admitted.Close()
			}
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.rel, err, c.want)
		}
	}
	if got := errSourceExtension.Error(); got != "source file must have .ergo extension" {
		t.Fatalf("the refusal reads %q", got)
	}
}
