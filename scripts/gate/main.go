// Command gate runs the repository merge gate.
//
// Every check runs as a step of a small dependency graph: independent steps run
// concurrently, each step's output is captured whole and printed in declaration
// order, and the gate exits nonzero naming every failed step. A step whose
// result is a pure function of declared inputs records its pass under the
// content hash of those inputs (outside the tree, shared by every worktree);
// unchanged inputs replay the recorded receipt instead of running again. Go
// tests are not cached here: Go's own per-package test cache stays
// authoritative, and slow packages are split into processes whose union is
// complete by construction (the last shard skips exactly what the others run).
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// cacheVersion is part of every key; bump it when a step's meaning changes in
// a way its declared inputs cannot see.
const cacheVersion = "effra-gate-v2"

// Step is one gate check.
type Step struct {
	Name  string
	Needs []string
	// Argv runs in the repository root with the gate's environment.
	Argv []string
	// Inputs, when set, makes the step's pass reusable under the hash of
	// exactly these inputs. Nil means the step always runs: the tool owns its
	// cache, the step is cheaper than hashing, or it reads inputs no digest
	// here captures, such as Git history.
	Inputs *Inputs
	// ToolEnv names executables the step's own cache cannot see, such as the
	// JavaScript tools Go tests spawn. Their versions, with the environment
	// they read (testToolEnvironment), reach the step as one digest in
	// EFFRA_TOOL_VERSIONS; a test that reads it keys Go's test cache on it.
	ToolEnv []string
}

// Inputs declares everything a cached step's result depends on.
type Inputs struct {
	// Paths selects repository files (tracked and untracked, not ignored):
	// "dir/" matches a subtree, anything else is a path.Match pattern. Empty
	// selects every file.
	Paths   []string
	Exclude []string
	// Tools names executables whose version is an input.
	Tools []string
	// Env names environment variables whose values are inputs.
	Env []string
	// Clean makes the tree's committability an input: whether it has
	// unstaged changes to tracked files or untracked, non-ignored files.
	Clean bool
	// Submodule makes a submodule's checked-out commit and status an input.
	Submodule string
	// Installed names directories outside Git's view, such as an installed
	// node_modules, whose contents are an input: every file's path,
	// permissions and content and every symlink's target, following links
	// as Node and Bun resolve them. An absent directory is a distinct input.
	Installed []string
}

type outcome int

const (
	passed outcome = iota
	cached
	failed
	blocked
)

type result struct {
	outcome  outcome
	output   []byte
	duration time.Duration
	cpu      time.Duration
	note     string
}

type receipt struct {
	Step     string    `json:"step"`
	Key      string    `json:"key"`
	Duration float64   `json:"durationSeconds"`
	Passed   time.Time `json:"passedAt"`
	Output   string    `json:"output"`
}

type gate struct {
	root     string
	cacheDir string
	noCache  bool
	steps    []*Step
	files    []string
	digests  map[string]string
	tools    map[string]string
	toolsMu  sync.Mutex
	clean    string
	modules  map[string]string
	factsMu  sync.Mutex

	installedDigests map[string]string
	installedMu      sync.Mutex
}

func main() {
	list := flag.Bool("list", false, "print the checks the gate runs, one per line, and exit")
	noCache := flag.Bool("no-cache", false, "run every step even when a pass for identical inputs is recorded")
	recordDurations := flag.Bool("record-durations", false, "measure the sharded Go packages and rewrite "+durationsFile)
	flag.Parse()
	root, err := repositoryRoot()
	if err != nil {
		fatal(err)
	}
	// File-mode assertions in the process tests expect the conventional
	// creation mask; the gate fixes it rather than inheriting a caller's.
	setCreationMask()
	// Steps run concurrently, and git status/diff (including the Go
	// toolchain's VCS stamping) otherwise take the index lock to refresh
	// stat data, failing a concurrent step that must write the index.
	os.Setenv("GIT_OPTIONAL_LOCKS", "0")
	if *recordDurations {
		if err := writeDurations(root); err != nil {
			fatal(err)
		}
		return
	}
	steps, err := gateSteps(root)
	if err != nil {
		fatal(err)
	}
	if *list {
		for _, step := range steps {
			fmt.Printf("%s: %s\n", step.Name, strings.Join(step.Argv, " "))
		}
		return
	}
	// The first line states whether the README comparison program is
	// strictly type-checked; TestReadmeSmoke skips that check without tsc.
	if tsc, err := exec.LookPath("tsc"); err != nil {
		fmt.Fprintln(os.Stderr, "typescript: unchecked (no tsc on PATH); the README comparison program is not strictly type-checked")
	} else {
		fmt.Printf("typescript: strict (%s)\n", tsc)
	}
	g := &gate{root: root, cacheDir: cacheDirectory(), noCache: *noCache, steps: steps}
	os.Exit(g.run(os.Stdout))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gate:", err)
	os.Exit(2)
}

