package sketchtest

import "strings"

// The tables below mirror the acceptance tables of unit parser-195
// (grammar fixtures and malformed inputs), so the parser and printer tests
// run them at the public seam without the CLI.

// GrammarFixtures are designed forms of docs/spec/grammar.md, the Go
// fallback and the decided extensions (spec D8) that the corpus and the
// spec fences do not all cover. Each must parse, format idempotently and
// canonically, and keep its comments and tokens.
var GrammarFixtures = []struct{ Name, Src string }{
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
	{"function-type-needs-row", `package p

func run(f func() needs (Clock, Console), g func(), h func(int64) (int64, error[E]) needs R) error[E] needs Clock, Console, R {
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

wire App provides Account, Invoice {
	Store = MemoryStore{}
}

wire Live provides Orders needs Config, Clock {
	Orders = OpenOrders
}

func main() needs Console {
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
	Put(key string, v string) needs Clock
}

//ef:go net/url.Parse
func parseURL(raw string) (url.URL, error[go.Error]) needs go.Foreign

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

type Malformed struct {
	Name     string
	Src      string
	Generate func() string
	Lines    []int // acceptable lines of the first diagnostic
}

// MalformedTable: each input must give a located, registered syntax
// diagnostic (S0xx in docs/spec/diagnostics.md) on one of the listed lines;
// ergo fmt must exit 2, name the code and leave the file unchanged.
var MalformedTable = []Malformed{
	{Name: "enum-type", Src: "package p\n\ntype Payment enum {\n\tPending\n}\n", Lines: []int{3}},
	{Name: "const-removed", Src: "package p\n\nconst limit = 3\n", Lines: []int{3}},
	{Name: "goto-removed", Src: "package p\n\nfunc f() {\nagain:\n\tgoto again\n}\n", Lines: []int{5}},
	{Name: "fallthrough-removed", Src: "package p\n\nfunc f(x int64) {\n\tswitch x {\n\tcase 1:\n\t\tfallthrough\n\tdefault:\n\t}\n}\n", Lines: []int{6}},
	{Name: "unterminated-string", Src: "package p\n\nfunc f() {\n\tx := \"abc\n}\n", Lines: []int{4}},
	{Name: "unterminated-raw-string", Src: "package p\n\nvar s = `abc\n", Lines: []int{3}},
	{Name: "unterminated-block-comment", Src: "package p\n\n/* open\nfunc f() {}\n", Lines: []int{3}},
	{Name: "nonletter-identifier", Src: "package p\n\nfunc f() {\n\tx⪪ := 1\n}\n", Lines: []int{4}},
	{Name: "invalid-utf8", Src: "package p\n\nfunc f() {\n\t\xff := 1\n}\n", Lines: []int{4}},
	{Name: "unknown-number-suffix", Src: "package p\n\nvar d = 5kg\n", Lines: []int{3}},
	{Name: "mixed-keyed-positional", Src: "package p\n\nvar o = Order{ID: \"1\", 2}\n", Lines: []int{3}},
	{Name: "missing-package", Src: "func f() {}\n", Lines: []int{1}},
	{Name: "import-after-declaration", Src: "package p\n\nfunc f() {}\n\nimport \"strings\"\n", Lines: []int{5}},
	{Name: "unclosed-brace", Src: "package p\n\nfunc f() {\n\tg()\n", Lines: []int{4, 5}},
	{Name: "stray-paren", Src: "package p\n\nfunc f() {\n\tg())\n}\n", Lines: []int{4}},
	{Name: "union-trailing-bar", Src: "package p\n\ntype U interface{ A | }\n", Lines: []int{3}},
	{Name: "multi-subject-arity", Src: "package p\n\nfunc f(a A, b B) {\n\tswitch x, y := a.(type) {\n\tcase (A, B):\n\t}\n}\n", Lines: []int{4}},
	{Name: "tuple-case-in-single-switch", Src: "package p\n\nfunc f(a A) {\n\tswitch x := a.(type) {\n\tcase (A, B):\n\t\tuse(x)\n\t}\n}\n", Lines: []int{5}},
	{Name: "machine-unknown-entry", Src: "package p\n\ntype M machine[S, E] {\n\tinitial Idle{}\n\tstart step\n}\n", Lines: []int{5}},
	{Name: "go-without-call", Src: "package p\n\nfunc f() {\n\tgo x\n}\n", Lines: []int{4}},
	{Name: "type-guard-outside-switch", Src: "package p\n\nfunc f(v V) {\n\tt := v.(type)\n\tuse(t)\n}\n", Lines: []int{4}},
	{Name: "bare-try", Src: "package p\n\nfunc f() {\n\tx := try\n}\n", Lines: []int{4, 5}},
	{Name: "catch-binding-not-a-name", Src: "package p\n\nfunc f() {\n\tx := g() catch 1 {\n\t}\n}\n", Lines: []int{4}},
	{Name: "end-of-statement", Src: "package p\n\nfunc f() {\n\tx := 1 2\n}\n", Lines: []int{4}},
	{Name: "two-receivers", Src: "package p\n\nfunc (a A, b B) M() {}\n", Lines: []int{3}},
	{Name: "unexpected-character", Src: "package p\n\nfunc f() {\n\tx := 1 # 2\n}\n", Lines: []int{4}},
	{Name: "defer-non-simple", Src: "package p\n\nfunc f() {\n\tdefer if true {}\n}\n", Lines: []int{4}},
	{Name: "empty-type-parameters", Src: "package p\n\nfunc f[]() {}\n", Lines: []int{3}},
	{Name: "parenless-trailing-block", Src: "package p\n\nfunc f() {\n\tsync.Atomically {\n\t\tmove()\n\t}\n}\n", Lines: []int{5}},
	// #217: the removed keyword uses, in every position a needs clause or a
	// provides clause can stand, and as a name.
	{Name: "uses-hint-declaration", Src: "package p\n\nfunc main() uses Console {\n}\n", Lines: []int{3}},
	{Name: "uses-hint-after-result", Src: "package p\n\nfunc f() (int64, error[E]) uses A, B {\n}\n", Lines: []int{3}},
	{Name: "uses-hint-function-type", Src: "package p\n\nfunc f(g func() int64 uses (A, B)) {\n}\n", Lines: []int{3}},
	{Name: "uses-hint-interface-method", Src: "package p\n\ntype Store interface {\n\tPut(key string) uses Clock\n}\n", Lines: []int{4}},
	{Name: "uses-hint-wire", Src: "package p\n\nwire App uses Account {\n\tStore = M{}\n}\n", Lines: []int{3}},
	{Name: "uses-hint-wire-after-provides", Src: "package p\n\nwire App provides Account uses Config {\n\tStore = M{}\n}\n", Lines: []int{3}},
	{Name: "uses-hint-name", Src: "package p\n\nvar uses = 1\n", Lines: []int{3}},
	{Name: "wire-needs-before-provides", Src: "package p\n\nwire App needs Config provides Account {\n\tStore = M{}\n}\n", Lines: []int{3}},
	{Name: "deep-nesting", Generate: func() string {
		return "package p\n\nvar x = " + strings.Repeat("(", 200000) + "1" + strings.Repeat(")", 200000) + "\n"
	}, Lines: []int{3}},
}
