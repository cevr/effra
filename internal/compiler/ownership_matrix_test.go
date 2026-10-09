package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The ownership matrix (lane E2 R5) decides every generated program and
// every hand probe with the checker and compares the verdict with runtime
// truth recorded by executing the same program with ownership refusals
// erased. Every verdict must equal its truth on every target.

const ownershipFixturePlaceholder = "{{fixture}}"

type ownershipTruth struct {
	Name string `json:"name"`
	Go   string `json:"go"`
	JS   string `json:"js,omitempty"`
}

type ownershipProbe struct {
	Name string `json:"name"`
	Go   string `json:"go"`
	JS   string `json:"js,omitempty"`
	// Witness names the probe whose execution witnesses this probe's truth.
	// This probe's own source cannot be witnessed directly (it never runs the
	// shape, or its failure races the program's exit); the witness probe is
	// the same shape made deterministic.
	Witness string `json:"witness,omitempty"`
}

type ownershipCase struct {
	Set    string
	Name   string
	Target string
	Class  string
}

type ownershipDecision struct {
	ownershipCase
	diagnostics []Diagnostic
	reports     []ownershipReport
}

func readOwnershipJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(path, err)
	}
}

// ownershipClass classifies one check-only verdict against its truth: safe,
// unsafe (use after close) or row-unsafe (a failure escaped every declared
// row). A refusal is EF123 or EF107; any other diagnostic is malformed. An
// unsafe program refused without EF123, or a row-unsafe program refused
// without EF107, is refused for the wrong reason.
// ownershipInterrupted is the truth of a program which ends by uncaught
// interruption with no typed failure beside it. It raises no undeclared
// failure, so the checker must admit it exactly as it admits a safe program.
const ownershipInterrupted = "interrupted"

func ownershipClass(diagnostics []Diagnostic, truth string) string {
	refused, ownership, rows := false, false, false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != "EF123" && diagnostic.Code != "EF107" {
			return "malformed"
		}
		refused = true
		ownership = ownership || diagnostic.Code == "EF123"
		rows = rows || diagnostic.Code == "EF107"
	}
	switch {
	case (truth == "safe" || truth == ownershipInterrupted) && refused:
		return "over-refused"
	case truth == "safe" || truth == ownershipInterrupted:
		return "ok-admitted"
	case truth == "unsafe" && refused && !ownership:
		return "ok-refused-wrong-reason"
	case (truth == "row-unsafe" || truth == ownershipDeclaredRowPolicy) && refused && !rows:
		return "ok-refused-wrong-reason"
	case refused:
		return "ok-refused"
	default:
		return "red-admitted"
	}
}

func ownershipCorrect(class string) bool { return class == "ok-admitted" || class == "ok-refused" }

// decideOwnership checks every (program, target) pair in parallel.
func decideOwnership(jobs []ownershipDecision, sources map[string]string, truths map[string]string) []ownershipDecision {
	var wait sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for i := range next {
				job := &jobs[i]
				r := CompileFor(sources[job.Set+"/"+job.Name], job.Target)
				job.diagnostics = r.Diagnostics
				if r.projector != nil {
					job.reports = r.projector.ownershipReports
				}
				job.Class = ownershipClass(job.diagnostics, truths[job.Set+"/"+job.Name+"/"+job.Target])
			}
		}()
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wait.Wait()
	return jobs
}

func ownershipProbeSources(t *testing.T, fixture string) map[string]string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("testdata", "ownership_probes", "*.ef"))
	if err != nil || len(paths) == 0 {
		t.Fatal("ownership probes missing", err)
	}
	sources := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources[strings.TrimSuffix(filepath.Base(path), ".ef")] = strings.ReplaceAll(string(data), ownershipFixturePlaceholder, fixture)
	}
	return sources
}

