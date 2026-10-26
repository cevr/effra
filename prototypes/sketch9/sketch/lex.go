// Package sketch is a prototype compiler for the sketch-9 core: it parses a
// small Go-shaped subset, checks error and capability rows, and emits direct Go.
package sketch

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pos is a 1-based source position.
type Pos struct{ Line, Col int }

type tokKind int

const (
	tEOF tokKind = iota
	tIdent
	tInt
	tDuration
	tString
	tOp // punctuation and operators
	tSemi
)

type token struct {
	kind tokKind
	text string
	pos  Pos
}

var keywords = map[string]bool{
	"package": true, "import": true, "type": true, "struct": true, "interface": true,
	"func": true, "return": true, "if": true, "else": true, "switch": true, "case": true,
	"wire": true, "uses": true, "try": true, "catch": true,
	// Go keywords outside the subset: reserved, so they parse as errors.
	"break": true, "chan": true, "const": true, "continue": true, "default": true,
	"defer": true, "fallthrough": true, "for": true, "go": true, "goto": true,
	"map": true, "range": true, "select": true, "var": true,
}

// identRune applies Go's identifier rule to one decoded rune: a letter or '_'
// anywhere, and a decimal digit after the first rune.
func identRune(r rune, first bool) bool {
	return r == '_' || unicode.IsLetter(r) || !first && unicode.IsDigit(r)
}

// lex splits src into tokens, inserting semicolons at line ends with Go's rule.
func lex(src string) ([]token, error) {
	var toks []token
	line, col := 1, 1
	i := 0
	needSemi := func() bool {
		if len(toks) == 0 {
			return false
		}
		last := toks[len(toks)-1]
		switch last.kind {
		case tIdent:
			return !keywords[last.text] || last.text == "return"
		case tInt, tDuration, tString:
			return true
		case tOp:
			return last.text == ")" || last.text == "]" || last.text == "}"
		}
		return false
	}
	advance := func(n int) {
		for k := 0; k < n; k++ {
			if src[i] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			i++
		}
	}
	for i < len(src) {
		c := src[i]
		pos := Pos{line, col}
		switch {
		case c == '\n':
			if needSemi() {
				toks = append(toks, token{tSemi, "\n", pos})
			}
			advance(1)
		case c == ' ' || c == '\t' || c == '\r':
			advance(1)
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				advance(1)
			}
		case c == '"':
			j := i + 1
			for j < len(src) && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				if j < len(src) && src[j] == '\n' {
					return nil, fmt.Errorf("%d:%d: unterminated string", pos.Line, pos.Col)
				}
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("%d:%d: unterminated string", pos.Line, pos.Col)
			}
			toks = append(toks, token{tString, src[i : j+1], pos})
			advance(j + 1 - i)
		case c == '`':
			j := strings.IndexByte(src[i+1:], '`')
			if j < 0 {
				return nil, fmt.Errorf("%d:%d: unterminated raw string", pos.Line, pos.Col)
			}
			toks = append(toks, token{tString, src[i : i+j+2], pos})
			advance(j + 2)
		case '0' <= c && c <= '9':
			j := i
			for j < len(src) && '0' <= src[j] && src[j] <= '9' {
				j++
			}
			k := j
			for k < len(src) {
				r, size := utf8.DecodeRuneInString(src[k:])
				if !unicode.IsLetter(r) {
					break
				}
				k += size
			}
			kind := tInt
			if k > j {
				switch src[j:k] {
				case "ns", "us", "ms", "s", "m", "h":
					kind = tDuration
				default:
					return nil, fmt.Errorf("%d:%d: unknown number suffix %q", pos.Line, pos.Col, src[j:k])
				}
			}
			toks = append(toks, token{kind, src[i:k], pos})
			advance(k - i)
		case c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c >= utf8.RuneSelf:
			// Identifiers are tested on decoded code points, never on single
			// bytes: the bytes of a multi-byte symbol can each look like a
			// Latin-1 letter. A non-ASCII rune that cannot start an identifier
			// is rejected here, including one that ends the previous identifier.
			r, size := utf8.DecodeRuneInString(src[i:])
			if r == utf8.RuneError && size == 1 {
				return nil, fmt.Errorf("%d:%d: invalid UTF-8 byte %#x", pos.Line, pos.Col, c)
			}
			if !identRune(r, true) {
				return nil, fmt.Errorf("%d:%d: unexpected character %q", pos.Line, pos.Col, r)
			}
			j := i + size
			for j < len(src) {
				r, size := utf8.DecodeRuneInString(src[j:])
				if !identRune(r, false) { // U+FFFD, as for invalid UTF-8, is no letter
					break
				}
				j += size
			}
			toks = append(toks, token{tIdent, src[i:j], pos})
			advance(j - i)
		default:
			op := ""
			for _, cand := range []string{":=", "==", "!=", "&&", "||", "<=", ">="} {
				if strings.HasPrefix(src[i:], cand) {
					op = cand
					break
				}
			}
			if op == "" {
				if !strings.ContainsRune("{}()[],;.:=+-*/|<>!", rune(c)) {
					return nil, fmt.Errorf("%d:%d: unexpected character %q", pos.Line, pos.Col, c)
				}
				op = string(c)
			}
			if op == ";" {
				toks = append(toks, token{tSemi, ";", pos})
			} else {
				toks = append(toks, token{tOp, op, pos})
			}
			advance(len(op))
		}
	}
	if needSemi() {
		toks = append(toks, token{tSemi, "\n", Pos{line, col}})
	}
	toks = append(toks, token{tEOF, "", Pos{line, col}})
	return toks, nil
}
