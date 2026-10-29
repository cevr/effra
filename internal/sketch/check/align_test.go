package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"ergo.local/prototype/internal/sketch/syntax"
)

// The go/types oracle reads a generated program as Go text; the checker
// reads the same program as decided Ergo text (a twin, differential_test.go).
// The two texts do not match byte for byte, so the differential tests match
// their expressions by tokens: the Ergo text keeps every function body and
// every package variable, in order, token for token, apart from the
// spellings that differ (var, let and mut; map and Map with the brackets and
// commas of a type-argument list; the any constraint; := and =; ;; range,
// in and the blank of a for clause; switch written match). An
// expression's key is its region (the n-th body or variable) and the
// indices of its first and last kept token there. A region whose kept
// tokens differ between the two texts is reported, so a generator whose two
// texts disagree on a body fails the test rather than mismatching keys.

// alignKey identifies an expression in either text.
type alignKey struct{ region, from, to int }

// alignTok is a kept token: its offset and its text.
type alignTok struct {
	off  int
	text string
}

// alignRegion is one body or package variable: its byte range and kept
// tokens.
type alignRegion struct {
	name     string
	from, to int
	toks     []alignTok
}

var alignDropped = map[string]bool{
	"var": true, "let": true, "mut": true, "map": true, "Map": true, "any": true,
	"[": true, "]": true, ",": true, "=": true, ":=": true, ";": true,
	"range": true, "in": true, "_": true,
}

// alignSame spells an Ergo keyword as the Go one it replaces.
var alignSame = map[string]string{"match": "switch"}

var alignOps = []string{"==", "!=", "<=", ">=", "&&", "||", "<<", ">>", ":=", "..", "<-", "+=", "-=", "*=", "/="}

// alignTokens lexes src[from:to] into its kept tokens: words and numbers,
// string and rune literals, operators, and [] as one token (a slice type);
// comments and white space are skipped.
func alignTokens(src string, from, to int) []alignTok {
	var out []alignTok
	word := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}
	for i := from; i < to; {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
			continue
		case strings.HasPrefix(src[i:], "//"):
			for i < to && src[i] != '\n' {
				i++
			}
			continue
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			i += end + 4
			continue
		}
		start := i
		switch {
		case word(c):
			for i < to && (word(src[i]) || src[i] == '.' && i+1 < to && src[i+1] >= '0' && src[i+1] <= '9' && c >= '0' && c <= '9') {
				i++
			}
		case c == '"' || c == '\'' || c == '`':
			i++
			for i < to && src[i] != c {
				if src[i] == '\\' && c != '`' {
					i++
				}
				i++
			}
			i++
		case c == '[' && strings.HasPrefix(strings.TrimLeft(src[i+1:to], " "), "]"):
			i = strings.IndexByte(src[i:to], ']') + i + 1
			out = append(out, alignTok{start, "[]"})
			continue
		default:
			i++
			for _, op := range alignOps {
				if strings.HasPrefix(src[start:to], op) {
					i = start + len(op)
					break
				}
			}
		}
		text := src[start:i]
		if s, ok := alignSame[text]; ok {
			text = s
		}
		if !alignDropped[text] {
			out = append(out, alignTok{start, text})
		}
	}
	return out
}

// goRegions lists a Go file's function bodies and package variables.
func goRegions(fset *token.FileSet, f *ast.File, src string) []alignRegion {
	off := func(p token.Pos) int { return fset.Position(p).Offset }
	var out []alignRegion
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Body != nil {
				out = append(out, alignRegion{name: d.Name.Name, from: off(d.Body.Lbrace), to: off(d.Body.Rbrace) + 1})
			}
		case *ast.GenDecl:
			if d.Tok == token.VAR {
				for _, s := range d.Specs {
					out = append(out, alignRegion{name: s.(*ast.ValueSpec).Names[0].Name, from: off(s.Pos()), to: off(s.End())})
				}
			}
		}
	}
	for i := range out {
		out[i].toks = alignTokens(src, out[i].from, out[i].to)
	}
	return out
}

// ergoRegions lists a decided file's function bodies and package
// variables, in the order goRegions lists the Go file's.
func ergoRegions(f *syntax.File, src string) []alignRegion {
	var out []alignRegion
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *syntax.FuncDecl:
			if d.Body != nil {
				out = append(out, alignRegion{name: d.Name.Name, from: int(d.Body.Pos()), to: int(d.Body.End())})
			}
		case *syntax.VarDecl:
			for _, s := range d.Specs {
				name := ""
				if len(s.Names) > 0 {
					name = s.Names[0].Name
				}
				out = append(out, alignRegion{name: name, from: int(s.Pos()), to: int(s.End())})
			}
		}
	}
	for i := range out {
		out[i].toks = alignTokens(src, out[i].from, out[i].to)
	}
	return out
}

// alignCheck reports the first region whose name or kept tokens differ.
func alignCheck(g, e []alignRegion) error {
	if len(g) != len(e) {
		return fmt.Errorf("the Ergo text has %d bodies and variables, the Go text %d", len(e), len(g))
	}
	for i := range g {
		if g[i].name != e[i].name {
			return fmt.Errorf("region %d is %s in the Go text and %s in the Ergo text", i, g[i].name, e[i].name)
		}
		if len(g[i].toks) != len(e[i].toks) {
			return fmt.Errorf("%s: %d kept tokens in the Go text, %d in the Ergo text", g[i].name, len(g[i].toks), len(e[i].toks))
		}
		for k := range g[i].toks {
			if g[i].toks[k].text != e[i].toks[k].text {
				return fmt.Errorf("%s: token %d is %q in the Go text and %q in the Ergo text", g[i].name, k, g[i].toks[k].text, e[i].toks[k].text)
			}
		}
	}
	return nil
}

// alignKeyOf returns the key of the byte range [from, to) in regions, or
// false when the range is outside every region or holds no kept token.
func alignKeyOf(regions []alignRegion, from, to int) (alignKey, bool) {
	for r, reg := range regions {
		if from < reg.from || to > reg.to {
			continue
		}
		first, last := -1, -1
		for k, tok := range reg.toks {
			if tok.off >= from && tok.off < to {
				if first < 0 {
					first = k
				}
				last = k
			}
		}
		if first < 0 {
			return alignKey{}, false
		}
		return alignKey{r, first, last}, true
	}
	return alignKey{}, false
}
