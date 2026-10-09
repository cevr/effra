// Command managed is the idiomatic Go control for fixtures/managed.ef. It
// keeps the same contract with the standard library only: children are owned
// by the program's scope, an interrupted child is awaited before the program
// continues, the deadline cancels and awaits its work, a child's failure is
// recovered by type, and a termination signal cancels the whole program.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var errBad = errors.New("bad")

// scope owns child goroutines. Closing it cancels every child and waits for
// all of them, so no work outlives the scope.
type scope struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func openScope(parent context.Context) *scope {
	ctx, cancel := context.WithCancel(parent)
	return &scope{ctx: ctx, cancel: cancel}
}

func (s *scope) close() {
	s.cancel()
	s.wg.Wait()
}

type child[T any] struct {
	cancel context.CancelFunc
	done   chan struct{}
	value  T
	err    error
}

func fork[T any](s *scope, work func(context.Context) (T, error)) *child[T] {
	ctx, cancel := context.WithCancel(s.ctx)
	c := &child[T]{cancel: cancel, done: make(chan struct{})}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(c.done)
		defer cancel()
		c.value, c.err = work(ctx)
	}()
	return c
}

func (c *child[T]) join(ctx context.Context) (T, error) {
	select {
	case <-c.done:
		return c.value, c.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}

// interrupt requests cancellation and waits until the child has finished.
func (c *child[T]) interrupt() {
	c.cancel()
	<-c.done
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func immediate(context.Context) (string, error) { return "child joined", nil }

func pending(ctx context.Context) (string, error) {
	if err := sleep(ctx, 10*time.Second); err != nil {
		return "", err
	}
	return "late", nil
}

func bad(context.Context) (string, error) { return "", errBad }

func program(ctx context.Context) (string, error) {
	s := openScope(ctx)
	defer s.close()
	joined, err := fork(s, immediate).join(s.ctx)
	if err != nil {
		return "", err
	}
	fork(s, pending).interrupt()
	limited, stop := context.WithTimeout(s.ctx, time.Millisecond)
	deadline, err := pending(limited)
	stop()
	if errors.Is(err, context.DeadlineExceeded) {
		deadline = "timed out"
	} else if err != nil {
		return "", err
	}
	recovered, err := fork(s, bad).join(s.ctx)
	if errors.Is(err, errBad) {
		recovered = "recovered"
	} else if err != nil {
		return "", err
	}
	return joined + "; interrupted; " + deadline + "; " + recovered, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	result, err := program(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(result)
}
