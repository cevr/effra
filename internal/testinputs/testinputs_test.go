package testinputs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// This package is the one test package that does not call Main: its
// end-to-end cases run observed test binaries, and an observed process
// cannot observe its own children (a nested listener hides their calls from
// the outer one, which then refuses the result). Go's test cache keys its
// result on what this process reads itself: the sources the fixtures copy
// and ToolDigest, which stands for the tools the fixtures run. The guard
// (internal/testinputs/guard) records the exemption.

func TestTestLog(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"-test.testlogfile=/b/testlog.txt"}, "/b/testlog.txt"},
		{[]string{"--test.testlogfile=/b/testlog.txt"}, "/b/testlog.txt"},
		{[]string{"-test.v", "-test.testlogfile", "/b/t.txt", "-test.run=x"}, "/b/t.txt"},
		{[]string{"-test.testlogfile"}, ""},
		{[]string{"-test.paniconexit0", "--", "-test.testlogfile=/b/t.txt"}, ""},
		{[]string{"-test.testlogfilex=/b/t.txt"}, ""},
		{[]string{"-test.testlogfile=/a dir/with=equals.txt"}, "/a dir/with=equals.txt"},
	} {
		if got := testLog(c.args); got != c.want {
			t.Errorf("testLog(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}

// parse reads test-log lines the way cmd/go does
// (cmd/go/internal/test.computeTestInputsID): the operation is everything
// before the first space and the name everything after it, without quoting
// or escapes. It fails on a line cmd/go would reject.
func parse(t *testing.T, lines []string) [][2]string {
	t.Helper()
	var entries [][2]string
	for _, line := range lines {
		body, ok := strings.CutSuffix(line, "\n")
		if !ok || strings.Contains(body, "\n") {
			t.Fatalf("line %q is not exactly one log line", line)
		}
		op, name, found := strings.Cut(body, " ")
		if !found || !slices.Contains([]string{"getenv", "stat", "open"}, op) {
			t.Fatalf("cmd/go rejects the line %q", line)
		}
		entries = append(entries, [2]string{op, name})
	}
	return entries
}

// linkedModule makes a module whose logical root is a link to its physical
// directory, and a directory outside it with a link into it.
func linkedModule(t *testing.T) (logical, physical, outside string) {
	t.Helper()
	base := t.TempDir()
	physical = filepath.Join(base, "physical")
	logical = filepath.Join(base, "logical")
	outside = filepath.Join(base, "outside")
	for _, dir := range []string{filepath.Join(physical, "src"), filepath.Join(physical, "bin"), outside} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(physical, logical); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(physical, "src"), filepath.Join(outside, "src")); err != nil {
		t.Fatal(err)
	}
	return logical, physical, outside
}

func TestRecorderKeepsAccessesInsideTheRoot(t *testing.T) {
	logical, physical, outside := linkedModule(t)
	r := newRecorder(logical)
	r.open(filepath.Join(logical, "src/a.ts"), true)
	r.open(filepath.Join(physical, "src/b.ts"), true) // physical name, logical entry
	r.stat(filepath.Join(outside, "src/new.ts"))      // a link from outside, resolved
	r.stat(filepath.Join(outside, "src/no/dir/x.ts")) // missing directories behind the link
	r.stat(filepath.Join(outside, "missing/x.ts"))    // outside, through no link
	r.open(filepath.Join(logical, "out.txt"), false)  // write-only: existence only
	r.stat(filepath.Join(logical, "src/a.ts"))        // opened and looked up: both kept
	r.open(logical+"/src//./c.ts", true)              // cleaned
	r.open(logical+"/src/../src/d.ts", true)          // ".." kept for the kernel to resolve
	r.open("/proc/self/status", true)                 // never rechecked
	r.open("/dev/null", true)                         //
	r.stat("/sys/kernel")                             //
	r.open("/usr/lib/libc.so.6", true)                // outside the root
	r.open(logical, true)                             // the root listed
	// A file outside that links in, and ".." applied after a link, as the
	// kernel applies it: outside/src/.. is the physical root, not outside.
	if err := os.Symlink(filepath.Join(physical, "src/e.ts"), filepath.Join(outside, "e.ts")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(physical, "src/e.ts"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.open(filepath.Join(outside, "e.ts"), true)
	r.stat(outside + "/src/../g.ts")
	// A dangling link from outside into the root: the target's absence is
	// the input.
	if err := os.Symlink(filepath.Join(physical, "gone/h.ts"), filepath.Join(outside, "h.ts")); err != nil {
		t.Fatal(err)
	}
	r.open(filepath.Join(outside, "h.ts"), true)
	got := parse(t, r.lines())
	want := [][2]string{
		{"open", logical},
		{"open", logical + "/gone/h.ts"},
		{"open", logical + "/src/../src/d.ts"},
		{"open", logical + "/src/a.ts"},
		{"open", logical + "/src/b.ts"},
		{"open", logical + "/src/c.ts"},
		{"open", logical + "/src/e.ts"},
		{"stat", logical + "/out.txt"},
		{"stat", logical + "/src/../g.ts"},
		{"stat", logical + "/src/a.ts"},
		{"stat", logical + "/src/new.ts"},
		{"stat", logical + "/src/no/dir/x.ts"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries:\n got %q\nwant %q", got, want)
	}
	if r.reason() != "" {
		t.Fatalf("failure %q", r.reason())
	}
}

// TestRecorderSeesChangesOutsideTheRoot checks that nothing the observed
// tree can change outside the root is remembered between calls: a link
// retargeted mid-run, and a read-only directory of this user's made
// writable and given a link into the root.
func TestRecorderSeesChangesOutsideTheRoot(t *testing.T) {
	logical, physical, outside := linkedModule(t)
	for _, dir := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(physical, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	current := filepath.Join(outside, "current")
	if err := os.Symlink(filepath.Join(physical, "a"), current); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(outside, "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	r := newRecorder(logical)
	r.open(filepath.Join(current, "value.txt"), true)
	r.open(filepath.Join(locked, "in", "value.txt"), true)
	if err := os.Remove(current); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(physical, "b"), current); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(physical, "src"), filepath.Join(locked, "in")); err != nil {
		t.Fatal(err)
	}
	r.open(filepath.Join(current, "value.txt"), true)
	r.open(filepath.Join(locked, "in", "value.txt"), true)
	got := parse(t, r.lines())
	want := [][2]string{
		{"open", logical + "/a/value.txt"},
		{"open", logical + "/b/value.txt"},
		{"open", logical + "/src/value.txt"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries:\n got %q\nwant %q", got, want)
	}
}

// TestRecorderRemembersOnlySealedDirectories checks the memo's rule: what a
// lookup finds is remembered only in a directory that another user owns and
// this user cannot write, on a path of such directories, and never for a
// privileged process.
func TestRecorderRemembersOnlySealedDirectories(t *testing.T) {
	logical, _, outside := linkedModule(t)
	r := newRecorder(logical)
	if !r.memoize {
		t.Skip("a privileged process remembers nothing")
	}
	if !foreignDir("/") || writable("/") {
		t.Skip("this user owns or can write /")
	}
	if !r.sealedDir("/") {
		t.Fatal("/ is not sealed")
	}
	if r.sealedDir(outside) || r.sealedDir(t.TempDir()) {
		t.Fatal("a directory this user owns is sealed")
	}
	r.open(filepath.Join(outside, "x"), true)
	r.fixed.Range(func(path, _ any) bool {
		if strings.HasPrefix(path.(string), outside) {
			t.Errorf("remembered %s, in a directory this user owns", path)
		}
		return true
	})
	// A lookup in a sealed directory is remembered.
	sealed := ""
	for _, dir := range []string{"/usr/lib", "/usr/bin", "/usr", "/etc"} {
		if physical, ok := physicalDir(dir); ok && physical == dir && r.sealedDir(dir) {
			sealed = dir
			break
		}
	}
	if sealed == "" {
		t.Skip("no sealed directory on this host")
	}
	missing := filepath.Join(sealed, "effra-testinputs-missing")
	r.open(missing, true)
	if e, ok := r.fixed.Load(missing); !ok || !e.(entry).missing {
		t.Fatalf("the lookup of %s in the sealed %s was not remembered as missing: %v", missing, sealed, e)
	}
	if s, ok := r.sealed.Load(sealed); !ok || !s.(bool) {
		t.Fatalf("%s is not remembered as sealed", sealed)
	}
}

// TestRecorderStatsLinksAlongPaths checks that a link inside the root on
// the way to a recorded path is looked up too, so retargeting it to a
// look-alike file changes the key, and that a missing component ends the
// search.
func TestRecorderStatsLinksAlongPaths(t *testing.T) {
	logical, physical, _ := linkedModule(t)
	if err := os.MkdirAll(filepath.Join(physical, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(physical, "dirs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../a", filepath.Join(physical, "dirs", "current")); err != nil {
		t.Fatal(err)
	}
	r := newRecorder(logical)
	r.open(filepath.Join(logical, "dirs/current/value.txt"), true)
	r.stat(filepath.Join(logical, "src/missing/x"))
	r.stop()
	got := parse(t, r.lines())
	want := [][2]string{
		{"open", logical + "/dirs/current/value.txt"},
		{"stat", logical + "/dirs/current"},
		{"stat", logical + "/src/missing/x"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries:\n got %q\nwant %q", got, want)
	}
}

// TestRecorderResolvesDotDotBeforeSkipping checks that /proc, /dev and /sys
// are skipped by where a path leads, not by how it starts.
func TestRecorderResolvesDotDotBeforeSkipping(t *testing.T) {
	logical, _, _ := linkedModule(t)
	for path, want := range map[string]bool{
		"/proc/self/status":      true,
		"/dev/null":              true,
		"/sys":                   true,
		"/proc/../" + logical:    false,
		"/dev/../" + logical:     false,
		"/tmp/../proc/self":      true,
		"/nonexistent/../dev/x":  true,
		"/nonexistent/../tmp/..": false,
	} {
		if got := virtual(path); got != want {
			t.Errorf("virtual(%q) = %v, want %v", path, got, want)
		}
	}
	r := newRecorder(logical)
	r.stat("/dev/../" + logical + "/src/x")
	if got, want := parse(t, r.lines()), [][2]string{{"stat", logical + "/src/x"}}; !slices.Equal(got, want) {
		t.Fatalf("entries:\n got %q\nwant %q", got, want)
	}
	if runtime.GOOS != "linux" {
		return
	}
	// A descriptor link before a "..": the kernel goes to the parent of the
	// directory the descriptor holds, which cleaning by name would skip.
	src, err := os.Open(filepath.Join(logical, "src"))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	path := fmt.Sprintf("/tmp/../proc/%d/fd/%d/../go.mod", os.Getpid(), src.Fd())
	if virtual(path) {
		t.Errorf("virtual(%q) = true, want false: it leads into the module", path)
	}
}

// TestRecorderKeysAListingOnItsEntries checks that a directory read is
// "open", which cmd/go keys on every entry's name and metadata, so an entry
// added with the directory's modification time restored still counts.
func TestRecorderKeysAListingOnItsEntries(t *testing.T) {
	logical, physical, _ := linkedModule(t)
	old := time.Now().Add(-time.Hour)
	if err := os.WriteFile(filepath.Join(physical, "bin", "tool"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(physical, "bin"), old, old); err != nil {
		t.Fatal(err)
	}
	r := newRecorder(logical)
	r.open(filepath.Join(logical, "bin"), true)
	r.open(filepath.Join(logical, "bin", "tool"), true)
	got := parse(t, r.lines())
	want := [][2]string{
		{"open", logical + "/bin"},
		{"open", logical + "/bin/tool"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries:\n got %q\nwant %q", got, want)
	}
}

// TestRecorderNamesAreRaw checks that names cmd/go reads without quoting
// survive: spaces, quotes, backslashes, shell metacharacters and bytes that
// are not UTF-8. Only a newline cannot be carried, and it fails closed.
func TestRecorderNamesAreRaw(t *testing.T) {
	logical, _, _ := linkedModule(t)
	names := []string{
		"a b.ts", `it's "quoted".ts`, `back\slash\n.ts`, "$(x) `y` ;|&.ts",
		"tab\there.ts", "\xff\xfe.ts", "trailing space .ts", " leading.ts",
	}
	r := newRecorder(logical)
	for _, name := range names {
		r.open(filepath.Join(logical, "src", name), true)
	}
	r.exec("/nonexistent/loop-probe-x", []string{"WITH SPACE="})
	entries := parse(t, r.lines())
	var got []string
	for _, entry := range entries {
		got = append(got, entry[1])
	}
	var want []string
	for _, name := range names {
		want = append(want, filepath.Join(logical, "src", name))
	}
	want = append(want, "WITH SPACE=", ToolDigest)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) || r.reason() != "" {
		t.Fatalf("names:\n got %q\nwant %q (failure %q)", got, want, r.reason())
	}

	r.open(filepath.Join(logical, "src", "new\nline.ts"), true)
	if r.reason() == "" {
		t.Fatal("a path with a newline was recorded")
	}
	r = newRecorder(logical)
	r.exec("/nonexistent/loop-probe-x", []string{"NEW\nLINE"})
	if r.reason() == "" {
		t.Fatal("a variable name with a newline was recorded")
	}
}

func TestRecorderExecRecordsTheInterpreterChain(t *testing.T) {
	logical, _, _ := linkedModule(t)
	write := func(name, text string) string {
		path := filepath.Join(logical, "bin", name)
		if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	inner := write("inner", "\x7fELF")
	outer := write("outer", "#!"+inner+" -x\necho\n")
	script := write("script", "#! "+outer+"\n")
	r := newRecorder(logical)
	r.exec(script, []string{"PATH"})
	got := parse(t, r.lines())
	want := [][2]string{
		{"getenv", ToolDigest},
		{"getenv", "PATH"},
		{"open", inner},
		{"open", outer},
		{"open", script},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("entries:\n got %q\nwant %q", got, want)
	}
	if lines := newRecorder(logical).lines(); len(lines) != 0 {
		t.Fatalf("a run without programs keys on %q", lines)
	}
}

// TestUnobservedLeavesTheLogMalformed checks the fail-closed path: cmd/go
// caches a result only when its test log ends in a newline
// (cmd/go/internal/test.builderRunTest.saveOutput).
func TestUnobservedLeavesTheLogMalformed(t *testing.T) {
	log := filepath.Join(t.TempDir(), "testlog.txt")
	if err := os.WriteFile(log, []byte("# test log\nopen /m/a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := unobserved(log, 0, "unshare\nwith a newline"); code != 0 {
		t.Fatalf("a marked log turned a pass into %d", code)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "# test log\nopen /m/a\ntestinputs: unobserved: unshare with a newline" {
		t.Fatalf("log %q", got)
	}
	// A log that cannot be marked would be cached as the test process left
	// it, so a pass fails instead; a failure keeps its code.
	unwritable := t.TempDir() // opening a directory for writing fails
	if code := unobserved(unwritable, 0, "unshare"); code != 1 {
		t.Fatalf("an unmarked log left the pass as %d", code)
	}
	if code := unobserved(unwritable, 3, "unshare"); code != 3 {
		t.Fatalf("an unmarked log changed the failure to %d", code)
	}
	missing := filepath.Join(t.TempDir(), "never-written.txt")
	if err := appendLog(missing, "open /m/a\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("appendLog created a log the test process never wrote: %v", err)
	}
}
