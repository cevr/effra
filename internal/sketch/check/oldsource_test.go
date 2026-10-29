package check

import (
	"fmt"
	"strings"

	"ergo.local/prototype/internal/migrate196"
)

// fromOld translates a source that a test generates in the old Go-shaped
// syntax (the text go/types reads, and the generators' templates) into the
// decided syntax of #196, with the one-off translator (the merge round's
// bridge; check-v1 rewrites the generators and deletes it). A source that
// the translator cannot carry without a hand edit is a generator bug here:
// fromOld panics with the notes, so the test fails at its generator.
func fromOld(src string) string {
	out, notes, err := migrate196.Translate(src)
	if err != nil {
		panic(fmt.Sprintf("fromOld: %v\n%s", err, src))
	}
	if len(notes) > 0 {
		var b strings.Builder
		for _, n := range notes {
			fmt.Fprintf(&b, "\n\tline %d: %s", n.Line, n.Text)
		}
		panic(fmt.Sprintf("fromOld: the translation needs a hand edit:%s\n%s", b.String(), src))
	}
	return out
}

// FromOld is fromOld for the external tests.
var FromOld = fromOld
