package overflowbench

import (
	"errors"
	"math"
	"math/big"
	"testing"
)

// Benchmark inputs. They are built at run time from a seed, so the compiler
// cannot fold them.
const (
	benchItems   = 1024
	benchSeed    = 0x108
	shortKeyLen  = 12 // the longest lowercase key whose hash never overflows
	shortKeys    = 256
	longKeyLen   = 4096
	overflowByte = 12 // byte index at which a longer key first overflows
)

var (
	sink    int64
	sinkErr error
)

// ---- the overflow tests against math/big ----------------------------------------

type check struct {
	name  string
	op    func(z, a, b *big.Int) *big.Int
	check func(a, b int64) (int64, bool)
}

var checks = []check{
	{"addCmp", (*big.Int).Add, addCmp},
	{"addPost", (*big.Int).Add, addPost},
	{"addXor", (*big.Int).Add, addXor},
	{"addBits", (*big.Int).Add, addBits},
	{"subCmp", (*big.Int).Sub, subCmp},
	{"subPost", (*big.Int).Sub, subPost},
	{"subXor", (*big.Int).Sub, subXor},
	{"subBits", (*big.Int).Sub, subBits},
	{"mulCmp", (*big.Int).Mul, mulCmp},
	{"mulBits", (*big.Int).Mul, mulBits},
	{"mulRange", (*big.Int).Mul, mulRange},
}

// operands are the boundary values plus run-time pseudo-random values of
// every magnitude.
func operands() []int64 {
	xs := []int64{
		math.MinInt64, math.MinInt64 + 1, math.MinInt64 / 2, -1 << 32, -3037000500, -3037000499,
		-31, -2, -1, 0, 1, 2, 31, 3037000499, 3037000500, 1 << 32, math.MaxInt64 / 2,
		math.MaxInt64/2 + 1, math.MaxInt64 - 1, math.MaxInt64,
	}
	g := lcg{state: 7}
	for shift := 0; shift < 64; shift += 3 {
		v := int64(g.next()<<31^g.next()) >> shift
		xs = append(xs, v, -v)
	}
	return xs
}

func TestChecksAgreeWithBig(t *testing.T) {
	xs := operands()
	minI, maxI := big.NewInt(math.MinInt64), big.NewInt(math.MaxInt64)
	for _, c := range checks {
		overflows := 0
		for _, a := range xs {
			for _, b := range xs {
				exact := c.op(new(big.Int), big.NewInt(a), big.NewInt(b))
				want := exact.Cmp(minI) < 0 || exact.Cmp(maxI) > 0
				// The wrapped result is the exact result modulo 2^64.
				wrapped := int64(new(big.Int).And(exact, new(big.Int).SetUint64(math.MaxUint64)).Uint64())
				got, overflow := c.check(a, b)
				if overflow != want || got != wrapped {
					t.Fatalf("%s(%d, %d) = (%d, %v), want (%d, %v)", c.name, a, b, got, overflow, wrapped, want)
				}
				if want {
					overflows++
				}
			}
		}
		if overflows == 0 || overflows == len(xs)*len(xs) {
			t.Fatalf("%s: %d of %d pairs overflow; the operands do not exercise both outcomes", c.name, overflows, len(xs)*len(xs))
		}
	}
}

