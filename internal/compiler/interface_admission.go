package compiler

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
)

// The cache contains private transport only. It never contains an arena ID,
// checked occurrence, declaration pointer, or callback relation from a caller.
var bundledSummaryCache = struct {
	sync.Mutex
	entries map[string]interfaceSummary
	bytes   int
}{entries: map[string]interfaceSummary{}}

func (c *checker) admitBundledSummaries() error {
	modules := map[string][]*Function{}
	for _, f := range c.program.BundledFunctions {
		modules[f.Module] = append(modules[f.Module], f)
	}
	for _, r := range c.program.BundledTemplates {
		if _, present := modules[r.Module]; !present {
			modules[r.Module] = []*Function{}
		}
	}
	// Admission interns each module's callable contracts into the receiving
	// arena, so modules are admitted in identity order rather than map order.
	for _, module := range slices.Sorted(maps.Keys(modules)) {
		functions := modules[module]
		slices.SortFunc(functions, func(a, b *Function) int { return strings.Compare(a.Identity, b.Identity) })
		input := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%s", module, c.result.Target, SemanticProducerIdentity, interfaceSummarySchema, ownershipSummarySchema, bundledInterfaceVersion)
		for _, f := range functions {
			data, err := bundledSources.ReadFile(bundledIndex[module][f.Name].Source)
			if err != nil {
				return err
			}
			input += "\x00" + f.Identity + "\x00" + formatDigest(string(data))
		}
		for _, source := range c.result.Sources {
			if source.Module == module {
				input += "\x00" + source.ID + "\x00" + source.Digest
			}
		}
		key := formatDigest(input)
		bundledSummaryCache.Lock()
		dto, ok := bundledSummaryCache.entries[key]
		bundledSummaryCache.Unlock()
		if !ok {
			var err error
			dto, err = produceBundledSummary(c, module, key, functions)
			if err != nil {
				return err
			}
			data, err := json.Marshal(dto)
			if err != nil {
				return err
			}
			dto, err = decodeInterfaceSummary(data, dto.ContentHash, key)
			if err != nil {
				return err
			}
			// Validate in the receiving arena before publishing any cache entry.
			if err := c.admitInterfaceSummary(dto, functions); err != nil {
				return err
			}
			bundledSummaryCache.Lock()
			if _, exists := bundledSummaryCache.entries[key]; !exists && len(bundledSummaryCache.entries) < maxBundledDeclarations && bundledSummaryCache.bytes+len(data) <= maxInterfaceClosureBytes {
				bundledSummaryCache.entries[key] = dto
				bundledSummaryCache.bytes += len(data)
			}
			bundledSummaryCache.Unlock()
		} else if err := c.admitInterfaceSummary(dto, functions); err != nil {
			return err
		}
		if c.admittedSummaries == nil {
			c.admittedSummaries = map[string]interfaceSummary{}
		}
		c.admittedSummaries[module] = dto
	}
	return nil
}

