package main

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"effra.local/sketch9/sketch"
)

// The byte budget holds while reading (M4): a trace three times the budget is
// refused after reading at most the budget and one byte, so the refusal
// allocates about one budget, not the whole file. The reviewer's version of
// this test measured 33,678,368 bytes for a 32 MiB file before the fix.
func TestTraceByteBudgetBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	line := []byte(strings.Repeat("x", 1<<20-1) + "\n")
	for range 3 * sketch.TraceByteBudget / len(line) {
		if _, err := f.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err = trace("../../testdata/trace/timeout.sketch", path, false, io.Discard)
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("got %v; want a refusal over the byte budget", err)
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("refused a %d-byte trace after allocating %d bytes", 3*sketch.TraceByteBudget, allocated)
	if allocated > sketch.TraceByteBudget*3/2 {
		t.Fatalf("refusing a %d-byte trace allocated %d bytes; want at most 1.5 times the %d-byte budget", 3*sketch.TraceByteBudget, allocated, sketch.TraceByteBudget)
	}
}

// A trace within the budget is read whole, byte for byte.
func TestReadTraceWithinBudget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.jsonl")
	want := "{\"schema\":\"effra-trace-3\"}\n"
	if err := os.WriteFile(path, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readTrace(path)
	if err != nil || string(got) != want {
		t.Fatalf("readTrace: %q, %v; want %q", got, err, want)
	}
}
