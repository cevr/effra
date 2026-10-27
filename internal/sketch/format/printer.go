package format

import (
	"strings"
	"unicode/utf8"

	"effra.local/prototype/internal/sketch/syntax"
)

// outLine is one output line: its indentation (tabs) and its cells. Every
// cell but the last is padded to the width of its column block. indent is
// -1 for the continuation of a multi-line raw string or block comment,
// which is never re-indented.
type outLine struct {
	indent int
	cells  []string
}

type printer struct {
	file     *syntax.File
	comments []*syntax.Comment
	ci       int // next comment to print

	lines   []outLine
	cells   []string
	cell    strings.Builder
	started bool // the current line has an item
	indent  int

	pendingNL    int  // line breaks before the next item
	pendingSpace bool // a blank before the next item on the same line
	noBlank      bool // the next line break is never a blank line

	lastEnd syntax.Pos // end of the last printed source item (token or comment)
	lastTok syntax.Pos // end of the last printed source token
}

func newPrinter(f *syntax.File) *printer {
	p := &printer{file: f, lastEnd: syntax.NoPos, lastTok: syntax.NoPos}
	for _, g := range f.Comments {
		p.comments = append(p.comments, g.List...)
	}
	return p
}

func (p *printer) line(pos syntax.Pos) int { return p.file.Line(pos) }

// breakBefore reports whether the source has a line break between the last
// printed token and pos.
func (p *printer) breakBefore(pos syntax.Pos) bool {
	return p.lastTok.IsValid() && p.line(pos) > p.line(p.lastTok)
}

// ---------------------------------------------------------------- whitespace requests

func (p *printer) space() { p.pendingSpace = true }

func (p *printer) newline() {
	if p.pendingNL == 0 {
		p.pendingNL = 1
	}
}

// newlineNoBlank requests a line break that is never a blank line (after
// an opening brace, before a closing one).
func (p *printer) newlineNoBlank() {
	p.newline()
	p.noBlank = true
}

// gap is the whitespace before the token at pos in a gap whose line break
// the source decides: a line break if the source has one, else ws.
func (p *printer) gap(pos syntax.Pos, blank bool) bool {
	if p.breakBefore(pos) {
		p.newline()
		return true
	}
	if blank {
		p.space()
	}
	return false
}

// ---------------------------------------------------------------- output

// emit applies the pending whitespace before an item at pos (NoPos for a
// synthesized token).
func (p *printer) emit(pos syntax.Pos) {
	if p.pendingNL > 0 && (p.started || len(p.lines) > 0) {
		n := 1
		if pos.IsValid() && p.lastEnd.IsValid() && !p.noBlank && p.line(pos)-p.line(p.lastEnd) >= 2 {
			n = 2
		}
		p.endLine()
		if n == 2 {
			p.lines = append(p.lines, outLine{})
		}
	}
	p.pendingNL = 0
	p.noBlank = false
	if !p.started {
		p.started = true
		p.pendingSpace = false
		p.lineIndent()
		return
	}
	if p.pendingSpace {
		p.cell.WriteByte(' ')
	}
	p.pendingSpace = false
}

func (p *printer) lineIndent() {
	p.lines = append(p.lines, outLine{indent: p.indent})
	p.lines[len(p.lines)-1].cells = nil
}

func (p *printer) endLine() {
	if !p.started {
		return
	}
	p.cells = append(p.cells, p.cell.String())
	p.cell.Reset()
	for len(p.cells) > 1 && p.cells[len(p.cells)-1] == "" {
		p.cells = p.cells[:len(p.cells)-1]
	}
	p.lines[len(p.lines)-1].cells = p.cells
	p.cells = nil
	p.started = false
}

// sep ends the current cell: the next text aligns with the cells of the
// same column on the neighbouring lines.
func (p *printer) sep() {
	p.cells = append(p.cells, strings.TrimRight(p.cell.String(), " "))
	p.cell.Reset()
}

// lastByte is the last byte written on the current line, or 0.
func (p *printer) lastByte() byte {
	if s := p.cell.String(); s != "" {
		return s[len(s)-1]
	}
	for i := len(p.cells) - 1; i >= 0; i-- {
		if s := p.cells[i]; s != "" {
			return s[len(s)-1]
		}
	}
	return 0
}

// mergers are the byte pairs that would lex as one token, or start a
// comment, if written without a blank between them.
var mergers = map[[2]byte]bool{
	{'-', '-'}: true, {'+', '+'}: true, {'&', '&'}: true, {'&', '^'}: true, {'|', '|'}: true,
	{'<', '-'}: true, {'<', '<'}: true, {'>', '>'}: true, {'=', '='}: true, {'!', '='}: true,
	{'<', '='}: true, {'>', '='}: true, {':', '='}: true, {'.', '.'}: true, {'/', '/'}: true,
	{'/', '*'}: true, {'-', '='}: true, {'+', '='}: true, {'*', '='}: true, {'/', '='}: true,
	{'%', '='}: true, {'&', '='}: true, {'|', '='}: true, {'^', '='}: true, {'*', '/'}: true,
}

