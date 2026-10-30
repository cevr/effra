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
	Push(int) error
	Pop() (int, error)
	Close()
	Values() iter.Seq[int]
} = (*Queue[int])(nil)

// closed reports whether err is the queue.Closed failure, and fails the
// test on any other error.
func closed(t *testing.T, err error) bool {
	t.Helper()
	if err != nil && err != (Closed{}) {
		t.Fatalf("failed with %#v; want queue.Closed or nothing", err)
	}
	return err == (Closed{})
}

// push pushes onto a queue that must take the value.
func push[T any](t *testing.T, q *Queue[T], v T) {
	t.Helper()
	if err := q.Push(v); err != nil {
		t.Fatalf("Push(%v) failed with %v", v, err)
	}
}

// pop pops from a queue that must hold a value.
func pop[T any](t *testing.T, q *Queue[T]) T {
	t.Helper()
	v, err := q.Pop()
	if err != nil {
		t.Fatalf("Pop failed with %v", err)
	}
	return v
}

// TestQueueContract walks each rule of the queue contract (runtime.md,
// Queues).
func TestQueueContract(t *testing.T) {
	t.Run("FIFO", func(t *testing.T) {
		q := New[int](3)
		push(t, q, 1)
		push(t, q, 2)
		push(t, q, 3)
		for want := 1; want <= 3; want++ {
			if got := pop(t, q); got != want {
				t.Fatalf("Pop() = %d; want %d", got, want)
			}
		}
	})
	t.Run("backpressure", func(t *testing.T) {
		q := New[int](1)
		push(t, q, 1)
		done := make(chan error, 1)
		go func() { done <- q.Push(2) }()
		parked(t, q, 1) // Push into a full queue waits
		if got := pop(t, q); got != 1 {
			t.Fatalf("Pop() = %d; want 1", got)
		}
		if err := result(t, done); err != nil {
			t.Fatalf("the waiting Push failed with %v", err)
		}
		if got := pop(t, q); got != 2 {
			t.Fatalf("Pop() = %d; want 2", got)
		}
	})
	t.Run("capacity 0 is a rendezvous", func(t *testing.T) {
		q := New[string](0)
		done := make(chan error, 1)
		go func() { done <- q.Push("v") }()
		parked(t, q, 1) // the Push waits for the Pop that takes its value
		if got := pop(t, q); got != "v" {
			t.Fatalf("Pop() = %q; want v", got)
		}
		if err := result(t, done); err != nil {
			t.Fatalf("the Push failed with %v", err)
		}
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
		if !closed(t, q.Push(1)) {
			t.Fatal("Push on a closed queue did not fail with queue.Closed")
		}
		if _, err := q.Pop(); !closed(t, err) {
			t.Fatal("the rejected value entered the queue")
		}
	})
	t.Run("Pop drains, then fails", func(t *testing.T) {
		q := New[int](2)
		push(t, q, 1)
		push(t, q, 2)
		q.Close()
		q.Close() // idempotent
		if a, b := pop(t, q), pop(t, q); a != 1 || b != 2 {
			t.Fatalf("Pop, Pop = %d, %d; want 1, 2", a, b)
		}
		if _, err := q.Pop(); !closed(t, err) {
			t.Fatal("Pop on a closed, empty queue did not fail with queue.Closed")
		}
		if (Closed{}).Error() != "queue.Closed" {
			t.Fatalf("the failure's text is %q", Closed{}.Error())
		}
	})
	t.Run("a waiting Push fails at Close", func(t *testing.T) {
		q := New[int](1)
		push(t, q, 1)
		done := make(chan error, 1)
		go func() { done <- q.Push(2) }()
		parked(t, q, 1)
		q.Close()
		if err := result(t, done); err != (Closed{}) {
			t.Fatalf("the waiting Push gave %v; want queue.Closed", err)
		}
		var got []int
		for v := range q.Values() {
			got = append(got, v)
		}
		if fmt.Sprint(got) != "[1]" {
			t.Fatalf("drained %v; want [1]: the failed Push's value never enters", got)
		}
	})
	t.Run("a waiting Pop fails at Close", func(t *testing.T) {
		q := New[int](1)
		done := make(chan error, 1)
		go func() {
			_, err := q.Pop()
			done <- err
		}()
		parked(t, q, 1)
		q.Close()
		if err := result(t, done); err != (Closed{}) {
			t.Fatalf("the waiting Pop gave %v; want queue.Closed", err)
		}
	})
	t.Run("Values drains and ends at Close", func(t *testing.T) {
		q := New[int](4)
		producer := make(chan struct{})
		defer func() { <-producer }()
		go func() {
			defer close(producer)
			for i := range 100 {
				_ = q.Push(i)
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
			push(t, q, i)
		}
		var got []int
		for v := range q.Values() {
			got = append(got, v)
			if len(got) == 2 {
				break
			}
		}
		if fmt.Sprint(got) != "[1 2]" || pop(t, q) != 3 {
			t.Fatalf("took %v; want [1 2], then 3 still queued", got)
		}
	})
	t.Run("Close never waits", func(t *testing.T) {
		q := New[int](0)
		done := make(chan error, 1)
		go func() { done <- q.Push(1) }()
		parked(t, q, 1)
		q.Close() // returns while a Push waits: it never waits itself
		if err := result(t, done); err != (Closed{}) {
			t.Fatalf("the waiting Push gave %v; want queue.Closed", err)
		}
	})
	t.Run("a closed queue is a typed failure, not an interruption", func(t *testing.T) {
		// The review's case (stage 3a round 1, m1): a child that pops a
		// closed, empty queue fails with queue.Closed; its owner receives
		// it from Wait as a typed failure, and main exits 1, not 130.
		q := New[int](1)
		q.Close()
		err := runMain(func(root *rt.Task) error {
			return frame(root, func(s *Scope) error {
				c := Go(s, "consumer", func(*rt.Task) (int, error) { return q.Pop() })
				_, err := c.Wait()
				return err
			})
		})
		var out strings.Builder
		if code := report(&out, err); err != (Closed{}) || code != 1 || out.String() != "error: queue.Closed\n" {
			t.Errorf("exit %v, status %d, report %q; want queue.Closed, 1", err, code, out.String())
		}
	})
}

// parked waits until n operations wait in q. It reads the count under q's
// mutex, so each of them has released the lock and holds the wake channel
// that the next change closes: the change it waits for then reaches the
// wait, not the fast path. The deadline only fails the setup.
func parked[T any](t *testing.T, q *Queue[T], n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		q.mu.Lock()
		k := q.parked
		q.mu.Unlock()
		if k >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup: %d operations wait in the queue after 10s; want %d", k, n)
		}
		time.Sleep(50 * time.Microsecond)
	}
}

