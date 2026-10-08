package lint

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func testPack(check func(*Pass) error) *Pack {
	return &Pack{
		Namespace: "acme", Version: "1.0.0", FactVersions: []int{FactSchemaVersion},
		Rules: []*Rule{{
			Name: "no-op", Version: "1", Description: "test rule", DefaultSeverity: SeverityWarning,
			Requires: []Family{FamilyCallables},
			Options: []OptionSpec{
				{Name: "label", Type: OptionString, Enum: []string{"a", "b"}, Default: json.RawMessage(`"a"`)},
				{Name: "names", Type: OptionStringList},
				{Name: "limit", Type: OptionInteger},
			},
			Check: check,
		}},
	}
}

func testManifest(t *testing.T, pack *Pack) Manifest {
	t.Helper()
	manifest, err := pack.Manifest(Executable{Path: "bin/acme-lint", Args: []string{"--protocol", "1"}})
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func testSnapshot() *Snapshot {
	return &Snapshot{
		Schema:    Schema{Name: FactSchemaName, Version: FactSchemaVersion},
		Semantic:  Semantic{SchemaVersion: 7, Revision: "r1", Target: "go", Producer: "sha256:p", ReuseScope: "artifact"},
		Source:    Source{Bytes: 20},
		Checked:   true,
		Families:  []Family{FamilyDeclarations, FamilyCallables},
		Callables: []Callable{{Identity: "function:main", Name: "main", Kind: CallableFunction, Span: Span{Offset: 2, Length: 4, Line: 1, Column: 3}}},
	}
}

func configure(t *testing.T, registry *Registry, document string) *Configuration {
	t.Helper()
	config, err := ParseConfig([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	configuration, problems := registry.Configure(config)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	return configuration
}

var testBuiltins = []BuiltinRule{
	{Name: "unused-recipe", Code: "EFL001", Description: "unused", DefaultSeverity: SeverityWarning},
	{Name: "invalid-suppression", Code: "EFL004", Description: "suppression", DefaultSeverity: SeverityError, Fixed: true},
}

func TestManifestRoundTripAndIdentity(t *testing.T) {
	manifest := testManifest(t, testPack(func(*Pass) error { return nil }))
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Identity() != manifest.Identity() || !strings.HasPrefix(manifest.Identity(), "sha256:") {
		t.Fatalf("round trip changed identity: %s %s", parsed.Identity(), manifest.Identity())
	}
	changed := testPack(func(*Pass) error { return nil })
	changed.Rules[0].Options[0].Default = json.RawMessage(`"b"`)
	if testManifest(t, changed).Identity() == manifest.Identity() {
		t.Fatal("an option schema change kept the pack identity")
	}
	moved, err := testPack(func(*Pass) error { return nil }).Manifest(Executable{Path: "bin/other"})
	if err != nil || moved.Identity() == manifest.Identity() {
		t.Fatalf("an executable change kept the pack identity: %v", err)
	}
}

func TestManifestValidationRefusals(t *testing.T) {
	valid := testManifest(t, testPack(func(*Pass) error { return nil }))
	data, _ := json.Marshal(valid)
	base := string(data)
	cases := []struct{ name, document, want string }{
		{"unknown field", strings.Replace(base, `"namespace"`, `"extra":1,"namespace"`, 1), "unknown field"},
		{"duplicate key", strings.Replace(base, `"namespace":"acme"`, `"namespace":"acme","namespace":"other"`, 1), "duplicate key"},
		{"trailing data", base + "{}", "after JSON value"},
		{"version", strings.Replace(base, `"manifestVersion":1`, `"manifestVersion":2`, 1), "manifestVersion"},
		{"reserved namespace", strings.Replace(base, `"namespace":"acme"`, `"namespace":"effra"`, 1), "reserved"},
		{"bad namespace", strings.Replace(base, `"namespace":"acme"`, `"namespace":"Acme"`, 1), "kebab-case"},
		{"bad severity", strings.Replace(base, `"defaultSeverity":"warning"`, `"defaultSeverity":"fatal"`, 1), "invalid default severity"},
		{"off is not a default", strings.Replace(base, `"defaultSeverity":"warning"`, `"defaultSeverity":"off"`, 1), "invalid default severity"},
		{"unknown family", strings.Replace(base, `"requires":["callables"]`, `"requires":["tokens"]`, 1), "unknown fact family"},
		{"no family", strings.Replace(base, `"requires":["callables"]`, `"requires":[]`, 1), "at least one fact family"},
		{"fact schema", strings.Replace(base, FactSchemaName, "other.facts", 1), "factSchema"},
		{"no executable", strings.Replace(base, `"path":"bin/acme-lint"`, `"path":""`, 1), "executable path"},
		{"bad option type", strings.Replace(base, `"type":"integer"`, `"type":"float"`, 1), "unsupported type"},
		{"bad default", strings.Replace(base, `"default":"a"`, `"default":"c"`, 1), "not one of"},
		{"bad target", strings.Replace(base, `"requires":["callables"]`, `"requires":["callables"],"targets":["wasm"]`, 1), "unsupported target"},
		{"null default", strings.Replace(base, `"default":"a"`, `"default":null`, 1), "null is not a string value"},
		{"null list default", strings.Replace(base, `"type":"string-list"`, `"type":"string-list","default":[null]`, 1), "expected an array of strings"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(c.document)); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
	duplicate := testPack(func(*Pass) error { return nil })
	duplicate.Rules = append(duplicate.Rules, duplicate.Rules[0])
	if _, err := duplicate.Manifest(Executable{Path: "x"}); err == nil || !strings.Contains(err.Error(), "duplicate rule acme/no-op") {
		t.Fatalf("duplicate rule ids accepted: %v", err)
	}
	duplicateOption := testPack(func(*Pass) error { return nil })
	duplicateOption.Rules[0].Options = append(duplicateOption.Rules[0].Options, duplicateOption.Rules[0].Options[0])
	if _, err := duplicateOption.Manifest(Executable{Path: "x"}); err == nil || !strings.Contains(err.Error(), "duplicate option") {
		t.Fatalf("duplicate option accepted: %v", err)
	}
	required := testPack(func(*Pass) error { return nil })
	required.Rules[0].Options[0].Required = true
	if _, err := required.Manifest(Executable{Path: "x"}); err == nil || !strings.Contains(err.Error(), "cannot have a default") {
		t.Fatalf("required option with default accepted: %v", err)
	}
	if _, err := ParseManifest(make([]byte, MaxManifestBytes+1)); err == nil {
		t.Fatal("oversized manifest accepted")
	}
}

func TestRegistryRefusesDuplicateAndReservedIdentities(t *testing.T) {
	manifest := testManifest(t, testPack(func(*Pass) error { return nil }))
	if _, err := NewRegistry(testBuiltins, manifest, manifest); err == nil || !strings.Contains(err.Error(), "duplicate rule pack namespace acme") {
		t.Fatalf("duplicate namespace accepted: %v", err)
	}
	if _, err := NewRegistry(append(testBuiltins, testBuiltins[0])); err == nil {
		t.Fatal("duplicate built-in accepted")
	}
	reserved := manifest
	reserved.Namespace = "compiler"
	if _, err := NewRegistry(testBuiltins, reserved); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("reserved namespace accepted: %v", err)
	}
}

func TestConfigurationValidation(t *testing.T) {
	ran := false
	registry, err := NewRegistry(testBuiltins, testManifest(t, testPack(func(*Pass) error { ran = true; return nil })))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, document, code string }{
		{"version", `{"version":2}`, ProblemVersion},
		{"unknown namespace", `{"version":1,"rules":{"other/no-op":"error"}}`, ProblemUnknownNamespace},
		{"unknown pack rule", `{"version":1,"rules":{"acme/missing":"error"}}`, ProblemUnknownRule},
		{"unknown builtin", `{"version":1,"rules":{"no-console":"error"}}`, ProblemUnknownRule},
		{"bad severity", `{"version":1,"rules":{"acme/no-op":"fatal"}}`, ProblemInvalidSeverity},
		{"suggestion is not public", `{"version":1,"rules":{"unused-recipe":"suggestion"}}`, ProblemInvalidSeverity},
		{"fixed severity", `{"version":1,"rules":{"invalid-suppression":"warning"}}`, ProblemFixedSeverity},
		{"fixed off", `{"version":1,"rules":{"invalid-suppression":"off"}}`, ProblemFixedSeverity},
		{"unknown option", `{"version":1,"rules":{"acme/no-op":{"options":{"other":1}}}}`, ProblemInvalidOptions},
		{"wrong option type", `{"version":1,"rules":{"acme/no-op":{"options":{"limit":"ten"}}}}`, ProblemInvalidOptions},
		{"enum option", `{"version":1,"rules":{"acme/no-op":{"options":{"label":"z"}}}}`, ProblemInvalidOptions},
		{"options not object", `{"version":1,"rules":{"acme/no-op":{"options":[1]}}}`, ProblemInvalidOptions},
		{"builtin has no options", `{"version":1,"rules":{"unused-recipe":{"options":{"x":true}}}}`, ProblemInvalidOptions},
		{"null integer", `{"version":1,"rules":{"acme/no-op":{"options":{"limit":null}}}}`, ProblemInvalidOptions},
		{"null string", `{"version":1,"rules":{"acme/no-op":{"options":{"label":null}}}}`, ProblemInvalidOptions},
		{"null list", `{"version":1,"rules":{"acme/no-op":{"options":{"names":null}}}}`, ProblemInvalidOptions},
		{"null list element", `{"version":1,"rules":{"acme/no-op":{"options":{"names":["a",null]}}}}`, ProblemInvalidOptions},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			config, err := ParseConfig([]byte(c.document))
			if err != nil {
				t.Fatal(err)
			}
			configuration, problems := registry.Configure(config)
			if configuration != nil || len(problems) != 1 || problems[0].Code != c.code {
				t.Fatalf("got %+v, want %s", problems, c.code)
			}
		})
	}
	for _, document := range []string{
		`{"version":1,"rules":{"acme/no-op":"error","acme/no-op":"off"}}`,
		`{"version":1,"rules":{"acme/no-op":{"severity":"error","severity":"off"}}}`,
		`{"version":1,"rules":{"acme/no-op":{"options":{"limit":1,"limit":2}}}}`,
		`{"version":1,"rules":{"acme/no-op":null}}`,
		`{"version":1,"rules":{"acme/no-op":""}}`,
		`{"version":1,"rules":{"acme/no-op":{"severity":null}}}`,
		`{"version":1,"rules":{"acme/no-op":{"severity":""}}}`,
		`{"version":1,"rules":{"acme/no-op":{"severity":1}}}`,
	} {
		config, err := ParseConfig([]byte(document))
		if err == nil {
			_, problems := registry.Configure(config)
			t.Fatalf("malformed rule setting accepted: %s %+v", document, problems)
		}
	}
	if _, err := ParseConfig([]byte(`{"version":1,"preset":"x"}`)); err == nil {
		t.Fatal("unknown configuration field accepted")
	}
	// Omitting severity still selects the default, with options.
	defaults := configure(t, registry, `{"version":1,"rules":{"acme/no-op":{"options":{"names":["x"]}}}}`)
	if setting, _ := defaults.Setting("acme/no-op"); setting.Severity != SeverityWarning || !setting.Configured || !reflect.DeepEqual(setting.Options.StringList("names"), []string{"x"}) {
		t.Fatalf("omitted severity: %+v", setting)
	}
	// A required boolean given null is refused, not read as false.
	required := []OptionSpec{{Name: "enabled", Type: OptionBoolean, Required: true}}
	if _, err := ValidateOptions(required, json.RawMessage(`{"enabled":null}`)); err == nil || !strings.Contains(err.Error(), "null is not a boolean value") {
		t.Fatalf("null required boolean: %v", err)
	}
	if ran {
		t.Fatal("a rule ran while configuration was refused")
	}
}

