package lint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// BuiltinRule is the static metadata of a compiler-owned rule. Built-in
// names are unqualified and reserved. A Fixed rule's severity cannot be
// configured: it guards configuration itself.
type BuiltinRule struct {
	Name            string   `json:"name"`
	Code            string   `json:"code"`
	Description     string   `json:"description"`
	DefaultSeverity Severity `json:"defaultSeverity"`
	Requires        []Family `json:"requires,omitempty"`
	Fixed           bool     `json:"fixed,omitempty"`
}

// Registry is the set of rules a configuration may name: built-in rules and
// the manifests of explicitly selected packs. It holds metadata only.
type Registry struct {
	builtins []BuiltinRule
	packs    []Manifest
}

// NewRegistry validates built-in metadata and pack manifests together.
// Duplicate namespaces, reserved namespaces and duplicate built-in names
// are refused before any configuration is considered.
func NewRegistry(builtins []BuiltinRule, packs ...Manifest) (*Registry, error) {
	registry := &Registry{builtins: slices.Clone(builtins)}
	names := map[string]bool{}
	for _, rule := range builtins {
		if !validIdentifier(rule.Name) || rule.Code == "" || !rule.DefaultSeverity.valid() {
			return nil, fmt.Errorf("invalid built-in rule %q", rule.Name)
		}
		if names[rule.Name] {
			return nil, fmt.Errorf("duplicate built-in rule %s", rule.Name)
		}
		names[rule.Name] = true
	}
	namespaces := map[string]bool{}
	for _, manifest := range packs {
		if err := manifest.Validate(); err != nil {
			return nil, err
		}
		if namespaces[manifest.Namespace] {
			return nil, fmt.Errorf("duplicate rule pack namespace %s", manifest.Namespace)
		}
		namespaces[manifest.Namespace] = true
		registry.packs = append(registry.packs, manifest)
	}
	slices.SortFunc(registry.packs, func(a, b Manifest) int { return strings.Compare(a.Namespace, b.Namespace) })
	return registry, nil
}

// Pack returns the selected manifest for namespace.
func (r *Registry) Pack(namespace string) (Manifest, bool) {
	for _, manifest := range r.packs {
		if manifest.Namespace == namespace {
			return manifest, true
		}
	}
	return Manifest{}, false
}

// ConfigVersion is the lint configuration format version.
const ConfigVersion = 1

// MaxConfigBytes bounds one configuration document.
const MaxConfigBytes = 1 << 20

// Config is a lint configuration document. Rule keys are built-in names or
// namespace/rule identities of selected packs.
type Config struct {
	Version int                   `json:"version"`
	Rules   map[string]RuleConfig `json:"rules,omitempty"`
}

// RuleConfig selects a severity and options. In JSON, a bare severity
// string is shorthand for {"severity": ...}.
type RuleConfig struct {
	Severity Severity        `json:"severity,omitempty"`
	Options  json.RawMessage `json:"options,omitempty"`
}

func (c *RuleConfig) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return fmt.Errorf("a rule setting must be a severity or an object")
	}
	var severity string
	if err := json.Unmarshal(data, &severity); err == nil {
		*c = RuleConfig{Severity: Severity(severity)}
		return nil
	}
	type plain RuleConfig
	var value plain
	if err := decodeStrict(data, &value); err != nil {
		return err
	}
	*c = RuleConfig(value)
	return nil
}

// ParseConfig strictly decodes a configuration document. Semantic
// validation against a registry is Registry.Configure.
func ParseConfig(data []byte) (Config, error) {
	if len(data) > MaxConfigBytes {
		return Config{}, fmt.Errorf("lint configuration exceeds %d bytes", MaxConfigBytes)
	}
	var config Config
	if err := decodeStrict(data, &config); err != nil {
		return Config{}, fmt.Errorf("invalid lint configuration: %w", err)
	}
	return config, nil
}

// Problem codes reported by Configure.
const (
	ProblemVersion          = "unsupported-version"
	ProblemUnknownNamespace = "unknown-namespace"
	ProblemUnknownRule      = "unknown-rule"
	ProblemInvalidSeverity  = "invalid-severity"
	ProblemFixedSeverity    = "fixed-severity"
	ProblemInvalidOptions   = "invalid-options"
)

// Problem is one configuration refusal. Any problem refuses the whole
// configuration: invalid configuration must never start a pack.
type Problem struct {
	Code    string `json:"code"`
	Rule    string `json:"rule,omitempty"`
	Message string `json:"message"`
}