func TestCheckedOperatorsReportOverflow(t *testing.T) {
	defects := map[string]func(){
		"addDefectCmp":  func() { addDefectCmp(math.MaxInt64, 1) },
		"subDefectCmp":  func() { subDefectCmp(math.MinInt64, 1) },
		"mulDefectCmp":  func() { mulDefectCmp(-1, math.MinInt64) },
		"addDefectPost": func() { addDefectPost(math.MinInt64, -1) },
		"subDefectPost": func() { subDefectPost(math.MaxInt64, -1) },
		"addDefectXor":  func() { addDefectXor(math.MaxInt64, 1) },
		"subDefectXor":  func() { subDefectXor(math.MinInt64, 1) },
		"addDefectBits": func() { addDefectBits(math.MaxInt64, 1) },
		"subDefectBits": func() { subDefectBits(math.MinInt64, 1) },
		"mulDefectBits": func() { mulDefectBits(math.MinInt64, -1) },
	}
	for name, f := range defects {
		if got := recovered(f); got != overflowDefect {
			t.Errorf("%s: recovered %v, want the overflow defect", name, got)
		}
	}
	fails := map[string]func() (int64, error){
		"addFailCmp":  func() (int64, error) { return addFailCmp(math.MaxInt64, 1) },
		"subFailCmp":  func() (int64, error) { return subFailCmp(math.MinInt64, 1) },
		"mulFailCmp":  func() (int64, error) { return mulFailCmp(math.MinInt64, -1) },
		"addFailPost": func() (int64, error) { return addFailPost(math.MaxInt64, 1) },
		"subFailPost": func() (int64, error) { return subFailPost(math.MinInt64, 1) },
		"addFailXor":  func() (int64, error) { return addFailXor(math.MinInt64, -1) },
		"subFailXor":  func() (int64, error) { return subFailXor(math.MaxInt64, -1) },
		"addFailBits": func() (int64, error) { return addFailBits(math.MaxInt64, 1) },
		"subFailBits": func() (int64, error) { return subFailBits(math.MinInt64, 1) },
		"mulFailBits": func() (int64, error) { return mulFailBits(3037000500, 3037000500) },
	}
	for name, f := range fails {
		if v, err := f(); v != 0 || !errors.As(err, new(Overflow)) {
			t.Errorf("%s = (%d, %v), want (0, Overflow)", name, v, err)
		}
	}
}

func recovered(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return nil
}

// ---- kernel 1: agreement before timing --------------------------------------------

type sumVariant struct {
	name string
	sum  func([]Item) (int64, error)
}

func infallible(f func([]Item) int64) func([]Item) (int64, error) {
	return func(items []Item) (int64, error) { return f(items), nil }
}

var sumVariants = []sumVariant{
	{"wrap", infallible(SumWrap)},
	{"defect-cmp", infallible(SumDefectCmp)},
	{"defect-post", infallible(SumDefectPost)},
	{"defect-xor", infallible(SumDefectXor)},
	{"defect-bits", infallible(SumDefectBits)},
	{"defect-range", infallible(SumDefectRange)},
	{"fail-cmp", SumFailCmp},
	{"fail-post", SumFailPost},
	{"fail-xor", SumFailXor},
	{"fail-bits", SumFailBits},
	{"fail-range", SumFailRange},
}

func bigSum(items []Item) *big.Int {
	total := new(big.Int)
	for _, it := range items {
		line := new(big.Int).Mul(big.NewInt(it.Qty), big.NewInt(it.Price))
		total.Add(total, line.Sub(line, big.NewInt(it.Discount)))
	}
	return total
}

// TestAccountingSumAgrees is the agreement check: on inputs that never
// overflow, every variant returns the exact total and no failure. The
// benchmark input is among the cases.
func TestAccountingSumAgrees(t *testing.T) {
	cases := [][]Item{nil, NewItems(1, 1), NewItems(benchItems, benchSeed), NewItems(10_000, 99)}
	for _, items := range cases {
		want := bigSum(items)
		if !want.IsInt64() {
			t.Fatalf("input of %d lines overflows; the generator broke its bound", len(items))
		}
		for _, v := range sumVariants {
			got, err := v.sum(items)
			if err != nil || got != want.Int64() {
				t.Errorf("%s on %d lines = (%d, %v), want (%d, nil)", v.name, len(items), got, err, want.Int64())
			}
		}
	}
}

