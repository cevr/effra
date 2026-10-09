package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// durationsFile records measured per-test seconds for the sharded packages.
// It only balances shards; a stale file costs wall time, never coverage.
const durationsFile = "scripts/gate/durations.json"

// shardTarget is the measured work each Go test process should receive.
const shardTargetSeconds = 12.0

// testTimeout is a safety net for a hung test process, never a performance
// budget: a shard that needs it is a defect to split or fix.
const testTimeout = "-timeout=15m"

// testTools are the executables Go tests spawn besides the Go toolchain,
// whose binary and version Go's test cache already keys on.
var testTools = []string{"node", "bun", "tsc", "git"}

// testToolEnvironment are variables those tools read but Go tests do not, so
// Go's test cache cannot see them; they join the tool digest.
var testToolEnvironment = []string{"NODE_OPTIONS", "NODE_PATH", "BUN_CONFIG_REGISTRY"}

// testInstalled are installed dependency trees Go tests run Node and Bun
// over; their content digest joins the tool digest.
var testInstalled = []string{"node_modules"}

// gofmtDirectories are the Go trees the gate requires to be gofmt-clean.
var gofmtDirectories = []string{"cmd", "internal", "runtime", "lint", "scripts/gate", "scripts/conformance", "examples/go-interop", "examples/sdk", "examples/compare", "examples/hosttypes", "examples/lintpack"}

func gateSteps(root string) ([]*Step, error) {
	var steps []*Step
	add := func(step *Step) *Step {
		steps = append(steps, step)
		return step
	}
	add(&Step{Name: "no tracked python bytecode", Argv: []string{"sh", "-c", `tracked=$(git ls-files '*.pyc'); if [ -n "$tracked" ]; then echo "tracked Python bytecode (git rm it; __pycache__/ is ignored):"; echo "$tracked"; exit 1; fi`}})
	add(&Step{Name: "wayfinder snapshot check", Argv: []string{"go", "run", "./cmd/wayfinder", "check", "--snapshot"}, Inputs: wayfinderSnapshotInputs()})

	// The corpus verifier reads the pinned submodule's git objects and the
	// mapping names evidence tests in internal/compiler. Its own tests run with
	// the other Go packages.
	conformance := &Inputs{Paths: []string{"scripts/conformance/", "conformance/", ".gitmodules", "scripts/init_upstream.sh", "internal/compiler/*_test.go", "go.mod"}, Tools: []string{"go", "git"}, Submodule: "conformance/upstream/effect"}
	add(&Step{Name: "effect conformance import", Argv: []string{"go", "run", "./scripts/conformance", "import"}, Inputs: conformance})
	// The Foldkit checks walk the snapshot directory, so files Git ignores
	// there are inputs too.
	// there are inputs too. The importer's controls run with the other Go
	// packages.
	foldkit := &Inputs{Paths: []string{"scripts/conformance/", "conformance/", "go.mod"}, Tools: []string{"go"}, Installed: []string{"conformance/upstream/foldkit-d21db423"}}
	add(&Step{Name: "foldkit corpus self-check", Argv: []string{"go", "run", "./scripts/conformance", "foldkit", "--self-check"}, Inputs: foldkit})
	references, err := frameworkReferenceInputs(root)
	if err != nil {
		return nil, err
	}
	add(&Step{Name: "framework-port references", Argv: []string{"node", frameworkPorts + "scripts/check-references.mjs"}, Inputs: references})
	add(&Step{Name: "effect conformance mapping", Argv: []string{"go", "run", "./scripts/conformance", "check"}, Inputs: conformance})

	add(&Step{Name: "gofmt", Argv: append([]string{"sh", "-c", `unformatted=$(gofmt -l "$@"); if [ -n "$unformatted" ]; then echo "gofmt needed:"; echo "$unformatted"; exit 1; fi`, "gofmt"}, gofmtDirectories...)})
	// Building every test binary once lets vet, the shards and the CLI build
	// reuse one compilation instead of racing to repeat it.
	add(&Step{Name: "go test build", Argv: []string{"go", "test", "-run", "^$", "./..."}})
	add(&Step{Name: "go vet", Needs: []string{"go test build"}, Argv: []string{"go", "vet", "./..."}})

	packages, err := goPackages(root)
	if err != nil {
		return nil, err
	}
	durations, err := readDurations(root)
	if err != nil {
		return nil, err
	}
	var unsharded []string
	for _, pkg := range packages {
		measured, ok := durations[pkg]
		if !ok {
			unsharded = append(unsharded, pkg)
			continue
		}
		dir := filepath.Join(root, strings.TrimPrefix(pkg, modulePath(root)+"/"))
		names, err := testNames(dir)
		if err != nil {
			return nil, err
		}
		total := 0.0
		for _, name := range names {
			total += measured[name]
		}
		n := int(math.Ceil(total / shardTargetSeconds))
		n = max(1, min(n, runtime.NumCPU(), len(names)))
		selections := shardArgs(shard(names, measured, n))
		for i, selection := range selections {
			argv := append([]string{"go", "test", testTimeout}, selection...)
			add(&Step{Name: fmt.Sprintf("go test %s [%d/%d]", shortPackage(root, pkg), i+1, len(selections)), Needs: []string{"go test build"}, Argv: append(argv, pkg), ToolEnv: testTools})
		}
	}
	add(&Step{Name: "go test (other packages)", Needs: []string{"go test build"}, Argv: append([]string{"go", "test", testTimeout}, unsharded...), ToolEnv: testTools})
	// Without a VCS stamp the binary depends only on its sources, so a commit
	// that changes none of them reuses the link instead of relinking for a
	// new revision. The gate's bin/ef therefore declares no vcs.revision in
	// its producer identity, while its artifact digest still identifies the
	// executing bytes.
	add(&Step{Name: "build bin/ef", Needs: []string{"go test build"}, Argv: []string{"go", "build", "-buildvcs=false", "-o", "bin/ef", "./cmd/ef"}})

	examples, err := authoredExamples(root)
	if err != nil {
		return nil, err
	}
	add(&Step{Name: "ef fmt --check", Needs: []string{"build bin/ef"}, Argv: append([]string{"./bin/ef", "fmt", "--check"}, examples...)})

	return steps, nil
}