// RuleSetting is the effective, validated setting of one rule.
type RuleSetting struct {
	Rule            string   `json:"rule"`
	Builtin         bool     `json:"builtin,omitempty"`
	Pack            string   `json:"pack,omitempty"`
	DefaultSeverity Severity `json:"defaultSeverity"`
	Severity        Severity `json:"severity"`
	Configured      bool     `json:"configured,omitempty"`
	Options         Options  `json:"-"`
}

// Configuration is a registry plus validated settings for every rule.
type Configuration struct {
	registry *Registry
	settings []RuleSetting
}

// Configure validates config against the registry. Every rule receives an
// effective setting; unconfigured rules use their default severity and
// default options. A non-empty problem list means no Configuration.
func (r *Registry) Configure(config Config) (*Configuration, []Problem) {
	var problems []Problem
	if config.Version != ConfigVersion {
		problems = append(problems, Problem{Code: ProblemVersion, Message: fmt.Sprintf("lint configuration version must be %d", ConfigVersion)})
	}
	configured := map[string]RuleConfig{}
	keys := make([]string, 0, len(config.Rules))
	for key := range config.Rules {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if problem, ok := r.admitRuleKey(key); !ok {
			problems = append(problems, problem)
			continue
		}
		configured[key] = config.Rules[key]
	}
	configuration := &Configuration{registry: r}
	settle := func(setting RuleSetting, specs []OptionSpec, fixed bool) {
		options := json.RawMessage(nil)
		if rule, ok := configured[setting.Rule]; ok {
			setting.Configured = true
			options = rule.Options
			if rule.Severity != "" {
				switch {
				case fixed && rule.Severity != setting.DefaultSeverity:
					problems = append(problems, Problem{Code: ProblemFixedSeverity, Rule: setting.Rule, Message: "the severity of " + setting.Rule + " cannot be configured"})
				case rule.Severity != SeverityOff && !rule.Severity.valid(), fixed && rule.Severity == SeverityOff:
					problems = append(problems, Problem{Code: ProblemInvalidSeverity, Rule: setting.Rule, Message: fmt.Sprintf("invalid severity %q for %s; use off, error, warning, information or hint", rule.Severity, setting.Rule)})
				default:
					setting.Severity = rule.Severity
				}
			}
		}
		// A disabled rule without options needs no required values; options
		// given to a disabled rule are still validated.
		if setting.Severity == SeverityOff && len(options) == 0 {
			configuration.settings = append(configuration.settings, setting)
			return
		}
		values, err := ValidateOptions(specs, options)
		if err != nil {
			problems = append(problems, Problem{Code: ProblemInvalidOptions, Rule: setting.Rule, Message: setting.Rule + ": " + err.Error()})
		}
		setting.Options = values
		configuration.settings = append(configuration.settings, setting)
	}
	for _, rule := range r.builtins {
		settle(RuleSetting{Rule: rule.Name, Builtin: true, DefaultSeverity: rule.DefaultSeverity, Severity: rule.DefaultSeverity}, nil, rule.Fixed)
	}
	for _, manifest := range r.packs {
		for _, rule := range manifest.Rules {
			id := manifest.Namespace + "/" + rule.Name
			settle(RuleSetting{Rule: id, Pack: manifest.Namespace, DefaultSeverity: rule.DefaultSeverity, Severity: rule.DefaultSeverity}, rule.Options, false)
		}
	}
	if len(problems) > 0 {
		return nil, problems
	}
	slices.SortFunc(configuration.settings, func(a, b RuleSetting) int { return strings.Compare(a.Rule, b.Rule) })
	return configuration, nil
}

func (r *Registry) admitRuleKey(key string) (Problem, bool) {
	namespace, name, qualified := strings.Cut(key, "/")
	if !qualified {
		if slices.ContainsFunc(r.builtins, func(rule BuiltinRule) bool { return rule.Name == key }) {
			return Problem{}, true
		}
		return Problem{Code: ProblemUnknownRule, Rule: key, Message: "unknown built-in lint rule " + key}, false
	}
	manifest, ok := r.Pack(namespace)
	if !ok {
		return Problem{Code: ProblemUnknownNamespace, Rule: key, Message: "lint rule namespace " + namespace + " is not a selected rule pack"}, false
	}
	if _, ok := manifest.Rule(name); !ok {
		return Problem{Code: ProblemUnknownRule, Rule: key, Message: "rule pack " + namespace + " has no rule " + name}, false
	}
	return Problem{}, true
}

// Settings lists every effective rule setting ordered by rule identity.
func (c *Configuration) Settings() []RuleSetting { return slices.Clone(c.settings) }

// Setting returns the effective setting for a rule identity.
func (c *Configuration) Setting(rule string) (RuleSetting, bool) {
	index := slices.IndexFunc(c.settings, func(setting RuleSetting) bool { return setting.Rule == rule })
	if index < 0 {
		return RuleSetting{}, false
	}
	return c.settings[index], true
}

