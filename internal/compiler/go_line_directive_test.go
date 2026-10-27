package compiler

import (
	"bufio"
	"fmt"
	"go/format"
	goscanner "go/scanner"
	gotoken "go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"ergo.local/prototype/internal/gosyntax"
)

// lineDirectiveProbe reaches two stack traces the Go runtime prints itself:
// a host call that prints the calling goroutine's stack, and a pure
// recursion that exhausts a lowered stack limit, which Go reports as a fatal
// stack overflow that no defect boundary can recover. Each user frame must
// name its .ef line; generated glue and runtime frames keep their Go paths.
const lineDirectiveProbe = `import go debug "runtime/debug"

fn depth(n: i64) -> i64 {
    1 + depth(n + 1)
}

effect fn trace() -> void uses { Foreign } {
    run debug.PrintStack()
}

effect fn deep() -> i64 uses { Foreign } {
    let previous = run debug.SetMaxStack(262144)
    depth(0)
}

effect fn main() -> void {
    run trace().provide<Foreign>(Host)
    let total = run deep().provide<Foreign>(Host)
    void
}
`

// stackFrame is one frame of a Go traceback: the function line and the
// file:line position printed beneath it, without the PC offset.
type stackFrame struct{ function, position string }

func tracebackFrames(output string) []stackFrame {
	frames := []stackFrame{}
	scanner := bufio.NewScanner(strings.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	function := ""
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "\t") {
			function = line
			continue
		}
		if function == "" {
			continue
		}
		position, _, _ := strings.Cut(strings.TrimPrefix(line, "\t"), " ")
		frames = append(frames, stackFrame{function, position})
		function = ""
	}
	return frames
}

