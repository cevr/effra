package check

import (
	"strings"
	"testing"
)

// decidedMessages has one row per message whose text the review checked
// (spec D-A7: messages name the decided form; `let`, never `var`): the
// program's one diagnostic is S108 at line, containing msg.
var decidedMessages = []struct {
	name, src string
	line      int
	msg       string
}{
	{"a local let with no zero value", `type Pending {}
type Paid { N int }
type Payment = Pending | Paid
func a() Payment {
	let p Payment
	return p
}`, 5, "let p of type main.Payment needs a value"},
	{"a package-level let with no zero value", `type Pending {}
type Paid { N int }
type Payment = Pending | Paid
let P Payment
func a() Payment {
	return P
}`, 4, "let P of type main.Payment needs a value"},
	{"a package-level let is immutable", `let Limit = 10
func b() {
	Limit = 3
}`, 3, "the package-level let Limit is immutable"},
}

// TestDecidedMessages: each message speaks the decided language.
func TestDecidedMessages(t *testing.T) {
	for _, row := range decidedMessages {
		t.Run(row.name, func(t *testing.T) {
			_, diags := CheckSource(row.src + "\n")
			if len(diags) != 1 || diags[0].Code != CodeType || diags[0].Line != row.line || !strings.Contains(diags[0].Message, row.msg) {
				t.Fatalf("want one S108 at line %d containing %q, got %s", row.line, row.msg, describeDiags(diags))
			}
			for _, old := range []string{"var ", "failure row", "service row"} {
				if strings.Contains(diags[0].Message, old) {
					t.Fatalf("the message names %q: %s", old, diags[0].Message)
				}
			}
		})
	}
}
