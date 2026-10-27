package syntax

import (
	"fmt"
	goscanner "go/scanner"
	gotoken "go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var fenceStart = regexp.MustCompile("^```go\\s*$")

// specFences returns every ```go fence of docs/spec/*.md, split into chunks
// at blank lines outside brackets. A chunk that starts with `package` is a
// file; one that starts with import, type, func, var or wire is wrapped in
// `package snippet`; any other is the body of `func snippet()`.
func specFences(t testing.TB) map[string]string {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join("..", "..", "..", "docs", "spec", "*.md"))
	inputs := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(data), "\n")
		fence := 0
		for i := 0; i < len(lines); i++ {
			if !fenceStart.MatchString(lines[i]) {
				continue
			}
			fence++
			var body []string
			for i++; i < len(lines) && !strings.HasPrefix(lines[i], "```"); i++ {
				body = append(body, lines[i])
			}
			for c, chunk := range splitChunks(strings.Join(body, "\n")) {
				inputs[fmt.Sprintf("%s#fence%d.%d", filepath.Base(path), fence, c+1)] = wrapChunk(chunk)
			}
		}
	}
	if len(inputs) < 20 {
		t.Fatalf("found %d spec fence chunks; the extraction is broken", len(inputs))
	}
	return inputs
}

func splitChunks(text string) []string {
	src := []byte(text)
	fset := gotoken.NewFileSet()
	file := fset.AddFile("fence", -1, len(src))
	var s goscanner.Scanner
	s.Init(file, src, nil, goscanner.ScanComments)
	type mark struct{ offset, depth int }
	var marks []mark
	depth := 0
	for {
		pos, tok, _ := s.Scan()
		if tok == gotoken.EOF {
			break
		}
		switch tok {
		case gotoken.LBRACE, gotoken.LPAREN, gotoken.LBRACK:
			depth++
		case gotoken.RBRACE, gotoken.RPAREN, gotoken.RBRACK:
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

func wrapChunk(chunk string) string {
	first := ""
	for _, line := range strings.Split(chunk, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		first = strings.FieldsFunc(t, func(r rune) bool { return r == ' ' || r == '\t' || r == '(' || r == '{' })[0]
		break
	}
	switch first {
	case "package":
		return chunk + "\n"
	case "import", "type", "func", "var", "wire":
		return "package snippet\n\n" + chunk + "\n"
	}
	return "package snippet\n\nfunc snippet() {\n" + chunk + "\n}\n"
}

func TestSpecFencesParse(t *testing.T) {
	for name, src := range specFences(t) {
		if _, err := Parse(src); err != nil {
			t.Errorf("%s: %v\n%s", name, err, src)
		}
	}
}