func repositoryRoot() (string, error) {
	executable, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
	}
	return strings.TrimSpace(string(executable)), nil
}

func cacheDirectory() string {
	if dir := os.Getenv("EFFRA_GATE_CACHE"); dir != "" {
		return dir
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "effra-gate")
}

// run executes the graph and returns the process exit code.
func (g *gate) run(out io.Writer) int {
	started := time.Now()
	index := map[string]int{}
	for i, step := range g.steps {
		if _, duplicate := index[step.Name]; duplicate {
			fmt.Fprintf(out, "gate: duplicate step %q\n", step.Name)
			return 2
		}
		index[step.Name] = i
	}
	for _, step := range g.steps {
		for _, need := range step.Needs {
			if _, ok := index[need]; !ok {
				fmt.Fprintf(out, "gate: step %q needs unknown step %q\n", step.Name, need)
				return 2
			}
		}
	}
	// Digest the test dependency trees while files load and early steps run;
	// the first Go test step then finds the digest ready.
	for _, step := range g.steps {
		if len(step.ToolEnv) > 0 {
			for _, dir := range testInstalled {
				go g.installed(dir)
			}
			break
		}
	}
	if err := g.loadFiles(); err != nil {
		fmt.Fprintln(out, "gate:", err)
		return 2
	}

	results := make([]*result, len(g.steps))
	done := make([]chan struct{}, len(g.steps))
	for i := range done {
		done[i] = make(chan struct{})
	}
	slots := make(chan struct{}, max(2, runtime.NumCPU()))
	for i, step := range g.steps {
		go func() {
			defer close(done[i])
			for _, need := range step.Needs {
				j := index[need]
				<-done[j]
				if results[j].outcome == failed || results[j].outcome == blocked {
					results[i] = &result{outcome: blocked, note: "needs " + need}
					return
				}
			}
			slots <- struct{}{}
			results[i] = g.execute(step)
			<-slots
		}()
	}

	var failures []string
	for i, step := range g.steps {
		<-done[i]
		r := results[i]
		switch r.outcome {
		case passed:
			fmt.Fprintf(out, "== %s: ok (%.1fs, cpu %.1fs)\n", step.Name, r.duration.Seconds(), r.cpu.Seconds())
		case cached:
			fmt.Fprintf(out, "== %s: ok, cached (%s)\n", step.Name, r.note)
		case failed:
			fmt.Fprintf(out, "== %s: FAILED (%.1fs, cpu %.1fs)\n", step.Name, r.duration.Seconds(), r.cpu.Seconds())
			failures = append(failures, step.Name)
		case blocked:
			fmt.Fprintf(out, "== %s: not run (%s)\n", step.Name, r.note)
			failures = append(failures, step.Name)
		}
		out.Write(r.output)
		if len(r.output) > 0 && r.output[len(r.output)-1] != '\n' {
			fmt.Fprintln(out)
		}
	}
	elapsed := time.Since(started).Seconds()
	if len(failures) > 0 {
		fmt.Fprintf(out, "gate: FAILED in %.1fs: %s\n", elapsed, strings.Join(failures, ", "))
		return 1
	}
	fmt.Fprintf(out, "gate: passed %d steps in %.1fs\n", len(g.steps), elapsed)
	return 0
}

