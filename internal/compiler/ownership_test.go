package compiler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const ownershipDirectEscape = `
effect fn bad() -> File throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let alias = file
  alias
 }
}
effect fn main() -> () { () }
`

func TestOwnershipRejectsInnerOwnedAliasEscape(t *testing.T) {
	r := Compile(ownershipDirectEscape)
	if r.Checked || !hasCode(r, "EF123") {
		t.Fatalf("expected compiler ownership escape diagnostic: %+v", r.Diagnostics)
	}
}

const ownershipBorrowedOuter = `
effect fn borrow(file: File) -> File {
 scope {
  let alias = file
  alias
 }
}
effect fn main() -> () { () }
`

func TestOwnershipAllowsBorrowedOuterAlias(t *testing.T) {
	r := Compile(ownershipBorrowedOuter)
	if !r.Checked {
		t.Fatalf("borrowed outer handle should remain valid: %+v", r.Diagnostics)
	}
	symbol := r.Find("borrow")
	if symbol == nil || len(symbol.Actual.Ownership) != 1 {
		t.Fatalf("expected canonical ownership metadata: %+v", symbol)
	}
	fact := symbol.Actual.Ownership[0]
	if fact.Status != "borrowed" || fact.Region != "parameter:file" || fact.Origin != "parameter" {
		t.Fatalf("unexpected borrowed ownership fact: %+v", fact)
	}
}

func TestOwnershipMetadataIsPresentForBothTargets(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(ownershipBorrowedOuter, target)
		if !r.Checked {
			t.Fatalf("%s ownership metadata should check: %+v", target, r.Diagnostics)
		}
		if r.SchemaVersion < 3 {
			t.Fatalf("%s result schema did not advance for ownership metadata: %d", target, r.SchemaVersion)
		}
	}
}

func TestOwnershipMetadataIsInspectableThroughResultAndQuery(t *testing.T) {
	r := Compile(ownershipBorrowedOuter)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	info, err := r.TypeAt(strings.LastIndex(ownershipBorrowedOuter, "alias"))
	if err != nil || len(info.Type.Ownership) != 1 || info.Type.Ownership[0].Status != "borrowed" {
		t.Fatalf("query lost canonical ownership evidence: %+v %v", info, err)
	}
	encoded, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(encoded), `"ownership"`) || !strings.Contains(string(encoded), `"borrowed"`) {
		t.Fatalf("result metadata is not inspectable: %v %s", err, encoded)
	}
}

func TestOwnershipSeparatesDeferredCaptureFromInvocationResult(t *testing.T) {
	source := `
effect fn readLater(file: File) -> string throws {IoError} uses {Files} {
 run Files.readText(file)
}

effect fn executed(file: File) -> string throws {IoError} uses {Files} {
 run readLater(file)
}
effect fn main() -> () { () }
`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	deferred, err := r.TypeAt(strings.Index(source, "readLater(file)"))
	if err != nil || len(deferred.Type.Captures) != 1 || deferred.Type.Captures[0].Origin != "parameter" {
		t.Fatalf("deferred recipe did not retain its capture evidence: %+v %v", deferred, err)
	}
	executed, err := r.TypeAt(strings.Index(source, "run readLater"))
	if err != nil || len(executed.Type.Captures) != 0 || len(executed.Type.Ownership) != 0 {
		t.Fatalf("run did not consume recipe captures for its string result: %+v %v", executed, err)
	}
}

