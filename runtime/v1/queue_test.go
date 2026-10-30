package v1

import (
	"fmt"
	"iter"
	"math/rand/v2"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ergo.local/prototype/runtime/rt"
)

// The operations interface the lowering emits for queue.Queue[int].
var _ interface {
	Push(int)
	Pop() int
	Close()
	Values() iter.Seq[int]
} = (*Queue[int])(nil)

// interrupts reports whether f raises the interruption, and fails the test
// on any other panic.
func interrupts(t *testing.T, f func()) bool {
	t.Helper()
	p := recovered(f)
	if p != nil && p != rt.Interrupted {
		t.Fatalf("raised %#v; want rt.Interrupted or nothing", p)
	}
	return p == rt.Interrupted
}

// blocked reports whether done is still open after a moment: the operation
// that closes it is waiting.
func blocked(done <-chan struct{}) bool {
	select {
	case <-done:
		return false
	case <-time.After(20 * time.Millisecond):
		return true
	}
}

// TestQueueContract walks each rule of the queue contract (runtime.md,
// Queues).
func TestQueueContract(t *testing.T) {
	t.Run("FIFO", func(t *testing.T) {
		q := New[int](3)
		q.Push(1)
		q.Push(2)
		q.Push(3)
		for want := 1; want <= 3; want++ {
			if got := q.Pop(); got != want {
				t.Fatalf("Pop() = %d; want %d", got, want)
			}
		}
	})
	t.Run("backpressure", func(t *testing.T) {
		q := New[int](1)
		q.Push(1)
		done := make(chan struct{})
		go func() { q.Push(2); close(done) }()
		if !blocked(done) {
			t.Fatal("Push into a full queue did not wait")
		}
		if got := q.Pop(); got != 1 {
			t.Fatalf("Pop() = %d; want 1", got)
		}
		<-done
		if got := q.Pop(); got != 2 {
			t.Fatalf("Pop() = %d; want 2", got)
		}
	})
	t.Run("capacity 0 is a rendezvous", func(t *testing.T) {
		q := New[string](0)
		done := make(chan struct{})
		go func() { q.Push("v"); close(done) }()
		if !blocked(done) {
			t.Fatal("Push into a capacity-0 queue returned before a Pop took the value")
		}
		if got := q.Pop(); got != "v" {
			t.Fatalf("Pop() = %q; want v", got)
		}
		<-done
	})
	t.Run("negative capacity", func(t *testing.T) {
		p := recovered(func() { New[int](-1) })
		if s, _ := p.(string); s != "queue.New: negative capacity -1" {
			t.Fatalf("New(-1) raised %#v; want the defect", p)
		}
	})
	t.Run("Push after Close", func(t *testing.T) {
		q := New[int](2)
		q.Close()
		if !interrupts(t, func() { q.Push(1) }) {
			t.Fatal("Push on a closed queue did not interrupt")
		}
		if !interrupts(t, func() { q.Pop() }) {
			t.Fatal("the rejected value entered the queue")
		}
	})
	t.Run("Pop drains, then interrupts", func(t *testing.T) {
		q := New[int](2)
		q.Push(1)
		q.Push(2)
		q.Close()
		q.Close() // idempotent
		if a, b := q.Pop(), q.Pop(); a != 1 || b != 2 {
			t.Fatalf("Pop, Pop = %d, %d; want 1, 2", a, b)
		}
		if !interrupts(t, func() { q.Pop() }) {
			t.Fatal("Pop on a closed, empty queue did not interrupt")
		}
	})
	t.Run("a waiting Push interrupts at Close", func(t *testing.T) {
		q := New[int](1)
		q.Push(1)
		var interrupted atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			interrupted.Store(recovered(func() { q.Push(2) }) == rt.Interrupted)
		}()
		if !blocked(done) {
			t.Fatal("Push did not wait")
		}
		q.Close()
		<-done
		if !interrupted.Load() {
			t.Fatal("the waiting Push did not interrupt")
		}
		var got []int
		for v := range q.Values() {
			got = append(got, v)
		}
		if fmt.Sprint(got) != "[1]" {
			t.Fatalf("drained %v; want [1]: the interrupted Push's value never enters", got)
		}
	})
	t.Run("a waiting Pop interrupts at Close", func(t *testing.T) {
		q := New[int](1)
		var interrupted atomic.Bool
		done := make(chan struct{})
		go func() {
			defer close(done)
			interrupted.Store(recovered(func() { q.Pop() }) == rt.Interrupted)
		}()
		if !blocked(done) {
			t.Fatal("Pop did not wait")
		}
		q.Close()
		<-done
		if !interrupted.Load() {
			t.Fatal("the waiting Pop did not interrupt")
		}
	})
	t.Run("Values drains and ends at Close", func(t *testing.T) {
		q := New[int](4)
		go func() {
			for i := range 100 {
				q.Push(i)
			}
			q.Close()
		}()
		n := 0
		for v := range q.Values() {
			if v != n {
				t.Fatalf("value %d; want %d", v, n)
			}
			n++
		}
		if n != 100 {
			t.Fatalf("%d values; want 100", n)
		}
	})
	t.Run("Values stops early", func(t *testing.T) {
		q := New[int](4)
		for i := 1; i <= 4; i++ {
			q.Push(i)
		}
		var got []int
		for v := range q.Values() {
			got = append(got, v)
			if len(got) == 2 {
				break
			}
		}
		if fmt.Sprint(got) != "[1 2]" || q.Pop() != 3 {
			t.Fatalf("took %v; want [1 2], then 3 still queued", got)
		}
	})
	t.Run("Close never waits", func(t *testing.T) {
		q := New[int](0)
		go func() { recovered(func() { q.Push(1) }) }()
		time.Sleep(time.Millisecond)
		q.Close()
	})
}

