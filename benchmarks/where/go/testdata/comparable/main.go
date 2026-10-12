// Option (c) control for spike-where (#116): the Go that a structural
// comparable / cmp.Ordered design would emit, hand-written because Effra has
// no such constraint. It is under testdata so `go build ./...` skips it; run
// it with `go run .` in this directory.
//
// It shows what (c) leaves to run time: NaN under cmp.Ordered, a NaN map key
// that is never found again, and an interface-typed key holding a function,
// which panics when hashed.
package main

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

type Three[T any] struct{ A, B, C T }

// sort3 for an Ordered element: the operator, no witness.
func sort3[T cmp.Ordered](xs Three[T]) Three[T] {
	s := []T{xs.A, xs.B, xs.C}
	slices.Sort(s)
	return Three[T]{s[0], s[1], s[2]}
}

// A record is not Ordered, so it needs SortFunc and a comparison function:
// option (a)'s explicit witness.
type User struct {
	Name string
	Age  int64
}

func sort3Func[T any](xs Three[T], compare func(T, T) int) Three[T] {
	s := []T{xs.A, xs.B, xs.C}
	slices.SortFunc(s, compare)
	return Three[T]{s[0], s[1], s[2]}
}

// A record of comparable fields is comparable: a native map key.
type UserID struct {
	Org string
	N   int64
}

// An enum lowers to an interface; its variants are comparable only if every
// dynamic value is, which the compiler cannot check.
type Key interface{ isKey() }
type NamedKey struct{ Name string }
type CallbackKey struct{ Run func() }

func (NamedKey) isKey()    {}
func (CallbackKey) isKey() {}

func main() {
	fmt.Println(sort3(Three[int64]{3, 1, 2}), sort3(Three[string]{"b", "a", "c"}))
	fmt.Println(sort3(Three[float64]{2, math.NaN(), 1}), "cmp.Compare(NaN, 1) =", cmp.Compare(math.NaN(), 1.0))
	fmt.Println(sort3Func(Three[User]{User{"c", 30}, User{"a", 10}, User{"b", 20}}, func(a, b User) int { return cmp.Compare(a.Age, b.Age) }))

	byID := map[UserID]string{}
	byID[UserID{"acme", 7}] = "ada"
	byID[UserID{"acme", 7}] = "ada2"
	fmt.Println("struct key:", byID[UserID{"acme", 7}], len(byID))

	floats := map[float64]string{}
	floats[math.NaN()] = "v"
	_, found := floats[math.NaN()]
	fmt.Println("NaN key found:", found, "entries:", len(floats))

	keys := map[Key]string{NamedKey{"a"}: "named"}
	defer func() { fmt.Println("callback key:", recover()) }()
	keys[CallbackKey{func() {}}] = "boom"
}
