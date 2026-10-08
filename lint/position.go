package lint

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

// sourceBounds checks finding spans against the admitted source. Every span
// must lie inside the source's bytes. When the host supplied the source
// text, a span must also start and end on UTF-8 boundaries, and its line and
// column must be the ones its offset has: the 1-based line, and the 1-based
// byte column within that line, as the compiler's lexer counts them. Every
// projection of a finding (byte span, line/column, LSP UTF-16 range) then
// names the same place.
type sourceBounds struct {
	source Source
	// lines holds the offset of each line start, built on first use when
	// the text is available.
	lines []int
	err   error
}

func newSourceBounds(source Source) *sourceBounds {
	bounds := &sourceBounds{source: source}
	if source.Text == "" {
		return bounds
	}
	if len(source.Text) != source.Bytes {
		bounds.err = fmt.Errorf("source text has %d bytes; the snapshot admits %d", len(source.Text), source.Bytes)
		return bounds
	}
	bounds.lines = []int{0}
	for offset := 0; offset < len(source.Text); offset++ {
		if source.Text[offset] == '\n' {
			bounds.lines = append(bounds.lines, offset+1)
		}
	}
	return bounds
}

func (b *sourceBounds) check(span Span) error {
	source := b.source
	if span.Offset < 0 || span.Length < 0 || span.Offset > source.Bytes || span.Length > source.Bytes-span.Offset || span.Line < 1 || span.Column < 1 {
		return fmt.Errorf("span %+v is outside the %d-byte source", span, source.Bytes)
	}
	if b.err != nil {
		return b.err
	}
	if b.lines == nil {
		return nil
	}
	text := source.Text
	for _, boundary := range []int{span.Offset, span.Offset + span.Length} {
		if boundary < len(text) && !utf8.RuneStart(text[boundary]) {
			return fmt.Errorf("span %+v does not start and end on UTF-8 character boundaries", span)
		}
	}
	line := sort.Search(len(b.lines), func(i int) bool { return b.lines[i] > span.Offset })
	if column := span.Offset - b.lines[line-1] + 1; span.Line != line || span.Column != column {
		return fmt.Errorf("span %+v is at line %d, column %d of the source", span, line, column)
	}
	return nil
}