// result receives an operation's result; the deadline fails a wait that
// nothing woke.
func result(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("the operation did not return after 10s: nothing woke it")
		return nil
	}
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
		isClosed := false
		next := 0
		for step := range 40 {
			switch rng.IntN(5) {
			case 0, 1:
				if isClosed {
					if !closed(t, q.Push(next)) {
						t.Fatalf("seed %d step %d: Push on a closed queue did not fail with queue.Closed", seed, step)
					}
				} else if len(model) < capacity {
					push(t, q, next)
					model = append(model, next)
				}
				next++
			case 2, 3:
				switch {
				case len(model) > 0:
					if got := pop(t, q); got != model[0] {
						t.Fatalf("seed %d step %d: Pop() = %d; want %d", seed, step, got, model[0])
					}
					model = model[1:]
				case isClosed:
					if _, err := q.Pop(); !closed(t, err) {
						t.Fatalf("seed %d step %d: Pop on a closed, empty queue did not fail with queue.Closed", seed, step)
					}
				}
			case 4:
				if rng.IntN(4) == 0 {
					q.Close()
					isClosed = true
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
// Push returned is taken exactly once; no value whose Push failed is
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

// TestQueueStress is the stage 3a round 1 review's stress test: more
// producers and consumers and more values than the interleavings test, with
// a Close at a random moment. Every value whose Push returned nil is taken
// exactly once, by a consumer or the final drain; no value whose Push failed
// is taken; each consumer sees each producer's values in order.
func TestQueueStress(t *testing.T) {
	base := runtime.NumGoroutine()
	seeds := uint64(60)
	if testing.Short() {
		seeds = 10
	}
	for seed := uint64(1); seed <= seeds; seed++ {
		rng := rand.New(rand.NewPCG(seed, 9))
		capacity := []int{0, 1, 2, 8}[rng.IntN(4)]
		producers, consumers, per := 2+rng.IntN(5), 2+rng.IntN(5), 50+rng.IntN(300)
		q := New[item](capacity)
		var mu sync.Mutex
		accepted, rejected := map[item]bool{}, map[item]bool{}
		taken := make([][]item, consumers)
		var wg sync.WaitGroup
		for p := range producers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range per {
					v := item{p, i}
					if closedQuiet(q.Push(v)) {
						mu.Lock()
						rejected[v] = true
						mu.Unlock()
						return
					}
					mu.Lock()
					accepted[v] = true
					mu.Unlock()
				}
			}()
		}
		for c := range consumers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if c%2 == 0 {
					for v := range q.Values() {
						taken[c] = append(taken[c], v)
					}
					return
				}
				for {
					v, err := q.Pop()
					if closedQuiet(err) {
						return
					}
					taken[c] = append(taken[c], v)
				}
			}()
		}
		time.Sleep(time.Duration(rng.IntN(3000)) * time.Microsecond)
		q.Close()
		wg.Wait()
		seen := map[item]int{}
		for c, list := range taken {
			last := map[int]int{}
			for _, v := range list {
				seen[v]++
				if l, ok := last[v.p]; ok && v.i <= l {
					t.Fatalf("seed %d: consumer %d took %v after %d (FIFO)", seed, c, v, l)
				}
				last[v.p] = v.i
			}
		}
		for v := range q.Values() {
			seen[v]++
		}
		for v, n := range seen {
			switch {
			case n != 1:
				t.Fatalf("seed %d: %v taken %d times", seed, v, n)
			case rejected[v]:
				t.Fatalf("seed %d: %v was taken, but its Push failed", seed, v)
			case !accepted[v]:
				t.Fatalf("seed %d: %v was taken, but no Push of it returned", seed, v)
			}
		}
		for v := range accepted {
			if seen[v] != 1 {
				t.Fatalf("seed %d: delivered %v was taken %d times", seed, v, seen[v])
			}
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
				if closedQuiet(q.Push(it)) {
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
				it, err := q.Pop()
				if closedQuiet(err) {
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
			t.Errorf("seed %d: %v was taken, but its Push failed with queue.Closed", seed, it)
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

// closedQuiet is closed for another goroutine, where t.Fatalf may not run:
// any error other than queue.Closed panics.
func closedQuiet(err error) bool {
	if err != nil && err != (Closed{}) {
		panic(fmt.Sprintf("queue failed with %#v; want queue.Closed or nothing", err))
	}
	return err != nil
}