func produceBundledSummary(receiver *checker, module, key string, functions []*Function) (interfaceSummary, error) {
	target := receiver.result.Target
	p, diagnostics := parse("")
	if len(diagnostics) != 0 {
		return interfaceSummary{}, fmt.Errorf("interface producer parse failed")
	}
	p.interfaceProducer = true
	p.BundledBindings = map[string]map[string]*Function{}
	p.BundledTypeBindings = map[string]map[string]*Record{}
	r := &Result{Program: p, Target: target, Revision: key, Diagnostics: []Diagnostic{}, Symbols: []Symbol{}, facts: map[*Expr]ExpressionFacts{}, Sources: []SourceInfo{}}
	for _, original := range receiver.program.BundledTemplates {
		if original.Module != module {
			continue
		}
		data, err := bundledSources.ReadFile(bundledIndex[module][original.Name].Source)
		if err != nil {
			return interfaceSummary{}, err
		}
		parsed, diagnostics := parse(string(data))
		dataDeclarations := append(append([]*DataDeclaration{}, parsed.Records...), parsed.Enums...)
		if len(diagnostics) != 0 || len(dataDeclarations) != 1 {
			return interfaceSummary{}, fmt.Errorf("invalid template producer source")
		}
		rd := dataDeclarations[0]
		rd.Module, rd.SourceID, rd.Identity, rd.EmissionName = module, original.SourceID, original.Identity, original.EmissionName
		p.BundledTemplates = append(p.BundledTemplates, rd)
		for name, typ := range parsed.typeExpressions {
			p.typeExpressions[name] = typ
		}
		r.Sources = append(r.Sources, SourceInfo{ID: rd.SourceID, Module: module, Digest: formatDigest(string(data)), Version: bundledInterfaceVersion})
	}
	for _, original := range functions {
		data, err := bundledSources.ReadFile(bundledIndex[module][original.Name].Source)
		if err != nil {
			return interfaceSummary{}, err
		}
		parsed, diagnostics := parse(string(data))
		if len(diagnostics) != 0 || len(parsed.Functions) != 1 {
			return interfaceSummary{}, fmt.Errorf("invalid interface producer source")
		}
		f := parsed.Functions[0]
		if !admitBundledIntrinsic(bundledIndex[module][original.Name].Intrinsic, f) {
			return interfaceSummary{}, fmt.Errorf("invalid interface producer intrinsic")
		}
		f.Module, f.SourceID, f.Identity, f.EmissionName = module, original.SourceID, original.Identity, original.EmissionName
		p.BundledFunctions = append(p.BundledFunctions, f)
		for name, typ := range parsed.typeExpressions {
			p.typeExpressions[name] = typ
		}
		r.Sources = append(r.Sources, SourceInfo{ID: f.SourceID, Module: module, Digest: formatDigest(string(data)), Version: bundledInterfaceVersion})
	}
	slices.SortFunc(r.Sources, func(a, b SourceInfo) int { return strings.Compare(a.ID, b.ID) })
	c := newChecker(p, r)
	c.check()
	if len(r.Diagnostics) != 0 {
		return interfaceSummary{}, fmt.Errorf("interface producer checking failed: %s", r.Diagnostics[0].Message)
	}
	return exportInterfaceSummary(c, module, key, p.BundledFunctions)
}

type summaryAdmission struct {
	c                  *checker
	functions          map[string]*Function
	types              map[string]TypeID
	rows               map[string]RowID
	evidence           map[string]callableEvidence
	occurrences        map[string]summaryOccurrence
	relations          map[string]summaryRelation
	resolved           map[string]*callbackResultRelation
	visiting           map[string]bool
	occurrenceVisiting map[string]bool
	err                error
}

