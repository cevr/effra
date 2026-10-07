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

// One hundred fresh compiles of generic-users.ef publish one canonical
// snapshot, one generated main and one generation. The bundled templates
// Option and Result once entered the program in either order, which changed
// the generated Go bytes and with them the published generation identity.
func TestGenericNativeGenerationIsDeterministic(t *testing.T) {
	source, err := os.ReadFile("../../examples/generic-users.ef")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	facts, mains, generations := map[string]int{}, map[string]int{}, map[string]int{}
	for i := 0; i < 100; i++ {
		r := CompileAt(string(source), "go", "../../examples")
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		application, err := r.GoApplication(GoGenerationBuild)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := r.GoSourceSnapshot("/origin/generic-users.ef", application)
		if err != nil {
			t.Fatal(err)
		}
		generation, err := PublishGoSourceSnapshot(root, snapshot)
		if err != nil {
			t.Fatal(err)
		}
		facts[checkedFacts(r)]++
		mains[digestBytes(application.Main)]++
		generations[generation.GenerationID]++
	}
	if len(facts) != 1 || len(mains) != 1 || len(generations) != 1 {
		t.Fatalf("100 compiles produced %d canonical snapshots, %d generated mains and %d generations", len(facts), len(mains), len(generations))
	}
}

// mixedBundledConsumer uses two bundled modules whose declarations carry
// distinct callable contracts, so their summaries both intern callables into
// the receiving arena.
const mixedBundledConsumer = `import Convert "effra/conversions"
import Fns "effra/functions"
record User { name: string }
effect fn decode(input: string) -> User { User { name: input } }
effect fn encode(user: User) -> string { user.name }
effect fn main() -> string {
    let converter = Convert.witness(decode, encode)
    let user = run converter.decode("Ada")
    Fns.identity(run converter.encode(user))
}
`

func resetBundledSummaryCache() {
	bundledSummaryCache.Lock()
	defer bundledSummaryCache.Unlock()
	bundledSummaryCache.entries = map[string]interfaceSummary{}
	bundledSummaryCache.bytes = 0
}

// Bundled summary modules are admitted in module identity order, so a
// consumer of several callable-bearing modules allocates one canonical arena
// whether each summary is produced cold or admitted from the warm cache, on
// either target.
func TestMixedBundledSummaryAdmissionIsDeterministic(t *testing.T) {
	t.Cleanup(resetBundledSummaryCache)
	for _, target := range []string{"go", "js"} {
		var first string
		for i := 0; i < 40; i++ {
			if i%2 == 0 {
				resetBundledSummaryCache()
			}
			r := CompileFor(mixedBundledConsumer, target)
			if !r.Checked {
				t.Fatal(target, r.Diagnostics)
			}
			if len(r.Program.semantic.admittedSummaries) < 2 {
				t.Fatalf("%s: fixture must admit two bundled summaries", target)
			}
			facts := checkedFacts(r)
			if i == 0 {
				first = facts
			} else if facts != first {
				t.Fatalf("%s: compile %d (%s summaries) allocated a different canonical arena", target, i+1, map[bool]string{true: "cold", false: "warm"}[i%2 == 0])
			}
		}
	}
}
