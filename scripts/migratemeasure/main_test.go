package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/prototype/internal/testinputs"
)

// TestMain runs the tests under observation, so the files and variables
// any process they start reads are inputs of Go's test cache.
func TestMain(m *testing.M) { os.Exit(testinputs.Main(m)) }

const root = "../.."

// The ledger's totals are the sums of its edit groups, and its minutes are
// its measured seconds, so the table cannot drift from the record.
func TestLedgerTotalsMatchTheEdits(t *testing.T) {
	ledger, err := readLedger(filepath.Join(root, ledgerPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Examples) != 5 {
		t.Fatalf("ledger has %d examples, want 5", len(ledger.Examples))
	}
	for _, example := range ledger.Examples {
		sums := map[string]int{}
		for _, edit := range example.Edits {
			sums[edit.Kind] += edit.Count
		}
		if sums["mechanical"] != example.Mechanical || sums["judgment"] != example.Judgment || len(sums) > 2 {
			t.Errorf("%s: edit groups sum to %v, totals are mechanical %d judgment %d", example.Name, sums, example.Mechanical, example.Judgment)
		}
		if want := math.Round(float64(example.Seconds)/60*100) / 100; example.Minutes != want {
			t.Errorf("%s: minutes %.2f, want %.2f from %d s", example.Name, example.Minutes, want, example.Seconds)
		}
	}
}

func TestRunPrintsOneRowPerExample(t *testing.T) {
	var out strings.Builder
	if err := run(&out, root); err != nil {
		t.Fatal(err)
	}
	rows := 0
	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.HasPrefix(line, "| examples/") {
			rows++
		}
	}
	if rows != 5 {
		t.Fatalf("got %d example rows, want 5:\n%s", rows, out.String())
	}
}

func TestTokensAndCodeLines(t *testing.T) {
	text := "// note\nx := \"a // b\" + 500ms\n\nif a >= b { f(a) }\n"
	if got := codeLines(text); got != 2 {
		t.Errorf("codeLines = %d, want 2", got)
	}
	// x := "a // b" + 500ms | if a >= b { f ( a ) }
	if got := tokens(text); got != 15 {
		t.Errorf("tokens = %d, want 15", got)
	}
}