func (c *checker) admitInterfaceSummary(dto interfaceSummary, functions []*Function) error {
	a := summaryAdmission{occurrenceVisiting: map[string]bool{}, c: c, functions: map[string]*Function{}, types: map[string]TypeID{}, rows: map[string]RowID{"": emptyRowID}, evidence: map[string]callableEvidence{}, occurrences: map[string]summaryOccurrence{}, relations: map[string]summaryRelation{}, resolved: map[string]*callbackResultRelation{}, visiting: map[string]bool{}}
	if len(dto.Declarations) != len(functions) {
		return fmt.Errorf("declaration closure mismatch")
	}
	sources := []SourceInfo{}
	for _, source := range c.result.Sources {
		if source.Module == dto.Module {
			sources = append(sources, source)
		}
	}
	slices.SortFunc(sources, func(a, b SourceInfo) int { return strings.Compare(a.ID, b.ID) })
	transportSources := append([]SourceInfo{}, dto.Sources...)
	slices.SortFunc(transportSources, func(a, b SourceInfo) int { return strings.Compare(a.ID, b.ID) })
	if !reflect.DeepEqual(sources, transportSources) {
		return fmt.Errorf("source manifest mismatch")
	}
	for _, f := range functions {
		if f.Module != dto.Module {
			return fmt.Errorf("module owner mismatch")
		}
		a.functions[f.Identity] = f
	}
	// Only already checked signature owners admit nominal and native types.
	// Structural contracts can then be reconstructed from those canonical IDs.
	var admitType func(TypeID)
	seedEdges := 0
	childType := func(id TypeID) {
		seedEdges++
		if seedEdges > maxInterfaceTableEntries {
			a.err = fmt.Errorf("checked layout admission exceeds budget")
			return
		}
		admitType(id)
	}
	admitType = func(id TypeID) {
		if id == invalidTypeID || a.err != nil {
			return
		}
		ref := c.typeNodeID(id)
		if _, ok := a.types[ref]; ok {
			return
		}
		a.types[ref] = id
		n := c.node(id)
		for _, arg := range n.Args {
			childType(arg)
		}
		if n.Result != invalidTypeID {
			childType(n.Result)
		}
		c.walkDataFields(id, func(_ string, field Field) { childType(field.typeID) })
	}
	for _, f := range functions {
		admitType(c.checkedFunction(f, true, false).contractID())
	}
	for _, r := range c.program.BundledTemplates {
		if r.Module == dto.Module {
			for _, p := range r.Parameters {
				admitType(p.typeID)
				admitType(p.shapeID)
			}
			for _, field := range r.Fields {
				admitType(field.typeID)
			}
			for _, variant := range r.Variants {
				for _, field := range variant.Fields {
					admitType(field.typeID)
				}
			}
		}
	}
	if a.err != nil {
		return a.err
	}
	seenTemplates := map[string]bool{}
	for _, template := range dto.Templates {
		r := c.templates[template.Ref]
		if r == nil || r.Module != dto.Module || seenTemplates[template.Ref] || r.SourceID != template.Source || template.Kind != r.Kind || len(template.Parameters) != len(r.Parameters) || len(template.Fields) != len(r.Fields) || len(template.Variants) != len(r.Variants) {
			return fmt.Errorf("template declaration owner mismatch")
		}
		seenTemplates[template.Ref] = true
		for i, p := range template.Parameters {
			owner := r.Parameters[i]
			shape := ""
			if owner.shapeID != invalidTypeID {
				shape = c.typeNodeID(owner.shapeID)
			}
			if p.Name != owner.Name || p.Kind != owner.Kind || p.Ref != owner.Identity || p.Variable != c.typeNodeID(owner.typeID) || p.Shape != shape {
				return fmt.Errorf("template variable/constraint owner mismatch")
			}
		}
		for i, field := range template.Fields {
			if field.Name != r.Fields[i].Name || field.Type != c.typeNodeID(r.Fields[i].typeID) {
				return fmt.Errorf("template field owner mismatch")
			}
		}
		for i, variant := range template.Variants {
			owner := r.Variants[i]
			if variant.Name != owner.Name || len(variant.Fields) != len(owner.Fields) {
				return fmt.Errorf("template variant owner mismatch")
			}
			for j, field := range variant.Fields {
				if field.Name != owner.Fields[j].Name || field.Type != c.typeNodeID(owner.Fields[j].typeID) {
					return fmt.Errorf("template variant field owner mismatch")
				}
			}
		}
	}
	for _, r := range c.program.BundledTemplates {
		if r.Module == dto.Module && !seenTemplates[r.Identity] {
			return fmt.Errorf("template closure incomplete")
		}
	}
	for _, row := range dto.Rows {
		if len(a.rows) > maxInterfaceTableEntries || row.Ref == "" || len(row.Labels) == 0 {
			return fmt.Errorf("invalid row table")
		}
		if _, exists := a.rows[row.Ref]; exists {
			return fmt.Errorf("duplicate row reference")
		}
		if !slices.Equal(row.Labels, normalized(row.Labels)) {
			return fmt.Errorf("noncanonical row labels")
		}
		for _, label := range row.Labels {
			if _, parameter := c.rowDefinitions[label]; !parameter && c.errors[label] == nil && c.services[label] == nil {
				return fmt.Errorf("row label has no admitted declaration owner")
			}
		}
		for _, parameter := range row.Parameters {
			if actual, ok := c.rowDefinitions[parameter.ID]; !ok || !reflect.DeepEqual(actual, parameter) {
				return fmt.Errorf("row parameter owner mismatch")
			}
		}
		id := c.internRow(row.Labels)
		actual := c.rows[id-1]
		if row.Ref != c.rowNodeID(id) || !reflect.DeepEqual(append([]RowParameter{}, actual.Parameters...), row.Parameters) {
			return fmt.Errorf("row identity mismatch")
		}
		a.rows[row.Ref] = id
	}
	if len(dto.Types) > maxInterfaceTableEntries {
		return fmt.Errorf("type table budget exceeded")
	}
	pending := append([]summaryType{}, dto.Types...)
	seen := map[string]bool{}
	for _, typ := range pending {
		if typ.Ref == "" || seen[typ.Ref] {
			return fmt.Errorf("duplicate type reference")
		}
		seen[typ.Ref] = true
	}
	for _, typ := range pending {
		for _, ref := range typ.Arguments {
			if !seen[ref] {
				return fmt.Errorf("dangling type argument")
			}
		}
		if typ.Result != "" && !seen[typ.Result] {
			return fmt.Errorf("dangling result type")
		}
	}
	for len(pending) > 0 {
		next := []summaryType{}
		for _, typ := range pending {
			args := []TypeID{}
			ready := true
			for _, ref := range typ.Arguments {
				id, ok := a.types[ref]
				ready = ready && ok
				args = append(args, id)
			}
			result := invalidTypeID
			if typ.Result != "" {
				var ok bool
				result, ok = a.types[typ.Result]
				ready = ready && ok
			}
			failure, fok := a.rows[typ.Failures]
			service, sok := a.rows[typ.Services]
			if !fok || !sok {
				return fmt.Errorf("dangling type row")
			}
			if !ready {
				next = append(next, typ)
				continue
			}
			id, owned := a.types[typ.Ref]
			if !owned {
				if typ.Kind == "application" {
					owner := c.templates[typ.Declaration]
					var err error
					id, err = c.templateApplication(owner, args)
					if err != nil {
						return fmt.Errorf("application has no admitted checked owner: %w", err)
					}
				} else if (typ.Kind != "callable" && typ.Kind != "recipe" && typ.Kind != "providerRecipe") || typ.Declaration != "" || result == invalidTypeID || (typ.Mode != "pure" && typ.Mode != "effect") {
					return fmt.Errorf("type has no admitted canonical owner")
				} else {
					id = c.internContract(typ.Kind, typ.Mode, result, args, failure, service)
				}
			}
			n := c.node(id)
			if n.Kind != typ.Kind || n.Name != typ.Name || n.Declaration != typ.Declaration || n.Mode != typ.Mode || !slices.Equal(n.Args, args) || n.Result != result || n.FailureRow != failure || n.ServiceRow != service || c.typeNodeID(id) != typ.Ref {
				return fmt.Errorf("type owner or shape mismatch")
			}
			a.types[typ.Ref] = id
		}
		if len(next) == len(pending) {
			return fmt.Errorf("dangling or cyclic type graph")
		}
		pending = next
	}
	for ref := range a.types {
		if !seen[ref] {
			delete(a.types, ref)
		}
	}
	for _, item := range dto.Evidence {
		if len(a.evidence) >= maxInterfaceTableEntries || item.Ref == "" {
			return fmt.Errorf("invalid evidence table")
		}
		if _, exists := a.evidence[item.Ref]; exists {
			return fmt.Errorf("duplicate evidence reference")
		}
		e := callableEvidence{unresolved: item.Unresolved}
		if len(item.Callees) > len(e.callees) {
			return fmt.Errorf("known callee budget exceeded")
		}
		for i, ref := range item.Callees {
			f := a.functions[ref]
			if f == nil || slices.Contains(item.Callees[:i], ref) {
				return fmt.Errorf("invalid callee declaration")
			}
			e.callees[i] = f
			e.count++
		}
		binding := item.Parameter
		switch binding.Kind {
		case "absent":
			if binding.Declaration != "" || binding.Ordinal != -1 || binding.Path != "" {
				return fmt.Errorf("invalid absent evidence")
			}
		case "parameter":
			f := a.functions[binding.Declaration]
			if f == nil || binding.Ordinal < 0 || binding.Ordinal >= len(f.Params) {
				return fmt.Errorf("invalid callable parameter owner")
			}
			id := c.fieldContract(f.Params[binding.Ordinal].typeID, binding.Path)
			if id == invalidTypeID || c.node(id).Kind != "callable" {
				return fmt.Errorf("invalid callable field parameter owner")
			}
			e.parameter, e.parameterName = f, f.Params[binding.Ordinal].Name
			e.parameterPath = binding.Path
		default:
			return fmt.Errorf("unsupported evidence discriminant")
		}
		a.evidence[item.Ref] = e
	}
	for _, item := range dto.Occurrences {
		if len(a.occurrences) >= maxInterfaceTableEntries || item.Ref == "" {
			return fmt.Errorf("invalid occurrence table")
		}
		if _, ok := a.occurrences[item.Ref]; ok {
			return fmt.Errorf("duplicate occurrence")
		}
		a.occurrences[item.Ref] = item
	}
	for _, item := range dto.Relations {
		if len(a.relations) >= maxInterfaceTableEntries || item.Ref == "" {
			return fmt.Errorf("invalid relation table")
		}
		if _, ok := a.relations[item.Ref]; ok {
			return fmt.Errorf("duplicate relation")
		}
		a.relations[item.Ref] = item
	}
	// Validate every retained node, including nodes not reached by one declaration.
	for _, ref := range slices.Sorted(maps.Keys(a.occurrences)) {
		a.occurrence(ref, 0)
	}
	for _, ref := range slices.Sorted(maps.Keys(a.relations)) {
		a.relation(ref, 0)
	}
	if a.err != nil {
		return a.err
	}
	seenDeclarations := map[string]bool{}
	type admitted struct {
		f                   *Function
		ownership, captures []OwnershipFact
		evidence            callableEvidence
		failures            failureEvidence
	}
	values := []admitted{}
	for _, d := range dto.Declarations {
		f := a.functions[d.Ref]
		if f == nil || seenDeclarations[d.Ref] || f.SourceID != d.Source || !seen[d.Signature] || c.typeNodeID(c.checkedFunction(f, true, false).contractID()) != d.Signature || len(f.Params) != len(d.Parameters) {
			return fmt.Errorf("declaration owner mismatch")
		}
		seenDeclarations[d.Ref] = true
		for i, p := range d.Parameters {
			if p.Name != f.Params[i].Name || a.types[p.Type] != f.Params[i].typeID || p.RequiredChoice != f.Params[i].RequiredChoice || !sameConstantValue(p.DefaultValue, f.Params[i].DefaultValue) {
				return fmt.Errorf("parameter owner mismatch")
			}
		}
		e, ok := a.evidence[d.ReturnEvidence]
		if !ok {
			return fmt.Errorf("dangling return evidence")
		}
		body := a.occurrence(d.Body, 0)
		ownership, captures := a.facts(d.Ownership, 0), a.facts(d.Captures, 0)
		failures := a.failures(d.Failures, 0)
		// A decoder refusal is the cause: it is reported before the decoded
		// evidence is compared with what the retained body recomputes.
		if a.err != nil {
			return a.err
		}
		if err := a.wellFormedLayers(f, ownership, captures); err != nil {
			return err
		}
		summary := c.summaryOccurrence(body, f.Effect)
		ownershipMatches := slices.Equal(summary.ownershipFacts(), ownership)
		capturesMatch := slices.Equal(summary.captureFacts(), captures)
		evidenceMatches := body.callableEvidence == e
		// The declaration's payload evidence is recomputed from the
		// retained body exactly as the producer's function edge does.
		expected := mapFailureFacts(c.declaredFailures(summary.failures, c.failureLayerRows(body.valueID()), c.callFailureRows(f.returnID, f.Effect, f.failureID)), transportableFacts)
		failuresMatch := equalFailures(expected, failures)
		if !ownershipMatches || !capturesMatch || !evidenceMatches || !failuresMatch {
			return fmt.Errorf("declaration %s summary disagrees with retained body occurrence (ownership=%t captures=%t evidence=%t failures=%t)", d.Ref, ownershipMatches, capturesMatch, evidenceMatches, failuresMatch)
		}
		values = append(values, admitted{f, ownership, captures, e, failures})
	}
	if a.err != nil {
		return a.err
	}
	for _, v := range values {
		v.f.Ownership, v.f.Captures, v.f.returnCallableEvidence, v.f.failures = v.ownership, v.captures, v.evidence, v.failures
		for _, d := range dto.Declarations {
			if d.Ref == v.f.Identity {
				v.f.returnFields = cloneFieldOccurrences(c.summaryOccurrence(a.occurrence(d.Body, 0), v.f.Effect).fields)
				break
			}
		}
	}
	return nil
}

