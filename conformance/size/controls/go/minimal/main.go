// Command minimal is the idiomatic Go control for fixtures/minimal.ef with
// the same entry contract: a termination signal cancels the program, the
// program runs inside an owning scope that cancels and awaits anything it
// starts, a cancelled program fails instead of reporting a result, and the
// result is printed on success.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

// scope owns the work a program starts. Closing it cancels that work and
// waits for all of it.
type scope struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func runScoped(parent context.Context, program func(*scope) (string, error)) (string, error) {
	ctx, cancel := context.WithCancel(parent)
	s := &scope{ctx: ctx, cancel: cancel}
	defer func() {
		s.cancel()
		s.wg.Wait()
	}()
	return program(s)
}

func program(s *scope) (string, error) {
	if err := s.ctx.Err(); err != nil {
		return "", err
	}
	return "minimal", nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	result, err := runScoped(ctx, program)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(result)
}