func (g *gate) execute(step *Step) *result {
	key := ""
	if step.Inputs != nil && g.cacheDir != "" {
		var err error
		key, err = g.key(step)
		if err != nil {
			return &result{outcome: failed, output: []byte("gate: hash inputs: " + err.Error() + "\n")}
		}
		if !g.noCache {
			if recorded, ok := g.lookup(key); ok {
				return &result{outcome: cached, output: []byte(recorded.Output),
					note: fmt.Sprintf("passed in %.1fs at %s, inputs %s", recorded.Duration, recorded.Passed.UTC().Format(time.RFC3339), key[:12])}
			}
		}
	}
	started := time.Now()
	command := exec.CommandContext(context.Background(), step.Argv[0], step.Argv[1:]...)
	command.Dir = g.root
	command.Env = stepEnvironment()
	if len(step.ToolEnv) > 0 {
		digest := sha256.New()
		for _, tool := range step.ToolEnv {
			fmt.Fprintf(digest, "%s\x00%s\x00", tool, g.toolVersion(tool))
		}
		for _, name := range testToolEnvironment {
			value, set := os.LookupEnv(name)
			fmt.Fprintf(digest, "env\x00%s\x00%t\x00%s\x00", name, set, value)
		}
		for _, dir := range testInstalled {
			fmt.Fprintf(digest, "installed\x00%s\x00%s\x00", dir, g.installed(dir))
		}
		command.Env = append(command.Env, "EFFRA_TOOL_VERSIONS="+hex.EncodeToString(digest.Sum(nil))[:16])
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	r := &result{outcome: passed, output: output.Bytes(), duration: time.Since(started)}
	if command.ProcessState != nil {
		// A reaped child's usage includes the descendants it reaped.
		r.cpu = command.ProcessState.UserTime() + command.ProcessState.SystemTime()
	}
	if err != nil {
		r.outcome = failed
		fmt.Fprintf(&output, "gate: %s: %v\n", step.Name, err)
		r.output = output.Bytes()
		return r
	}
	if key != "" {
		g.record(receipt{Step: step.Name, Key: key, Duration: r.duration.Seconds(), Passed: time.Now(), Output: output.String()})
	}
	return r
}

// gcPercent is the Go collector target for every step's Go processes (tests,
// the toolchain and the CLI) unless the caller sets GOGC. Measured on the
// slowest compiler test, 400 instead of the default 100 cut CPU by 42% for 6%
// more peak memory; no test depends on collection frequency.
const gcPercent = "400"

func stepEnvironment() []string {
	environment := os.Environ()
	if _, set := os.LookupEnv("GOGC"); !set {
		environment = append(environment, "GOGC="+gcPercent)
	}
	return environment
}

// loadFiles hashes every tracked and untracked (non-ignored) file once.
func (g *gate) loadFiles() error {
	listing, err := g.git("ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	g.digests = map[string]string{}
	for _, name := range strings.Split(string(listing), "\x00") {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		g.files = append(g.files, name)
	}
	sort.Strings(g.files)
	var mu sync.Mutex
	var wg sync.WaitGroup
	work := make(chan string)
	for range max(2, runtime.NumCPU()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range work {
				digest := fileDigest(filepath.Join(g.root, filepath.FromSlash(name)))
				mu.Lock()
				g.digests[name] = digest
				mu.Unlock()
			}
		}()
	}
	for _, name := range g.files {
		work <- name
	}
	close(work)
	wg.Wait()
	return nil
}

// fileDigest identifies a file's content and kind. Missing tracked files,
// directories (submodule gitlinks) and symlinks hash distinctly from content.
func fileDigest(name string) string {
	info, err := os.Lstat(name)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "missing"
	case err != nil:
		return "error:" + err.Error()
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(name)
		if err != nil {
			return "error:" + err.Error()
		}
		return "symlink:" + target
	case info.IsDir():
		return "directory"
	}
	content, err := os.ReadFile(name)
	if err != nil {
		return "error:" + err.Error()
	}
	sum := sha256.Sum256(content)
	mode := "file"
	if info.Mode()&0111 != 0 {
		mode = "executable"
	}
	return mode + ":" + hex.EncodeToString(sum[:])
}