// maxSummaryLayers bounds the layer and level indices a summary may name.
const maxSummaryLayers = 64

// owner decodes one owner atom. Closed owners are transient: a summary
// never carries a timeout or child owner of the producing module.
func (a *summaryAdmission) owner(name string, r summaryRegion) (ownershipOwnerKind, string, bool) {
	index := slices.Index(summaryOwnerKinds[:], name)
	kind := ownershipOwnerKind(index)
	if index < 0 || closedOwnerKind(kind) {
		return 0, "", false
	}
	if (r.Kind != "parameter" && r.Kind != "exec" && r.Ordinal != -1) || (r.Kind != "parameter" && r.Declaration != "") {
		a.err = fmt.Errorf("noncanonical region")
		return 0, "", false
	}
	switch r.Kind {
	case "unknown":
		return kind, "", true
	case "wildcard":
		return kind, "*", true
	case "invocation":
		return kind, "invocation", kind == ownershipOwnerLexical
	case "parameter-wildcard":
		return kind, "parameter:*", true
	case "exec":
		if kind != ownershipOwnerExec || r.Ordinal < 0 || r.Ordinal > maxSummaryLayers || r.Declaration != "" {
			return 0, "", false
		}
		return kind, execRegion(r.Ordinal), true
	case "parameter":
		f := a.functions[r.Declaration]
		if f == nil || r.Ordinal < 0 || r.Ordinal >= len(f.Params) {
			a.err = fmt.Errorf("invalid region owner")
			return 0, "", false
		}
		return kind, "parameter:" + f.Params[r.Ordinal].Name, true
	}
	a.err = fmt.Errorf("unsupported region discriminant")
	return 0, "", false
}

