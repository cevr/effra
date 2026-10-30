package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
)

// The systemd scope. The gate re-executes itself through
//
//	systemd-run --user --scope -p Delegate=yes -p MemoryMax=<gate budget> \
//	  -p MemorySwapMax=0 -p OOMPolicy=continue
//
// so all its steps share one cgroup v2 subtree capped at the gate budget,
// which systemd delegates to it. Inside, the gate moves itself to a leaf
// cgroup and starts every step directly in a cgroup of its own
// (CLONE_INTO_CGROUP) with memory.max at the step budget, so the cap covers
// go test, every test binary and every process they start, from their first
// byte. Swap is denied to the whole subtree, so a step cannot page out past
// its budget. The kernel's out-of-memory killer stops a step's largest
// process at its budget (go test then reports the package it lost) or, when
// the steps together reach the gate budget, the largest process of the gate.
// After the step the gate reads the step cgroup's memory.events, which it
// still owns, so the verdict has no race with the step's exit; then it
// removes the cgroup. OOMPolicy=continue keeps systemd from stopping the
// whole gate when the kernel kills one step's process.

// enterScope re-executes the gate in its systemd scope, and returns only if
// it cannot: then it says why, and the gate uses the RSS watch.
func enterScope(budget memoryBudget) string {
	if os.Getenv(mechanismVariable) != "" {
		return ""
	}
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return "no systemd-run"
	}
	executable, err := os.Executable()
	if err != nil {
		return "locate the gate executable: " + err.Error()
	}
	probe := exec.Command("systemd-run", scopeArguments("", budget, []string{"true"})...)
	probe.Env = scopeEnvironment("")
	if output, err := probe.CombinedOutput(); err != nil {
		return fmt.Sprintf("systemd-run --user --scope failed (%v): %s", err, firstLine(output))
	}
	unit := scopeUnit()
	command, err := exec.LookPath("systemd-run")
	if err != nil {
		return "no systemd-run"
	}
	argv := append([]string{"systemd-run"}, scopeArguments(unit, budget, append([]string{executable}, os.Args[1:]...))...)
	err = syscall.Exec(command, argv, scopeEnvironment(unit))
	return "re-execute in a systemd scope: " + err.Error()
}

// scopeUnit is a fresh unit name for one gate's scope.
func scopeUnit() string {
	suffix := make([]byte, 4)
	rand.Read(suffix)
	return fmt.Sprintf("ergo-gate-%d-%s", os.Getpid(), hex.EncodeToString(suffix))
}

// scopeArguments are systemd-run's arguments that run argv in a gate scope,
// named unit unless unit is empty.
func scopeArguments(unit string, budget memoryBudget, argv []string) []string {
	arguments := []string{"--user", "--scope", "--quiet", "--collect"}
	if unit != "" {
		arguments = append(arguments, "--unit="+unit)
	}
	arguments = append(arguments,
		"-p", "Delegate=yes",
		"-p", "MemoryMax="+strconv.FormatInt(budget.gate, 10),
		"-p", "MemorySwapMax=0",
		"-p", "OOMPolicy=continue",
		"--")
	return append(arguments, argv...)
}

// scopeEnvironment is the environment of systemd-run: the gate's, naming
// the scope's unit to the gate it starts. systemd-run finds the user's
// service manager through XDG_RUNTIME_DIR, which a caller with a filtered
// environment (a Go test step) lacks; the conventional directory stands in.
func scopeEnvironment(unit string) []string {
	environment := os.Environ()
	if unit != "" {
		environment = append(environment, scopeVariable+"="+unit)
	}
	if os.Getenv("XDG_RUNTIME_DIR") == "" && os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		runtime := fmt.Sprintf("/run/user/%d", os.Getuid())
		if _, err := os.Stat(filepath.Join(runtime, "bus")); err == nil {
			environment = append(environment, "XDG_RUNTIME_DIR="+runtime)
		}
	}
	return environment
}

// cgroupRoot is where the cgroup v2 hierarchy is mounted.
const cgroupRoot = "/sys/fs/cgroup"

// scopeLimiter runs steps in cgroups of the gate's scope.
type scopeLimiter struct {
	budget memoryBudget
	dir    string
	steps  atomic.Int64
}

