package migrate196

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strconv"
	"strings"

	"ergo.local/prototype/internal/migrate196/oldsyntax"
	"ergo.local/prototype/internal/sketch/syntax"
)

// GoStrings translates every string literal of a Go file that parses as an
// old-syntax source: raw strings keep their form, interpreted strings are
// quoted again. A translation with notes leaves its literal unchanged and
// reports the notes at the literal's line.
func GoStrings(src string) (string, []Note, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		return "", nil, err
	}
	type repl struct {
		start, end int
		text       string
	}
	var repls []repl
	var notes []Note
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		fragment := false
		if _, err := oldsyntax.Parse(value); err != nil {
			if !isDeclFragment(value) {
				return true
			}
			fragment = true
		}
		line := fset.Position(lit.Pos()).Line
		var out string
		var ns []Note
		if fragment {
			out, ns, err = Translate("package snippet\n\n" + value)
			out = keepEdges(value, out)
		} else {
			out, ns, err = Translate(value)
		}
		if err != nil || len(ns) > 0 {
			for _, n := range ns {
				notes = append(notes, Note{Line: line, Text: fmt.Sprintf("embedded source line %d: %s", n.Line, n.Text)})
			}
			if err != nil {
				notes = append(notes, Note{Line: line, Text: err.Error()})
			}
			return true
		}
		var text string
		if lit.Value[0] == '`' && !strings.Contains(out, "`") {
			text = "`" + out + "`"
		} else {
			text = strconv.Quote(out)
		}
		repls = append(repls, repl{fset.Position(lit.Pos()).Offset, fset.Position(lit.End()).Offset, text})
		return true
	})
	sort.Slice(repls, func(i, j int) bool { return repls[i].start < repls[j].start })
	var b strings.Builder
	last := 0
	for _, r := range repls {
		b.WriteString(src[last:r.start])
		b.WriteString(r.text)
		last = r.end
	}
	b.WriteString(src[last:])
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return "", nil, err
	}
	return string(out), notes, nil
}

// isDeclFragment reports whether value is a run of old-syntax declarations
// without a package clause (a test source that a test joins to a shared
// prefix): it starts with a declaration keyword, parses as old syntax once a
// package clause is added, and does not already parse as decided syntax.
func isDeclFragment(value string) bool {
	first := firstWord(value)
	switch first {
	case "import", "type", "func", "var", "wire":
	default:
		return false
	}
	if _, err := oldsyntax.Parse("package snippet\n\n" + value); err != nil {
		return false
	}
	_, err := syntax.Parse(value)
	return err != nil
}

// keepEdges gives a translated fragment the leading line breaks of its
// original, and drops the final line break when the original had none, so
// that string joins in a test still produce the same lines around it.
func keepEdges(orig, out string) string {
	lead := orig[:len(orig)-len(strings.TrimLeft(orig, "\n"))]
	out = lead + strings.TrimLeft(out, "\n")
	if !strings.HasSuffix(orig, "\n") {
		out = strings.TrimRight(out, "\n")
	}
	return out
}

// Markdown translates the go fences of a Markdown file chunk by chunk, as
// sketchtest.SpecFences splits them: a chunk that starts with package,
// import, type, func, var or wire is a file; any other is a function body.
// A chunk that does not parse as old syntax stays unchanged.
func Markdown(src string) (string, []Note) {
	lines := strings.Split(src, "\n")
	var out []string
	var notes []Note
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		if strings.TrimSpace(lines[i]) != "```go" {
			continue
		}
		// A fence inside a list item is indented; its body and its closing
		// line carry the same indentation.
		indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " "))]
		start := i + 1
		var body []string
		for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```"); i++ {
			body = append(body, strings.TrimPrefix(lines[i], indent))
		}
		var chunks []string
		for _, chunk := range splitChunks(strings.Join(body, "\n")) {
			text, ns := translateChunk(chunk)
			for _, n := range ns {
				notes = append(notes, Note{Line: start + 1, Text: n.Text})
			}
			chunks = append(chunks, text)
		}
		if len(chunks) > 0 {
			for _, line := range strings.Split(strings.Join(chunks, "\n\n"), "\n") {
				if line != "" {
					line = indent + line
				}
				out = append(out, line)
			}
		}
		if i < len(lines) {
			out = append(out, lines[i])
		}
	}
	return strings.Join(out, "\n"), notes
}

func translateChunk(chunk string) (string, []Note) {
	first := firstWord(chunk)
	var src string
	body := false
	switch first {
	case "package":
		src = chunk + "\n"
	case "import", "type", "func", "var", "wire":
		src = "package snippet\n\n" + chunk + "\n"
	default:
		src = "package snippet\n\nfunc snippet() {\n" + chunk + "\n}\n"
		body = true
	}
	if _, err := oldsyntax.Parse(src); err != nil {
		return chunk, nil
	}
	out, notes, err := Translate(src)
	if err != nil {
		return chunk, []Note{{Text: err.Error()}}
	}
	if len(notes) > 0 {
		return chunk, notes
	}
	out = strings.TrimRight(out, "\n")
	if body {
		ls := strings.Split(out, "\n")
		if len(ls) >= 2 && strings.HasPrefix(ls[0], "func snippet() {") && ls[len(ls)-1] == "}" {
			ls = ls[1 : len(ls)-1]
			for j := range ls {
				ls[j] = strings.TrimPrefix(ls[j], "\t")
			}
			out = strings.Join(ls, "\n")
		} else if strings.HasPrefix(out, "func snippet() { ") && strings.HasSuffix(out, " }") {
			out = strings.TrimSuffix(strings.TrimPrefix(out, "func snippet() { "), " }")
		}
	}
	return out, nil
}

// splitChunks splits a fence at blank lines outside brackets.
func splitChunks(text string) []string {
	data := []byte(text)
	fset := token.NewFileSet()
	file := fset.AddFile("x", -1, len(data))
	var s scanner.Scanner
	s.Init(file, data, func(token.Position, string) {}, scanner.ScanComments)
	type mark struct{ offset, depth int }
	var marks []mark
	depth := 0
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			break
		}
		switch tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
		}
		marks = append(marks, mark{file.Offset(pos), depth})
	}
	depthAt := func(offset int) int {
		d := 0
		for _, m := range marks {
			if m.offset >= offset {
				break
			}
			d = m.depth
		}
		return d
	}
	var chunks, current []string
	offset := 0
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" && depthAt(offset) == 0 {
			if len(current) > 0 {
				chunks = append(chunks, strings.Join(current, "\n"))
				current = nil
			}
		} else {
			current = append(current, line)
		}
		offset += len(line) + 1
	}
	if len(current) > 0 {
		chunks = append(chunks, strings.Join(current, "\n"))
	}
	return chunks
}

// firstWord is the first word of the first line of text that is neither
// blank nor a line comment, up to a blank, ( or {; "" when there is none.
func firstWord(text string) string {
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		if words := strings.FieldsFunc(t, func(r rune) bool { return r == ' ' || r == '\t' || r == '(' || r == '{' }); len(words) > 0 {
			return words[0]
		}
		return ""
	}
	return ""
}