func TestConfigurationDefaultsInspectionAndIdentity(t *testing.T) {
	ran := false
	pack := testPack(func(*Pass) error { ran = true; return nil })
	manifest := testManifest(t, pack)
	registry, err := NewRegistry(testBuiltins, manifest)
	if err != nil {
		t.Fatal(err)
	}
	defaults := configure(t, registry, `{"version":1}`)
	tuned := configure(t, registry, `{"version":1,"rules":{"acme/no-op":{"severity":"error","options":{"label":"b","names":["main"]}},"unused-recipe":"off"}}`)
	info := tuned.Inspect()
	if len(info) != 3 || info[0].Rule != "acme/no-op" || info[0].DefaultSeverity != SeverityWarning || info[0].Severity != SeverityError || info[0].PackIdentity != manifest.Identity() || info[0].Requires[0] != FamilyCallables || len(info[0].Options) != 3 {
		t.Fatalf("inspection: %+v", info)
	}
	if info[2].Rule != "unused-recipe" || info[2].Severity != SeverityOff || info[2].DefaultSeverity != SeverityWarning || !info[1].Fixed {
		t.Fatalf("built-in inspection: %+v", info)
	}
	if ran {
		t.Fatal("registry, configuration or inspection executed rule code")
	}
	setting, _ := defaults.Setting("acme/no-op")
	if setting.Configured || setting.Options.String("label") != "a" || setting.Options.Has("names") {
		t.Fatalf("defaults were not applied: %+v", setting)
	}
	setting, _ = tuned.Setting("acme/no-op")
	if !setting.Configured || setting.Options.String("label") != "b" || setting.Options.StringList("names")[0] != "main" {
		t.Fatalf("configured options: %+v", setting)
	}
	explicitDefault := configure(t, registry, `{"version":1,"rules":{"acme/no-op":{"options":{"label":"a"}}}}`)
	if explicitDefault.Identity() != defaults.Identity() {
		t.Fatal("an explicit default changed the effective configuration identity")
	}
	if tuned.Identity() == defaults.Identity() {
		t.Fatal("an option change kept the configuration identity")
	}

	snapshot := testSnapshot()
	base := defaults.Analysis(snapshot)
	if base.ReuseScope != "artifact" || base.Configuration != defaults.Identity() || len(base.Packs) != 1 || base.Packs[0] != "acme@"+manifest.Identity() {
		t.Fatalf("analysis identity: %+v", base)
	}
	if tuned.Analysis(snapshot).Digest == base.Digest {
		t.Fatal("lint configuration did not qualify the analysis identity")
	}
	revised := testSnapshot()
	revised.Semantic.Revision = "r2"
	rebuilt := testSnapshot()
	rebuilt.Semantic.Producer = "sha256:q"
	for _, other := range []*Snapshot{revised, rebuilt} {
		if defaults.Analysis(other).Digest == base.Digest {
			t.Fatalf("snapshot change kept the analysis identity: %+v", other.Semantic)
		}
	}
	changedPack := testPack(func(*Pass) error { return nil })
	changedPack.Version = "1.0.1"
	otherRegistry, _ := NewRegistry(testBuiltins, testManifest(t, changedPack))
	otherDefaults := configure(t, otherRegistry, `{"version":1}`)
	if otherDefaults.Identity() != defaults.Identity() || otherDefaults.Analysis(snapshot).Digest == base.Digest {
		t.Fatal("pack identity must qualify the analysis but not the configuration identity")
	}
	unqualified := testSnapshot()
	unqualified.Semantic.Producer, unqualified.Semantic.ReuseScope = "", ""
	if defaults.Analysis(unqualified).ReuseScope != "none" {
		t.Fatal("an unqualified snapshot claimed reusable analysis")
	}
}

