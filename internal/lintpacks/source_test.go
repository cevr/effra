package lintpacks_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ergo.local/prototype/internal/lintpacks"
)

// admittedBytes is a SourceReader over bytes the caller already admitted.
type admittedBytes []byte

func (a admittedBytes) Read(int) ([]byte, error) { return a, nil }

// FixtureSource and ReceiptSource read the source only from the admitted
// reader: the file at the path holds other bytes, which do not parse, and
// neither entry reads them.
func TestAdmittedEntriesReadOnlyTheAdmittedSource(t *testing.T) {
	dir := packs(t)
	serve := filepath.Join(dir, "fixture-serve", "manifest.json")
	text, err := os.ReadFile("../../examples/lintpack/testdata/provider_boundary.ef")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "provider_boundary.ef")
	if err := os.WriteFile(path, []byte("not ergo source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	session := load(t, lintpacks.Selection{Manifests: []string{serve}})
	expectation, err := session.FixtureSource(context.Background(), path, admittedBytes(text), "go")
	if err != nil || len(expectation.Rules) == 0 {
		t.Fatalf("FixtureSource: %v %+v", err, expectation)
	}
	receipt, err := session.ReceiptSource(context.Background(), path, admittedBytes(text), "go", 2)
	if err != nil || len(receipt.Runs) != 2 || receipt.Source.Bytes != len(text) {
		t.Fatalf("ReceiptSource: %v %+v", err, receipt)
	}
	for _, run := range receipt.Runs {
		if !run.Checked {
			t.Fatalf("a run checked other bytes than the admitted source: %+v", run)
		}
	}
	// The path entry points still read the path.
	if _, err := session.Fixture(context.Background(), path, "go"); err == nil {
		t.Fatal("Fixture did not read the file at its path")
	}
}
