package rt

import (
  "sync"
  "testing"
  "time"
)

func TestRound2ConcurrentStopAndChild(t *testing.T) {
  for iteration := 0; iteration < 300; iteration++ {
    root := Root()
    parent := root.Child()
    start := make(chan struct{})
    children := make([]*Task, 32)
    var workers sync.WaitGroup
    for i := range children {
      workers.Add(1)
      go func(i int) {
        defer workers.Done()
        <-start
        children[i] = parent.Child()
      }(i)
    }
    for i := 0; i < 4; i++ {
      workers.Add(1)
      go func() { defer workers.Done(); <-start; root.Stop() }()
    }
    close(start)
    workers.Wait()
    for _, child := range children {
      if !child.Stopped() { t.Fatal("child admitted concurrently with Stop remained live") }
      child.release()
    }
    parent.release()
  }
}

func TestRound2ConcurrentStopAndRelease(t *testing.T) {
  for iteration := 0; iteration < 150; iteration++ {
    root := Root()
    clock := manual{make(chan time.Time)}
    ready := make(chan struct{}, 32)
    finish := make(chan struct{})
    errs := make(chan error, 32)
    var workers sync.WaitGroup
    for i := 0; i < 32; i++ {
      workers.Add(1)
      go func() {
        defer workers.Done()
        _, err := After(clock, root, time.Hour, func(*Task) (int, error) {
          ready <- struct{}{}
          <-finish
          return 1, nil
        })
        errs <- err
      }()
    }
    for i := 0; i < 32; i++ { <-ready }
    stopped := make(chan struct{})
    go func() { root.Stop(); close(stopped) }()
    close(finish)
    workers.Wait()
    <-stopped
    close(errs)
    for err := range errs {
      if err != nil && err != Interrupted { t.Fatalf("unexpected error %v", err) }
    }
    root.mu.Lock()
    retained := len(root.children)
    root.mu.Unlock()
    if retained != 0 { t.Fatalf("retained %d completed children", retained) }
  }
}

func TestRound2SuccessfulAfterReleasesChildren(t *testing.T) {
  root := Root()
  clock := manual{make(chan time.Time)}
  for i := 0; i < 200; i++ {
    value, err := After(clock, root, time.Hour, func(*Task) (int, error) { return 1, nil })
    if value != 1 || err != nil { t.Fatalf("got %d, %v", value, err) }
  }
  root.mu.Lock()
  retained := len(root.children)
  root.mu.Unlock()
  if retained != 0 { t.Fatalf("retained %d completed children", retained) }
}
