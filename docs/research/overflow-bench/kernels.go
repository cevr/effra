// Package overflowbench holds hand-emitted Go for issue #108: what an
// overflow-checked int64 +, - and * costs against wrapping, in direct-Go
// output. It is measurement only; nothing here is compiler or runtime code.
//
// Each kernel exists in three policy variants:
//
//   - wrap: plain Go int64 operators, the signed-64 wrap profile that the
//     sketch types today (docs/spec/types.md, Basic types 2).
//   - defect: check, then panic. A defect is not in any error row
//     (docs/spec/rows.md, rule 6); the prototype emitter lowers a failure in
//     a frame that cannot fail to panic (internal/sketch/proto/emit.go, fail).
//   - fail: check, then a typed failure. A function with error[Overflow]
//     lowers to a Go (T, error) result; the error type is a value struct
//     with a value-receiver Error method, and a fallible call is followed by
//     `if err != nil { return zero, err }` (emit.go, signature and fail).
//
// Every checked variant comes in check families that differ only in how
// the overflow test is spelled:
//
//   - cmp: compare before the operation (CERT INT32-C), and for * divide the
//     product back. This is what Effra source could spell without a library.
//
//   - post: compute the wrapped result, then compare it with an operand.
//
//   - xor: compute the wrapped result, then test the sign bits.
//
//   - bits: math/bits (bits.Add64, bits.Sub64, bits.Mul64) with the signed
//     overflow derived from the carry or the high word.
//
//   - range: the xor family for + and -, and for * a plain multiply when
//     both operands fit in 32 bits, else bits.Mul64.
//
// The families differ for + and -. For * there are three checks: divide
// back (cmp), bits.Mul64, and the 32-bit range fast path. The post, xor and
// bits families all use bits.Mul64, so they differ from each other only in
// + and -.
//
// The kernels are marked noinline so that each has its own hot loop in the
// assembly and every variant pays the same call cost.
package overflowbench

import (
	"math"
	"math/bits"
)

// overflowDefect is the defect payload. A constant string panics without an
// allocation, as a defect in emitted Go does.
const overflowDefect = "effra: int64 overflow"

// Overflow is the typed failure a checked operator reports, lowered as the
// prototype emitter lowers an error type: a struct with a value Error method.
type Overflow struct{}

func (Overflow) Error() string { return "int64 overflow" }

// ---- overflow tests ---------------------------------------------------------
//
// Each returns the wrapped result and whether the exact result is outside
// the int64 range. The tests compare them with math/big.

// addCmp checks before adding.
func addCmp(a, b int64) (int64, bool) {
	return a + b, (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b)
}

func subCmp(a, b int64) (int64, bool) {
	return a - b, (b < 0 && a > math.MaxInt64+b) || (b > 0 && a < math.MinInt64+b)
}

// mulCmp divides the product back. Go wraps MinInt64 / -1 to MinInt64, so
// the pair (-1, MinInt64) needs its own case.
func mulCmp(a, b int64) (int64, bool) {
	p := a * b
	if a == 0 {
		return p, false
	}
	if a == -1 && b == math.MinInt64 {
		return p, true
	}
	return p, p/a != b
}

// addPost compares the wrapped sum with an operand: the sum fell when b is
// not negative, or rose when b is negative.
func addPost(a, b int64) (int64, bool) {
	s := a + b
	return s, (s < a) != (b < 0)
}

func subPost(a, b int64) (int64, bool) {
	d := a - b
	return d, (d > a) != (b < 0)
}

// addXor is the two's-complement identity: overflow when both operands
// have the same sign and the sum has the other.
func addXor(a, b int64) (int64, bool) {
	s := a + b
	return s, (s^a)&(s^b) < 0
}

func subXor(a, b int64) (int64, bool) {
	d := a - b
	return d, (a^b)&(a^d) < 0
}

// addBits uses bits.Add64. Its carry is the unsigned overflow; the signed
// overflow is the carry out of bit 63 xor the carry into it.
func addBits(a, b int64) (int64, bool) {
	s, c := bits.Add64(uint64(a), uint64(b), 0)
	return int64(s), (c^((s^uint64(a)^uint64(b))>>63))&1 != 0
}

func subBits(a, b int64) (int64, bool) {
	d, borrow := bits.Sub64(uint64(a), uint64(b), 0)
	return int64(d), (borrow^((d^uint64(a)^uint64(b))>>63))&1 != 0
}

