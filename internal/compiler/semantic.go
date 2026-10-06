package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

type ValueType struct {
	Success   string          `json:"success"`
	Type      TypeRef         `json:"type"`
	Effect    bool            `json:"effect"`
	Errors    []string        `json:"failures"`
	Services  []string        `json:"requirements"`
	Ownership []OwnershipFact `json:"ownership,omitempty"`
	Captures  []OwnershipFact `json:"captures,omitempty"`
	Child     []OwnershipFact `json:"childOwnership,omitempty"`
}

// ownershipOwnerKind separates the origin of an owner from its rendered
// region. The region string remains part of the inspection contract, while
// this internal kind prevents a deferred recipe result from being confused
// with a value already owned by the caller's lexical scope.
type ownershipOwnerKind uint8

const (
	ownershipOwnerUnknown ownershipOwnerKind = iota
	ownershipOwnerParameter
	ownershipOwnerDeferred
	ownershipOwnerInvocationResult
	ownershipOwnerLexical
	ownershipOwnerChild
	ownershipOwnerTimeout
)

// OwnershipFact is the bounded ownership evidence carried by a checked value.
// A fact is deliberately explicit about uncertainty: the compiler rejects a
// value when it proves that the value belongs to a closing scope or when
// bounded analysis retains potential ownership after exhausting its budget.
// Foreign values and summaries outside this model stay unknown; unknown can
// also represent bounded analysis that did not establish a complete proof.
type OwnershipFact struct {
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Region string `json:"region,omitempty"`
	Origin string `json:"origin,omitempty"`

	// source is the path relative to a parameter which supplied this fact. It
	// is kept out of the inspection schema because it is an implementation
	// detail of function-summary instantiation. Without it, a helper returning
	// pair.outer would have to conservatively retain pair.inner as well.
	source string
	// sourceSet distinguishes a parameter-relative root path (the valid empty
	// path) from a fact that has no parameter source.
	sourceSet bool
	ownerKind ownershipOwnerKind
	// potentialOwner marks bounded evidence which may contain an owned path that
	// the representation could not retain. It may be attached to a wildcard or
	// to a concrete path: the latter records that the selected path itself is
	// incomplete, while a wildcard records an incompleteness outside the paths
	// retained explicitly. It is internal so the public contract still reports
	// bounded evidence while escape checks diagnose exhausted analysis instead
	// of treating it as safe.
	potentialOwner bool
	// remainder marks uncertainty outside retained exact paths. Projection may
	// discharge this marker at an independently complete selected terminal;
	// wildcard alternatives which may include that terminal remain live.
	remainder bool
	// remainderExclusions records exact paths retained in the same abstract
	// alternative. A remainder covers every other path, so a join cannot use a
	// sibling fact from another alternative to discharge it.
	remainderExclusions string
}

// TypeRef is the canonical semantic identity used by checking, emission and
// inspection. Success remains a stable rendered string for existing clients;
// TypeRef prevents the compiler from growing another string-encoded type
// grammar as nominal application data is added. Kind is intentionally open:
// provider, opaque-handle and ownership wrappers can be added without
// changing the public contract shape, with Args carrying nested identities.
type TypeRef struct {
	Kind string    `json:"kind"`
	Name string    `json:"name,omitempty"`
	Args []TypeRef `json:"args,omitempty"`
}

const SemanticSchemaVersion = 3

type Contribution struct {
	Kind  string   `json:"kind"`
	Names []string `json:"names"`
	Span  Span     `json:"span"`
}
type Symbol struct {
	Name          string         `json:"name"`
	Params        []Param        `json:"parameters"`
	Contract      ValueType      `json:"contract"`
	Actual        ValueType      `json:"bodyContract"`
	Span          Span           `json:"span"`
	Contributions []Contribution `json:"contributions"`
}
type Timings struct {
	ImportMicros int64 `json:"importMicros"`
	ParseMicros  int64 `json:"parseMicros"`
	CheckMicros  int64 `json:"checkMicros"`
	TotalMicros  int64 `json:"totalMicros"`
}
type Result struct {
	ModuleSum     []byte        `json:"-"`
	Bindings      []Binding     `json:"bindings,omitempty"`
	SchemaVersion int           `json:"schemaVersion"`
	Revision      string        `json:"revision"`
	Target        string        `json:"target"`
	Checked       bool          `json:"checked"`
	Diagnostics   []Diagnostic  `json:"diagnostics"`
	Symbols       []Symbol      `json:"symbols"`
	Declarations  []Declaration `json:"declarations,omitempty"`
	Timings       Timings       `json:"timings"`
	Program       *Program      `json:"-"`
}
type checker struct {
	program             *Program
	result              *Result
	functions           map[string]*Function
	services            map[string]*Service
	providers           map[string]*Provider
	records             map[string]*Record
	enums               map[string]*Enum
	errors              map[string]*ErrorDecl
	reasons             []Contribution
	region              string
	suppressDiagnostics bool
}

func value(success string) ValueType {
	return ValueType{Success: success, Type: typeRef(success), Errors: []string{}, Services: []string{}, Ownership: ownershipForType(success)}
}

func ownershipForType(success string) []OwnershipFact {
	if success == "File" || strings.HasPrefix(success, "Fiber:") {
		return []OwnershipFact{{Status: "unknown", Origin: "unknown"}}
	}
	return nil
}

func (c *checker) ownershipPaths(typeName, prefix string, seen map[string]bool) []string {
	// A shared record graph can have exponentially many leaf paths. First memoize
	// whether each structural type can contain a managed handle; then enumerate
	// only handle-bearing branches under a total structural visit budget. The
	// wildcard keeps the fact that a handle exists somewhere in the shape when
	// the bounded representation cannot retain every path.
	const maxPaths = 64
	const maxVisits = 256
	const maxContainsVisits = 256
	containsMemo := map[string]bool{}
	containsVisiting := map[string]bool{}
	containsVisits := 0
	var containsHandle func(string) bool
	containsHandle = func(name string) bool {
		if name == "File" || strings.HasPrefix(name, "Fiber:") {
			return true
		}
		if known, ok := containsMemo[name]; ok {
			return known
		}
		if containsVisits >= maxContainsVisits {
			// A budget overflow means the checker cannot prove that this shape is
			// handle-free. Treat it as handle-bearing and let the path walk add a
			// bounded wildcard instead of spending unbounded time on structure.
			containsMemo[name] = true
			return true
		}
		containsVisits++
		if containsVisiting[name] {
			return false
		}
		containsVisiting[name] = true
		found := false
		if record := c.records[name]; record != nil {
			for _, field := range record.Fields {
				if containsHandle(field.Type) {
					found = true
					break
				}
			}
		}
		if !found {
			if enum := c.enums[name]; enum != nil {
				for _, variant := range enum.Variants {
					for _, field := range variant.Fields {
						if containsHandle(field.Type) {
							found = true
							break
						}
					}
					if found {
						break
					}
				}
			}
		}
		delete(containsVisiting, name)
		containsMemo[name] = found
		return found
	}
	if !containsHandle(typeName) {
		return nil
	}
	paths := make([]string, 0, maxPaths)
	truncated := false
	visits := 0
	var visit func(string, string)
	visit = func(name, path string) {
		if len(paths) >= maxPaths || visits >= maxVisits {
			truncated = true
			return
		}
		visits++
		if name == "File" || strings.HasPrefix(name, "Fiber:") {
			paths = append(paths, path)
			return
		}
		if !containsHandle(name) || seen[name] {
			return
		}
		seen[name] = true
		defer delete(seen, name)
		if record := c.records[name]; record != nil {
			for _, field := range record.Fields {
				fieldPath := field.Name
				if path != "" {
					fieldPath = path + "." + fieldPath
				}
				visit(field.Type, fieldPath)
				if truncated {
					return
				}
			}
		}
		if enum := c.enums[name]; enum != nil {
			for _, variant := range enum.Variants {
				for _, field := range variant.Fields {
					fieldPath := variant.Name + "." + field.Name
					if path != "" {
						fieldPath = path + "." + fieldPath
					}
					visit(field.Type, fieldPath)
					if truncated {
						return
					}
				}
			}
		}
	}
	visit(typeName, prefix)
	if truncated {
		paths = append(paths, "*")
	}
	return paths
}

func (c *checker) unknownOwnership(typeName string) []OwnershipFact {
	facts := []OwnershipFact{}
	for _, path := range c.ownershipPaths(typeName, "", map[string]bool{}) {
		fact := OwnershipFact{Path: path, Status: "unknown", Origin: "unknown"}
		if isWildcardPath(path) {
			fact.Origin = "bounded"
			fact.potentialOwner = true
			fact.remainder = true
		}
		facts = append(facts, fact)
	}
	for i := range facts {
		if isWildcardPath(facts[i].Path) {
			facts[i].remainderExclusions = remainderExclusionsForFacts(facts)
		}
	}
	return normalizeFacts(facts)
}

func (c *checker) borrowedOwnership(typeName, region string) []OwnershipFact {
	facts := []OwnershipFact{}
	for _, path := range c.ownershipPaths(typeName, "", map[string]bool{}) {
		fact := OwnershipFact{Path: path, Status: "borrowed", Region: region, Origin: "parameter", source: path, sourceSet: true, ownerKind: ownershipOwnerParameter}
		if isWildcardPath(path) {
			fact.Origin = "bounded"
			fact.potentialOwner = true
			fact.remainder = true
		}
		facts = append(facts, fact)
	}
	for i := range facts {
		if isWildcardPath(facts[i].Path) {
			facts[i].remainderExclusions = remainderExclusionsForFacts(facts)
		}
	}
	return normalizeFacts(facts)
}

func cloneFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	return append([]OwnershipFact{}, facts...)
}

func wildcardPathPrefix(path string) (string, bool) {
	if path == "*" {
		return "", true
	}
	if strings.HasSuffix(path, ".*") {
		return strings.TrimSuffix(path, ".*"), true
	}
	return "", false
}

func isWildcardPath(path string) bool {
	_, wildcard := wildcardPathPrefix(path)
	return wildcard
}

const ownershipRemainderSeparator = "\x00"

func prependOwnershipPath(prefix, path string) string {
	if prefix == "" {
		return path
	}
	if path == "" {
		return prefix
	}
	if path == "*" {
		return prefix + ".*"
	}
	return prefix + "." + path
}

func encodeRemainderExclusions(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	paths = append([]string{}, paths...)
	slices.Sort(paths)
	unique := paths[:0]
	for _, path := range paths {
		if len(unique) == 0 || unique[len(unique)-1] != path {
			unique = append(unique, path)
		}
	}
	return strings.Join(unique, ownershipRemainderSeparator)
}

func decodeRemainderExclusions(encoded string) []string {
	if encoded == "" {
		return nil
	}
	return strings.Split(encoded, ownershipRemainderSeparator)
}

func remainderExcludes(fact OwnershipFact, path string) bool {
	for _, excluded := range decodeRemainderExclusions(fact.remainderExclusions) {
		if excluded == path {
			return true
		}
		base, wildcard := wildcardPathPrefix(excluded)
		if wildcard && (base == "" || pathWithinPrefix(path, base)) {
			return true
		}
	}
	return false
}

func projectRemainderExclusions(encoded, selected string) string {
	paths := decodeRemainderExclusions(encoded)
	if len(paths) == 0 {
		return ""
	}
	rebased := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == selected {
			rebased = append(rebased, "")
			continue
		}
		if strings.HasPrefix(path, selected+".") {
			rebased = append(rebased, strings.TrimPrefix(path, selected+"."))
			continue
		}
		base, wildcard := wildcardPathPrefix(path)
		if !wildcard {
			continue
		}
		if base == "" || base == selected || strings.HasPrefix(selected, base+".") {
			if base == selected {
				rebased = append(rebased, "*")
			} else if base == "" {
				rebased = append(rebased, "*")
			} else {
				rebased = append(rebased, "*")
			}
			continue
		}
		if strings.HasPrefix(base, selected+".") {
			rebased = append(rebased, strings.TrimPrefix(base, selected+".")+".*")
		}
	}
	return encodeRemainderExclusions(rebased)
}

func intersectRemainderExclusions(a, b string) string {
	if a == "" || b == "" {
		return ""
	}
	left := decodeRemainderExclusions(a)
	right := decodeRemainderExclusions(b)
	seen := make(map[string]bool, len(right))
	for _, path := range right {
		seen[path] = true
	}
	common := make([]string, 0, min(len(left), len(right)))
	for _, path := range left {
		if seen[path] {
			common = append(common, path)
		}
	}
	return encodeRemainderExclusions(common)
}

func remainderExclusionsForFacts(facts []OwnershipFact) string {
	paths := make([]string, 0, len(facts))
	for _, fact := range facts {
		if fact.Status != "borrowed" || fact.potentialOwner || fact.remainder {
			continue
		}
		paths = append(paths, fact.Path)
	}
	return encodeRemainderExclusions(paths)
}

// ownershipFactMayOwnPath reports whether one fact can still account for an
// owned value at path. A complete borrowed fact from a parameter is still a
// deferred source relation: the caller may instantiate that parameter with an
// owned value. Only a complete borrowed fact whose owner is already resolved
// outside a parameter can prove that the path is foreign; every other status
// remains possible evidence unless a remainder explicitly excludes that path.
func borrowedFactHasDeferredSource(fact OwnershipFact) bool {
	return fact.Status == "borrowed" && !fact.potentialOwner && !fact.remainder && strings.HasPrefix(fact.Region, "parameter:")
}