func TestOwnershipFactsStayBoundedForSharedNestedPayloads(t *testing.T) {
	var source strings.Builder
	for i := 0; i <= 20; i++ {
		if i == 0 {
			fmt.Fprintln(&source, "record R0 { file: File }")
		} else {
			fmt.Fprintf(&source, "record R%d { left: R%d, right: R%d }\n", i, i-1, i-1)
		}
	}
	fmt.Fprintln(&source, `effect fn bad() -> R20 throws {IoError} uses {Files} {`)
	fmt.Fprintln(&source, ` scope {`)
	fmt.Fprintln(&source, `  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprintln(&source, `  let value0 = R0 { file: file }`)
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&source, "  let value%d = R%d { left: value%d, right: value%d }\n", i, i, i-1, i-1)
	}
	fmt.Fprintln(&source, `  value20`)
	fmt.Fprintln(&source, ` }`)
	fmt.Fprintln(&source, `}`)
	fmt.Fprintln(&source, `effect fn main() -> () { () }`)
	r := Compile(source.String())
	if !hasCode(r, "EF123") {
		t.Fatalf("shared nested payload should retain an ownership escape proof: %+v", r.Diagnostics)
	}
	symbol := r.Find("bad")
	if symbol == nil || len(symbol.Actual.Ownership) > 64 {
		t.Fatalf("ownership metadata grew beyond its bounded representation: %+v", symbol)
	}
}

func TestOwnershipProviderCaptureIsSeparateFromMethodResult(t *testing.T) {
	source := `
service Store { effect fn get() -> File }
impl Captured(file: File) for Store {
 effect fn get() -> File { file }
}
effect fn use(file: File) -> () {
 let provider = run Captured(file); ()
}
effect fn main() -> () { () }
`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, "run Captured(file)"))
	if err != nil || len(info.Type.Captures) != 1 || info.Type.Captures[0].Status != "borrowed" || info.Type.Captures[0].Path != "capture:file" {
		t.Fatalf("provider capture metadata was not retained: %+v %v", info, err)
	}
}

const ownershipBorrowingFiles = `
impl Borrowing(file: File) for Files {
 effect fn openRead(path: string) -> File { file }
 effect fn readText(file: File) -> string { "borrowed" }
 effect fn readFile(path: string) -> string { "borrowed" }
}
effect fn borrow(file: File) -> File throws {IoError} uses {Files} {
 let provider = run Borrowing(file)
 run Files.openRead("fixture").provide<Files>(provider)
}
effect fn main() -> () { () }
`

func TestOwnershipKeepsCustomFilesAcquisitionUnknown(t *testing.T) {
	r := Compile(ownershipBorrowingFiles)
	if !r.Checked {
		t.Fatalf("custom Files provider should remain admitted: %+v", r.Diagnostics)
	}
	symbol := r.Find("borrow")
	if symbol == nil || len(symbol.Actual.Ownership) != 1 || symbol.Actual.Ownership[0].Status != "unknown" {
		t.Fatalf("custom Files provider must expose unknown ownership: %+v", symbol)
	}
}

func diagnosticCount(r *Result, code string) int {
	count := 0
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}

func requireOwnershipRejected(t *testing.T, source string) *Result {
	t.Helper()
	r := Compile(source)
	if r.Checked || diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("expected an isolated ownership rejection: %+v", r.Diagnostics)
	}
	return r
}

func requireOwnershipAccepted(t *testing.T, source string) *Result {
	t.Helper()
	r := Compile(source)
	if !r.Checked {
		t.Fatalf("expected an isolated ownership acceptance: %+v", r.Diagnostics)
	}
	return r
}

func TestOwnershipWrapperProjectionPreservesRootParameter(t *testing.T) {
	requireOwnershipRejected(t, `
record Box { file: File }
fn wrap(file: File) -> Box { Box { file: file } }
fn unwrap(file: File) -> File { wrap(file).file }
effect fn bad() -> File throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  unwrap(file)
 }
}
effect fn main() -> () { () }
`)
}

func TestOwnershipFieldProjectionDiscardsUnselectedOwnedSibling(t *testing.T) {
	requireOwnershipAccepted(t, `
record Pair { outer: File, inner: File }
fn selectOuter(pair: Pair) -> File { pair.outer }
effect fn good(borrowed: File) -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  selectOuter(Pair { outer: borrowed, inner: inner })
 }
}
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
record Pair { outer: File, inner: File }
fn selectInner(pair: Pair) -> File { pair.inner }
effect fn bad(borrowed: File) -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  selectInner(Pair { outer: borrowed, inner: inner })
 }
}
effect fn main() -> () { () }
`)
}

func TestOwnershipEnumProjectionFiltersVariantsAndRetainsNestedFacts(t *testing.T) {
	requireOwnershipAccepted(t, `