// mulBits uses bits.Mul64. The signed high word is the unsigned high word
// minus b when a is negative and minus a when b is negative; the product
// fits when that high word is the sign extension of the low word.
func mulBits(a, b int64) (int64, bool) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	shi := int64(hi) - (a>>63)&b - (b>>63)&a
	return int64(lo), shi != int64(lo)>>63
}

// mulRange multiplies directly when both operands fit in 32 bits, where the
// product cannot overflow, and takes the bits.Mul64 check otherwise. It is a
// lowering an emitter can choose without knowing the values: one biased
// unsigned compare guards the plain IMULQ.
//
// Wrapped in a defect or failure helper, this check exceeds Go's inlining
// budget, so every multiplication becomes a call (an outlined slow path
// does not help: the call itself costs most of the budget). The range
// kernels therefore emit the fast path inline, as an emitter writes code at
// the operation, and call the inlinable bits helper on the slow path.
// mulRange is the reference those kernels follow, tested against math/big.
func mulRange(a, b int64) (int64, bool) {
	if fits32(a, b) {
		return a * b, false
	}
	return mulBits(a, b)
}

// fits32 reports whether both operands are in [-2^31, 2^31).
func fits32(a, b int64) bool {
	return uint64(a+1<<31)|uint64(b+1<<31) < 1<<32
}

// ---- inputs -------------------------------------------------------------------

// Item is one accounting line in cents.
type Item struct {
	Qty, Price, Discount int64
}

type lcg struct{ state uint64 }

func (g *lcg) next() uint64 {
	g.state = g.state*6364136223846793005 + 1442695040888963407
	return g.state >> 33
}

// NewItems returns n lines with quantity in [1, 1000], price in [1, 1e9] and
// a discount of at most a tenth of the line. A line is at most 1e12, so a sum
// of fewer than 9e6 lines cannot overflow.
func NewItems(n int, seed uint64) []Item {
	g := lcg{state: seed}
	items := make([]Item, n)
	for i := range items {
		qty := int64(1 + g.next()%1000)
		price := int64(1 + g.next()%1_000_000_000)
		discount := int64(g.next() % uint64(qty*price/10+1))
		items[i] = Item{Qty: qty, Price: price, Discount: discount}
	}
	return items
}

// NewKey returns n lowercase ASCII letters.
func NewKey(n int, seed uint64) []byte {
	g := lcg{state: seed}
	key := make([]byte, n)
	for i := range key {
		key[i] = byte('a' + g.next()%26)
	}
	return key
}

// ---- kernel 1: accounting sum ---------------------------------------------------
//
// total += qty*price - discount, one *, one - and one + per line. The inputs
// never overflow, so every variant must return the same total.

//go:noinline
func SumWrap(items []Item) int64 {
	var total int64
	for i := range items {
		it := &items[i]
		total += it.Qty*it.Price - it.Discount
	}
	return total
}

//go:noinline
func SumDefectCmp(items []Item) int64 {
	var total int64
	for i := range items {
		it := &items[i]
		line := mulDefectCmp(it.Qty, it.Price)
		net := subDefectCmp(line, it.Discount)
		total = addDefectCmp(total, net)
	}
	return total
}

//go:noinline
func SumDefectPost(items []Item) int64 {
	var total int64
	for i := range items {
		it := &items[i]
		line := mulDefectBits(it.Qty, it.Price)
		net := subDefectPost(line, it.Discount)
		total = addDefectPost(total, net)
	}
	return total
}

//go:noinline
func SumDefectXor(items []Item) int64 {
	var total int64
	for i := range items {
		it := &items[i]
		line := mulDefectBits(it.Qty, it.Price)
		net := subDefectXor(line, it.Discount)
		total = addDefectXor(total, net)
	}
	return total
}

//go:noinline
func SumDefectBits(items []Item) int64 {
	var total int64
	for i := range items {
		it := &items[i]
		line := mulDefectBits(it.Qty, it.Price)
		net := subDefectBits(line, it.Discount)
		total = addDefectBits(total, net)
	}
	return total
}

// SumDefectRange is the xor family with the range fast path for *, emitted
// inline (see mulRange).
//
//go:noinline
func SumDefectRange(items []Item) int64 {
	var total int64
	for i := range items {
		it := &items[i]
		var line int64
		if fits32(it.Qty, it.Price) {
			line = it.Qty * it.Price
		} else {
			line = mulDefectBits(it.Qty, it.Price)
		}
		net := subDefectXor(line, it.Discount)
		total = addDefectXor(total, net)
	}
	return total
}

