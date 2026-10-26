package sketch

import (
	gotoken "go/token"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestIdentifierRunesMatchGo sweeps every non-ASCII code point, in the first
// and in a later position, and requires the lexer to admit exactly the
// identifiers go/token admits. Anything else must be a lexer error, so no
// name Go cannot parse reaches emission.
func TestIdentifierRunesMatchGo(t *testing.T) {
	for r := rune(utf8.RuneSelf); r <= utf8.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		for _, name := range []string{"x" + string(r), string(r) + "x"} {
			toks, err := lex(name)
			admitted := err == nil && len(toks) == 3 && toks[0].kind == tIdent && toks[0].text == name
			if admitted != gotoken.IsIdentifier(name) {
				t.Fatalf("%q (U+%04X): lexer admits %v, go/token admits %v (err %v)", name, r, admitted, !admitted, err)
			}
		}
	}
}

// TestIdentifierBytes rejects invalid UTF-8 in identifier position with the
// syntax code, wherever it falls in the name.
func TestIdentifierBytes(t *testing.T) {
	for _, name := range []string{"x\xff", "\xffx", "x\xe2\xaa", "x\xc0\x80"} {
		src := "package demo\nfunc main() uses Console {\n  " + name + " := \"ok\"\n  Console.Println(" + name + ")\n}\n"
		if got := strings.Join(codes(t, src), " "); got != "S001" {
			t.Fatalf("%q: got %q, want S001", name, got)
		}
	}
}
