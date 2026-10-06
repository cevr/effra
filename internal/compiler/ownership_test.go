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

const ownershipPayloadAndBranchCases = `
error WithFile { file: File }
record Envelope { file: File, label: string }
enum Packet { Full { file: File }, Empty }
fn identity(file: File) -> File { file }
fn identityEnvelope(envelope: Envelope) -> Envelope { envelope }
effect fn acquire() -> File throws {IoError} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn payloadEscape() -> Envelope throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  Envelope { file: file, label: "inner" }
 }
}
effect fn enumEscape() -> Packet throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  Packet.Full { file: file }
 }
}
effect fn failurePayloadEscape() -> string throws {WithFile, IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  fail WithFile { file: file }
 }
}
effect fn conditionalEscape(file: File, choose: bool) -> File throws {IoError} uses {Files} {
 scope {
  let inner = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  if choose { inner } else { file }
 }
}
effect fn helperEscape() -> File throws {IoError} uses {Files} {
 scope { run acquire() }
}
effect fn aliasHelperEscape() -> File throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  identity(file)
 }
}
effect fn payloadHelperEscape() -> Envelope throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  identityEnvelope(Envelope { file: file, label: "helper" })
 }
}
effect fn projectionSafe() -> string throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let envelope = Envelope { file: file, label: "safe" }
  envelope.label
 }
}
effect fn enumProjectionSafe() -> string throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let packet = Packet.Full { file: file }
  match packet { Packet.Full { file } => run Files.readText(file) Packet.Empty => "empty" }
 }
}
effect fn readLater(file: File) -> string throws {IoError} uses {Files} {
 run Files.readText(file)
}
effect fn deferredCaptureEscape() -> string throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  readLater(file)
 }
}
impl Capturing(file: File) for Files {
 effect fn openRead(path: string) -> File { file }
 effect fn readText(file: File) -> string { "captured" }
 effect fn readFile(path: string) -> string { "captured" }
}
effect fn providerCaptureEscape() -> string throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  let provider = run Capturing(file)
  Files.readText(file).provide<Files>(provider)
 }
}
effect fn childFile() -> File throws {IoError} uses {Files} {
 run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
}
effect fn childEscape() -> File throws {IoError} uses {Files} {
 let child = fork childFile()
 run child.join()
}
effect fn childUseAfterJoin() -> string throws {IoError} uses {Files} {
 let child = fork childFile()
 let file = run child.join()
 run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn fiberHandleEscape() -> () throws {IoError} uses {Files} {
 scope {
  let child = fork childFile()
  child
 }
}
effect fn forwardHelperEscape() -> File throws {IoError} uses {Files} {
 scope {
  let file = run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)
  identityLater(file)
 }
}
fn identityLater(file: File) -> File { file }
effect fn main() -> () { () }
`

func TestOwnershipTracksPayloadHelpersAndConditionals(t *testing.T) {
	r := Compile(ownershipPayloadAndBranchCases)
	for _, name := range []string{"payloadEscape", "enumEscape", "failurePayloadEscape", "conditionalEscape", "helperEscape", "aliasHelperEscape", "payloadHelperEscape"} {
		symbol := r.Find(name)
		if symbol == nil {
			t.Fatalf("missing ownership case %s: %+v", name, r.Diagnostics)
		}
	}
	for _, name := range []string{"deferredCaptureEscape", "providerCaptureEscape", "childEscape", "childUseAfterJoin", "fiberHandleEscape", "forwardHelperEscape"} {
		if symbol := r.Find(name); symbol == nil {
			t.Fatalf("missing extended ownership case %s: %+v", name, r.Diagnostics)
		}
	}
	if !hasCode(r, "EF123") {
		t.Fatalf("expected ownership diagnostics for proven payload/helper/branch escapes: %+v", r.Diagnostics)
	}
	if hasDiagnosticAt(r, "EF123", r.Find("projectionSafe").Span) {
		t.Fatalf("field projection should discard an owned sibling: %+v", r.Diagnostics)
	}
	if hasDiagnosticAt(r, "EF123", r.Find("enumProjectionSafe").Span) {
		t.Fatalf("enum field projection should discard an owned sibling: %+v", r.Diagnostics)
	}
}

func hasDiagnosticAt(r *Result, code string, span Span) bool {
	for _, diagnostic := range r.Diagnostics {
		if diagnostic.Code == code && diagnostic.Span == span {
			return true
		}
	}
	return false
}