// A rule declares the targets it supports and is skipped under another.
// Enabled only by its pack's default or a preset, it does not apply to that
// target: the skip is marked inapplicable and the report stays complete.
// Enabled by the project's own configuration, it is a guarantee the run
// cannot give: the skip leaves the report incomplete. Who enabled the rule
// is part of its effective setting, so it qualifies the configuration.
func TestTargetApplicabilityFollowsWhoEnabledTheRule(t *testing.T) {
	pack := testPack(func(pass *Pass) error {
		pass.Reportf(pass.Snapshot.FunctionNamed("main").Span, "ran")
		return nil
	})
	pack.Rules[0].Targets = []string{"go"}
	pack.Presets = []Preset{{Name: "on", Rules: map[string]RuleConfig{"no-op": {Severity: SeverityError}}}}
	registry, err := NewRegistry(testBuiltins, testManifest(t, pack))
	if err != nil {
		t.Fatal(err)
	}
	js := testSnapshot()
	js.Semantic.Target = "js"
	cases := []struct {
		name, document         string
		snapshot               *Snapshot
		status                 string
		complete, inapplicable bool
	}{
		{"default under go", `{"version":1}`, testSnapshot(), StatusCompleted, true, false},
		{"default under js", `{"version":1}`, js, StatusSkipped, true, true},
		{"preset under js", `{"version":1,"extends":["acme/on"]}`, js, StatusSkipped, true, true},
		{"configured severity under js", `{"version":1,"rules":{"acme/no-op":"warning"}}`, js, StatusSkipped, false, false},
		{"configured options over a preset under js", `{"version":1,"extends":["acme/on"],"rules":{"acme/no-op":{"options":{"label":"b"}}}}`, js, StatusSkipped, false, false},
		{"configured off under js", `{"version":1,"rules":{"acme/no-op":"off"}}`, js, StatusOff, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			report, err := Evaluate(pack, configure(t, registry, c.document), c.snapshot)
			if err != nil {
				t.Fatal(err)
			}
			status := report.Rules[0]
			if status.Status != c.status || report.Complete != c.complete {
				t.Fatalf("%+v", report)
			}
			if status.Inapplicable != c.inapplicable {
				t.Fatalf("inapplicable: %+v", status)
			}
			if c.status == StatusSkipped && (status.Reason != "target-unsupported: js" || len(report.Findings) != 0) {
				t.Fatalf("skip: %+v", report)
			}
		})
	}
	preset := configure(t, registry, `{"version":1,"extends":["acme/on"]}`)
	configured := configure(t, registry, `{"version":1,"rules":{"acme/no-op":"error"}}`)
	if preset.Identity() == configured.Identity() {
		t.Fatal("a preset and the project's configuration enabling a target-restricted rule share a configuration identity")
	}
	if preset.Inspect()[0].Enforced || !configured.Inspect()[0].Enforced {
		t.Fatalf("inspection: %+v %+v", preset.Inspect()[0], configured.Inspect()[0])
	}
}

