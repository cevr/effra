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
// structural guard). The check package's tests hold the checker's corpus
// inline, as Go string literals: a case's source, often after a decls
// constant. This test reads them from ../check/*_test.go with go/parser,
// so a new check test joins the corpus with no list to keep. Each literal,
// alone and after each string constant of its function or file, is a
// candidate. Every candidate that parses and checks clean is lowered and
// built with go vet and go build, all in one Go module. A lowering refusal
// (a family a later stage owns) is skipped and counted; a panic or another
// lowering error fails. The .ergo fixtures under ../check/testdata are
// lowered and built by TestAcceptedNodesLower.
//
// Bounds: at stage C the corpus is about 1,900 candidates, of which about
// 430 check clean and lower; one go command vets and builds them in about
// ten seconds, under the gate's memory limit. Past maxCorpus programs the
// test takes every k-th program in a stable order and says so.
func TestCheckCorpusBuilds(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the check corpus")
	}
	const maxCorpus = 3000
	sources, files := checkCorpus(t, "../check")
	if files < 30 || len(sources) < 200 {
		t.Fatalf("the corpus has %d candidates from %d files; the check tests moved?", len(sources), files)
	}
	var programs []*lowered
	rejected, refused := 0, 0
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
			refused++
		default:
			programs = append(programs, o)
		}
	}
	if len(programs) < 100 {
		t.Fatalf("only %d clean programs lowered; the corpus reader lost the check tests?", len(programs))
	}
	if n := len(programs); n > maxCorpus {
		k := (n + maxCorpus - 1) / maxCorpus
		var sample []*lowered
		for i := 0; i < n; i += k {
			sample = append(sample, programs[i])
		}
		t.Logf("sampled every %dth of %d lowered programs", k, n)
		programs = sample
	}
	t.Logf("candidates %d from %d files: rejected by the parser or checker %d, refused %d, built %d", len(sources), files, rejected, refused, len(programs))
	buildAll(t, programs, false)
}

// checkCorpus returns the distinct candidate sources in dir's test files,
// in a stable order, and the number of files read.
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
		fileConsts := stringConsts(f)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			prefixes := append(stringConsts(fn.Body), fileConsts...)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !looksLikeSource(s) {
					return true
				}
				add(s)
				for _, pre := range prefixes {
					if pre != s {
						add(pre + s)
					}
				}
				return true
			})
		}
	}
	return out, len(paths)
}

// stringConsts returns the values of the string constants declared in n
// whose values are literals or sums of literals and known constants.
func stringConsts(n ast.Node) []string {
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
	ast.Inspect(n, func(x ast.Node) bool {
		g, ok := x.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			return true
		}
		for _, spec := range g.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					break
				}
				if s, ok := eval(vs.Values[i]); ok && looksLikeSource(s) {
					known[name.Name] = s
					out = append(out, s)
				}
			}
		}
		return true
	})
	return out
}

// looksLikeSource keeps the literals that may be Ergo programs: those with
// a top-level declaration keyword.
func looksLikeSource(s string) bool {
	for _, kw := range []string{"func ", "type ", "let "} {
		if strings.HasPrefix(s, kw) || strings.Contains(s, "\n"+kw) {
			return true
		}
	}
	return false
}