func completeBorrowedCoverage(fact OwnershipFact) bool {
	// A wildcard is complete only when its constructor proved that every
	// descendant shares the same borrowed provenance. An arbitrary wildcard is
	// already bounded uncertainty and must remain possible ownership.
	if fact.Status != "borrowed" || fact.potentialOwner || fact.remainder {
		return false
	}
	return !isWildcardPath(fact.Path) || fact.Origin == "bounded-all-borrowed"
}

func ownershipFactMayOwnPath(fact OwnershipFact, path string) bool {
	if isWildcardPath(fact.Path) {
		base, _ := wildcardPathPrefix(fact.Path)
		if base != "" && !pathWithinPrefix(path, base) {
			return false
		}
	} else if fact.Path != path {
		return false
	}
	if fact.remainder && remainderExcludes(fact, path) {
		return false
	}
	if completeBorrowedCoverage(fact) && !borrowedFactHasDeferredSource(fact) {
		return false
	}
	return true
}

func wildcardIntersectionBase(left, right string) (string, bool) {
	leftBase, leftWildcard := wildcardPathPrefix(left)
	rightBase, rightWildcard := wildcardPathPrefix(right)
	if !leftWildcard || !rightWildcard {
		return "", false
	}
	if leftBase == "" {
		return rightBase, true
	}
	if rightBase == "" {
		return leftBase, true
	}
	if pathWithinPrefix(leftBase, rightBase) {
		return leftBase, true
	}
	if pathWithinPrefix(rightBase, leftBase) {
		return rightBase, true
	}
	return "", false
}

func remainderExcludesWildcardIntersection(fact OwnershipFact, target string) bool {
	intersection, ok := wildcardIntersectionBase(fact.Path, target)
	if !ok {
		return false
	}
	for _, exclusion := range decodeRemainderExclusions(fact.remainderExclusions) {
		base, wildcard := wildcardPathPrefix(exclusion)
		if wildcard && (base == "" || pathWithinPrefix(intersection, base)) {
			return true
		}
	}
	return false
}

func ownershipFactMayOwnTarget(fact OwnershipFact, target string) bool {
	if !isWildcardPath(target) {
		return ownershipFactMayOwnPath(fact, target)
	}
	if !isWildcardPath(fact.Path) {
		base, _ := wildcardPathPrefix(target)
		if base != "" && !pathWithinPrefix(fact.Path, base) {
			return false
		}
		return ownershipFactMayOwnPath(fact, fact.Path)
	}
	if _, ok := wildcardIntersectionBase(fact.Path, target); !ok {
		return false
	}
	if fact.remainder && remainderExcludesWildcardIntersection(fact, target) {
		return false
	}
	if completeBorrowedCoverage(fact) && !borrowedFactHasDeferredSource(fact) {
		return false
	}
	return true
}

func omittedOwnershipMayOwnTarget(allFacts, retained []OwnershipFact, target string) bool {
	for _, fact := range allFacts {
		if slices.Contains(retained, fact) {
			continue
		}
		if ownershipFactMayOwnTarget(fact, target) {
			return true
		}
	}
	return false
}

// remainderExclusionsForRetainedFacts only discharges a generated remainder
// at a retained borrowed terminal when every omitted alternative is outside
// that terminal. In particular, a potential wildcard which overlaps the
// terminal keeps the generated remainder live; a disjoint wildcard or a
// remainder which explicitly excludes the terminal does not.
func remainderExclusionsForRetainedFacts(retained, allFacts []OwnershipFact) string {
	paths := make([]string, 0, len(retained))
	for _, fact := range retained {
		if fact.Status != "borrowed" || fact.potentialOwner || fact.remainder {
			continue
		}
		if omittedOwnershipMayOwnTarget(allFacts, retained, fact.Path) {
			continue
		}
		paths = append(paths, fact.Path)
	}
	return encodeRemainderExclusions(paths)
}

func completeWildcardCovers(wildcard OwnershipFact, path string) bool {
	if !isWildcardPath(wildcard.Path) || (wildcard.Origin != "bounded-all-owned" && wildcard.Origin != "bounded-all-borrowed") {
		return false
	}
	base, _ := wildcardPathPrefix(wildcard.Path)
	return base == "" || path == base || strings.HasPrefix(path, base+".")
}

// projectWildcardPath rebases an uncertain subtree through one member
// projection. A descendant prefix is kept until the selected terminal path is
// complete; one retained descendant does not prove the whole subtree.
func projectWildcardPath(fact OwnershipFact, selected string, terminal bool) (rebased string, canDrop bool, ok bool) {
	base, wildcard := wildcardPathPrefix(fact.Path)
	if !wildcard {
		return "", false, false
	}
	if base == "" {
		return "*", fact.remainder && terminal && remainderExcludes(fact, selected), true
	}
	if base == selected {
		return "*", fact.remainder && terminal && remainderExcludes(fact, selected), true
	}
	if strings.HasPrefix(base, selected+".") {
		return strings.TrimPrefix(base, selected+".") + ".*", false, true
	}
	if strings.HasPrefix(selected, base+".") {
		return "*", fact.remainder && terminal && remainderExcludes(fact, selected), true
	}
	return "", false, false
}

func normalizeProjectedWildcard(fact OwnershipFact) OwnershipFact {
	completeOwned := fact.Status == "owned" && fact.Origin == "bounded-all-owned"
	completeBorrowed := fact.Status == "borrowed" && fact.Origin == "bounded-all-borrowed"
	if !completeOwned && !completeBorrowed {
		// A projected wildcard is an incomplete subtree unless its constructor
		// explicitly proved that every descendant had the same provenance. Keep
		// the parameter-relative source while converting it to scoped potential;
		// substitution must not turn an omitted owner into ordinary unknown.
		fact.Status = "unknown"
		fact.Origin = "bounded"
		fact.potentialOwner = true
	}
	return fact
}

func prependFacts(prefix string, facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		copy := fact
		if prefix != "" {
			copy.Path = prependOwnershipPath(prefix, copy.Path)
			paths := decodeRemainderExclusions(copy.remainderExclusions)
			for i, path := range paths {
				paths[i] = prependOwnershipPath(prefix, path)
			}
			copy.remainderExclusions = encodeRemainderExclusions(paths)
		}
		out = append(out, copy)
	}
	return normalizeFacts(out)
}

func projectFacts(facts []OwnershipFact, field string) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	prefix := field + "."
	exact := false
	inexact := false
	descendant := false
	for _, fact := range facts {
		if fact.Path == field {
			if fact.Status == "owned" || fact.Status == "borrowed" {
				exact = true
			} else {
				inexact = true
			}
		} else if strings.HasPrefix(fact.Path, prefix) {
			descendant = true
		}
	}
	terminal := exact && !inexact && !descendant
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if rebased, canDrop, ok := projectWildcardPath(fact, field, terminal); ok {
			if canDrop {
				continue
			}
			copy := fact
			copy.Path = rebased
			copy.remainderExclusions = projectRemainderExclusions(fact.remainderExclusions, field)
			copy = normalizeProjectedWildcard(copy)
			out = append(out, copy)
			continue
		}
		if fact.Path == field {
			copy := fact
			copy.Path = ""
			if !copy.sourceSet {
				copy.source = field
				copy.sourceSet = true
			}
			out = append(out, copy)
			continue
		}
		if strings.HasPrefix(fact.Path, prefix) {
			copy := fact
			copy.Path = strings.TrimPrefix(fact.Path, prefix)
			if !copy.sourceSet {
				copy.source = fact.Path
				copy.sourceSet = true
			}
			out = append(out, copy)
		}
	}
	return normalizeFacts(out)
}

func projectVariantFacts(facts []OwnershipFact, variant, field string) []OwnershipFact {
	prefix := variant + "." + field
	exact := false
	inexact := false
	descendant := false
	for _, fact := range facts {
		if fact.Path == prefix {
			if fact.Status == "owned" || fact.Status == "borrowed" {
				exact = true
			} else {
				inexact = true
			}
		} else if strings.HasPrefix(fact.Path, prefix+".") {
			descendant = true
		}
	}
	terminal := exact && !inexact && !descendant
	projected := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if rebased, canDrop, ok := projectWildcardPath(fact, prefix, terminal); ok {
			if canDrop {
				continue
			}
			copy := fact
			copy.Path = rebased
			copy.remainderExclusions = projectRemainderExclusions(fact.remainderExclusions, prefix)
			copy = normalizeProjectedWildcard(copy)
			projected = append(projected, copy)
			continue
		}
		path := fact.Path
		if path == prefix {
			copy := fact
			copy.Path = ""
			if !copy.sourceSet {
				copy.source = prefix
				copy.sourceSet = true
			}
			projected = append(projected, copy)
		} else if strings.HasPrefix(path, prefix+".") {
			copy := fact
			copy.Path = strings.TrimPrefix(path, prefix+".")
			if !copy.sourceSet {
				copy.source = path
				copy.sourceSet = true
			}
			projected = append(projected, copy)
		}
	}
	return normalizeFacts(projected)
}

func mergeFacts(a, b []OwnershipFact) []OwnershipFact {
	if len(a) == 0 {
		return cloneFacts(b)
	}
	if len(b) == 0 {
		return cloneFacts(a)
	}
	paths := map[string][]OwnershipFact{}
	for _, fact := range append(append([]OwnershipFact{}, a...), b...) {
		paths[fact.Path] = append(paths[fact.Path], fact)
	}
	out := make([]OwnershipFact, 0, len(paths))
	for path, facts := range paths {
		normalized := normalizeFacts(facts)
		if len(normalized) == 1 {
			out = append(out, normalized[0])
			continue
		}
		// Keep every proven owned alternative. Collapsing an owned branch into
		// unknown would make a conditional escape look safe at a scope edge.
		out = append(out, normalized...)
		out = append(out, OwnershipFact{Path: path, Status: "unknown", Origin: "conditional"})
	}
	return normalizeFacts(out)
}

func materializeExecutionFacts(facts []OwnershipFact, region string, ownerKind ownershipOwnerKind) []OwnershipFact {
	out := cloneFacts(facts)
	for i := range out {
		if out[i].ownerKind == ownershipOwnerInvocationResult || out[i].ownerKind == ownershipOwnerDeferred {
			out[i].Region = region
			out[i].ownerKind = ownerKind
		}
	}
	return normalizeFacts(out)
}

func summarizeInvocationFacts(facts []OwnershipFact) []OwnershipFact {
	out := cloneFacts(facts)
	for i := range out {
		if out[i].ownerKind == ownershipOwnerLexical && out[i].Region == "invocation" {
			out[i].ownerKind = ownershipOwnerInvocationResult
		}
	}
	return normalizeFacts(out)
}

func conservativeCycleFacts(observed ...[]OwnershipFact) []OwnershipFact {
	owned := make([]OwnershipFact, 0)
	for _, facts := range observed {
		for _, fact := range facts {
			if fact.Status != "owned" {
				continue
			}
			fact.Path = "*"
			fact.Origin = "bounded"
			fact.potentialOwner = true
			owned = append(owned, fact)
		}
	}
	if len(owned) == 0 {
		return []OwnershipFact{{Path: "*", Status: "unknown", Origin: "bounded", potentialOwner: true}}
	}
	return normalizeFacts(owned)
}

func ownershipPathMatches(argumentPath, sourcePath string) bool {
	// A bounded wildcard means that a handle exists somewhere below the
	// argument. It must remain eligible for a helper's parameter-relative
	// summary; dropping it would turn a proven owned path into an unsafe
	// unknown result.
	if argumentPath == sourcePath || argumentPath == "*" || sourcePath == "*" {
		return true
	}
	if sourcePrefix, sourceWildcard := wildcardPathPrefix(sourcePath); sourceWildcard && sourcePrefix != "" {
		if argumentPath == sourcePrefix || strings.HasPrefix(argumentPath, sourcePrefix+".") {
			return true
		}
	}
	if argumentPrefix, argumentWildcard := wildcardPathPrefix(argumentPath); argumentWildcard && argumentPrefix != "" {
		if sourcePath == argumentPrefix || strings.HasPrefix(sourcePath, argumentPrefix+".") {
			return true
		}
	}
	return false
}

func preservesCompleteWildcardArgument(summary, argument OwnershipFact) bool {
	// A parameter-relative wildcard can preserve its complete argument evidence
	// through a helper boundary. The returned path may be nested, so only the
	// source and both complete wildcard proofs determine whether this is safe.
	if !isWildcardPath(summary.Path) || summary.potentialOwner || summary.remainder ||
		summary.remainderExclusions != "" || !summary.sourceSet || summary.source != "*" ||
		argument.Path != "*" || argument.potentialOwner || argument.remainder ||
		argument.remainderExclusions != "" {
		return false
	}
	return (argument.Status == "owned" && argument.Origin == "bounded-all-owned") ||
		(argument.Status == "borrowed" && argument.Origin == "bounded-all-borrowed")
}

