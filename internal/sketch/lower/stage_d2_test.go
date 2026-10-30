package lower

import (
	"strings"
	"testing"
)

// Stage D2 (check-v1): one for over a slice, a Map, a Range and a Seq
// (D-D4), `a..b` as a Range[T] value, a Map write, and len (D-D5). The
// refusals are the checker's (internal/sketch/check stage_d2_test.go);
// every form the stage admits runs here through the public CLI.

// TestErgoRunForDomains: a for over a slice binds the element, or the
// index and the element; a for over a Map binds the value, or the key and
// the value (summed, so the unspecified order does not show); a Range
// counts from Start up to End, End excluded, over int and int64, from a
// literal `a..b` and from a Range value; an empty Range runs no body; `_`
// ignores a binding; break and continue reach the for-in, through a match
// too; a return leaves the function from inside the loop; a function
// literal captures one iteration's binding; a binding may be a record
// pattern; and the domain is evaluated once.
func TestErgoRunForDomains(t *testing.T) {
	wantRun(t, `type Pair { A int, B int }

func report(name string, ok bool) needs Console {
	if ok {
		Console.Println(name)
	} else {
		Console.Println("FAIL " + name)
	}
}

func sumSlice(xs []int) int {
	let mut s = 0
	for x in xs {
		s += x
	}
	s
}

func weighted(xs []int) int {
	let mut s = 0
	for i, x in xs {
		s += i * x
	}
	s
}

func mapValues(m Map[string, int]) int {
	let mut s = 0
	for v in m {
		s += v
	}
	s
}

func mapKeys(m Map[string, int]) int {
	let mut s = 0
	for k, v in m {
		s += len(k) * v
	}
	s
}

func count(n int64) int64 {
	let mut s int64 = 0
	for i in 0..n {
		s += i
	}
	s
}

func fromValue(r Range[int]) int {
	let mut s = 0
	for i in r {
		s = s * 10 + i
	}
	s
}

func ignored(xs []string) int {
	let mut n = 0
	for _ in xs {
		n += 1
	}
	for _, _ in xs {
		n += 1
	}
	for i, _ in xs {
		n += i
	}
	n
}

func firstOver(xs []int, limit int) int {
	for x in xs {
		if x > limit {
			return x
		}
	}
	-1
}

func skipAndStop(xs []int) int {
	let mut s = 0
	for x in xs {
		if x == 2 {
			continue
		}
		match x {
			case 5: break
			case _: s += x
		}
	}
	s
}

func captured() int {
	let mut fs = []func() int{func() int { 0 }, func() int { 0 }, func() int { 0 }}
	for i in 1..4 {
		fs[i - 1] = func() int { i }
	}
	let mut s = 0
	for f in fs {
		s = s * 10 + f()
	}
	s
}

func pairs(ps []Pair) int {
	let mut s = 0
	for Pair{A: a, B: b} in ps {
		s += a * b
	}
	s
}

func calls(n int) int needs Console {
	Console.Println("domain")
	n
}

func once() int needs Console {
	let mut s = 0
	for i in 0..calls(3) {
		s += i
	}
	s
}

func main() needs Console {
	report("slice", sumSlice([]int{1, 2, 3}) == 6)
	report("slice index", weighted([]int{5, 6, 7}) == 20)
	report("map values", mapValues(Map[string, int]{"a": 1, "bb": 2, "ccc": 3}) == 6)
	report("map keys", mapKeys(Map[string, int]{"a": 1, "bb": 2, "ccc": 3}) == 14)
	report("range int64", count(5) == 10)
	report("range value", fromValue(1..4) == 123)
	report("empty range", fromValue(3..3) == 0 && fromValue(4..2) == 0)
	report("blank", ignored([]string{"x", "y", "z"}) == 9)
	report("return", firstOver([]int{1, 4, 9}, 3) == 4 && firstOver([]int{1}, 3) == -1)
	report("break continue", skipAndStop([]int{1, 2, 3, 5, 7}) == 4)
	report("captured", captured() == 123)
	report("pattern", pairs([]Pair{Pair{A: 2, B: 3}, Pair{A: 4, B: 5}}) == 26)
	report("once", once() == 3)
}
`, "slice\nslice index\nmap values\nmap keys\nrange int64\nrange value\nempty range\nblank\nreturn\nbreak continue\ncaptured\npattern\ndomain\nonce\n")
}