// TestAccountingSumOverflowControl proves that each check is live inside its
// kernel: one input overflows at *, one at - and one at +, and every checked
// variant must refuse each one while wrap returns the wrapped total.
func TestAccountingSumOverflowControl(t *testing.T) {
	controls := map[string][]Item{
		"mul": {{Qty: math.MaxInt64/2 + 1, Price: 2}},
		"sub": {{Qty: 1, Price: math.MinInt64 + 5, Discount: 6}},
		"add": {{Qty: 1, Price: math.MaxInt64}, {Qty: 1, Price: 1}},
	}
	for op, items := range controls {
		exact := bigSum(items)
		wrapped := int64(new(big.Int).And(exact, new(big.Int).SetUint64(math.MaxUint64)).Uint64())
		for _, v := range sumVariants {
			var (
				got int64
				err error
			)
			r := recovered(func() { got, err = v.sum(items) })
			switch {
			case v.name == "wrap":
				if r != nil || err != nil || got != wrapped {
					t.Errorf("%s overflow, wrap = (%d, %v, panic %v), want %d", op, got, err, r, wrapped)
				}
			case v.name[:6] == "defect":
				if r != overflowDefect {
					t.Errorf("%s overflow, %s recovered %v, want the overflow defect", op, v.name, r)
				}
			default:
				if r != nil || got != 0 || !errors.As(err, new(Overflow)) {
					t.Errorf("%s overflow, %s = (%d, %v, panic %v), want (0, Overflow)", op, v.name, got, err, r)
				}
			}
		}
	}
}

// ---- kernel 2: the hash mix policy comparison -------------------------------------------

type hashVariant struct {
	name string
	hash func([]byte) (int64, error)
}

func infallibleHash(f func([]byte) int64) func([]byte) (int64, error) {
	return func(key []byte) (int64, error) { return f(key), nil }
}

var hashVariants = []hashVariant{
	{"wrap", infallibleHash(HashWrap)},
	{"explicit-wrap", infallibleHash(HashExplicitWrap)},
	{"defect-cmp", infallibleHash(HashDefectCmp)},
	{"defect-bits", infallibleHash(HashDefectBits)},
	{"fail-cmp", HashFailCmp},
	{"fail-bits", HashFailBits},
}

// bigHash is the exact polynomial hash.
func bigHash(key []byte) *big.Int {
	h := new(big.Int)
	for _, b := range key {
		h.Mul(h, big.NewInt(31)).Add(h, big.NewInt(int64(b)))
	}
	return h
}

// uintHash is the wrapped hash computed on uint64, independently of int64.
func uintHash(key []byte) int64 {
	var h uint64
	for _, b := range key {
		h = h*31 + uint64(b)
	}
	return int64(h)
}

// TestHashMixPolicy records what each policy does to code that relies on
// wrap. Short keys never overflow, so every variant agrees; at the first
// overflowing byte the wrap variants return the wrapped hash, the defect
// variants panic and the failure variants return Overflow.
func TestHashMixPolicy(t *testing.T) {
	long := NewKey(longKeyLen, benchSeed)
	for n := 0; n <= 40; n++ {
		key := long[:n]
		overflows := !bigHash(key).IsInt64()
		if overflows != (n > overflowByte) {
			t.Fatalf("key of %d bytes: overflow %v; want the first overflow at byte index %d", n, overflows, overflowByte)
		}
		want := uintHash(key)
		for _, v := range hashVariants {
			var (
				got int64
				err error
			)
			r := recovered(func() { got, err = v.hash(key) })
			switch {
			case !overflows || v.name == "wrap" || v.name == "explicit-wrap":
				if r != nil || err != nil || got != want {
					t.Errorf("%s on %d bytes = (%d, %v, panic %v), want %d", v.name, n, got, err, r, want)
				}
			case v.name[:6] == "defect":
				if r != overflowDefect {
					t.Errorf("%s on %d bytes recovered %v, want the overflow defect", v.name, n, r)
				}
			default:
				if r != nil || got != 0 || !errors.As(err, new(Overflow)) {
					t.Errorf("%s on %d bytes = (%d, %v, panic %v), want (0, Overflow)", v.name, n, got, err, r)
				}
			}
		}
	}
	// The extremes of the alphabet bound every lowercase key.
	for _, c := range []byte{'a', 'z'} {
		key := make([]byte, overflowByte+1)
		for i := range key {
			key[i] = c
		}
		if !bigHash(key[:overflowByte]).IsInt64() || bigHash(key).IsInt64() {
			t.Fatalf("a key of %q does not first overflow at byte index %d", c, overflowByte)
		}
	}
}

// ---- operator kernels: agreement ------------------------------------------------------