func selected(name string, inputs *Inputs) bool {
	match := func(patterns []string) bool {
		for _, pattern := range patterns {
			if strings.HasSuffix(pattern, "/") {
				if strings.HasPrefix(name, pattern) {
					return true
				}
				continue
			}
			if ok, _ := path.Match(pattern, name); ok {
				return true
			}
		}
		return false
	}
	if len(inputs.Paths) > 0 && !match(inputs.Paths) {
		return false
	}
	return !match(inputs.Exclude)
}

func (g *gate) key(step *Step) (string, error) {
	inputs := step.Inputs
	hash := sha256.New()
	fmt.Fprintf(hash, "%s\x00%s\x00%q\x00", cacheVersion, step.Name, step.Argv)
	files := 0
	for _, name := range g.files {
		if selected(name, inputs) {
			fmt.Fprintf(hash, "file\x00%s\x00%s\x00", name, g.digests[name])
			files++
		}
	}
	if files == 0 {
		return "", fmt.Errorf("%s: declared inputs select no files", step.Name)
	}
	for _, tool := range inputs.Tools {
		fmt.Fprintf(hash, "tool\x00%s\x00%s\x00", tool, g.toolVersion(tool))
	}
	for _, name := range inputs.Env {
		value, set := os.LookupEnv(name)
		fmt.Fprintf(hash, "env\x00%s\x00%t\x00%s\x00", name, set, value)
	}
	if inputs.Clean {
		state, err := g.committable()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "clean\x00%s\x00", state)
	}
	if inputs.Submodule != "" {
		state, err := g.submoduleState(inputs.Submodule)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "submodule\x00%s\x00%s\x00", inputs.Submodule, state)
	}
	for _, dir := range inputs.Installed {
		fmt.Fprintf(hash, "installed\x00%s\x00%s\x00", dir, g.installed(dir))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// installed digests an installed tree once per gate run.
func (g *gate) installed(dir string) string {
	g.installedMu.Lock()
	defer g.installedMu.Unlock()
	if g.installedDigests == nil {
		g.installedDigests = map[string]string{}
	}
	if digest, ok := g.installedDigests[dir]; ok {
		return digest
	}
	digest, err := installedDigest(filepath.Join(g.root, filepath.FromSlash(dir)), g.root)
	if err != nil {
		// An unreadable tree never matches a recorded pass.
		digest = fmt.Sprintf("error:%v:%d", err, time.Now().UnixNano())
	}
	g.installedDigests[dir] = digest
	return digest
}

// installedDigest hashes a tree's contents, following symbolic links (each
// directory once, by its resolved path) and hashing files concurrently. A
// link from the tree back into the repository, such as a workspace package,
// contributes only its target: the files there are repository inputs, and
// outputs Git ignores there are not inputs.
func installedDigest(root, repository string) (string, error) {
	if _, err := os.Lstat(root); errors.Is(err, os.ErrNotExist) {
		return "absent", nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedRepository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return "", err
	}
	within := func(name, dir string) bool {
		relative, err := filepath.Rel(dir, name)
		return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	}
	type file struct{ relative, path string }
	var entries []string
	var files []file
	visited := map[string]bool{}
	var walk func(relative, name string) error
	walk = func(relative, name string) error {
		info, err := os.Lstat(name)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(name)
			if err != nil {
				return err
			}
			entries = append(entries, "link\x00"+relative+"\x00"+target)
			resolved, err := filepath.EvalSymlinks(name)
			if err != nil {
				entries = append(entries, "dangling\x00"+relative)
				return nil
			}
			if within(resolved, resolvedRepository) && !within(resolved, resolvedRoot) {
				return nil
			}
			if info, err = os.Stat(name); err != nil {
				return err
			}
		}
		switch {
		case info.IsDir():
			resolved, err := filepath.EvalSymlinks(name)
			if err != nil {
				return err
			}
			if visited[resolved] {
				entries = append(entries, "seen\x00"+relative)
				return nil
			}
			visited[resolved] = true
			children, err := os.ReadDir(name)
			if err != nil {
				return err
			}
			entries = append(entries, "dir\x00"+relative)
			for _, child := range children {
				if err := walk(path.Join(relative, child.Name()), filepath.Join(name, child.Name())); err != nil {
					return err
				}
			}
		case info.Mode().IsRegular():
			entries = append(entries, fmt.Sprintf("file\x00%s\x00%v", relative, info.Mode().Perm()))
			files = append(files, file{relative, name})
		default:
			entries = append(entries, fmt.Sprintf("other\x00%s\x00%v", relative, info.Mode()))
		}
		return nil
	}
	if err := walk(".", root); err != nil {
		return "", err
	}
	sums := make([]string, len(files))
	errs := make([]error, len(files))
	var wg sync.WaitGroup
	work := make(chan int)
	for range max(2, runtime.NumCPU()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range work {
				content, err := os.ReadFile(files[index].path)
				sum := sha256.Sum256(content)
				sums[index], errs[index] = hex.EncodeToString(sum[:]), err
			}
		}()
	}
	for index := range files {
		work <- index
	}
	close(work)
	wg.Wait()
	hash := sha256.New()
	for _, entry := range entries {
		fmt.Fprintf(hash, "%s\x00", entry)
	}
	for index, f := range files {
		if errs[index] != nil {
			return "", errs[index]
		}
		fmt.Fprintf(hash, "%s\x00%s\x00", f.relative, sums[index])
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// toolVersion fingerprints an executable on PATH; an absent tool is a
// distinct input, so a step that degrades without it never reuses a pass
// recorded with it.
func (g *gate) toolVersion(tool string) string {
	g.toolsMu.Lock()
	defer g.toolsMu.Unlock()
	if g.tools == nil {
		g.tools = map[string]string{}
	}
	if version, ok := g.tools[tool]; ok {
		return version
	}
	var argv []string
	switch tool {
	case "go":
		argv = []string{"go", "env", "GOVERSION", "GOOS", "GOARCH", "GOAMD64", "CGO_ENABLED", "GOEXPERIMENT", "GOFLAGS", "GOTOOLCHAIN", "GOWORK", "GODEBUG"}
	default:
		argv = []string{tool, "--version"}
	}
	version := "absent"
	if _, err := exec.LookPath(argv[0]); err == nil {
		output, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		version = strings.TrimSpace(string(output))
		if err != nil {
			version = "failed: " + version
		}
	}
	g.tools[tool] = version
	return version
}

// committable reports whether the tree has no unstaged changes to tracked
// files and no untracked, non-ignored files.
func (g *gate) committable() (string, error) {
	g.factsMu.Lock()
	defer g.factsMu.Unlock()
	if g.clean != "" {
		return g.clean, nil
	}
	unstaged, err := g.git("diff", "--name-only", "-z")
	if err != nil {
		return "", err
	}
	untracked, err := g.git("ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return "", err
	}
	g.clean = fmt.Sprintf("unstaged=%t untracked=%t", len(unstaged) > 0, len(untracked) > 0)
	return g.clean, nil
}

func (g *gate) submoduleState(dir string) (string, error) {
	g.factsMu.Lock()
	defer g.factsMu.Unlock()
	if g.modules == nil {
		g.modules = map[string]string{}
	}
	if state, ok := g.modules[dir]; ok {
		return state, nil
	}
	checkout := filepath.Join(g.root, filepath.FromSlash(dir))
	state := "absent"
	if head, err := exec.Command("git", "-C", checkout, "rev-parse", "HEAD").Output(); err == nil {
		status, err := exec.Command("git", "-C", checkout, "status", "--porcelain=v1", "-z", "--untracked-files=no", "--ignore-submodules=all").Output()
		if err != nil {
			return "", fmt.Errorf("submodule %s status: %w", dir, err)
		}
		ignoreStat, _ := exec.Command("git", "-C", checkout, "config", "--get-all", "core.ignoreStat").Output()
		flags, err := exec.Command("git", "-C", checkout, "ls-files", "-z", "-v").Output()
		if err != nil {
			return "", fmt.Errorf("submodule %s index flags: %w", dir, err)
		}
		sum := sha256.Sum256(bytes.Join([][]byte{head, status, ignoreStat, flags}, []byte{0}))
		state = hex.EncodeToString(sum[:])
	}
	g.modules[dir] = state
	return state, nil
}

func (g *gate) git(args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = g.root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func (g *gate) receiptPath(key string) string {
	return filepath.Join(g.cacheDir, cacheVersion, key[:2], key+".json")
}

func (g *gate) lookup(key string) (receipt, bool) {
	var recorded receipt
	data, err := os.ReadFile(g.receiptPath(key))
	if err != nil || json.Unmarshal(data, &recorded) != nil || recorded.Key != key {
		return receipt{}, false
	}
	return recorded, true
}

// record stores a pass atomically; a lost write only costs a rerun.
func (g *gate) record(r receipt) {
	name := g.receiptPath(r.Key)
	if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
		return
	}
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(name), ".receipt-")
	if err != nil {
		return
	}
	_, writeErr := temporary.Write(data)
	closeErr := temporary.Close()
	if writeErr != nil || closeErr != nil || os.Rename(temporary.Name(), name) != nil {
		_ = os.Remove(temporary.Name())
	}
}

