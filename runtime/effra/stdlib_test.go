package effra

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestGoInteropDefersCallsAndPreservesPartialResults(t *testing.T) {
	calls := 0
	buffer := make([]byte, 8)
	program := FromGo(func(context.Context) (int, error) {
		calls++
		return io.ReadFull(bytes.NewBufferString("abc"), buffer)
	})
	if calls != 0 {
		t.Fatal("foreign call ran during construction")
	}
	raw := Run(program)
	if raw.IsFailure() || calls != 1 || raw.Value.Value != 3 || !errors.Is(raw.Value.Err, io.ErrUnexpectedEOF) || string(buffer[:3]) != "abc" {
		t.Fatalf("native partial result lost: %+v", raw)
	}
	adapted := Run(OrFail(program))
	if adapted.Failure == nil || adapted.Failure.Tag != "GoError" {
		t.Fatalf("missing explicit failure: %+v", adapted)
	}
	payload := adapted.Failure.Payload.(GoError)
	if payload.Partial != 3 || !errors.Is(payload.Err, io.ErrUnexpectedEOF) || calls != 2 {
		t.Fatalf("partial error payload lost: %+v", payload)
	}
}

func TestGoInteropForwardsCancellationAndContainsPanics(t *testing.T) {
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Exit[GoResult[string]], 1)
	go func() {
		done <- RunContext(ctx, FromGo(func(ctx context.Context) (string, error) {
			close(started)
			<-ctx.Done()
			return "partial", ctx.Err()
		}))
	}()
	wait(t, started)
	cancel()
	if !wait(t, done).Interrupted {
		t.Fatal("foreign call did not acknowledge cancellation")
	}
	panicExit := Run(FromGo(func(context.Context) (string, error) { panic("SDK panic") }))
	if panicExit.Defect == nil || panicExit.Failure != nil {
		t.Fatalf("foreign panic became a fabricated typed failure: %+v", panicExit)
	}
}

func TestManagedFileClosesWithItsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input.txt")
	if err := os.WriteFile(path, []byte("managed data"), 0600); err != nil {
		t.Fatal(err)
	}
	var escaped *File
	read := Run(Scoped(func(fc *FiberContext) Exit[string] {
		opened := Invoke(fc, OpenRead(path))
		if opened.IsFailure() {
			return Propagate[string](opened)
		}
		escaped = opened.Value
		snapshot := fc.Scope().Snapshot()
		if snapshot.ResourceCount != 1 || len(snapshot.Resources) != 1 {
			t.Errorf("missing owned resource: %+v", snapshot)
		}
		return Invoke(fc, ReadText(escaped))
	}))
	if read.IsFailure() || read.Value != "managed data" {
		t.Fatalf("file read: %+v", read)
	}
	closed := Run(ReadText(escaped))
	if closed.Failure == nil || closed.Failure.Tag != "IoError" {
		t.Fatalf("closed handle remained usable: %+v", closed)
	}
}

func TestScopeInspectionTracksOwnedChildCompletion(t *testing.T) {
	started, releaseStarted, finishRelease := make(chan struct{}), make(chan struct{}), make(chan struct{})
	out := Run(func(fc *FiberContext) Exit[Unit] {
		child := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
			resource := Invoke(child, AcquireRelease("child", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error {
				close(releaseStarted)
				<-finishRelease
				return nil
			}))
			if resource.IsFailure() {
				return resource
			}
			close(started)
			<-child.Context().Done()
			return Interrupt[Unit](child.Context().Err())
		}))
		if child.IsFailure() {
			return Propagate[Unit](child)
		}
		wait(t, started)
		child.Value.Cancel()
		wait(t, releaseStarted)
		snapshot := fc.Scope().Snapshot()
		if len(snapshot.ChildStates) != 1 || snapshot.ChildStates[0].State != "Cancelling" {
			t.Errorf("cleanup was reported complete: %+v", snapshot)
		}
		close(finishRelease)
		interrupted := Invoke(fc, child.Value.Interrupt())
		if interrupted.IsFailure() {
			return interrupted
		}
		if fc.Scope().Snapshot().ChildStates[0].State != "Done" {
			t.Error("completed cleanup was not reported")
		}
		return Succeed(Unit{})
	})
	if out.IsFailure() {
		t.Fatalf("inspection scenario failed: %+v", out)
	}
}