// wellFormedLayers checks a declaration's summary against its call layers
// (design §8 row 35): a held layer names a layer of the call occurrence and
// an Exec owner names an enclosing binder, so no Exec is free beyond the
// declared depth and no transient Lexical region survives the edge.
func (a *summaryAdmission) wellFormedLayers(f *Function, ownership, captures []OwnershipFact) error {
	layers := a.c.invocationLayers(f.returnID, f.Effect)
	for _, fact := range ownership {
		if fact.ownerKind == ownershipOwnerExec && fact.exec >= layers {
			return fmt.Errorf("declaration %s summary names a free layer executor", f.Identity)
		}
		if fact.ownerKind == ownershipOwnerLexical {
			return fmt.Errorf("declaration %s summary retains a transient region", f.Identity)
		}
	}
	for _, fact := range captures {
		if fact.layer > layers || (fact.ownerKind == ownershipOwnerExec && fact.exec >= fact.layer) {
			return fmt.Errorf("declaration %s summary names a free layer executor", f.Identity)
		}
		if fact.ownerKind == ownershipOwnerLexical {
			return fmt.Errorf("declaration %s summary retains a transient region", f.Identity)
		}
	}
	return nil
}

func (a *summaryAdmission) facts(items []summaryOwner, depth int) []OwnershipFact {
	out := []OwnershipFact{}
	for _, item := range items {
		if (!item.SourceSet && item.SourcePath != "") || (!item.Remainder && item.RemainderExclusions != "") {
			a.err = fmt.Errorf("noncanonical ownership source or remainder")
			return nil
		}
		kind, region, ok := a.owner(item.OwnerKind, item.Region)
		if !ok || (item.Status != "owned" && item.Status != "borrowed" && item.Status != "unknown") || item.Layer < 0 || item.Layer > maxSummaryLayers {
			a.err = fmt.Errorf("unsupported owner facts")
			return nil
		}
		fact := OwnershipFact{Path: item.Path, Status: item.Status, Region: region, Origin: item.Origin, source: item.SourcePath, sourceSet: item.SourceSet, ownerKind: kind, potentialOwner: item.PotentialOwner, remainder: item.Remainder, remainderExclusions: item.RemainderExclusions, layer: item.Layer}
		if kind == ownershipOwnerExec {
			fact.exec = item.Region.Ordinal
		}
		env := make([]OwnershipFact, 0, len(item.Environment))
		for _, entry := range item.Environment {
			owner, ownerRegion, ok := a.owner(entry.OwnerKind, entry.Region)
			if !ok || owner == ownershipOwnerRelation || owner == ownershipOwnerParameter || owner == ownershipOwnerUnknown || len(env) > maxSummaryLayers {
				a.err = fmt.Errorf("unsupported relation environment")
				return nil
			}
			atom := OwnershipFact{Status: "owned", Region: ownerRegion, ownerKind: owner}
			if owner == ownershipOwnerExec {
				atom.exec = entry.Region.Ordinal
			}
			env = append(env, atom)
		}
		switch item.Relation.Kind {
		case "absent":
			if item.Relation.Ref != "" || kind == ownershipOwnerRelation || len(env) != 0 {
				a.err = fmt.Errorf("invalid absent relation")
			}
		case "relation":
			fact.callbackRelation = a.relation(item.Relation.Ref, depth+1)
			if kind != ownershipOwnerRelation || len(env) < 2 {
				a.err = fmt.Errorf("relation evidence requires a relation owner and its environment")
			}
			fact.relationEnv = encodeOwnerEnv(env)
		default:
			a.err = fmt.Errorf("unsupported relation discriminant")
		}
		out = append(out, fact)
	}
	return out
}

