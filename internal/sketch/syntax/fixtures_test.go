package syntax

import "strings"

// The tables below mirror the acceptance tables of unit parser-195
// (grammar fixtures and malformed inputs), so the parser and printer tests
// run them at the public seam without the CLI.

// grammarFixtures are designed forms of docs/spec/grammar.md, the Go
// fallback and the decided extensions (spec D8) that the corpus and the
// spec fences do not all cover. Each must parse, format idempotently and
// canonically, and keep its comments and tokens.
var grammarFixtures = []struct{ name, src string }{
	{"constraints-and-instantiation", `package p

import "cmp"

// Max keeps a named constraint in the bracket list.
func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func On[F error](n int64) int64 { return n }

type Set[K comparable] struct {
	items map[K]bool
}

func use() int64 {
	return Max[int64](1, 2) + On[Failed | timeout.Expired](3)
}
`},
	{"function-type-uses-row", `package p

func run(f func() uses (Clock, Console), g func(), h func(int64) (int64, error[E]) uses R) error[E] uses Clock, Console, R {
	f()
	g()
	try h(1)
}
`},
	{"wire-forms", `package p

wire Test {
	Live
	replace Orders = MemoryOrders{}
	Gateway = OpenGateway
}

wire App uses Account, Invoice {
	Store = MemoryStore{}
}

func main() uses Console {
	with(wire{ http.Client = FakeClient{} }) { serve() }
	with(Test, trace.Span("t")) {
		serve()
	}
}
`},
	{"control-flow", `package p

func loop(xs []int64, ch chan int64, done <-chan struct{}) int64 {
	var total int64
	a, b := 1, 2
	a, b = b, a
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	for _, x := range xs {
		if x > 10 {
			break
		} else if x < 0 {
			continue
		} else {
			total++
		}
	}
	for range 3 {
		total--
	}
	for total < 100 {
		total *= 2
	}
outer:
	for {
		select {
		case v := <-ch:
			total = total + v
		case <-done:
			break outer
		default:
			continue outer
		}
	}
	switch {
	case total == 0 || a == b:
		return 0
	}
	switch total {
	case 0, 1:
		return 0
	default:
		return total % 7
	}
}
`},
	{"catch-binding-and-try-operands", `package p

func handle(req Request) (Response, error[Fatal]) {
	r := created(req) catch e {
	case Invalid:
		log(e)
		Status(422)
	case Missing:
		return Status(404)
	}
	if !try ok(req) {
		return r
	}
	return label(r) + " " + try render(r)
}
`},
	{"tasks-defer-channels", `package p

func work(done chan<- int64, in <-chan int64) {
	t := go {
		compute()
	}
	defer t.Stop()
	defer done <- 1
	for v := range in {
		go process(v)
	}
	running := map[int64]Task[struct{}]{}
	running[0] = go read()
}
`},
	{"type-switches", `package p

func step(s State, e Event) Decision[State, string] {
	switch s, e := s.(type), e.(type) {
	case (Idle, Start):
		return Enter{Running{}}
	case (Running, Stop), (Paused, Stop):
		return Done{"stopped"}
	case (_, _):
		return Ignore{}
	}
}

func kind(v Value) string {
	switch v.(type) {
	case A, B:
		return "ab"
	default:
		return "other"
	}
}

func bind(v Value) string {
	switch x := v.(type) {
	case A:
		return x.Name
	case _:
		return ""
	}
}
`},
	{"declarations", `package p

import (
	"strings"
	. "html"
	go "net/url"
)

var limit = 3

var (
	first  = "a"
	second = "b"
)

type Tally struct {
	n    int64 // the count
	name string
}

type Counter machine[State, Event] {
	initial Idle{}
	step    step
}

type Shape interface{ Circle | Square }

type Store interface {
	Get(key string) (string, error[NotFound])
	Put(key string, v string) uses Clock
}

//ef:go net/url.Parse
func parseURL(raw string) (url.URL, error[go.Error]) uses go.Foreign

func (t *Tally) Bump(xs ...int64) (int64, bool) {
	r := 'x' /* a rune */
	return int64(len(xs)), r == 'x' && true
}

func (Tally) Name() string { return strings.ToUpper("t") }
`},
	{"literals-and-calls", `package p

var routes = http.Routes{
	"POST /orders": place,
	"GET /health":  health,
}

func build() {
	m := map[string][]int64{"a": {1, 2}, "b": nil}
	s := []Pair{{1, 2}, {First: 3, Second: 4}}
	t := s[1:]
	c := json.Codec[Order]{MaxBytes: 4096, MaxDepth: 3}
	n := try retry.On[Failed](retry.Exponential(100ms).Times(3)) { fetch() } catch {
	case Failed:
		0
	}
	slices.SortFunc(t, func(a, b) int { return cmp.Compare(a.First, b.First) })
	v := sync.Atomically() {
		move(m, n)
	}
	w := -n * (2 + 3) / 4
	use(m, t, c, n, v, w, ` + "`raw\n// not a comment`" + `)
}
`},
}

