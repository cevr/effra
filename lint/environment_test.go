package lint

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func noHost(string) (string, bool) { return "", false }

// The environment a process receives is exactly what its identity digests:
// later entries win, values are bytes, empty differs from absent, and
// entry order and unrelated variables do not matter.
func TestProcessEnvironmentIsQualifiedByteExactly(t *testing.T) {
	digestOf := func(goos string, env ...string) string {
		t.Helper()
		admitted, err := newProcessEnvironment(goos, env, func(string) (string, bool) { return `C:\Windows`, true })
		if err != nil {
			t.Fatal(err)
		}
		return admitted.digest("amd64")
	}
	alpha, beta := digestOf("linux", "EF_LINT_PROBE=alpha"), digestOf("linux", "EF_LINT_PROBE=beta")
	empty, absent := digestOf("linux", "EF_LINT_PROBE="), digestOf("linux")
	if len(map[string]bool{alpha: true, beta: true, empty: true, absent: true}) != 4 {
		t.Fatal("selected values, an empty value and absence collide")
	}
	if digestOf("linux", "A=1", "B=2") != digestOf("linux", "B=2", "A=1") || digestOf("linux", "A=0", "A=1") != digestOf("linux", "A=1") {
		t.Fatal("entry order or a replaced entry changed the identity")
	}
	// Framing: no value can absorb the next name.
	if digestOf("linux", "A=BC") == digestOf("linux", "AB=C") || digestOf("linux", "A=1B=2") == digestOf("linux", "A=1", "B=2") || digestOf("linux", "A=\xff") == digestOf("linux", "A=\ufffd") {
		t.Fatal("distinct environments share a digest")
	}
	if digestOf("linux", "PATH=/bin") == digestOf("windows", "PATH=/bin") {
		t.Fatal("the platform is not qualified")
	}
	admitted, _ := newProcessEnvironment("linux", []string{"B=2", "A=\xff", "B=3"}, noHost)
	if !reflect.DeepEqual(admitted.entries(), []string{"A=\xff", "B=3"}) || !reflect.DeepEqual(admitted.names(), []string{"A", "B"}) {
		t.Fatalf("%q", admitted.entries())
	}
	if none, _ := newProcessEnvironment("linux", nil, noHost); none.entries() == nil || len(none.entries()) != 0 {
		t.Fatal("an empty environment must not be nil: os/exec would inherit")
	}
	if _, err := newProcessEnvironment("linux", []string{"NOVALUE"}, noHost); err == nil {
		t.Fatal("an entry without = was admitted")
	}

	// Windows: names compare without case, SYSTEMROOT comes from the host
	// when missing and must be nonempty, and values must be UTF-8.
	windows, err := newProcessEnvironment("windows", []string{"Path=a", "PATH=b"}, func(name string) (string, bool) { return `C:\Windows`, name == "SYSTEMROOT" })
	if err != nil || !reflect.DeepEqual(windows.entries(), []string{"PATH=b", `SYSTEMROOT=C:\Windows`}) {
		t.Fatalf("%q %v", windows.entries(), err)
	}
	if digestOf("windows", "Path=x") != digestOf("windows", "PATH=x") {
		t.Fatal("Windows names differing in case are distinct identities")
	}
	if _, err := newProcessEnvironment("windows", nil, noHost); err == nil || !strings.Contains(err.Error(), "SYSTEMROOT") {
		t.Fatalf("missing SYSTEMROOT: %v", err)
	}
	if _, err := newProcessEnvironment("windows", []string{"SYSTEMROOT="}, func(string) (string, bool) { return "", true }); err == nil {
		t.Fatal("an empty SYSTEMROOT was admitted")
	}
	if _, err := newProcessEnvironment("windows", []string{`SYSTEMROOT=C:\W`, "A=\xff"}, noHost); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid UTF-8 on Windows: %v", err)
	}
}

