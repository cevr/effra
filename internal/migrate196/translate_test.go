package migrate196

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"ergo.local/prototype/internal/sketch/proto"
	"ergo.local/prototype/internal/sketch/syntax"
)

// handFinished are the frozen inputs whose translation has notes: forms
// with no decided spelling of the same meaning (channels, make, an
// anonymous struct), a range whose kind the syntax does not prove, a defer
// whose call is not stable or not at a function's top level, a law
// function. Their owned copies are finished by hand (round requests,
// parse-v1).
var handFinished = map[string]bool{
	"v0/sketch-eval-8/loops/poller.go.sketch":       true,
	"v0/sketch-eval-9/loops/poller.go.sketch":       true,
	"v0/sketch-eval-8/loops/tasks-map.go.sketch":    true,
	"v0/sketch-eval-9/loops/tasks-map.go.sketch":    true,
	"v0/sketch-eval-8/loops/wire-effects.go.sketch": true,
	"v0/sketch-eval-9/loops/wire-effects.go.sketch": true,
	"v0/sketch-eval-8/loops/wordcount.go.sketch":    true,
	"v0/sketch-eval-9/loops/wordcount.go.sketch":    true,
	"v0/sketch-eval-8/loops/ledger.go.sketch":       true,
	"v0/sketch-eval-9/loops/ledger.go.sketch":       true,
	"v0/sketch-eval-9/loops/fetchall.go.sketch":     true,
	"v0/sketch-eval-9/actor.go.sketch":              true,
	"v0/sketch-eval-9/observe.go.sketch":            true,
}

type frozen struct {
	rel, base string
}

// frozenInputs reads testdata/old-verdicts.tsv: each frozen old input and
// its verdict at 92fac236 (the codes of `ergo check`, sorted, or OK).
func frozenInputs(t *testing.T) []frozen {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "old-verdicts.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []frozen
	s := bufio.NewScanner(f)
	for s.Scan() {
		rel, base, _ := strings.Cut(s.Text(), "\t")
		out = append(out, frozen{rel, base})
	}
	if len(out) < 140 {
		t.Fatalf("only %d frozen inputs", len(out))
	}
	return out
}

// verdict is the prototype's verdict on a decided source, the path `ergo
// check` took at 92fac236: the sorted codes, or OK.
func verdict(t *testing.T, src string) string {
	t.Helper()
	file, err := syntax.Parse(src)
	if err != nil {
		return err.(*syntax.Error).Code
	}
	converted, diags := proto.FromSyntax(file)
	if converted != nil {
		_, diags = proto.Check(converted)
	}
	var codes []string
	for _, d := range diags {
		codes = append(codes, d.Code)
	}
	sort.Strings(codes)
	if len(codes) == 0 {
		return "OK"
	}
	return strings.Join(codes, " ")
}

// TestTranslateMeaningKept (#196, D7): every frozen old input that parsed at
// base translates with no notes, and where its base verdict has no S109 the
// translation's prototype verdict has the same codes.
func TestTranslateMeaningKept(t *testing.T) {
	checked := 0
	for _, in := range frozenInputs(t) {
		if strings.HasPrefix(in.base, "S001") {
			continue
		}
		src, err := os.ReadFile(filepath.Join("testdata", "old", in.rel))
		if err != nil {
			t.Fatal(err)
		}
		out, notes, err := Translate(string(src))
		if err != nil {
			t.Errorf("%s: %v", in.rel, err)
			continue
		}
		if handFinished[in.rel] {
			if len(notes) == 0 {
				t.Errorf("%s: translates without notes now; drop it from handFinished", in.rel)
			}
			continue
		}
		if len(notes) > 0 {
			t.Errorf("%s: notes %v", in.rel, notes)
			continue
		}
		if strings.Contains(in.base, "S109") {
			continue
		}
		checked++
		if got := verdict(t, out); got != in.base {
			t.Errorf("%s: verdict changed: base [%s], translated [%s]\n%s", in.rel, in.base, got, out)
		}
	}
	if checked < 85 {
		t.Errorf("only %d inputs had their verdict compared", checked)
	}
}