// TestErgoRunSeq: a for over a Seq[T] built by conversion from a function
// literal runs the body as yield; a break makes yield return false, so a
// lawful Seq stops (the Seq counts its yields); a Seq value is also called
// directly, through its declared function type.
func TestErgoRunSeq(t *testing.T) {
	wantRun(t, `func upTo(n int) Seq[int] {
	Seq[int](func(yield func(v int) bool) {
		let mut i = 0
		for i < n {
			if !yield(i) {
				return
			}
			i += 1
		}
	})
}

func report(name string, ok bool) needs Console {
	if ok {
		Console.Println(name)
	} else {
		Console.Println("FAIL " + name)
	}
}

func main() needs Console {
	let mut s = 0
	for v in upTo(4) {
		s = s * 10 + v
	}
	report("seq", s == 123)

	let mut yields = 0
	let counted = Seq[string](func(yield func(v string) bool) {
		for w in []string{"a", "b", "c", "d"} {
			yields += 1
			if !yield(w) {
				return
			}
		}
	})
	let mut seen = ""
	for w in counted {
		if w == "c" {
			break
		}
		seen = seen + w
	}
	report("seq break", seen == "ab" && yields == 3)

	let mut n = 0
	for _ in upTo(3) {
		n += 1
	}
	report("seq blank", n == 3)

	let mut direct = 0
	upTo(5)(func(v int) bool {
		direct += v
		v < 2
	})
	report("seq call", direct == 3)
}
`, "seq\nseq break\nseq blank\nseq call\n")
}

// TestErgoRunMapWriteAndLen: a Map write inserts a new entry and replaces
// an existing one; len counts a slice, a Map and a string's bytes.
func TestErgoRunMapWriteAndLen(t *testing.T) {
	wantRun(t, `func main() needs Console {
	let mut m = Map[string, int]{"a": 1}
	m["b"] = 2
	m["a"] = 10
	let mut s = 0
	for k, v in m {
		s += len(k) * v
	}
	if s == 12 && len(m) == 2 {
		Console.Println("map write")
	}
	if len([]int{1, 2, 3}) == 3 && len("héllo") == 6 && len(Map[int, int]{}) == 0 {
		Console.Println("len")
	}
}
`, "map write\nlen\n")
}

// TestQueueValuesRefusedNamingD3: queue.Values() returns a Seq[T] and a for
// over it checks clean, but a call of a std operation, with the caller's
// task as its hidden argument, is lowered by stage D3 (the Info requests
// from lower-v1 3b-runtime): a located refusal at the call names D3, never
// an internal compiler error.
func TestQueueValuesRefusedNamingD3(t *testing.T) {
	src := "import queue\n\nfunc f(q queue.Queue[int]) int {\n\tlet mut s = 0\n\tfor v in q.Values() {\n\t\ts += v\n\t}\n\ts\n}\n"
	o := lowerSource("r", src, false)
	if len(o.diags) != 0 {
		t.Fatalf("want a clean check; got %v", o.diags)
	}
	if o.panicked != nil || len(o.refused) != 1 {
		t.Fatalf("want one refusal; got refusals %v, panic %v, err %v\n%s", o.refused, o.panicked, o.err, o.mainGo())
	}
	r := o.refused[0]
	if r.Line != 5 || r.Column != 11 || !strings.Contains(r.Message, "check-v1 stage D3") {
		t.Fatalf("want a refusal at 5:11 naming check-v1 stage D3; got %+v", r)
	}
}

// TestRangeLowersWithoutRefusal:no for-in, `a..b` or Map write is refused
// by the lowering any more (the stage D refusals are gone).
func TestRangeLowersWithoutRefusal(t *testing.T) {
	o := lowerSource("r", "func f(xs []int, m Map[int, int]) int {\n\tlet mut s = 0\n\tlet mut w = m\n\tfor i, x in xs {\n\t\tw[i] = x\n\t}\n\tfor v in w {\n\t\ts += v\n\t}\n\tlet r = 0..len(xs)\n\tfor i in r {\n\t\ts += i\n\t}\n\ts\n}\n", false)
	if len(o.diags) != 0 || len(o.refused) != 0 || o.panicked != nil {
		t.Fatalf("want a clean lowering; got diags %v, refusals %v, panic %v", o.diags, o.refused, o.panicked)
	}
	if !strings.Contains(o.mainGo(), "range") {
		t.Fatalf("want a Go range loop in:\n%s", o.mainGo())
	}
}
