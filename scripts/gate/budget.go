package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// The memory budget (see the package comment). Every step runs under a step
// budget, and the steps of one gate share a gate budget.
const (
	// stepMemoryVariable overrides the step budget, defaultStepMemory.
	stepMemoryVariable = "ERGO_GATE_STEP_MEMORY"
	// gateMemoryVariable overrides the gate budget, half of physical memory.
	gateMemoryVariable = "ERGO_GATE_MEMORY"
	// mechanismVariable set to "rss" forces the RSS watch where the systemd
	// scope would work, so its tests run on Linux too.
	mechanismVariable = "ERGO_GATE_MEMORY_MECHANISM"
	// scopeVariable names, to the gate re-executed in its systemd scope, the
	// scope's unit. The gate reads it once and removes it from its
	// environment, so no step inherits it.
	scopeVariable = "ERGO_GATE_SCOPE"
)

// defaultStepMemory is the step budget unless stepMemoryVariable sets one:
// the owner's figure (2026-10-30). An uncached gate on main e36c9471 with
// -count=1 measured these step peaks: "go test (other packages)" 2.5G (it
// holds internal/sketch/check, which durations.json does not shard),
// "go test ./cmd/ergo" 602M, "go test build" 560M (1.0G with a cold build
// cache), every other step under 110M. So 8G leaves the largest step more
// than 3x headroom, and a runaway test, such as the checker test that
// reached 44 GB, stops at it. The gate budget, half of the workbox's 62G,
// is 31G: room for the concurrent steps of one gate (about 4G together at
// these peaks) and for one runaway step, while one gate never takes more
// than half the host.
const defaultStepMemory int64 = 8 << 30

// memoryBudget is the hard cap, in bytes, of each step and of all the steps
// of one gate together.
type memoryBudget struct{ step, gate int64 }

// currentBudget reads the budget from the environment. The gate budget
// defaults to half of physical memory, so one gate cannot take the host,
// and a step budget above the gate budget is lowered to it.
func currentBudget() (memoryBudget, error) {
	budget := memoryBudget{step: defaultStepMemory}
	if value := os.Getenv(stepMemoryVariable); value != "" {
		parsed, err := parseBytes(value)
		if err != nil {
			return memoryBudget{}, fmt.Errorf("%s: %w", stepMemoryVariable, err)
		}
		budget.step = parsed
	}
	if value := os.Getenv(gateMemoryVariable); value != "" {
		parsed, err := parseBytes(value)
		if err != nil {
			return memoryBudget{}, fmt.Errorf("%s: %w", gateMemoryVariable, err)
		}
		budget.gate = parsed
	} else {
		total := physicalMemory()
		if total <= 0 {
			return memoryBudget{}, fmt.Errorf("physical memory unknown on this host: set %s", gateMemoryVariable)
		}
		// Rounded down to whole gibibytes (mebibytes on a tiny host), which
		// read well in the budget line.
		half, unit := total/2, int64(1<<30)
		if half < unit {
			unit = 1 << 20
		}
		budget.gate = half / unit * unit
	}
	budget.step = min(budget.step, budget.gate)
	return budget, nil
}

// softLimit is the GOMEMLIMIT each step's Go processes get unless the
// caller sets one: three quarters of the step budget, so the collector
// works harder before the kernel or the watch stops the step.
func softLimit(budget memoryBudget) string {
	return strconv.FormatInt(budget.step/4*3, 10)
}

// stepSoftLimit is softLimit for the budget in the environment, or for the
// default budget when the environment's does not parse (the gate refuses
// to start then, so no step runs with it).
func stepSoftLimit() string {
	budget, err := currentBudget()
	if err != nil {
		budget = memoryBudget{step: defaultStepMemory, gate: defaultStepMemory}
	}
	return softLimit(budget)
}

var physicalMemoryOnce = sync.OnceValue(totalMemory)

// physicalMemory is the host's memory in bytes, or 0 if unknown.
func physicalMemory() int64 { return physicalMemoryOnce() }

// parseBytes reads a size as systemd's MemoryMax does: a decimal number of
// bytes with an optional binary suffix K, M, G or T.
func parseBytes(value string) (int64, error) {
	digits, shift := value, 0
	if n := len(value); n > 0 {
		switch value[n-1] {
		case 'K', 'k':
			shift = 10
		case 'M', 'm':
			shift = 20
		case 'G', 'g':
			shift = 30
		case 'T', 't':
			shift = 40
		}
		if shift > 0 {
			digits = value[:n-1]
		}
	}
	number, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || number <= 0 || number > (1<<62)>>shift {
		return 0, fmt.Errorf("%q is not a positive size such as 512M or 8G", value)
	}
	return number << shift, nil
}

// formatBytes writes a size the way parseBytes reads it, exactly where a
// suffix divides it, and to a tenth otherwise.
func formatBytes(size int64) string {
	for _, unit := range []struct {
		shift  uint
		suffix string
	}{{40, "T"}, {30, "G"}, {20, "M"}, {10, "K"}} {
		if size >= 1<<unit.shift {
			if size%(1<<unit.shift) == 0 {
				return strconv.FormatInt(size>>unit.shift, 10) + unit.suffix
			}
			return strconv.FormatFloat(float64(size)/float64(int64(1)<<unit.shift), 'f', 1, 64) + unit.suffix
		}
	}
	return strconv.FormatInt(size, 10)
}

// A limiter runs steps under the memory budget.
type limiter interface {
	// describe names the mechanism and the budget, for the gate's log.
	describe() string
	// prepare readies command, not yet started, to run the named step under
	// the step budget.
	prepare(name string, command *exec.Cmd) (stepWatch, error)
	close()
}

// stepWatch follows one step.
type stepWatch interface {
	// started is called once the command has started.
	started(pid int)
	// finish is called once the command has ended or failed to start.
	finish() usage
}

// usage is what a step used of its budget.
type usage struct {
	// peak is the most memory the step held, in bytes, or 0 if unknown.
	peak int64
	// verdict is empty, or says why the step fails whatever its exit
	// status: its budget or the gate's was exceeded, or the budget could not
	// be enforced.
	verdict string
}

func stepExceeded(budget memoryBudget) string {
	return fmt.Sprintf("memory budget exceeded (%s)", formatBytes(budget.step))
}

func gateExceeded(budget memoryBudget) string {
	return fmt.Sprintf("gate memory budget exceeded (%s); the step was the largest when the gate's steps together passed it", formatBytes(budget.gate))
}

// openLimiter chooses the mechanism: the systemd scope the gate runs in, or
// else the RSS watch, saying why.
func (g *gate) openLimiter(budget memoryBudget) (limiter, error) {
	switch mechanism := os.Getenv(mechanismVariable); mechanism {
	case "":
	case "rss":
		return openRSSWatch(budget, "forced by "+mechanismVariable+"=rss")
	default:
		return nil, fmt.Errorf("%s=%q: the only value is rss", mechanismVariable, mechanism)
	}
	reason := g.scopeNote
	if g.scope != "" {
		scope, err := openScope(g.scope, budget)
		if err == nil {
			return scope, nil
		}
		reason = "systemd scope unusable: " + err.Error()
	}
	if reason == "" {
		reason = "not started in a systemd scope"
	}
	return openRSSWatch(budget, reason)
}

// describeBudget is the budget part of the gate's mechanism line.
func describeBudget(budget memoryBudget) string {
	return fmt.Sprintf("%s per step, %s per gate", formatBytes(budget.step), formatBytes(budget.gate))
}

// firstLine is the first line of a command's output, for a reason.
func firstLine(output []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n")
	return line
}