// TestQueueModel compares the queue with a model on random sequences of
// operations from one goroutine. An operation that would wait is skipped:
// the interleavings test covers waits.
func TestQueueModel(t *testing.T) {
	for seed := uint64(1); seed <= 300; seed++ {
		rng := rand.New(rand.NewPCG(seed, 1))
		capacity := rng.IntN(4)
		q := New[int](capacity)
		var model []int
		closed := false
		next := 0
		for step := range 40 {
			switch rng.IntN(5) {
			case 0, 1:
				if closed {
					if !interrupts(t, func() { q.Push(next) }) {
						t.Fatalf("seed %d step %d: Push on a closed queue did not interrupt", seed, step)
					}
				} else if len(model) < capacity {
					q.Push(next)
					model = append(model, next)
				}
				next++
			case 2, 3:
				switch {
				case len(model) > 0:
					if got := q.Pop(); got != model[0] {
						t.Fatalf("seed %d step %d: Pop() = %d; want %d", seed, step, got, model[0])
					}
					model = model[1:]
				case closed:
					if !interrupts(t, func() { q.Pop() }) {
						t.Fatalf("seed %d step %d: Pop on a closed, empty queue did not interrupt", seed, step)
					}
				}
			case 4:
				if rng.IntN(4) == 0 {
					q.Close()
					closed = true
				}
			}
		}
		q.Close()
		var rest []int
		for v := range q.Values() {
			rest = append(rest, v)
		}
		if fmt.Sprint(rest) != fmt.Sprint(append([]int{}, model...)) && !(len(rest) == 0 && len(model) == 0) {
			t.Fatalf("seed %d: drained %v; want %v", seed, rest, model)
		}
	}
}

// item is a value pushed by one producer: its producer and its number.
type item struct{ p, i int }

// TestQueueInterleavings is the generated interleaving test: for each seed,
// random capacity, producers, consumers (by Pop or by Values, some stopping
// early) and a Close at a random point run concurrently. Every value whose
// Push returned is taken exactly once; no value whose Push interrupted is
// ever taken; each consumer sees each producer's values in push order (FIFO),
// and the values left after every consumer stopped come after all of them.
func TestQueueInterleavings(t *testing.T) {
	base := runtime.NumGoroutine()
	seeds := uint64(300)
	if testing.Short() {
		seeds = 50
	}
	for seed := uint64(1); seed <= seeds; seed++ {
		done := make(chan struct{})
		go func() {
			defer close(done)
			interleave(t, seed)
		}()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			buf := make([]byte, 1<<16)
			t.Fatalf("seed %d did not end:\n%s", seed, buf[:runtime.Stack(buf, true)])
		}
		if t.Failed() {
			return
		}
	}
	waitForGoroutines(t, base)
}

