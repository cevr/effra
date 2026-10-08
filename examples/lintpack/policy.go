// Package lintpack is an example Effra rule pack written against the public
// lint SDK. Its two unrelated project policies read resolved facts only:
// provider and failure identities, provision edges and checked rows. Neither
// policy is a language restriction; configuration names the boundaries.
package lintpack

import (
	"fmt"
	"slices"

	"effra.local/prototype/lint"
)

// Pack is the example pack. Its manifest is derived with Pack.Manifest.
var Pack = &lint.Pack{
	Namespace:    "policy",
	Version:      "0.1.0",
	Description:  "Example project policies over resolved Effra facts.",
	FactVersions: []int{lint.FactSchemaVersion},
	Rules:        []*lint.Rule{ProviderBoundary, ForbiddenFailure},
}

// ProviderBoundary restricts a configured provider to composition
// functions. It matches provision edges by resolved provider identity, so a
// local value spelled like the provider does not match while an alias or a
// layer selecting the provider does.
var ProviderBoundary = &lint.Rule{
	Name:            "provider-boundary",
	Version:         "1",
	Description:     "Only the configured composition functions may provide the configured provider.",
	DefaultSeverity: lint.SeverityError,
	Requires:        []lint.Family{lint.FamilyDeclarations, lint.FamilyCallables, lint.FamilyProvisions},
	Options: []lint.OptionSpec{
		{Name: "provider", Type: lint.OptionString, Required: true, Description: "Provider declaration name."},
		{Name: "allow", Type: lint.OptionStringList, Required: true, Description: "Function names permitted to provide it."},
	},
	Check: func(pass *lint.Pass) error {
		snapshot := pass.Snapshot
		provider := snapshot.ProviderNamed(pass.Options.String("provider"))
		if provider == nil {
			return fmt.Errorf("configured provider %q is not declared", pass.Options.String("provider"))
		}
		allowed, err := functionIdentities(snapshot, pass.Options.StringList("allow"))
		if err != nil {
			return err
		}
		for _, provision := range snapshot.Provisions {
			if provision.Provider != provider.Identity || slices.Contains(allowed, provision.Callable) {
				continue
			}
			finding := lint.Finding{
				Message: fmt.Sprintf("%s provides %s; only %v may provide it", callableName(snapshot, provision.Callable), provider.Name, pass.Options.StringList("allow")),
				Span:    provision.Span,
			}
			if provision.Selection != nil {
				finding.Related = append(finding.Related, lint.Related{Message: "selected by this layer binding", Span: *provision.Selection})
			}
			pass.Report(finding)
		}
		return nil
	},
}

// ForbiddenFailure forbids a configured failure from escaping selected
// functions. It reads the checked body row, which includes failures
// forwarded from helpers and contributed by operators, and excludes
// failures removed by recovery.
var ForbiddenFailure = &lint.Rule{
	Name:            "forbidden-failure",
	Version:         "1",
	Description:     "The configured failure must not escape the selected functions.",
	DefaultSeverity: lint.SeverityError,
	Requires:        []lint.Family{lint.FamilyDeclarations, lint.FamilyCallables},
	Options: []lint.OptionSpec{
		{Name: "failure", Type: lint.OptionString, Required: true, Description: "Failure declaration name."},
		{Name: "functions", Type: lint.OptionStringList, Required: true, Description: "Function names that must not let it escape."},
	},
	Check: func(pass *lint.Pass) error {
		snapshot := pass.Snapshot
		failure := snapshot.FailureNamed(pass.Options.String("failure"))
		if failure == nil {
			return fmt.Errorf("configured failure %q is not declared", pass.Options.String("failure"))
		}
		selected, err := functionIdentities(snapshot, pass.Options.StringList("functions"))
		if err != nil {
			return err
		}
		for _, identity := range selected {
			callable := snapshot.Callable(identity)
			if callable.Body == nil || !slices.Contains(callable.Body.Failures, failure.Identity) {
				continue
			}
			finding := lint.Finding{Message: fmt.Sprintf("failure %s can escape %s", failure.Name, callable.Name), Span: callable.Span}
			for _, contribution := range callable.Contributions {
				if slices.Contains(contribution.Members, failure.Identity) && len(finding.Related) < lint.MaxRelatedLocations {
					finding.Related = append(finding.Related, lint.Related{Message: failure.Name + " enters the body here", Span: contribution.Span})
				}
			}
			pass.Report(finding)
		}
		return nil
	},
}

func functionIdentities(snapshot *lint.Snapshot, names []string) ([]string, error) {
	identities := []string{}
	for _, name := range names {
		function := snapshot.FunctionNamed(name)
		if function == nil {
			return nil, fmt.Errorf("configured function %q is not declared", name)
		}
		identities = append(identities, function.Identity)
	}
	return identities, nil
}

func callableName(snapshot *lint.Snapshot, identity string) string {
	if callable := snapshot.Callable(identity); callable != nil {
		return callable.Name
	}
	return identity
}