// write appends text; a text with line breaks (a raw string or block
// comment) continues on lines that are never re-indented.
func (p *printer) write(text string) {
	if text == "" {
		return
	}
	if last := p.lastByte(); last != 0 && mergers[[2]byte{last, text[0]}] {
		p.cell.WriteByte(' ')
	}
	for {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			p.cell.WriteString(text)
			return
		}
		p.cell.WriteString(text[:i])
		p.endLine()
		p.started = true
		p.lines = append(p.lines, outLine{indent: -1})
		text = text[i+1:]
	}
}

// token prints a source token at pos after the comments before it.
func (p *printer) token(pos syntax.Pos, text string) {
	p.tokenEnd(pos, text, pos+syntax.Pos(len(text)))
}

func (p *printer) tokenEnd(pos syntax.Pos, text string, end syntax.Pos) {
	p.flushComments(pos)
	p.emit(pos)
	p.write(text)
	p.lastEnd, p.lastTok = end, end
}

// synth prints a token that is not in the source, such as a comma added
// before a closer on its own line.
func (p *printer) synth(text string) {
	p.emit(syntax.NoPos)
	p.write(text)
}

// ---------------------------------------------------------------- comments

// flushComments prints every comment before pos.
func (p *printer) flushComments(pos syntax.Pos) {
	for p.ci < len(p.comments) && p.comments[p.ci].Slash < pos {
		p.comment(pos)
	}
}

// flushClauseComments prints, at the current (clause body) indentation, the
// comments before pos up to the first one that a blank line separates from
// what precedes it; that one and the rest belong to the next clause.
func (p *printer) flushClauseComments(pos syntax.Pos) {
	for p.ci < len(p.comments) && p.comments[p.ci].Slash < pos {
		c := p.comments[p.ci]
		if p.lastEnd.IsValid() && p.line(c.Slash)-p.line(p.lastEnd) >= 2 {
			return
		}
		p.comment(pos)
	}
}

func (p *printer) comment(limit syntax.Pos) {
	c := p.comments[p.ci]
	next := limit
	if p.ci+1 < len(p.comments) && p.comments[p.ci+1].Slash < limit {
		next = p.comments[p.ci+1].Slash
	}
	p.ci++
	isLine := strings.HasPrefix(c.Text, "//")
	text := c.Text
	if isLine {
		// A line comment ends at a newline; trailing blanks, and a carriage
		// return the scanner kept because a blank followed it, are dropped,
		// so the output reads back as the same comment.
		text = strings.TrimRight(text, " \t\r")
	}
	trailing := p.lastEnd.IsValid() && p.line(c.Slash) == p.line(p.lastEnd)
	switch {
	case trailing && p.pendingNL > 0:
		// A comment that ends a line aligns with its neighbours.
		nl, noBlank := p.pendingNL, p.noBlank
		p.pendingNL, p.pendingSpace = 0, false
		p.emit(c.Slash)
		p.sep()
		p.cell.WriteString(text)
		p.pendingNL, p.noBlank = nl, noBlank
	case trailing:
		p.space()
		p.emit(c.Slash)
		p.cell.WriteString(text)
		if isLine {
			p.newline()
		} else {
			p.space()
		}
	default:
		if p.lastEnd.IsValid() {
			p.newline()
		}
		p.emit(c.Slash)
		p.write(text)
		if isLine || p.line(next) > p.line(c.EndPos) {
			p.newline()
		} else {
			p.space()
		}
	}
	p.lastEnd = c.EndPos
}

// ---------------------------------------------------------------- rendering

// render aligns the cells of each column block and joins the lines. A
// column block is a run of adjacent lines with the same indentation that
// all have a cell in that column followed by another cell, as in
// text/tabwriter; blocks of a column nest inside the blocks of the column
// before it. Cells are padded with spaces to the block's widest cell plus
// one; a block of empty cells takes no width.
func (p *printer) render() string {
	p.flushComments(p.file.EOF + 1)
	p.endLine()
	lines := p.lines
	widths := make([][]int, len(lines))
	p.align(lines, widths, 0, len(lines), 0)
	var b strings.Builder
	for i, l := range lines {
		if l.indent > 0 {
			b.WriteString(strings.Repeat("\t", l.indent))
		}
		for j, c := range l.cells {
			b.WriteString(c)
			if j < len(l.cells)-1 {
				if w := widths[i][j]; w > 0 {
					b.WriteString(strings.Repeat(" ", w-utf8.RuneCountInString(c)))
				}
			}
		}
		b.WriteByte('\n')
	}
	out := b.String()
	// Lines that were only a blank cell carry no trailing blanks.
	return strings.TrimLeft(out, "\n")
}

func (p *printer) align(lines []outLine, widths [][]int, from, to, column int) {
	for i := from; i < to; {
		if len(lines[i].cells) <= column+1 {
			i++
			continue
		}
		j := i + 1
		for j < to && len(lines[j].cells) > column+1 && lines[j].indent == lines[i].indent {
			j++
		}
		width := 0
		for k := i; k < j; k++ {
			width = max(width, utf8.RuneCountInString(lines[k].cells[column]))
		}
		if width > 0 {
			width++
		}
		for k := i; k < j; k++ {
			for len(widths[k]) <= column {
				widths[k] = append(widths[k], 0)
			}
			widths[k][column] = width
		}
		p.align(lines, widths, i, j, column+1)
		i = j
	}
}
