package compiler

import "testing"

func TestTestContractsAndLiveCapabilities(t *testing.T) {
	for _, source := range []string{
		`fn test_wrong() -> void {void}`,
		`effect fn test_wrong(x:string) -> void {void}`,
		`effect fn test_wrong() -> string {"wrong"}`,
		`effect fn test_wrong() -> void uses {Console} {run Console.log("wrong")}`,
		`effect fn helper() -> void {void}`,
	} {
		r := Compile(source)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		if _, err := r.Tests(); err == nil {
			t.Fatal("admitted invalid test", source)
		}
	}
	source := `effect fn test_ok() -> void raises {AssertionFailed} uses {Assert} {run Assert.check(true,"ok")}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if _, err := r.Tests(); err != nil {
		t.Fatal(err)
	}
	if err := r.TestMode(false); err != nil {
		t.Fatal(err)
	}
	live := Compile(`effect fn test_live() -> void {run Clock.sleep(1).provide<Clock>(LiveClock)}`)
	if !live.Checked {
		t.Fatal(live.Diagnostics)
	}
	if live.TestMode(false) == nil || live.TestMode(true) != nil {
		t.Fatal("live host check")
	}
	for _, test := range []struct {
		name   string
		source string
	}{
		{
			name:   "clock layer",
			source: `layer LiveClockLayer { Clock = LiveClock } effect fn test_layer() -> void { void }`,
		},
		{
			name:   "scheduler layer",
			source: `layer LiveSchedulerLayer { Scheduler = LiveScheduler } effect fn test_layer() -> void { void }`,
		},
		{
			name:   "environment layer",
			source: `layer LiveEnvLayer { Env = LiveEnv } effect fn test_layer() -> void { void }`,
		},
		{
			name: "retained layer selections",
			source: `
layer Hidden { Clock = LiveClock }
layer Merged { merge Hidden }
layer Fixture { Clock = TestClock }
layer Replaced { merge Fixture; replace Clock = LiveClock }
layer Unused { Scheduler = LiveScheduler; Env = LiveEnv }
effect fn test_layer() -> void { void }`,
		},
		{
			name:   "test providers",
			source: `layer Fixtures { Clock = TestClock; Scheduler = TestScheduler } effect fn test_layer() -> void { void }`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				r := CompileFor(test.source, target)
				if !r.Checked {
					t.Fatalf("%s target: %+v", target, r.Diagnostics)
				}
				wantLive := test.name != "test providers"
				if gotLive := r.TestMode(false) != nil; gotLive != wantLive {
					t.Fatalf("%s target: TestMode(false) live=%v, want %v", target, gotLive, wantLive)
				}
				if err := r.TestMode(true); err != nil {
					t.Fatalf("%s target explicit live mode: %v", target, err)
				}
			}
		})
	}
}