//go:noinline
func SumFailCmp(items []Item) (int64, error) {
	var total int64
	for i := range items {
		it := &items[i]
		line, err := mulFailCmp(it.Qty, it.Price)
		if err != nil {
			return 0, err
		}
		net, err := subFailCmp(line, it.Discount)
		if err != nil {
			return 0, err
		}
		total, err = addFailCmp(total, net)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

//go:noinline
func SumFailPost(items []Item) (int64, error) {
	var total int64
	for i := range items {
		it := &items[i]
		line, err := mulFailBits(it.Qty, it.Price)
		if err != nil {
			return 0, err
		}
		net, err := subFailPost(line, it.Discount)
		if err != nil {
			return 0, err
		}
		total, err = addFailPost(total, net)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

//go:noinline
func SumFailXor(items []Item) (int64, error) {
	var total int64
	for i := range items {
		it := &items[i]
		line, err := mulFailBits(it.Qty, it.Price)
		if err != nil {
			return 0, err
		}
		net, err := subFailXor(line, it.Discount)
		if err != nil {
			return 0, err
		}
		total, err = addFailXor(total, net)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

//go:noinline
func SumFailBits(items []Item) (int64, error) {
	var total int64
	for i := range items {
		it := &items[i]
		line, err := mulFailBits(it.Qty, it.Price)
		if err != nil {
			return 0, err
		}
		net, err := subFailBits(line, it.Discount)
		if err != nil {
			return 0, err
		}
		total, err = addFailBits(total, net)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

//go:noinline
func SumFailRange(items []Item) (int64, error) {
	var total int64
	for i := range items {
		it := &items[i]
		var line int64
		if fits32(it.Qty, it.Price) {
			line = it.Qty * it.Price
		} else {
			var err error
			line, err = mulFailBits(it.Qty, it.Price)
			if err != nil {
				return 0, err
			}
		}
		net, err := subFailXor(line, it.Discount)
		if err != nil {
			return 0, err
		}
		total, err = addFailXor(total, net)
		if err != nil {
			return 0, err
		}
	}
	return total, nil
}

// ---- kernel 2: hash mix that relies on wrap ----------------------------------------
//
// h = h*31 + b over the key's bytes, the polynomial string hash. With
// lowercase letters h*31 passes 2^63 at the thirteenth byte, so a key of at
// most twelve bytes never overflows and a longer key always does. Wrap
// returns a hash; a checked variant reports the overflow at byte index 12.

//go:noinline
func HashWrap(key []byte) int64 {
	var h int64
	for _, b := range key {
		h = h*31 + int64(b)
	}
	return h
}

// HashExplicitWrap is the rewrite that a checked default asks of code that
// relies on wrap: named wrapping operations (Rust wrapping_mul, Swift &*,
// Zig *%). They lower to the plain Go operators.
//
//go:noinline
func HashExplicitWrap(key []byte) int64 {
	var h int64
	for _, b := range key {
		h = wrapAdd(wrapMul(h, 31), int64(b))
	}
	return h
}

func wrapAdd(a, b int64) int64 { return a + b }
func wrapMul(a, b int64) int64 { return a * b }

// HashDefectCmp checks the constant multiplier with two constant bounds, the
// check a compiler can emit when it knows the multiplier, and the sum with
// the compare form.
//
//go:noinline
func HashDefectCmp(key []byte) int64 {
	var h int64
	for _, b := range key {
		if h > math.MaxInt64/31 || h < math.MinInt64/31 {
			panic(overflowDefect)
		}
		h = addDefectCmp(h*31, int64(b))
	}
	return h
}

//go:noinline
func HashDefectBits(key []byte) int64 {
	var h int64
	for _, b := range key {
		h = addDefectXor(mulDefectBits(h, 31), int64(b))
	}
	return h
}

//go:noinline
func HashFailCmp(key []byte) (int64, error) {
	var h int64
	for _, b := range key {
		if h > math.MaxInt64/31 || h < math.MinInt64/31 {
			return 0, Overflow{}
		}
		var err error
		h, err = addFailCmp(h*31, int64(b))
		if err != nil {
			return 0, err
		}
	}
	return h, nil
}

//go:noinline
func HashFailBits(key []byte) (int64, error) {
	var h int64
	for _, b := range key {
		m, err := mulFailBits(h, 31)
		if err != nil {
			return 0, err
		}
		h, err = addFailXor(m, int64(b))
		if err != nil {
			return 0, err
		}
	}
	return h, nil
}

// ---- operator kernels -----------------------------------------------------------------
//
// One checked operator each, defect style, to attribute the accounting cost
// to an operator and a check family. Add and sub carry the result from one
// iteration to the next; mul multiplies pairs and wrap-adds the products.

//go:noinline
func OpAddWrap(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc += x
	}
	return acc
}

//go:noinline
func OpAddCmp(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = addDefectCmp(acc, x)
	}
	return acc
}

//go:noinline
func OpAddPost(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = addDefectPost(acc, x)
	}
	return acc
}

//go:noinline
func OpAddXor(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = addDefectXor(acc, x)
	}
	return acc
}

//go:noinline
func OpAddBits(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = addDefectBits(acc, x)
	}
	return acc
}

//go:noinline
func OpSubWrap(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc -= x
	}
	return acc
}

//go:noinline
func OpSubCmp(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = subDefectCmp(acc, x)
	}
	return acc
}

//go:noinline
func OpSubPost(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = subDefectPost(acc, x)
	}
	return acc
}