// A capture resolves selections: missing stays absent, empty stays empty,
// unselected variables never pass, and Windows always adds SYSTEMROOT.
func TestHostEnvironmentSelection(t *testing.T) {
	host := captureHost("linux", []string{"EF_A=1", "EF_EMPTY=", "SECRET=x", "=C:=C:\\", "BROKEN"})
	if got := host.Select([]string{"EF_A", "EF_EMPTY", "EF_MISSING"}); !reflect.DeepEqual(got, []string{"EF_A=1", "EF_EMPTY="}) {
		t.Fatalf("%q", got)
	}
	if got := host.Select(nil); len(got) != 0 {
		t.Fatalf("an empty selection passed %q", got)
	}
	windows := captureHost("windows", []string{"SystemRoot=C:\\Windows", "Path=C:\\bin", "SECRET=x"})
	if got := windows.Select([]string{"PATH"}); !reflect.DeepEqual(got, []string{"Path=C:\\bin", "SystemRoot=C:\\Windows"}) {
		t.Fatalf("%q", got)
	}
	for _, name := range []string{"PATH", "_x", "a1"} {
		if !ValidEnvironmentName(name) {
			t.Fatalf("%s refused", name)
		}
	}
	for _, name := range []string{"", "1A", "A-B", "A=B", "Ä"} {
		if ValidEnvironmentName(name) {
			t.Fatalf("%q admitted", name)
		}
	}
}

// Each pack's selection is the last one stated by its presets and then its
// pack entry; it is part of the configuration identity by name only.
func TestConfigurationSelectsEnvironmentPerPack(t *testing.T) {
	pack := presetPack()
	pack.Presets = append(pack.Presets, Preset{Name: "node", Env: Selects("PATH", "NODE_PATH")}, Preset{Name: "bare", Env: Selects()})
	manifest := testManifest(t, pack)
	data, _ := json.Marshal(manifest)
	if parsed, err := ParseManifest(data); err != nil || !reflect.DeepEqual(parsed.Presets, manifest.Presets) {
		t.Fatalf("preset environment did not round-trip: %v %s", err, data)
	}
	registry, err := NewRegistry(testBuiltins, manifest)
	if err != nil {
		t.Fatal(err)
	}
	selected := func(env string) Config {
		config, err := ParseConfig([]byte(`{"version":1,"packs":[{"manifest":"m"` + env + `}],"extends":["acme/node"]}`))
		if err != nil {
			t.Fatal(err)
		}
		config.Packs[0].Namespace = "acme"
		return config
	}
	cases := []struct {
		env  string
		want []string
	}{
		{``, []string{"NODE_PATH", "PATH"}},
		{`,"env":[]`, []string{}},
		{`,"env":["HOME"]`, []string{"HOME"}},
	}
	identities := map[string]bool{}
	for _, c := range cases {
		configuration, problems := registry.Configure(selected(c.env))
		if len(problems) > 0 {
			t.Fatal(problems)
		}
		if got := configuration.Environment("acme"); !slices.Equal(got, c.want) {
			t.Fatalf("%s: %q, want %q", c.env, got, c.want)
		}
		identities[configuration.Identity()] = true
		for _, info := range configuration.Inspect() {
			if info.Rule == "acme/no-op" && !slices.Equal(info.Environment, c.want) && len(c.want) > 0 {
				t.Fatalf("inspection %+v", info)
			}
		}
	}
	if len(identities) != 3 {
		t.Fatal("environment selections share a configuration identity")
	}
	later, _ := ParseConfig([]byte(`{"version":1,"extends":["acme/node","acme/bare"]}`))
	if configuration, _ := registry.Configure(later); len(configuration.Environment("acme")) != 0 {
		t.Fatal("a later preset's empty selection did not replace an earlier one")
	}
	none, _ := ParseConfig([]byte(`{"version":1}`))
	empty := selected(`,"env":[]`)
	empty.Extends = nil
	a, _ := registry.Configure(none)
	b, _ := registry.Configure(empty)
	if a.Identity() != b.Identity() {
		t.Fatal("selecting no variables differs from the default")
	}

	refusals := []struct{ env, want string }{
		{`,"env":["1PATH"]`, "invalid environment variable name"},
		{`,"env":["PATH","PATH"]`, "selected twice"},
	}
	for _, c := range refusals {
		if configuration, problems := registry.Configure(selected(c.env)); configuration != nil || len(problems) != 1 || problems[0].Code != ProblemInvalidEnvironment || !strings.Contains(problems[0].Message, c.want) {
			t.Fatalf("%s: %+v", c.env, problems)
		}
	}
	unresolved := selected(`,"env":["PATH"]`)
	unresolved.Packs[0].Namespace = ""
	if _, problems := registry.Configure(unresolved); len(problems) != 1 || problems[0].Code != ProblemInvalidEnvironment {
		t.Fatalf("an unloaded pack selection's environment: %+v", problems)
	}
	if runtime.GOOS == "windows" {
		t.Skip("case-insensitive duplicate names are checked on Windows only")
	}
	if _, problems := registry.Configure(selected(`,"env":["Path","PATH"]`)); len(problems) != 0 {
		t.Fatalf("Unix names differing in case are distinct: %+v", problems)
	}
	bad := presetPack()
	bad.Presets = []Preset{{Name: "p", Env: Selects("A", "A")}}
	if _, err := bad.Manifest(Executable{Path: "x"}); err == nil || !strings.Contains(err.Error(), "selected twice") {
		t.Fatalf("duplicate preset environment: %v", err)
	}
}