func instantiateFacts(facts []OwnershipFact, params []Param, args []ValueType) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		matched := false
		for i, param := range params {
			if fact.Region != "parameter:"+param.Name && fact.Region != "parameter:*" {
				continue
			}
			matched = true
			if i >= len(args) || len(args[i].Ownership) == 0 {
				out = append(out, OwnershipFact{Path: fact.Path, Status: "unknown", Origin: "helper"})
				continue
			}
			hasConcreteSource := slices.ContainsFunc(args[i].Ownership, func(argument OwnershipFact) bool {
				return !isWildcardPath(argument.Path) && argument.Path == fact.source
			})
			for _, argument := range args[i].Ownership {
				if hasConcreteSource && isWildcardPath(argument.Path) && canSkipWildcardForSource(argument, fact.source) {
					continue
				}
				if !ownershipPathMatches(argument.Path, fact.source) {
					continue
				}
				copy := argument
				copy.potentialOwner = copy.potentialOwner || fact.potentialOwner
				copy.remainder = fact.remainder
				copy.remainderExclusions = fact.remainderExclusions
				preserveWildcard := preservesCompleteWildcardArgument(fact, argument)
				if isWildcardPath(fact.source) || isWildcardPath(argument.Path) {
					// A wildcard source or argument is evidence about an
					// unspecified descendant. Preserve complete whole-value
					// wildcard evidence, but do not turn an ambiguous projected
					// field into a certain owned fact.
					if parameterSourceTop(fact) {
						copy.Origin = "conditional"
					} else if !preserveWildcard {
						copy.Status = "unknown"
						copy.Origin = "bounded"
						copy.potentialOwner = true
						if !fact.remainder {
							copy.remainder = false
							copy.remainderExclusions = ""
						}
					}
				}
				// The summary path is relative to the returned value. The
				// argument contributes status/region, not another path prefix;
				// projection and payload construction already recorded the
				// returned shape before this call boundary.
				copy.Path = fact.Path
				copy.Origin = "helper"
				if preserveWildcard {
					copy.Origin = argument.Origin
				}
				out = append(out, copy)
			}
			if !slices.ContainsFunc(args[i].Ownership, func(argument OwnershipFact) bool {
				return ownershipPathMatches(argument.Path, fact.source)
			}) {
				out = append(out, OwnershipFact{Path: fact.Path, Status: "unknown", Origin: "helper"})
			}
		}
		if !matched {
			copy := fact
			// Invocation ownership belongs to the eventual execution, rather
			// than to the scope where a deferred recipe is constructed. Keep
			// this marker until run/fork materializes the recipe.
			out = append(out, copy)
		}
	}
	return normalizeFacts(out)
}

func canSkipWildcardForSource(fact OwnershipFact, source string) bool {
	if !isWildcardPath(fact.Path) {
		return false
	}
	if fact.remainder {
		return remainderExcludes(fact, source)
	}
	return fact.Status == "borrowed" && !fact.potentialOwner && fact.Origin == "bounded-all-borrowed"
}

func mergeSamePathAlternatives(facts []OwnershipFact) (OwnershipFact, bool) {
	if len(facts) < 2 {
		return OwnershipFact{}, false
	}
	status := facts[0].Status
	for _, fact := range facts[1:] {
		if fact.Status != status {
			return OwnershipFact{}, false
		}
	}
	if isWildcardPath(facts[0].Path) {
		if status != "unknown" {
			return OwnershipFact{}, false
		}
		merged := facts[0]
		for _, fact := range facts[1:] {
			merged = mergePotentialWildcard(merged, fact)
		}
		return merged, true
	}
	merged := facts[0]
	allParameterRegions := strings.HasPrefix(merged.Region, "parameter:")
	sourceTop := false
	for _, fact := range facts[1:] {
		if fact.Region != merged.Region {
			if !strings.HasPrefix(fact.Region, "parameter:") {
				allParameterRegions = false
			}
			merged.Region = "*"
		}
		merged.potentialOwner = merged.potentialOwner || fact.potentialOwner
		if merged.ownerKind != fact.ownerKind {
			merged.ownerKind = ownershipOwnerUnknown
		}
		if merged.sourceSet != fact.sourceSet || (merged.sourceSet && merged.source != fact.source) {
			sourceTop = true
			merged.sourceSet = false
			merged.source = ""
		}
	}
	if allParameterRegions && sourceTop {
		if merged.Region == "*" {
			merged.Region = "parameter:*"
		}
		merged.source = "*"
		merged.sourceSet = true
		merged.Origin = "conditional"
	} else if allParameterRegions && merged.Region == "*" {
		merged.Region = "parameter:*"
	}
	if merged.Origin != facts[len(facts)-1].Origin {
		merged.Origin = "conditional"
	}
	merged.remainder = false
	merged.remainderExclusions = ""
	return merged, true
}

func parameterSourceTop(fact OwnershipFact) bool {
	return fact.sourceSet && fact.source == "*" && fact.Origin == "conditional" && strings.HasPrefix(fact.Region, "parameter:")
}

type ownershipProvenanceKey struct {
	region    string
	sourceSet bool
	ownerKind ownershipOwnerKind
}

type ownershipFrontierNode struct {
	exact    []OwnershipFact
	wildcard []OwnershipFact
	children map[string]*ownershipFrontierNode
}

func ownershipProvenance(fact OwnershipFact) ownershipProvenanceKey {
	return ownershipProvenanceKey{
		region:    fact.Region,
		sourceSet: fact.sourceSet,
		ownerKind: fact.ownerKind,
	}
}

func ownershipFrontierMarker(key ownershipProvenanceKey, prefix string) OwnershipFact {
	marker := OwnershipFact{
		Path:      "*",
		Status:    "borrowed",
		Region:    key.region,
		Origin:    "bounded-all-borrowed",
		sourceSet: key.sourceSet,
		ownerKind: key.ownerKind,
	}
	if prefix != "" {
		marker.Path = prefix + ".*"
	}
	if key.sourceSet {
		marker.source = "*"
	}
	return marker
}

func addOwnershipFrontierFact(root *ownershipFrontierNode, fact OwnershipFact) {
	base, wildcard := wildcardPathPrefix(fact.Path)
	if wildcard {
		fact.Path = base
	} else {
		base = fact.Path
	}
	segments := []string{}
	if base != "" {
		segments = strings.Split(base, ".")
	}
	node := root
	for _, segment := range segments {
		if node.children == nil {
			node.children = map[string]*ownershipFrontierNode{}
		}
		child := node.children[segment]
		if child == nil {
			child = &ownershipFrontierNode{}
			node.children[segment] = child
		}
		node = child
	}
	if wildcard {
		node.wildcard = append(node.wildcard, fact)
	} else {
		node.exact = append(node.exact, fact)
	}
}

func ownershipFrontierKeys(node *ownershipFrontierNode) map[ownershipProvenanceKey]bool {
	keys := map[ownershipProvenanceKey]bool{}
	for _, fact := range append(append([]OwnershipFact{}, node.exact...), node.wildcard...) {
		keys[ownershipProvenance(fact)] = true
	}
	for _, child := range node.children {
		for key := range ownershipFrontierKeys(child) {
			keys[key] = true
		}
	}
	return keys
}

func ownershipFrontierSingleKey(node *ownershipFrontierNode) (ownershipProvenanceKey, bool) {
	keys := ownershipFrontierKeys(node)
	if len(keys) != 1 {
		return ownershipProvenanceKey{}, false
	}
	for key := range keys {
		return key, true
	}
	return ownershipProvenanceKey{}, false
}

func ownershipFrontierCanCollapse(node *ownershipFrontierNode) bool {
	if len(node.wildcard) > 0 || len(node.children) <= 1 {
		return true
	}
	for _, child := range node.children {
		if len(child.children) > 0 || len(child.wildcard) > 0 {
			return false
		}
	}
	return true
}

func collectOwnershipFrontier(node *ownershipFrontierNode, prefix string) ([]OwnershipFact, bool) {
	if key, ok := ownershipFrontierSingleKey(node); ok {
		if prefix == "" {
			if ownershipFrontierCanCollapse(node) && (len(node.children) > 0 || len(node.wildcard) > 0) {
				return []OwnershipFact{ownershipFrontierMarker(key, "")}, true
			}
			if len(node.exact) == 1 {
				return cloneFacts(node.exact), true
			}
		} else if ownershipFrontierCanCollapse(node) && (len(node.children) > 0 || len(node.wildcard) > 0) {
			return []OwnershipFact{ownershipFrontierMarker(key, prefix)}, true
		} else if len(node.exact) > 0 {
			return cloneFacts(node.exact), true
		}
	}

	if len(node.wildcard) > 0 {
		// A wildcard already covers every descendant. Keeping it beside a
		// different provenance key would make one source stand in for another.
		return nil, false
	}
	out := cloneFacts(node.exact)
	children := make([]string, 0, len(node.children))
	for name := range node.children {
		children = append(children, name)
	}
	slices.Sort(children)
	for _, name := range children {
		childPrefix := name
		if prefix != "" {
			childPrefix = prefix + "." + name
		}
		child, ok := collectOwnershipFrontier(node.children[name], childPrefix)
		if !ok {
			return nil, false
		}
		out = append(out, child...)
	}
	return out, true
}

// compactBorrowedFacts retains complete terminal facts and replaces a
// homogeneous descendant set with a provenance-preserving prefix frontier.
// A wildcard source means any descendant of the same parameter, rather than
// an arbitrary borrowed value from another parameter.
func compactBorrowedFacts(facts []OwnershipFact, maxFacts int) ([]OwnershipFact, bool) {
	root := &ownershipFrontierNode{}
	for _, fact := range facts {
		if fact.Status != "borrowed" || fact.potentialOwner {
			return nil, false
		}
		if isWildcardPath(fact.Path) && fact.Origin != "bounded-all-borrowed" {
			return nil, false
		}
		addOwnershipFrontierFact(root, fact)
	}
	frontier, ok := collectOwnershipFrontier(root, "")
	if !ok || len(frontier) > maxFacts {
		return nil, false
	}
	return normalizeFacts(frontier), true
}

func compactOwnedFacts(facts []OwnershipFact, maxFacts int) ([]OwnershipFact, bool) {
	if len(facts) <= maxFacts {
		return nil, false
	}
	region := ""
	ownerKind := ownershipOwnerUnknown
	for i, fact := range facts {
		if fact.Status != "owned" || fact.potentialOwner {
			return nil, false
		}
		if i == 0 {
			region = fact.Region
			ownerKind = fact.ownerKind
			continue
		}
		if fact.Region != region {
			region = "*"
		}
		if fact.ownerKind != ownerKind {
			region = "*"
			ownerKind = ownershipOwnerUnknown
		}
	}
	return []OwnershipFact{{Path: "*", Status: "owned", Region: region, Origin: "bounded-all-owned", ownerKind: ownerKind}}, true
}

func wildcardBaseSegments(path string) ([]string, bool) {
	base, wildcard := wildcardPathPrefix(path)
	if !wildcard {
		return nil, false
	}
	if base == "" {
		return nil, true
	}
	return strings.Split(base, "."), true
}

func commonWildcardPrefix(paths [][]string) []string {
	if len(paths) == 0 {
		return nil
	}
	common := append([]string{}, paths[0]...)
	for _, path := range paths[1:] {
		limit := min(len(common), len(path))
		for i := 0; i < limit; i++ {
			if common[i] != path[i] {
				limit = i
				break
			}
		}
		common = common[:limit]
		if len(common) == 0 {
			return nil
		}
	}
	return common
}

func pathWithinPrefix(path, prefix string) bool {
	return path == prefix || strings.HasPrefix(path, prefix+".")
}

func coalesceOwnedWildcards(facts []OwnershipFact) []OwnershipFact {
	groups := map[string][]int{}
	for i, fact := range facts {
		if fact.Status != "owned" || fact.Origin != "bounded-all-owned" || fact.potentialOwner {
			continue
		}
		base, wildcard := wildcardPathPrefix(fact.Path)
		if !wildcard || base == "" {
			continue
		}
		key := fmt.Sprintf("%s\x00%d", fact.Region, fact.ownerKind)
		groups[key] = append(groups[key], i)
	}
	if len(groups) == 0 {
		return facts
	}
	removed := make(map[int]bool)
	added := []OwnershipFact{}
	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		bases := make([][]string, 0, len(indexes))
		for _, index := range indexes {
			base, _ := wildcardBaseSegments(facts[index].Path)
			bases = append(bases, base)
		}
		common := commonWildcardPrefix(bases)
		if len(common) == 0 {
			continue
		}
		prefix := strings.Join(common, ".")
		conflict := false
		for i, fact := range facts {
			if slices.Contains(indexes, i) {
				continue
			}
			if pathWithinPrefix(fact.Path, prefix) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		marker := facts[indexes[0]]
		marker.Path = prefix + ".*"
		added = append(added, marker)
		for _, index := range indexes {
			removed[index] = true
		}
	}
	if len(removed) == 0 {
		return facts
	}
	result := make([]OwnershipFact, 0, len(facts)-len(removed)+len(added))
	for i, fact := range facts {
		if !removed[i] {
			result = append(result, fact)
		}
	}
	result = append(result, added...)
	slices.SortStableFunc(result, func(a, b OwnershipFact) int {
		if a.Path != b.Path {
			return strings.Compare(a.Path, b.Path)
		}
		return strings.Compare(a.Region, b.Region)
	})
	return result
}

func ownershipPathDepth(path string) int {
	if isWildcardPath(path) {
		return 1 << 30
	}
	if path == "" {
		return 0
	}
	return strings.Count(path, ".") + 1
}

func ownershipPathCoveredByIncomplete(path string, incomplete map[string]bool) bool {
	for prefix := range incomplete {
		base, wildcard := wildcardPathPrefix(prefix)
		if wildcard && (base == "" || pathWithinPrefix(path, base)) {
			return true
		}
	}
	return false
}

