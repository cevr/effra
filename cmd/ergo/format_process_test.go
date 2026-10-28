package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"ergo.local/prototype/internal/report"
	"ergo.local/prototype/internal/sketchfront"
)

// TestFmtUsesTheOneFormatter: every file of a request, and stdin, is
// formatted by the one formatter, which the report names at the top level
// and per file; a syntax error is EFMT_SYNTAX naming S001 and its position.
func TestFmtUsesTheOneFormatter(t *testing.T) {
	binary := buildTestCLI(t)
	root := smokeDir(t)
	first := smokeWrite(t, root, "one/main.ef", sketchUnformatted)
	second := smokeWrite(t, root, "two/main.ef", sketchUnformatted)
	malformedSource := "package p\n\nfunc main() {\n\tx := )\n}\n"
	malformed := smokeWrite(t, root, "bad.ef", malformedSource)

	stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, "", "fmt", "--check", "--json", "one/main.ef", "two/main.ef")
	if code != 1 {
		t.Fatalf("ergo fmt --check: exit %d, want 1\n%s\n%s", code, stdout, stderr)
	}
	var formatted formatReport
	if err := json.Unmarshal(stdout, &formatted); err != nil {
		t.Fatalf("report %s: %v", stdout, err)
	}
	if formatted.FormatterVersion != sketchfront.FormatterIdentity || formatted.SchemaVersion != report.FormatterSchemaVersion || len(formatted.Files) != 2 {
		t.Fatalf("report %+v", formatted)
	}
	for index, file := range formatted.Files {
		if file.FormatterVersion != formatted.FormatterVersion || !file.Changed || file.Written || file.OutputDigest != report.FormatDigest(sketchCleanSource) {
			t.Fatalf("file %d: %+v, want the one formatter, changed to the canonical text, not written", index, file)
		}
	}

	if stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, "", "fmt", "one/main.ef", "two/main.ef"); code != 0 {
		t.Fatalf("ergo fmt: exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, path := range []string{first, second} {
		if after := smokeRead(t, path); string(after) != sketchCleanSource {
			t.Fatalf("%s is %q", path, after)
		}
	}

	_, stderr, code = runSmokeCLI(t, processDeadline, binary, root, "", "fmt", "bad.ef")
	if code != 2 || !strings.Contains(string(stderr), "EFMT_SYNTAX") || !strings.Contains(string(stderr), "source cannot be formatted: S001 at 4:7: ") {
		t.Fatalf("ergo fmt of a malformed file: exit %d, stderr %q", code, stderr)
	}
	stdout, _, _ = runSmokeCLI(t, processDeadline, binary, root, "", "fmt", "--json", "bad.ef")
	formatted = formatReport{}
	if err := json.Unmarshal(stdout, &formatted); err != nil || len(formatted.Failures) != 1 || formatted.Failures[0].Code != "EFMT_SYNTAX" ||
		len(formatted.Failures[0].Diagnostics) != 1 || formatted.Failures[0].Diagnostics[0].Code != "S001" || formatted.Failures[0].Diagnostics[0].Span.Line != 4 {
		t.Fatalf("ergo fmt --json of a malformed file: %s", stdout)
	}
	if after := smokeRead(t, malformed); string(after) != malformedSource {
		t.Fatal("the malformed file changed")
	}

	if stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, sketchUnformatted, "fmt", "--stdin"); code != 0 || string(stdout) != sketchCleanSource {
		t.Fatalf("ergo fmt --stdin: exit %d, stdout %q\n%s", code, stdout, stderr)
	}
	if stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, legacyCleanSource, "fmt", "--stdin"); code != 2 || len(stdout) != 0 || !strings.Contains(string(stderr), "S001") {
		t.Fatalf("ergo fmt --stdin of retired .ef source: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestFmtReadsTheFileTheSpellingReaches: the formatter walks a spelling as
// the file system does, so `alias/..` after a directory link applies to the
// link's target. It formats the file that walk reaches, in check mode and
// in place, and never the file a lexically cleaned spelling names.
func TestFmtReadsTheFileTheSpellingReaches(t *testing.T) {
	binary := buildTestCLI(t)
	for _, mode := range [][]string{{"fmt", "--check"}, {"fmt"}} {
		t.Run(strings.Join(mode, " "), func(t *testing.T) {
			root := smokeDir(t)
			lexical := smokeWrite(t, root, "main.ef", sketchUnformatted)
			target := smokeWrite(t, root, "new/main.ef", sketchUnformatted)
			if err := os.MkdirAll(filepath.Join(root, "new", "deeper"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "new", "deeper"), filepath.Join(root, "alias")); err != nil {
				t.Fatal(err)
			}
			// filepath.Join would clean the `..` before the process sees it.
			spelling := root + "/alias/../main.ef"
			if reached, err := filepath.EvalSymlinks(spelling); err != nil || reached != target {
				t.Fatalf("the spelling reaches %s (%v), want %s", reached, err, target)
			}
			args := append(append([]string{}, mode...), spelling)
			stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, "", args...)
			check := len(mode) == 2
			if (check && code != 1) || (!check && code != 0) {
				t.Fatalf("ergo %s: exit %d\nstdout %s\nstderr %s", strings.Join(args, " "), code, stdout, stderr)
			}
			want := sketchCleanSource
			if check {
				want = sketchUnformatted
			}
			if after := smokeRead(t, target); string(after) != want {
				t.Fatalf("the reached file is %q, want %q", after, want)
			}
			if after := smokeRead(t, lexical); string(after) != sketchUnformatted {
				t.Fatal("the lexically cleaned spelling's file changed")
			}
		})
	}
}

// TestFmtFormatsAMixedVarGroupUnderTheDefaultBound (review r2 M1): a var
// group whose long type is followed by an inferred declaration and 2,000
// short typed ones formats under the default 4 MiB bound. Before the fix
// the empty type cell carried the long type's width to every short line,
// and the 39,225-byte file needed 8,232,442 bytes of output.
func TestFmtFormatsAMixedVarGroupUnderTheDefaultBound(t *testing.T) {
	binary := buildTestCLI(t)
	root := smokeDir(t)
	long := strings.Repeat("L", 4096)
	var b strings.Builder
	b.WriteString("package p\ntype " + long + " struct{}\nvar (\na " + long + " = " + long + "{}\nb = 2\n")
	for i := range 2000 {
		fmt.Fprintf(&b, "c%d int = 3\n", i)
	}
	b.WriteString(")\n")
	source := b.String()
	path := smokeWrite(t, root, "alignment-empty.ef", source)
	stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, "", "fmt", "--json", "alignment-empty.ef")
	if code != 0 {
		t.Fatalf("ergo fmt: exit %d\n%s\n%s", code, firstLines(stdout), stderr)
	}
	if after := smokeRead(t, path); len(after) > len(source)+len(source)/5 {
		t.Fatalf("a %d-byte source formatted to %d bytes", len(source), len(after))
	}
}

