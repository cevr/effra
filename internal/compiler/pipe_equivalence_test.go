package compiler

import (
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The pipe has no semantics of its own, so rewriting every call that has a
// positional first argument and a static callee into pipe form must leave the
// compiled output untouched. The corpus is every .ef file in the repository
// and every Effra program written as a raw string in a Go test.

type pipeCorpusSource struct {
	name, dir, text string
}

// pipeCorpus gathers the programs the equivalence differential rewrites.
func pipeCorpus(t *testing.T) []pipeCorpusSource {
	t.Helper()
	root := filepath.Join("..", "..")
	var corpus []pipeCorpusSource
	seen := map[string]bool{}
	add := func(name, dir, text string) {
		if seen[text] {
			return
		}
		seen[text] = true
		corpus = append(corpus, pipeCorpusSource{name, dir, text})
	}
	// Authored programs and Go tests live in the module's source trees, walked
	// in the order a walk of the repository root would reach them. Listing
	// the root itself would make the corpus, and this package's test cache,
	// depend on build outputs (dist/, bin/) that other processes rewrite.
	var err error
	for _, tree := range []string{"cmd", "examples", "internal", "lint", "runtime"} {
		err = filepath.WalkDir(filepath.Join(root, tree), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && (entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == "upstream") {
				return filepath.SkipDir
			}
			switch {
			case strings.HasSuffix(path, ".ef"):
				text, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				add(path, filepath.Dir(path), string(text))
			case strings.HasSuffix(path, "_test.go"):
				file, err := goparser.ParseFile(gotoken.NewFileSet(), path, nil, 0)
				if err != nil {
					return err
				}
				ast.Inspect(file, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != gotoken.STRING || !strings.HasPrefix(lit.Value, "`") {
						return true
					}
					if text, err := strconv.Unquote(lit.Value); err == nil && strings.Contains(text, "(") {
						add(path, ".", text)
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	return corpus
}

// expressionsOf returns every expression reachable through exported program
// fields, once each, in source order.
func expressionsOf(program *Program) []*Expr {
	var found []*Expr
	seen := map[uintptr]bool{}
	var visit func(reflect.Value)
	visit = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() || seen[v.Pointer()] {
				return
			}
			seen[v.Pointer()] = true
			if expr, ok := v.Interface().(*Expr); ok {
				found = append(found, expr)
			}
			visit(v.Elem())
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Type().Field(i).IsExported() {
					visit(v.Field(i))
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				visit(v.Index(i))
			}
		case reflect.Map:
			for _, key := range v.MapKeys() {
				visit(v.MapIndex(key))
			}
		}
	}
	visit(reflect.ValueOf(program))
	sort.SliceStable(found, func(i, j int) bool { return found[i].Extent.Offset < found[j].Extent.Offset })
	return found
}

// staticPath reports whether e is a name path, the only callee a pipe can name.
func staticPath(e *Expr) bool {
	switch e.Kind {
	case "name":
		return !pipeHeadReserved[e.Name]
	case "member":
		return staticPath(e.Left)
	}
	return false
}

// pipeRewriter turns each rewritable call f(a, rest) of one source into
// a |> f(rest). A call is rewritable when its callee is a static path and its
// first argument is positional. A call whose text holds a comment is left
// alone so the rewrite never has to carry a comment.
type pipeRewriter struct {
	source   string
	calls    []*Expr
	operands map[*Expr]bool
	keep     map[*Expr]bool
	bounds   map[*Expr]pipeBounds
}

// pipeBounds locates one call in the source: the callee's first byte, the
// opening parenthesis of its arguments and the byte after the closing one.
// Parentheses around the whole call are not part of it.
type pipeBounds struct{ lo, open, hi int }

func newPipeRewriter(source string, program *Program) *pipeRewriter {
	r := &pipeRewriter{source: source, operands: map[*Expr]bool{}, keep: map[*Expr]bool{}, bounds: map[*Expr]pipeBounds{}}
	for _, e := range expressionsOf(program) {
		switch {
		case e.Kind == "binary":
			r.operands[e.Left], r.operands[e.Right] = true, true
		case e.Kind == "run" || e.Kind == "fork":
			// Keep a rewritten recipe call grouped when run/fork is adjacent
			// to a binary operator, so the pipe is not ambiguous with it.
			r.operands[e.Left] = true
		case e.Kind == "call" && r.rewritable(e):
			r.calls = append(r.calls, e)
		}
	}
	return r
}

func (r *pipeRewriter) text(span Span) string { return r.source[span.Offset : span.Offset+span.Length] }

func (r *pipeRewriter) rewritable(call *Expr) bool {
	// A call already written with a pipe has its subject before the callee.
	if call.PipeSpan.Length > 0 || len(call.Args) == 0 || !staticPath(call.Left) || strings.HasPrefix(r.text(call.Left.Extent), "(") {
		return false
	}
	lo := call.Left.Extent.Offset
	open := lo + strings.Index(r.source[lo:], "(")
	hi := closingParenthesis(r.source, open)
	if hi < 0 || strings.Contains(r.source[lo:hi], "//") || strings.Contains(r.source[lo:hi], "/*") {
		return false
	}
	r.bounds[call] = pipeBounds{lo, open, hi}
	for _, field := range call.Fields {
		if field.Value == call.Args[0] {
			return false
		}
	}
	return true
}

// render copies source[lo:hi], replacing the outermost rewritable calls inside.
func (r *pipeRewriter) render(lo, hi int) string {
	var out strings.Builder
	at := lo
	for _, call := range r.calls {
		start, end := r.bounds[call].lo, r.bounds[call].hi
		if start < at || end > hi || r.keep[call] {
			continue
		}
		out.WriteString(r.source[at:start])
		text := r.rewrite(call)
		if strings.HasPrefix(text, "(") && startsLine(r.source, start) {
			// A statement that opens with a parenthesis continues the previous
			// line as a call, so this call keeps its nested form and only its
			// inner calls are rewritten.
			r.keep[call] = true
			text = r.render(start, end)
			delete(r.keep, call)
		}
		out.WriteString(text)
		at = end
	}
	out.WriteString(r.source[at:hi])
	return out.String()
}

// closingParenthesis returns the offset after the parenthesis that closes the
// one at open, or -1.
func closingParenthesis(source string, open int) int {
	depth := 0
	for i := open; i < len(source); i++ {
		switch source[i] {
		case '"':
			for i++; i < len(source) && source[i] != '"'; i++ {
				if source[i] == '\\' {
					i++
				}
			}
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func startsLine(source string, offset int) bool {
	for i := offset - 1; i >= 0; i-- {
		switch source[i] {
		case ' ', '\t':
		case '\n':
			return true
		default:
			return false
		}
	}
	return true
}

func (r *pipeRewriter) rewrite(call *Expr) string {
	first := call.Args[0]
	end := r.bounds[call].hi
	callee := strings.TrimSpace(r.source[r.bounds[call].lo:r.bounds[call].open])
	firstEnd := first.Extent.Offset + first.Extent.Length
	rest := strings.TrimLeft(r.render(firstEnd, end-1), " \t\r\n")
	rest = strings.TrimPrefix(rest, ",")
	rest = strings.TrimLeft(rest, " \t\r\n")
	subject := r.render(first.Extent.Offset, firstEnd)
	switch first.Kind {
	case "name", "member", "call", "string", "integer", "bool", "void":
	default:
		subject = "(" + subject + ")"
	}
	text := subject + " |> " + callee + "(" + rest + ")"
	if r.operands[call] {
		text = "(" + text + ")"
	}
	return text
}

func TestPipeRewriterGroupsQualifiedCallUnderRunBeforeConcatenation(t *testing.T) {
	source := `import go strconv "strconv"
effect fn main() -> string uses { Foreign } {
    run strconv.Itoa(2) + "!"
}
`
	program, diagnostics := safeParse(source)
	if len(diagnostics) != 0 || program == nil {
		t.Fatalf("original source did not parse: %+v", diagnostics)
	}
	rewritten := newPipeRewriter(source, program).render(0, len(source))
	if !strings.Contains(rewritten, `run (2 |> strconv.Itoa()) + "!"`) {
		t.Fatalf("qualified call under run was not grouped before concatenation:\n%s", rewritten)
	}
	if _, diagnostics := safeParse(rewritten); len(diagnostics) != 0 {
		t.Fatalf("rewritten source did not parse: %+v\n%s", diagnostics, rewritten)
	}
}

// compileTransformed is CompileAt with a transform between its parse and check
// phases, which production runs back to back. A control uses it to inject a
// wrong desugaring; the transform may be nil.
func compileTransformed(source, target, dir string, transform func(*Program)) *Result {
	r, program := parseSource(source, target)
	if program == nil {
		return r
	}
	if transform != nil {
		transform(program)
	}
	return checkParsed(r, program, dir, source)
}

// Statuses of one compared artifact. They stay distinct so that a program one
// target rejects, a program whose emission fails and a program that emits are
// never compared as if they were the same thing.
const (
	pipeEmitted        = "emitted"
	pipeRejected       = "rejected"        // checking failed; codes hold the diagnostics
	pipeEmissionFailed = "emission-failed" // checked, but emission returned an error
	pipeNoTests        = "no-tests"        // checked, but the program has no valid tests
	pipeNoEntry        = "no-entry"        // checked, but there is no entry to emit
)

// pipeArtifact is one compared output: its status, the diagnostic codes when
// rejected, and the text when emitted (minus the source-revision line).
type pipeArtifact struct {
	Status string
	Codes  []string
	Text   string
}

// pipeOutput holds every artifact one program compiles to, keyed by
// "<target>/<mode>": js/check, js/entry, js/entry.d.mts, js/library,
// js/library.d.mts, js/tests, js/tests.d.mts, go/check, go/build, go/test.
type pipeOutput map[string]pipeArtifact

func (o pipeOutput) record(key, status, text string) {
	o[key] = pipeArtifact{Status: status, Text: afterFirstLine(text)}
}

func pipeOutputsEqual(a, b pipeOutput) bool {
	return reflect.DeepEqual(a, b)
}

func compilePipeOutput(t *testing.T, source, dir string, transform func(*Program)) pipeOutput {
	return compilePipeOutputWithRawMutation(t, source, dir, transform, nil)
}

// compilePipeOutputWithRawMutation applies mutate to emitted text before the
// same record/normalization boundary used by the differential. It is a
// test-only seam for proving that controls observe raw artifacts rather than
// only text retained after normalization.
func compilePipeOutputWithRawMutation(t *testing.T, source, dir string, transform func(*Program), mutate func(key, text string) string) pipeOutput {
	t.Helper()
	out := pipeOutput{}
	record := func(key, status, text string) {
		if status == pipeEmitted && mutate != nil {
			text = mutate(key, text)
		}
		out.record(key, status, text)
	}
	for _, target := range []string{"js", "go"} {
		r := compileTransformed(source, target, dir, transform)
		if !r.Checked {
			codes := []string{}
			for _, d := range r.Diagnostics {
				codes = append(codes, d.Code)
			}
			out[target+"/check"] = pipeArtifact{Status: pipeRejected, Codes: codes}
			continue
		}
		out[target+"/check"] = pipeArtifact{Status: pipeEmitted}
		_, testsErr := r.Tests()
		entryErr := r.Entry()
		if target == "js" {
			emitJS := func(mode string, emit func() (string, string, error)) {
				text, declarations, err := emit()
				if err != nil {
					record("js/"+mode, pipeEmissionFailed, "")
					record("js/"+mode+".d.mts", pipeEmissionFailed, "")
					return
				}
				record("js/"+mode, pipeEmitted, text)
				record("js/"+mode+".d.mts", pipeEmitted, declarations)
			}
			if entryErr != nil {
				record("js/entry", pipeNoEntry, "")
				record("js/entry.d.mts", pipeNoEntry, "")
			} else {
				emitJS("entry", func() (string, string, error) { return r.Emit(true) })
			}
			emitJS("library", func() (string, string, error) { return r.Emit(false) })
			if testsErr != nil {
				record("js/tests", pipeNoTests, "")
			} else {
				emitJS("tests", r.EmitJSTests)
			}
			continue
		}
		if entryErr != nil {
			record("go/build", pipeNoEntry, "")
		} else if app, err := r.GoApplication(GoGenerationBuild); err != nil {
			record("go/build", pipeEmissionFailed, "")
		} else {
			record("go/build", pipeEmitted, string(app.Main))
		}
		if testsErr != nil {
			record("go/test", pipeNoTests, "")
		} else if text, err := r.EmitGoTests(); err != nil {
			record("go/test", pipeEmissionFailed, "")
		} else {
			record("go/test", pipeEmitted, text)
		}
	}
	return out
}

// sourcePositions matches the only emitted text that names a source position,
// and is anchored to exactly these two patterns:
//
//   - `binding:<Name>:<digits>`, the byte offset that ends a layer binding's
//     node id in both targets (the digits are masked, the name is not);
//   - `Offset: N, Length: N, Line: N, Column: N`, the source location in a Go
//     NodeSpec.
//
// Rewriting a call to pipe form moves every later declaration by a few bytes,
// which is a property of the edited text and not of the desugaring, so these
// positions are masked before comparing. Offset-derived layer ids are unstable
// under any unrelated edit (backlog LID1); the mask records that, it does not
// excuse it. TestPipeDifferentialMaskIsAnchored proves nothing else is masked.
var sourcePositions = regexp.MustCompile(`(binding:\w+:)\d+|Offset: \d+, Length: \d+, Line: \d+, Column: \d+`)

const pipeRevisionHeader = "// Generated by the Effra prototype. Source revision: "

func stripPipeRevisionHeader(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 && strings.HasPrefix(text[:i], pipeRevisionHeader) && len(text[:i]) > len(pipeRevisionHeader) {
		text = text[i:]
	}
	return text
}

// afterFirstLine drops the generated source-revision line and masks source
// positions. Declarations and other artifacts without that header keep their
// first line in the comparison.
func afterFirstLine(text string) string {
	text = stripPipeRevisionHeader(text)
	return sourcePositions.ReplaceAllString(text, "${1}#")
}

type pipeDifferential struct {
	programs, rewritten, calls int
	// status counts the programs (before rewriting) by artifact key and
	// status, so "rejected", "emission-failed" and "emitted" stay distinct.
	status map[string]map[string]int
	// compared counts the programs whose artifact was emitted on both sides of
	// the comparison, by key: the artifacts whose text was actually compared.
	compared   map[string]int
	mismatches []string
}

// both is the number of programs whose JavaScript entry and Go build were both
// emitted and compared.
func (d pipeDifferential) both() int {
	return d.compared["both"]
}

// report is one line per artifact: how many programs reached each status and
// how many had their emitted text compared.
func (d pipeDifferential) report() string {
	keys := make([]string, 0, len(d.status))
	for key := range d.status {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out strings.Builder
	for _, key := range keys {
		statuses := make([]string, 0, len(d.status[key]))
		for status, n := range d.status[key] {
			statuses = append(statuses, status+"="+strconv.Itoa(n))
		}
		sort.Strings(statuses)
		out.WriteString("\n  " + key + ": " + strings.Join(statuses, " ") + " compared=" + strconv.Itoa(d.compared[key]))
	}
	return out.String()
}

// runPipeDifferential rewrites each corpus program and compares every
// artifact on both targets, with its status and diagnostic codes, to the
// original's. transform is applied to the rewritten program only, which lets a
// control inject a wrong desugaring.
func runPipeDifferential(t *testing.T, corpus []pipeCorpusSource, transform func(*Program)) pipeDifferential {
	t.Helper()
	// Each program compiles independently, so the corpus runs as parallel
	// subtests; the tally is then folded in corpus order.
	type outcome struct {
		piped         int
		before, after pipeOutput
	}
	outcomes := make([]*outcome, len(corpus))
	t.Run("corpus", func(t *testing.T) {
		for i, entry := range corpus {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				t.Parallel()
				program, diagnostics := safeParse(entry.text)
				if len(diagnostics) > 0 || program == nil {
					return
				}
				rewriter := newPipeRewriter(entry.text, program)
				rewritten := rewriter.render(0, len(entry.text))
				if len(rewriter.calls) == 0 {
					return
				}
				again, diagnostics := safeParse(rewritten)
				if len(diagnostics) > 0 {
					t.Fatalf("%s: the pipe form does not parse: %+v\n%s", entry.name, diagnostics, rewritten)
				}
				piped := 0
				for _, e := range expressionsOf(again) {
					if e.Kind == "call" && e.PipeSpan.Length > 0 {
						piped++
					}
				}
				if piped == 0 {
					return
				}
				outcomes[i] = &outcome{
					piped:  piped,
					before: compilePipeOutput(t, entry.text, entry.dir, nil),
					after:  compilePipeOutput(t, rewritten, entry.dir, transform),
				}
			})
		}
	})
	result := pipeDifferential{status: map[string]map[string]int{}, compared: map[string]int{}}
	for i, entry := range corpus {
		o := outcomes[i]
		if o == nil {
			continue
		}
		before, after := o.before, o.after
		result.programs++
		result.calls += o.piped
		for key, artifact := range before {
			if result.status[key] == nil {
				result.status[key] = map[string]int{}
			}
			result.status[key][artifact.Status]++
			if artifact.Status == pipeEmitted && after[key].Status == pipeEmitted && key != "js/check" && key != "go/check" {
				result.compared[key]++
			}
		}
		if before["js/entry"].Status == pipeEmitted && before["go/build"].Status == pipeEmitted {
			result.compared["both"]++
		}
		if !pipeOutputsEqual(before, after) {
			result.mismatches = append(result.mismatches, entry.name)
		} else {
			result.rewritten++
		}
	}
	return result
}

// safeParse parses a corpus program, treating a parser panic as unparseable.
func safeParse(source string) (program *Program, diagnostics []Diagnostic) {
	defer func() {
		if recover() != nil {
			program, diagnostics = nil, []Diagnostic{{Code: "EF002"}}
		}
	}()
	return parse(source)
}

func TestPipeFormCompilesToTheSameOutputAsNestedCalls(t *testing.T) {
	corpus := pipeCorpus(t)
	result := runPipeDifferential(t, corpus, nil)
	t.Logf("%d corpus programs, %d rewritten, %d calls piped, %d emitted on both targets (JS entry and Go build)%s", len(corpus), result.programs, result.calls, result.both(), result.report())
	if result.programs < 200 || result.calls < 500 || result.both() < 50 {
		t.Fatalf("corpus too small to prove equivalence: %d programs, %d calls, %d emitted on both targets", result.programs, result.calls, result.both())
	}
	// Every compared artifact must be exercised, or its claim is empty.
	for _, key := range []string{"js/entry", "js/entry.d.mts", "js/library", "js/library.d.mts", "js/tests", "js/tests.d.mts", "go/build", "go/test"} {
		if result.compared[key] < pipeMinimumCompared[key] {
			t.Fatalf("only %d programs compared %s, want at least %d", result.compared[key], key, pipeMinimumCompared[key])
		}
	}
	if len(result.mismatches) > 0 {
		t.Fatalf("%d programs compile differently in pipe form: %v", len(result.mismatches), result.mismatches[:min(len(result.mismatches), 10)])
	}
}

// pipeMinimumCompared is the least number of programs whose emitted text must
// be compared per artifact. The floors sit below the counts the corpus reaches
// today (see the test log) so that a corpus change does not trip them, and
// above zero so that a mode silently dropping out of the comparison does.
var pipeMinimumCompared = map[string]int{
	"js/entry": 80, "js/entry.d.mts": 80, "js/library": 100, "js/library.d.mts": 100,
	"js/tests": 5, "js/tests.d.mts": 5, "go/build": 100, "go/test": 5,
}

// The statuses the differential records stay distinct: a rejected program, a
// program with no entry or no tests, and an emitted one are not interchangeable.
func TestPipeDifferentialRecordsPerTargetStatus(t *testing.T) {
	status := func(source string) pipeOutput { return compilePipeOutput(t, source, ".", nil) }
	want := func(out pipeOutput, key, status string) {
		t.Helper()
		if got := out[key].Status; got != status {
			t.Errorf("%s = %q, want %q", key, got, status)
		}
	}
	rejected := status("fn bad() -> string { 1 }\n")
	want(rejected, "js/check", pipeRejected)
	want(rejected, "go/check", pipeRejected)
	if got := rejected["js/check"].Codes; len(got) == 0 {
		t.Error("a rejected program records no diagnostic codes")
	}
	if _, ok := rejected["js/library"]; ok {
		t.Error("a rejected program records an emission")
	}
	library := status("fn shout(text: string) -> string { text + \"!\" }\n")
	want(library, "js/entry", pipeNoEntry)
	want(library, "go/build", pipeNoEntry)
	want(library, "js/library", pipeEmitted)
	want(library, "js/library.d.mts", pipeEmitted)
	want(library, "js/tests", pipeNoTests)
	want(library, "go/test", pipeNoTests)
	full := status("effect fn main() -> string { \"ok\" }\neffect fn test_ok() -> void raises { AssertionFailed } uses { Assert } {\n    run Assert.check(true, \"holds\")\n}\n")
	for _, key := range []string{"js/entry", "js/entry.d.mts", "js/library", "js/library.d.mts", "js/tests", "js/tests.d.mts", "go/build", "go/test"} {
		want(full, key, pipeEmitted)
	}
}

func TestPipeDifferentialRecordsEmissionFailure(t *testing.T) {
	source := `fn shout(text: string) -> string { text + "!" }
effect fn main() -> string { shout("ok") }
effect fn test_ok() -> void raises { AssertionFailed } uses { Assert } {
    run Assert.check(true, "holds")
}
`
	goOnly := func(program *Program) { program.GoOnly = true }
	out := compilePipeOutput(t, source, ".", goOnly)
	for _, key := range []string{"js/entry", "js/entry.d.mts", "js/library", "js/library.d.mts", "js/tests", "js/tests.d.mts"} {
		if got := out[key].Status; got != pipeEmissionFailed {
			t.Errorf("%s status = %q, want reachable %q", key, got, pipeEmissionFailed)
		}
	}
	for _, key := range []string{"go/check", "go/build", "go/test"} {
		if got := out[key].Status; got != pipeEmitted {
			t.Errorf("%s status = %q, want %q", key, got, pipeEmitted)
		}
	}
}

func TestPipeDifferentialComparesFirstLineOfEveryArtifact(t *testing.T) {
	source := `effect fn main() -> string { "ok" }
effect fn test_ok() -> void raises { AssertionFailed } uses { Assert } {
    run Assert.check(true, "holds")
}
`
	out := compilePipeOutput(t, source, ".", nil)
	for _, key := range []string{"js/entry", "js/entry.d.mts", "js/library", "js/library.d.mts", "js/tests", "js/tests.d.mts", "go/build", "go/test"} {
		artifact := out[key]
		if artifact.Status != pipeEmitted {
			t.Fatalf("%s status = %q, want %q", key, artifact.Status, pipeEmitted)
		}
		changed := compilePipeOutputWithRawMutation(t, source, ".", nil, func(candidate, text string) string {
			if candidate != key {
				return text
			}
			return mutateFirstRawArtifactLine(t, candidate, text)
		})
		if pipeOutputsEqual(out, changed) {
			t.Errorf("changing the first raw line of %s did not change the compared output", key)
		}
	}
}

// mutateFirstRawArtifactLine changes the first meaningful raw line while
// retaining a recognized generated revision header. Declaration artifacts do
// not have that header: their first line is a real import and must remain part
// of the comparison.
func mutateFirstRawArtifactLine(t *testing.T, key, text string) string {
	t.Helper()
	firstEnd := strings.IndexByte(text, '\n')
	if firstEnd < 0 {
		firstEnd = len(text)
	}
	start := 0
	if firstEnd < len(text) && strings.HasPrefix(text[:firstEnd], pipeRevisionHeader) && len(text[:firstEnd]) > len(pipeRevisionHeader) {
		start = firstEnd + 1
	}
	lineEnd := strings.IndexByte(text[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text)
	} else {
		lineEnd += start
	}
	if start >= lineEnd {
		t.Fatalf("%s has no first meaningful raw line", key)
	}
	line := text[start:lineEnd]
	if key == "js/library.d.mts" {
		if !strings.HasPrefix(line, "import type ") || !strings.Contains(line, "from 'effect';") {
			t.Fatalf("%s first line is not the real declaration import: %q", key, line)
		}
	}
	return text[:lineEnd] + " /* first raw-line change: " + key + " */" + text[lineEnd:]
}

// The differential must be able to fail: a parser that appended the piped
// value as the last argument, as F# does, changes the output of the corpus.
func TestPipeEquivalenceCatchesAppendingTheSubjectLast(t *testing.T) {
	appendLast := func(program *Program) {
		for _, e := range expressionsOf(program) {
			if e.Kind == "call" && e.PipeSpan.Length > 0 && len(e.Args) > 1 {
				e.Args = append(slices.Clone(e.Args[1:]), e.Args[0])
			}
		}
	}
	result := runPipeDifferential(t, pipeCorpus(t), appendLast)
	if len(result.mismatches) == 0 {
		t.Fatal("the differential accepted a last-argument desugaring")
	}
	t.Logf("%d programs differ under the wrong desugaring", len(result.mismatches))
}

// The mask may hide the two position patterns and nothing else: changing any
// byte outside the masked digits must change the compared text, and changing
// a masked digit must not.
func TestPipeDifferentialMaskIsAnchored(t *testing.T) {
	for _, target := range []string{"js", "go"} {
		source, err := os.ReadFile(filepath.Join("..", "..", "examples", "layers.ef"))
		if err != nil {
			t.Fatal(err)
		}
		text := emittedProgram(t, compileTransformed(string(source), target, filepath.Join("..", "..", "examples"), nil))
		if !strings.HasPrefix(text, pipeRevisionHeader) {
			t.Fatalf("%s: emitted source has no revision header", target)
		}
		text = stripPipeRevisionHeader(text)
		// The permitted mask is spelled out here, independently of the one
		// under test, so widening that one is caught.
		masks := regexp.MustCompile(`binding:\w+:\d+|Offset: \d+, Length: \d+, Line: \d+, Column: \d+`).FindAllStringIndex(text, -1)
		if len(masks) == 0 {
			t.Fatalf("%s: the program has no layer positions to mask", target)
		}
		maskedDigit := func(i int) bool {
			if text[i] < '0' || text[i] > '9' {
				return false
			}
			for _, m := range masks {
				if i >= m[0] && i < m[1] {
					match := text[m[0]:m[1]]
					return !strings.HasPrefix(match, "binding:") || i > m[0]+strings.LastIndexByte(match, ':')
				}
			}
			return false
		}
		nearMask := func(i int) bool {
			for _, m := range masks {
				if i >= m[0]-3 && i < m[1]+3 {
					return true
				}
			}
			return false
		}
		controlHeader := pipeRevisionHeader + "mask-control\n"
		want := afterFirstLine(controlHeader + text)
		for i := 0; i < len(text); i++ {
			if text[i] == '\n' {
				continue
			}
			if i%11 != 0 && !maskedDigit(i) && !nearMask(i) {
				continue
			}
			mutated := []byte(text)
			if c := text[i]; c >= '0' && c <= '9' {
				mutated[i] = '0' + (c-'0'+1)%10
			} else {
				mutated[i] = '~'
			}
			same := afterFirstLine(controlHeader+string(mutated)) == want
			if masked := maskedDigit(i); masked && !same {
				t.Fatalf("%s: a masked digit at byte %d changed the compared text", target, i)
			} else if !masked && same {
				t.Fatalf("%s: changing byte %d (%q) went unnoticed by the differential", target, i, text[i])
			}
		}
	}
}