// TestRules pins each rule of D5 on a small input.
func TestRules(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"package and imports", "package p\n\nimport (\n\t\"strings\"\n\tjson \"example.com/json\"\n\tgo \"net/http\"\n\tx \"a/b\"\n)\n",
			"import strings\nimport example.com/json\nimport go net/http\nimport x a/b\n"},
		{"define and mut", "package p\n\nfunc f() int64 {\n\tx := 1\n\ty := 2\n\ty = y + x\n\treturn y\n}\n",
			"func f() int64 {\n\tlet x = 1\n\tlet mut y = 2\n\ty = y + x\n\treturn y\n}\n"},
		{"inc", "package p\n\nfunc f() {\n\tn := 0\n\tn++\n\tuse(n)\n}\n",
			"func f() {\n\tlet mut n = 0\n\tn += 1\n\tuse(n)\n}\n"},
		{"three-clause for", "package p\n\nfunc f(n int64) {\n\tfor i := 0; i < n; i++ {\n\t\tuse(i)\n\t}\n}\n",
			"func f(n int64) {\n\tfor i in 0..n {\n\t\tuse(i)\n\t}\n}\n"},
		{"range forms", "package p\n\nfunc f(xs []int64, n int64) {\n\tfor i, x := range xs {\n\t\tuse(i, x)\n\t}\n\tfor _, x := range xs {\n\t\tuse(x)\n\t}\n\tfor i := range xs {\n\t\tuse(i)\n\t}\n\tfor i := range n {\n\t\tuse(i)\n\t}\n\tfor range xs {\n\t}\n}\n",
			"func f(xs []int64, n int64) {\n\tfor i, x in xs {\n\t\tuse(i, x)\n\t}\n\tfor x in xs {\n\t\tuse(x)\n\t}\n\tfor i, _ in xs {\n\t\tuse(i)\n\t}\n\tfor i in 0..n {\n\t\tuse(i)\n\t}\n\tfor _ in xs {\n\t}\n}\n"},
		{"struct interface union", "package p\n\ntype T struct {\n\tA int64\n\tB string\n}\n\ntype S interface {\n\tGet() T\n}\n\ntype U interface{ A | B }\n",
			"type T {\n\tA int64,\n\tB string,\n}\n\ntype S {\n\tGet() T,\n}\n\ntype U = A | B\n"},
		{"error rows", "package p\n\nfunc f() (Order, error[Gone | Lost]) {\n\treturn Order{}\n}\n\nfunc g() error[Gone] {\n\treturn Gone{}\n}\n",
			"func f() Order fails Gone | Lost {\n\treturn Order{}\n}\n\nfunc g() fails Gone {\n\treturn Gone{}\n}\n"},
		{"receivers", "package p\n\nfunc (o Order) Fee(rate int64) int64 {\n\treturn o.Total * rate\n}\n\nfunc (MemoryOrders) Name() string {\n\treturn \"m\"\n}\n",
			"func Fee(o Order, rate int64) int64 {\n\treturn o.Total * rate\n}\n\nfunc Name(_ MemoryOrders) string {\n\treturn \"m\"\n}\n"},
		{"implicit type parameters", "package p\n\nfunc Map(xs []A, f func(A) B) []B {\n\treturn nil\n}\n\nfunc Max[T cmp.Ordered](a, b T) T {\n\treturn a\n}\n",
			"func Map[A, B](xs []A, f func(A) B) []B {\n\treturn nil\n}\n\nfunc Max[T ordered](a, b T) T {\n\treturn a\n}\n"},
		{"map append delete", "package p\n\nfunc f(m map[string]int64, xs []int64) []int64 {\n\tdelete(m, \"a\")\n\treturn append(xs, 1, 2)\n}\n",
			"func f(m Map[string, int64], xs []int64) []int64 {\n\tm.Delete(\"a\")\n\treturn xs.Append(1).Append(2)\n}\n"},
		{"wire and with", "package p\n\nwire Live {\n\tClock = Fixed{}\n\tOrders = Memory{}\n}\n\nfunc main() {\n\twith(Live) { run() }\n}\n",
			"let Live = wire {\n\tClock:  Fixed{},\n\tOrders: Memory{},\n}\n\nfunc main() {\n\twith Live { run() }\n}\n"},
		{"with a function", "package p\n\nfunc main() string {\n\treturn with(Fixture, labels)\n}\n",
			"func main() string {\n\treturn with Fixture { labels() }\n}\n"},
		{"extern", "package p\n\n//ef:go strings.ToUpper\nfunc Upper(s string) string\n",
			"extern go strings.ToUpper func Upper(s string) string\n"},
		{"literal switch", "package p\n\nfunc f(x int64) string {\n\tswitch x {\n\tcase 1, 2:\n\t\treturn \"a\"\n\tdefault:\n\t\treturn \"b\"\n\t}\n}\n",
			"func f(x int64) string {\n\tmatch x {\n\tcase 1, 2:\n\t\treturn \"a\"\n\tcase _:\n\t\treturn \"b\"\n\t}\n}\n"},
		{"tagless switch", "package p\n\nfunc f(x int64) string {\n\tswitch {\n\tcase x > 1:\n\t\treturn \"a\"\n\tdefault:\n\t\treturn \"b\"\n\t}\n}\n",
			"func f(x int64) string {\n\tif x > 1 {\n\t\treturn \"a\"\n\t} else {\n\t\treturn \"b\"\n\t}\n}\n"},
		{"type switch", "package p\n\nfunc f(v Value) string {\n\tswitch p := v.(type) {\n\tcase A:\n\t\treturn p.Name\n\tcase B, C:\n\t\treturn show(p)\n\tdefault:\n\t\treturn \"\"\n\t}\n}\n",
			"func f(v Value) string {\n\tmatch v {\n\tcase A as p:\n\t\treturn p.Name\n\tcase B, C:\n\t\treturn show(v)\n\tcase _:\n\t\treturn \"\"\n\t}\n}\n"},
		{"catch binding", "package p\n\nfunc f() string {\n\treturn g() catch e {\n\tcase NotFound:\n\t\te.ID\n\tcase Other:\n\t\t\"x\"\n\t}\n}\n",
			"func f() string {\n\treturn g() catch {\n\tcase NotFound as e:\n\t\te.ID\n\tcase Other:\n\t\t\"x\"\n\t}\n}\n"},
		{"try in operand", "package p\n\nfunc f() (int64, error[Gone]) {\n\tx := 1 + try g()\n\tok := !try h()\n\tuse(ok)\n\treturn x\n}\n",
			"func f() int64 fails Gone {\n\tlet x = try 1 + g()\n\tlet ok = try !h()\n\tuse(ok)\n\treturn x\n}\n"},
		{"multi return", "package p\n\nfunc f() (int64, int64) {\n\treturn 1, 2\n}\n",
			"func f() (int64, int64) {\n\treturn (1, 2)\n}\n"},
		// task and scope are not in the prelude: the rewrite imports them
		// (round-1 decision 8), after the file's own imports.
		{"go and defer", "package p\n\nfunc f() {\n\tt := go work()\n\tdefer done()\n\tuse(t)\n}\n",
			"import scope\nimport task\n\nfunc f() {\n\tlet t = task.Go() { work() }\n\tscope.OnExit() { done() }\n\tuse(t)\n}\n"},
		{"go with stable arguments", "package p\n\nimport \"strings\"\n\nfunc f(n int64) {\n\tx := 1\n\tgo work(x, n, \"a\")\n\tuse(strings.ToUpper(\"a\"))\n}\n",
			"import strings\nimport task\n\nfunc f(n int64) {\n\tlet x = 1\n\ttask.Go() { work(x, n, \"a\") }\n\tuse(strings.ToUpper(\"a\"))\n}\n"},
		// M3: each operand of T.Equal keeps its own tree.
		{"equal precedence", "package p\n\nfunc TestEq() needs testing.T {\n\tT.Equal(false, false || true)\n\tT.Equal(a == b, c)\n\tT.Equal(x && y, z)\n\tT.Equal(1+2, 3)\n\tT.Equal(-a, b*c)\n}\n",
			"test Eq {\n\tassert false == (false || true)\n\tassert (a == b) == c\n\tassert (x && y) == z\n\tassert 1+2 == 3\n\tassert -a == b*c\n}\n"},
		// M4: a composite literal, or a local whose declaration writes the
		// type, proves the kind.
		{"range kind proved", "package p\n\nfunc f() {\n\tfor i := range []int64{10, 20} {\n\t\tuse(i)\n\t}\n\txs := []int64{10, 20}\n\tfor i := range xs {\n\t\tuse(i)\n\t}\n\tvar n int64\n\tfor i := range n {\n\t\tuse(i)\n\t}\n}\n",
			"func f() {\n\tfor i, _ in []int64{10, 20} {\n\t\tuse(i)\n\t}\n\tlet xs = []int64{10, 20}\n\tfor i, _ in xs {\n\t\tuse(i)\n\t}\n\tlet n int64\n\tfor i in 0..n {\n\t\tuse(i)\n\t}\n}\n"},
		// M5: a bound the body writes, a bound with a call, or another step
		// keeps Go's condition loop.
		{"for with a written bound", "package p\n\nfunc f() {\n\tn := 3\n\tfor i := 0; i < n; i++ {\n\t\tn = 0\n\t\tuse(i)\n\t}\n}\n",
			"func f() {\n\tlet mut n = 3\n\tlet mut i = 0\n\tfor i < n {\n\t\tn = 0\n\t\tuse(i)\n\t\ti += 1\n\t}\n}\n"},
		{"for with a call in the bound", "package p\n\nfunc f() {\n\tfor i := 0; i < count(); i++ {\n\t\tuse(i)\n\t}\n}\n",
			"func f() {\n\tlet mut i = 0\n\tfor i < count() {\n\t\tuse(i)\n\t\ti += 1\n\t}\n}\n"},
		{"for with another step", "package p\n\nfunc f() {\n\tfor i := 0; i < 9; i += 2 {\n\t\tuse(i)\n\t}\n}\n",
			"func f() {\n\tlet mut i = 0\n\tfor i < 9 {\n\t\tuse(i)\n\t\ti += 2\n\t}\n}\n"},
		{"for with a stable local bound", "package p\n\nfunc f(xs []int64) {\n\tn := 3\n\tfor i := 1; i < n; i++ {\n\t\tuse(i)\n\t}\n}\n",
			"func f(xs []int64) {\n\tlet n = 3\n\tfor i in 1..n {\n\t\tuse(i)\n\t}\n}\n"},
		// M6: the tag is read once, and each case stays one operand.
		{"switch case precedence", "package p\n\nfunc f(x bool, a bool, b bool) int64 {\n\tswitch x {\n\tcase a || b:\n\t\treturn 1\n\tdefault:\n\t\treturn 2\n\t}\n}\n",
			"func f(x bool, a bool, b bool) int64 {\n\tif x == (a || b) {\n\t\treturn 1\n\t} else {\n\t\treturn 2\n\t}\n}\n"},
		{"switch tag snapshot", "package p\n\nfunc f(x int64) int64 {\n\tswitch x {\n\tcase next():\n\t\treturn 1\n\tcase other():\n\t\treturn 2\n\t}\n\treturn 3\n}\n",
			"func f(x int64) int64 {\n\tlet xSubject = x\n\tif xSubject == next() {\n\t\treturn 1\n\t} else if xSubject == other() {\n\t\treturn 2\n\t}\n\treturn 3\n}\n"},
		{"tagless switch with several cases", "package p\n\nfunc f(a bool, b bool, c bool) int64 {\n\tswitch {\n\tcase a && b, c:\n\t\treturn 1\n\t}\n\treturn 2\n}\n",
			"func f(a bool, b bool, c bool) int64 {\n\tif a && b || c {\n\t\treturn 1\n\t}\n\treturn 2\n}\n"},
		// M7: a record key of a declared struct is not a read.
		{"multi-type arm with a record key", "package p\n\ntype Out struct {\n\tp int64\n}\n\nfunc f(v Value) Out {\n\tswitch p := v.(type) {\n\tcase A, B:\n\t\treturn Out{p: size(p)}\n\t}\n\treturn Out{}\n}\n",
			"type Out {\n\tp int64,\n}\n\nfunc f(v Value) Out {\n\tmatch v {\n\tcase A, B:\n\t\treturn Out{p: size(v)}\n\t}\n\treturn Out{}\n}\n"},
		// M11: an embed never stands before an entry.
		{"wire embed after entries", "package p\n\nwire W {\n\tBase\n\tClock = Fixed{}\n}\n\nwire V { Base; Clock = Fixed{} }\n",
			"let W = wire {\n\tClock: Fixed{},\n\tBase,\n}\n\nlet V = wire { Clock: Fixed{}, Base }\n"},
		{"test", "package p\n\nfunc TestAdd() needs testing.T {\n\tT.Equal(1+1, 2)\n\tT.True(ok())\n}\n",
			"test Add {\n\tassert 1+1 == 2\n\tassert ok()\n}\n"},
		{"keyword names", "package p\n\nfunc f(in int64) int64 {\n\tmatch := in\n\treturn match\n}\n",
			"func f(in2 int64) int64 {\n\tlet match2 = in2\n\treturn match2\n}\n"},
		{"inner shadow", "package p\n\nfunc f() string {\n\tx := \"a\"\n\tif ok() {\n\t\tx := x + \"b\"\n\t\tuse(x)\n\t}\n\treturn x\n}\n",
			"func f() string {\n\tlet x = \"a\"\n\tif ok() {\n\t\tlet x2 = x + \"b\"\n\t\tuse(x2)\n\t}\n\treturn x\n}\n"},
		{"if init", "package p\n\nfunc f() {\n\tif v := g(); v > 0 {\n\t\tuse(v)\n\t}\n}\n",
			"func f() {\n\tlet v = g()\n\tif v > 0 {\n\t\tuse(v)\n\t}\n}\n"},
		{"var", "package p\n\nvar limit = 3\n\nfunc f() int64 {\n\tvar n int64\n\tn = limit\n\treturn n\n}\n",
			"let limit = 3\n\nfunc f() int64 {\n\tlet mut n int64\n\tn = limit\n\treturn n\n}\n"},
	} {
		got, notes, err := Translate(c.in)
		if err != nil || len(notes) > 0 {
			t.Errorf("%s: err %v, notes %v\n%s", c.name, err, notes, got)
			continue
		}
		if got != c.want {
			t.Errorf("%s:\n--- got\n%s\n--- want\n%s", c.name, got, c.want)
		}
	}
}

