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

func TestCommittabilityAndToolsAreInputs(t *testing.T) {
	root := fixture(t)
	g := &gate{root: root, cacheDir: t.TempDir()}
	key := func(inputs *Inputs) string {
		t.Helper()
		g.files, g.digests, g.clean, g.tools = nil, nil, "", nil
		if err := g.loadFiles(); err != nil {
			t.Fatal(err)
		}
		k, err := g.key(&Step{Name: "s", Argv: []string{"true"}, Inputs: inputs})
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	clean := &Inputs{Paths: []string{"src/"}, Clean: true}
	before := key(clean)
	write(t, root, "scratch.txt", "untracked\n")
	if key(clean) == before {
		t.Fatal("an untracked file did not change a committability-keyed step")
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
