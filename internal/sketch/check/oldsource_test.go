package check

import (
	"fmt"
	"regexp"
	"strings"

	"ergo.local/prototype/internal/migrate196"
	"ergo.local/prototype/internal/sketch/format"
)

// fromOld translates a source that a test generates in the old Go-shaped
// syntax (the text go/types reads, and the generators' templates) into the
// decided syntax of #196, with the one-off translator (the merge round's
// bridge; check-v1 rewrites the generators and deletes it). A bodiless
// function, which the decided syntax spells only as an extern, is bound
// to `extern go fixture.<name>`, as the fixtures bind theirs. Any other
// source that the translator cannot carry without a hand edit is a
// generator bug here: fromOld panics with the notes, so the test fails at
// its generator.
func fromOld(src string) string {
	out, notes, err := migrate196.Translate(src)
	if err != nil {
		panic(fmt.Sprintf("fromOld: %v\n%s", err, src))
	}
	var other []migrate196.Note
	for _, n := range notes {
		if !strings.HasPrefix(n.Text, "a function without a body") {
			other = append(other, n)
		}
	}
	if len(other) > 0 {
		var b strings.Builder
		for _, n := range other {
			fmt.Fprintf(&b, "\n\tline %d: %s", n.Line, n.Text)
		}
		panic(fmt.Sprintf("fromOld: the translation needs a hand edit:%s\n%s", b.String(), src))
	}
	if len(notes) == 0 {
		return out
	}
	// With a note the translation is left unformatted: bind each bodiless
	// function (a func line with no block), then format.
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if m := bodilessFunc.FindStringSubmatch(line); m != nil {
			lines[i] = "extern go fixture." + m[1] + " " + line
		}
	}
	formatted, err := format.Source(strings.Join(lines, "\n"))
	if err != nil {
		panic(fmt.Sprintf("fromOld: the bound translation does not format: %v\n%s", err, src))
	}
	return formatted
}

// bodilessFunc is a translated function declaration line with no block.
var bodilessFunc = regexp.MustCompile(`^func ([A-Za-z_][A-Za-z0-9_]*)[^{]*$`)

// FromOld is fromOld for the external tests.
var FromOld = fromOld
