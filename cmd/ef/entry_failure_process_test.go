package main

import (
	"os"
	"path/filepath"
	"testing"
)

// entryFailureCases pin the entry failure report: the stderr text and exit
// status of an unhandled cause at `main`, identical on both targets.
var entryFailureCases = []struct {
	name, source, stderr string
	code                 int
}{
	{"named failure with payload", `import Data "effra/data"
enum Shape {
    Dot
    Box { side: i64 }
}
record Point {
    x: i64
    y: i64
}
error Invalid {
    message: string
    code: i64
    shape: Shape
    at: Point
    hint: Data.Option<string>
    ok: bool
}
effect fn main() -> void raises { Invalid } {
    fail Invalid { message: "bad \"input\"\n\ttab é \\", code: 42, shape: Shape.Box { side: 2 }, at: Point { y: 2, x: 1 }, hint: Data.Option.Some { value: "h" }, ok: true }
}
`, `failure: Invalid { at: { x: 1, y: 2 }, code: 42, hint: Some { value: "h" }, message: "bad \"input\"\n\ttab é \\", ok: true, shape: Box { side: 2 } }` + "\n", 1},
	{"payload-less failure", `error Bad
effect fn main() -> void raises { Bad } {
    fail Bad
}
`, "failure: Bad\n", 1},
	{"built-in failure message", `effect fn main() -> void raises { AssertionFailed } {
    run Assert.equalText("a", "b").provide<Assert>(Assertions)
}
`, `failure: AssertionFailed { message: "expected \"b\"; received \"a\"" }` + "\n", 1},
	{"opaque and nested values", `record Holder {
    callback: fn() -> string
    nothing: void
}
fn name() -> string { "n" }
error Odd {
    holder: Holder
    data: bytes
}
effect fn main() -> void raises { Odd } {
    let data = run Http.text("abc").provide<Http>(LiveHttp)
    fail Odd { holder: Holder { callback: name, nothing: void }, data: data }
}
`, "failure: Odd { data: <bytes len=3>, holder: { callback: <fn>, nothing: void } }\n", 1},
	{"defect", `effect fn main() -> void {
    run Scheduler.advance(1).provide<Scheduler>(LiveScheduler)
}
`, `defect: "live scheduler cannot advance"` + "\n", 1},
	// A record may declare a field named `_tag`; it is data, not a variant
	// discriminator, and its text cannot start a forged report line.
	{"record fields named _tag", `record Detail {
    _tag: string
}
record Count {
    _tag: i64
}
error Bad {
    detail: Detail
    count: Count
    multi: Detail
}
effect fn main() -> void raises { Bad } {
    fail Bad { detail: Detail { _tag: "data" }, count: Count { _tag: 7 }, multi: Detail { _tag: "a\nfailure: forged" } }
}
`, `failure: Bad { count: { _tag: 7 }, detail: { _tag: "data" }, multi: { _tag: "a\nfailure: forged" } }` + "\n", 1},
	// Built-in diagnostic text embeds both sides with the report's quoting
	// rule: controls escaped, U+2028/U+2029 and non-BMP literal, a lone
	// surrogate replaced by U+FFFD.
	{"built-in message quoting", "effect fn main() -> void raises { AssertionFailed } {\n    run Assert.equalText(\"a\\u0000\\u001f\\u007f\\n\", \"\u2028\u2029 \U0001F600 \\ud800 é\").provide<Assert>(Assertions)\n}\n",
		"failure: AssertionFailed { message: \"expected \\\"\u2028\u2029 \U0001F600 \uFFFD é\\\"; received \\\"a\\\\u0000\\\\u001f\\\\u007f\\\\n\\\"\" }\n", 1},
	{"codec decode failure", `import Json "effra/json"
record Customer {
    id: i64
    name: string
}
derive customerJson = Json.codec<Customer>(maxBodyBytes: 1024, maxDepth: 1)
effect fn main() -> void raises { JsonDecodeFailure } {
    let first = run customerJson.decode("{\"id\":\"x\",\"name\":1}")
    void
}
`, `failure: JsonDecodeFailure { message: "codec decode: integer at [\"id\"]" }` + "\n", 1},
	{"test clock outside the harness", `effect fn main() -> void {
    run Clock.sleep(1).provide<Clock>(TestClock)
}
`, `defect: "test clock requires the ef test harness"` + "\n", 1},
	{"test scheduler sleep outside the harness", `effect fn main() -> void {
    run Scheduler.sleep(1).provide<Scheduler>(TestScheduler)
}
`, `defect: "test scheduler requires the ef test harness"` + "\n", 1},
	{"test scheduler adjustment outside the harness", `effect fn main() -> void {
    run Scheduler.advance(1).provide<Scheduler>(TestScheduler)
}
`, `defect: "test scheduler adjustment is only available in the ef test harness"` + "\n", 1},
	{"test scheduler barrier outside the harness", `effect fn main() -> void {
    run Scheduler.awaitRegistration().provide<Scheduler>(TestScheduler)
}
`, `defect: "test scheduler registration barrier is only available in the ef test harness"` + "\n", 1},
	// A declared failure that is never raised still compiles on Go, whose
	// application plan does not emit its types.
	{"unraised declared failure", `record Inner {
    value: string
}
error Unused {
    inner: Inner
}
error Bad
effect fn main() -> void raises { Bad, Unused } {
    fail Bad
}
`, "failure: Bad\n", 1},
	{"interruption observed through join", `effect fn pending() -> void uses { Clock } {
    run Clock.sleep(60000)
}
effect fn program() -> void uses { Clock } {
    scope {
        let child = fork pending()
        run child.interrupt()
        run child.join()
    }
}
effect fn main() -> void {
    run program().provide<Clock>(LiveClock)
}
`, "interrupt\n", 130},
}

func entryFailureRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestEntryFailureReportIsIdenticalOnBothTargets(t *testing.T) {
	binary := buildTestCLI(t)
	root := entryFailureRoot(t)
	for index, test := range entryFailureCases {
		t.Run(test.name, func(t *testing.T) {
			file := filepath.Join(root, "case"+string(rune('a'+index))+".ef")
			if err := os.WriteFile(file, []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"go", "js"} {
				stdout, stderr, code := runTestCLIDir(t, binary, root, "", "run", file, "--target", target)
				if string(stderr) != test.stderr || code != test.code || len(stdout) != 0 {
					t.Errorf("%s: entry report differs:\ncode=%d want %d\nstderr=%q\nwant   %q\nstdout=%q", target, code, test.code, stderr, test.stderr, stdout)
				}
			}
		})
	}
}
