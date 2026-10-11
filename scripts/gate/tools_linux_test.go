//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// What a tool query leaves running, on Linux, where /proc shows whether a
// recorded process still runs and setsid(1) exists. A query runs in its own
// process group, killed whole on timeout, on interrupt and after the tool
// answers; only a descendant that leaves the group is merely cut off, by
// WaitDelay.

// TestToolQueryLeavesNothingInItsGroup runs tools that hang after forking a
// pipe holder, that hang after forking a holder which leaves the group, and
// that answer after forking a child; each fixture records the pids it forks.
// Every recorded process that stayed in the group is gone afterwards.
func TestToolQueryLeavesNothingInItsGroup(t *testing.T) {
	pids := filepath.Join(t.TempDir(), "pids")
	escaped := filepath.Join(t.TempDir(), "escaped")
	killRecorded(t, pids, escaped)
	cases := map[string]struct {
		body string
		hung bool
	}{
		"forks a holder and hangs":                       {"sleep 30 &\necho $! >> " + pids + "\nwait\n", true},
		"forks a holder that leaves the group and hangs": {"setsid sh -c 'echo $$ >> " + escaped + "; exec sleep 30' &\nsleep 30 &\necho $! >> " + pids + "\nwait\n", true},
		"answers after forking a child":                  {"sleep 30 >/dev/null 2>&1 &\necho $! >> " + pids + "\necho v1.0.0\n", false},
	}
	if _, err := exec.LookPath("setsid"); err != nil {
		delete(cases, "forks a holder that leaves the group and hangs")
	}
	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			shortQueryTimeout(t)
			bin := t.TempDir()
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			write(t, bin, "package.json", `{"name": "probe"}`)
			executable(t, bin, "probe-tool", "#!/bin/sh\n"+fixture.body)
			if fixture.hung {
				assertHungToolUnlocated(t)
				return
			}
			if first := fingerprint(t, "probe-tool"); !strings.HasPrefix(first, "v1.0.0") || strings.Contains(first, "unlocated") {
				t.Fatalf("a tool that answered has no located fingerprint: %q", first)
			}
		})
	}
	assertGone(t, recordedPids(t, pids))
}

// TestInterruptKillsRunningQueries stops a gate whose tool query hangs with
// each signal a terminal or a supervisor sends: Ctrl-C, kill, a hangup and
// Ctrl-\. A terminal signals its foreground process group, which holds the
// gate but not the query's own group. The gate kills the query's group and
// then ends by the signal's default effect: death by that signal, or for
// SIGQUIT, Go's goroutine dump and exit status 2.
func TestInterruptKillsRunningQueries(t *testing.T) {
	if os.Getenv("GATE_INTERRUPT_HELPER") != "" {
		t.Skip("the helper runs as a subprocess of this test")
	}
	for _, stop := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT} {
		t.Run(stop.String(), func(t *testing.T) {
			pids := filepath.Join(t.TempDir(), "pids")
			killRecorded(t, pids)
			bin := t.TempDir()
			write(t, bin, "package.json", `{"name": "probe"}`)
			executable(t, bin, "probe-tool", "#!/bin/sh\necho $$ >> "+pids+"\nexec sleep 30\n")
			helper := exec.Command(os.Args[0], "-test.run=^TestInterruptHelper$", "-test.count=1")
			helper.Env = append(os.Environ(), "GATE_INTERRUPT_HELPER="+t.TempDir(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			var output bytes.Buffer
			helper.Stdout, helper.Stderr = &output, &output
			if err := helper.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- helper.Wait() }()
			deadline := time.Now().Add(5 * time.Second)
			for len(recordedPids(t, pids)) == 0 {
				if time.Now().After(deadline) {
					helper.Process.Kill()
					<-exited
					t.Fatalf("the helper's query never started:\n%s", output.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := helper.Process.Signal(stop); err != nil {
				t.Fatal(err)
			}
			var err error
			select {
			case err = <-exited:
			case <-time.After(5 * time.Second):
				helper.Process.Kill()
				<-exited
				t.Fatalf("the gate did not end on %v:\n%s", stop, output.String())
			}
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("the gate ended on %v with %v:\n%s", stop, err, output.String())
			}
			status := exit.Sys().(syscall.WaitStatus)
			if stop == syscall.SIGQUIT {
				if status.Signaled() || status.ExitStatus() != 2 || !strings.Contains(output.String(), "SIGQUIT") {
					t.Fatalf("the gate did not end by Go's SIGQUIT dump (status %v):\n%s", status, output.String())
				}
			} else if !status.Signaled() || status.Signal() != stop {
				t.Fatalf("the gate did not die by %v (status %v):\n%s", stop, status, output.String())
			}
			assertGone(t, recordedPids(t, pids))
		})
	}
}

// TestInterruptHelper is the gate TestInterruptKillsRunningQueries
// interrupts: a run over a directory that is no repository, whose one step
// names probe-tool. Loading the files fails at once, and run then waits for
// the hung fingerprint query it started.
func TestInterruptHelper(t *testing.T) {
	root := os.Getenv("GATE_INTERRUPT_HELPER")
	if root == "" {
		t.Skip("run by TestInterruptKillsRunningQueries")
	}
	g := &gate{root: root, cacheDir: t.TempDir(), steps: []*Step{{Name: "probe", Argv: []string{"true"}, Inputs: &Inputs{Tools: []string{"probe-tool"}}}}}
	g.run(&bytes.Buffer{})
	t.Fatal("run returned before the interrupt")
}

// killRecorded kills, when the test ends, every process recorded in the
// pid files, so a failing assertion leaves nothing running.
func killRecorded(t *testing.T, files ...string) {
	t.Cleanup(func() {
		for _, file := range files {
			for _, pid := range recordedPids(t, file) {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
}

// assertGone fails unless every pid has exited within 2 s.
func assertGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for _, pid := range pids {
		for running(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d a query started survived it", pid)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// recordedPids reads the process IDs a fixture appended to path.
func recordedPids(t *testing.T, path string) []int {
	t.Helper()
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, field := range strings.Fields(string(content)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	return pids
}

// running reports whether a process exists and has not exited.
func running(pid int) bool {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false
	}
	_, after, _ := strings.Cut(string(stat), ") ")
	return !strings.HasPrefix(after, "Z")
}