func boundedBorrowedWithPotential(facts []OwnershipFact, incomplete map[string]bool, maxFacts int) []OwnershipFact {
	ownedCoverage := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if fact.Status == "owned" && fact.Origin == "bounded-all-owned" && !fact.potentialOwner {
			ownedCoverage = append(ownedCoverage, fact)
		}
	}
	// Parameter-relative borrowed facts are deferred source relations, not
	// proof of foreign ownership. Keep complete relations in the bounded
	// representation before ordinary borrowed terminals so substitution can
	// still inspect the caller's ownership evidence.
	deferredCoverage := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if !borrowedFactHasDeferredSource(fact) || !completeBorrowedCoverage(fact) {
			continue
		}
		deferredCoverage = append(deferredCoverage, fact)
	}
	retained := append(append([]OwnershipFact{}, ownedCoverage...), deferredCoverage...)
	if len(retained) >= maxFacts {
		return []OwnershipFact{{Path: "*", Status: "unknown", Origin: "bounded", potentialOwner: true}}
	}
	candidates := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if fact.Status != "borrowed" || fact.potentialOwner || incomplete[fact.Path] || slices.Contains(retained, fact) {
			continue
		}
		candidates = append(candidates, fact)
	}
	slices.SortStableFunc(candidates, func(a, b OwnershipFact) int {
		if depth := ownershipPathDepth(a.Path) - ownershipPathDepth(b.Path); depth != 0 {
			return depth
		}
		return strings.Compare(a.Path, b.Path)
	})
	available := maxFacts - len(retained) - 1
	if available < 0 {
		available = 0
	}
	if len(candidates) > available {
		candidates = candidates[:available]
	}
	bounded := append(append([]OwnershipFact{}, retained...), candidates...)
	bounded = append(bounded, OwnershipFact{
		Path:                "*",
		Status:              "unknown",
		Region:              "*",
		Origin:              "bounded",
		potentialOwner:      true,
		remainder:           true,
		remainderExclusions: remainderExclusionsForRetainedFacts(bounded, facts),
	})
	return normalizeFacts(bounded)
}

func incompleteWildcard(fact OwnershipFact) bool {
	if !isWildcardPath(fact.Path) {
		return false
	}
	return fact.potentialOwner || (fact.Origin != "bounded-all-owned" && fact.Origin != "bounded-all-borrowed")
}

func mergePotentialWildcard(a, b OwnershipFact) OwnershipFact {
	merged := a
	merged.Status = "unknown"
	merged.Origin = "bounded"
	merged.potentialOwner = true
	if merged.remainder && b.remainder {
		merged.remainderExclusions = intersectRemainderExclusions(merged.remainderExclusions, b.remainderExclusions)
	} else {
		merged.remainder = false
		merged.remainderExclusions = ""
	}
	if merged.Region != b.Region {
		merged.Region = "*"
	}
	if merged.ownerKind != b.ownerKind {
		merged.ownerKind = ownershipOwnerUnknown
	}
	if merged.sourceSet != b.sourceSet || (merged.sourceSet && merged.source != b.source) {
		merged.source = ""
		merged.sourceSet = false
	}
	return merged
}

// retainIncompleteWildcards turns every non-complete wildcard into an
// explicit scoped potential marker. The marker covers only its own prefix;
// unrelated sibling facts remain available for the bounded representation.
// This is the widening boundary for path truncation and projected uncertainty.
func retainIncompleteWildcards(facts []OwnershipFact) []OwnershipFact {
	markers := map[string]OwnershipFact{}
	for _, fact := range facts {
		if !incompleteWildcard(fact) {
			continue
		}
		if previous, ok := markers[fact.Path]; ok {
			markers[fact.Path] = mergePotentialWildcard(previous, fact)
			continue
		}
		fact.Status = "unknown"
		fact.Origin = "bounded"
		fact.potentialOwner = true
		markers[fact.Path] = fact
	}
	if len(markers) == 0 {
		return facts
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if incompleteWildcard(fact) {
			continue
		}
		covered := false
		for _, marker := range markers {
			if marker.remainder {
				continue
			}
			base, _ := wildcardPathPrefix(marker.Path)
			if base == "" || pathWithinPrefix(fact.Path, base) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, fact)
		}
	}
	for _, marker := range markers {
		out = append(out, marker)
	}
	slices.SortStableFunc(out, compareOwnershipFacts)
	return out
}

func compareOwnershipFacts(a, b OwnershipFact) int {
	if a.Path != b.Path {
		return strings.Compare(a.Path, b.Path)
	}
	if a.Status != b.Status {
		return strings.Compare(a.Status, b.Status)
	}
	if a.Region != b.Region {
		return strings.Compare(a.Region, b.Region)
	}
	if a.Origin != b.Origin {
		return strings.Compare(a.Origin, b.Origin)
	}
	if a.sourceSet != b.sourceSet {
		if !a.sourceSet {
			return -1
		}
		return 1
	}
	if a.source != b.source {
		return strings.Compare(a.source, b.source)
	}
	if a.ownerKind < b.ownerKind {
		return -1
	}
	if a.ownerKind > b.ownerKind {
		return 1
	}
	if a.potentialOwner != b.potentialOwner {
		if !a.potentialOwner {
			return -1
		}
		return 1
	}
	if a.remainder != b.remainder {
		if !a.remainder {
			return -1
		}
		return 1
	}
	if a.remainderExclusions != b.remainderExclusions {
		return strings.Compare(a.remainderExclusions, b.remainderExclusions)
	}
	return 0
}

func normalizeFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := append([]OwnershipFact{}, facts...)
	slices.SortStableFunc(out, compareOwnershipFacts)
	result := make([]OwnershipFact, 0, len(out))
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && out[j].Path == out[i].Path && out[j].Status == out[i].Status {
			j++
		}
		group := out[i:j]
		if len(group) > 1 {
			if merged, ok := mergeSamePathAlternatives(group); ok {
				result = append(result, merged)
				i = j
				continue
			}
		}
		for _, fact := range group {
			if len(result) == 0 || result[len(result)-1] != fact {
				result = append(result, fact)
			}
		}
		i = j
	}
	result = coalesceOwnedWildcards(result)
	result = retainIncompleteWildcards(result)
	const maxFacts = 64
	if len(result) > maxFacts {
		allOwned := true
		allBorrowed := true
		for _, fact := range result {
			if fact.Status != "owned" {
				allOwned = false
			}
			if fact.Status != "borrowed" {
				allBorrowed = false
			}
		}
		if allOwned {
			if bounded, ok := compactOwnedFacts(result, maxFacts); ok {
				return bounded
			}
		}
		if allBorrowed {
			if bounded, ok := compactBorrowedFacts(result, maxFacts); ok {
				return bounded
			}
		}
		// A path with several alternatives is indivisible evidence. Retaining a
		// borrowed fact for a selected path while dropping its owned sibling
		// would let projection mistake an incomplete path for a complete one.
		// Keep every fact in such a group before spending the remaining budget on
		// independent paths. This keeps the bounded representation useful for
		// safe siblings without turning truncation into a fabricated proof.
		groups := make(map[string][]OwnershipFact)
		incomplete := make(map[string]bool)
		incompleteCount := 0
		for _, fact := range result {
			groups[fact.Path] = append(groups[fact.Path], fact)
		}
		for path, group := range groups {
			if len(group) > 1 || slices.ContainsFunc(group, func(fact OwnershipFact) bool {
				return fact.potentialOwner || (isWildcardPath(fact.Path) && (fact.Origin == "bounded-all-owned" || fact.Origin == "bounded-all-borrowed"))
			}) {
				incomplete[path] = true
				incompleteCount += len(group)
			}
		}
		if incompleteCount >= maxFacts {
			// No bounded representation can retain every alternative for the
			// selected path. Preserve independently complete borrowed terminals
			// when there is room; the top marker still rejects every omitted path.
			return boundedBorrowedWithPotential(result, incomplete, maxFacts)
		}

		bounded := make([]OwnershipFact, 0, maxFacts)
		for _, fact := range result {
			if incomplete[fact.Path] {
				bounded = append(bounded, fact)
			}
		}
		if len(bounded) >= maxFacts {
			return boundedBorrowedWithPotential(result, incomplete, maxFacts)
		}

		// Reserve a marker when a discarded fact could itself be owned or could
		// carry unresolved ownership. A retained concrete borrowed sibling may
		// still discharge the marker during projection; a discarded selected
		// path has only the marker and is rejected conservatively.
		potentialNeeded := false
		candidateCount := 0
		for _, fact := range result {
			if incomplete[fact.Path] || (isWildcardPath(fact.Path) && (fact.Origin == "bounded-all-owned" || fact.Origin == "bounded-all-borrowed")) {
				continue
			}
			if slices.ContainsFunc(result, func(candidate OwnershipFact) bool {
				// A complete bounded subtree already accounts for every fact
				// below its prefix, so retaining another global potential marker
				// would incorrectly poison unrelated sibling projections.
				return candidate != fact && completeWildcardCovers(candidate, fact.Path)
			}) {
				continue
			}
			candidateCount++
			switch {
			case fact.Status == "owned", fact.Status == "unknown", fact.potentialOwner, allBorrowed && fact.Status == "borrowed":
				potentialNeeded = true
			}
		}
		available := maxFacts - len(bounded)
		if candidateCount > available {
			potentialNeeded = true
		}
		if potentialNeeded {
			available--
		}
		if available < 0 {
			return boundedBorrowedWithPotential(result, incomplete, maxFacts)
		}

		// Keep known borrowed paths first so an independently safe sibling stays
		// observable even when owned siblings consume most of the budget. The
		// retained incomplete groups above always win over these priorities.
		for _, status := range []string{"borrowed", "owned", "unknown"} {
			candidates := make([]OwnershipFact, 0, len(result))
			for _, fact := range result {
				if incomplete[fact.Path] || fact.Status != status {
					continue
				}
				candidates = append(candidates, fact)
			}
			slices.SortStableFunc(candidates, func(a, b OwnershipFact) int {
				aDeferred := borrowedFactHasDeferredSource(a)
				bDeferred := borrowedFactHasDeferredSource(b)
				if aDeferred != bDeferred {
					if aDeferred {
						return -1
					}
					return 1
				}
				aCovered := ownershipPathCoveredByIncomplete(a.Path, incomplete)
				bCovered := ownershipPathCoveredByIncomplete(b.Path, incomplete)
				if aCovered != bCovered {
					if !aCovered {
						return -1
					}
					return 1
				}
				if depth := ownershipPathDepth(a.Path) - ownershipPathDepth(b.Path); depth != 0 {
					return depth
				}
				return strings.Compare(a.Path, b.Path)
			})
			for _, fact := range candidates {
				if available == 0 {
					break
				}
				bounded = append(bounded, fact)
				available--
			}
		}
		if potentialNeeded {
			bounded = append(bounded, OwnershipFact{
				Path:                "*",
				Status:              "unknown",
				Region:              "*",
				Origin:              "bounded",
				potentialOwner:      true,
				remainder:           true,
				remainderExclusions: remainderExclusionsForRetainedFacts(bounded, result),
			})
		}
		return normalizeFacts(bounded)
	}
	return result
}

func hasOwnedFact(facts []OwnershipFact, region string) bool {
	for _, fact := range facts {
		if fact.Status == "owned" && (fact.Region == "*" || fact.ownerKind == ownershipOwnerUnknown || (fact.ownerKind == ownershipOwnerLexical && fact.Region == region)) {
			return true
		}
	}
	return false
}

func hasOwnedClosed(facts []OwnershipFact) bool {
	for _, fact := range facts {
		if fact.Status == "owned" && (fact.ownerKind == ownershipOwnerChild || fact.ownerKind == ownershipOwnerTimeout || strings.HasPrefix(fact.Region, "child:") || strings.HasPrefix(fact.Region, "timeout:")) {
			return true
		}
	}
	return false
}

func hasPotentialOwner(facts []OwnershipFact) bool {
	for _, fact := range facts {
		if fact.potentialOwner {
			return true
		}
	}
	return false
}

func (c *checker) rejectOwnedEscape(facts []OwnershipFact, span Span) {
	if hasPotentialOwner(facts) {
		c.diagnostic("EF123", "ownership analysis budget exhausted before proving value safe", span)
	} else if hasOwnedFact(facts, c.region) || hasOwnedClosed(facts) {
		c.diagnostic("EF123", "value owned by closing scope cannot escape", span)
	}
}

