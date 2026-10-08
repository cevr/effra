package lint

import (
	"fmt"
	"regexp"
	"slices"
	"unicode/utf8"
)

// Severity is a public diagnostic severity. Off is valid only in
// configuration: it disables a rule rather than describing a finding.
type Severity string

const (
	SeverityOff         Severity = "off"
	SeverityError       Severity = "error"
	SeverityWarning     Severity = "warning"
	SeverityInformation Severity = "information"
	SeverityHint        Severity = "hint"
)

func (s Severity) valid() bool {
	return s == SeverityError || s == SeverityWarning || s == SeverityInformation || s == SeverityHint
}

// Limits on rule output. A rule exceeding them fails rather than having its
// findings silently truncated.
const (
	MaxFindingsPerRule  = 1000
	MaxMessageBytes     = 4096
	MaxRelatedLocations = 16
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

const maxIdentifierBytes = 64

func validIdentifier(name string) bool {
	return len(name) <= maxIdentifierBytes && identifierPattern.MatchString(name)
}

// ReservedNamespaces cannot be claimed by a rule pack: built-in rules use
// unqualified names, and the compiler and runner own these prefixes.
var ReservedNamespaces = []string{"builtin", "compiler", "ef", "effra", "lint", "runner"}

// Rule is one Go rule. Name is local to its pack; the public identity is
// namespace/name. Check reads pass.Snapshot and pass.Options and calls
// pass.Report; it must not retain or mutate the snapshot.
type Rule struct {
	Name            string
	Version         string
	Description     string
	DefaultSeverity Severity
	Requires        []Family
	// Targets limits the rule to compilation targets; empty means all.
	Targets []string
	Options []OptionSpec
	Check   func(*Pass) error
}

// Pack is a namespaced, versioned set of rules distributed together.
type Pack struct {
	Namespace   string
	Version     string
	Description string
	// FactVersions lists the supported fact schema versions.
	FactVersions []int
	Rules        []*Rule
}

// Rule returns the pack rule called name.
func (p *Pack) Rule(name string) *Rule {
	for _, rule := range p.Rules {
		if rule.Name == name {
			return rule
		}
	}
	return nil
}

// Related is a secondary location supporting a finding.
type Related struct {
	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Finding is one rule result. The runner assigns rule identity and
// severity; a rule cannot choose either.
type Finding struct {
	Message string    `json:"message"`
	Span    Span      `json:"span"`
	Related []Related `json:"related,omitempty"`
}

// Pass is one rule execution over one snapshot.
type Pass struct {
	Snapshot *Snapshot
	Options  Options
	findings []Finding
}

// Report records a finding.
func (p *Pass) Report(finding Finding) { p.findings = append(p.findings, finding) }

// Reportf records a finding with a formatted message.
func (p *Pass) Reportf(span Span, format string, args ...any) {
	p.Report(Finding{Message: fmt.Sprintf(format, args...), Span: span})
}

// Apply runs rule over snapshot with already validated options. It is the
// single execution path for built-in and pack rules: it recovers a panic as
// a failure and validates every finding against the snapshot's source.
// Missing families or targets are the caller's admission decision.
func Apply(rule *Rule, snapshot *Snapshot, options Options) (findings []Finding, err error) {
	if rule == nil || rule.Check == nil {
		return nil, fmt.Errorf("rule has no check")
	}
	pass := &Pass{Snapshot: snapshot, Options: options}
	defer func() {
		if recovered := recover(); recovered != nil {
			findings, err = nil, fmt.Errorf("rule panicked: %v", recovered)
		}
	}()
	if err := rule.Check(pass); err != nil {
		return nil, err
	}
	if len(pass.findings) > MaxFindingsPerRule {
		return nil, fmt.Errorf("rule reported %d findings; the limit is %d", len(pass.findings), MaxFindingsPerRule)
	}
	for _, finding := range pass.findings {
		if err := validateFinding(finding, snapshot.Source); err != nil {
			return nil, err
		}
	}
	return slices.Clone(pass.findings), nil
}

func validateFinding(finding Finding, source Source) error {
	if err := validateMessage(finding.Message); err != nil {
		return err
	}
	if err := validateSpan(finding.Span, source); err != nil {
		return fmt.Errorf("finding %q: %w", finding.Message, err)
	}
	if len(finding.Related) > MaxRelatedLocations {
		return fmt.Errorf("finding %q has %d related locations; the limit is %d", finding.Message, len(finding.Related), MaxRelatedLocations)
	}
	for _, related := range finding.Related {
		if err := validateMessage(related.Message); err != nil {
			return err
		}
		if err := validateSpan(related.Span, source); err != nil {
			return fmt.Errorf("related location %q: %w", related.Message, err)
		}
	}
	return nil
}

func validateMessage(message string) error {
	if message == "" || len(message) > MaxMessageBytes || !utf8.ValidString(message) {
		return fmt.Errorf("finding message must be non-empty UTF-8 of at most %d bytes", MaxMessageBytes)
	}
	return nil
}

func validateSpan(span Span, source Source) error {
	if span.Offset < 0 || span.Length < 0 || span.Offset > source.Bytes || span.Length > source.Bytes-span.Offset || span.Line < 1 || span.Column < 1 {
		return fmt.Errorf("span %+v is outside the %d-byte source", span, source.Bytes)
	}
	return nil
}