// wayfinderSnapshotInputs keys the check that the committed Wayfinder snapshot
// is a structurally valid map: the snapshot, the command's non-test sources
// (it imports only the standard library) and the Go toolchain. The check never
// reads GitHub, so a stale snapshot passes; the command's fixture tests run
// with the other Go packages.
func wayfinderSnapshotInputs() *Inputs {
	return &Inputs{
		Paths:   []string{"docs/wayfinder/snapshot.json", "cmd/wayfinder/", "go.mod"},
		Exclude: []string{"cmd/wayfinder/*_test.go"},
		Tools:   []string{"go"},
		Env:     []string{"GOFLAGS", "GOEXPERIMENT"},
	}
}

// frameworkPorts is the framework-port reference leaf: its own lockfile and
// installed node_modules, which Git ignores.
const frameworkPorts = "conformance/framework-ports/ports/"

// frameworkReferenceInputs keys the framework-port reference check. Beyond
// its leaf, the counter-domain control builds ./cmd/ef from source and runs
// the compiled Go and JavaScript programs, so every non-test file of the
// compiler's main-module packages is an input, with the Go and JavaScript
// toolchains and the environment they read.
func frameworkReferenceInputs(root string) (*Inputs, error) {
	command := exec.Command("go", "list", "-deps", "-f", "{{if .Module}}{{if .Module.Main}}{{.Dir}}{{end}}{{end}}", "./cmd/ef")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps ./cmd/ef: %w", err)
	}
	inputs := &Inputs{
		Paths:     []string{frameworkPorts, "go.mod", "go.sum"},
		Tools:     []string{"node", "bun", "go", "git"},
		Env:       append([]string{"GOFLAGS", "GOOS", "GOARCH", "CGO_ENABLED", "GOEXPERIMENT"}, testToolEnvironment...),
		Installed: []string{frameworkPorts + "node_modules"},
	}
	for _, dir := range strings.Fields(string(output)) {
		relative, err := filepath.Rel(root, dir)
		if err != nil || strings.HasPrefix(relative, "..") {
			return nil, fmt.Errorf("compiler package %s is outside %s", dir, root)
		}
		relative = filepath.ToSlash(relative)
		inputs.Paths = append(inputs.Paths, relative+"/")
		// Test files are not build inputs.
		inputs.Exclude = append(inputs.Exclude, relative+"/*_test.go")
	}
	return inputs, nil
}

