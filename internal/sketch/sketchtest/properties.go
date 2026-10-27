package sketchtest

import (
	"go/scanner"
	"go/token"
	"math/rand/v2"
	"strings"
)

// The properties below lex sources with go/scanner, independently of the
// parser under test: the 2026 lexical grammar is Go's for every token the
// corpus uses.

type lexeme struct {
	start, end int
	tok        token.Token
	text       string
}

func lexAll(src string) ([]lexeme, bool) {
	data := []byte(src)
	fset := token.NewFileSet()
	file := fset.AddFile("x", -1, len(data))
	errs := 0
	var s scanner.Scanner
	s.Init(file, data, func(token.Position, string) { errs++ }, scanner.ScanComments)
	var out []lexeme
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		text := lit
		if text == "" {
			text = tok.String()
		}
		start := file.Offset(pos)
		end := start + len(text)
		if tok == token.COMMENT || tok == token.STRING {
			end = start + rawLen(src[start:])
			text = src[start:end]
		}
		out = append(out, lexeme{start, end, tok, text})
	}
	return out, errs == 0
}

func rawLen(rest string) int {
	switch {
	case strings.HasPrefix(rest, "//"):
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			return i
		}
		return len(rest)
	case strings.HasPrefix(rest, "/*"):
		if i := strings.Index(rest[2:], "*/"); i >= 0 {
			return i + 4
		}
		return len(rest)
	case strings.HasPrefix(rest, "`"):
		if i := strings.IndexByte(rest[1:], '`'); i >= 0 {
			return i + 2
		}
		return len(rest)
	}
	for i := 1; i < len(rest); i++ {
		if rest[i] == '\\' {
			i++
			continue
		}
		if rest[i] == '"' {
			return i + 1
		}
	}
	return len(rest)
}

// Comments returns every comment in order, with trailing blanks dropped
// from each line and leading blanks from a block comment's inner lines.
func Comments(src string) ([]string, bool) {
	lexemes, ok := lexAll(src)
	var out []string
	for _, l := range lexemes {
		if l.tok != token.COMMENT {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(l.text, "\r", ""), "\n")
		for i := range lines {
			lines[i] = strings.TrimRight(lines[i], " \t")
			if i > 0 {
				lines[i] = strings.TrimLeft(lines[i], " \t")
			}
		}
		out = append(out, strings.Join(lines, "\n"))
	}
	return out, ok
}

// Tokens returns the token sequence without comments, semicolons and a
// comma directly before a closer: what formatting must keep.
func Tokens(src string) ([]string, bool) {
	lexemes, ok := lexAll(src)
	var kept []lexeme
	for _, l := range lexemes {
		if l.tok != token.COMMENT && l.tok != token.SEMICOLON {
			kept = append(kept, l)
		}
	}
	var out []string
	for i, l := range kept {
		if l.tok == token.COMMA && i+1 < len(kept) {
			switch kept[i+1].tok {
			case token.RPAREN, token.RBRACK, token.RBRACE:
				continue
			}
		}
		out = append(out, l.text)
	}
	return out, ok
}

// FirstDiff returns the first index where a and b differ, or -1.
func FirstDiff(a, b []string) (int, string, string) {
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := "<none>", "<none>"
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return i, x, y
		}
	}
	return -1, "", ""
}

// Perturb rewrites the whitespace between tokens, keeping every token and
// the line breaks of each gap (one more in a gap that already holds a blank
// line): re-indentation, intra-line gaps, trailing blanks and leading and
// trailing newlines. A canonical formatter maps it to the same output.
func Perturb(src string, seed uint64) (string, bool) {
	lexemes, ok := lexAll(src)
	if !ok {
		return "", false
	}
	r := rand.New(rand.NewPCG(seed, 195))
	pick := func(options ...string) string { return options[r.IntN(len(options))] }
	var b strings.Builder
	prev := 0
	prevComment := false
	for i, l := range lexemes {
		gap := src[prev:l.start]
		switch {
		case strings.Trim(gap, " \t\r\n") != "":
			b.WriteString(gap)
		case i == 0:
			b.WriteString(pick("", "\n", "\n\n"))
		default:
			b.WriteString(perturbGap(gap, prevComment, r, pick))
		}
		b.WriteString(src[l.start:l.end])
		prev = l.end
		prevComment = l.tok == token.COMMENT && strings.HasPrefix(l.text, "//")
	}
	if prevComment {
		b.WriteString(pick("\n", "\n\n\n", ""))
	} else {
		b.WriteString(pick("\n", "\n\n\n", "  \n", ""))
	}
	return b.String(), true
}

func perturbGap(gap string, afterLineComment bool, r *rand.Rand, pick func(...string) string) string {
	n := strings.Count(gap, "\n")
	if n == 0 {
		if gap == "" {
			return ""
		}
		return pick(" ", "  ", "\t", " \t")
	}
	if n >= 2 {
		n += r.IntN(2)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if !(i == 0 && afterLineComment) && r.IntN(3) == 0 {
			b.WriteString(pick(" ", "\t "))
		}
		b.WriteByte('\n')
	}
	b.WriteString(pick("", "\t", "    ", "\t\t ", " "))
	return b.String()
}
