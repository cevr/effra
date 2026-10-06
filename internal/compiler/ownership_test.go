package compiler

import (
	"encoding/json"
	"fmt"
	"slices"
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

func requireOwnershipRejectedWithoutArgumentUse(t *testing.T, source string) *Result {
	t.Helper()
	r := requireOwnershipRejected(t, source)
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Code == "EF123" && strings.Contains(diagnostic.Message, "cannot be used") {
			t.Fatalf("expected result provenance to reject the value, got argument rejection: %+v", r.Diagnostics)
		}
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

func ownershipConditionalRecordSource(width int, reverseConstructor, borrowedFirst bool, mode, selection string) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for position := 0; position < width; position++ {
		i := position
		if reverseConstructor {
			i = width - position - 1
		}
		if position > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if mode == "helper" {
		fmt.Fprintf(&source, "fn pick(x: Wide) -> File { x.%s }\n", selection)
	} else if mode == "recipe" {
		fmt.Fprintf(&source, "effect fn pickLater(x: Wide) -> File throws {IoError} { x.%s }\n", selection)
	}
	fmt.Fprintf(&source, "effect fn probe(borrowed: File, choose: bool) -> File throws {IoError} {\n scope {\n  let inner = run Files.openRead(\"examples/fixture.txt\").provide<Files>(LiveFiles)\n  let value = Wide {")
	for position := 0; position < width; position++ {
		i := position
		if reverseConstructor {
			i = width - position - 1
		}
		if position > 0 {
			fmt.Fprint(&source, ",")
		}
		value := "inner"
		if i == 9 {
			if borrowedFirst {
				value = "if choose { borrowed } else { inner }"
			} else {
				value = "if choose { inner } else { borrowed }"
			}
		} else if i == width-1 && selection != "f9" {
			value = "borrowed"
		}
		fmt.Fprintf(&source, " f%d: %s", i, value)
	}
	fmt.Fprintln(&source, " }")
	switch mode {
	case "helper":
		fmt.Fprintln(&source, "  pick(value)")
	case "recipe":
		fmt.Fprintln(&source, "  let recipe = pickLater(value)")
		fmt.Fprintln(&source, "  run recipe")
	default:
		fmt.Fprintf(&source, "  value.%s\n", selection)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipConditionalEnumSource(width int, borrowedFirst bool, mode, selection string) string {
	var source strings.Builder
	fmt.Fprint(&source, "enum Wide { P {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " } }")
	if mode == "helper" {
		fmt.Fprintf(&source, "fn pick(x: Wide) -> File { match x { Wide.P { %s } => %s } }\n", selection, selection)
	} else if mode == "recipe" {
		fmt.Fprintf(&source, "effect fn pickLater(x: Wide) -> File throws {IoError} { match x { Wide.P { %s } => %s } }\n", selection, selection)
	}
	fmt.Fprintf(&source, "effect fn probe(borrowed: File, choose: bool) -> File throws {IoError} {\n scope {\n  let inner = run Files.openRead(\"examples/fixture.txt\").provide<Files>(LiveFiles)\n  let value = Wide.P {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		value := "inner"
		if i == 9 {
			if borrowedFirst {
				value = "if choose { borrowed } else { inner }"
			} else {
				value = "if choose { inner } else { borrowed }"
			}
		}
		fmt.Fprintf(&source, " f%d: %s", i, value)
	}
	fmt.Fprintln(&source, " }")
	switch mode {
	case "helper":
		fmt.Fprintf(&source, "  match value { Wide.P { %s } => pick(value) }\n", selection)
	case "recipe":
		fmt.Fprintln(&source, "  let recipe = pickLater(value)")
		fmt.Fprintln(&source, "  run recipe")
	default:
		fmt.Fprintf(&source, "  match value { Wide.P { %s } => %s }\n", selection, selection)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipNestedRecordSource(width, layers int, conditional bool, selection string) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if layers == 0 {
		fmt.Fprintln(&source, "record Root { wide: Wide }")
	} else {
		fmt.Fprintln(&source, "record Level0 { wide: Wide }")
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "record Level%d { child: Level%d }\n", i, i-1)
		}
		fmt.Fprintf(&source, "record Root { child: Level%d }\n", layers)
	}
	fmt.Fprintf(&source, "effect fn probe(borrowed: File, choose: bool) -> File throws {IoError} {\n scope {\n  let inner = run Files.openRead(\"examples/fixture.txt\").provide<Files>(LiveFiles)\n  let value = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		value := "inner"
		if conditional && i == 9 {
			value = "if choose { inner } else { borrowed }"
		} else if !conditional && i == width-1 {
			value = "borrowed"
		}
		fmt.Fprintf(&source, " f%d: %s", i, value)
	}
	fmt.Fprintln(&source, " }")
	if layers == 0 {
		fmt.Fprintln(&source, "  let root = Root { wide: value }")
		fmt.Fprintf(&source, "  root.wide.%s\n", selection)
	} else {
		fmt.Fprintln(&source, "  let node0 = Level0 { wide: value }")
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "  let node%d = Level%d { child: node%d }\n", i, i, i-1)
		}
		fmt.Fprintf(&source, "  let root = Root { child: node%d }\n", layers)
		path := "root.child"
		for i := 0; i < layers; i++ {
			path += ".child"
		}
		fmt.Fprintf(&source, "  %s.wide.%s\n", path, selection)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipNestedEnumSource(width, layers int, conditional bool, selection string) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if layers == 0 {
		fmt.Fprintln(&source, "enum Root { P { wide: Wide } }")
	} else {
		fmt.Fprintln(&source, "record Level0 { wide: Wide }")
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "record Level%d { child: Level%d }\n", i, i-1)
		}
		fmt.Fprintf(&source, "enum Root { P { child: Level%d } }\n", layers)
	}
	fmt.Fprintf(&source, "effect fn probe(borrowed: File, choose: bool) -> File throws {IoError} {\n scope {\n  let inner = run Files.openRead(\"examples/fixture.txt\").provide<Files>(LiveFiles)\n  let value = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		value := "inner"
		if conditional && i == 9 {
			value = "if choose { inner } else { borrowed }"
		} else if !conditional && i == width-1 {
			value = "borrowed"
		}
		fmt.Fprintf(&source, " f%d: %s", i, value)
	}
	fmt.Fprintln(&source, " }")
	if layers == 0 {
		fmt.Fprintln(&source, "  let root = Root.P { wide: value }")
		fmt.Fprintf(&source, "  match root { Root.P { wide } => wide.%s }\n", selection)
	} else {
		fmt.Fprintln(&source, "  let node0 = Level0 { wide: value }")
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "  let node%d = Level%d { child: node%d }\n", i, i, i-1)
		}
		fmt.Fprintf(&source, "  let root = Root.P { child: node%d }\n", layers)
		path := "child"
		for i := 0; i < layers; i++ {
			path += ".child"
		}
		fmt.Fprintf(&source, "  match root { Root.P { child } => %s.wide.%s }\n", path, selection)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipNestedSiblingSource(width, layers int, selected string) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "record Level0 { wide: Wide }")
	for i := 1; i <= layers; i++ {
		fmt.Fprintf(&source, "record Level%d { child: Level%d }\n", i, i-1)
	}
	fmt.Fprintf(&source, "record Root { unsafe: Level%d, safe: Level%d }\n", layers, layers)
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	for _, entry := range []struct {
		name     string
		borrowed bool
	}{
		{name: "unsafe", borrowed: false},
		{name: "safe", borrowed: true},
	} {
		fmt.Fprintf(&source, "  let %sValue = Wide {", entry.name)
		for i := 0; i < width; i++ {
			if i > 0 {
				fmt.Fprint(&source, ",")
			}
			value := "inner"
			if entry.borrowed && i == width-1 {
				value = "borrowed"
			}
			fmt.Fprintf(&source, " f%d: %s", i, value)
		}
		fmt.Fprintln(&source, " }")
		fmt.Fprintf(&source, "  let %sNode0 = Level0 { wide: %sValue }\n", entry.name, entry.name)
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "  let %sNode%d = Level%d { child: %sNode%d }\n", entry.name, i, i, entry.name, i-1)
		}
	}
	fmt.Fprintf(&source, "  let root = Root { unsafe: unsafeNode%d, safe: safeNode%d }\n", layers, layers)
	path := "root." + selected
	for i := 0; i < layers; i++ {
		path += ".child"
	}
	fmt.Fprintf(&source, "  %s.wide.f%d\n", path, width-1)
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipCoverageSiblingSource(width int, enum, reverse, independentOwner, safe bool) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if enum {
		if reverse {
			fmt.Fprintln(&source, "enum Root { P { z: File, a: Wide } }")
		} else {
			fmt.Fprintln(&source, "enum Root { P { a: Wide, z: File } }")
		}
	} else if reverse {
		fmt.Fprintln(&source, "record Root { z: File, a: Wide }")
	} else {
		fmt.Fprintln(&source, "record Root { a: Wide, z: File }")
	}
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} {")
	if independentOwner {
		fmt.Fprintln(&source, ` let outer = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	}
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprint(&source, "  let wide = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		owner := "inner"
		if independentOwner {
			owner = "outer"
		}
		fmt.Fprintf(&source, " f%d: %s", i, owner)
	}
	fmt.Fprintln(&source, " }")
	zOwner := "inner"
	if safe {
		zOwner = "borrowed"
	}
	if enum {
		if reverse {
			fmt.Fprintf(&source, "  let root = Root.P { z: %s, a: wide }\n", zOwner)
		} else {
			fmt.Fprintf(&source, "  let root = Root.P { a: wide, z: %s }\n", zOwner)
		}
		fmt.Fprintln(&source, "  match root { Root.P { z } => z }")
	} else {
		if reverse {
			fmt.Fprintf(&source, "  let root = Root { z: %s, a: wide }\n", zOwner)
		} else {
			fmt.Fprintf(&source, "  let root = Root { a: wide, z: %s }\n", zOwner)
		}
		fmt.Fprintln(&source, "  root.z")
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipCoverageBorrowedSummarySource(width int, enum, reverse bool) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if enum {
		if reverse {
			fmt.Fprintln(&source, "enum Root { P { z: Wide, middle: File, a: Wide } }")
		} else {
			fmt.Fprintln(&source, "enum Root { P { a: Wide, middle: File, z: Wide } }")
		}
	} else if reverse {
		fmt.Fprintln(&source, "record Root { z: Wide, middle: File, a: Wide }")
	} else {
		fmt.Fprintln(&source, "record Root { a: Wide, middle: File, z: Wide }")
	}
	fmt.Fprintln(&source, "fn pick(wide: Wide, file: File) -> File {")
	if enum {
		if reverse {
			fmt.Fprintln(&source, " let root = Root.P { z: wide, middle: file, a: wide }")
		} else {
			fmt.Fprintln(&source, " let root = Root.P { a: wide, middle: file, z: wide }")
		}
		fmt.Fprintln(&source, " match root { Root.P { middle } => middle }")
	} else {
		if reverse {
			fmt.Fprintln(&source, " let root = Root { z: wide, middle: file, a: wide }")
		} else {
			fmt.Fprintln(&source, " let root = Root { a: wide, middle: file, z: wide }")
		}
		fmt.Fprintln(&source, " root.middle")
	}
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprint(&source, "  let wide = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: borrowed", i)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "  pick(wide, inner)")
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipJoinSiblingSource(width int, enum, reverse, bothBorrowed bool) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if enum {
		if reverse {
			fmt.Fprintln(&source, "enum Root { P { z: File, a: Wide } }")
		} else {
			fmt.Fprintln(&source, "enum Root { P { a: Wide, z: File } }")
		}
	} else if reverse {
		fmt.Fprintln(&source, "record Root { z: File, a: Wide }")
	} else {
		fmt.Fprintln(&source, "record Root { a: Wide, z: File }")
	}
	writeRoot := func(name, wideOwner, zOwner string) {
		if enum {
			if reverse {
				fmt.Fprintf(&source, "  let %s = Root.P { z: %s, a: Wide {", name, zOwner)
			} else {
				fmt.Fprintf(&source, "  let %s = Root.P { a: Wide {", name)
			}
		} else if reverse {
			fmt.Fprintf(&source, "  let %s = Root { z: %s, a: Wide {", name, zOwner)
		} else {
			fmt.Fprintf(&source, "  let %s = Root { a: Wide {", name)
		}
		for i := 0; i < width; i++ {
			if i > 0 {
				fmt.Fprint(&source, ",")
			}
			fmt.Fprintf(&source, " f%d: %s", i, wideOwner)
		}
		if reverse {
			fmt.Fprintln(&source, " } }")
		} else {
			fmt.Fprintf(&source, " }, z: %s }\n", zOwner)
		}
	}
	fmt.Fprintln(&source, "effect fn probe(borrowed: File, second: File, choose: bool) -> File throws {IoError} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	firstOwner, firstZ := "inner", "inner"
	if bothBorrowed {
		firstOwner, firstZ = "borrowed", "borrowed"
	}
	writeRoot("first", firstOwner, firstZ)
	writeRoot("secondRoot", "borrowed", "second")
	if enum {
		fmt.Fprintln(&source, "  match if choose { first } else { secondRoot } { Root.P { z } => z }")
	} else {
		fmt.Fprintln(&source, "  let selected = if choose { first } else { secondRoot }")
		fmt.Fprintln(&source, "  selected.z")
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipHelperBoxSource(width int, innerFile bool) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "record Box { f: File }")
	fmt.Fprintln(&source, "record Pair { x: Wide, z: Box }")
	fmt.Fprintln(&source, "fn pick(x: Wide, file: File) -> File {")
	fmt.Fprintln(&source, " let pair = Pair { x: x, z: Box { f: file } }")
	fmt.Fprintln(&source, " pair.z.f")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn probe(borrowed: File) -> File throws {IoError} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	fmt.Fprint(&source, "  let wide = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: borrowed", i)
	}
	fmt.Fprintln(&source, " }")
	file := "borrowed"
	if innerFile {
		file = "inner"
	}
	fmt.Fprintf(&source, "  pick(wide, %s)\n", file)
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipJoinHelperSource(width int, bothBorrowed bool) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "record Root { a: Wide, z: File }")
	fmt.Fprintln(&source, "fn pick(root: Root) -> File { root.z }")
	fmt.Fprintln(&source, "effect fn probe(borrowed: File, choose: bool) -> File throws {IoError} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	firstWide, firstZ := "inner", "inner"
	if bothBorrowed {
		firstWide, firstZ = "borrowed", "borrowed"
	}
	writeRoot := func(name, wideOwner, zOwner string) {
		fmt.Fprintf(&source, "  let %s = Root { a: Wide {", name)
		for i := 0; i < width; i++ {
			if i > 0 {
				fmt.Fprint(&source, ",")
			}
			fmt.Fprintf(&source, " f%d: %s", i, wideOwner)
		}
		fmt.Fprintf(&source, " }, z: %s }\n", zOwner)
	}
	writeRoot("first", firstWide, firstZ)
	writeRoot("second", "borrowed", "borrowed")
	fmt.Fprintln(&source, "  let selected = if choose { first } else { second }")
	fmt.Fprintln(&source, "  pick(selected)")
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipSameParameterJoinSource(nested, reverse, bothBorrowed bool) string {
	var source strings.Builder
	if nested {
		fmt.Fprintln(&source, "record Box { value: File }")
	}
	if nested {
		if reverse {
			fmt.Fprintln(&source, "record Pair { b: Box, a: Box }")
		} else {
			fmt.Fprintln(&source, "record Pair { a: Box, b: Box }")
		}
	} else if reverse {
		fmt.Fprintln(&source, "record Pair { b: File, a: File }")
	} else {
		fmt.Fprintln(&source, "record Pair { a: File, b: File }")
	}
	fmt.Fprintln(&source, "fn select(pair: Pair, choose: bool) -> File {")
	left, right := "pair.a", "pair.b"
	if nested {
		left += ".value"
		right += ".value"
	}
	fmt.Fprintf(&source, " if choose { %s } else { %s }\n", left, right)
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn probe(borrowed: File, choose: bool) -> File throws {IoError} {")
	fmt.Fprintln(&source, " scope {")
	fmt.Fprintln(&source, `  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`)
	a, b := "inner", "borrowed"
	if bothBorrowed {
		a, b = "borrowed", "borrowed"
	}
	wrap := func(value string) string {
		if nested {
			return "Box { value: " + value + " }"
		}
		return value
	}
	if reverse {
		fmt.Fprintf(&source, "  let pair = Pair { b: %s, a: %s }\n", wrap(b), wrap(a))
	} else {
		fmt.Fprintf(&source, "  let pair = Pair { a: %s, b: %s }\n", wrap(a), wrap(b))
	}
	fmt.Fprintln(&source, "  select(pair, choose)")
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func ownershipCoverageTerminalSource(width, layers int, enum bool, borrowedIndex, selection int) string {
	var source strings.Builder
	fmt.Fprint(&source, "record Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		fmt.Fprintf(&source, " f%d: File", i)
	}
	fmt.Fprintln(&source, " }")
	if layers == 0 {
		if enum {
			fmt.Fprintln(&source, "enum Root { P { wide: Wide } }")
		} else {
			fmt.Fprintln(&source, "record Root { wide: Wide }")
		}
	} else {
		fmt.Fprintln(&source, "record Level0 { wide: Wide }")
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "record Level%d { child: Level%d }\n", i, i-1)
		}
		if enum {
			fmt.Fprintf(&source, "enum Root { P { child: Level%d } }\n", layers)
		} else {
			fmt.Fprintf(&source, "record Root { child: Level%d }\n", layers)
		}
	}
	fmt.Fprintf(&source, "effect fn probe(borrowed: File) -> File throws {IoError} {\n scope {\n  let inner = run Files.openRead(\"examples/fixture.txt\").provide<Files>(LiveFiles)\n  let value = Wide {")
	for i := 0; i < width; i++ {
		if i > 0 {
			fmt.Fprint(&source, ",")
		}
		value := "inner"
		if i == borrowedIndex {
			value = "borrowed"
		}
		fmt.Fprintf(&source, " f%d: %s", i, value)
	}
	fmt.Fprintln(&source, " }")
	if layers == 0 {
		if enum {
			fmt.Fprintf(&source, "  let root = Root.P { wide: value }\n  match root { Root.P { wide } => wide.f%d }\n", selection)
		} else {
			fmt.Fprintf(&source, "  let root = Root { wide: value }\n  root.wide.f%d\n", selection)
		}
	} else {
		fmt.Fprintln(&source, "  let node0 = Level0 { wide: value }")
		for i := 1; i <= layers; i++ {
			fmt.Fprintf(&source, "  let node%d = Level%d { child: node%d }\n", i, i, i-1)
		}
		if enum {
			fmt.Fprintf(&source, "  let root = Root.P { child: node%d }\n", layers)
			path := "child"
			for i := 0; i < layers; i++ {
				path += ".child"
			}
			fmt.Fprintf(&source, "  match root { Root.P { child } => %s.wide.f%d }\n", path, selection)
		} else {
			fmt.Fprintf(&source, "  let root = Root { child: node%d }\n", layers)
			path := "root.child"
			for i := 0; i < layers; i++ {
				path += ".child"
			}
			fmt.Fprintf(&source, "  %s.wide.f%d\n", path, selection)
		}
	}
	fmt.Fprintln(&source, " }")
	fmt.Fprintln(&source, "}")
	fmt.Fprintln(&source, "effect fn main() -> () { () }")
	return source.String()
}

func TestOwnershipSamePathAlternativesRemainIncompleteAtTheFactCap(t *testing.T) {
	for _, width := range []int{16, 64, 65, 80} {
		for _, borrowedFirst := range []bool{false, true} {
			for _, reverseConstructor := range []bool{false, true} {
				name := fmt.Sprintf("record-%d-borrowed-first-%t-reverse-%t", width, borrowedFirst, reverseConstructor)
				t.Run(name, func(t *testing.T) {
					requireOwnershipRejected(t, ownershipConditionalRecordSource(width, reverseConstructor, borrowedFirst, "direct", "f9"))
				})
			}
			name := fmt.Sprintf("enum-%d-borrowed-first-%t", width, borrowedFirst)
			t.Run(name, func(t *testing.T) {
				requireOwnershipRejected(t, ownershipConditionalEnumSource(width, borrowedFirst, "direct", "f9"))
			})
		}
	}

	for _, mode := range []string{"helper", "recipe"} {
		for _, source := range []string{
			ownershipConditionalRecordSource(65, false, false, mode, "f9"),
			ownershipConditionalEnumSource(65, false, mode, "f9"),
		} {
			requireOwnershipRejected(t, source)
		}
	}
}

func TestOwnershipSamePathAlternativeKeepsSafeSiblingAdmitted(t *testing.T) {
	for _, width := range []int{64, 65, 80} {
		for _, reverseConstructor := range []bool{false, true} {
			requireOwnershipAccepted(t, ownershipConditionalRecordSource(width, reverseConstructor, false, "direct", fmt.Sprintf("f%d", width-1)))
		}
	}
}

func TestOwnershipNestedProjectionKeepsUncertaintyUntilTheTerminalPath(t *testing.T) {
	for _, layers := range []int{0, 1, 2, 3} {
		name := fmt.Sprintf("record-layers-%d", layers)
		t.Run(name, func(t *testing.T) {
			requireOwnershipRejected(t, ownershipNestedRecordSource(64, layers, true, "f7"))
		})
		name = fmt.Sprintf("enum-layers-%d", layers)
		t.Run(name, func(t *testing.T) {
			requireOwnershipRejected(t, ownershipNestedEnumSource(64, layers, true, "f7"))
		})
	}
}

func TestOwnershipNestedProjectionAdmitsCompleteSafeLeaves(t *testing.T) {
	for _, layers := range []int{0, 1, 2, 3} {
		name := fmt.Sprintf("record-layers-%d", layers)
		t.Run(name, func(t *testing.T) {
			requireOwnershipAccepted(t, ownershipNestedRecordSource(65, layers, false, "f64"))
		})
		name = fmt.Sprintf("enum-layers-%d", layers)
		t.Run(name, func(t *testing.T) {
			requireOwnershipAccepted(t, ownershipNestedEnumSource(65, layers, false, "f64"))
		})
	}
}

func TestOwnershipNestedSafeSiblingStaysIndependentFromIncompleteSibling(t *testing.T) {
	for _, selected := range []string{"unsafe", "safe"} {
		name := "select-" + selected
		t.Run(name, func(t *testing.T) {
			source := ownershipNestedSiblingSource(65, 2, selected)
			if selected == "safe" {
				requireOwnershipAccepted(t, source)
			} else {
				requireOwnershipRejected(t, source)
			}
		})
	}
}

func TestOwnershipCoverageRetainsOmittedSiblingUncertainty(t *testing.T) {
	for _, enum := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			name := fmt.Sprintf("enum-%t-reverse-%t", enum, reverse)
			t.Run(name, func(t *testing.T) {
				requireOwnershipRejected(t, ownershipCoverageSiblingSource(65, enum, reverse, false, false))
				requireOwnershipRejected(t, ownershipCoverageSiblingSource(65, enum, reverse, true, false))
				requireOwnershipAccepted(t, ownershipCoverageSiblingSource(65, enum, reverse, false, true))
			})
		}
	}
}

func TestOwnershipCoverageRetainsParameterProvenance(t *testing.T) {
	for _, enum := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			name := fmt.Sprintf("enum-%t-reverse-%t", enum, reverse)
			t.Run(name, func(t *testing.T) {
				source := ownershipCoverageBorrowedSummarySource(65, enum, reverse)
				requireOwnershipRejectedWithoutArgumentUse(t, source)
			})
		}
	}
}

func TestOwnershipJoinRetainsEveryBranchOwnerAtTheFactCap(t *testing.T) {
	for _, width := range []int{63, 64, 65, 66} {
		for _, enum := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				name := fmt.Sprintf("width-%d-enum-%t-reverse-%t", width, enum, reverse)
				t.Run(name, func(t *testing.T) {
					requireOwnershipRejected(t, ownershipJoinSiblingSource(width, enum, reverse, false))
					requireOwnershipAccepted(t, ownershipJoinSiblingSource(width, enum, reverse, true))
				})
			}
		}
	}
}

func TestOwnershipJoinThenHelperRetainsWildcardAlternatives(t *testing.T) {
	for _, width := range []int{63, 64} {
		width := width
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			requireOwnershipRejected(t, ownershipJoinHelperSource(width, false))
			if width == 63 {
				requireOwnershipAccepted(t, ownershipJoinHelperSource(width, true))
			}
		})
	}
}

func TestOwnershipSameParameterJoinKeepsAlternativeSources(t *testing.T) {
	for _, nested := range []bool{false, true} {
		for _, reverse := range []bool{false, true} {
			name := fmt.Sprintf("nested-%t-reverse-%t", nested, reverse)
			t.Run(name, func(t *testing.T) {
				requireOwnershipRejected(t, ownershipSameParameterJoinSource(nested, reverse, false))
				requireOwnershipAccepted(t, ownershipSameParameterJoinSource(nested, reverse, true))
			})
		}
	}
}

func TestOwnershipHelperRetainsEvictedParameterProvenance(t *testing.T) {
	for _, width := range []int{63, 64, 65, 66} {
		width := width
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			unsafe := requireOwnershipRejectedWithoutArgumentUse(t, ownershipHelperBoxSource(width, true))
			pick := unsafe.Find("pick")
			if pick == nil || len(pick.Actual.Ownership) != 1 {
				t.Fatalf("helper lost its returned ownership fact at width %d: %+v", width, pick)
			}
			fact := pick.Actual.Ownership[0]
			if fact.Status != "borrowed" || fact.Region != "parameter:file" {
				t.Fatalf("helper lost file parameter provenance at width %d: %+v", width, fact)
			}
			if width != 64 {
				requireOwnershipAccepted(t, ownershipHelperBoxSource(width, false))
			}
		})
	}
}

func TestOwnershipRemainderJoinProjectionLaws(t *testing.T) {
	borrowed := func(path string) OwnershipFact {
		return OwnershipFact{Path: path, Status: "borrowed", Region: "parameter:file", Origin: "parameter", sourceSet: true}
	}
	marker := func(exclusions ...string) OwnershipFact {
		return OwnershipFact{
			Path:                "*",
			Status:              "unknown",
			Origin:              "bounded",
			potentialOwner:      true,
			remainder:           true,
			remainderExclusions: encodeRemainderExclusions(exclusions),
		}
	}
	branchA := normalizeFacts([]OwnershipFact{marker("z"), borrowed("z")})
	branchB := normalizeFacts([]OwnershipFact{marker("a"), borrowed("z")})
	if projected := projectFacts(branchA, "z"); hasPotentialOwner(projected) {
		t.Fatalf("same-alternative exact override did not discharge its remainder: %+v", projected)
	}
	if projected := projectFacts(branchB, "z"); !hasPotentialOwner(projected) {
		t.Fatalf("remainder without a same-alternative override was discharged: %+v", projected)
	}

	joinedAB := mergeFacts(branchA, branchB)
	joinedBA := mergeFacts(branchB, branchA)
	if !slices.Equal(joinedAB, joinedBA) {
		t.Fatalf("join is not commutative:\nAB=%+v\nBA=%+v", joinedAB, joinedBA)
	}
	if !hasPotentialOwner(projectFacts(joinedAB, "z")) {
		t.Fatalf("join lost an alternative remainder at the selected terminal: %+v", joinedAB)
	}
	if !slices.Equal(joinedAB, mergeFacts(joinedAB, joinedAB)) {
		t.Fatalf("join is not idempotent: first=%+v twice=%+v", joinedAB, mergeFacts(joinedAB, joinedAB))
	}
	if !slices.Equal(branchA, normalizeFacts(normalizeFacts(branchA))) {
		t.Fatalf("normalization is not idempotent: first=%+v twice=%+v", branchA, normalizeFacts(normalizeFacts(branchA)))
	}

	branchC := normalizeFacts([]OwnershipFact{marker("z"), borrowed("z")})
	joinedAC := mergeFacts(branchA, branchC)
	if hasPotentialOwner(projectFacts(joinedAC, "z")) {
		t.Fatalf("identical remainder exclusions did not survive a join: %+v", joinedAC)
	}

	rebased := prependFacts("outer", branchA)
	if hasPotentialOwner(projectFacts(projectFacts(rebased, "outer"), "z")) {
		t.Fatalf("remainder exclusions were not rebased through nested projection: %+v", rebased)
	}
}

func TestOwnershipNormalizationCanonicalWithMultipleRemainders(t *testing.T) {
	facts := make([]OwnershipFact, 0, 8)
	for i := 0; i < 8; i++ {
		path := fmt.Sprintf("x%d.*", i)
		facts = append(facts, OwnershipFact{
			Path:                path,
			Status:              "unknown",
			Origin:              "bounded",
			potentialOwner:      true,
			remainder:           true,
			remainderExclusions: encodeRemainderExclusions([]string{fmt.Sprintf("x%d.z", i)}),
		})
	}
	first := normalizeFacts(facts)
	second := normalizeFacts(first)
	if !slices.Equal(first, second) {
		t.Fatalf("normalization changed canonical order or meaning:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestOwnershipBudgetWideningRetainsOverlappingOwnedWildcard(t *testing.T) {
	owned := []OwnershipFact{{Path: "*", Status: "owned", Region: "scope:inner", Origin: "bounded-all-owned", ownerKind: ownershipOwnerLexical}}
	borrowed := []OwnershipFact{{Path: "z", Status: "borrowed", Region: "parameter:file", Origin: "parameter", sourceSet: true}}
	for i := 0; i < 63; i++ {
		path := fmt.Sprintf("x%d.*", i)
		borrowed = append(borrowed, OwnershipFact{
			Path:                path,
			Status:              "unknown",
			Origin:              "bounded",
			potentialOwner:      true,
			remainder:           true,
			remainderExclusions: encodeRemainderExclusions([]string{fmt.Sprintf("x%d.z", i)}),
		})
	}
	borrowed = normalizeFacts(borrowed)
	joins := [][]OwnershipFact{
		mergeFacts(owned, borrowed),
		mergeFacts(borrowed, owned),
		mergeFacts(mergeFacts(owned, borrowed[:32]), borrowed[32:]),
	}
	for i, joined := range joins {
		projected := projectFacts(joined, "z")
		if !slices.ContainsFunc(projected, func(fact OwnershipFact) bool {
			return fact.Status == "owned"
		}) {
			t.Fatalf("join %d dropped an owned wildcard covering z: joined=%+v projected=%+v", i, joined, projected)
		}
	}
}

func TestOwnershipInstantiationKeepsRemainderRelativeToTheReturnedShape(t *testing.T) {
	argument := ValueType{Ownership: []OwnershipFact{{
		Path:                "*",
		Status:              "unknown",
		Origin:              "bounded",
		potentialOwner:      true,
		remainder:           true,
		remainderExclusions: encodeRemainderExclusions([]string{"wide.f"}),
	}}}
	summary := []OwnershipFact{{
		Path:                "box.f",
		Status:              "borrowed",
		Region:              "parameter:wide",
		Origin:              "parameter",
		source:              "wide.f",
		sourceSet:           true,
		remainder:           true,
		remainderExclusions: encodeRemainderExclusions([]string{"box.f"}),
	}}
	got := instantiateFacts(summary, []Param{{Name: "wide"}}, []ValueType{argument})
	if len(got) != 1 || !got[0].potentialOwner {
		t.Fatalf("wildcard argument uncertainty was lost during helper substitution: %+v", got)
	}
	if got[0].remainderExclusions != encodeRemainderExclusions([]string{"box.f"}) {
		t.Fatalf("returned-shape exclusions were not retained: %+v", got)
	}
	if strings.Contains(got[0].remainderExclusions, "wide.f") {
		t.Fatalf("argument-relative exclusion leaked into returned shape: %+v", got)
	}
}

func TestOwnershipCoverageRetainsEveryTerminalAroundTheFactCap(t *testing.T) {
	for _, width := range []int{63, 64, 65, 66} {
		width := width
		for _, layers := range []int{0, 1, 2} {
			layers := layers
			for _, enum := range []bool{false, true} {
				enum := enum
				name := fmt.Sprintf("width-%d-layers-%d-enum-%t", width, layers, enum)
				t.Run(name, func(t *testing.T) {
					for position := 0; position < width; position++ {
						position := position
						t.Run(fmt.Sprintf("position-%d-safe", position), func(t *testing.T) {
							requireOwnershipAccepted(t, ownershipCoverageTerminalSource(width, layers, enum, position, position))
						})
						if position == 0 {
							continue
						}
						t.Run(fmt.Sprintf("position-%d-unsafe", position), func(t *testing.T) {
							requireOwnershipRejected(t, ownershipCoverageTerminalSource(width, layers, enum, position, 0))
						})
					}
				})
			}
		}
	}
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
