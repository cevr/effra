package lint

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

var fixture struct {
	once sync.Once
	dir  string
	path string
	err  error
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fixture.dir != "" {
		os.RemoveAll(fixture.dir)
	}
	os.Exit(code)
}

// fixturePack builds testdata/fixturepack once: every runner test starts a
// real separate process.
func fixturePack(t *testing.T) string {
	t.Helper()
	fixture.once.Do(func() {
		fixture.dir, fixture.err = os.MkdirTemp("", "effra-lint-fixture-")
		if fixture.err != nil {
			return
		}
		fixture.path = filepath.Join(fixture.dir, "fixturepack")
		if output, err := exec.Command("go", "build", "-o", fixture.path, "./testdata/fixturepack").CombinedOutput(); err != nil {
			fixture.err = errors.New(string(output))
		}
	})
	if fixture.err != nil {
		t.Fatal(fixture.err)
	}
	return fixture.path
}

// fixtureManifest mirrors the fixture pack's metadata; args select its
// behaviour.
func fixtureManifest(t *testing.T, args ...string) Manifest {
	t.Helper()
	manifest := Manifest{
		ManifestVersion: ManifestVersion, Namespace: "fixture", Version: "1.0.0",
		FactSchema: ManifestSchema{Name: FactSchemaName, Versions: []int{FactSchemaVersion}},
		Executable: Executable{Path: fixturePack(t), Args: args},
		Rules: []ManifestRule{{
			Name: "rename-main", Version: "1", Description: "fixture rule", DefaultSeverity: SeverityWarning,
			Requires: []Family{FamilyCallables},
			Options:  []OptionSpec{{Name: "to", Type: OptionString, Default: []byte(`"entry"`)}},
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	return manifest
}

// fixtureRunner configures the fixture pack in the test goroutine and
// returns a run safe to call from another goroutine.
func fixtureRunner(t *testing.T, args ...string) func(context.Context, Limits) Report {
	t.Helper()
	registry, err := NewRegistry(testBuiltins, fixtureManifest(t, args...))
	if err != nil {
		t.Fatal(err)
	}
	configuration := configure(t, registry, `{"version":1}`)
	return func(ctx context.Context, limits Limits) Report {
		report, err := Run(ctx, configuration, "fixture", testSnapshot(), RunOptions{Limits: limits})
		if err != nil {
			t.Error(err)
		}
		return report
	}
}

func runFixture(t *testing.T, ctx context.Context, limits Limits, args ...string) Report {
	t.Helper()
	return fixtureRunner(t, args...)(ctx, limits)
}

var renameFinding = ReportedFinding{
	Rule: "fixture/rename-main", Severity: SeverityWarning, Message: "main should be entry", Span: Span{2, 4, 1, 3},
	Suggestions: []Suggestion{{Message: "rename to entry", Edits: []Edit{{Span: Span{2, 4, 1, 3}, NewText: "entry"}}}},
}

func TestRunCompletesThroughTheProtocol(t *testing.T) {
	report := runFixture(t, context.Background(), Limits{}, "serve")
	if !report.Complete || report.Failure != nil || !reflect.DeepEqual(report.Rules, []RuleStatus{{Rule: "fixture/rename-main", Status: StatusCompleted, Findings: 1}}) || !reflect.DeepEqual(report.Findings, []ReportedFinding{renameFinding}) {
		t.Fatalf("%+v", report)
	}
	// stderr is diagnostics: a pack flooding it neither blocks nor fails.
	if report := runFixture(t, context.Background(), Limits{}, "stderr-flood"); !report.Complete {
		t.Fatalf("stderr flood: %+v", report)
	}
}

func TestRunDecidesWithoutStartingWhenNoRuleMayRun(t *testing.T) {
	registry, err := NewRegistry(testBuiltins, fixtureManifest(t, "crash"))
	if err != nil {
		t.Fatal(err)
	}
	unchecked := testSnapshot()
	unchecked.Families = nil
	for _, run := range []struct {
		config   string
		snapshot *Snapshot
		status   string
	}{
		{`{"version":1,"rules":{"fixture/rename-main":"off"}}`, testSnapshot(), StatusOff},
		{`{"version":1}`, unchecked, StatusSkipped},
	} {
		report, err := Run(context.Background(), configure(t, registry, run.config), "fixture", run.snapshot, RunOptions{})
		if err != nil || report.Failure != nil || report.Rules[0].Status != run.status {
			t.Fatalf("a crashing pack was started for %s: %+v %v", run.status, report, err)
		}
	}
}

func assertPackFailure(t *testing.T, report Report, code, message string) {
	t.Helper()
	if report.Complete || report.Failure == nil || report.Failure.Code != code || report.Failure.Pack != "fixture" || !strings.Contains(report.Failure.Message, message) {
		t.Fatalf("want %s %q: %+v %+v", code, message, report, report.Failure)
	}
	if len(report.Findings) != 0 || len(report.Rules) != 1 || report.Rules[0].Status != StatusFailed || report.Rules[0].Reason != "pack-failed: "+code {
		t.Fatalf("pack failure did not fail its rules: %+v", report)
	}
}

func TestRunReportsCrashesAndMalformedOutput(t *testing.T) {
	report := runFixture(t, context.Background(), Limits{}, "crash")
	assertPackFailure(t, report, FailureCrashed, "exited unsuccessfully")
	if report.Failure.Exit != "exit status 2" || !strings.Contains(report.Failure.Stderr, "panic: fixture pack crashed") {
		t.Fatalf("crash evidence: %+v", report.Failure)
	}
	// A complete response does not excuse a failing exit.
	assertPackFailure(t, runFixture(t, context.Background(), Limits{}, "exit-after-response"), FailureCrashed, "exited unsuccessfully")
	for _, run := range []struct{ mode, code, message string }{
		{"silent", FailureMalformed, "no frame"},
		{"garbage", FailureMalformed, `malformed frame header "debug: starting fixture pack"`},
		{"raw-trailing", FailureMalformed, "data follows the frame"},
		{"raw-suppress", FailureMalformed, `unknown field "suppress"`},
		{"raw-version", FailureProtocol, "pack speaks protocol version 2; the runner speaks 1"},
		{"huge-header", FailureOversized, "response of 1073741824 bytes exceeds the 16777216-byte limit"},
		{"raw-stale", FailureStale, `snapshot revision "an-older-revision"; the request carried "r1"`},
		{"raw-foreign", FailureInvalid, `answers request "sha256:another-request"`},
		{"raw-masquerade", FailureInvalid, `names rule "unused-recipe", which was not requested`},
		{"raw-duplicate", FailureInvalid, `repeats rule "rename-main"`},
		{"raw-missing", FailureInvalid, `no result for rule "rename-main"`},
		{"raw-status", FailureInvalid, `status "skipped"`},
	} {
		t.Run(run.mode, func(t *testing.T) {
			report := runFixture(t, context.Background(), Limits{}, run.mode)
			assertPackFailure(t, report, run.code, run.message)
			// Whether the transport or acceptance refused the response, the
			// failure keeps the process's evidence.
			if strings.HasPrefix(run.mode, "raw-") && (report.Failure.Exit == "" || !strings.Contains(report.Failure.Stderr, "raw fixture: "+run.mode)) {
				t.Fatalf("%s lost its process evidence: %+v", run.mode, report.Failure)
			}
		})
	}
	// The response limits are explicit: the same pack passes under the
	// default limits and fails under tighter ones.
	if report := runFixture(t, context.Background(), Limits{}, "raw-many"); !report.Complete || len(report.Findings) != 20 {
		t.Fatalf("20 findings within the default limit: %+v", report)
	}
	assertPackFailure(t, runFixture(t, context.Background(), Limits{MaxFindings: 19}, "raw-many"), FailureOversized, "20 findings; the limit is 19")
	if report := runFixture(t, context.Background(), Limits{}, "serve"); !report.Complete {
		t.Fatalf("serve within the default response size: %+v", report)
	}
	assertPackFailure(t, runFixture(t, context.Background(), Limits{MaxResponseBytes: 64}, "serve"), FailureOversized, "exceeds the 64-byte limit")
}

func TestRunValidatesSpansAndSuggestedEdits(t *testing.T) {
	report := runFixture(t, context.Background(), Limits{}, "raw-valid")
	if !report.Complete || len(report.Findings) != 1 || len(report.Findings[0].Suggestions[0].Edits) != 2 {
		t.Fatalf("valid edits: %+v", report)
	}
	for _, run := range []struct{ mode, reason string }{
		{"raw-span-outside", "outside the 20-byte source"},
		{"raw-edit-outside", "outside the 20-byte source"},
		{"raw-edit-overlap", "overlapping edits at bytes 2 and 5"},
		{"raw-edit-same-start", "overlapping edits at bytes 8 and 8"},
		{"raw-edit-none", "between 1 and 64 edits"},
	} {
		report := runFixture(t, context.Background(), Limits{}, run.mode)
		// An invalid finding fails its rule, not the pack, and is never
		// reported in part.
		if report.Complete || report.Failure != nil || report.Rules[0].Status != StatusFailed || !strings.HasPrefix(report.Rules[0].Reason, "invalid output: ") || !strings.Contains(report.Rules[0].Reason, run.reason) || len(report.Findings) != 0 {
			t.Fatalf("%s: %+v", run.mode, report)
		}
	}
}

func TestRunRefusesSpawnFailuresAndUnselectedPacks(t *testing.T) {
	manifest := fixtureManifest(t)
	manifest.Executable = Executable{Path: "missing-pack"}
	registry, err := NewRegistry(testBuiltins, manifest)
	if err != nil {
		t.Fatal(err)
	}
	configuration := configure(t, registry, `{"version":1}`)
	// A bare name resolves against Dir, never PATH.
	report, err := Run(context.Background(), configuration, "fixture", testSnapshot(), RunOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	assertPackFailure(t, report, FailureSpawn, "missing-pack")
	if _, err := Run(context.Background(), configuration, "other", testSnapshot(), RunOptions{}); err == nil {
		t.Fatal("an unselected namespace ran")
	}
}

func TestServeAnswersAnUnsupportedProtocolWithItsOwnVersion(t *testing.T) {
	var out bytes.Buffer
	err := Serve(testPack(func(*Pass) error { return nil }), strings.NewReader("EFFRA-LINT 2 2\n{}"), &out)
	if err == nil || !strings.HasPrefix(out.String(), "EFFRA-LINT 1 ") || !strings.Contains(out.String(), "unsupported protocol version 2") {
		t.Fatalf("%q %v", out.String(), err)
	}
	for _, input := range []string{"", "EFFRA-LINT 1 05\n{}", "EFFRA-LINT 1 -2\n{}", "EFFRA-LINT 1 2\n{}x", "EFFRA-LINT 1 9\n{}", "EFFRA-LINT 1 2 extra\n{}"} {
		if err := Serve(testPack(nil), strings.NewReader(input), &out); err == nil {
			t.Fatalf("accepted framing %q", input)
		}
	}
}

// A response that answers an earlier rule validly but omits a later one is
// refused as a whole: none of its findings is published and every requested
// rule has exactly one failed status.
func TestRunRefusesAPartialResponseAtomically(t *testing.T) {
	manifest := fixtureManifest(t, "raw-valid")
	second := manifest.Rules[0]
	second.Name = "zz-second"
	manifest.Rules = append(manifest.Rules, second)
	registry, err := NewRegistry(testBuiltins, manifest)
	if err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), configure(t, registry, `{"version":1}`), "fixture", testSnapshot(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := []RuleStatus{
		{Rule: "fixture/rename-main", Status: StatusFailed, Reason: "pack-failed: " + FailureInvalid},
		{Rule: "fixture/zz-second", Status: StatusFailed, Reason: "pack-failed: " + FailureInvalid},
	}
	if report.Complete || report.Failure == nil || report.Failure.Code != FailureInvalid || !strings.Contains(report.Failure.Message, `no result for rule "zz-second"`) ||
		len(report.Findings) != 0 || !reflect.DeepEqual(report.Rules, want) {
		t.Fatalf("partial response leaked into the report: %+v %+v", report, report.Failure)
	}
}

// The response byte limit is part of shared acceptance: output that the
// process path refuses at the default limit is refused in-process too, with
// the same failure and no findings. Every finding is individually valid.
func TestEvaluateAppliesTheResponseByteLimitLikeRun(t *testing.T) {
	bulky := &Pack{
		Namespace: "fixture", Version: "1.0.0", FactVersions: []int{FactSchemaVersion},
		Rules: []*Rule{{
			Name: "rename-main", Version: "1", Description: "fixture rule", DefaultSeverity: SeverityWarning,
			Requires: []Family{FamilyCallables},
			Options:  []OptionSpec{{Name: "to", Type: OptionString, Default: []byte(`"entry"`)}},
			// The fixture program's "bulky" mode reports the same findings.
			Check: func(pass *Pass) error {
				span := pass.Snapshot.FunctionNamed("main").Span
				text := strings.Repeat("x", 4000)
				for i := 0; i < 1000; i++ {
					related := []Related{}
					for j := 0; j < 4; j++ {
						related = append(related, Related{Message: text, Span: span})
					}
					pass.Report(Finding{Message: text, Span: span, Related: related})
				}
				return nil
			},
		}},
	}
	registry, err := NewRegistry(testBuiltins, fixtureManifest(t, "bulky"))
	if err != nil {
		t.Fatal(err)
	}
	configuration := configure(t, registry, `{"version":1}`)
	evaluated, err := Evaluate(bulky, configuration, testSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	run, err := Run(context.Background(), configuration, "fixture", testSnapshot(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for name, report := range map[string]Report{"Evaluate": evaluated, "Run": run} {
		if report.Complete || report.Failure == nil || report.Failure.Code != FailureOversized || len(report.Findings) != 0 {
			t.Fatalf("%s accepted an oversized response: complete=%v failure=%+v findings=%d", name, report.Complete, report.Failure, len(report.Findings))
		}
	}
	if evaluated.Failure.Message != run.Failure.Message || !reflect.DeepEqual(evaluated.Rules, run.Rules) {
		t.Fatalf("in-process and process refusals differ:\n%+v %+v\n%+v %+v", evaluated.Failure, evaluated.Rules, run.Failure, run.Rules)
	}
}
