package v1

import (
	"errors"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"ergo.local/prototype/runtime/rt"
)

const mainVariable = "ERGO_RUNTIME_V1_MAIN"

// TestMainHelper is the child of TestMain_; it does nothing in a normal run.
func TestMainHelper(t *testing.T) {
	mode := os.Getenv(mainVariable)
	if mode == "" {
		t.Skip("the child of TestMainExit")
	}
	Main(func(root *rt.Task) error {
		switch mode {
		case "ok":
			return nil
		case "failure":
			return bad{"main"}
		case "defect":
			return frame(root, func(s *Scope) error {
				_, err := Go(s, "worker at w.ergo:7:3", func(*rt.Task) (int, error) { return explode(), nil }).Wait()
				return err
			})
		case "interrupted":
			root.Interrupt()
			sleepUntilStopped(root)
		case "closed":
			q := New[int](1)
			q.Close()
			return q.Push(1)
		}
		return nil
	})
}

// TestMainExit: the program's exit status and report (Exit causes 6), from
// a real process: 0 and nothing on success; 1 and the failure's text; 2 and
// the defect with its task chain and the stack that names the panicking
// function and its line; 130 and "interrupted" for an interruption alone.
func TestMainExit(t *testing.T) {
	cases := []struct {
		mode   string
		status int
		stderr string // a regular expression
	}{
		{"ok", 0, `^$`},
		{"failure", 1, `^error: bad main\n$`},
		{"defect", 2, `^panic: runtime error: index out of range \[3\] with length 0\n\ttask #\d+ worker at w\.ergo:7:3\n\ttask #\d+ main\n\n(?s:.*)v1\.explode\(\)\n\t[^\n]*task_test\.go:\d+`},
		{"interrupted", 130, `^interrupted\n$`},
		{"closed", 1, `^error: queue\.Closed\n$`},
	}
	for _, c := range cases {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(), mainVariable+"="+c.mode)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		status := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			status = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", c.mode, err)
		}
		if status != c.status {
			t.Errorf("%s: status %d; want %d\nstderr: %s", c.mode, status, c.status, stderr.String())
		}
		if !regexp.MustCompile(c.stderr).MatchString(stderr.String()) {
			t.Errorf("%s: stderr does not match %q:\n%s", c.mode, c.stderr, stderr.String())
		}
	}
}