// Identity digests the effective settings: severities and options after
// defaults. It is independent of pack identity, so the same configuration
// text over a changed pack keeps its configuration identity.
func (c *Configuration) Identity() string {
	type entry struct {
		Rule     string          `json:"rule"`
		Severity Severity        `json:"severity"`
		Options  json.RawMessage `json:"options"`
	}
	entries := make([]entry, 0, len(c.settings))
	for _, setting := range c.settings {
		entries = append(entries, entry{setting.Rule, setting.Severity, setting.Options.canonical()})
	}
	return digest(map[string]any{"configVersion": ConfigVersion, "rules": entries})
}

// RuleInfo describes one registered rule for inspection. Producing it reads
// metadata only; no pack code runs.
type RuleInfo struct {
	Rule            string       `json:"rule"`
	Builtin         bool         `json:"builtin,omitempty"`
	Code            string       `json:"code,omitempty"`
	Pack            string       `json:"pack,omitempty"`
	PackVersion     string       `json:"packVersion,omitempty"`
	PackIdentity    string       `json:"packIdentity,omitempty"`
	Version         string       `json:"version,omitempty"`
	Description     string       `json:"description"`
	DefaultSeverity Severity     `json:"defaultSeverity"`
	Severity        Severity     `json:"severity"`
	Fixed           bool         `json:"fixed,omitempty"`
	Requires        []Family     `json:"requires,omitempty"`
	Targets         []string     `json:"targets,omitempty"`
	FactVersions    []int        `json:"factVersions,omitempty"`
	Options         []OptionSpec `json:"options,omitempty"`
}

// Inspect lists every rule with its effective severity.
func (c *Configuration) Inspect() []RuleInfo {
	var rules []RuleInfo
	for _, rule := range c.registry.builtins {
		setting, _ := c.Setting(rule.Name)
		rules = append(rules, RuleInfo{Rule: rule.Name, Builtin: true, Code: rule.Code, Description: rule.Description, DefaultSeverity: rule.DefaultSeverity, Severity: setting.Severity, Fixed: rule.Fixed, Requires: slices.Clone(rule.Requires)})
	}
	for _, manifest := range c.registry.packs {
		identity := manifest.Identity()
		for _, rule := range manifest.Rules {
			id := manifest.Namespace + "/" + rule.Name
			setting, _ := c.Setting(id)
			rules = append(rules, RuleInfo{Rule: id, Pack: manifest.Namespace, PackVersion: manifest.Version, PackIdentity: identity, Version: rule.Version, Description: rule.Description, DefaultSeverity: rule.DefaultSeverity, Severity: setting.Severity, Requires: slices.Clone(rule.Requires), Targets: slices.Clone(rule.Targets), FactVersions: slices.Clone(manifest.FactSchema.Versions), Options: slices.Clone(rule.Options)})
		}
	}
	slices.SortFunc(rules, func(a, b RuleInfo) int { return strings.Compare(a.Rule, b.Rule) })
	return rules
}

// AnalysisIdentity qualifies one lint analysis. It combines the semantic
// snapshot and producer with the fact schema, every selected pack identity
// and the configuration identity. ReuseScope is the semantic snapshot's
// scope, or "none" when the snapshot is unqualified.
type AnalysisIdentity struct {
	Digest        string   `json:"digest"`
	ReuseScope    string   `json:"reuseScope"`
	Semantic      Semantic `json:"semantic"`
	FactSchema    Schema   `json:"factSchema"`
	Packs         []string `json:"packs"`
	Configuration string   `json:"configuration"`
}

// Analysis computes the analysis identity of evaluating this configuration
// over snapshot.
func (c *Configuration) Analysis(snapshot *Snapshot) AnalysisIdentity {
	identity := AnalysisIdentity{Semantic: snapshot.Semantic, FactSchema: snapshot.Schema, Packs: []string{}, Configuration: c.Identity()}
	for _, manifest := range c.registry.packs {
		identity.Packs = append(identity.Packs, manifest.Namespace+"@"+manifest.Identity())
	}
	identity.ReuseScope = snapshot.Semantic.ReuseScope
	if snapshot.Semantic.Producer == "" || identity.ReuseScope == "" {
		identity.ReuseScope = "none"
	}
	identity.Digest = digest(identity)
	return identity
}

func digest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Rule statuses. Only completed establishes that a rule found nothing.
const (
	StatusCompleted = "completed"
	StatusSkipped   = "skipped"
	StatusFailed    = "failed"
	StatusOff       = "off"
)

