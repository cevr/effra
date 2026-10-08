package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func presetPack() *Pack {
	pack := testPack(func(*Pass) error { return nil })
	pack.Presets = []Preset{
		{Name: "strict", Description: "fail on everything", Rules: map[string]RuleConfig{"no-op": {Severity: SeverityError, Options: json.RawMessage(`{"names":["x"]}`)}}},
		{Name: "quiet", Rules: map[string]RuleConfig{"no-op": {Severity: SeverityOff}}},
	}
	return pack
}

func TestPresetsLayerUnderTheConfiguration(t *testing.T) {
	manifest := testManifest(t, presetPack())
	data, _ := json.Marshal(manifest)
	if parsed, err := ParseManifest(data); err != nil || !reflect.DeepEqual(parsed.Presets, manifest.Presets) {
		t.Fatalf("presets did not round-trip: %v %+v", err, parsed.Presets)
	}
	registry, err := NewRegistry(testBuiltins, manifest)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, document string
		severity       Severity
		names          []string
	}{
		{"preset", `{"version":1,"extends":["acme/strict"]}`, SeverityError, []string{"x"}},
		// A later severity replaces the preset's; its omitted options keep the preset's.
		{"severity over preset", `{"version":1,"extends":["acme/strict"],"rules":{"acme/no-op":"hint"}}`, SeverityHint, []string{"x"}},
		{"options over preset", `{"version":1,"extends":["acme/strict"],"rules":{"acme/no-op":{"options":{"names":["y"]}}}}`, SeverityError, []string{"y"}},
		// Presets apply in order; the later one wins what it states.
		{"later preset", `{"version":1,"extends":["acme/strict","acme/quiet"]}`, SeverityOff, []string{"x"}},
		{"earlier preset", `{"version":1,"extends":["acme/quiet","acme/strict"]}`, SeverityError, []string{"x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setting, _ := configure(t, registry, c.document).Setting("acme/no-op")
			if setting.Severity != c.severity || !setting.Configured || !reflect.DeepEqual(setting.Options.StringList("names"), c.names) {
				t.Fatalf("%+v", setting)
			}
		})
	}
	// A preset is shorthand: it has the identity of the settings it expands to.
	extended := configure(t, registry, `{"version":1,"extends":["acme/strict"]}`)
	written := configure(t, registry, `{"version":1,"rules":{"acme/no-op":{"severity":"error","options":{"names":["x"]}}}}`)
	if extended.Identity() != written.Identity() {
		t.Fatal("a preset and the settings it expands to have different identities")
	}
	refusals := []struct{ name, document, code string }{
		{"unknown preset", `{"version":1,"extends":["acme/missing"]}`, ProblemUnknownPreset},
		{"unselected pack", `{"version":1,"extends":["other/strict"]}`, ProblemUnknownNamespace},
		{"unqualified", `{"version":1,"extends":["strict"]}`, ProblemUnknownNamespace},
		{"twice", `{"version":1,"extends":["acme/strict","acme/strict"]}`, ProblemUnknownPreset},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			config, err := ParseConfig([]byte(c.document))
			if err != nil {
				t.Fatal(err)
			}
			if configuration, problems := registry.Configure(config); configuration != nil || len(problems) != 1 || problems[0].Code != c.code {
				t.Fatalf("got %+v, want %s", problems, c.code)
			}
		})
	}
}

