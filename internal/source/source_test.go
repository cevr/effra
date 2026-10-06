package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularFileChecksConfiguredBound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.ef")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegularFile(path, 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}
