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
// A fact is deliberately explicit about uncertainty: the compiler only
// rejects a value when it can prove that the value belongs to a scope which is
// closing. Foreign values and summaries outside this model stay unknown.
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
	// potentialOwner marks a bounded wildcard which may contain an owned path
	// that the representation could not retain. It is internal so the public
	// contract still reports bounded evidence while escape checks diagnose
	// exhausted analysis instead of treating it as safe.
	potentialOwner bool
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
		facts = append(facts, OwnershipFact{Path: path, Status: "unknown", Origin: "unknown"})
	}
	return normalizeFacts(facts)
}

func (c *checker) borrowedOwnership(typeName, region string) []OwnershipFact {
	facts := []OwnershipFact{}
	for _, path := range c.ownershipPaths(typeName, "", map[string]bool{}) {
		facts = append(facts, OwnershipFact{Path: path, Status: "borrowed", Region: region, Origin: "parameter", source: path, sourceSet: true, ownerKind: ownershipOwnerParameter})
	}
	return normalizeFacts(facts)
}

func cloneFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	return append([]OwnershipFact{}, facts...)
}

func prependFacts(prefix string, facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		copy := fact
		if prefix != "" && copy.Path != "*" {
			if copy.Path == "" {
				copy.Path = prefix
			} else {
				copy.Path = prefix + "." + copy.Path
			}
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
	concrete := false
	for _, fact := range facts {
		if (fact.Path == field || strings.HasPrefix(fact.Path, prefix)) && (fact.Status == "owned" || fact.Status == "borrowed") {
			concrete = true
			break
		}
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		if fact.Path == "*" {
			if concrete {
				continue
			}
			copy := fact
			// A wildcard proves that some descendant has this status, not that
			// the selected field has it. Keep whole-value owned evidence at the
			// boundary, but report a projected field as unknown when its path is
			// ambiguous.
			if copy.Status == "owned" && copy.Origin != "bounded-all-owned" {
				copy.Status = "unknown"
				copy.Origin = "bounded"
			}
			if copy.Status == "borrowed" && copy.Origin != "bounded-all-borrowed" {
				copy.Status = "unknown"
				copy.Origin = "bounded"
			}
			copy.source = ""
			copy.sourceSet = false
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
	concrete := false
	for _, fact := range facts {
		if (fact.Path == prefix || strings.HasPrefix(fact.Path, prefix+".")) && (fact.Status == "owned" || fact.Status == "borrowed") {
			concrete = true
			break
		}
	}
	projected := make([]OwnershipFact, 0)
	for _, fact := range facts {
		if fact.Path == "*" {
			if concrete {
				continue
			}
			copy := fact
			if copy.Status == "owned" && copy.Origin != "bounded-all-owned" {
				copy.Status = "unknown"
				copy.Origin = "bounded"
			}
			if copy.Status == "borrowed" && copy.Origin != "bounded-all-borrowed" {
				copy.Status = "unknown"
				copy.Origin = "bounded"
			}
			copy.source = ""
			copy.sourceSet = false
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
			owned = append(owned, fact)
		}
	}
	if len(owned) == 0 {
		return []OwnershipFact{{Path: "*", Status: "unknown", Origin: "bounded"}}
	}
	return normalizeFacts(owned)
}

func ownershipPathMatches(argumentPath, sourcePath string) bool {
	// A bounded wildcard means that a handle exists somewhere below the
	// argument. It must remain eligible for a helper's parameter-relative
	// summary; dropping it would turn a proven owned path into an unsafe
	// unknown result.
	return argumentPath == "*" || sourcePath == "*" || argumentPath == sourcePath
}

func instantiateFacts(facts []OwnershipFact, params []Param, args []ValueType) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := make([]OwnershipFact, 0, len(facts))
	for _, fact := range facts {
		matched := false
		for i, param := range params {
			if fact.Region != "parameter:"+param.Name {
				continue
			}
			matched = true
			if i >= len(args) || len(args[i].Ownership) == 0 {
				out = append(out, OwnershipFact{Path: fact.Path, Status: "unknown", Origin: "helper"})
				continue
			}
			hasConcreteSource := slices.ContainsFunc(args[i].Ownership, func(argument OwnershipFact) bool {
				return argument.Path != "*" && argument.Path == fact.source
			})
			for _, argument := range args[i].Ownership {
				if hasConcreteSource && argument.Path == "*" {
					continue
				}
				if !ownershipPathMatches(argument.Path, fact.source) {
					continue
				}
				copy := argument
				if fact.source == "*" || argument.Path == "*" {
					// A wildcard source or argument is evidence about an
					// unspecified descendant. Preserve a whole-value owned
					// wildcard, but do not turn an ambiguous projected field into
					// a certain owned fact.
					if !(fact.Path == "*" && fact.sourceSet && fact.source == "*" && argument.Path == "*" && argument.Status == "owned" && argument.Origin == "bounded-all-owned") {
						copy.Status = "unknown"
						copy.Origin = "bounded"
						copy.potentialOwner = true
					}
				}
				// The summary path is relative to the returned value. The
				// argument contributes status/region, not another path prefix;
				// projection and payload construction already recorded the
				// returned shape before this call boundary.
				copy.Path = fact.Path
				copy.Origin = "helper"
				if argument.Path == "*" && argument.Status == "owned" && argument.Origin == "bounded-all-owned" {
					copy.Origin = "bounded-all-owned"
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

func normalizeFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	out := append([]OwnershipFact{}, facts...)
	slices.SortStableFunc(out, func(a, b OwnershipFact) int {
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
		return 0
	})
	result := make([]OwnershipFact, 0, len(out))
	for _, fact := range out {
		if len(result) == 0 || result[len(result)-1] != fact {
			result = append(result, fact)
		}
	}
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
			bounded := append([]OwnershipFact{}, result[:maxFacts-1]...)
			bounded = append(bounded, OwnershipFact{Path: "*", Status: "owned", Region: "*", Origin: "bounded-all-owned"})
			return bounded
		}
		if allBorrowed {
			bounded := append([]OwnershipFact{}, result[:maxFacts-2]...)
			bounded = append(bounded, result[len(result)-1])
			bounded = append(bounded, OwnershipFact{Path: "*", Status: "borrowed", Region: "*", Origin: "bounded-all-borrowed", source: "*", sourceSet: true})
			return bounded
		}
		for i := range result {
			if result[i].Path == "*" && (result[i].Origin == "bounded-all-owned" || result[i].Origin == "bounded-all-borrowed") {
				result[i].Status = "unknown"
				result[i].Origin = "bounded"
				result[i].potentialOwner = true
			}
		}
		owned := make([]OwnershipFact, 0, len(result))
		borrowed := make([]OwnershipFact, 0, len(result))
		other := make([]OwnershipFact, 0, len(result))
		for _, fact := range result {
			switch fact.Status {
			case "owned":
				owned = append(owned, fact)
			case "borrowed":
				borrowed = append(borrowed, fact)
			default:
				other = append(other, fact)
			}
		}
		potentialNeeded := false
		for _, fact := range other {
			if fact.potentialOwner {
				potentialNeeded = true
				break
			}
		}
		if len(owned) > maxFacts || (len(borrowed) > 0 && len(owned)+len(borrowed) > maxFacts) {
			potentialNeeded = true
		}
		ownedLimit := len(owned)
		if potentialNeeded {
			// Leave room for a bounded potential-owner marker. Exact owned
			// evidence is retained before any borrowed or unknown sibling is
			// summarized away.
			ownedLimit = maxFacts - 1
			if len(borrowed) > 0 {
				ownedLimit = maxFacts - 2
			}
		}
		if ownedLimit > len(owned) {
			ownedLimit = len(owned)
		}
		bounded := make([]OwnershipFact, 0, maxFacts)
		bounded = append(bounded, owned[:ownedLimit]...)
		reserve := 0
		if potentialNeeded {
			reserve = 1
		}
		for _, fact := range borrowed {
			if len(bounded) >= maxFacts-reserve {
				break
			}
			bounded = append(bounded, fact)
		}
		for _, fact := range other {
			if len(bounded) >= maxFacts-reserve {
				break
			}
			bounded = append(bounded, fact)
		}
		if potentialNeeded {
			bounded = append(bounded, OwnershipFact{Path: "*", Status: "unknown", Region: "*", Origin: "bounded", potentialOwner: true})
		}
		return bounded
	}
	return result
}

func hasOwnedFact(facts []OwnershipFact, region string) bool {
	for _, fact := range facts {
		if fact.Status == "owned" && (fact.Region == "*" || (fact.ownerKind == ownershipOwnerLexical && fact.Region == region)) {
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
	case name == "File", name == "Handler":
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
		case "string", "bool", "i64", "bytes", "File", "Handler", "Fiber", "Context", "Effect", "Scope", "Exit", "Cause", "Option", "never", "invalid":
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
	collectFunctionDependenciesExpr(e.Left, known, out)
	collectFunctionDependenciesExpr(e.Right, known, out)
	for _, arg := range e.Args {
		collectFunctionDependenciesExpr(arg, known, out)
	}
	for _, field := range e.Fields {
		collectFunctionDependenciesExpr(field.Value, known, out)
	}
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
	case "string", "bool", "()", "i64", "File", "bytes", "Handler":
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
