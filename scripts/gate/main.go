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
	"slices"
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
	// JavaScript tools Go tests spawn. Their fingerprints, with the environment
	// they and the Go programs a test runs read (testToolEnvironment,
	// goRuntimeEnvironment), reach the step as one digest in
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
	// Tools names executables whose fingerprint (toolVersion) is an input.
	Tools []string
	// Env names environment variables whose values are inputs.
	Env []string
	// Submodule makes a submodule's checked-out commit and status an input.
	Submodule string
	// Installed names directories outside Git's view, such as an installed
	// node_modules, whose contents are an input: every file's path,
	// permissions and content and every symlink's target, following links
	// as Node and Bun resolve them. An absent directory is a distinct input.
	Installed []string
	// Generated names files or directories that a step this one Needs
	// writes during the run, such as emitted artifacts. Each is digested
	// like Installed, but when this step's key is computed: Installed trees
	// are digested once per run because nothing writes them.
	Generated []string
	// Trees names directories a step walks on disk itself, as the
	// framework-port capture check walks its vendored tree, so every entry
	// there is an input, files Git ignores included. Each is digested like
	// Generated, when this step's key is computed, and a link out of it into
	// the repository is followed. TreeExclude names repository paths under
	// them that are not inputs: trees declared separately, and directories
	// the step empties before it reads them.
	Trees       []string
	TreeExclude []string
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
	// begun is when the step started, from the start of the run, so a
	// log shows the critical path and not only each step's own time.
	begun time.Duration
	note  string
}

type receipt struct {
	Step     string    `json:"step"`
	Key      string    `json:"key"`
	Duration float64   `json:"durationSeconds"`
	Passed   time.Time `json:"passedAt"`
	Output   string    `json:"output"`
}