func ownershipFixture(t *testing.T) string {
	t.Helper()
	fixture := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(fixture, []byte("fixture-ok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

type ownershipMatrix struct {
	sources   map[string]string
	truths    map[string]string
	decisions []ownershipDecision
}

// loadOwnershipMatrix reads the generated programs, the hand probes, their
// truth, and decides every pair when decide is set.
func loadOwnershipMatrix(t *testing.T, fixture string, decide bool) ownershipMatrix {
	t.Helper()
	var truthRows []ownershipTruth
	var probes []ownershipProbe
	m := ownershipMatrix{sources: map[string]string{}, truths: map[string]string{}}
	readOwnershipJSON(t, filepath.Join("testdata", "ownership_matrix", "truth.json"), &truthRows)
	readOwnershipJSON(t, filepath.Join("testdata", "ownership_probes", "probes.json"), &probes)
	programs := ownershipMatrixPrograms(fixture)
	if len(programs) != len(truthRows) {
		t.Fatalf("generator yields %d programs, truth records %d", len(programs), len(truthRows))
	}
	var jobs []ownershipDecision
	for i, program := range programs {
		truth := truthRows[i]
		if truth.Name != program.Name || truth.Go != program.Oracle || (truth.JS != "") != program.JSRunnable {
			t.Fatalf("generator and truth disagree at %d: %+v vs %s %s %v", i, truth, program.Name, program.Oracle, program.JSRunnable)
		}
		m.sources["matrix/"+program.Name] = program.Source
		m.truths["matrix/"+program.Name+"/go"] = truth.Go
		jobs = append(jobs, ownershipDecision{ownershipCase: ownershipCase{Set: "matrix", Name: program.Name, Target: "go"}})
		if program.JSRunnable {
			m.truths["matrix/"+program.Name+"/js"] = truth.JS
			jobs = append(jobs, ownershipDecision{ownershipCase: ownershipCase{Set: "matrix", Name: program.Name, Target: "js"}})
		}
	}
	probeSources := ownershipProbeSources(t, fixture)
	if len(probeSources) != len(probes) {
		t.Fatalf("%d probe sources, %d probe expectations", len(probeSources), len(probes))
	}
	for _, probe := range probes {
		source, ok := probeSources[probe.Name]
		if !ok {
			t.Fatal("missing probe source", probe.Name)
		}
		m.sources["probe/"+probe.Name] = source
		for target, truth := range map[string]string{"go": probe.Go, "js": probe.JS} {
			if truth == "" {
				continue
			}
			m.truths["probe/"+probe.Name+"/"+target] = truth
			jobs = append(jobs, ownershipDecision{ownershipCase: ownershipCase{Set: "probe", Name: probe.Name, Target: target}})
		}
	}
	if decide {
		m.decisions = decideOwnership(jobs, m.sources, m.truths)
	}
	return m
}

// TestOwnershipMatrixMatchesTruth requires every verdict of the matrix and
// of the hand probes to equal runtime truth, on Go and on JS.
func TestOwnershipMatrixMatchesTruth(t *testing.T) {
	m := loadOwnershipMatrix(t, ownershipFixture(t), true)
	counts := map[string]int{}
	for _, decision := range m.decisions {
		counts[decision.Set+"/"+decision.Target+"/"+decision.Class]++
		if !ownershipCorrect(decision.Class) {
			t.Errorf("wrong verdict %s %s/%s: %s %v", decision.Class, decision.Set, decision.Name, decision.Target, decision.diagnostics)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(counts)) {
		t.Logf("%s %d", key, counts[key])
	}
}

// TestOwnershipMatrixRaisingTwins keeps the truth of every row whose use
// after close needs a failure its fixture never raises equal to the executed
// truth of its raising twin, and requires a twin for every such row.
func TestOwnershipMatrixRaisingTwins(t *testing.T) {
	m := loadOwnershipMatrix(t, ownershipFixture(t), false)
	fixture := ownershipFixture(t)
	twins := 0
	for _, p := range ownershipMatrixCases(fixture) {
		if !p.handlerAcquiresAtBoundary() || slices.Contains(p.acq, p.b) || !slices.Contains(ownershipMatrixClosing, p.boundary) {
			continue
		}
		row := "matrix/" + p.name()
		p.raising = true
		twin := "matrix/" + p.name()
		if m.truths[twin+"/go"] == "" {
			t.Errorf("%s has no raising twin", row)
			continue
		}
		twins++
		if m.truths[row+"/go"] != m.truths[twin+"/go"] {
			t.Errorf("%s records %s, its raising twin executed %s", row, m.truths[row+"/go"], m.truths[twin+"/go"])
		}
	}
	for key := range m.truths {
		if name, ok := strings.CutSuffix(key, "_raising/go"); ok && m.truths[name+"/go"] == "" {
			t.Errorf("raising twin %s has no row", key)
		}
	}
	if twins == 0 {
		t.Fatal("no raising twins")
	}
}

// TestOwnershipMatrixReportsDistinctRoots: a program refused with more than
// one EF123 reports a distinct (origin, closing owner) root at each, so no
// diagnostic repeats a fault an earlier one named (design §7.1). The
// programs with several reports are exactly the reviewed residual set: each
// has several independent faults.
func TestOwnershipMatrixReportsDistinctRoots(t *testing.T) {
	m := loadOwnershipMatrix(t, ownershipFixture(t), true)
	// kp_provision_out lets a provision-owned handle escape the provision
	// edge and then reads the payload handle a handler kept; each is a
	// fault. rv2-m4-helper-read has three recoveries, each with a handler
	// that reads the timeout-owned payload through a different helper.
	residual := map[string]bool{"matrix/kp_provision_out/go": false, "probe/rv2-m4-helper-read/go": false}
	multiple := 0
	for _, decision := range m.decisions {
		count := 0
		for _, diagnostic := range decision.diagnostics {
			if diagnostic.Code == "EF123" {
				count++
			}
		}
		if count < 2 {
			continue
		}
		multiple++
		key := decision.Set + "/" + decision.Name + "/" + decision.Target
		if _, reviewed := residual[key]; !reviewed {
			t.Errorf("%s reports %d EF123 and is not in the reviewed residual set", key, count)
		}
		residual[key] = true
		if len(decision.reports) != count {
			t.Errorf("%s/%s %s: %d EF123 but %d recorded reports", decision.Set, decision.Name, decision.Target, count, len(decision.reports))
			continue
		}
		if repeated := distinctOwnershipReports(decision.reports); len(repeated) > 0 {
			t.Errorf("%s: reports %v name only roots an earlier report named", key, repeated)
		}
		seen := map[reportedOwnershipRoot]bool{}
		for _, report := range decision.reports {
			if len(report.roots) == 0 {
				t.Errorf("%s/%s %s: EF123 at %d:%d names no root", decision.Set, decision.Name, decision.Target, report.span.Line, report.span.Column)
			}
			for _, root := range report.roots {
				if seen[root] {
					t.Errorf("%s/%s %s: root %v reported twice", decision.Set, decision.Name, decision.Target, root.root)
				}
				seen[root] = true
			}
		}
	}
	for key, seen := range residual {
		if !seen {
			t.Errorf("residual %s no longer reports several EF123; update the reviewed set", key)
		}
	}
	t.Logf("programs with more than one EF123: %d", multiple)
}

// TestOwnershipMatrixNativeTruth executes a sample natively with the real Go
// backend: an admitted safe program, the admitted safe hand probes and an
// observer row whose joined child's failure is recovered where the join
// raises it. The native verdict must equal recorded truth.
func TestOwnershipMatrixNativeTruth(t *testing.T) {
	fixture := ownershipFixture(t)
	m := loadOwnershipMatrix(t, fixture, false)
	sample := []string{"matrix/s1_run@1_plain", "probe/h2-final-run-control", "probe/rv2-rows-nested-scope-join", "matrix/rw_join_observer_ignore"}
	for _, key := range sample {
		truth := m.truths[key+"/go"]
		r := CompileFor(m.sources[key], "go")
		if !r.Checked {
			t.Fatalf("%s: native sample must be admitted: %v", key, r.Diagnostics)
		}
		if got := ownershipNativeTruth(t, r); got != truth {
			t.Errorf("%s: native truth %s, recorded %s", key, got, truth)
		}
	}
}

func ownershipNativeTruth(t *testing.T, r *Result) string {
	t.Helper()
	binary := buildGeneratedGo(t, r, GoGenerationBuild)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary).CombinedOutput()
	var lines []string
	for _, line := range strings.Split(string(output), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	switch {
	case err == nil && slices.Contains(lines, "closed"):
		return "unsafe"
	case err == nil && len(lines) > 0:
		return "safe"
	default:
		return fmt.Sprintf("unknown: %v %s", err, output)
	}
}

// TestOwnershipMatrixDump writes every generated program (in generator
// order, with the generator's oracle) and every hand probe (with its recorded
// truth) to $EFFRA_OWNERSHIP_DUMP/programs.json, for scripts/ownership_truth.py
// to execute. $EFFRA_OWNERSHIP_FIXTURE names the file the programs open. It
// is skipped unless the directory is set; it needs no truth.json, which the
// script regenerates.
func TestOwnershipMatrixDump(t *testing.T) {
	dir := os.Getenv("EFFRA_OWNERSHIP_DUMP")
	if dir == "" {
		t.Skip("EFFRA_OWNERSHIP_DUMP not set")
	}
	fixture := os.Getenv("EFFRA_OWNERSHIP_FIXTURE")
	if fixture == "" {
		t.Fatal("EFFRA_OWNERSHIP_FIXTURE not set")
	}
	type row struct {
		Set    string `json:"set"`
		Name   string `json:"name"`
		Go     string `json:"go"`
		JS     string `json:"js,omitempty"`
		Source string `json:"source"`
		// Witness is the probe key whose execution witnesses this row.
		Witness string `json:"witness,omitempty"`
	}
	var rows []row
	for _, program := range ownershipMatrixPrograms(fixture) {
		r := row{Set: "matrix", Name: program.Name, Go: program.Oracle, Source: program.Source}
		if program.JSRunnable {
			r.JS = program.Oracle
		}
		rows = append(rows, r)
	}
	var probes []ownershipProbe
	readOwnershipJSON(t, filepath.Join("testdata", "ownership_probes", "probes.json"), &probes)
	sources := ownershipProbeSources(t, fixture)
	for _, probe := range probes {
		r := row{Set: "probe", Name: probe.Name, Go: probe.Go, JS: probe.JS, Source: sources[probe.Name]}
		if probe.Witness != "" {
			r.Witness = "probe/" + probe.Witness
		}
		rows = append(rows, r)
	}
	data, err := json.MarshalIndent(rows, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "programs.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestOwnershipProbeWitnessesAreExecutable checks the bookkeeping of probe
// witnesses; it executes nothing. A probe's recorded truth is witnessed by
// executing it, or by executing the probe it names, which must itself be
// executed and record the same truth. The execution is
// scripts/ownership_truth.py, which fails on any probe whose executed class
// differs from the recorded one.
func TestOwnershipProbeWitnessesAreExecutable(t *testing.T) {
	var probes []ownershipProbe
	readOwnershipJSON(t, filepath.Join("testdata", "ownership_probes", "probes.json"), &probes)
	byName := map[string]ownershipProbe{}
	for _, probe := range probes {
		byName[probe.Name] = probe
	}
	witnessed := 0
	for _, probe := range probes {
		if probe.Witness == "" {
			continue
		}
		witnessed++
		twin, ok := byName[probe.Witness]
		switch {
		case !ok:
			t.Errorf("%s names missing witness %s", probe.Name, probe.Witness)
		case twin.Witness != "":
			t.Errorf("%s names witness %s which is itself witnessed elsewhere", probe.Name, probe.Witness)
		case twin.Go != probe.Go || twin.JS != probe.JS:
			t.Errorf("%s records %s/%s, its witness %s records %s/%s", probe.Name, probe.Go, probe.JS, twin.Name, twin.Go, twin.JS)
		}
	}
	if witnessed != 2 {
		t.Errorf("want the 2 reviewed witnessed probes, got %d", witnessed)
	}
}

// TestOwnershipDeclaredRowPolicyHasRaisingTwins: a declared-row-policy row is
// not executed truth (its recorded schedule executes safe). Each one must
// have a raising twin whose children really fail, recorded row-unsafe: the
// shape the policy refuses. scripts/ownership_truth.py executes both.
func TestOwnershipDeclaredRowPolicyHasRaisingTwins(t *testing.T) {
	m := loadOwnershipMatrix(t, ownershipFixture(t), false)
	policy := 0
	for key, truth := range m.truths {
		row, ok := strings.CutSuffix(key, "/go")
		if !ok || truth != ownershipDeclaredRowPolicy {
			continue
		}
		policy++
		if twin := m.truths[row+"_raising/go"]; twin != "row-unsafe" {
			t.Errorf("%s is a declared-row-policy row, its raising twin records %q, want row-unsafe", row, twin)
		}
	}
	if policy == 0 {
		t.Fatal("no declared-row-policy rows")
	}
	for key := range m.truths {
		if strings.HasPrefix(key, "matrix/") && strings.HasSuffix(key, "/js") {
			if truth := m.truths[key]; truth == ownershipDeclaredRowPolicy {
				t.Errorf("%s: a policy row is Go-only (its JS truth would be executed)", key)
			}
		}
	}
}

// TestOwnershipClassIsStrong: a refusal must be for the recorded reason.
func TestOwnershipClassIsStrong(t *testing.T) {
	own := []Diagnostic{{Code: "EF123"}}
	rows := []Diagnostic{{Code: "EF107"}}
	for _, c := range []struct {
		diagnostics []Diagnostic
		truth, want string
	}{
		{nil, "safe", "ok-admitted"},
		{nil, ownershipInterrupted, "ok-admitted"},
		{own, ownershipInterrupted, "over-refused"},
		{own, "safe", "over-refused"},
		{own, "unsafe", "ok-refused"},
		{rows, "unsafe", "ok-refused-wrong-reason"},
		{rows, "row-unsafe", "ok-refused"},
		{rows, ownershipDeclaredRowPolicy, "ok-refused"},
		{own, ownershipDeclaredRowPolicy, "ok-refused-wrong-reason"},
		{nil, ownershipDeclaredRowPolicy, "red-admitted"},
		{own, "row-unsafe", "ok-refused-wrong-reason"},
		{append(slices.Clone(own), rows...), "row-unsafe", "ok-refused"},
		{nil, "row-unsafe", "red-admitted"},
		{[]Diagnostic{{Code: "EF105"}}, "safe", "malformed"},
	} {
		if got := ownershipClass(c.diagnostics, c.truth); got != c.want {
			t.Errorf("%v against %s: %s, want %s", c.diagnostics, c.truth, got, c.want)
		}
	}
}