func (c *checker) withRegion(region string, fn func() ValueType) ValueType {
	previous := c.region
	c.region = region
	defer func() { c.region = previous }()
	return fn()
}
func typeRef(name string) TypeRef {
	switch {
	case name == "":
		return TypeRef{}
	case name == "string", name == "bool", name == "i64", name == "bytes", name == "()":
		return TypeRef{Kind: "primitive", Name: name}
	case name == "File", name == "Handler", name == "Latch":
		return TypeRef{Kind: "opaque", Name: name}
	case strings.HasPrefix(name, "Fiber:"):
		return TypeRef{Kind: "fiber", Args: []TypeRef{typeRef(strings.TrimPrefix(name, "Fiber:"))}}
	case strings.HasPrefix(name, "GoResult:"):
		return TypeRef{Kind: "goResult", Args: []TypeRef{typeRef(strings.TrimPrefix(name, "GoResult:"))}}
	case strings.HasPrefix(name, "provider:"):
		return TypeRef{Kind: "provider", Name: strings.TrimPrefix(name, "provider:")}
	case name == "never":
		return TypeRef{Kind: "never"}
	case name == "invalid":
		return TypeRef{Kind: "invalid"}
	default:
		return TypeRef{Kind: "named", Name: name}
	}
}
func contract(f *Function) ValueType {
	v := value(f.Return)
	v.Effect = f.Effect
	v.Errors = normalized(f.Errors)
	v.Services = normalized(f.Services)
	return v
}
func providerContract(p *Provider) ValueType {
	return ValueType{
		Success:  "provider:" + p.Service,
		Type:     typeRef("provider:" + p.Service),
		Effect:   true,
		Errors:   []string{},
		Services: normalized(p.Services),
	}
}
func normalized(names []string) []string {
	out := append([]string{}, names...)
	slices.Sort(out)
	return slices.Compact(out)
}
func union(a, b []string) []string { return normalized(append(append([]string{}, a...), b...)) }
func remove(a []string, n string) []string {
	out := []string{}
	for _, v := range a {
		if v != n {
			out = append(out, v)
		}
	}
	return out
}
func difference(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}
func Compile(source string) *Result { return CompileFor(source, "go") }
func CompileFor(source, target string) *Result {
	return CompileAt(source, target, ".")
}
func CompileAt(source, target, dir string) *Result {
	start := time.Now()
	hash := sha256.Sum256([]byte(source))
	r := &Result{SchemaVersion: SemanticSchemaVersion, Revision: hex.EncodeToString(hash[:]), Target: target, Diagnostics: []Diagnostic{}, Symbols: []Symbol{}}
	if target != "go" && target != "js" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF110", Message: "unsupported target " + target})
		return r
	}
	program, diagnostics := parse(source)
	r.Timings.ParseMicros = time.Since(start).Microseconds()
	if len(diagnostics) > 0 {
		r.Diagnostics = diagnostics
		r.Timings.TotalMicros = time.Since(start).Microseconds()
		return r
	}
	r.Program = program
	r.loadImports(dir)
	c := &checker{program: program, result: r, functions: map[string]*Function{}, services: map[string]*Service{}, providers: map[string]*Provider{}, records: map[string]*Record{}, enums: map[string]*Enum{}, errors: map[string]*ErrorDecl{}, region: "invocation"}
	checkStart := time.Now()
	c.check()
	r.Timings.CheckMicros = time.Since(checkStart).Microseconds()
	r.Timings.TotalMicros = time.Since(start).Microseconds()
	r.Checked = len(r.Diagnostics) == 0
	return r
}
func (c *checker) diagnostic(code, message string, span Span) {
	if c.suppressDiagnostics {
		return
	}
	c.result.Diagnostics = append(c.result.Diagnostics, Diagnostic{code, message, span})
}
func (c *checker) check() {
	names := map[string]bool{}
	for _, s := range builtins() {
		c.services[s.Name] = s
		names[s.Name] = true
	}
	for _, p := range builtinProviders() {
		c.providers[p.Name] = p
		names[p.Name] = true
	}
	for _, name := range builtinErrors() {
		names[name] = true
		c.errors[name] = &ErrorDecl{Name: name}
	}
	claim := func(name string, span Span) {
		if names[name] {
			c.diagnostic("EF101", "duplicate declaration "+name, span)
		}
		names[name] = true
	}
	claimData := func(name string, span Span) {
		switch name {
		case "string", "bool", "i64", "bytes", "File", "Latch", "Handler", "Fiber", "Context", "Effect", "Scope", "Exit", "Cause", "Option", "never", "invalid":
			c.diagnostic("EF101", "reserved data declaration "+name, span)
		}
		claim(name, span)
	}
	for _, imp := range c.program.Imports {
		claim(imp.Alias, imp.Span)
	}
	errors := make([]string, 0, len(c.program.Errors))
	for name := range c.program.Errors {
		errors = append(errors, name)
	}
	slices.Sort(errors)
	for _, name := range errors {
		claimData(name, c.program.Errors[name])
	}
	for _, name := range builtinErrors() {
		c.program.Errors[name] = Span{}
	}
	for _, decl := range c.program.ErrorDecls {
		if c.errors[decl.Name] == nil {
			c.errors[decl.Name] = decl
		}
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "error", Name: decl.Name, Fields: decl.Fields, Span: decl.Span})
	}
	for _, record := range c.program.Records {
		claimData(record.Name, record.Span)
		c.records[record.Name] = record
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "record", Name: record.Name, Fields: record.Fields, Span: record.Span})
	}
	for _, enum := range c.program.Enums {
		claimData(enum.Name, enum.Span)
		c.enums[enum.Name] = enum
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "enum", Name: enum.Name, Variants: enum.Variants, Span: enum.Span})
	}
	for _, decl := range c.program.ErrorDecls {
		for i := range decl.Fields {
			decl.Fields[i].TypeRef = c.typeRef(decl.Fields[i].Type)
		}
	}
	for _, record := range c.program.Records {
		for i := range record.Fields {
			record.Fields[i].TypeRef = c.typeRef(record.Fields[i].Type)
		}
	}
	for _, enum := range c.program.Enums {
		for i := range enum.Variants {
			for j := range enum.Variants[i].Fields {
				enum.Variants[i].Fields[j].TypeRef = c.typeRef(enum.Variants[i].Fields[j].Type)
			}
		}
	}
	for _, decl := range c.program.ErrorDecls {
		c.validateFields(decl.Fields, decl.Name+"Error", true)
	}
	for _, record := range c.program.Records {
		c.validateFields(record.Fields, record.Name, false)
	}
	for _, enum := range c.program.Enums {
		variants := map[string]bool{}
		for _, variant := range enum.Variants {
			if variants[variant.Name] {
				c.diagnostic("EF101", "duplicate variant "+variant.Name+" in "+enum.Name, variant.Span)
			}
			variants[variant.Name] = true
			c.validateFields(variant.Fields, enum.Name+"."+variant.Name, true)
		}
	}
	c.validateDataLayouts()
	slices.SortStableFunc(c.result.Declarations, func(a, b Declaration) int {
		return a.Span.Offset - b.Span.Offset
	})
	for _, s := range c.program.Services {
		claim(s.Name, s.Span)
		c.services[s.Name] = s
	}
	for _, p := range c.program.Providers {
		claim(p.Name, p.Span)
		c.providers[p.Name] = p
	}
	for _, f := range c.program.Functions {
		claim(f.Name, f.Span)
		c.functions[f.Name] = f
	}
	for _, s := range c.program.Services {
		methods := map[string]bool{}
		for _, f := range s.Methods {
			if methods[f.Name] {
				c.diagnostic("EF101", "duplicate method "+f.Name, f.Span)
			}
			methods[f.Name] = true
			c.signature(f)
			if len(f.Services) > 0 {
				c.diagnostic("EF103", "service methods cannot declare uses in this prototype", f.Span)
			}
		}
	}
	for _, f := range c.program.Functions {
		c.signature(f)
	}
	for _, p := range c.program.Providers {
		s, exists := c.services[p.Service]
		if !exists {
			c.diagnostic("EF102", "unknown service "+p.Service, p.Span)
			continue
		}
		c.providerSignature(p)
		methods := map[string]*Function{}
		for _, f := range p.Methods {
			if methods[f.Name] != nil {
				c.diagnostic("EF101", "duplicate implementation method "+f.Name, f.Span)
			}
			methods[f.Name] = f
			c.signature(f)
			for _, required := range normalized(f.Services) {
				if !slices.Contains(normalized(p.Services), required) {
					c.diagnostic("EF103", "provider method "+p.Name+"."+f.Name+" captures undeclared service "+required, f.Span)
				}
			}
			c.providerFunction(p, f)
		}
		for _, want := range s.Methods {
			got := methods[want.Name]
			if got == nil {
				c.diagnostic("EF104", "provider "+p.Name+" is missing method "+want.Name, p.Span)
				continue
			}
			equal := got.Effect && got.Return == want.Return && len(got.Params) == len(want.Params)
			if equal {
				for i := range got.Params {
					equal = equal && got.Params[i].Type == want.Params[i].Type
				}
			}
			if !equal || len(difference(got.Errors, want.Errors)) > 0 {
				c.diagnostic("EF104", "implementation does not satisfy service method "+s.Name+"."+want.Name, got.Span)
			}
		}
		for _, f := range p.Methods {
			found := false
			for _, want := range s.Methods {
				found = found || want.Name == f.Name
			}
			if !found {
				c.diagnostic("EF104", "unexpected implementation method "+f.Name, f.Span)
			}
		}
	}
	if len(c.program.Functions) > 0 {
		c.prepareFunctionSummaries()
	}
	for _, f := range c.program.Functions {
		c.function(f, true)
	}
	c.validateJSDeclarationNames()
}

// prepareFunctionSummaries computes bounded ownership summaries before the
// diagnostic-producing pass. Calls may refer to a helper declared later in a
// module. A dependency-ordered pass computes acyclic helpers once; only the
// residual cyclic portion uses a fixed point. This keeps unrelated functions
// independent instead of rescanning every function for every declaration.
func (c *checker) prepareFunctionSummaries() {
	previous := c.suppressDiagnostics
	c.suppressDiagnostics = true

	functions := c.program.Functions
	known := make(map[string]*Function, len(functions))
	for _, f := range functions {
		known[f.Name] = f
	}
	dependents := make(map[*Function][]*Function, len(functions))
	remaining := make(map[*Function]int, len(functions))
	for _, caller := range functions {
		deps := map[*Function]bool{}
		collectFunctionDependencies(caller.Body, known, deps)
		delete(deps, caller)
		remaining[caller] = len(deps)
		for callee := range deps {
			dependents[callee] = append(dependents[callee], caller)
		}
	}

	processed := map[*Function]bool{}
	ready := make([]*Function, 0, len(functions))
	for _, f := range functions {
		if remaining[f] == 0 {
			ready = append(ready, f)
		}
	}
	for head := 0; head < len(ready); head++ {
		f := ready[head]
		c.function(f, false)
		processed[f] = true
		for _, caller := range dependents[f] {
			remaining[caller]--
			if remaining[caller] == 0 {
				ready = append(ready, caller)
			}
		}
	}

	// Recursive groups have no acyclic order. Process the residual graph as a
	// local worklist and revisit a member only when one of its dependencies
	// changes. The summaries are bounded, and the iteration cap keeps a
	// non-monotone summary from monopolizing checking. Facts observed before a
	// cap are retained conservatively so a proven owned result cannot become
	// unknown merely because convergence was inconclusive.
	cycle := make([]*Function, 0)
	cycleSet := map[*Function]bool{}
	for _, f := range functions {
		if !processed[f] {
			cycle = append(cycle, f)
			cycleSet[f] = true
		}
	}
	queue := append([]*Function{}, cycle...)
	queued := map[*Function]bool{}
	for _, f := range queue {
		queued[f] = true
	}
	observedOwnership := map[*Function][][]OwnershipFact{}
	observedCaptures := map[*Function][][]OwnershipFact{}
	maxIterations := len(cycle) * 8
	if maxIterations < 32 {
		maxIterations = 32
	}
	iterations := 0
	for head := 0; head < len(queue) && iterations < maxIterations; head++ {
		f := queue[head]
		queued[f] = false
		beforeOwnership, beforeCaptures := cloneFacts(f.Ownership), cloneFacts(f.Captures)
		c.function(f, false)
		observedOwnership[f] = append(observedOwnership[f], beforeOwnership, cloneFacts(f.Ownership))
		observedCaptures[f] = append(observedCaptures[f], beforeCaptures, cloneFacts(f.Captures))
		iterations++
		changed := !slices.Equal(beforeOwnership, f.Ownership) || !slices.Equal(beforeCaptures, f.Captures)
		if !changed {
			continue
		}
		for _, caller := range dependents[f] {
			if !cycleSet[caller] || queued[caller] {
				continue
			}
			queue = append(queue, caller)
			queued[caller] = true
		}
	}
	if iterations >= maxIterations && len(queue) > iterations {
		for _, f := range cycle {
			f.Ownership = conservativeCycleFacts(observedOwnership[f]...)
			f.Captures = conservativeCycleFacts(observedCaptures[f]...)
		}
	}
	c.suppressDiagnostics = previous
}

func collectFunctionDependencies(block *Block, known map[string]*Function, out map[*Function]bool) {
	if block == nil {
		return
	}
	for _, statement := range block.Statements {
		collectFunctionDependenciesExpr(statement.Value, known, out)
		collectFunctionDependenciesExpr(statement.Payload, known, out)
	}
}

func collectFunctionDependenciesExpr(e *Expr, known map[string]*Function, out map[*Function]bool) {
	if e == nil {
		return
	}
	if e.Kind == "call" && e.Left != nil && e.Left.Kind == "name" {
		if f := known[e.Left.Name]; f != nil {
			out[f] = true
		}
	}
	// Named call arguments are retained in both Args and Fields for source
	// inspection, with the same child pointers in each view. Use the shared
	// traversal seam so summary preparation remains linear in the syntax tree.
	forEachExprChild(e, func(child *Expr) {
		collectFunctionDependenciesExpr(child, known, out)
	})
	for _, arm := range e.Arms {
		collectFunctionDependencies(arm.Body, known, out)
	}
	collectFunctionDependencies(e.Then, known, out)
	collectFunctionDependencies(e.Else, known, out)
}

// providerSignature checks the explicit constructor boundary. Constructor
// parameters are ordinary values; captured services are checked as a distinct
// requirement row and never inferred from an ambient context.
func (c *checker) providerSignature(p *Provider) {
	names := map[string]bool{}
	for i := range p.Params {
		param := &p.Params[i]
		if !c.typeKnown(param.Type) {
			c.diagnostic("EF102", "unknown or unsupported provider configuration type "+param.Type, param.Span)
		}
		param.TypeRef = c.typeRef(param.Type)
		if names[param.Name] {
			c.diagnostic("EF101", "duplicate provider configuration parameter "+param.Name, param.Span)
		}
		names[param.Name] = true
	}
	for _, name := range normalized(p.Services) {
		if _, exists := c.services[name]; !exists {
			c.diagnostic("EF102", "unknown service "+name, p.Span)
		}
	}
}

