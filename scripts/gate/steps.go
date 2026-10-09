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

// gofmtDirectories are the Go trees the gate requires to be gofmt-clean.
var gofmtDirectories = []string{"cmd", "internal", "runtime", "lint", "scripts/gate", "examples/go-interop", "examples/sdk", "examples/compare", "examples/hosttypes", "examples/lintpack"}

// pythonSteps are the repository's Python checks in their historical order.
var pythonChecks = []struct {
	name string
	argv []string
}{
	{"wayfinder check", []string{"python3", "scripts/wayfinder.py", "check"}},
	{"wayfinder migration: hosted run binding", []string{"python3", "scripts/wayfinder_migration.py", "--input-snapshot", "hosted-run-binding-2026-10-08", "--check"}},
	{"wayfinder migration: current map", []string{"python3", "scripts/wayfinder_migration.py", "--current-identity-intake", "github-wayfinder-current-intake-2026-10-08", "--mapping", "docs/wayfinder/migration/current-hosted-identities-2026-10-09-source-reconciled.json", "--check", "--output-snapshot", "current-wayfinder-map-2026-10-09-source-reconciled"}},
	{"wayfinder migration tests", []string{"python3", "-B", "scripts/test_wayfinder_migration.py"}},
	{"wayfinder hosted reconciliation tests", []string{"python3", "-B", "scripts/test_wayfinder_hosted_reconciliation.py"}},
	{"effect conformance import", []string{"python3", "-B", "scripts/import_effect_conformance.py"}},
	{"effect conformance import tests", []string{"python3", "-B", "scripts/test_import_effect_conformance.py"}},
	{"effect conformance mapping", []string{"python3", "-B", "scripts/check_effect_conformance.py"}},
	{"effect conformance mapping tests", []string{"python3", "-B", "scripts/test_effect_conformance.py"}},
}

// smokeChains are the process smokes against bin/ef. A chain runs in order:
// smokes in one chain write the same dist/<name> outputs in the repository
// root, so they must not overlap; separate chains share no output path.
var smokeChains = [][]string{
	{"smoke", "producer_smoke"},
	{"diagnostics_smoke"},
	{"lsp_smoke"},
	{"readme_smoke"},
	{"bundled_smoke"},
	{"format_smoke"},
	{"mcp_text_smoke"},
	{"layer_smoke"},
	{"type_smoke"},
	{"http_smoke"},
	{"http_transport_smoke"},
}

func gateSteps(root string) ([]*Step, error) {
	var steps []*Step
	add := func(step *Step) *Step {
		steps = append(steps, step)
		return step
	}
	add(&Step{Name: "no tracked python bytecode", Argv: []string{"sh", "-c", `tracked=$(git ls-files '*.pyc'); if [ -n "$tracked" ]; then echo "tracked Python bytecode (git rm it; __pycache__/ is ignored):"; echo "$tracked"; exit 1; fi`}})

	python := []string{"python3", "git"}
	wayfinder := &Inputs{Paths: []string{"scripts/*.py", "docs/wayfinder/", ".gitignore"}, Tools: python}
	conformance := &Inputs{Paths: []string{"scripts/*.py", "conformance/", ".gitmodules", "scripts/init_upstream.sh", "internal/compiler/*_test.go"}, Tools: python, Submodule: "conformance/upstream/effect"}
	for _, check := range pythonChecks {
		inputs := conformance
		if strings.HasPrefix(check.name, "wayfinder") {
			inputs = wayfinder
		}
		if check.name == "wayfinder hosted reconciliation tests" {
			// Its ordinary-clone control refuses unstaged or untracked files,
			// then reruns the checker in a clone rebuilt from HEAD plus the
			// staged patch. Git guarantees that clone holds the staged tree,
			// so beyond committability only the checker's own inputs matter.
			inputs = &Inputs{Paths: wayfinder.Paths, Tools: python, Clean: true}
		}
		add(&Step{Name: check.name, Argv: check.argv, Inputs: inputs})
	}

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
			add(&Step{Name: fmt.Sprintf("go test %s [%d/%d]", shortPackage(root, pkg), i+1, len(selections)), Needs: []string{"go test build"}, Argv: append(argv, pkg)})
		}
	}
	add(&Step{Name: "go test (other packages)", Needs: []string{"go test build"}, Argv: append([]string{"go", "test", testTimeout}, unsharded...)})
	add(&Step{Name: "build bin/ef", Needs: []string{"go test build"}, Argv: []string{"go", "build", "-o", "bin/ef", "./cmd/ef"}})

	examples, err := authoredExamples(root)
	if err != nil {
		return nil, err
	}
	add(&Step{Name: "ef fmt --check", Needs: []string{"build bin/ef"}, Argv: append([]string{"./bin/ef", "fmt", "--check"}, examples...)})

	// The smokes drive bin/ef, Go, Bun, Node and tsc over the whole module,
	// examples and runtime; only design records are outside their reach.
	smokeTools := []string{"go", "python3", "bun", "node", "tsc", "git"}
	smokeEnv := []string{"NODE_OPTIONS", "NODE_PATH", "BUN_CONFIG_REGISTRY"}
	smokeInputs := &Inputs{Exclude: []string{"docs/", "plans/", "AGENTS.md", "CHANGELOG.md", "GLOSSARY.md", "NORTH_STAR.md", "PRIOR_ARTS.md", "void-migration-freeze-*.md"}, Tools: smokeTools, Env: smokeEnv}
	readmeInputs := &Inputs{Tools: smokeTools, Env: smokeEnv}
	for _, chain := range smokeChains {
		needs := []string{"build bin/ef"}
		for _, smoke := range chain {
			inputs := smokeInputs
			if smoke == "readme_smoke" {
				// README excerpts may quote any file.
				inputs = readmeInputs
			}
			add(&Step{Name: smoke, Needs: needs, Argv: []string{"python3", "scripts/" + smoke + ".py"}, Inputs: inputs})
			needs = []string{"build bin/ef", smoke}
		}
	}
	return steps, nil
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
		// subtests, so a test's weight is its wall time from run to finish.
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
			case "run":
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
