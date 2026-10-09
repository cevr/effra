// Package lintpacks runs the explicitly selected custom lint rule packs for
// the CLI, MCP and LSP surfaces. It loads one lint configuration at
// startup, runs the selected packs over a checked result, and hands their
// reports to the compiler, which merges them with built-in findings. Every
// surface shares this path, so they agree on selection, configuration and
// results.
package lintpacks

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/lint"
)

// Selection names the lint configuration a surface was started with: an
// optional configuration file and manifests selected directly (--rules).
type Selection struct {
	Config    string
	Manifests []string
}

// Session is a loaded, validated lint configuration with its selected
// packs. A nil Session is the default configuration with no packs.
// Loading and inspecting a Session never starts a pack.
type Session struct {
	selection     lint.Selection
	configuration *lint.Configuration
}

// Load reads and validates the selection. Every error is an invocation
// error: an invalid configuration must not start a pack. An empty
// selection loads as nil.
func Load(selection Selection) (*Session, error) {
	if selection.Config == "" && len(selection.Manifests) == 0 {
		return nil, nil
	}
	var loaded lint.Selection
	if selection.Config != "" {
		var err error
		if loaded, err = lint.LoadConfig(selection.Config); err != nil {
			return nil, err
		}
	} else {
		loaded.Config = lint.Config{Version: lint.ConfigVersion}
	}
	for _, path := range selection.Manifests {
		pack, err := lint.LoadManifest(path)
		if err != nil {
			return nil, err
		}
		loaded.Packs = append(loaded.Packs, pack)
	}
	registry, err := compiler.LintRegistry(loaded.Manifests()...)
	if err != nil {
		return nil, err
	}
	configuration, problems := registry.Configure(loaded.Config)
	if len(problems) > 0 {
		return nil, problemError(problems)
	}
	return &Session{selection: loaded, configuration: configuration}, nil
}

type problemError []lint.Problem

func (p problemError) Error() string {
	messages := make([]string, 0, len(p))
	for _, problem := range p {
		messages = append(messages, problem.Code+": "+problem.Message)
	}
	return "invalid lint configuration: " + strings.Join(messages, "; ")
}

// SelectsPacks reports whether the session selects any rule pack; the
// default session selects none.
func (s *Session) SelectsPacks() bool {
	return s != nil && len(s.selection.Packs) > 0
}

// Rules inspects every rule with its effective severity, without running
// pack code.
func (s *Session) Rules() []lint.RuleInfo {
	return s.Configuration().Inspect()
}

// Configuration is the effective configuration; the default one for nil.
func (s *Session) Configuration() *lint.Configuration {
	if s == nil {
		return compiler.DefaultLintConfiguration()
	}
	return s.configuration
}

// Run runs every selected pack over a checked result, in parallel, and
// returns their reports in namespace order regardless of completion order.
// source is the analysed document: its URI names it in the snapshot, and
// its text lets the runner check that every finding position is a real
// character position whose line and column match its offset. Packs never
// run over unchecked source: lint is unavailable there.
func (s *Session) Run(ctx context.Context, result *compiler.Result, source compiler.SourceSnapshot) compiler.LintPacks {
	return s.run(ctx, result, source, nil)
}

// run is Run, recording its cost in traces when traces is non-nil;
// traces.Packs[i] is the run of the pack reported at index i.
func (s *Session) run(ctx context.Context, result *compiler.Result, source compiler.SourceSnapshot, traces *runTraces) compiler.LintPacks {
	packs := compiler.LintPacks{Configuration: s.Configuration()}
	if !s.SelectsPacks() || !result.Checked {
		return packs
	}
	// One capture of the host environment serves every pack of this
	// analysis: each receives exactly its selected variables from it.
	host := lint.CaptureHost()
	began := time.Now()
	snapshot := result.LintFacts(s.families()...)
	snapshot.Source.URI, snapshot.Source.Text = source.URI, source.Text
	selected := slices.Clone(s.selection.Packs)
	slices.SortFunc(selected, func(a, b lint.SelectedPack) int { return strings.Compare(a.Manifest.Namespace, b.Manifest.Namespace) })
	packs.Reports = make([]compiler.LintPackReport, len(selected))
	if traces != nil {
		traces.Facts = time.Since(began)
		traces.Packs = make([]lint.Trace, len(selected))
	}
	began = time.Now()
	var wait sync.WaitGroup
	for i, pack := range selected {
		namespace := pack.Manifest.Namespace
		packs.Reports[i] = compiler.LintPackReport{Pack: namespace, Identity: pack.Manifest.Identity()}
		options := lint.RunOptions{Dir: pack.Dir, Env: host.Select(s.configuration.Environment(namespace))}
		if traces != nil {
			options.Trace = &traces.Packs[i]
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			// Run reads the snapshot without changing it, so the packs
			// share one.
			report, err := lint.Run(ctx, s.configuration, namespace, snapshot, options)
			if err != nil {
				// Run refuses only an unselected namespace or a snapshot
				// it cannot encode: a runner fault, never a clean result.
				report = lint.Report{Failure: &lint.ExecutionFailure{Pack: namespace, Code: lint.FailureSpawn, Message: err.Error()}}
			}
			packs.Reports[i].Report = report
		}()
	}
	wait.Wait()
	if traces != nil {
		traces.Wall = time.Since(began)
	}
	return packs
}

// runTraces is the cost of one Session.run: fact extraction, each pack's
// run, and the wall time of all packs running in parallel.
type runTraces struct {
	Facts time.Duration
	Packs []lint.Trace
	Wall  time.Duration
}

// families is every fact family an enabled pack rule requires.
func (s *Session) families() []lint.Family {
	var families []lint.Family
	for _, setting := range s.configuration.Settings() {
		if setting.Pack == "" || setting.Severity == lint.SeverityOff {
			continue
		}
		pack, _ := s.selection.Pack(setting.Pack)
		rule, _ := pack.Manifest.Rule(strings.TrimPrefix(setting.Rule, setting.Pack+"/"))
		for _, family := range rule.Requires {
			if !slices.Contains(families, family) {
				families = append(families, family)
			}
		}
	}
	return families
}