func goPackages(root string) ([]string, error) {
	command := exec.Command("go", "list", "./...")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("go list ./...: %w", err)
	}
	return strings.Fields(string(output)), nil
}

func modulePath(root string) string {
	content, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func shortPackage(root, pkg string) string {
	return "./" + strings.TrimPrefix(pkg, modulePath(root)+"/")
}

func readDurations(root string) (map[string]map[string]float64, error) {
	content, err := os.ReadFile(filepath.Join(root, durationsFile))
	if os.IsNotExist(err) {
		return map[string]map[string]float64{}, nil
	}
	if err != nil {
		return nil, err
	}
	var durations map[string]map[string]float64
	if err := json.Unmarshal(content, &durations); err != nil {
		return nil, fmt.Errorf("%s: %w", durationsFile, err)
	}
	return durations, nil
}

func authoredExamples(root string) ([]string, error) {
	const list = "scripts/authored_examples.txt"
	content, err := os.ReadFile(filepath.Join(root, list))
	if err != nil {
		return nil, err
	}
	var examples []string
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		example := scanner.Text()
		if example == "" {
			return nil, fmt.Errorf("empty authored example entry in %s", list)
		}
		if info, err := os.Stat(filepath.Join(root, example)); err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("missing authored example: %s", example)
		}
		examples = append(examples, example)
	}
	if len(examples) == 0 {
		return nil, fmt.Errorf("authored example selection is empty: %s", list)
	}
	return examples, nil
}

// slowPackages are measured by -record-durations and sharded by the gate.
var slowPackages = []string{"./internal/compiler", "./cmd/ef"}

// writeDurations measures each slow package's tests serially (as a single
// process runs them) and rewrites the durations file.
func writeDurations(root string) error {
	durations := map[string]map[string]float64{}
	module := modulePath(root)
	for _, pkg := range slowPackages {
		command := exec.Command("go", "test", "-count=1", "-json", testTimeout, pkg)
		command.Env = stepEnvironment()
		command.Dir = root
		command.Stderr = os.Stderr
		output, err := command.Output()
		if err != nil {
			return fmt.Errorf("go test -json %s: %w", pkg, err)
		}
		// Elapsed omits time a test spends waiting for its parallel
		// subtests, so a test's weight is its wall time to finish, counted
		// from its start or, for a parallel test, from when it resumes.
		measured := map[string]float64{}
		started := map[string]time.Time{}
		decoder := json.NewDecoder(bytes.NewReader(output))
		for decoder.More() {
			var event struct {
				Time   time.Time
				Action string
				Test   string
			}
			if err := decoder.Decode(&event); err != nil {
				return err
			}
			if event.Test == "" || strings.Contains(event.Test, "/") {
				continue
			}
			switch event.Action {
			case "run", "cont":
				started[event.Test] = event.Time
			case "pass", "fail", "skip":
				measured[event.Test] = math.Round(event.Time.Sub(started[event.Test]).Seconds()*100) / 100
			}
		}
		durations[module+"/"+strings.TrimPrefix(pkg, "./")] = measured
	}
	data, err := json.MarshalIndent(durations, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, durationsFile), append(data, '\n'), 0644)
}