var testFunction = regexp.MustCompile(`(?m)^func ((?:Test|Fuzz|Example)[A-Za-z0-9_]*)\(`)

// testNames lists a package directory's top-level test, fuzz and example
// functions. It only balances shards: completeness never depends on it.
func testNames(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var names []string
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		for _, match := range testFunction.FindAllSubmatch(content, -1) {
			name := string(match[1])
			if name == "TestMain" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// shard partitions names into n groups by longest-processing-time-first over
// recorded durations. Unknown tests weigh the median, so a new test does not
// reshuffle the others.
func shard(names []string, durations map[string]float64, n int) [][]string {
	known := make([]float64, 0, len(durations))
	for _, name := range names {
		if d, ok := durations[name]; ok {
			known = append(known, d)
		}
	}
	sort.Float64s(known)
	fallback := 0.1
	if len(known) > 0 {
		fallback = known[len(known)/2]
	}
	weight := func(name string) float64 {
		if d, ok := durations[name]; ok {
			return d
		}
		return fallback
	}
	ordered := append([]string(nil), names...)
	sort.SliceStable(ordered, func(i, j int) bool {
		wi, wj := weight(ordered[i]), weight(ordered[j])
		if wi != wj {
			return wi > wj
		}
		return ordered[i] < ordered[j]
	})
	groups := make([][]string, n)
	loads := make([]float64, n)
	for _, name := range ordered {
		lightest := 0
		for i := 1; i < n; i++ {
			if loads[i] < loads[lightest] {
				lightest = i
			}
		}
		groups[lightest] = append(groups[lightest], name)
		loads[lightest] += weight(name)
	}
	for _, group := range groups {
		sort.Strings(group)
	}
	return groups
}

func exactNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = regexp.QuoteMeta(name)
	}
	return "^(" + strings.Join(quoted, "|") + ")$"
}

// shardArgs returns the go test selection flags for each shard. Shards before
// the last run exactly their names; the last skips exactly those names, so
// every test the package defines runs in exactly one shard even if testNames
// missed it.
func shardArgs(groups [][]string) [][]string {
	var args [][]string
	var assigned []string
	for i, group := range groups {
		if i == len(groups)-1 {
			if len(assigned) == 0 {
				args = append(args, nil)
			} else {
				args = append(args, []string{"-skip", exactNames(assigned)})
			}
			break
		}
		if len(group) == 0 {
			continue
		}
		args = append(args, []string{"-run", exactNames(group)})
		assigned = append(assigned, group...)
	}
	return args
}
