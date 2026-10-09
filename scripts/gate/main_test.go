package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// fixture is a git repository with a counter script: each run of `count NAME`
// appends a line to runs/NAME, so a test can tell an execution from a replay.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-c", "user.name=gate", "-c", "user.email=gate@invalid", "-c", "commit.gpgsign=false"}, args...)...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	git("init", "-q")
	write(t, root, ".gitignore", "/runs/\n")
	write(t, root, "src/input.txt", "one\n")
	write(t, root, "docs/notes.md", "notes\n")
	git("add", ".")
	git("commit", "-qm", "fixture")
	return root
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func count(name string) []string {
	return []string{"sh", "-c", `mkdir -p runs && echo run >> runs/"$1" && echo "ran $1"`, "count", name}
}

func runs(t *testing.T, root, name string) int {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, "runs", name))
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(content), "run\n")
}

func runGate(t *testing.T, root, cache string, steps []*Step) (int, string) {
	t.Helper()
	var out bytes.Buffer
	g := &gate{root: root, cacheDir: cache, steps: steps}
	return g.run(&out), out.String()
}

func TestFailingStepFailsTheGateAndBlocksOnlyItsDependents(t *testing.T) {
	root := fixture(t)
	steps := []*Step{
		{Name: "broken", Argv: []string{"sh", "-c", "echo broken output; exit 3"}},
		{Name: "dependent", Needs: []string{"broken"}, Argv: count("dependent")},
		{Name: "independent", Argv: count("independent")},
	}
	code, out := runGate(t, root, t.TempDir(), steps)
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	for _, want := range []string{"== broken: FAILED", "broken output", "== dependent: not run (needs broken)", "== independent: ok", "gate: FAILED in", ": broken, dependent\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if runs(t, root, "dependent") != 0 || runs(t, root, "independent") != 1 {
		t.Fatalf("dependent ran %d times, independent %d", runs(t, root, "dependent"), runs(t, root, "independent"))
	}
}

func TestOutputFollowsDeclarationOrderNotCompletionOrder(t *testing.T) {
	root := fixture(t)
	steps := []*Step{
		{Name: "slow", Argv: []string{"sh", "-c", "sleep 0.3; echo slow done"}},
		{Name: "fast", Argv: []string{"sh", "-c", "echo fast done"}},
	}
	code, out := runGate(t, root, t.TempDir(), steps)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if regexp.MustCompile(`(?s)== slow: ok.*slow done.*== fast: ok.*fast done.*gate: passed 2 steps`).FindString(out) == "" {
		t.Fatalf("output out of declaration order:\n%s", out)
	}
}