enum Packet { A { file: File } B { file: File } }
effect fn good(borrowed: File) -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let packet = Packet.A { file: inner }
  match packet { Packet.A { file } => borrowed Packet.B { file } => file }
 }
}
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
record Box { file: File }
enum Packet { Full { box: Box } }
effect fn bad() -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let packet = Packet.Full { box: Box { file: inner } }
  match packet { Packet.Full { box } => box.file }
 }
}
effect fn main() -> () { () }
`)
}

func TestOwnershipForwardAndRecursiveSummariesRetainUnsafeFacts(t *testing.T) {
	requireOwnershipRejected(t, `
effect fn bad() -> File throws {IoError} uses {Files} { scope { run outer() } }
effect fn outer() -> File throws {IoError} uses {Files} { run middle() }
effect fn middle() -> File throws {IoError} uses {Files} { run leaf() }
effect fn leaf() -> File throws {IoError} uses {Files} { run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles) }
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
effect fn bad() -> File throws {IoError} uses {Files} { scope { run first(true) } }
effect fn first(stop: bool) -> File throws {IoError} uses {Files} { if stop { run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles) } else { run second(true) } }
effect fn second(stop: bool) -> File throws {IoError} uses {Files} { run first(stop) }
effect fn main() -> () { () }
`)
}

func TestOwnershipScopeForkAndTimeoutOwnersRemainDistinct(t *testing.T) {
	requireOwnershipRejected(t, `
effect fn bad() -> File throws {IoError} uses {Files} {
 scope { run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles) }
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
effect fn borrow(file: File) -> File { file }
effect fn good(file: File) -> File throws {IoError} {
 let child = fork borrow(file)
 run child.join()
}
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
effect fn bad() -> File throws {IoError, Timeout} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles).timeout(1000)
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
effect fn borrow(file: File) -> File { file }
effect fn good(file: File) -> File throws {IoError, Timeout} {
 run borrow(file).timeout(1000)
}
effect fn main() -> () { () }
`)
}

func TestOwnershipConcreteCallerBorrowsStayMaterialized(t *testing.T) {
	requireOwnershipAccepted(t, `
effect fn borrow(file: File) -> File { file }
effect fn good() -> File throws {IoError} uses {Files} {
 let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 let recipe = borrow(f)
 scope { run recipe }
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
effect fn borrow(file: File) -> File { file }
effect fn good() -> string throws {IoError} uses {Files} {
 let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 let child = fork borrow(f)
 let result = run child.join()
 run Files.readText(result).provide<Files>(LiveFiles)
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
error Missing
effect fn absent() -> File throws {Missing} { fail Missing }
effect fn good() -> File throws {IoError} uses {Files} {
 let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 let recovered = absent().catch<Missing>(f)
 scope { run recovered }
}
effect fn main() -> () { () }
`)
}

func TestOwnershipTimeoutSeparatesEagerArgumentsFromDeferredExecution(t *testing.T) {
	requireOwnershipAccepted(t, `
effect fn borrow(file: File) -> File { file }
effect fn good() -> string throws {IoError, Timeout} uses {Files} {
 let result = run borrow(run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)).timeout(1000)
 run Files.readText(result).provide<Files>(LiveFiles)
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
effect fn borrow(file: File) -> File { file }
effect fn good() -> string throws {IoError, Timeout} uses {Files} {
 let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 let result = run borrow(f).timeout(1000)
 run Files.readText(result).provide<Files>(LiveFiles)
}
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
effect fn acquire() -> File throws {IoError} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn bad() -> File throws {IoError, Timeout} uses {Files} {
 run acquire().timeout(1000)
}
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
effect fn bad() -> File throws {IoError, Timeout} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles).timeout(1000)
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
error Missing
effect fn absent() -> File throws {Missing} { fail Missing }
effect fn borrow(file: File) -> File { file }
effect fn good() -> string throws {IoError, Timeout} uses {Files} {
 let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 let recovered = run absent().catch<Missing>(f)
 let result = run borrow(recovered).timeout(1000)
 run Files.readText(result).provide<Files>(LiveFiles)
}
effect fn main() -> () { () }
`)
	requireOwnershipRejected(t, `
effect fn borrow(file: File) -> File { file }
effect fn bad() -> File throws {IoError, Timeout} uses {Files} {
 scope {
  let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  run borrow(f).timeout(1000)
 }
}
effect fn main() -> () { () }
`)
}

func TestOwnershipCatchPreservesBothBranchesWithoutRebindingBorrow(t *testing.T) {
	requireOwnershipRejected(t, `
error Missing
effect fn absent() -> File throws {Missing} { fail Missing }
effect fn bad() -> File throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  run absent().catch<Missing>(file)
 }
}
effect fn main() -> () { () }
`)
	requireOwnershipAccepted(t, `