// openScope takes over the scope the gate runs in, checking that it is the
// named unit and enforces the gate budget, and that a step can start in a
// cgroup of it.
func openScope(unit string, budget memoryBudget) (limiter, error) {
	membership, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return nil, err
	}
	var path string
	for _, line := range strings.Split(string(membership), "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok {
			path = rest
		}
	}
	if filepath.Base(path) != unit+".scope" {
		return nil, fmt.Errorf("the gate is in cgroup %q, not in its scope %s.scope", path, unit)
	}
	s := &scopeLimiter{budget: budget, dir: filepath.Join(cgroupRoot, path)}
	if limit, err := readTrimmed(filepath.Join(s.dir, "memory.max")); err != nil || limit != strconv.FormatInt(budget.gate, 10) {
		return nil, fmt.Errorf("the scope's memory.max is %q (%v), not the gate budget %d", limit, err, budget.gate)
	}
	if swap, err := readTrimmed(filepath.Join(s.dir, "memory.swap.max")); err == nil && swap != "0" {
		return nil, fmt.Errorf("the scope's memory.swap.max is %q, not 0", swap)
	}
	// A cgroup that enables controllers for its children holds no process,
	// so the gate first moves itself to a leaf.
	leaf := filepath.Join(s.dir, "gate")
	if err := os.Mkdir(leaf, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(leaf, "cgroup.procs"), []byte("0"), 0); err != nil {
		return nil, fmt.Errorf("move the gate to its leaf cgroup: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, "cgroup.subtree_control"), []byte("+memory"), 0); err != nil {
		return nil, fmt.Errorf("enable the memory controller for the steps: %w", err)
	}
	probe := exec.Command("true")
	watch, err := s.prepare("probe", probe)
	if err != nil {
		return nil, err
	}
	err = probe.Run()
	watch.finish()
	if err != nil {
		return nil, fmt.Errorf("start a process in a step cgroup: %w", err)
	}
	return s, nil
}

func (s *scopeLimiter) describe() string {
	return "systemd scope " + describeBudget(s.budget)
}

func (s *scopeLimiter) prepare(name string, command *exec.Cmd) (stepWatch, error) {
	dir := filepath.Join(s.dir, fmt.Sprintf("step-%d", s.steps.Add(1)))
	if err := os.Mkdir(dir, 0o755); err != nil {
		return nil, fmt.Errorf("memory budget: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.max"), []byte(strconv.FormatInt(s.budget.step, 10)), 0); err != nil {
		os.Remove(dir)
		return nil, fmt.Errorf("memory budget: %w", err)
	}
	handle, err := os.Open(dir)
	if err != nil {
		os.Remove(dir)
		return nil, fmt.Errorf("memory budget: %w", err)
	}
	command.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: int(handle.Fd())}
	return &scopeStep{limiter: s, dir: dir, handle: handle}, nil
}

func (s *scopeLimiter) close() {}

// scopeStep is one step's cgroup.
type scopeStep struct {
	limiter *scopeLimiter
	dir     string
	handle  *os.File
}

func (s *scopeStep) started(int) {}

// finish reads the step cgroup's counters. Its own "oom" counts the times
// the step reached its memory.max; "oom_kill" counts its processes the
// kernel killed, for its own budget or for the gate's. The cgroup is removed
// when it is empty; a process the step left running keeps it until the
// scope ends.
func (s *scopeStep) finish() usage {
	defer s.handle.Close()
	defer os.Remove(s.dir)
	var u usage
	if peak, err := readTrimmed(filepath.Join(s.dir, "memory.peak")); err == nil {
		u.peak, _ = strconv.ParseInt(peak, 10, 64)
	}
	events, err := readEvents(filepath.Join(s.dir, "memory.events"))
	switch {
	case err != nil:
		u.verdict = "memory budget not enforced: read the step's memory events: " + err.Error()
	case events["oom"] > 0:
		u.verdict = stepExceeded(s.limiter.budget)
	case events["oom_kill"] > 0:
		u.verdict = gateExceeded(s.limiter.budget)
	}
	return u
}

func readTrimmed(name string) (string, error) {
	content, err := os.ReadFile(name)
	return strings.TrimSpace(string(content)), err
}

// readEvents parses a cgroup "name value" counter file.
func readEvents(name string) (map[string]int64, error) {
	content, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	events := map[string]int64{}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), " ")
		if !ok {
			return nil, fmt.Errorf("%s: unexpected line %q", name, scanner.Text())
		}
		count, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		events[key] = count
	}
	if len(events) == 0 {
		return nil, errors.New(name + " is empty")
	}
	return events, nil
}

// totalMemory reads MemTotal from /proc/meminfo.
func totalMemory() int64 {
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(content), "\n") {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			fields := strings.Fields(rest)
			if len(fields) == 2 && fields[1] == "kB" {
				kilobytes, err := strconv.ParseInt(fields[0], 10, 64)
				if err == nil {
					return kilobytes << 10
				}
			}
		}
	}
	return 0
}