// providerFunction checks a method with the constructor's captured
// requirements in scope. A method may repeat a captured requirement with
// `uses { ... }` for local clarity, but it can never widen the constructor's
// row. The service operation contract remains the service declaration's
// contract, which has no captured construction row.
func (c *checker) providerFunction(p *Provider, f *Function) {
	c.functionWithLocals(f, false, p.Params, p.Services)
}

func (c *checker) validateJSDeclarationNames() {
	if c.result.Target != "js" {
		return
	}
	reserved := map[string]bool{
		"as": true, "asserts": true, "async": true, "await": true, "break": true,
		"case": true, "catch": true, "class": true, "const": true, "continue": true,
		"debugger": true, "default": true, "delete": true, "do": true, "else": true,
		"enum": true, "export": true, "extends": true, "finally": true, "for": true,
		"false": true, "from": true, "function": true, "get": true, "if": true, "implements": true,
		"import": true, "in": true, "infer": true, "instanceof": true, "interface": true,
		"is": true, "keyof": true, "let": true, "module": true, "namespace": true,
		"new": true, "null": true, "number": true, "object": true, "of": true, "package": true, "private": true, "protected": true,
		"public": true, "readonly": true, "return": true, "satisfies": true, "set": true,
		"static": true, "string": true, "super": true, "switch": true, "symbol": true, "throw": true, "this": true, "true": true, "try": true,
		"type": true, "typeof": true, "undefined": true, "unknown": true, "using": true, "var": true,
		"any": true, "bigint": true, "boolean": true, "never": true, "void": true, "while": true, "with": true, "yield": true,
	}
	validateIdentifier := func(name, owner string, span Span) {
		if reserved[name] {
			c.diagnostic("EF110", "JavaScript declaration name "+name+" is reserved by TypeScript for "+owner, span)
		}
	}
	occupied := map[string]string{}
	claim := func(name, owner string, span Span) {
		if previous, exists := occupied[name]; exists {
			c.diagnostic("EF110", "JavaScript declaration name "+name+" collides between "+previous+" and "+owner, span)
			return
		}
		occupied[name] = owner
	}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			validateIdentifier(declaration.Name+"Error", "error payload declaration", declaration.Span)
		} else {
			validateIdentifier(declaration.Name, "data declaration", declaration.Span)
		}
		claim(declaration.Name, "data declaration", declaration.Span)
	}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			claim(declaration.Name+"Error", "error payload declaration", declaration.Span)
		}
	}
	services := append(append([]*Service{}, builtins()...), c.program.Services...)
	for _, service := range services {
		validateIdentifier(service.Name, "service export", service.Span)
		claim(service.Name+"Requirement", "service requirement declaration", service.Span)
		claim(service.Name+"Provider", "service provider declaration", service.Span)
	}
	for _, provider := range c.program.Providers {
		validateIdentifier(provider.Name, "provider export", provider.Span)
	}
	for _, function := range c.program.Functions {
		validateIdentifier(function.Name, "function export", function.Span)
	}
}
func (c *checker) signature(f *Function) {
	valid := func(t string, span Span) {
		if !c.typeKnown(t) {
			c.diagnostic("EF102", "unknown or unsupported value type "+t, span)
		}
	}
	valid(f.Return, f.Span)
	names := map[string]bool{}
	for i := range f.Params {
		p := &f.Params[i]
		valid(p.Type, p.Span)
		p.TypeRef = c.typeRef(p.Type)
		if names[p.Name] {
			c.diagnostic("EF101", "duplicate parameter "+p.Name, p.Span)
		}
		names[p.Name] = true
	}
	for _, name := range normalized(f.Errors) {
		if _, exists := c.program.Errors[name]; !exists {
			c.diagnostic("EF102", "unknown failure "+name, f.Span)
		}
	}
	for _, name := range normalized(f.Services) {
		if _, exists := c.services[name]; !exists {
			c.diagnostic("EF102", "unknown service "+name, f.Span)
		}
	}
	if !f.Effect && (len(f.Errors) > 0 || len(f.Services) > 0) {
		c.diagnostic("EF103", "ordinary functions cannot declare effect rows", f.Span)
	}
}
func (c *checker) typeKnown(name string) bool {
	switch name {
	case "string", "bool", "()", "i64", "File", "Latch", "bytes", "Handler":
		return true
	}
	if c.records[name] != nil || c.enums[name] != nil {
		return true
	}
	return false
}
func (c *checker) typeRef(name string) TypeRef {
	var canonical func(TypeRef) TypeRef
	canonical = func(ref TypeRef) TypeRef {
		for i := range ref.Args {
			ref.Args[i] = canonical(ref.Args[i])
		}
		if ref.Kind == "named" {
			if c.records[ref.Name] != nil {
				ref.Kind = "record"
			} else if c.enums[ref.Name] != nil {
				ref.Kind = "enum"
			} else if c.errors[ref.Name] != nil {
				ref.Kind = "error"
			}
		}
		return ref
	}
	return canonical(typeRef(name))
}
func (c *checker) validateFields(fields []Field, owner string, reserveTag bool) {
	names := map[string]bool{}
	for _, field := range fields {
		if names[field.Name] {
			c.diagnostic("EF101", "duplicate field "+field.Name+" in "+owner, field.Span)
		}
		names[field.Name] = true
		if field.Name == "_tag" && reserveTag {
			c.diagnostic("EF120", "_tag is reserved for closed variant/error discriminators", field.Span)
		}
		if !c.typeKnown(field.Type) {
			c.diagnostic("EF102", "unknown or unsupported field type "+field.Type, field.Span)
		}
	}
}
func (c *checker) validateDataLayouts() {
	state := map[string]int{}
	reported := map[string]bool{}
	var visit func(string, []string)
	visit = func(name string, path []string) {
		if state[name] == 1 {
			cycle := append(path, name)
			key := strings.Join(cycle, "->")
			if !reported[key] {
				reported[key] = true
				span := Span{}
				if record := c.records[name]; record != nil {
					span = record.Span
				} else if enum := c.enums[name]; enum != nil {
					span = enum.Span
				} else if failure := c.errors[name]; failure != nil {
					span = failure.Span
				}
				c.diagnostic("EF119", "recursive data layout is unsupported: "+key, span)
			}
			return
		}
		if state[name] == 2 {
			return
		}
		state[name] = 1
		fields := []Field{}
		if record := c.records[name]; record != nil {
			fields = record.Fields
		} else if enum := c.enums[name]; enum != nil {
			for _, variant := range enum.Variants {
				fields = append(fields, variant.Fields...)
			}
		} else if failure := c.errors[name]; failure != nil {
			fields = failure.Fields
		}
		for _, field := range fields {
			if c.records[field.Type] != nil || c.enums[field.Type] != nil || c.errors[field.Type] != nil {
				visit(field.Type, append(path, name))
			}
		}
		state[name] = 2
	}
	for _, record := range c.program.Records {
		visit(record.Name, nil)
	}
	for _, enum := range c.program.Enums {
		visit(enum.Name, nil)
	}
	for _, failure := range c.program.ErrorDecls {
		visit(failure.Name, nil)
	}
}
func (c *checker) function(f *Function, record bool) {
	c.functionWithLocals(f, record, nil, f.Services)
}