func TestManifestPresetRefusals(t *testing.T) {
	cases := []struct {
		name   string
		preset Preset
		want   string
	}{
		{"unknown rule", Preset{Name: "p", Rules: map[string]RuleConfig{"missing": {Severity: SeverityError}}}, `no rule "missing"`},
		{"bad severity", Preset{Name: "p", Rules: map[string]RuleConfig{"no-op": {Severity: "fatal"}}}, `invalid severity "fatal"`},
		{"bad options", Preset{Name: "p", Rules: map[string]RuleConfig{"no-op": {Options: json.RawMessage(`{"label":"z"}`)}}}, "not one of"},
		{"empty", Preset{Name: "p", Rules: map[string]RuleConfig{}}, "at least one rule"},
		{"bad name", Preset{Name: "Strict", Rules: map[string]RuleConfig{"no-op": {Severity: SeverityError}}}, "kebab-case"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pack := testPack(func(*Pass) error { return nil })
			pack.Presets = []Preset{c.preset}
			if _, err := pack.Manifest(Executable{Path: "x"}); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
	pack := presetPack()
	pack.Presets = append(pack.Presets, pack.Presets[0])
	if _, err := pack.Manifest(Executable{Path: "x"}); err == nil || !strings.Contains(err.Error(), "duplicate preset acme/strict") {
		t.Fatalf("duplicate preset accepted: %v", err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigResolvesManifestsAgainstItsDirectory(t *testing.T) {
	root := t.TempDir()
	manifest, _ := json.Marshal(testManifest(t, presetPack()))
	writeFile(t, filepath.Join(root, "packs", "acme", "manifest.json"), string(manifest))
	writeFile(t, filepath.Join(root, "config", "lint.json"), `{"version":1,"packs":[{"manifest":"../packs/acme/manifest.json"}],"extends":["acme/strict"]}`)
	// The caller's directory has no packs/ directory: only the
	// configuration's own directory can resolve the manifest.
	os.MkdirAll(filepath.Join(root, "elsewhere", "deeper"), 0o755)
	t.Chdir(filepath.Join(root, "elsewhere", "deeper"))
	selection, err := LoadConfig("../../config/lint.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, ok := selection.Pack("acme")
	if !ok || len(selection.Packs) != 1 || pack.Dir != filepath.Join(root, "packs", "acme") || pack.Path != filepath.Join(pack.Dir, "manifest.json") {
		t.Fatalf("%+v", selection.Packs)
	}
	registry, err := NewRegistry(testBuiltins, selection.Manifests()...)
	if err != nil {
		t.Fatal(err)
	}
	configuration, problems := registry.Configure(selection.Config)
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if setting, _ := configuration.Setting("acme/no-op"); setting.Severity != SeverityError {
		t.Fatalf("preset of a loaded pack: %+v", setting)
	}

	refusals := []struct{ name, config, want string }{
		{"missing manifest", `{"version":1,"packs":[{"manifest":"none.json"}]}`, "none.json"},
		{"empty manifest path", `{"version":1,"packs":[{"manifest":""}]}`, "needs a manifest path"},
		{"invalid manifest", `{"version":1,"packs":[{"manifest":"bad.json"}]}`, "invalid manifest"},
		{"directory manifest", `{"version":1,"packs":[{"manifest":"dir"}]}`, "not a regular file"},
		{"directory executable", `{"version":1,"packs":[{"manifest":"program/manifest.json"}]}`, "is not a regular file"},
		{"unknown field", `{"version":1,"packs":[{"manifest":"x","inherit":true}]}`, "unknown field"},
		{"env not a list", `{"version":1,"packs":[{"manifest":"x","env":"PATH"}]}`, "env must be an array"},
		{"env null", `{"version":1,"packs":[{"manifest":"x","env":null}]}`, "not null"},
		{"env not names", `{"version":1,"packs":[{"manifest":"x","env":[1]}]}`, "env must be an array"},
		{"invalid document", `{"version":1,`, "invalid lint configuration"},
	}
	writeFile(t, filepath.Join(root, "refusals", "bad.json"), `{"manifestVersion":1}`)
	os.MkdirAll(filepath.Join(root, "refusals", "dir"), 0o755)
	// An executable that exists must be a regular file. (An absent one
	// loads: rules can be inspected before the pack is built.)
	os.MkdirAll(filepath.Join(root, "refusals", "program", "bin", "acme-lint"), 0o755)
	writeFile(t, filepath.Join(root, "refusals", "program", "manifest.json"), string(manifest))
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(root, "refusals", "lint.json")
			writeFile(t, path, c.config)
			if _, err := LoadConfig(path); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v, want %q", err, c.want)
			}
		})
	}
	if _, err := LoadConfig(filepath.Join(root, "absent.json")); err == nil {
		t.Fatal("a missing configuration file loaded")
	}
	writeFile(t, filepath.Join(root, "huge.json"), strings.Repeat(" ", MaxConfigBytes+1))
	if _, err := LoadConfig(filepath.Join(root, "huge.json")); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized configuration: %v", err)
	}
	if _, err := LoadManifest(filepath.Join(root, "refusals", "bad.json")); err == nil {
		t.Fatal("an invalid manifest loaded")
	}
}
