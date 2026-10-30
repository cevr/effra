package v1

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestText: the texts of basic values inside an error type's text, as Go's
// %v writes them, from Go's basic types only.
func TestText(t *testing.T) {
	cases := []struct{ got, want string }{
		{TextBool(true), "true"},
		{TextBool(false), "false"},
		{TextInt(-3), "-3"},
		{TextInt(math.MinInt64), "-9223372036854775808"},
		{TextUint(math.MaxUint64), "18446744073709551615"},
		{TextFloat(float64(float32(0.1)), 32), "0.1"},
		{TextFloat(0.1, 64), "0.1"},
		{TextFloat(1e21, 64), "1e+21"},
		{TextDuration(1500 * time.Millisecond), "1.5s"},
		{TextList(nil), "[]"},
		{TextList([]string{"a", "b"}), "[a b]"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("case %d: %q, want %q", i, c.got, c.want)
		}
	}
}

// TestSleep: Sleep suspends the caller for at least d.
func TestSleep(t *testing.T) {
	start := time.Now()
	Sleep(20 * time.Millisecond)
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("Sleep returned after %v", elapsed)
	}
}

const helperVariable = "ERGO_RUNTIME_V1_HELPER"

// TestHelperProcess is the child of TestConsoleAndFail; it does nothing in
// a normal run.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperVariable) != "1" {
		t.Skip("the child of TestConsoleAndFail")
	}
	Console{}.Println("hello")
	Fail(errors.New("Bad{at:x}"))
}

// TestConsoleAndFail: Println writes its line and a line feed to standard
// output; Fail writes "error: " and the failure to standard error and exits
// with status 1.
func TestConsoleAndFail(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), helperVariable+"=1")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("the child ended with %v, want exit status 1\nstderr: %s", err, stderr.String())
	}
	if stdout.String() != "hello\n" {
		t.Errorf("stdout %q, want %q", stdout.String(), "hello\n")
	}
	if stderr.String() != "error: Bad{at:x}\n" {
		t.Errorf("stderr %q, want %q", stderr.String(), "error: Bad{at:x}\n")
	}
}
