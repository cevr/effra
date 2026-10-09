package lint

import (
	"slices"
	"strings"
)

// SuppressionTarget is what the rule name of a suppression directive
// denotes under a configuration. Deciding it reads metadata only: no pack
// starts to validate a directive.
type SuppressionTarget string

const (
	// TargetBuiltin is a built-in rule that suppression can name.
	TargetBuiltin SuppressionTarget = "builtin"
	// TargetPack is a rule of a selected pack, named namespace/rule.
	TargetPack SuppressionTarget = "pack"
	// TargetUnselected is a well-formed namespace/rule whose namespace no
	// selected pack has and no reservation forbids. Its rule cannot run, so
	// the directive cannot be evaluated; that is not an error.
	TargetUnselected SuppressionTarget = "unselected"
	// TargetUnknown is an unqualified name that is no suppressible built-in
	// rule (including a fixed rule such as invalid-suppression), a rule a
	// selected pack does not have, or a reserved namespace.
	TargetUnknown SuppressionTarget = "unknown"
	// TargetMalformed is a qualified name that is not exactly one
	// namespace and one rule identifier.
	TargetMalformed SuppressionTarget = "malformed"
)

// SuppressionTarget classifies the rule name of a suppression directive.
// Built-in rules are unqualified and pack rules are namespace/rule, so a
// directive can never name a compiler diagnostic or the lint runner.
func (c *Configuration) SuppressionTarget(name string) SuppressionTarget {
	namespace, rule, qualified := strings.Cut(name, "/")
	if !qualified {
		index := slices.IndexFunc(c.registry.builtins, func(builtin BuiltinRule) bool { return builtin.Name == name })
		if index < 0 || c.registry.builtins[index].Fixed {
			return TargetUnknown
		}
		return TargetBuiltin
	}
	if !validIdentifier(namespace) || !validIdentifier(rule) {
		return TargetMalformed
	}
	if slices.Contains(ReservedNamespaces, namespace) {
		return TargetUnknown
	}
	manifest, selected := c.registry.Pack(namespace)
	if !selected {
		return TargetUnselected
	}
	if _, ok := manifest.Rule(rule); !ok {
		return TargetUnknown
	}
	return TargetPack
}