func interleave(t *testing.T, seed uint64) {
	rng := rand.New(rand.NewPCG(seed, 2))
	capacity := rng.IntN(4)
	producers, consumers := 1+rng.IntN(3), 1+rng.IntN(3)
	perProducer := rng.IntN(25)
	closeAfter := int64(rng.IntN(producers*perProducer + 1))
	q := New[item](capacity)

	var (
		mu               sync.Mutex
		accepted         = map[item]bool{}
		rejected         = map[item]bool{}
		taken            = make([][]item, consumers)
		pops             atomic.Int64
		producersDone    atomic.Int64
		consumersDone    atomic.Int64
		wg               sync.WaitGroup
		yieldProducer    = make([]bool, producers)
		consumerByValues = make([]bool, consumers)
		consumerLimit    = make([]int, consumers)
	)
	for p := range yieldProducer {
		yieldProducer[p] = rng.IntN(2) == 0
	}
	for c := range consumers {
		consumerByValues[c] = rng.IntN(2) == 0
		consumerLimit[c] = -1
		if rng.IntN(3) == 0 {
			consumerLimit[c] = rng.IntN(perProducer + 1)
		}
	}

	for p := range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer producersDone.Add(1)
			for i := range perProducer {
				it := item{p, i}
				if interruptsQuiet(func() { q.Push(it) }) {
					mu.Lock()
					rejected[it] = true
					mu.Unlock()
					return
				}
				mu.Lock()
				accepted[it] = true
				mu.Unlock()
				if yieldProducer[p] {
					runtime.Gosched()
				}
			}
		}()
	}
	for c := range consumers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer consumersDone.Add(1)
			keep := func(it item) bool {
				taken[c] = append(taken[c], it)
				pops.Add(1)
				return consumerLimit[c] < 0 || len(taken[c]) < consumerLimit[c]
			}
			if consumerLimit[c] == 0 {
				return
			}
			if consumerByValues[c] {
				for it := range q.Values() {
					if !keep(it) {
						return
					}
				}
				return
			}
			for {
				var it item
				if interruptsQuiet(func() { it = q.Pop() }) {
					return
				}
				if !keep(it) {
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for pops.Load() < closeAfter && producersDone.Load() < int64(producers) && consumersDone.Load() < int64(consumers) {
			time.Sleep(20 * time.Microsecond)
		}
		q.Close()
	}()
	wg.Wait()

	var rest []item
	for it := range q.Values() {
		rest = append(rest, it)
	}

	seen := map[item]int{}
	for c := range taken {
		last := map[int]int{}
		for _, it := range taken[c] {
			seen[it]++
			if l, ok := last[it.p]; ok && it.i <= l {
				t.Errorf("seed %d: consumer %d took %v after %d of the same producer (FIFO)", seed, c, it, l)
			}
			last[it.p] = it.i
		}
	}
	maxTaken := map[int]int{}
	for it := range seen {
		if m, ok := maxTaken[it.p]; !ok || it.i > m {
			maxTaken[it.p] = it.i
		}
	}
	lastRest := map[int]int{}
	for _, it := range rest {
		seen[it]++
		if m, ok := maxTaken[it.p]; ok && it.i <= m {
			t.Errorf("seed %d: %v was left in the queue behind a later value of its producer (FIFO)", seed, it)
		}
		if l, ok := lastRest[it.p]; ok && it.i <= l {
			t.Errorf("seed %d: the rest has %v after %d (FIFO)", seed, it, l)
		}
		lastRest[it.p] = it.i
	}
	for it, n := range seen {
		switch {
		case n > 1:
			t.Errorf("seed %d: %v was taken %d times", seed, it, n)
		case rejected[it]:
			t.Errorf("seed %d: %v was taken, but its Push interrupted", seed, it)
		case !accepted[it]:
			t.Errorf("seed %d: %v was taken, but no Push of it returned", seed, it)
		}
	}
	var lost []string
	for it := range accepted {
		if seen[it] == 0 {
			lost = append(lost, fmt.Sprint(it))
		}
	}
	if len(lost) > 0 {
		sort.Strings(lost)
		t.Errorf("seed %d (capacity %d, %d producers, %d consumers): delivered but never taken: %s", seed, capacity, producers, consumers, strings.Join(lost, " "))
	}
}

// interruptsQuiet is interrupts for another goroutine: it re-raises any
// other panic.
func interruptsQuiet(f func()) bool {
	p := recovered(f)
	if p != nil && p != rt.Interrupted {
		panic(p)
	}
	return p == rt.Interrupted
}
