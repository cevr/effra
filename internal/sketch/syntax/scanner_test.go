package syntax

import (
	"errors"
	gotoken "go/token"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestIdentifierRunesMatchGo sweeps every non-ASCII code point, in the first
// and in a later position, and requires the scanner to admit exactly the
// identifiers go/token admits (ported from the prototype's lex_test.go).
func TestIdentifierRunesMatchGo(t *testing.T) {
	for r := rune(utf8.RuneSelf); r <= utf8.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		for _, name := range []string{"x" + string(r), string(r) + "x"} {
			s := scan(name)
			admitted := s.err == nil && len(s.toks) == 3 && s.toks[0].tok == IDENT && s.toks[0].lit == name
			if admitted != gotoken.IsIdentifier(name) {
				t.Fatalf("%q (U+%04X): scanner admits %v, go/token admits %v (err %v)", name, r, admitted, !admitted, s.err)
			}
		}
	}
}

// TestIdentifierBytes rejects invalid UTF-8 in identifier position with
// S001 at the offending byte, wherever it falls in the name.
func TestIdentifierBytes(t *testing.T) {
	for _, name := range []string{"x\xff", "\xffx", "x\xe2\xaa", "x\xc0\x80"} {
		src := "package demo\nfunc main() uses Console {\n  " + name + " := \"ok\"\n}\n"
		_, err := Parse(src)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeSyntax || e.Line != 3 {
			t.Fatalf("%q: got %v, want S001 on line 3", name, err)
		}
		if bad := strings.IndexFunc(src[e.Offset:], func(r rune) bool { return r == utf8.RuneError }); bad != 0 {
			t.Fatalf("%q: the error at byte %d is not at the invalid byte", name, e.Offset)
		}
	}
}

// TestSemicolonInsertion: Go's rule, including after break, continue, ++,
// --, return, a closing brace and a block comment that spans lines.
func TestSemicolonInsertion(t *testing.T) {
	cases := map[string][]Token{
		"x\n":               {IDENT, SEMICOLON, EOF},
		"break\n":           {BREAK, SEMICOLON, EOF},
		"continue\n":        {CONTINUE, SEMICOLON, EOF},
		"return\n":          {RETURN, SEMICOLON, EOF},
		"i++\n":             {IDENT, INC, SEMICOLON, EOF},
		"i--\n":             {IDENT, DEC, SEMICOLON, EOF},
		"}\n":               {RBRACE, SEMICOLON, EOF},
		"x /* a\nb */ y":    {IDENT, SEMICOLON, IDENT, SEMICOLON, EOF},
		"x /* a */\n":       {IDENT, SEMICOLON, EOF},
		"try\n":             {TRY, EOF},
		"x // c\n":          {IDENT, SEMICOLON, EOF},
		"500ms 'r' \"s\"\n": {DURATION, RUNE, STRING, SEMICOLON, EOF},
		"a &^= b <<= c":     {IDENT, AND_NOT_ASSIGN, IDENT, SHL_ASSIGN, IDENT, SEMICOLON, EOF},
		"go.Error":          {GO, PERIOD, IDENT, SEMICOLON, EOF},
		"initial step":      {IDENT, IDENT, SEMICOLON, EOF},
	}
	for src, want := range cases {
		s := scan(src)
		if s.err != nil {
			t.Fatalf("%q: %v", src, s.err)
		}
		var got []Token
		for _, tok := range s.toks {
			got = append(got, tok.tok)
		}
		if len(got) != len(want) {
			t.Fatalf("%q: tokens %v, want %v", src, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%q: tokens %v, want %v", src, got, want)
			}
		}
	}
}

// TestNumberForms: the #81 forms stay S001 with a message that names them.
func TestNumberForms(t *testing.T) {
	for src, want := range map[string]string{
		"0x1F":  "hexadecimal",
		"1.5":   "floating-point",
		"0.A":   "floating-point",
		"1.":    "floating-point",
		"1_000": "separators",
		"5kg":   `unknown number suffix "kg"`,
	} {
		s := scan(src)
		if s.err == nil || !strings.Contains(s.err.Message, want) {
			t.Fatalf("%q: %v, want a message with %q", src, s.err, want)
		}
	}
}

// TestKeywords: 27 keywords; goto, fallthrough and const are not keywords
// but are never names.
func TestKeywords(t *testing.T) {
	if n := len(Keywords()); n != 27 {
		t.Fatalf("%d keywords, want 27", n)
	}
	for word := range removed {
		if _, ok := keywords[word]; ok {
			t.Fatalf("%s is a keyword", word)
		}
		if _, err := Parse("package p\n\nvar " + word + " = 1\n"); err == nil {
			t.Fatalf("%s is a name", word)
		}
	}
}

// TestPositions: a CRLF source keeps byte offsets, and a span at the end of
// the file is empty.
func TestPositions(t *testing.T) {
	_, err := Parse("package p\r\n\r\nfunc f() {\r\n\tg(\r\n")
	var e *Error
	if !errors.As(err, &e) || e.Line != 5 || e.Length != 0 {
		t.Fatalf("got %+v, want an empty span on line 5", err)
	}
	f, err := Parse("package p\r\n\r\n// é\r\nfunc f() {}\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if line, column := f.Position(f.Decls[0].Pos()); line != 4 || column != 1 {
		t.Fatalf("func at %d:%d, want 4:1", line, column)
	}
	if text := f.Comments[0].List[0].Text; text != "// é" {
		t.Fatalf("comment text %q keeps its carriage return", text)
	}
	if doc := f.Decls[0].(*FuncDecl).Doc; doc == nil || doc.List[0].Text != "// é" {
		t.Fatalf("the func has no doc comment")
	}
}