func TestEvaluateStatuses(t *testing.T) {
	evaluate := func(t *testing.T, check func(*Pass) error, document string, snapshot *Snapshot) Report {
		t.Helper()
		pack := testPack(check)
		registry, err := NewRegistry(testBuiltins, testManifest(t, pack))
		if err != nil {
			t.Fatal(err)
		}
		report, err := Evaluate(pack, configure(t, registry, document), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	reportMain := func(pass *Pass) error {
		pass.Reportf(pass.Snapshot.FunctionNamed("main").Span, "label %s", pass.Options.String("label"))
		return nil
	}
	report := evaluate(t, reportMain, `{"version":1,"rules":{"acme/no-op":{"severity":"error","options":{"label":"b"}}}}`, testSnapshot())
	if !report.Complete || report.Rules[0].Status != StatusCompleted || len(report.Findings) != 1 || !reflect.DeepEqual(report.Findings[0], ReportedFinding{Rule: "acme/no-op", Severity: SeverityError, Message: "label b", Span: Span{2, 4, 1, 3}}) {
		t.Fatalf("completed: %+v", report)
	}
	if report := evaluate(t, reportMain, `{"version":1,"rules":{"acme/no-op":"off"}}`, testSnapshot()); !report.Complete || report.Rules[0].Status != StatusOff || len(report.Findings) != 0 {
		t.Fatalf("off: %+v", report)
	}
	unchecked := testSnapshot()
	unchecked.Checked, unchecked.Families = false, nil
	unchecked.Unavailable = []UnavailableFamily{{FamilyCallables, ReasonUncheckedSource}}
	if report := evaluate(t, reportMain, `{"version":1}`, unchecked); report.Complete || report.Rules[0].Status != StatusSkipped || report.Rules[0].Reason != "facts-unavailable: callables (unchecked-source)" {
		t.Fatalf("unavailable: %+v", report)
	}
	future := testSnapshot()
	future.Schema.Version = FactSchemaVersion + 1
	if report := evaluate(t, reportMain, `{"version":1}`, future); report.Complete || report.Rules[0].Status != StatusFailed || !strings.Contains(report.Rules[0].Reason, "unsupported fact schema") {
		t.Fatalf("schema: %+v", report)
	}
	failures := map[string]func(*Pass) error{
		"panicked":            func(*Pass) error { panic("boom") },
		"outside the 20-byte": func(pass *Pass) error { pass.Reportf(Span{18, 5, 1, 19}, "past EOF"); return nil },
		"non-empty UTF-8":     func(pass *Pass) error { pass.Reportf(Span{0, 0, 1, 1}, ""); return nil },
		"the limit is 1000": func(pass *Pass) error {
			for i := 0; i <= MaxFindingsPerRule; i++ {
				pass.Reportf(Span{0, 0, 1, 1}, "finding %d", i)
			}
			return nil
		},
		"configured function": func(*Pass) error { return errorString("configured function missing") },
	}
	for want, check := range failures {
		report := evaluate(t, check, `{"version":1}`, testSnapshot())
		if report.Complete || report.Rules[0].Status != StatusFailed || !strings.Contains(report.Rules[0].Reason, want) || len(report.Findings) != 0 {
			t.Fatalf("%s: %+v", want, report)
		}
	}
}

// A rule cannot authorize its own output by enlarging the source bound in
// its snapshot: findings are validated against the admitted source.
func TestApplyValidatesAgainstTheAdmittedSource(t *testing.T) {
	outside := Span{Offset: 500, Length: 1, Line: 1, Column: 501}
	for name, check := range map[string]func(*Pass) error{
		"identical span": func(pass *Pass) error { pass.Reportf(outside, "outside"); return nil },
		"enlarged bound": func(pass *Pass) error {
			pass.Snapshot.Source.Bytes = 10_000
			pass.Reportf(outside, "outside")
			return nil
		},
	} {
		pack := testPack(check)
		registry, err := NewRegistry(testBuiltins, testManifest(t, pack))
		if err != nil {
			t.Fatal(err)
		}
		report, err := Evaluate(pack, configure(t, registry, `{"version":1}`), testSnapshot())
		if err != nil || report.Complete || report.Rules[0].Status != StatusFailed || !strings.Contains(report.Rules[0].Reason, "outside the 20-byte source") || len(report.Findings) != 0 {
			t.Fatalf("%s: %+v %v", name, report, err)
		}
	}
}

// In-process rules each read a private copy: a rule that rewrites its
// snapshot cannot change what a later rule or the caller sees.
func TestEvaluateIsolatesRuleSnapshots(t *testing.T) {
	pack := testPack(func(pass *Pass) error {
		pass.Snapshot.Callables[0].Name = "mutated"
		pass.Snapshot.Callables = nil
		return nil
	})
	observed := ""
	pack.Rules = append(pack.Rules, &Rule{
		Name: "observer", Version: "1", Description: "test rule", DefaultSeverity: SeverityWarning,
		Requires: []Family{FamilyCallables},
		Check: func(pass *Pass) error {
			observed = pass.Snapshot.FunctionNamed("main").Name
			return nil
		},
	})
	registry, err := NewRegistry(testBuiltins, testManifest(t, pack))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testSnapshot()
	report, err := Evaluate(pack, configure(t, registry, `{"version":1}`), snapshot)
	if err != nil || !report.Complete {
		t.Fatalf("%+v %v", report, err)
	}
	if observed != "main" || len(snapshot.Callables) != 1 || snapshot.Callables[0].Name != "main" {
		t.Fatalf("a rule's mutation escaped: observed %q, caller %+v", observed, snapshot.Callables)
	}
}

type errorString string

func (e errorString) Error() string { return string(e) }

// The SDK and example pack are consumed by independent rule authors: they
// must depend on the public fact schema, never on compiler internals.
func TestSDKDoesNotImportCompilerInternals(t *testing.T) {
	for _, pattern := range []string{"*.go", "../examples/lintpack/*.go"} {
		files, err := filepath.Glob(pattern)
		if err != nil || len(files) == 0 {
			t.Fatalf("%s: %v", pattern, err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				path, _ := strconv.Unquote(spec.Path.Value)
				if strings.Contains(path, "/internal/") || strings.HasSuffix(path, "/internal") {
					t.Fatalf("%s imports %s", file, path)
				}
			}
		}
	}
}

// A decoded RuleConfig is replaced, never merged: reusing a value for the
// bare-severity shorthand clears earlier options exactly as the equivalent
// object does, and a refused document leaves the value unchanged.
func TestRuleConfigDecodingReplacesAReusedValue(t *testing.T) {
	for _, shorthand := range []string{`"off"`, `{"severity":"off"}`} {
		var setting RuleConfig
		if err := json.Unmarshal([]byte(`{"severity":"warning","options":{"limit":7}}`), &setting); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(shorthand), &setting); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(setting, RuleConfig{Severity: SeverityOff}) {
			t.Fatalf("%s kept part of the earlier setting: %+v (options %s)", shorthand, setting, setting.Options)
		}
	}
	for _, refused := range []string{`""`, `{"severity":""}`, `{"severity":null,"options":{}}`, `{"options":{},"extra":1}`} {
		setting := RuleConfig{Severity: SeverityHint, Options: json.RawMessage(`{"limit":7}`)}
		if err := json.Unmarshal([]byte(refused), &setting); err == nil {
			t.Fatalf("%s decoded", refused)
		}
		if setting.Severity != SeverityHint || string(setting.Options) != `{"limit":7}` {
			t.Fatalf("refused %s changed the value: %+v", refused, setting)
		}
	}
}