// TestFmtFormatsATaperedStructUnderTheDefaultBound (review r3 M1): a struct
// whose first field name is 4,096 wide, whose next names taper to 40 (each
// half the geometric mean of the names before it) and which ends with
// 16,384 short names formats under the default 4 MiB bound. Before the
// section bound, the 225,682-byte file needed 69,090,014 bytes of output.
func TestFmtFormatsATaperedStructUnderTheDefaultBound(t *testing.T) {
	binary := buildTestCLI(t)
	root := smokeDir(t)
	var b strings.Builder
	b.WriteString("package p\ntype T struct {\n")
	lnsum, count := 0.0, 0
	for w := 4096; ; {
		name := fmt.Sprintf("p%d", count)
		fmt.Fprintf(&b, "%s%s int\n", name, strings.Repeat("a", w-len(name)))
		lnsum += math.Log(float64(w))
		count++
		if w == 40 {
			break
		}
		w = max(40, int(math.Ceil(math.Exp(lnsum/float64(count))/2)))
	}
	for i := range 4 * 4096 {
		fmt.Fprintf(&b, "f%d int\n", i)
	}
	b.WriteString("}\n")
	source := b.String()
	if len(source) != 225682 {
		t.Fatalf("the generated taper is %d bytes, want the reviewer's 225682", len(source))
	}
	path := smokeWrite(t, root, "alignment-taper.ef", source)
	stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, root, "", "fmt", "--json", "alignment-taper.ef")
	if code != 0 {
		t.Fatalf("ergo fmt: exit %d\n%s\n%s", code, firstLines(stdout), stderr)
	}
	if after := smokeRead(t, path); len(after) > 2*len(source) {
		t.Fatalf("a %d-byte source formatted to %d bytes", len(source), len(after))
	}
}