func (a *summaryAdmission) occurrence(ref string, depth int) checkedExpression {
	item, ok := a.occurrences[ref]
	if !ok || depth > 32 || a.occurrenceVisiting[ref] {
		a.err = fmt.Errorf("dangling or excessive occurrence graph")
		return checkedExpression{}
	}
	a.occurrenceVisiting[ref] = true
	defer delete(a.occurrenceVisiting, ref)
	id, tok := a.types[item.Contract]
	evidence, eok := a.evidence[item.Evidence]
	ef, fok := a.rows[item.Evaluation.Failures]
	es, sok := a.rows[item.Evaluation.Services]
	xf, xfok := a.rows[item.Executed.Failures]
	xs, xsok := a.rows[item.Executed.Services]
	if !tok || !eok || !fok || !sok || !xfok || !xsok {
		a.err = fmt.Errorf("dangling occurrence reference")
		return checkedExpression{}
	}
	evaluation, executed := a.evaluation(ef, es, item.Evaluation, depth), a.evaluation(xf, xs, item.Executed, depth)
	failures := a.failures(item.Failures, depth)
	fields := map[string]checkedExpression{}
	layoutID := a.c.occurrenceLayoutID(id)
	shape, hasShape := a.c.checkedFields(layoutID)
	_, variants, enum := a.c.checkedVariants(layoutID)
	if enum {
		if len(item.Fields) != 0 {
			a.err = fmt.Errorf("enum occurrence has record fields")
			return checkedExpression{}
		}
		for _, variant := range item.Variants {
			index := slices.IndexFunc(variants, func(v Variant) bool { return v.Name == variant.Name })
			if _, duplicate := fields[variant.Name]; duplicate || index < 0 {
				a.err = fmt.Errorf("invalid variant occurrence owner")
				return checkedExpression{}
			}
			payload := a.c.checkedData(voidTypeName)
			payload.fields = a.payloadOccurrences(variant.Fields, variants[index].Fields, depth, true)
			if a.err != nil {
				return checkedExpression{}
			}
			fields[variant.Name] = payload
		}
		if a.c.callableFieldLayout(layoutID, map[TypeID]bool{}, map[TypeID]bool{}, 0) && len(fields) == 0 {
			a.err = fmt.Errorf("missing variant occurrence evidence")
			return checkedExpression{}
		}
	} else {
		if len(item.Variants) != 0 || !hasShape && len(item.Fields) > 0 {
			a.err = fmt.Errorf("invalid occurrence layout kind")
			return checkedExpression{}
		}
		complete := hasShape && a.c.callableFieldLayout(layoutID, map[TypeID]bool{}, map[TypeID]bool{}, 0)
		fields = a.payloadOccurrences(item.Fields, shape, depth, complete)
		if a.err != nil {
			return checkedExpression{}
		}
	}
	if len(fields) == 0 {
		fields = nil
	}
	return checkedExpression{fields: fields, value: a.c.values.occurrence(id, a.facts(item.Ownership, depth), a.facts(item.Captures, depth)), child: a.facts(item.Child, depth), callableEvidence: evidence, evaluation: evaluation, executed: executed, failures: failures}
}