func (c *checker) functionWithLocals(f *Function, record bool, locals []Param, allowedServices []string) {
	env := map[string]ValueType{}
	for _, p := range locals {
		parameter := value(p.Type)
		parameter.Ownership = c.borrowedOwnership(p.Type, "parameter:"+p.Name)
		env[p.Name] = parameter
	}
	for _, p := range f.Params {
		parameter := value(p.Type)
		parameter.Ownership = c.borrowedOwnership(p.Type, "parameter:"+p.Name)
		env[p.Name] = parameter
	}
	c.reasons = []Contribution{}
	actual := c.withRegion("invocation", func() ValueType { return c.block(f.Body, env, f.Effect) })
	if actual.Success != "never" && (actual.Success != f.Return || actual.Effect) {
		c.diagnostic("EF106", fmt.Sprintf("body returns %s; expected %s", display(actual), f.Return), f.Span)
	}
	if missing := difference(actual.Errors, f.Errors); len(missing) > 0 {
		c.diagnostic("EF107", "undeclared failures: "+strings.Join(missing, ", "), f.Span)
	}
	if missing := difference(actual.Services, allowedServices); len(missing) > 0 {
		c.diagnostic("EF108", "missing service requirements: "+strings.Join(missing, ", "), f.Span)
	}
	actual.Effect = f.Effect
	actual.Type = c.typeRef(actual.Success)
	f.Ownership = summarizeInvocationFacts(actual.Ownership)
	f.Captures = summarizeInvocationFacts(actual.Captures)
	if record {
		declared := contract(f)
		declared.Type = c.typeRef(f.Return)
		c.result.Symbols = append(c.result.Symbols, Symbol{f.Name, f.Params, declared, actual, f.Span, append([]Contribution{}, c.reasons...)})
	}
}
func display(t ValueType) string {
	if t.Effect {
		return "Effect<" + t.Success + ", {" + strings.Join(t.Errors, ", ") + "}, {" + strings.Join(t.Services, ", ") + "}>"
	}
	return t.Success
}
func clone(env map[string]ValueType) map[string]ValueType {
	copy := map[string]ValueType{}
	for n, t := range env {
		copy[n] = t
	}
	return copy
}
func sameType(actual, expected string) bool {
	return actual == expected || actual == "never"
}
func fieldsFor(c *checker, typeName, variantName string) ([]Field, bool) {
	if variantName == "" {
		if record := c.records[typeName]; record != nil {
			return record.Fields, true
		}
		return nil, false
	}
	if enum := c.enums[typeName]; enum != nil {
		for _, variant := range enum.Variants {
			if variant.Name == variantName {
				return variant.Fields, true
			}
		}
	}
	return nil, false
}
func fieldsMap(fields []Field) map[string]Field {
	result := make(map[string]Field, len(fields))
	for _, field := range fields {
		result[field.Name] = field
	}
	return result
}
func sortedBindingNames(bindings map[string]string) []string {
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
func (c *checker) payload(e *Expr, fields []Field, env map[string]ValueType, span Span) []OwnershipFact {
	declared := fieldsMap(fields)
	seen := map[string]bool{}
	ownership := []OwnershipFact{}
	for _, field := range e.Fields {
		want, exists := declared[field.Name]
		if !exists {
			c.diagnostic("EF114", "unknown payload field "+field.Name, field.Span)
			continue
		}
		if seen[field.Name] {
			c.diagnostic("EF114", "duplicate payload field "+field.Name, field.Span)
		}
		seen[field.Name] = true
		got := c.expr(field.Value, env, false)
		if got.Effect || !sameType(got.Success, want.Type) {
			c.diagnostic("EF115", "payload field "+field.Name+" must be "+want.Type, field.Span)
		}
		ownership = append(ownership, prependFacts(field.Name, got.Ownership)...)
	}
	for _, field := range fields {
		if !seen[field.Name] {
			c.diagnostic("EF114", "missing payload field "+field.Name, span)
		}
	}
	return normalizeFacts(ownership)
}
func (c *checker) block(b *Block, env map[string]ValueType, effect bool) ValueType {
	out := value("()")
	env = clone(env)
	terminated := false
	for _, s := range b.Statements {
		if terminated {
			c.diagnostic("EF109", "unreachable statement after fail", s.Span)
		}
		if s.Kind == "fail" {
			if !effect {
				c.diagnostic("EF105", "fail is only valid inside effect functions", s.Span)
			}
			if _, exists := c.program.Errors[s.Name]; !exists {
				c.diagnostic("EF102", "unknown failure "+s.Name, s.Span)
			}
			if s.Payload != nil {
				if s.Payload.Kind == "payload" {
					fields := []Field(nil)
					if decl := c.errors[s.Name]; decl != nil {
						fields = decl.Fields
					}
					c.rejectOwnedEscape(c.payload(s.Payload, fields, env, s.Payload.Span), s.Payload.Span)
				} else {
					payload := c.expr(s.Payload, env, false)
					c.rejectOwnedEscape(payload.Ownership, s.Payload.Span)
					if payload.Effect {
						c.diagnostic("EF105", "failure payload must be pure", s.Payload.Span)
					}
					if decl := c.errors[s.Name]; decl != nil {
						if len(decl.Fields) != 1 || !sameType(payload.Success, decl.Fields[0].Type) {
							c.diagnostic("EF115", "failure payload for "+s.Name+" must match its declared fields", s.Payload.Span)
						} else {
							s.Payload = &Expr{Kind: "payload", Fields: []FieldValue{{Name: decl.Fields[0].Name, Value: s.Payload, Span: s.Payload.Span}}, Span: s.Payload.Span}
						}
					}
				}
			} else if decl := c.errors[s.Name]; decl != nil {
				// The shorthand `fail Error` and `fail Error()` still need to
				// satisfy every declared payload field.
				c.rejectOwnedEscape(c.payload(&Expr{Kind: "payload", Span: s.Span}, decl.Fields, env, s.Span), s.Span)
			}
			out.Errors = union(out.Errors, []string{s.Name})
			out.Success = "never"
			out.Effect = false
			terminated = true
			c.reasons = append(c.reasons, Contribution{"failure", []string{s.Name}, s.Span})
			continue
		}
		t := c.expr(s.Value, env, effect)
		if s.Kind != "let" && (hasPotentialOwner(t.Ownership) || hasOwnedClosed(t.Ownership)) {
			c.diagnostic("EF123", "value owned by a closing scope cannot escape", s.Span)
		}
		out.Errors = union(out.Errors, tExecutedErrors(s.Value))
		out.Services = union(out.Services, tExecutedServices(s.Value))
		if s.Kind == "let" {
			if _, exists := env[s.Name]; exists {
				c.diagnostic("EF101", "duplicate local "+s.Name, s.Span)
			}
			env[s.Name] = t
			out.Success = "()"
			out.Effect = false
			out.Ownership = nil
			out.Captures = nil
		} else {
			if t.Effect {
				c.diagnostic("EF105", "unused lazy effect; execute with run or bind it with let", s.Span)
			}
			out.Success = t.Success
			out.Effect = t.Effect
			out.Ownership = cloneFacts(t.Ownership)
			out.Captures = cloneFacts(t.Captures)
		}
	}
	return out
}

// Effect values carry deferred rows. Only run (and executed branch bodies) contribute to the enclosing computation.
func tExecutedErrors(e *Expr) []string   { return executed(e, true) }
func tExecutedServices(e *Expr) []string { return executed(e, false) }
func executed(e *Expr, errors bool) []string {
	if e == nil {
		return []string{}
	}
	row := func(t ValueType) []string {
		if errors {
			return t.Errors
		}
		return t.Services
	}
	if e.Kind == "run" || e.Kind == "if" || e.Kind == "match" || e.Kind == "scope" || e.Kind == "fork" {
		return row(e.Type)
	}
	out := union(executed(e.Left, errors), executed(e.Right, errors))
	for _, a := range e.Args {
		out = union(out, executed(a, errors))
	}
	return out
}
func (c *checker) expr(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	t := value("invalid")
	switch e.Kind {
	case "integer":
		t = value("i64")
	case "string":
		t = value("string")
	case "bool":
		t = value("bool")
	case "unit":
		t = value("()")
	case "name":
		if v, exists := env[e.Name]; exists {
			t = v
			e.Text = "local"
		} else if p, exists := c.providers[e.Name]; exists {
			if len(p.Params) > 0 || len(p.Services) > 0 {
				c.diagnostic("EF104", "provider "+p.Name+" requires explicit construction", e.Span)
				t = value("invalid")
				break
			}
			t = value("provider:" + p.Service)
			if p.Service == "Files" || p.Service == "Runtime" || p.Service == "Foreign" || p.Service == "Http" {
				c.requireGo(e.Span, "native provider "+p.Service)
			}
			e.Text = "provider"
		} else if f := c.functions[e.Name]; f != nil && f.Effect && f.Return == "string" && len(f.Params) == 1 && f.Params[0].Type == "string" {
			t = contract(f)
			t.Success = "Handler"
			t.Effect = false
			e.Text = "handler"
		} else {
			c.diagnostic("EF102", "unknown value "+e.Name, e.Span)
		}
	case "call":
		if data, ok := c.dataCall(e, env, inEffect); ok {
			t = data
			break
		}
		if c.foreignCall(e, env, inEffect) {
			t = e.Type
			break
		}
		if c.fiberCall(e, env, inEffect) {
			t = e.Type
			break
		}
		if e.Left.Kind == "name" {
			if provider := c.providers[e.Left.Name]; provider != nil {
				if len(provider.Params) == 0 && len(provider.Services) == 0 {
					c.diagnostic("EF105", "provider "+provider.Name+" is a value and cannot be called", e.Span)
					t = value("invalid")
					break
				}
				if len(e.Args) != len(provider.Params) {
					c.diagnostic("EF106", "provider "+provider.Name+" expects "+fmt.Sprint(len(provider.Params))+" configuration arguments", e.Span)
				}
				argumentTypes := make([]ValueType, len(e.Args))
				for i, arg := range e.Args {
					got := c.expr(arg, env, false)
					argumentTypes[i] = got
					if i < len(provider.Params) && (got.Effect || got.Success != provider.Params[i].Type) {
						c.diagnostic("EF106", "provider configuration argument must be "+provider.Params[i].Type, arg.Span)
					}
				}
				t = providerContract(provider)
				for i, param := range provider.Params {
					if i < len(argumentTypes) {
						t.Captures = append(t.Captures, prependFacts("capture:"+param.Name, argumentTypes[i].Ownership)...)
					}
				}
				t.Captures = normalizeFacts(t.Captures)
				e.Text = "provider-constructor"
				break
			}
		}
		var f *Function
		serviceName := ""
		if e.Left.Kind == "name" {
			f = c.functions[e.Left.Name]
			if _, shadow := env[e.Left.Name]; shadow {
				c.diagnostic("EF103", "calling local values is not supported in this prototype", e.Span)
				f = nil
			}
		} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
			key := e.Left.Left.Name
			if key == "Files" || key == "Runtime" || key == "Http" {
				c.requireGo(e.Span, "native service "+key)
			}
			if _, shadow := env[key]; shadow {
				c.diagnostic("EF103", "a local shadows service "+key, e.Span)
			} else if s := c.services[key]; s != nil {
				for _, m := range s.Methods {
					if m.Name == e.Left.Name {
						f = m
						t.Services = []string{key}
						serviceName = key
						break
					}
				}
			}
		}
		if f == nil {
			c.diagnostic("EF102", "unknown function or service method", e.Span)
			for _, a := range e.Args {
				c.expr(a, env, inEffect)
			}
			break
		}
		services := t.Services
		t = contract(f)
		t.Services = union(t.Services, services)
		argumentTypes := make([]ValueType, len(e.Args))
		if len(f.Ownership) > 0 {
			t.Ownership = instantiateFacts(f.Ownership, f.Params, argumentTypes)
		}
		if len(f.Captures) > 0 {
			t.Captures = instantiateFacts(f.Captures, f.Params, argumentTypes)
		}
		if serviceName == "Files" && e.Left.Name == "openRead" {
			// A service name alone is not an acquisition proof: a custom Files
			// provider may borrow a File. The default service operation is
			// therefore explicit unknown until a known LiveFiles provision
			// discharges it below.
			t.Ownership = []OwnershipFact{{Status: "unknown", Origin: "service"}}
		}
		if len(e.Args) != len(f.Params) {
			c.diagnostic("EF106", "incorrect argument count", e.Span)
		}
		for i, a := range e.Args {
			arg := c.expr(a, env, inEffect)
			argumentTypes[i] = arg
			if hasPotentialOwner(arg.Ownership) || hasOwnedClosed(arg.Ownership) {
				c.diagnostic("EF123", "value owned by a closing scope cannot be used", a.Span)
			}
			if e.Left.Kind == "member" && e.Left.Left.Kind == "name" && e.Left.Left.Name == "Http" && i == 1 && arg.Success == "Handler" {
				t.Services = union(t.Services, arg.Services)
			}
			if i < len(f.Params) && (arg.Effect || arg.Success != f.Params[i].Type) {
				c.diagnostic("EF106", "argument must be "+f.Params[i].Type, a.Span)
			}
		}
		if len(f.Ownership) > 0 {
			t.Ownership = instantiateFacts(f.Ownership, f.Params, argumentTypes)
		}
		if len(f.Captures) > 0 {
			t.Captures = instantiateFacts(f.Captures, f.Params, argumentTypes)
		}
		if f.Effect {
			for i, argument := range argumentTypes {
				if i < len(f.Params) {
					t.Captures = append(t.Captures, prependFacts("capture:"+f.Params[i].Name, argument.Ownership)...)
				}
			}
			t.Captures = normalizeFacts(t.Captures)
		}
	case "member":
		inner := c.expr(e.Left, env, inEffect)
		if inner.Effect {
			c.diagnostic("EF106", "field access requires an executed value", e.Span)
			break
		}
		if fields, ok := fieldsFor(c, inner.Success, ""); ok {
			for _, field := range fields {
				if field.Name == e.Name {
					t = value(field.Type)
					t.Ownership = projectFacts(inner.Ownership, e.Name)
					t.Captures = projectFacts(inner.Captures, e.Name)
					if len(t.Ownership) == 0 {
						t.Ownership = c.unknownOwnership(field.Type)
					}
					e.Text = "field"
					break
				}
			}
			if t.Success != "invalid" {
				break
			}
			c.diagnostic("EF114", "unknown field "+e.Name+" on "+inner.Success, e.Span)
			break
		}
		if !strings.HasPrefix(inner.Success, "GoResult:") {
			c.diagnostic("EF106", "field access requires an executed GoResult", e.Span)
			break
		}
		switch e.Name {
		case "value":
			t = value(strings.TrimPrefix(inner.Success, "GoResult:"))
		case "hasError":
			t = value("bool")
		default:
			c.diagnostic("EF102", "GoResult exposes value and hasError", e.Span)
		}
	case "orFail":
		t = c.expr(e.Left, env, inEffect)
		if !t.Effect || !strings.HasPrefix(t.Success, "GoResult:") {
			c.diagnostic("EF106", "orFail requires an Effect returning GoResult", e.Span)
			break
		}
		t.Success = strings.TrimPrefix(t.Success, "GoResult:")
		t.Errors = union(t.Errors, []string{"GoError"})
	case "scope":
		if !inEffect {
			c.diagnostic("EF105", "scope requires an effect function", e.Span)
		}
		scopeRegion := fmt.Sprintf("scope:%d", e.Span.Offset)
		t = c.withRegion(scopeRegion, func() ValueType { return c.block(e.Then, env, inEffect) })
		if hasPotentialOwner(t.Ownership) || hasPotentialOwner(t.Captures) || hasOwnedFact(t.Ownership, scopeRegion) || hasOwnedFact(t.Captures, scopeRegion) {
			c.diagnostic("EF123", "value owned by closing scope cannot escape", e.Span)
		}
	case "fork":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect || !inner.Effect {
			c.diagnostic("EF105", "fork requires an Effect inside an effect function", e.Span)
		}
		childRegion := fmt.Sprintf("child:%d", e.Span.Offset)
		t = value("Fiber:" + inner.Success)
		t.Success = "Fiber:" + inner.Success
		t.Effect = false
		t.Errors = union(inner.Errors, executed(e.Left, true))
		t.Services = union(inner.Services, executed(e.Left, false))
		t.Ownership = []OwnershipFact{{Status: "owned", Region: c.region, Origin: "fork", ownerKind: ownershipOwnerLexical}}
		t.Captures = cloneFacts(inner.Captures)
		// The child executes its recipe under its own owner. This is distinct
		// from the owner of the Fiber handle returned to the parent.
		t.Child = materializeExecutionFacts(inner.Ownership, childRegion, ownershipOwnerChild)
		for i := range t.Child {
			if t.Child[i].Region == childRegion && t.Child[i].Status == "owned" {
				t.Child[i].Origin = "child-acquisition"
			}
		}
		c.reasons = append(c.reasons, Contribution{"owned-child", inner.Errors, e.Span})
	case "timeout":
		duration := c.expr(e.Right, env, inEffect)
		timeoutRegion := fmt.Sprintf("timeout:%d", e.Span.Offset)
		// Recipe construction and eager arguments execute in the caller. Only
		// deferred or symbolic invocation results are materialized under the
		// fresh timeout owner; rebinding an already materialized borrow here
		// would reject a valid caller-owned value.
		inner := c.expr(e.Left, env, inEffect)
		t = inner
		if !t.Effect || duration.Effect || duration.Success != "i64" {
			c.diagnostic("EF106", "timeout requires an Effect and an i64 millisecond duration", e.Span)
		}
		t.Ownership = materializeExecutionFacts(t.Ownership, timeoutRegion, ownershipOwnerTimeout)
		t.Captures = materializeExecutionFacts(t.Captures, timeoutRegion, ownershipOwnerTimeout)
		t.Errors = union(t.Errors, []string{"Timeout"})
		t.Services = union(t.Services, []string{"Scheduler"})
	case "run":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect {
			c.diagnostic("EF105", "run is only valid inside effect functions", e.Span)
		}
		if !inner.Effect {
			c.diagnostic("EF105", "run requires an Effect value", e.Span)
		}
		t = inner
		t.Effect = false
		// Ownership created by a deferred recipe belongs to the owner
		// which actually executes it. Construction may happen outside a
		// scope, or in an outer scope before a nested run.
		t.Ownership = materializeExecutionFacts(inner.Ownership, c.region, ownershipOwnerLexical)
		t.Captures = materializeExecutionFacts(inner.Captures, c.region, ownershipOwnerLexical)
		if !strings.HasPrefix(t.Success, "provider:") {
			t.Captures = nil
		}
		t.Errors = union(t.Errors, executed(e.Left, true))
		t.Services = union(t.Services, executed(e.Left, false))
		if len(t.Errors) > 0 {
			c.reasons = append(c.reasons, Contribution{"failure", t.Errors, e.Span})
		}
		if len(t.Services) > 0 {
			c.reasons = append(c.reasons, Contribution{"requirement", t.Services, e.Span})
		}
	case "provide":
		t = c.expr(e.Left, env, inEffect)
		provider := c.expr(e.Right, env, inEffect)
		if !t.Effect {
			c.diagnostic("EF105", "provide requires an Effect value", e.Span)
		}
		if c.services[e.Name] == nil {
			c.diagnostic("EF102", "unknown service "+e.Name, e.Span)
		}
		if provider.Success != "provider:"+e.Name || provider.Effect {
			c.diagnostic("EF104", "provider must implement "+e.Name, e.Right.Span)
		}
		t.Captures = normalizeFacts(append(t.Captures, provider.Captures...))
		if e.Name == "Files" && e.Right.Kind == "name" && e.Right.Name == "LiveFiles" {
			if e.Left.Kind == "call" && e.Left.Left != nil && e.Left.Left.Kind == "member" && e.Left.Left.Left.Kind == "name" && e.Left.Left.Left.Name == "Files" && e.Left.Left.Name == "openRead" {
				t.Ownership = []OwnershipFact{{Status: "owned", Region: "deferred", Origin: "acquisition", ownerKind: ownershipOwnerDeferred}}
			}
		}
		t.Services = remove(t.Services, e.Name)
	case "catch":
		t = c.expr(e.Left, env, inEffect)
		fallback := c.expr(e.Right, env, false)
		if !t.Effect {
			c.diagnostic("EF105", "catch requires an Effect value", e.Span)
		}
		if _, exists := c.program.Errors[e.Name]; !exists {
			c.diagnostic("EF102", "unknown failure "+e.Name, e.Span)
		} else if !slices.Contains(t.Errors, e.Name) {
			c.diagnostic("EF107", "effect does not admit failure "+e.Name, e.Span)
		}
		if fallback.Effect || fallback.Success != t.Success {
			c.diagnostic("EF106", "prototype catch fallback must be a pure "+t.Success, e.Right.Span)
		}
		// Recovery can publish the fallback value on the handled-failure
		// branch. Preserve both its returned ownership and any provider
		// captures; dropping either branch turns a closed-owner escape into a
		// false safe result.
		t.Ownership = mergeFacts(t.Ownership, fallback.Ownership)
		t.Captures = mergeFacts(t.Captures, fallback.Captures)
		t.Errors = remove(t.Errors, e.Name)
	case "construct":
		t = c.construct(e, env, inEffect)
	case "match":
		t = c.match(e, env, inEffect)
	case "binary":
		left, right := c.expr(e.Left, env, inEffect), c.expr(e.Right, env, inEffect)
		if left.Effect || right.Effect || left.Success != right.Success || (left.Success != "string" && left.Success != "bool" && left.Success != "i64") || (e.Name == "+" && left.Success != "string") {
			c.diagnostic("EF106", "operator requires matching primitive values; + accepts strings", e.Span)
		}
		t = value(left.Success)
		if e.Name == "==" {
			t.Success = "bool"
		}
	case "if":
		condition := c.expr(e.Left, env, inEffect)
		if condition.Effect || condition.Success != "bool" {
			c.diagnostic("EF106", "if condition must be bool", e.Left.Span)
		}
		a, b := c.block(e.Then, env, inEffect), c.block(e.Else, env, inEffect)
		if a.Success == "never" {
			t = b
		} else if b.Success == "never" {
			t = a
		} else {
			t = a
			if a.Success != b.Success || a.Effect != b.Effect {
				c.diagnostic("EF106", "if branches must return the same type", e.Span)
			}
			t.Ownership = mergeFacts(a.Ownership, b.Ownership)
			t.Captures = mergeFacts(a.Captures, b.Captures)
		}
		if a.Effect || b.Effect {
			c.diagnostic("EF103", "returning Effect values from branches is not supported in this prototype", e.Span)
		}
		t.Errors = union(union(a.Errors, b.Errors), executed(e.Left, true))
		t.Services = union(union(a.Services, b.Services), executed(e.Left, false))
		t.Effect = false
	default:
		c.diagnostic("EF103", "unsupported expression "+e.Kind, e.Span)
	}
	t.Type = c.typeRef(t.Success)
	e.Type = t
	return t
}