// A request ID commits to the execution that answers it: a response for
// the same snapshot and options under another environment cannot bind.
func TestRequestBindingCommitsToTheEnvironment(t *testing.T) {
	registry, err := NewRegistry(testBuiltins, testManifest(t, testPack(func(*Pass) error { return nil })))
	if err != nil {
		t.Fatal(err)
	}
	configuration := configure(t, registry, `{"version":1}`)
	bound := func(environment string) *execution {
		work, err := prepare(configuration, "acme", testSnapshot())
		if err != nil {
			t.Fatal(err)
		}
		work.bind(&ExecutionIdentity{Executable: "sha256:x", Complete: true, Environment: environment})
		return work
	}
	alpha, beta := bound("sha256:alpha"), bound("sha256:beta")
	if alpha.request.ID == beta.request.ID || alpha.report.Analysis.Digest == beta.report.Analysis.Digest {
		t.Fatal("different environments share a request or analysis identity")
	}
	response := Response{ID: alpha.request.ID, Revision: "r1", Rules: []RuleResult{{Name: "no-op", Status: StatusCompleted, Findings: []Finding{}}}}
	if failure := beta.accept(response, 0, DefaultLimits()); failure == nil || failure.Code != FailureInvalid {
		t.Fatalf("a response under another environment bound: %+v", failure)
	}
	if failure := alpha.accept(response, 0, DefaultLimits()); failure != nil {
		t.Fatal(failure)
	}
	// The in-process harness binds in its own domain and is never reusable.
	report, err := Evaluate(testPack(func(*Pass) error { return nil }), configuration, testSnapshot())
	if err != nil || !report.Analysis.InProcess || report.Analysis.ReuseScope != "none" || report.Analysis.Execution != nil {
		t.Fatalf("%+v %v", report.Analysis, err)
	}
}

// A real pack process sees exactly its selected variables: distinct
// values, an empty value and absence are distinct analyses, and the
// process never sees what was not passed.
func TestRunPassesExactlyTheQualifiedEnvironment(t *testing.T) {
	registry, err := NewRegistry(testBuiltins, fixtureManifest(t, "env"))
	if err != nil {
		t.Fatal(err)
	}
	configuration := configure(t, registry, `{"version":1}`)
	run := func(env ...string) Report {
		t.Helper()
		report, err := Run(context.Background(), configuration, "fixture", testSnapshot(), RunOptions{Env: env})
		if err != nil || report.Failure != nil || len(report.Findings) != 1 {
			t.Fatalf("%+v %v", report, err)
		}
		return report
	}
	extra := len(RequiredVariables(runtime.GOOS))
	cases := []struct {
		env     []string
		message string
	}{
		{[]string{"EF_LINT_PROBE=alpha"}, "present:alpha"},
		{[]string{"EF_LINT_PROBE=beta"}, "present:beta"},
		{[]string{"EF_LINT_PROBE="}, "present:"},
		{nil, "absent"},
	}
	digests := map[string]bool{}
	for _, c := range cases {
		report := run(c.env...)
		want := len(c.env) + extra
		if message := report.Findings[0].Message; !strings.HasSuffix(message, " "+c.message) || !strings.HasPrefix(message, fmt.Sprintf("env %d ", want)) {
			t.Fatalf("%q: %s", c.env, message)
		}
		digests[report.Analysis.Digest] = true
		if names := report.Analysis.Execution.Variables; len(names) != want {
			t.Fatalf("variables %q", names)
		}
	}
	if len(digests) != len(cases) {
		t.Fatal("different environments share an analysis identity")
	}
	if run("EF_LINT_PROBE=alpha").Analysis.Digest != run("EF_LINT_PROBE=alpha").Analysis.Digest {
		t.Fatal("equal environments differ in identity")
	}
	report, _ := Run(context.Background(), configuration, "fixture", testSnapshot(), RunOptions{Env: []string{"BROKEN"}})
	if report.Failure == nil || report.Failure.Code != FailureSpawn || report.Analysis.Execution != nil {
		t.Fatalf("an invalid environment started the pack: %+v", report)
	}
}