// formatFirstFailure is the first failure of a fmt --json report.
func formatFirstFailure(t *testing.T, stdout []byte) map[string]any {
	t.Helper()
	failures, _ := smokeJSON(t, stdout)["failures"].([]any)
	if len(failures) == 0 {
		t.Fatalf("report has no failures: %s", stdout)
	}
	failure, _ := failures[0].(map[string]any)
	return failure
}

// TestFormatSmokeLimitsAndPaths: the check, write and no-op chain, the
// alias, option-like and duplicate path rules, and the exact per-file and
// aggregate input and output bounds of `ergo fmt`.
func TestFormatSmokeLimitsAndPaths(t *testing.T) {
	binary := buildTestCLI(t)

	chain := t.TempDir()
	path := smokeWrite(t, chain, "main.ef", sketchUnformatted)
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, chain, "", "fmt", "--check", "--json", path)
	if code != 1 || len(stderr) != 0 {
		t.Fatalf("check: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	checkFiles, _ := smokeJSON(t, stdout)["files"].([]any)
	if file, _ := checkFiles[0].(map[string]any); len(checkFiles) != 1 || file["changed"] != true || file["written"] != false {
		t.Fatalf("check file report: %v", checkFiles)
	}
	stdout, stderr, code = runSmokeCLI(t, processDeadline, binary, chain, "", "fmt", "--json", path)
	if code != 0 || len(stderr) != 0 {
		t.Fatalf("write: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	writeReport := smokeJSON(t, stdout)
	if writeReport["formatterVersion"] != sketchfront.FormatterIdentity || writeReport["schemaVersion"] != float64(1) {
		t.Fatalf("write report: %v", writeReport)
	}
	written, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if written.Mode().Perm() != original.Mode().Perm() {
		t.Fatalf("write changed mode %#o to %#o", original.Mode().Perm(), written.Mode().Perm())
	}
	stdout, stderr, code = runSmokeCLI(t, processDeadline, binary, chain, "", "fmt", path)
	if code != 0 || len(stdout) != 0 || !strings.Contains(string(stderr), "already formatted") {
		t.Fatalf("no-op: exit %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if noop, err := os.Stat(path); err != nil || !noop.ModTime().Equal(written.ModTime()) {
		t.Fatal("no-op rewrote the formatted file")
	}

	t.Run("hardlink-alias", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		valid := smokeWrite(t, dir, "valid.ef", sketchUnformatted)
		alias := filepath.Join(dir, "alias.ef")
		if err := os.Link(valid, alias); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, dir, "", "fmt", "--json", valid, alias)
		if code != 2 || len(stderr) != 0 || formatFirstFailure(t, stdout)["code"] != "EFMT_ALIAS" {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if string(smokeRead(t, valid)) != sketchUnformatted {
			t.Fatal("alias refusal wrote a file")
		}
	})

	t.Run("option-like-path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		option := smokeWrite(t, dir, "--option.ef", sketchUnformatted)
		if stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, dir, "", "fmt", "--", "--option.ef"); code != 0 || string(smokeRead(t, option)) != sketchCleanSource {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})

	t.Run("duplicate-requested-paths", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		smokeWrite(t, dir, "main.ef", sketchCleanSource)
		stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, dir, "", "fmt", "--json", "main.ef", "./main.ef")
		if code != 0 || len(stderr) != 0 {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
		files, _ := smokeJSON(t, stdout)["files"].([]any)
		if file, _ := files[0].(map[string]any); len(files) != 1 || !reflect.DeepEqual(file["requestedPaths"], []any{"main.ef", "./main.ef"}) {
			t.Fatalf("files: %v", files)
		}
	})

	t.Run("input-limits", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		// Each file is exactly the per-file bound: a package clause and a
		// comment padding it out.
		exact := "package p\n//" + strings.Repeat("x", maxFormatInputBytes-len("package p\n//")-1) + "\n"
		var inputs []string
		for index := 0; index < 4; index++ {
			name := "input-" + strconv.Itoa(index) + ".ef"
			smokeWrite(t, dir, name, exact)
			inputs = append(inputs, name)
		}
		stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, dir, "", append([]string{"fmt", "--check", "--json"}, inputs...)...)
		if code != 0 || smokeJSON(t, stdout)["success"] != true {
			t.Fatalf("exact aggregate input: exit %d stdout=%q stderr=%q", code, firstLines(stdout), stderr)
		}
		smokeWrite(t, dir, "aggregate-extra.ef", " ")
		stdout, stderr, code = runSmokeCLI(t, processDeadline, binary, dir, "", append(append([]string{"fmt", "--json"}, inputs...), "aggregate-extra.ef")...)
		failure := formatFirstFailure(t, stdout)
		if message, _ := failure["message"].(string); code != 2 || failure["code"] != "EFMT_INPUT_LIMIT" || !strings.Contains(message, "aggregate input limit") {
			t.Fatalf("aggregate input over: exit %d stdout=%q stderr=%q", code, firstLines(stdout), stderr)
		}
		smokeWrite(t, dir, "aggregate-extra.ef", exact+" ")
		stdout, stderr, code = runSmokeCLI(t, processDeadline, binary, dir, "", "fmt", "--json", "aggregate-extra.ef")
		if code != 2 || formatFirstFailure(t, stdout)["code"] != "EFMT_INPUT_LIMIT" {
			t.Fatalf("per-file input over: exit %d stdout=%q stderr=%q", code, firstLines(stdout), stderr)
		}
	})

	t.Run("output-limits", func(t *testing.T) {
		t.Parallel()
		expansion := formatExpansion(37000)
		expanded, stderr, code := runSmokeCLI(t, processDeadline, binary, t.TempDir(), string(expansion), "fmt", "--stdin")
		if code != 0 {
			t.Fatalf("expansion: exit %d stderr=%q", code, stderr)
		}
		padding := maxFormatOutputBytes - len(expanded) - 3
		exactSource := append([]byte("//"+strings.Repeat("x", padding)+"\n"), expansion...)
		if padding < 0 || len(exactSource) > maxFormatInputBytes {
			t.Fatalf("expansion output %d bytes from %d input bytes leaves no exact source under the input bound", len(expanded), len(expansion))
		}
		t.Run("stdin", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, dir, string(exactSource), "fmt", "--stdin")
			if code != 0 || len(stdout) != maxFormatOutputBytes {
				t.Fatalf("exact output: exit %d stdout=%d bytes stderr=%q", code, len(stdout), stderr)
			}
			oneOver := append([]byte("//x"), exactSource[2:]...)
			stdout, stderr, code = runSmokeCLI(t, processDeadline, binary, dir, string(oneOver), "fmt", "--stdin")
			if code != 2 || len(stdout) != 0 || !bytes.Contains(stderr, []byte("EFMT_OUTPUT_LIMIT")) {
				t.Fatalf("one over output: exit %d stdout=%d bytes stderr=%q", code, len(stdout), stderr)
			}
		})
		t.Run("aggregate", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			var outputs []string
			for index := 0; index < 4; index++ {
				name := "output-" + strconv.Itoa(index) + ".ef"
				smokeWrite(t, dir, name, string(exactSource))
				outputs = append(outputs, name)
			}
			stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, dir, "", append([]string{"fmt", "--check", "--json"}, outputs...)...)
			if code != 1 || smokeJSON(t, stdout)["success"] != true {
				t.Fatalf("exact aggregate output: exit %d stdout=%q stderr=%q", code, firstLines(stdout), stderr)
			}
			smokeWrite(t, dir, "aggregate-extra.ef", "package p\n")
			stdout, stderr, code = runSmokeCLI(t, processDeadline, binary, dir, "", append(append([]string{"fmt", "--json"}, outputs...), "aggregate-extra.ef")...)
			failure := formatFirstFailure(t, stdout)
			if message, _ := failure["message"].(string); code != 2 || failure["code"] != "EFMT_OUTPUT_LIMIT" || !strings.Contains(message, "aggregate output limit") {
				t.Fatalf("aggregate output over: exit %d stdout=%q stderr=%q", code, firstLines(stdout), stderr)
			}
			for _, name := range outputs {
				if !bytes.Equal(smokeRead(t, filepath.Join(dir, name)), exactSource) {
					t.Fatalf("aggregate output refusal wrote %s", name)
				}
			}
		})
	})

	t.Run("stdin-json-invocation", func(t *testing.T) {
		t.Parallel()
		stdout, stderr, code := runSmokeCLI(t, processDeadline, binary, t.TempDir(), sketchUnformatted, "fmt", "--stdin", "--json")
		if code != 2 || len(stderr) != 0 || formatFirstFailure(t, stdout)["code"] != "EFMT_INVOCATION" {
			t.Fatalf("exit %d stdout=%q stderr=%q", code, stdout, stderr)
		}
	})
}