// canonicalWith reports whether labels are a sorted set of non-empty labels
// without a separator.
func canonicalWith(labels []string) bool {
	for i, label := range labels {
		if label == "" || strings.Contains(label, ",") || i > 0 && labels[i-1] >= label {
			return false
		}
	}
	return true
}

// failures decodes payload evidence. Keys are strictly ordered, so each
// failure has one canonical encoding; a label without an entry stays ⊤.
// Each invariant has its own refusal, checked before the evidence is
// compared with the retained body.
func (a *summaryAdmission) failures(items []summaryFailure, depth int) failureEvidence {
	var out failureEvidence
	var previous failureKey
	for i, item := range items {
		key := failureKey{layer: item.Layer, label: item.Label, pending: item.Pending, fork: item.Fork, with: joinWith(item.With)}
		var refusal string
		switch {
		case item.Layer < 0 || item.Layer > maxSummaryLayers:
			refusal = "failure evidence layer out of range"
		case item.Label == "":
			refusal = "failure evidence without a label"
		case !item.Pending && item.Fork != "":
			refusal = "fork instance on non-pending failure evidence"
		case !item.Pending && len(item.With) > 0:
			refusal = "with labels on non-pending failure evidence"
		case !canonicalWith(item.With):
			refusal = "noncanonical failure evidence with labels"
		case i > 0 && compareFailureKeys(previous, key) >= 0:
			refusal = "failure evidence out of canonical order"
		}
		if refusal != "" {
			a.err = fmt.Errorf("%s", refusal)
			return nil
		}
		previous = key
		if out == nil {
			out = failureEvidence{}
		}
		out[key] = canonicalDecodedFacts(a.facts(item.Owners, depth))
	}
	return out
}