func TestCachedStepReplaysOnlyWhileItsInputsAreUnchanged(t *testing.T) {
	root := fixture(t)
	cache := t.TempDir()
	steps := func() []*Step {
		return []*Step{{Name: "checked", Argv: count("checked"), Inputs: &Inputs{Paths: []string{"src/"}}}}
	}
	expect := func(executions int, cached bool) {
		t.Helper()
		code, out := runGate(t, root, cache, steps())
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, out)
		}
		if got := runs(t, root, "checked"); got != executions {
			t.Fatalf("executions = %d, want %d\n%s", got, executions, out)
		}
		if strings.Contains(out, "ok, cached") != cached || !strings.Contains(out, "ran checked") {
			t.Fatalf("cached = %t, want %t (the receipt replays the output)\n%s", !cached, cached, out)
		}
	}
	expect(1, false)
	expect(1, true)
	write(t, root, "docs/notes.md", "unrelated edit\n")
	expect(1, true)
	write(t, root, "src/input.txt", "two\n")
	expect(2, false)
	expect(2, true)
	write(t, root, "src/new.txt", "an untracked input\n")
	expect(3, false)
	if err := os.Remove(filepath.Join(root, "src/new.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "src/input.txt", "one\n")
	expect(3, true)
}

func TestFailuresAreNeverCached(t *testing.T) {
	root := fixture(t)
	cache := t.TempDir()
	step := func() []*Step {
		return []*Step{{Name: "flaky", Argv: []string{"sh", "-c", `mkdir -p runs && echo run >> runs/flaky && test -f pass`}, Inputs: &Inputs{Paths: []string{"src/"}}}}
	}
	if code, out := runGate(t, root, cache, step()); code != 1 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if code, out := runGate(t, root, cache, step()); code != 1 || runs(t, root, "flaky") != 2 {
		t.Fatalf("failure replayed: exit %d, runs %d\n%s", code, runs(t, root, "flaky"), out)
	}
}

func TestToolsAreInputs(t *testing.T) {
	root := fixture(t)
	g := &gate{root: root, cacheDir: t.TempDir()}
	key := func(inputs *Inputs) string {
		t.Helper()
		g.files, g.digests, g.tools = nil, nil, nil
		if err := g.loadFiles(); err != nil {
			t.Fatal(err)
		}
		k, err := g.key(&Step{Name: "s", Argv: []string{"true"}, Inputs: inputs})
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	if key(&Inputs{Paths: []string{"src/"}}) != key(&Inputs{Paths: []string{"src/"}}) {
		t.Fatal("keys are not deterministic")
	}
	if key(&Inputs{Paths: []string{"src/"}, Tools: []string{"definitely-absent-tool"}}) == key(&Inputs{Paths: []string{"src/"}}) {
		t.Fatal("an absent tool is not an input")
	}
	if _, err := g.key(&Step{Name: "s", Inputs: &Inputs{Paths: []string{"nothing/"}}}); err == nil {
		t.Fatal("inputs that select no files were accepted")
	}
}

func TestShardsRunEveryTestExactlyOnce(t *testing.T) {
	names := []string{"TestA", "TestB", "TestC", "TestD", "TestE"}
	durations := map[string]float64{"TestA": 10, "TestB": 1, "TestC": 1, "TestD": 8}
	groups := shard(names, durations, 3)
	selections := shardArgs(groups)
	if len(selections) != 3 || selections[2][0] != "-skip" {
		t.Fatalf("selections = %q", selections)
	}
	// TestUnlisted stands for a test the source scan missed: only the last
	// shard's skip-complement may run it, and it must.
	for _, test := range append(names, "TestUnlisted") {
		matched := 0
		for _, selection := range selections {
			pattern := regexp.MustCompile(selection[1])
			if (selection[0] == "-run") == pattern.MatchString(test) {
				matched++
			}
		}
		if matched != 1 {
			t.Fatalf("%s runs in %d shards: %q", test, matched, selections)
		}
	}
	if groups[0][0] != "TestA" || len(groups[0]) != 1 {
		t.Fatalf("the heaviest test should run alone: %q", groups)
	}
}

func TestTestNamesFindTopLevelTestsOnly(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a_test.go", "package a\n\nfunc TestMain(m *testing.M) {}\nfunc TestOne(t *testing.T) {}\nfunc helper() {}\nfunc (s suite) TestMethod(t *testing.T) {}\nfunc ExampleTwo() {}\nfunc FuzzThree(f *testing.F) {}\n")
	write(t, dir, "a.go", "package a\n\nfunc TestNotATestFile() {}\n")
	names, err := testNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "ExampleTwo,FuzzThree,TestOne" {
		t.Fatalf("names = %q", names)
	}
}

// keyOf computes a step key from a fresh view of the fixture.
func keyOf(t *testing.T, root string, inputs *Inputs) string {
	t.Helper()
	g := &gate{root: root, cacheDir: t.TempDir()}
	if err := g.loadFiles(); err != nil {
		t.Fatal(err)
	}
	k, err := g.key(&Step{Name: "s", Argv: []string{"true"}, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestInstalledTreeContentsAreInputs(t *testing.T) {
	root := fixture(t)
	write(t, root, ".gitignore", "/runs/\n/deps/\n/src/out/\n")
	write(t, root, "deps/pkg/index.js", "export const one = 1\n")
	write(t, root, "deps/.store/lib@1/lib.js", "export const lib = 1\n")
	if err := os.Symlink(".store/lib@1", filepath.Join(root, "deps/lib")); err != nil {
		t.Fatal(err)
	}
	// A workspace link back into the repository contributes only its target.
	if err := os.Symlink("../src", filepath.Join(root, "deps/workspace")); err != nil {
		t.Fatal(err)
	}
	inputs := &Inputs{Paths: []string{"docs/"}, Installed: []string{"deps"}}
	before := keyOf(t, root, inputs)
	if keyOf(t, root, inputs) != before {
		t.Fatal("installed keys are not deterministic")
	}

	// Same size and modification time, different bytes.
	file := filepath.Join(root, "deps/pkg/index.js")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "deps/pkg/index.js", "export const one = 2\n")
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	edited := keyOf(t, root, inputs)
	if edited == before {
		t.Fatal("a content edit that keeps size and mtime replayed the installed key")
	}
	write(t, root, "deps/.store/lib@1/lib.js", "export const lib = 2\n")
	linked := keyOf(t, root, inputs)
	if linked == edited {
		t.Fatal("an edit behind an internal symlink did not change the installed key")
	}
	if err := os.Chmod(file, 0o755); err != nil {
		t.Fatal(err)
	}
	moded := keyOf(t, root, inputs)
	if moded == linked {
		t.Fatal("a permission change did not change the installed key")
	}
	write(t, root, "src/out/generated.js", "ignored output\n")
	if keyOf(t, root, inputs) != moded {
		t.Fatal("an ignored output behind a workspace link changed the installed key")
	}
	if err := os.RemoveAll(filepath.Join(root, "deps")); err != nil {
		t.Fatal(err)
	}
	if keyOf(t, root, inputs) == moded {
		t.Fatal("removing the installed tree did not change the key")
	}
}

func TestToolDigestCoversInstalledTestDependencies(t *testing.T) {
	root := fixture(t)
	write(t, root, ".gitignore", "/runs/\n/node_modules/\n")
	write(t, root, "node_modules/effect/index.js", "export const one = 1\n")
	steps := func() []*Step {
		return []*Step{{Name: "digest", Argv: []string{"sh", "-c", `echo "tools=$EFFRA_TOOL_VERSIONS"`}, ToolEnv: []string{"sh"}}}
	}
	digest := func() string {
		t.Helper()
		code, out := runGate(t, root, t.TempDir(), steps())
		if code != 0 {
			t.Fatalf("exit %d\n%s", code, out)
		}
		match := regexp.MustCompile(`tools=([0-9a-f]{16})`).FindStringSubmatch(out)
		if match == nil {
			t.Fatalf("no tool digest in\n%s", out)
		}
		return match[1]
	}
	before := digest()
	file := filepath.Join(root, "node_modules/effect/index.js")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "node_modules/effect/index.js", "export const one = 2\n")
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if digest() == before {
		t.Fatal("an installed dependency edit left EFFRA_TOOL_VERSIONS unchanged")
	}
}

func TestEnvironmentInputsAreInputs(t *testing.T) {
	root := fixture(t)
	inputs := &Inputs{Paths: []string{"src/"}, Env: []string{"EFFRA_GATE_ENV_INPUT"}}
	t.Setenv("EFFRA_GATE_ENV_INPUT", "1")
	set := keyOf(t, root, inputs)
	os.Unsetenv("EFFRA_GATE_ENV_INPUT")
	if keyOf(t, root, inputs) == set {
		t.Fatal("a declared environment variable did not join the key")
	}
}

// A step that reads Git history declares no inputs, so it can never replay a
// pass recorded while history it needs was present. The control removes only
// a commit's root tree and keeps the commit, as a partial object store would.
func TestHistoryReadingStepsNeverReplay(t *testing.T) {
	root := fixture(t)
	git := func(args ...string) string {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	commit := git("rev-parse", "HEAD")
	tree := git("rev-parse", "HEAD^{tree}")
	reader := []*Step{{Name: "history", Argv: []string{"sh", "-c", `mkdir -p runs && echo run >> runs/history && git ls-tree "$1" -- src/input.txt`, "history", commit}}}
	cache := t.TempDir()
	for run := 1; run <= 2; run++ {
		if code, out := runGate(t, root, cache, reader); code != 0 || strings.Contains(out, "cached") || runs(t, root, "history") != run {
			t.Fatalf("run %d: exit %d, executions %d\n%s", run, code, runs(t, root, "history"), out)
		}
	}
	if err := os.Remove(filepath.Join(root, ".git", "objects", tree[:2], tree[2:])); err != nil {
		t.Fatal(err)
	}
	git("cat-file", "-e", commit+"^{commit}")
	code, out := runGate(t, root, cache, reader)
	if code != 1 || runs(t, root, "history") != 3 || !strings.Contains(out, "== history: FAILED") {
		t.Fatalf("a missing root tree did not fail the history reader: exit %d\n%s", code, out)
	}
}

func TestWayfinderSnapshotCheckKeysOnTheSnapshotSourcesAndGo(t *testing.T) {
	inputs := wayfinderSnapshotInputs()
	for name, want := range map[string]bool{
		"docs/wayfinder/snapshot.json": true, "cmd/wayfinder/model.go": true, "go.mod": true,
		"cmd/wayfinder/main_test.go": false, "docs/wayfinder/README.md": false,
	} {
		if selected(name, inputs) != want {
			t.Errorf("selected(%s) = %t, want %t", name, !want, want)
		}
	}
	root := fixture(t)
	write(t, root, "docs/wayfinder/snapshot.json", "{}\n")
	write(t, root, "cmd/wayfinder/main.go", "package main\n")
	withoutGo := *inputs
	withoutGo.Tools = nil
	if keyOf(t, root, inputs) == keyOf(t, root, &withoutGo) {
		t.Fatal("the Go version did not join the key")
	}
	before := keyOf(t, root, inputs)
	write(t, root, "docs/wayfinder/snapshot.json", "{\"changed\": true}\n")
	if keyOf(t, root, inputs) == before {
		t.Fatal("a snapshot edit replayed the recorded pass")
	}
}
