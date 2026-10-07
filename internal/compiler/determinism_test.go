package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// compilationObservation is everything a compile publishes or lowers from
// its checked facts: the canonical snapshot and type arena by value, the
// revision and producer identity, the check response (bundled interface
// digests included) and every emitted artifact.
type compilationObservation struct {
	facts, revision, producer, check, goSource, entry, library string
}

func observeCompilation(t *testing.T, source string) compilationObservation {
	t.Helper()
	r := CompileAt(source, "go", "../../examples")
	// Wall-clock timings, and the response sizes that count their digits, are
	// the only fields expected to vary between compiles.
	r.Timings = Timings{}
	check, err := json.Marshal(r.CheckResponse())
	if err != nil {
		t.Fatal(err)
	}
	o := compilationObservation{facts: checkedFacts(r), revision: r.Revision, producer: r.ProducerIdentity, check: string(check)}
	if r.Checked {
		goSource, err := r.EmitGo()
		o.goSource = artifact(goSource, "", err)
		o.entry = artifact(r.Emit(true))
		o.library = artifact(r.Emit(false))
	}
	return o
}

// artifact renders an emission, or its refusal, as one comparable value.
func artifact(code, declarations string, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return code + "\x00" + declarations
}

// Bundled template parameters used to receive arena identities in map order,
// so repeated compiles of generic-users.ef lowered different Go and JS.
func TestRepeatedCompilationIsDeterministic(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil || len(files) == 0 {
		t.Fatal("no authored examples", err)
	}
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		first := observeCompilation(t, string(source))
		for i := 0; i < 12; i++ {
			if again := observeCompilation(t, string(source)); again != first {
				t.Fatalf("%s: compile %d diverged from the first compile", filepath.Base(path), i+2)
			}
		}
	}
}
