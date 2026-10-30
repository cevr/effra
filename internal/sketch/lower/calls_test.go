package lower

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"ergo.local/prototype/internal/receipt"
)

const callsSource = `type Missing { name string }

type Store {
	Find(name string) string fails Missing,
}

type Mem {}

func Find(_ Mem, name string) string fails Missing {
	if name == "a" {
		return "A"
	}
	return Missing{name: name}
}

func Label(m Mem, s string) string fails Missing {
	return try m.Find(s)
}

let Live = wire {
	Store: Mem,
}

func double(n int) int {
	return n * 2
}

func lookup(name string) string fails Missing needs Store {
	return try Store.Find(name)
}

func apply(f func(string) string fails Missing, s string) string fails Missing {
	return try f(s)
}

func main() needs Console {
	let n = double(2)
	with Live {
		let v = lookup("a") catch {
		case Missing:
			"none"
		}
		Console.Println(v)
	}
	let m = Mem
	let w = apply(func(s string) string fails Missing { return try m.Label(s) }, "a") catch {
	case Missing:
		"none"
	}
	Console.Println(w)
	if n == 4 {
		Console.Println("four")
	}
}
`

// TestCallIdentity: each lowered call that can fail or uses a capability
// keeps its Ergo identity (#230): the declaration that makes it, what it
// calls and its position. The emitted package lists the identities in
// source order in ergoCalls; the Go line of each such call follows the
// //line directive of its own position; the table is reachable from the
// lowered package at run time; and a pure call has no identity.
func TestCallIdentity(t *testing.T) {
	o := mustLower(t, "calls", callsSource)
	code := o.mainGo()
	at := func(snippet string, nth int) (int, int) {
		i := -1
		for range nth {
			j := strings.Index(callsSource[i+1:], snippet)
			if j < 0 {
				t.Fatalf("snippet %q not found", snippet)
			}
			i += 1 + j
		}
		line := strings.Count(callsSource[:i], "\n") + 1
		return line, i - strings.LastIndex(callsSource[:i], "\n")
	}
	type want struct {
		fn, callee, snippet string
		nth                 int
		goCall              string // text of the Go line that makes the call
	}
	wants := []want{
		{"Mem.Label", "Mem.Find", "m.Find(s)", 1, ".Find(s)"},
		{"lookup", "Store.Find", "Store.Find(name)", 1, ".Store.Find(name)"},
		{"apply", "f", "f(s)", 1, " f("},
		{"main", "lookup", `lookup("a")`, 1, `lookup(`},
		{"main", "Console.Println", "Console.Println(v)", 1, ".Println(v"},
		{"main", "apply", "apply(func", 1, "apply("},
		{"main", "Mem.Label", "m.Label(s)", 1, ".Label("},
		{"main", "Console.Println", "Console.Println(w)", 1, ".Println(w)"},
		{"main", "Console.Println", `Console.Println("four")`, 1, `.Println("four")`},
	}
	var wantTable []string
	for _, w := range wants {
		line, col := at(w.snippet, w.nth)
		wantTable = append(wantTable, fmt.Sprintf("%s %s %s:%d:%d", w.fn, w.callee, testSourcePath, line, col))
	}

	entry := regexp.MustCompile(`\{Func: "([^"]*)", Callee: "([^"]*)", File: "([^"]*)", Line: (\d+), Column: (\d+)\}`)
	var gotTable []string
	for _, m := range entry.FindAllStringSubmatch(code, -1) {
		gotTable = append(gotTable, fmt.Sprintf("%s %s %s:%s:%s", m[1], m[2], m[3], m[4], m[5]))
	}
	if strings.Join(gotTable, "\n") != strings.Join(wantTable, "\n") {
		t.Errorf("call table:\n%s\nwant:\n%s\nGo:\n%s", strings.Join(gotTable, "\n"), strings.Join(wantTable, "\n"), code)
	}
	if strings.Contains(code, "double") && regexp.MustCompile(`Callee: "double"`).MatchString(code) {
		t.Error("the pure call double(2) has an identity")
	}

	// Each identified call's Go line follows its own directive.
	lines := strings.Split(code, "\n")
	for i, w := range wants {
		line, col := at(w.snippet, w.nth)
		directive := fmt.Sprintf("//line %s:%d:%d", testSourcePath, line, col)
		found := false
		for j, l := range lines {
			if l == directive && j+1 < len(lines) && strings.Contains(lines[j+1], w.goCall) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("entry %d (%s): no Go line holding %q directly after %q", i, w.callee, w.goCall, directive)
		}
	}

	// The table is reachable from the lowered package when it runs.
	module := t.TempDir()
	write := func(rel string, data []byte) {
		p := filepath.Join(module, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range append(Runtime(), o.files...) {
		write(f.Path, f.Data)
	}
	write("dump.go", []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc init() {\n\tif os.Getenv(\"ERGO_DUMP_CALLS\") == \"\" {\n\t\treturn\n\t}\n\tfor _, c := range ergoCalls {\n\t\tfmt.Printf(\"%s %s %s:%d:%d\\n\", c.Func, c.Callee, c.File, c.Line, c.Column)\n\t}\n\tos.Exit(0)\n}\n"))
	exe := filepath.Join(t.TempDir(), "calls")
	if out, err := receipt.GoBuildCommand(module, exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	run := func(dump bool) string {
		cmd := exec.Command(exe)
		if dump {
			cmd.Env = append(os.Environ(), "ERGO_DUMP_CALLS=1")
		}
		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		if err := cmd.Run(); err != nil {
			t.Fatalf("run (dump %v): %v", dump, err)
		}
		return stdout.String()
	}
	if got := run(false); got != "A\nA\nfour\n" {
		t.Errorf("program output %q; want A, A, four", got)
	}
	if got, want := run(true), strings.Join(wantTable, "\n")+"\n"; got != want {
		t.Errorf("the running program's table:\n%s\nwant:\n%s", got, want)
	}
}
