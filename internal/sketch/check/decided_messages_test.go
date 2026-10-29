package check

import (
	"strings"
	"testing"
)

// decidedMessages has one row per message whose text the review checked
// (spec D-A7: messages name the decided form; `let`, never `var`): the
// program's one diagnostic is code (S108 when empty) at line, containing
// msg.
var decidedMessages = []struct {
	name, src string
	line      int
	msg       string
	code      string // S108 when empty
}{
	{"a local let with no zero value", `type Pending {}
type Paid { N int }
type Payment = Pending | Paid
func a() Payment {
	let p Payment
	return p
}`, 5, "let p of type main.Payment needs a value", ""},
	{"a package-level let with no zero value", `type Pending {}
type Paid { N int }
type Payment = Pending | Paid
let P Payment
func a() Payment {
	return P
}`, 4, "let P of type main.Payment needs a value", ""},
	{"a package-level let is immutable", `let Limit = 10
func b() {
	Limit = 3
}`, 3, "the package-level let Limit is immutable", ""},
	// An operation-type value and a value that satisfies it through its
	// functions (196m) do not compare: the lowered == has no meaning
	// (round-1 review m7; D-E2 keeps the general rule for stage E).
	{"an operation value is not compared with a record", `type Store { Get() string }
type Mem { V string }
func Get(m Mem) string { return m.V }
func cmp(s Store, m Mem) bool {
	return s == m
}`, 5, "cannot compare main.Store with main.Mem: a main.Mem satisfies main.Store through its functions", ""},
	{"the comparison is refused either way round", `type Store { Get() string }
type Mem { V string }
func Get(m Mem) string { return m.V }
func cmp(s Store, m Mem) bool {
	return m != s
}`, 5, "cannot compare main.Mem with main.Store", ""},
	// A removed built-in's type argument is no second mistake (m8).
	{"new(int) is one S104", `func b() {
	let n = new(int)
	n
}`, 2, "new is not predeclared in Ergo", CodeUnknown},
}

// TestDecidedMessages: each message speaks the decided language.
func TestDecidedMessages(t *testing.T) {
	for _, row := range decidedMessages {
		t.Run(row.name, func(t *testing.T) {
			_, diags := CheckSource(row.src + "\n")
			code := row.code
			if code == "" {
				code = CodeType
			}
			if len(diags) != 1 || diags[0].Code != code || diags[0].Line != row.line || !strings.Contains(diags[0].Message, row.msg) {
				t.Fatalf("want one %s at line %d containing %q, got %s", code, row.line, row.msg, describeDiags(diags))
			}
			for _, old := range []string{"var ", "failure row", "service row"} {
				if strings.Contains(diags[0].Message, old) {
					t.Fatalf("the message names %q: %s", old, diags[0].Message)
				}
			}
		})
	}
}
