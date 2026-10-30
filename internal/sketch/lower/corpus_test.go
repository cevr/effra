package lower

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A program that checks clean builds (stage C round 3 review M1, the
// structural guard; round 4 review T1 to T3). The check package's tests
// hold the checker's corpus inline, as Go strings. This test reads them
// from ../check/*_test.go with go/parser, so a new check test joins the
// corpus with no list to keep.
//
// The enumeration rule (checkCorpus): in each test file, a named string is
// a const or var whose value is a string literal, or a sum of literals and
// named strings, at package level or in a function. The candidates are
// every named string, every string literal anywhere in the file (case
// tables, struct fields, call arguments, package-level declarations), and
// each literal after each named string of its file (the tests' decls
// prefixes). Only strings with a top-level func, type or let are kept.
//
// Every candidate that parses and checks clean is lowered and built with
// go vet and go build, in one Go module. A lowering panic or error fails,
// and so does any refusal that is not a documented deferred family whose
// owner is a later stage (deferredOwners; refusalReasons and
// refusedFamilies of TestAcceptedNodesLower). An internal compiler error
// always fails.
//
// Bounds: at stage C the enumerated corpus is about 2,000 candidates;
// about 450 check clean and lower, and one go command vets and builds them
// in about ten seconds. Past maxCandidates the test takes every k-th
// candidate in a stable order, before checking and lowering, and says so.
// The .ergo fixtures under ../check/testdata are lowered and built by
// TestAcceptedNodesLower.
func TestCheckCorpusBuilds(t *testing.T) {
	const maxCandidates = 8000
	sources, files := checkCorpus(t, "../check")
	if files < 30 || len(sources) < 200 {
		t.Fatalf("the corpus has %d candidates from %d files; the check tests moved?", len(sources), files)
	}
	if n := len(sources); n > maxCandidates {
		k := (n + maxCandidates - 1) / maxCandidates
		var sample []string
		for i := 0; i < n; i += k {
			sample = append(sample, sources[i])
		}
		t.Logf("sampled every %dth of %d candidates", k, n)
		sources = sample
	}
	var programs []*lowered
	rejected, deferred := 0, map[string]int{}
	for i, src := range sources {
		o := lowerSource(fmt.Sprintf("c%d", i), src, false)
		switch {
		case o.file == nil || len(o.diags) > 0:
			rejected++
		case o.panicked != nil:
			t.Errorf("a clean program's lowering panicked: %v\n%s", o.panicked, src)
		case o.err != nil:
			t.Errorf("a clean program does not lower: %v\n%s", o.err, src)
		case len(o.refused) > 0:
			for _, r := range o.refused {
				if why := undeferred(r.Message); why != "" {
					t.Errorf("%d:%d: %s: %s\n%s", r.Line, r.Column, why, r.Message, src)
					continue
				}
				f := refusalFamily(r.Message)
				deferred[f+" ("+refusedFamilies[f]+")"]++
			}
		default:
			programs = append(programs, o)
		}
	}
	if len(programs) < 100 {
		t.Fatalf("only %d clean programs lowered; the corpus reader lost the check tests?", len(programs))
	}
	var owners []string
	for o, n := range deferred {
		owners = append(owners, fmt.Sprintf("%s: %d", o, n))
	}
	sort.Strings(owners)
	t.Logf("candidates %d from %d files: rejected by the parser or checker %d; deferred refusals by family [%s]; built %d (every clean program of the enumerated corpus that lowers)", len(sources), files, rejected, strings.Join(owners, ", "), len(programs))
	buildAll(t, programs, false)
}

// deferredOwners are the later stages and units a refusal may name: the
// forms they own are not lowered yet. A refusal owned by a stage that has
// landed (check-v1 stages A to C, #196 stage b) is a lowering gap.
var deferredOwners = map[string]bool{
	"check-v1 stage D": true,
	"check-v1 stage E": true,
	"lower-v1 stage 3": true,
	"lower-v1 stage 4": true,
	"lower-v1 request": true,
	"unused by v1":     true,
}

// undeferred says why a refusal is not a documented deferral, or "".
func undeferred(message string) string {
	if strings.Contains(message, "internal compiler error") {
		return "an internal compiler error"
	}
	family := refusalFamily(message)
	if family == "" {
		return "an unclassified refusal (add it to refusalReasons with a refused family, or lower the form)"
	}
	owner := refusedFamilies[family]
	switch {
	case owner == "" || !strings.Contains(message, owner):
		return fmt.Sprintf("the refusal stands at %s, documented as owned by %q, and names another owner", family, owner)
	case !deferredOwners[owner]:
		return fmt.Sprintf("a refusal of %s, owned by %s, which has landed", family, owner)
	}
	return ""
}

// checkCorpus returns the distinct candidate sources in dir's test files
// by the enumeration rule of TestCheckCorpusBuilds, in a stable order, and
// the number of files read.
func checkCorpus(t *testing.T, dir string) ([]string, int) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	seen := map[string]bool{}
	var out []string
	add := func(src string) {
		if !looksLikeSource(src) {
			return
		}
		if !strings.HasSuffix(src, "\n") {
			src += "\n"
		}
		if !seen[src] {
			seen[src] = true
			out = append(out, src)
		}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, data, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		named := namedStrings(f)
		for _, s := range named {
			add(s)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || !looksLikeSource(s) {
				return true
			}
			add(s)
			for _, pre := range named {
				if pre != s {
					add(pre + s)
				}
			}
			return true
		})
	}
	return out, len(paths)
}

// namedStrings returns the values of the file's string consts and vars,
// at package level and in functions, whose values are literals or sums of
// literals and earlier named strings, in source order.
func namedStrings(f *ast.File) []string {
	known := map[string]string{}
	var out []string
	var eval func(e ast.Expr) (string, bool)
	eval = func(e ast.Expr) (string, bool) {
		switch e := e.(type) {
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				return "", false
			}
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		case *ast.ParenExpr:
			return eval(e.X)
		case *ast.Ident:
			s, ok := known[e.Name]
			return s, ok
		case *ast.BinaryExpr:
			if e.Op != token.ADD {
				return "", false
			}
			a, ok1 := eval(e.X)
			b, ok2 := eval(e.Y)
			return a + b, ok1 && ok2
		}
		return "", false
	}
	record := func(name string, value ast.Expr) {
		if s, ok := eval(value); ok && looksLikeSource(s) {
			known[name] = s
			out = append(out, s)
		}
	}
	ast.Inspect(f, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.GenDecl:
			if x.Tok != token.CONST && x.Tok != token.VAR {
				return true
			}
			for _, spec := range x.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						record(name.Name, vs.Values[i])
					}
				}
			}
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE && len(x.Lhs) == len(x.Rhs) {
				for i, l := range x.Lhs {
					if id, ok := l.(*ast.Ident); ok {
						record(id.Name, x.Rhs[i])
					}
				}
			}
		}
		return true
	})
	return out
}

// looksLikeSource keeps the strings that may be Ergo programs: those with
// a top-level declaration keyword.
func looksLikeSource(s string) bool {
	for _, kw := range []string{"func ", "type ", "let "} {
		if strings.HasPrefix(s, kw) || strings.Contains(s, "\n"+kw) {
			return true
		}
	}
	return false
}