type gate struct {
	started  time.Time
	root     string
	cacheDir string
	noCache  bool
	steps    []*Step
	files    []string
	digests  map[string]string
	tools    map[string]*toolFact
	toolsMu  sync.Mutex
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
	// The second line binds the log to the checkout it checked. A dirty
	// checkout still runs, and says so.
	identity, err := checkoutIdentity(root)
	if err != nil {
		fatal(err)
	}
	fmt.Println(identity)
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

// checkoutIdentity names the commit and tree the gate starts from, and
// whether the checkout matches them: no staged, unstaged or untracked
// (non-ignored) changes, submodules included.
func checkoutIdentity(root string) (string, error) {
	git := func(args ...string) (string, error) {
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return string(output), nil
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	tree, err := git("rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", err
	}
	// Submodule ignore settings in .gitmodules or config cannot hide a
	// change: the reported cleanliness covers every submodule.
	status, err := git("status", "--porcelain=v1", "-z", "--untracked-files=normal", "--ignore-submodules=none")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("gate: head %s tree %s clean=%t", strings.TrimSpace(head), strings.TrimSpace(tree), status == ""), nil
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
	g.started = started
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
	// The prefetches below are joined before run returns, so no digest or
	// fingerprint work outlives the run that started it.
	var prefetch sync.WaitGroup
	defer prefetch.Wait()
	for _, step := range g.steps {
		if len(step.ToolEnv) > 0 {
			for _, dir := range testInstalled {
				prefetch.Go(func() { g.installed(dir) })
			}
			break
		}
	}
	// Start every tool fingerprint while files load; each is computed once
	// per run, so a step that names a tool finds it ready or waits for it.
	prefetched := map[string]bool{}
	for _, step := range g.steps {
		tools := step.ToolEnv
		if step.Inputs != nil {
			tools = append(slices.Clone(step.Inputs.Tools), tools...)
		}
		for _, tool := range tools {
			if !prefetched[tool] {
				prefetched[tool] = true
				prefetch.Go(func() { g.toolVersion(tool) })
			}
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
			fmt.Fprintf(out, "== %s: ok (%.1fs, cpu %.1fs, from %.1fs)\n", step.Name, r.duration.Seconds(), r.cpu.Seconds(), r.begun.Seconds())
		case cached:
			fmt.Fprintf(out, "== %s: ok, cached (%s)\n", step.Name, r.note)
		case failed:
			fmt.Fprintf(out, "== %s: FAILED (%.1fs, cpu %.1fs, from %.1fs)\n", step.Name, r.duration.Seconds(), r.cpu.Seconds(), r.begun.Seconds())
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
		command.Env = append(command.Env, "EFFRA_TOOL_VERSIONS="+g.toolDigest(step))
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	r := &result{outcome: passed, output: output.Bytes(), duration: time.Since(started), begun: started.Sub(g.started)}
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

// toolDigest identifies, for a step's own cache, the tools it names in
// ToolEnv, the environment they and the Go programs they run read, and the
// installed trees they load.
func (g *gate) toolDigest(step *Step) string {
	digest := sha256.New()
	for _, tool := range step.ToolEnv {
		fmt.Fprintf(digest, "%s\x00%s\x00", tool, g.toolVersion(tool))
	}
	for _, name := range append(testToolEnvironment, goRuntimeEnvironment...) {
		value, set := stepVariable(name)
		fmt.Fprintf(digest, "env\x00%s\x00%t\x00%s\x00", name, set, value)
	}
	for _, dir := range testInstalled {
		fmt.Fprintf(digest, "installed\x00%s\x00%s\x00", dir, g.installed(dir))
	}
	return hex.EncodeToString(digest.Sum(nil))[:16]
}

// stepEnvironment is the environment every step runs with: the caller's,
// without the variables that would select an unkeyed executable, and with
// the gate's collector target unless the caller sets one.
func stepEnvironment() []string {
	var environment []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(unpinnedToolEnvironment, name) {
			environment = append(environment, entry)
		}
	}
	if _, set := os.LookupEnv("GOGC"); !set {
		environment = append(environment, "GOGC="+gcPercent)
	}
	return environment
}

// stepVariable reads a variable as steps see it, so a key names the value a
// step runs with rather than the caller's.
func stepVariable(name string) (string, bool) {
	environment := stepEnvironment()
	for i := len(environment) - 1; i >= 0; i-- {
		if value, ok := strings.CutPrefix(environment[i], name+"="); ok {
			return value, true
		}
	}
	return "", false
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

// keyedPermissions are the permission bits that can change a step's result.
// Steps run as the user who owns the checkout, so the kernel grants their
// access from the owner bits alone: owner-read decides whether a step can
// read an input. A cached step never writes its inputs, so owner-write cannot
// change what it reads. The three executable bits are data a check can read,
// as the capture check reads the owner's, and they decide what a step can
// execute. Group and other read and write bits are consulted for no process
// a step starts and asserted by no check, so keying them would only split
// receipts between checkouts made under different umasks.
const keyedPermissions os.FileMode = 0o511

// fileDigest identifies a file's content, kind and keyed permission bits.
// Missing tracked files, directories (submodule gitlinks) and symlinks hash
// distinctly from content.
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
	return fmt.Sprintf("file:%v:%s", info.Mode().Perm()&keyedPermissions, hex.EncodeToString(sum[:]))
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
		value, set := stepVariable(name)
		fmt.Fprintf(hash, "env\x00%s\x00%t\x00%s\x00", name, set, value)
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
	for _, name := range inputs.Generated {
		digest, err := installedDigest(filepath.Join(g.root, filepath.FromSlash(name)), g.root, false)
		if err != nil {
			return "", fmt.Errorf("digest generated %s: %w", name, err)
		}
		fmt.Fprintf(hash, "generated\x00%s\x00%s\x00", name, digest)
	}
	for _, tree := range inputs.Trees {
		var skip []string
		for _, excluded := range inputs.TreeExclude {
			if relative, ok := strings.CutPrefix(excluded, strings.TrimSuffix(tree, "/")+"/"); ok {
				skip = append(skip, strings.TrimSuffix(relative, "/"))
			}
		}
		digest, err := installedDigest(filepath.Join(g.root, filepath.FromSlash(tree)), g.root, true, skip...)
		if err != nil {
			return "", fmt.Errorf("digest tree %s: %w", tree, err)
		}
		fmt.Fprintf(hash, "tree\x00%s\x00%q\x00%s\x00", tree, skip, digest)
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
	digest, err := installedDigest(filepath.Join(g.root, filepath.FromSlash(dir)), g.root, false)
	if err != nil {
		// An unreadable tree never matches a recorded pass.
		digest = fmt.Sprintf("error:%v:%d", err, time.Now().UnixNano())
	}
	g.installedDigests[dir] = digest
	return digest
}

// installedDigest hashes a tree's contents, following symbolic links (each
// directory once, by its resolved path) and hashing files concurrently. A
// link from an installed tree back into the repository, such as a workspace
// package, contributes only its target: a step whose outcome depends on the
// files there declares them, through Paths or, when it reads files Git
// ignores, Trees. With followRepository, as for a tree input, the walk
// follows such a link too, so editing its target changes the digest. skip
// names paths relative to root that the walk leaves out. A file contributes
// its keyed permission bits.
func installedDigest(root, repository string, followRepository bool, skip ...string) (string, error) {
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
		if slices.Contains(skip, relative) {
			return nil
		}
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
			if !followRepository && within(resolved, resolvedRepository) && !within(resolved, resolvedRoot) {
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
			entries = append(entries, fmt.Sprintf("file\x00%s\x00%v", relative, info.Mode().Perm()&keyedPermissions))
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