error Missing
effect fn absent() -> File throws {Missing} { fail Missing }
effect fn good(file: File) -> File throws {IoError} {
 let recovered = absent().catch<Missing>(file)
 scope { run recovered }
}
effect fn main() -> () { () }
`)
}

func TestOwnershipHandleFreeSharedDAGIsStructurallyBounded(t *testing.T) {
	var source strings.Builder
	for i := 0; i <= 20; i++ {
		if i == 0 {
			fmt.Fprintln(&source, "record R0 { value: string }")
		} else {
			fmt.Fprintf(&source, "record R%d { left: R%d, right: R%d }\n", i, i-1, i-1)
		}
	}
	fmt.Fprintln(&source, "fn identity(value: R20) -> R20 { value }")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	r := requireOwnershipAccepted(t, source.String())
	if r.Timings.CheckMicros > 500_000 {
		t.Fatalf("handle-free shared DAG traversal exceeded bound: %dµs", r.Timings.CheckMicros)
	}
}

func TestOwnershipCappedProjectionDoesNotClaimAnAmbiguousOwnedField(t *testing.T) {
	var source strings.Builder
	for i := 0; i <= 20; i++ {
		if i == 0 {
			fmt.Fprintln(&source, "record R0 { file: File }")
		} else {
			fmt.Fprintf(&source, "record R%d { left: R%d, right: R%d }\n", i, i-1, i-1)
		}
	}
	fmt.Fprintln(&source, "record Root { outer: File, nested: R20 }")
	fmt.Fprintln(&source, "fn selectOuter(root: Root) -> File { root.outer }")
	fmt.Fprintln(&source, "effect fn good(borrowed: File) -> File throws {IoError} uses {Files} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprintln(&source, "  let value0 = R0 { file: inner }")
	for i := 1; i <= 20; i++ {
		fmt.Fprintf(&source, "  let value%d = R%d { left: value%d, right: value%d }\n", i, i, i-1, i-1)
	}
	fmt.Fprintln(&source, "  selectOuter(Root { outer: borrowed, nested: value20 })")
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	requireOwnershipAccepted(t, source.String())
}

func ownershipMixedRecordSource(width int, selection string, helper bool) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if helper {
		fmt.Fprintf(&source, "fn pick(x: Wide) -> File { x.%s }\n", selection)
	}
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} uses {Files} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprint(&source, "  let value = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		if i == width-1 {
			fmt.Fprintf(&source, " f%d: borrowed", i)
		} else {
			fmt.Fprintf(&source, " f%d: inner", i)
		}
	}
	fmt.Fprintln(&source, " }")
	if helper {
		fmt.Fprintln(&source, "  pick(value)")
	} else {
		fmt.Fprintf(&source, "  value.%s\n", selection)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipMixedEnumSource(width int, selection string) string {
	var source strings.Builder
	fmt.Fprint(&source, "enum Packet { P {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " } }")
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} uses {Files} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprint(&source, "  let packet = Packet.P {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		if i == width-1 {
			fmt.Fprintf(&source, " f%d: borrowed", i)
		} else {
			fmt.Fprintf(&source, " f%d: inner", i)
		}
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintf(&source, "  match packet { Packet.P { %s } => %s }\n", selection, selection)
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipMixedDeepSource(width int, selection string) string {
	var source strings.Builder
	fmt.Fprintln(&source, "record Leaf { file: File }")
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: Leaf", i)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} uses {Files} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprint(&source, "  let value = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		if i == width-1 {
			fmt.Fprintf(&source, " f%d: Leaf { file: borrowed }", i)
		} else {
			fmt.Fprintf(&source, " f%d: Leaf { file: inner }", i)
		}
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintf(&source, "  value.%s.file\n", selection)
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func TestOwnershipMixedFactBudgetRetainsKnownPaths(t *testing.T) {
	for _, width := range []int{64, 65} {
		width := width
		t.Run(fmt.Sprintf("record-direct-%d", width), func(t *testing.T) {
			requireOwnershipRejected(t, ownershipMixedRecordSource(width, "f0", false))
			requireOwnershipAccepted(t, ownershipMixedRecordSource(width, fmt.Sprintf("f%d", width-1), false))
		})
		t.Run(fmt.Sprintf("record-helper-%d", width), func(t *testing.T) {
			requireOwnershipRejected(t, ownershipMixedRecordSource(width, "f0", true))
		})
		t.Run(fmt.Sprintf("enum-%d", width), func(t *testing.T) {
			requireOwnershipRejected(t, ownershipMixedEnumSource(width, "f0"))
			requireOwnershipAccepted(t, ownershipMixedEnumSource(width, fmt.Sprintf("f%d", width-1)))
		})
		t.Run(fmt.Sprintf("deep-%d", width), func(t *testing.T) {
			requireOwnershipRejected(t, ownershipMixedDeepSource(width, "f0"))
			requireOwnershipAccepted(t, ownershipMixedDeepSource(width, fmt.Sprintf("f%d", width-1)))
		})
	}
}

func TestOwnershipOriginalPayloadAndCaptureCasesRemainIndependentlyRejected(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "record payload",
			source: `