// RuleStatus is the execution status of one selected rule.
type RuleStatus struct {
	Rule     string `json:"rule"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Findings int    `json:"findings"`
}

// ReportedFinding is a finding projected with its rule identity and
// configured severity.
type ReportedFinding struct {
	Rule     string    `json:"rule"`
	Severity Severity  `json:"severity"`
	Message  string    `json:"message"`
	Span     Span      `json:"span"`
	Related  []Related `json:"related,omitempty"`
}

// Report is the in-process evaluation result of one pack. Complete is false
// when any enabled rule was skipped or failed.
type Report struct {
	Analysis AnalysisIdentity  `json:"analysis"`
	Complete bool              `json:"complete"`
	Rules    []RuleStatus      `json:"rules"`
	Findings []ReportedFinding `json:"findings"`
}

// Evaluate runs a Go pack in-process over snapshot under configuration,
// which must have been built from a registry containing the pack's
// manifest. It is the stage-one authoring/test harness; a separate process
// runner reuses the same admission and finding validation. Each rule reads
// its own copy decoded from the snapshot's wire form, so a rule sees exactly
// what a pack process would and cannot alter another rule's facts.
func Evaluate(pack *Pack, configuration *Configuration, snapshot *Snapshot) (Report, error) {
	manifest, ok := configuration.registry.Pack(pack.Namespace)
	if !ok {
		return Report{}, fmt.Errorf("rule pack %s is not selected by this configuration", pack.Namespace)
	}
	wire, err := json.Marshal(snapshot)
	if err != nil {
		return Report{}, fmt.Errorf("encode fact snapshot: %w", err)
	}
	report := Report{Analysis: configuration.Analysis(snapshot), Complete: true, Rules: []RuleStatus{}, Findings: []ReportedFinding{}}
	for _, metadata := range manifest.Rules {
		id := pack.Namespace + "/" + metadata.Name
		setting, _ := configuration.Setting(id)
		status := RuleStatus{Rule: id}
		rule := pack.Rule(metadata.Name)
		switch refusal, reason := admit(manifest, metadata, snapshot); {
		case setting.Severity == SeverityOff:
			status.Status = StatusOff
		case rule == nil:
			status.Status, status.Reason = StatusFailed, "pack does not implement its manifest rule"
		case refusal != "":
			status.Status, status.Reason = refusal, reason
		default:
			view := &Snapshot{}
			if err := json.Unmarshal(wire, view); err != nil {
				return Report{}, fmt.Errorf("decode fact snapshot: %w", err)
			}
			findings, err := Apply(rule, view, setting.Options)
			if err != nil {
				status.Status, status.Reason = StatusFailed, err.Error()
				break
			}
			status.Status, status.Findings = StatusCompleted, len(findings)
			for _, finding := range findings {
				report.Findings = append(report.Findings, ReportedFinding{Rule: id, Severity: setting.Severity, Message: finding.Message, Span: finding.Span, Related: finding.Related})
			}
		}
		if status.Status == StatusSkipped || status.Status == StatusFailed {
			report.Complete = false
		}
		report.Rules = append(report.Rules, status)
	}
	slices.SortFunc(report.Rules, func(a, b RuleStatus) int { return strings.Compare(a.Rule, b.Rule) })
	slices.SortStableFunc(report.Findings, compareFindings)
	return report, nil
}

// admit returns the status refusing a rule over snapshot with its reason,
// or "" when the rule may run. An incompatible fact schema is a failure:
// the pack cannot read the request at all. A target the rule does not
// support or an unavailable family is a skip.
func admit(manifest Manifest, rule ManifestRule, snapshot *Snapshot) (string, string) {
	if snapshot.Schema.Name != FactSchemaName || !slices.Contains(manifest.FactSchema.Versions, snapshot.Schema.Version) {
		return StatusFailed, fmt.Sprintf("unsupported fact schema %s/%d", snapshot.Schema.Name, snapshot.Schema.Version)
	}
	if len(rule.Targets) > 0 && !slices.Contains(rule.Targets, snapshot.Semantic.Target) {
		return StatusSkipped, "target-unsupported: " + snapshot.Semantic.Target
	}
	for _, family := range rule.Requires {
		if reason := snapshot.UnavailableReason(family); reason != "" {
			return StatusSkipped, "facts-unavailable: " + string(family) + " (" + reason + ")"
		}
	}
	return "", ""
}

func compareFindings(a, b ReportedFinding) int {
	if a.Span.Offset != b.Span.Offset {
		return a.Span.Offset - b.Span.Offset
	}
	if a.Span.Length != b.Span.Length {
		return a.Span.Length - b.Span.Length
	}
	if order := strings.Compare(a.Rule, b.Rule); order != 0 {
		return order
	}
	return strings.Compare(a.Message, b.Message)
}