type malformed struct {
	name     string
	src      string
	generate func() string
	lines    []int // acceptable lines of the first diagnostic
}

// malformedTable: each input must give a located, registered syntax
// diagnostic (S0xx in docs/spec/diagnostics.md) on one of the listed lines;
// ef fmt must exit 2, name the code and leave the file unchanged.
var malformedTable = []malformed{
	{name: "enum-type", src: "package p\n\ntype Payment enum {\n\tPending\n}\n", lines: []int{3}},
	{name: "const-removed", src: "package p\n\nconst limit = 3\n", lines: []int{3}},
	{name: "goto-removed", src: "package p\n\nfunc f() {\nagain:\n\tgoto again\n}\n", lines: []int{5}},
	{name: "fallthrough-removed", src: "package p\n\nfunc f(x int64) {\n\tswitch x {\n\tcase 1:\n\t\tfallthrough\n\tdefault:\n\t}\n}\n", lines: []int{6}},
	{name: "unterminated-string", src: "package p\n\nfunc f() {\n\tx := \"abc\n}\n", lines: []int{4}},
	{name: "unterminated-raw-string", src: "package p\n\nvar s = `abc\n", lines: []int{3}},
	{name: "unterminated-block-comment", src: "package p\n\n/* open\nfunc f() {}\n", lines: []int{3}},
	{name: "nonletter-identifier", src: "package p\n\nfunc f() {\n\tx⪪ := 1\n}\n", lines: []int{4}},
	{name: "invalid-utf8", src: "package p\n\nfunc f() {\n\t\xff := 1\n}\n", lines: []int{4}},
	{name: "unknown-number-suffix", src: "package p\n\nvar d = 5kg\n", lines: []int{3}},
	{name: "mixed-keyed-positional", src: "package p\n\nvar o = Order{ID: \"1\", 2}\n", lines: []int{3}},
	{name: "missing-package", src: "func f() {}\n", lines: []int{1}},
	{name: "import-after-declaration", src: "package p\n\nfunc f() {}\n\nimport \"strings\"\n", lines: []int{5}},
	{name: "unclosed-brace", src: "package p\n\nfunc f() {\n\tg()\n", lines: []int{4, 5}},
	{name: "stray-paren", src: "package p\n\nfunc f() {\n\tg())\n}\n", lines: []int{4}},
	{name: "union-trailing-bar", src: "package p\n\ntype U interface{ A | }\n", lines: []int{3}},
	{name: "multi-subject-arity", src: "package p\n\nfunc f(a A, b B) {\n\tswitch x, y := a.(type) {\n\tcase (A, B):\n\t}\n}\n", lines: []int{4}},
	{name: "tuple-case-in-single-switch", src: "package p\n\nfunc f(a A) {\n\tswitch x := a.(type) {\n\tcase (A, B):\n\t\tuse(x)\n\t}\n}\n", lines: []int{5}},
	{name: "machine-unknown-entry", src: "package p\n\ntype M machine[S, E] {\n\tinitial Idle{}\n\tstart step\n}\n", lines: []int{5}},
	{name: "go-without-call", src: "package p\n\nfunc f() {\n\tgo x\n}\n", lines: []int{4}},
	{name: "type-guard-outside-switch", src: "package p\n\nfunc f(v V) {\n\tt := v.(type)\n\tuse(t)\n}\n", lines: []int{4}},
	{name: "bare-try", src: "package p\n\nfunc f() {\n\tx := try\n}\n", lines: []int{4, 5}},
	{name: "catch-binding-not-a-name", src: "package p\n\nfunc f() {\n\tx := g() catch 1 {\n\t}\n}\n", lines: []int{4}},
	{name: "end-of-statement", src: "package p\n\nfunc f() {\n\tx := 1 2\n}\n", lines: []int{4}},
	{name: "two-receivers", src: "package p\n\nfunc (a A, b B) M() {}\n", lines: []int{3}},
	{name: "unexpected-character", src: "package p\n\nfunc f() {\n\tx := 1 # 2\n}\n", lines: []int{4}},
	{name: "defer-non-simple", src: "package p\n\nfunc f() {\n\tdefer if true {}\n}\n", lines: []int{4}},
	{name: "empty-type-parameters", src: "package p\n\nfunc f[]() {}\n", lines: []int{3}},
	{name: "parenless-trailing-block", src: "package p\n\nfunc f() {\n\tsync.Atomically {\n\t\tmove()\n\t}\n}\n", lines: []int{5}},
	{name: "deep-nesting", generate: func() string {
		return "package p\n\nvar x = " + strings.Repeat("(", 200000) + "1" + strings.Repeat(")", 200000) + "\n"
	}, lines: []int{3}},
}
