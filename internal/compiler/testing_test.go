package compiler

import "testing"

func TestTestContractsAndLiveCapabilities(t *testing.T) {
	for _, source := range []string{
		`fn test_wrong() -> () {()}`,
		`effect fn test_wrong(x:string) -> () {()}`,
		`effect fn test_wrong() -> string {"wrong"}`,
		`effect fn test_wrong() -> () uses {Console} {run Console.log("wrong")}`,
		`effect fn helper() -> () {()}`,
	} {
		r := Compile(source)
		if !r.Checked {
			t.Fatal(r.Diagnostics)
		}
		if _, err := r.Tests(); err == nil {
			t.Fatal("admitted invalid test", source)
		}
	}
	source := `effect fn test_ok() -> () raises {AssertionFailed} uses {Assert} {run Assert.check(true,"ok")}`
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
	live := Compile(`effect fn test_live() -> () {run Clock.sleep(1).provide<Clock>(LiveClock)}`)
	if !live.Checked {
		t.Fatal(live.Diagnostics)
	}
	if live.TestMode(false) == nil || live.TestMode(true) != nil {
		t.Fatal("live host check")
	}
}