func (c *checker) dataCall(e *Expr, env map[string]ValueType, inEffect bool) (ValueType, bool) {
	if e.Left == nil {
		return ValueType{}, false
	}
	typeName, variantName := "", ""
	switch e.Left.Kind {
	case "name":
		typeName = e.Left.Name
	case "member":
		if e.Left.Left.Kind != "name" {
			return ValueType{}, false
		}
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	default:
		return ValueType{}, false
	}
	if variantName == "" && c.errors[typeName] != nil {
		c.diagnostic("EF102", "error declarations are failure payloads, not success values", e.Span)
		return value("invalid"), true
	}
	fields, ok := fieldsFor(c, typeName, variantName)
	if !ok || (variantName == "" && c.enums[typeName] != nil) {
		if variantName != "" && c.enums[typeName] == nil {
			return ValueType{}, false
		}
		if variantName == "" && c.records[typeName] == nil {
			return ValueType{}, false
		}
	}
	if variantName != "" {
		enum := c.enums[typeName]
		if enum == nil {
			return ValueType{}, false
		}
		found := false
		for _, variant := range enum.Variants {
			found = found || variant.Name == variantName
		}
		if !found {
			c.diagnostic("EF116", "unknown variant "+typeName+"."+variantName, e.Span)
			return value("invalid"), true
		}
	}
	if len(e.Fields) > 0 {
		if len(e.Fields) != len(e.Args) {
			c.diagnostic("EF122", "constructor arguments cannot mix named and positional forms", e.Span)
			for _, arg := range e.Args {
				c.expr(arg, env, false)
			}
			e.Text = "data"
			return value("invalid"), true
		}
		payloadExpr := &Expr{Kind: "payload", Fields: e.Fields, Span: e.Span}
		ownership := c.payload(payloadExpr, fields, env, e.Span)
		e.Text = "data"
		result := value(typeName)
		if variantName != "" {
			result.Ownership = prependFacts(variantName, ownership)
		} else {
			result.Ownership = ownership
		}
		return result, true
	}
	if len(e.Args) != len(fields) {
		c.diagnostic("EF115", "constructor "+typeName+" expects "+fmt.Sprint(len(fields))+" payload fields", e.Span)
	}
	argumentTypes := make([]ValueType, len(e.Args))
	for i, arg := range e.Args {
		got := c.expr(arg, env, false)
		argumentTypes[i] = got
		if i < len(fields) && (got.Effect || !sameType(got.Success, fields[i].Type)) {
			c.diagnostic("EF115", "payload field "+fields[i].Name+" must be "+fields[i].Type, arg.Span)
		}
	}
	e.Text = "data"
	e.Fields = make([]FieldValue, 0, len(e.Args))
	for i, arg := range e.Args {
		if i < len(fields) {
			e.Fields = append(e.Fields, FieldValue{Name: fields[i].Name, Value: arg, Span: arg.Span})
		}
	}
	result := value(typeName)
	ownership := []OwnershipFact{}
	for i, got := range argumentTypes {
		if i < len(fields) {
			ownership = append(ownership, prependFacts(fields[i].Name, got.Ownership)...)
		}
	}
	if variantName != "" {
		result.Ownership = prependFacts(variantName, ownership)
	} else {
		result.Ownership = normalizeFacts(ownership)
	}
	return result, true
}

func (c *checker) construct(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	if e.Left == nil {
		return value("invalid")
	}
	typeName, variantName := "", ""
	if e.Left.Kind == "name" {
		typeName = e.Left.Name
	} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	} else {
		c.diagnostic("EF114", "invalid data constructor", e.Span)
		return value("invalid")
	}
	if variantName == "" && c.errors[typeName] != nil {
		c.diagnostic("EF102", "error declarations are failure payloads, not success values", e.Span)
		return value("invalid")
	}
	fields, ok := fieldsFor(c, typeName, variantName)
	if !ok {
		if variantName != "" && c.enums[typeName] != nil {
			c.diagnostic("EF116", "unknown variant "+typeName+"."+variantName, e.Span)
		} else {
			c.diagnostic("EF102", "unknown data declaration "+typeName, e.Span)
		}
		return value("invalid")
	}
	if variantName != "" {
		if c.enums[typeName] == nil {
			c.diagnostic("EF116", typeName+" is not a closed enum", e.Span)
			return value("invalid")
		}
	}
	ownership := c.payload(e, fields, env, e.Span)
	result := value(typeName)
	if variantName != "" {
		result.Ownership = prependFacts(variantName, ownership)
	} else {
		result.Ownership = ownership
	}
	return result
}

func (c *checker) match(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	scrutinee := c.expr(e.Left, env, inEffect)
	if scrutinee.Effect {
		c.diagnostic("EF106", "match scrutinee must be a value; execute an Effect with run", e.Left.Span)
	}
	enum := c.enums[scrutinee.Success]
	if enum == nil {
		c.diagnostic("EF116", "match requires a closed enum value", e.Left.Span)
		return value("invalid")
	}
	declared := map[string]Variant{}
	for _, variant := range enum.Variants {
		declared[variant.Name] = variant
	}
	seen := map[string]bool{}
	result := value("never")
	branchErrors := []string{}
	branchServices := []string{}
	haveResult := false
	for _, arm := range e.Arms {
		pattern := arm.Pattern
		if pattern.TypeName == "_" {
			c.diagnostic("EF118", "catch-all match arms cannot claim exhaustive closed interpretation", pattern.Span)
			continue
		}
		if pattern.TypeName != enum.Name {
			c.diagnostic("EF116", "match pattern belongs to "+pattern.TypeName+", expected "+enum.Name, pattern.Span)
			continue
		}
		if pattern.VariantName == "" {
			c.diagnostic("EF118", "match arm must name a declared variant", pattern.Span)
			continue
		}
		variant, exists := declared[pattern.VariantName]
		if !exists {
			c.diagnostic("EF116", "unknown variant "+enum.Name+"."+pattern.VariantName, pattern.Span)
			continue
		}
		if seen[pattern.VariantName] {
			c.diagnostic("EF117", "duplicate match arm for "+enum.Name+"."+pattern.VariantName, pattern.Span)
			continue
		}
		seen[pattern.VariantName] = true
		branchEnv := clone(env)
		fields := fieldsMap(variant.Fields)
		aliases := map[string]bool{}
		for _, fieldName := range sortedBindingNames(pattern.Bindings) {
			binding := pattern.Bindings[fieldName]
			if binding != "_" {
				if aliases[binding] {
					c.diagnostic("EF121", "duplicate pattern binding "+binding, pattern.Span)
					continue
				}
				aliases[binding] = true
			}
			field, ok := fields[fieldName]
			if !ok {
				c.diagnostic("EF114", "unknown payload field "+fieldName+" in match arm", pattern.Span)
				continue
			}
			if binding == "_" {
				continue
			}
			bound := value(field.Type)
			bound.Ownership = projectVariantFacts(scrutinee.Ownership, pattern.VariantName, fieldName)
			if len(bound.Ownership) == 0 {
				bound.Ownership = c.unknownOwnership(field.Type)
			}
			branchEnv[binding] = bound
		}
		branch := c.block(arm.Body, branchEnv, inEffect)
		if branch.Success != "never" {
			if !haveResult {
				result, haveResult = branch, true
			} else if !sameType(result.Success, branch.Success) || result.Effect != branch.Effect {
				c.diagnostic("EF106", "match branches must return the same type", arm.Span)
			} else {
				result.Ownership = mergeFacts(result.Ownership, branch.Ownership)
				result.Captures = mergeFacts(result.Captures, branch.Captures)
			}
		}
		branchErrors = union(branchErrors, branch.Errors)
		branchServices = union(branchServices, branch.Services)
	}
	for _, variant := range enum.Variants {
		if !seen[variant.Name] {
			c.diagnostic("EF117", "missing match arm for "+enum.Name+"."+variant.Name, e.Span)
		}
	}
	if !haveResult {
		result = value("never")
	}
	result.Errors = union(branchErrors, tExecutedErrors(e.Left))
	result.Services = union(branchServices, tExecutedServices(e.Left))
	result.Effect = false
	return result
}
func (r *Result) Find(name string) *Symbol {
	for i := range r.Symbols {
		if r.Symbols[i].Name == name {
			return &r.Symbols[i]
		}
	}
	return nil
}
func (r *Result) FindDeclaration(name string) *Declaration {
	for i := range r.Declarations {
		if r.Declarations[i].Name == name {
			return &r.Declarations[i]
		}
	}
	return nil
}
func (r *Result) Entry() error {
	if !r.Checked {
		return fmt.Errorf("source has diagnostics")
	}
	main := r.Find("main")
	if main == nil {
		return fmt.Errorf("entry requires an effect fn main")
	}
	if !main.Contract.Effect || len(main.Params) > 0 {
		return fmt.Errorf("main must be an effect function with no parameters")
	}
	if len(main.Contract.Services) > 0 {
		return fmt.Errorf("main has unprovided services: %s", strings.Join(main.Contract.Services, ", "))
	}
	return nil
}

func (c *checker) fiberCall(e *Expr, env map[string]ValueType, inEffect bool) bool {
	if e.Left.Kind != "member" || e.Left.Left.Kind != "name" {
		return false
	}
	inner, exists := env[e.Left.Left.Name]
	if !exists || !strings.HasPrefix(inner.Success, "Fiber:") {
		return false
	}
	if len(e.Args) != 0 {
		c.diagnostic("EF106", "fiber operations take no arguments", e.Span)
	}
	t := value(strings.TrimPrefix(inner.Success, "Fiber:"))
	t.Effect = true
	t.Errors = inner.Errors
	switch e.Left.Name {
	case "join":
		t.Ownership = cloneFacts(inner.Child)
		t.Captures = nil
	case "interrupt":
		t.Success = "()"
		t.Ownership = nil
		t.Captures = nil
	case "cancel":
		t.Success = "()"
		t.Errors = []string{}
		t.Ownership = nil
		t.Captures = nil
	default:
		c.diagnostic("EF102", "unknown fiber operation "+e.Left.Name, e.Span)
	}
	e.Text = "fiber"
	e.Type = t
	return true
}

func (c *checker) requireGo(span Span, feature string) {
	c.program.GoOnly = true
	if c.result.Target != "go" {
		c.diagnostic("EF110", feature+" is currently implemented only for Go", span)
	}
}