// TestNotes pins the forms the translator leaves for a hand edit.
func TestNotes(t *testing.T) {
	for _, c := range []struct{ name, in, note string }{
		{"break in switch", "package p\n\nfunc f(xs []int64) {\n\tfor _, x := range xs {\n\t\tswitch x {\n\t\tcase 1:\n\t\t\tbreak\n\t\t}\n\t}\n}\n", "break inside a switch arm"},
		{"same-block redeclare", "package p\n\nfunc f() (int64, error[E]) {\n\ta, err := g()\n\tb, err := g()\n\treturn a + b\n}\n", "redeclares err"},
		{"channel", "package p\n\nfunc f(c chan int64) {}\n", "channel"},
		{"make", "package p\n\nfunc f() {\n\tuse(make([]int64, 3))\n}\n", "make has no decided form"},
		{"dot import", "package p\n\nimport . \"strings\"\n", "dot import"},
		{"three-clause for without an init", "package p\n\nfunc f(i int64) {\n\tfor ; i < 9; i++ {\n\t}\n}\n", "three-clause for"},
		// M5: Go runs the post statement on continue.
		{"three-clause for with continue", "package p\n\nfunc f() {\n\tfor i := 0; i < 9; i += 2 {\n\t\tif i == 4 {\n\t\t\tcontinue\n\t\t}\n\t\tuse(i)\n\t}\n}\n", "three-clause for whose bounds may change, or with continue"},
		// Go gives each iteration its own i; one let mut would be shared.
		{"three-clause for with a captured variable", "package p\n\nfunc f() {\n\tfor i := 0; i < count(); i++ {\n\t\trun(func() { use(i) })\n\t}\n}\n", "three-clause for whose bounds may change"},
		// M4: a call never proves what it returns.
		{"range over a call", "package p\n\nfunc f() {\n\tfor i := range values() {\n\t\tuse(i)\n\t}\n}\n", "a range over values(): its kind"},
		{"range over an untyped local", "package p\n\nfunc f() {\n\txs := values()\n\tfor i := range xs {\n\t\tuse(i)\n\t}\n}\n", "a range over xs: its kind"},
		{"range over a local out of scope", "package p\n\nfunc f(ok bool) {\n\tif ok {\n\t\txs := []int64{1}\n\t\tuse(xs)\n\t}\n\tfor i := range xs {\n\t\tuse(i)\n\t}\n}\n", "a range over xs: its kind"},
		// M7: a key of a literal whose type the file does not show, and an
		// arm that declares the subject's name, are not proved reads.
		{"multi-type arm with an unknown literal key", "package p\n\nfunc f(v Value) Out {\n\tswitch p := v.(type) {\n\tcase A, B:\n\t\treturn Out{p: size(p)}\n\t}\n\treturn Out{}\n}\n", "several types reads p"},
		{"multi-type arm that declares the subject", "package p\n\nfunc f(v Value) int64 {\n\tswitch p := v.(type) {\n\tcase A, B:\n\t\tv := 1\n\t\treturn size(p) + v\n\t}\n\treturn 0\n}\n", "several types reads p"},
		{"multi-type arm on a written subject", "package p\n\nfunc f(v Value) int64 {\n\tswitch p := v.(type) {\n\tcase A, B:\n\t\treturn size(p)\n\t}\n\tv = A{}\n\treturn 0\n}\n", "several types reads p"},
		// M13: a law moves into its operation type by hand.
		{"law", "package p\n\nfunc LawRound(q Quantity) bool {\n\treturn q == q\n}\n", "a law function LawRound"},
		// m1: located notes, never an internal error.
		{"else-if init", "package p\n\nfunc f(a int64) int64 {\n\tif a > 1 {\n\t\treturn 1\n\t} else if b := g(); b > 2 {\n\t\treturn b\n\t}\n\treturn 0\n}\n", "an else-if with an init statement"},
		{"default-only tagless switch", "package p\n\nfunc f() {\n\tswitch {\n\tdefault:\n\t\tuse(1)\n\t}\n}\n", "a switch with only a default arm"},
		// Round-1 decisions 1 and 2: go and defer need stable arguments,
		// and defer stands at a function's top level.
		{"go with a written argument", "package p\n\nfunc f() {\n\tx := 1\n\tgo work(x)\n\tx = 2\n\tuse(x)\n}\n", "go with arguments that may change"},
		{"go with a method call", "package p\n\nfunc f(s Server) {\n\tgo s.Serve()\n}\n", "go with arguments that may change"},
		{"defer in a loop", "package p\n\nfunc f(xs []int64) {\n\tfor _, x := range xs {\n\t\tdefer done(x)\n\t}\n}\n", "a defer that is not a call with stable arguments"},
		{"defer of a method", "package p\n\nfunc f(r Resource) {\n\tdefer r.Close()\n}\n", "a defer that is not a call with stable arguments"},
		{"defer with a computed argument", "package p\n\nfunc f(n int64) {\n\tdefer done(n + 1)\n}\n", "a defer that is not a call with stable arguments"},
		// M11: a commented embed before an entry is moved by hand.
		{"commented embed before an entry", "package p\n\nwire W {\n\tBase // the shared part\n\tClock = Fixed{}\n}\n", "an embed with a comment before an entry"},
		{"multi-type catch arm reads", "package p\n\nfunc f() string {\n\treturn g() catch e {\n\tcase A, B:\n\t\tshow(e)\n\t}\n}\n", "several types reads e"},
		{"comma ok", "package p\n\nfunc f(m map[string]int64) {\n\tv, ok := m[\"a\"]\n\tuse(v, ok)\n}\n", "comma-ok"},
		{"label", "package p\n\nfunc f() {\nL:\n\tfor {\n\t\tbreak L\n\t}\n}\n", "label"},
		{"variadic", "package p\n\nfunc f(xs ...int64) {}\n", "variadic"},
		{"method value", "package p\n\ntype T struct{}\n\nfunc (T) M() {}\n\nfunc f(t T) {\n\tuse(t.M)\n}\n", "method value"},
	} {
		_, notes, err := Translate(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		found := false
		for _, n := range notes {
			found = found || strings.Contains(n.Text, c.note)
		}
		if !found {
			t.Errorf("%s: no note containing %q in %v", c.name, c.note, notes)
		}
	}
}

func TestGoStringsAndMarkdown(t *testing.T) {
	goSrc := "package x\n\nvar src = `package main\n\nfunc main() needs Console {\n\tx := 1\n\tConsole.Println(x)\n}\n`\n\nvar other = \"not a program\"\n"
	out, notes, err := GoStrings(goSrc)
	if err != nil || len(notes) > 0 {
		t.Fatalf("GoStrings: %v %v", err, notes)
	}
	if !strings.Contains(out, "`func main() needs Console {\n\tlet x = 1\n") || !strings.Contains(out, `"not a program"`) {
		t.Fatalf("GoStrings:\n%s", out)
	}
	// A fragment without a package clause, joined to a prefix by the test
	// that holds it, keeps its edges; a string already in the decided
	// syntax is left alone.
	goSrc = "package x\n\nvar frag = `\nfunc f() error[Gone] {\n\tv := 1\n\treturn Gone{}\n}`\n\nvar done = `func g() fails Gone { return Gone{} }`\n"
	out, notes, err = GoStrings(goSrc)
	if err != nil || len(notes) > 0 {
		t.Fatalf("GoStrings fragment: %v %v", err, notes)
	}
	if !strings.Contains(out, "`\nfunc f() fails Gone {\n\tlet v = 1\n\treturn Gone{}\n}`") || !strings.Contains(out, "`func g() fails Gone { return Gone{} }`") {
		t.Fatalf("GoStrings fragment:\n%s", out)
	}
	md := "Text.\n\n```go\nx := 1\nuse(x)\n\nfunc f() (int64, error[Gone]) {\n\treturn 1\n}\n```\n\n```go\nnot ergo at all (\n```\n"
	got, notes := Markdown(md)
	want := "Text.\n\n```go\nlet x = 1\nuse(x)\n\nfunc f() int64 fails Gone {\n\treturn 1\n}\n```\n\n```go\nnot ergo at all (\n```\n"
	if len(notes) > 0 || got != want {
		t.Fatalf("Markdown (notes %v):\n%s", notes, got)
	}
	// A fence in a list item is indented, and so is its closing line.
	md = "1. Item:\n\n   ```go\n   x := 1\n   use(x)\n   ```\n\n```go\ny := 2\nuse(y)\n```\n"
	got, notes = Markdown(md)
	want = "1. Item:\n\n   ```go\n   let x = 1\n   use(x)\n   ```\n\n```go\nlet y = 2\nuse(y)\n```\n"
	if len(notes) > 0 || got != want {
		t.Fatalf("Markdown indented (notes %v):\n%s", notes, got)
	}
}