record Envelope { file: File, label: string }
effect fn bad() -> Envelope throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  Envelope { file: file, label: "inner" }
 }
}
effect fn main() -> () { () }
`,
		},
		{
			name: "failure payload",
			source: `
error WithFile { file: File }
effect fn bad() -> string throws {WithFile, IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  fail WithFile { file: file }
 }
}
effect fn main() -> () { () }
`,
		},
		{
			name: "conditional",
			source: `
effect fn bad(file: File, choose: bool) -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  if choose { inner } else { file }
 }
}
effect fn main() -> () { () }
`,
		},
		{
			name: "deferred capture",
			source: `
effect fn readLater(file: File) -> string throws {IoError} uses {Files} {
 run Files.readText(file)
}
effect fn bad() -> string throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  readLater(file)
 }
}
effect fn main() -> () { () }
`,
		},
		{
			name: "provider capture",
			source: `
impl Capturing(file: File) for Files {
 effect fn openRead(path: string) -> File { file }
 effect fn readText(file: File) -> string { "captured" }
 effect fn readFile(path: string) -> string { "captured" }
}
effect fn bad() -> string throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let provider = run Capturing(file)
  Files.readText(file).provide<Files>(provider)
 }
}
effect fn main() -> () { () }
`,
		},
		{
			name: "child result",
			source: `
effect fn childFile() -> File throws {IoError} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn bad() -> File throws {IoError} uses {Files} {
 let child = fork childFile()
 run child.join()
}
effect fn main() -> () { () }
`,
		},
		{
			name: "child use after join",
			source: `