// pendingAtZero decodes an evaluation's pending or live failures: pending
// entries of layer 0.
func (a *summaryAdmission) pendingAtZero(items []summaryFailure, depth int) failureEvidence {
	out := a.failures(items, depth)
	for key := range out {
		if !key.pending || key.layer != 0 {
			a.err = fmt.Errorf("noncanonical evaluation pending evidence")
			return nil
		}
	}
	return out
}

// evaluation decodes an evaluation's rows, layer-0 payload evidence, uses
// and the failures of its forked children.
func (a *summaryAdmission) evaluation(failureRow, serviceRow RowID, item summaryRows, depth int) ExpressionEvaluation {
	var payloads map[string][]OwnershipFact
	for i, payload := range item.Payloads {
		if payload.Layer != 0 || payload.Label == "" || payload.Pending || payload.Fork != "" || i > 0 && item.Payloads[i-1].Label >= payload.Label {
			a.err = fmt.Errorf("noncanonical evaluation payload evidence")
			return ExpressionEvaluation{}
		}
		if payloads == nil {
			payloads = map[string][]OwnershipFact{}
		}
		payloads[payload.Label] = canonicalDecodedFacts(a.facts(payload.Owners, depth))
	}
	e := a.c.evaluationWith(failureRow, serviceRow, payloads, canonicalDecodedFacts(a.facts(item.Uses, depth)))
	e.pending, e.live = a.pendingAtZero(item.Pending, depth), a.pendingAtZero(item.Live, depth)
	return e
}

func canonicalDecodedFacts(facts []OwnershipFact) []OwnershipFact {
	if len(facts) == 0 {
		return nil
	}
	return facts
}

func (a *summaryAdmission) payloadOccurrences(items []summaryFieldOccurrence, shape []Field, depth int, complete bool) map[string]checkedExpression {
	fields := map[string]checkedExpression{}
	for _, field := range items {
		if _, duplicate := fields[field.Name]; duplicate {
			a.err = fmt.Errorf("duplicate payload occurrence field")
			return nil
		}
		value := a.occurrence(field.Occurrence, depth+1)
		if a.err != nil {
			return nil
		}
		index := slices.IndexFunc(shape, func(f Field) bool { return f.Name == field.Name })
		if index < 0 || shape[index].typeID != value.contractID() {
			a.err = fmt.Errorf("field occurrence contract mismatch")
			return nil
		}
		fields[field.Name] = value
	}
	if complete && len(fields) != len(shape) {
		a.err = fmt.Errorf("incomplete field occurrence shape")
		return nil
	}
	return fields
}

func (a *summaryAdmission) relation(ref string, depth int) *callbackResultRelation {
	if a.visiting[ref] || depth > 32 {
		a.err = fmt.Errorf("cyclic or excessive callback graph")
		return nil
	}
	if relation := a.resolved[ref]; relation != nil {
		return relation
	}
	item, ok := a.relations[ref]
	result, tok := a.types[item.Result]
	evidence, eok := a.evidence[item.Evidence]
	if !ok || !tok || !eok || len(item.Arguments) > maxInterfaceTableEntries {
		a.err = fmt.Errorf("dangling callback relation")
		return nil
	}
	a.visiting[ref] = true
	args := []checkedExpression{}
	for _, arg := range item.Arguments {
		args = append(args, a.occurrence(arg, depth+1))
	}
	delete(a.visiting, ref)
	if a.err != nil {
		return nil
	}
	if item.Held < -1 || item.Held > maxSummaryLayers || item.Failure != "" && item.Held < 0 {
		a.err = fmt.Errorf("callback relation slot out of range")
		return nil
	}
	relation := a.c.callbackRelation(evidence, args, result, item.Path, item.Held, item.Effect, item.Failure)
	if relation == nil {
		a.err = fmt.Errorf("callback relation arena budget exceeded")
		return nil
	}
	// A receiving authority still verifies that interning preserves every
	// operational field; future changes cannot silently regress the owning key.
	for i, arg := range args {
		actual := relation.arguments[i]
		if !reflect.DeepEqual(actual.fields, arg.fields) || !reflect.DeepEqual(actual.captureFacts(), arg.captureFacts()) || !reflect.DeepEqual(actual.child, arg.child) || !reflect.DeepEqual(actual.evaluation, arg.evaluation) || !reflect.DeepEqual(actual.executed, arg.executed) || !reflect.DeepEqual(actual.failures, arg.failures) {
			a.err = fmt.Errorf("callback interner would lose operational facts")
			return nil
		}
	}
	a.resolved[ref] = relation
	return relation
}
