package effra

import (
	"context"
	"errors"
	"testing"
)

func TestInvalidLatchHandlesHaveTheSameBoundaryOnBothDrivers(t *testing.T) {
	for name, latch := range map[string]*Latch{"nil": nil, "zero": new(Latch)} {
		t.Run(name+"/direct", func(t *testing.T) {
			if latch.IsSignaled() || latch.Signal() {
				t.Fatal("invalid latch reported completion")
			}
			if err := latch.Await(context.Background()); !errors.Is(err, errInvalidLatch) {
				t.Fatalf("invalid latch await: %v", err)
			}
		})
		for operation, effect := range map[string]Effect[Unit]{"await": AwaitLatch(latch), "signal": SignalLatch(latch)} {
			for _, virtual := range []bool{false, true} {
				driver := "live"
				if virtual {
					driver = "virtual"
				}
				t.Run(name+"/"+operation+"/"+driver, func(t *testing.T) {
					var out Exit[Unit]
					if virtual {
						out = RunContextWithScheduler(context.Background(), NewTestScheduler(), effect)
					} else {
						out = Run(effect)
					}
					if len(out.Cause()) != 1 || out.Interrupted || !errors.Is(out.Defect, errInvalidLatch) {
						t.Fatalf("invalid handle must be one defect, got %+v", out)
					}
				})
			}
		}
	}
}
