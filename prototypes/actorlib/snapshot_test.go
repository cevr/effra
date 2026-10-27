package actorlib

import (
	"slices"
	"sync/atomic"
	"testing"
)

type inner struct {
	tags []string
}

type nested struct {
	name   string
	byKey  map[string][]int
	inner  inner
	arr    [2][]int
	any    any
	lease  lease // a resource: moves, is not copied
	next   *nested
	plainA [3]int64
}

func TestSnapshotSharesNoMutableStorage(t *testing.T) {
	var closed atomic.Int32
	shared := &nested{name: "pointer"}
	v := nested{
		name:  "v",
		byKey: map[string][]int{"a": {1, 2}},
		inner: inner{tags: []string{"x"}},
		arr:   [2][]int{{1}, {2}},
		any:   []string{"dyn"},
		lease: lease{&closed},
		next:  shared,
	}
	c := snapshot(v)
	c.byKey["a"][0] = 99
	c.byKey["b"] = nil
	c.inner.tags[0] = "changed"
	c.arr[1][0] = 99
	c.any.([]string)[0] = "changed"
	if v.byKey["a"][0] != 1 || len(v.byKey) != 1 || v.inner.tags[0] != "x" || v.arr[1][0] != 2 || v.any.([]string)[0] != "dyn" {
		t.Errorf("the copy aliases the original: %+v", v)
	}
	if c.lease != v.lease || c.next != shared {
		t.Errorf("a resource or a pointer was copied")
	}
}

func TestSnapshotOfAValueWithoutMutableStorageIsItself(t *testing.T) {
	type point struct{ x, y int64 }
	if got := snapshot(point{1, 2}); got != (point{1, 2}) {
		t.Errorf("got %v", got)
	}
	var i any = point{3, 4}
	if got := snapshot(i); got != i {
		t.Errorf("got %v", got)
	}
	var nilIface any
	if got := snapshot(nilIface); got != nil {
		t.Errorf("got %v", got)
	}
	s := []int64{1}
	if got := snapshot(s); !slices.Equal(got, s) || &got[0] == &s[0] {
		t.Errorf("a slice must be copied")
	}
}

func TestSnapshotAllocatesNothingWithoutMutableStorage(t *testing.T) {
	type point struct{ x, y int64 }
	var i any = point{3, 4}
	if n := testing.AllocsPerRun(100, func() { _ = snapshot(point{1, 2}); _ = snapshot(i) }); n != 0 {
		t.Errorf("snapshot of a value with no slice or map allocated %v times", n)
	}
}