//go:noinline
func OpSubXor(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = subDefectXor(acc, x)
	}
	return acc
}

//go:noinline
func OpSubBits(xs []int64) int64 {
	var acc int64
	for _, x := range xs {
		acc = subDefectBits(acc, x)
	}
	return acc
}

//go:noinline
func OpMulWrap(xs, ys []int64) int64 {
	var acc int64
	for i, x := range xs {
		acc += x * ys[i]
	}
	return acc
}

//go:noinline
func OpMulCmp(xs, ys []int64) int64 {
	var acc int64
	for i, x := range xs {
		acc += mulDefectCmp(x, ys[i])
	}
	return acc
}

//go:noinline
func OpMulBits(xs, ys []int64) int64 {
	var acc int64
	for i, x := range xs {
		acc += mulDefectBits(x, ys[i])
	}
	return acc
}

//go:noinline
func OpMulRange(xs, ys []int64) int64 {
	var acc int64
	for i, x := range xs {
		y := ys[i]
		if fits32(x, y) {
			acc += x * y
		} else {
			acc += mulDefectBits(x, y)
		}
	}
	return acc
}

// ---- checked operators: defect ---------------------------------------------------------

func addDefectCmp(a, b int64) int64 {
	r, o := addCmp(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func subDefectCmp(a, b int64) int64 {
	r, o := subCmp(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func mulDefectCmp(a, b int64) int64 {
	r, o := mulCmp(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func addDefectPost(a, b int64) int64 {
	r, o := addPost(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func subDefectPost(a, b int64) int64 {
	r, o := subPost(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func addDefectXor(a, b int64) int64 {
	r, o := addXor(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func subDefectXor(a, b int64) int64 {
	r, o := subXor(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func addDefectBits(a, b int64) int64 {
	r, o := addBits(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func subDefectBits(a, b int64) int64 {
	r, o := subBits(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

func mulDefectBits(a, b int64) int64 {
	r, o := mulBits(a, b)
	if o {
		panic(overflowDefect)
	}
	return r
}

// ---- checked operators: typed failure ----------------------------------------------------

func addFailCmp(a, b int64) (int64, error) {
	r, o := addCmp(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func subFailCmp(a, b int64) (int64, error) {
	r, o := subCmp(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func mulFailCmp(a, b int64) (int64, error) {
	r, o := mulCmp(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func addFailPost(a, b int64) (int64, error) {
	r, o := addPost(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func subFailPost(a, b int64) (int64, error) {
	r, o := subPost(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func addFailXor(a, b int64) (int64, error) {
	r, o := addXor(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func subFailXor(a, b int64) (int64, error) {
	r, o := subXor(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func addFailBits(a, b int64) (int64, error) {
	r, o := addBits(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func subFailBits(a, b int64) (int64, error) {
	r, o := subBits(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}

func mulFailBits(a, b int64) (int64, error) {
	r, o := mulBits(a, b)
	if o {
		return 0, Overflow{}
	}
	return r, nil
}