effect fn childFile() -> File throws {IoError} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn bad() -> string throws {IoError} uses {Files} {
 let child = fork childFile()
 let file = run child.join()
 run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn main() -> () { () }
`,
		},
		{
			name: "fiber handle",
			source: `
effect fn childFile() -> File throws {IoError} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn bad() -> () throws {IoError} uses {Files} {
 scope {
  let child = fork childFile()
  child
 }
}
effect fn main() -> () { () }
`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			requireOwnershipRejected(t, testCase.source)
		})
	}
}

func TestOwnershipRejectsNestedEnumProjectionEscape(t *testing.T) {
	source := `
record Box { file: File }
enum Packet { Full { box: Box } }
effect fn bad() -> File throws {IoError} uses {Files} {
 scope {
  let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let p = Packet.Full { box: Box { file: f } }
  match p { Packet.Full { box } => box.file }
 }
}
effect fn main() -> () { () }
`
	r := Compile(source)
	if r.Checked || diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("nested enum projection must retain the inner ownership proof: %+v", r.Diagnostics)
	}
}

func TestOwnershipRebasesDeferredAcquisitionToActualRunScope(t *testing.T) {
	source := `
effect fn bad() -> File throws {IoError} {
 let recipe = Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
 scope { run recipe }
}
effect fn main() -> () { () }
`
	r := Compile(source)
	if r.Checked || diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("deferred acquisition must be owned by its execution scope: %+v", r.Diagnostics)
	}
}

func TestOwnershipRebasesDeferredAcquisitionToForkChild(t *testing.T) {
	source := `
effect fn acquire() -> File throws {IoError} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn bad() -> string throws {IoError} {
 scope {
  let child = fork acquire()
  let file = run child.join()
  run Files.readText(file).provide<Files>(LiveFiles)
 }
}
effect fn main() -> () { () }
`
	r := Compile(source)
	if r.Checked || diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("fork child execution must retain child-owned result provenance: %+v", r.Diagnostics)
	}
}

func TestOwnershipCarriesCatchFallbackProvenance(t *testing.T) {
	source := `
error Missing
effect fn absent() -> File throws {Missing} { fail Missing }
effect fn bad() -> File throws {IoError} {
 scope {
  let f = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  run absent().catch<Missing>(f)
 }
}
effect fn main() -> () { () }
`
	r := Compile(source)
	if r.Checked || diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("catch fallback must carry its ownership proof: %+v", r.Diagnostics)
	}
}

const ownershipFieldSensitiveHelper = `
record Pair { outer: File, inner: File }
fn selectOuter(pair: Pair) -> File { pair.outer }
fn selectInner(pair: Pair) -> File { pair.inner }
effect fn safe(borrowed: File) -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  selectOuter(Pair { outer: borrowed, inner: inner })
 }
}
effect fn unsafe() -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  selectInner(Pair { outer: inner, inner: inner })
 }
}
effect fn main() -> () { () }
`

func TestOwnershipHelperProjectionIsParameterRelative(t *testing.T) {
	r := Compile(ownershipFieldSensitiveHelper)
	if diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("field-sensitive helper fixture lost the unsafe proof: %+v", r.Diagnostics)
	}
	unsafeStart := strings.Index(ownershipFieldSensitiveHelper, "effect fn unsafe")
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Code == "EF123" && diagnostic.Span.Offset < unsafeStart {
			t.Fatalf("helper returning the borrowed field should remain valid: %+v", r.Diagnostics)
		}
	}
}

func TestOwnershipSummariesFollowFunctionDependencies(t *testing.T) {
	source := `
effect fn leaf() -> File throws {IoError} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn middle() -> File throws {IoError} { run leaf() }
effect fn outer() -> File throws {IoError} { run middle() }
effect fn bad() -> File throws {IoError} { scope { run outer() } }
effect fn main() -> () { () }
`
	r := Compile(source)
	if r.Checked || diagnosticCount(r, "EF123") == 0 {
		t.Fatalf("dependency-ordered summaries must carry ownership through helper calls: %+v", r.Diagnostics)
	}
}