// The Go toolchain reports user frames at .ef lines. Origins in different
// checkouts name the same module-relative file, so the traceback reads
// ergo.generated/probe.ef under ergo build's -trimpath.
func TestGoLineDirectivesAttributeStackTracesToSource(t *testing.T) {
	r := CompileAt(lineDirectiveProbe, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	application, err := r.GoApplicationFrom(GoGenerationBuild, "/nonexistent/loop-probe-x/probe.ef")
	if err != nil {
		t.Fatal(err)
	}
	output, err := execGoModule(t, application, r, map[string][]byte{"main.go": application.Main})
	if err == nil || !strings.Contains(string(output), "fatal error: stack overflow") {
		t.Fatalf("probe did not reach the fatal stack overflow: %v\n%s", err, output)
	}
	frames := tracebackFrames(string(output))
	// Each user function's frames sit exactly at its statements: main is on
	// the stack at the trace call (line 17) and at the deep call (line 18).
	// The overflowing depth frame is in its entry stack check, which Go
	// places at the declaration (line 3); every caller is at the recursive
	// call (line 4).
	want := map[string][]string{
		"main.efFunction_trace.": {"ergo.generated/probe.ef:8"},
		"main.efFunction_main.":  {"ergo.generated/probe.ef:17", "ergo.generated/probe.ef:18"},
		"main.efFunction_depth(": {"ergo.generated/probe.ef:3", "ergo.generated/probe.ef:4"},
	}
	for prefix, positions := range want {
		seen := map[string]bool{}
		for _, frame := range frames {
			if strings.HasPrefix(frame.function, prefix) {
				seen[frame.position] = true
			}
		}
		for _, position := range positions {
			if !seen[position] {
				t.Errorf("no %s frame at %s", prefix, position)
			}
			delete(seen, position)
		}
		for position := range seen {
			t.Errorf("%s frame at %s, want only %v", prefix, position, positions)
		}
	}
	if t.Failed() {
		t.Logf("traceback:\n%s", output)
	}
	glue := false
	for _, frame := range frames {
		switch {
		case strings.HasPrefix(frame.function, "ergo.generated/runtime."):
			if !strings.HasPrefix(frame.position, "ergo.generated/runtime/") || !strings.Contains(frame.position, ".go:") {
				t.Errorf("runtime frame %s lost its Go position: %s", frame.function, frame.position)
			}
		case frame.function == "main.main()":
			glue = true
			if !strings.HasPrefix(frame.position, "ergo.generated/main.go:") {
				t.Errorf("generated entry frame at %s, want generated main.go", frame.position)
			}
		case strings.HasPrefix(frame.function, "runtime/debug."):
			if !strings.HasPrefix(frame.position, "runtime/debug/") {
				t.Errorf("standard library frame %s at %s", frame.function, frame.position)
			}
		}
	}
	if !glue {
		t.Errorf("no generated main.main frame in traceback:\n%s", output)
	}
}

// sourcePosition is the file and line a token is meant to have, or that Go
// assigns it.
type sourcePosition struct {
	file string
	line int
}

// intendedPositions reads the emitter's marks in a marked lowering and lists,
// for every compiled token, the position the emitter means it to have: the
// user source line of the mark before it, or the zero position for glue,
// which must keep its own main.go line.
func intendedPositions(t *testing.T, marked []byte) []sourcePosition {
	t.Helper()
	positions, current := []sourcePosition{}, sourcePosition{}
	if err := scanGoTokens(marked, func(_, _ int, token gotoken.Token, literal string) error {
		switch {
		case token == gotoken.COMMENT && literal == goLineReset:
			current = sourcePosition{}
		case token == gotoken.COMMENT && strings.HasPrefix(literal, "//line "):
			colon := strings.LastIndexByte(literal, ':')
			line, err := strconv.Atoi(literal[colon+1:])
			if err != nil {
				t.Fatalf("malformed mark %q", literal)
			}
			current = sourcePosition{literal[len("//line "):colon], line}
		case token != gotoken.COMMENT && token != gotoken.SEMICOLON:
			positions = append(positions, current)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return positions
}

// assignedPositions lists, for every compiled token of src, the position Go
// assigns it after applying src's line directives, the line it actually
// occupies in src, and the token itself.
func assignedPositions(t *testing.T, src []byte) ([]sourcePosition, []int, []string) {
	t.Helper()
	file := gotoken.NewFileSet().AddFile("main.go", -1, len(src))
	var scanner goscanner.Scanner
	scanner.Init(file, src, func(position gotoken.Position, message string) { t.Fatalf("%s: %s", position, message) }, goscanner.ScanComments)
	positions, raw, tokens := []sourcePosition{}, []int{}, []string{}
	for {
		at, token, literal := scanner.Scan()
		if token == gotoken.EOF {
			return positions, raw, tokens
		}
		if token == gotoken.COMMENT || token == gotoken.SEMICOLON {
			continue
		}
		assigned := file.PositionFor(at, true)
		positions = append(positions, sourcePosition{assigned.Filename, assigned.Line})
		raw = append(raw, file.PositionFor(at, false).Line)
		tokens = append(tokens, strings.TrimSpace(token.String()+" "+literal))
	}
}

// Every compiled token of every example's generated main, in both entry
// modes, sits exactly where the emitter meant it: a user token at the .ef
// line of its declaration or statement, a glue token at its own main.go line.
func TestGoLineDirectivesPutEveryTokenAtItsIntendedPosition(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil || len(files) == 0 {
		t.Fatal("no authored examples", err)
	}
	checked := 0
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r := CompileAt(string(source), "go", "../..")
		if !r.Checked {
			continue
		}
		name := filepath.Base(path)
		for _, mode := range []GoGenerationMode{GoGenerationBuild, GoGenerationTest} {
			plan, err := r.ApplicationPlan(mode)
			if err != nil {
				continue
			}
			marked, err := r.emitMarkedGo(plan, name)
			if err != nil {
				t.Fatalf("%s %s: %v", name, mode, err)
			}
			application, err := r.GoApplicationFrom(mode, filepath.Join("/nonexistent/loop-probe-x", name))
			if err != nil {
				t.Fatalf("%s %s: %v", name, mode, err)
			}
			intended := intendedPositions(t, marked)
			assigned, raw, tokens := assignedPositions(t, application.Main)
			if len(intended) != len(assigned) {
				t.Errorf("%s %s: %d marked tokens, %d generated", name, mode, len(intended), len(assigned))
				continue
			}
			wrong, examples := 0, []string{}
			for index, want := range intended {
				if want == (sourcePosition{}) {
					want = sourcePosition{"main.go", raw[index]}
				}
				if assigned[index] != want {
					wrong++
					if len(examples) < 3 {
						examples = append(examples, fmt.Sprintf("%q at %s:%d, want %s:%d", tokens[index], assigned[index].file, assigned[index].line, want.file, want.line))
					}
				}
			}
			if wrong > 0 {
				t.Errorf("%s %s: %d of %d tokens misplaced: %s", name, mode, wrong, len(assigned), strings.Join(examples, "; "))
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no example lowered")
	}
}

// An origin whose name Go would misread in a directive is refused rather
// than attributed to the wrong file or line.
func TestGoLineDirectivesRefuseUnnameableOrigins(t *testing.T) {
	r := CompileAt(lineDirectiveProbe, "go", "../..")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	for _, origin := range []string{"", "/", "/nonexistent/loop-probe-x/probe:12", "/nonexistent/loop-probe-x/probe:0", "/nonexistent/loop-probe-x/a\nb.ef", "/nonexistent/loop-probe-x/a\rb.ef"} {
		if _, err := r.GoApplicationFrom(GoGenerationBuild, origin); err == nil {
			t.Errorf("origin %q was admitted", origin)
		}
	}
	// Go reads these names whole: the text after the last colon is not a
	// number (cmd/compile/internal/syntax/parser.go updateBase).
	for _, name := range []string{"probe:v2.ef", "probe:12.ef", "probe:", "probe:-1", "main.go"} {
		application, err := r.GoApplicationFrom(GoGenerationBuild, filepath.Join("/nonexistent/loop-probe-x", name))
		if err != nil {
			t.Errorf("%q was refused: %v", name, err)
			continue
		}
		assigned, _, _ := assignedPositions(t, application.Main)
		if !slices.Contains(assigned, sourcePosition{name, 8}) {
			t.Errorf("Go does not read %q:8 from the directives", name)
		}
	}
}

// syntaxTree prints src's syntax tree up to layout and comments
// (gosyntax.Tree).
func syntaxTree(t *testing.T, src []byte) string {
	t.Helper()
	tree, err := gosyntax.Tree(src)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

// Every example's generated main, built and test entries alike:
//   - is byte-identical from two different absolute checkouts;
//   - parses to the unnamed lowering's syntax tree, so only comments and
//     layout change;
//   - is gofmt-formatted, with its directives intact;
//   - carries only well-formed directives at column 1, each naming a line
//     of the source or the true next line of main.go.
func TestGoLineDirectivesAreDeterministicCommentOnlyAndWellFormed(t *testing.T) {
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil || len(files) == 0 {
		t.Fatal("no authored examples", err)
	}
	attributed := 0
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r := CompileAt(string(source), "go", "../..")
		if !r.Checked {
			continue
		}
		name := filepath.Base(path)
		sourceLines := strings.Count(string(source), "\n") + 1
		for _, mode := range []GoGenerationMode{GoGenerationBuild, GoGenerationTest} {
			unnamed, err := r.GoApplication(mode)
			if err != nil {
				continue
			}
			first, err := r.GoApplicationFrom(mode, filepath.Join(t.TempDir(), "checkout-a", "examples", name))
			if err != nil {
				t.Fatalf("%s %s: %v", name, mode, err)
			}
			second, err := r.GoApplicationFrom(mode, filepath.Join(t.TempDir(), "elsewhere", "b", name))
			if err != nil {
				t.Fatalf("%s %s: %v", name, mode, err)
			}
			code := string(first.Main)
			if code != string(second.Main) {
				t.Errorf("%s %s: generated Go depends on the checkout path", name, mode)
			}
			if syntaxTree(t, first.Main) != syntaxTree(t, unnamed.Main) {
				t.Errorf("%s %s: generated Go parses to a different program from the unnamed lowering", name, mode)
			}
			if formatted, err := format.Source(first.Main); err != nil || string(formatted) != code {
				t.Errorf("%s %s: generated Go is not gofmt-formatted (%v)", name, mode, err)
			}
			for index, line := range strings.Split(code, "\n") {
				if !strings.Contains(line, "//line ") || strings.Contains(line, `"`) {
					continue
				}
				file, number, ok := strings.Cut(strings.TrimPrefix(line, "//line "), ":")
				value, err := strconv.Atoi(number)
				switch {
				case !strings.HasPrefix(line, "//line ") || !ok || err != nil:
					t.Errorf("%s %s:%d: malformed directive %q", name, mode, index+1, line)
				case file == name:
					attributed++
					if value < 1 || value > sourceLines {
						t.Errorf("%s %s:%d: directive names line %d of a %d-line source", name, mode, index+1, value, sourceLines)
					}
				case file == "main.go":
					if value != index+2 {
						t.Errorf("%s %s:%d: generated position %d, want %d", name, mode, index+1, value, index+2)
					}
				default:
					t.Errorf("%s %s:%d: directive names %q", name, mode, index+1, file)
				}
			}
		}
	}
	if attributed == 0 {
		t.Fatal("no example attributed generated Go to its source")
	}
}
