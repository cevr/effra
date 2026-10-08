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
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
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
	if len(call.Args) == 0 || !staticPath(call.Left) || strings.HasPrefix(r.text(call.Left.Extent), "(") {
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

// pipeOutput is what a compiled program emits, minus its source-revision line.
type pipeOutput struct {
	diagnostics []string
	js, goMain  string
	jsErr, goOK bool
}

func compilePipeOutput(t *testing.T, source, dir string, transform func(*Program)) pipeOutput {
	t.Helper()
	var out pipeOutput
	for _, target := range []string{"js", "go"} {
		r := compileAt(source, target, dir, transform)
		if !r.Checked {
			if target == "js" {
				for _, d := range r.Diagnostics {
					out.diagnostics = append(out.diagnostics, d.Code)
				}
			}
			continue
		}
		if target == "js" {
			text, _, err := r.Emit(true)
			out.jsErr = err != nil
			out.js = afterFirstLine(text)
		} else {
			app, err := r.GoApplication(GoGenerationBuild)
			out.goOK = err == nil
			if err == nil {
				out.goMain = afterFirstLine(string(app.Main))
			}
		}
	}
	return out
}

// sourcePositions matches the only emitted text that names a source position:
// a layer binding's node id ends in its byte offset, and the Go node spec
// carries the location. Rewriting a call to pipe form moves every later
// declaration by a few bytes, which is a property of the edited text and not
// of the desugaring, so these positions are masked before comparing.
var sourcePositions = regexp.MustCompile(`(binding:\w+:)\d+|Offset: \d+, Length: \d+, Line: \d+, Column: \d+`)

// afterFirstLine drops the source-revision line and masks source positions.
func afterFirstLine(text string) string {
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		text = text[i:]
	}
	return sourcePositions.ReplaceAllString(text, "${1}#")
}

type pipeDifferential struct {
	programs, rewritten, calls, emitted int
	mismatches                          []string
}

// runPipeDifferential rewrites each corpus program and compares both targets'
// output and the diagnostic codes with the original's. transform is applied to
// the rewritten program only, which lets a control inject a wrong desugaring.
func runPipeDifferential(t *testing.T, corpus []pipeCorpusSource, transform func(*Program)) pipeDifferential {
	t.Helper()
	var result pipeDifferential
	for _, entry := range corpus {
		program, diagnostics := safeParse(entry.text)
		if len(diagnostics) > 0 || program == nil {
			continue
		}
		rewriter := newPipeRewriter(entry.text, program)
		rewritten := rewriter.render(0, len(entry.text))
		if len(rewriter.calls) == 0 {
			continue
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
			continue
		}
		result.programs++
		result.calls += piped
		before := compilePipeOutput(t, entry.text, entry.dir, nil)
		after := compilePipeOutput(t, rewritten, entry.dir, transform)
		if before.js != "" && before.goMain != "" {
			result.emitted++
		}
		if !reflect.DeepEqual(before, after) {
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
	t.Logf("%d corpus programs, %d rewritten, %d calls piped, %d emitted on both targets", len(corpus), result.programs, result.calls, result.emitted)
	if result.programs < 200 || result.calls < 500 || result.emitted < 50 {
		t.Fatalf("corpus too small to prove equivalence: %d programs, %d calls, %d emitted", result.programs, result.calls, result.emitted)
	}
	if len(result.mismatches) > 0 {
		t.Fatalf("%d programs compile differently in pipe form: %v", len(result.mismatches), result.mismatches[:min(len(result.mismatches), 10)])
	}
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