func opInputs(seed uint64) (xs, ys []int64) {
	g := lcg{state: seed}
	xs, ys = make([]int64, benchItems), make([]int64, benchItems)
	for i := range xs {
		xs[i] = int64(g.next()%2_000_000_001) - 1_000_000_000
		ys[i] = int64(g.next()%2_000_000_001) - 1_000_000_000
	}
	return xs, ys
}

func TestOperatorKernelsAgree(t *testing.T) {
	xs, ys := opInputs(benchSeed)
	for name, got := range map[string]int64{"cmp": OpAddCmp(xs), "post": OpAddPost(xs), "xor": OpAddXor(xs), "bits": OpAddBits(xs)} {
		if want := OpAddWrap(xs); got != want {
			t.Errorf("add %s = %d, want %d", name, got, want)
		}
	}
	for name, got := range map[string]int64{"cmp": OpSubCmp(xs), "post": OpSubPost(xs), "xor": OpSubXor(xs), "bits": OpSubBits(xs)} {
		if want := OpSubWrap(xs); got != want {
			t.Errorf("sub %s = %d, want %d", name, got, want)
		}
	}
	for name, got := range map[string]int64{"cmp": OpMulCmp(xs, ys), "bits": OpMulBits(xs, ys), "range": OpMulRange(xs, ys)} {
		if want := OpMulWrap(xs, ys); got != want {
			t.Errorf("mul %s = %d, want %d", name, got, want)
		}
	}
}

// ---- benchmarks ---------------------------------------------------------------------------

func BenchmarkAccountingSum(b *testing.B) {
	items := NewItems(benchItems, benchSeed)
	for _, v := range sumVariants {
		b.Run(v.name, func(b *testing.B) {
			// The closure call is outside the kernel; every variant pays it.
			sum := v.sum
			for b.Loop() {
				sink, sinkErr = sum(items)
			}
		})
	}
}

func BenchmarkHashMix(b *testing.B) {
	keys := NewKey(shortKeyLen*shortKeys, benchSeed)
	long := NewKey(longKeyLen, benchSeed)
	b.Run("short", func(b *testing.B) {
		for _, v := range hashVariants {
			b.Run(v.name, func(b *testing.B) {
				hash := v.hash
				for b.Loop() {
					var acc int64
					for k := 0; k < len(keys); k += shortKeyLen {
						h, err := hash(keys[k : k+shortKeyLen])
						acc += h
						sinkErr = err
					}
					sink = acc
				}
			})
		}
	})
	b.Run("long", func(b *testing.B) {
		for _, v := range hashVariants[:2] {
			b.Run(v.name, func(b *testing.B) {
				hash := v.hash
				for b.Loop() {
					sink, sinkErr = hash(long)
				}
			})
		}
	})
}

func BenchmarkOp(b *testing.B) {
	xs, ys := opInputs(benchSeed)
	unary := func(name string, fs map[string]func([]int64) int64) {
		b.Run(name, func(b *testing.B) {
			for _, family := range []string{"wrap", "cmp", "post", "xor", "bits"} {
				f, ok := fs[family]
				if !ok {
					continue
				}
				b.Run(family, func(b *testing.B) {
					for b.Loop() {
						sink = f(xs)
					}
				})
			}
		})
	}
	unary("add", map[string]func([]int64) int64{"wrap": OpAddWrap, "cmp": OpAddCmp, "post": OpAddPost, "xor": OpAddXor, "bits": OpAddBits})
	unary("sub", map[string]func([]int64) int64{"wrap": OpSubWrap, "cmp": OpSubCmp, "post": OpSubPost, "xor": OpSubXor, "bits": OpSubBits})
	b.Run("mul", func(b *testing.B) {
		for _, v := range []struct {
			name string
			f    func(xs, ys []int64) int64
		}{{"wrap", OpMulWrap}, {"cmp", OpMulCmp}, {"bits", OpMulBits}, {"range", OpMulRange}} {
			b.Run(v.name, func(b *testing.B) {
				for b.Loop() {
					sink = v.f(xs, ys)
				}
			})
		}
	})
}
