package compiler

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	lintsdk "effra.local/prototype/lint"
)

// builtinFailureIdentity names a compiler-supplied failure. Source failures
// use their declaration identity; builtins have no source declaration.
func builtinFailureIdentity(name string) string { return "error:builtin:" + name }

// LintFacts derives a read-only custom-lint snapshot from checked facts.
// Requested families are derived; none means every family. Derivation reads
// the checker's retained resolutions, rows and original spans only: it never
// re-resolves names, consults inspection output or mutates the checker.
func (r *Result) LintFacts(families ...lintsdk.Family) *lintsdk.Snapshot {
	if len(families) == 0 {
		families = lintsdk.Families()
	}
	semantic := lintsdk.Semantic{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target}
	if qualified := r.producerMetadata.Snapshot; qualified.Producer != "" {
		semantic.Producer, semantic.ReuseScope = qualified.Producer, qualified.ReuseScope
	}
	snapshot := &lintsdk.Snapshot{
		Schema:   lintsdk.Schema{Name: lintsdk.FactSchemaName, Version: lintsdk.FactSchemaVersion},
		Semantic: semantic,
		Source:   lintsdk.Source{Bytes: r.sourceBytes},
		Checked:  r.Checked,
		Families: []lintsdk.Family{},
	}
	requested := map[lintsdk.Family]bool{}
	for _, family := range families {
		requested[family] = true
	}
	unavailable := func(family lintsdk.Family, reason string) {
		snapshot.Unavailable = append(snapshot.Unavailable, lintsdk.UnavailableFamily{Family: family, Reason: reason})
	}
	ready := r.Checked && r.Program != nil && r.Program.semantic != nil
	deriver := &lintFactDeriver{r: r}
	if ready {
		deriver.c = r.Program.semantic
		deriver.index()
	}
	for _, family := range lintsdk.Families() {
		if !requested[family] {
			continue
		}
		if !ready {
			unavailable(family, lintsdk.ReasonUncheckedSource)
			continue
		}
		var err error
		switch family {
		case lintsdk.FamilyDeclarations:
			err = deriver.declarations(snapshot)
		case lintsdk.FamilyCallables:
			err = deriver.callables(snapshot)
		case lintsdk.FamilyCalls, lintsdk.FamilyProvisions:
			err = deriver.edges(snapshot, family)
		case lintsdk.FamilyBindings:
			if r.lexical == nil || !r.lexical.complete {
				unavailable(family, lintsdk.ReasonFactBudget)
				continue
			}
			err = deriver.bindings(snapshot)
		case lintsdk.FamilyImports:
			deriver.imports(snapshot)
		}
		if err != nil {
			unavailable(family, "derivation-failed: "+err.Error())
			continue
		}
		snapshot.Families = append(snapshot.Families, family)
	}
	return snapshot
}

type lintFactDeriver struct {
	r *Result
	c *checker
	// sources are the source callables in declaration order with their
	// fact identities; operations has the owning service of an operation.
	sources    []sourceCallable
	operations map[*Function]*Service
	edgesDone  bool
	calls      []lintsdk.Call
	provisions []lintsdk.Provision
	edgesErr   error
}

type sourceCallable struct {
	function *Function
	kind     string
	owner    string
}

func (d *lintFactDeriver) index() {
	d.operations = map[*Function]*Service{}
	for _, service := range d.c.services {
		for _, operation := range service.Methods {
			d.operations[operation] = service
		}
	}
	for _, f := range d.r.Program.Functions {
		if _, checked := d.r.checkedFunctions[f]; checked {
			d.sources = append(d.sources, sourceCallable{function: f, kind: lintsdk.CallableFunction})
		}
	}
	for _, service := range d.r.Program.Services {
		for _, operation := range service.Methods {
			d.sources = append(d.sources, sourceCallable{function: operation, kind: lintsdk.CallableOperation, owner: serviceIdentity(service.Name)})
		}
	}
	for _, provider := range d.r.Program.Providers {
		for _, method := range provider.Methods {
			d.sources = append(d.sources, sourceCallable{function: method, kind: lintsdk.CallableProviderMethod, owner: providerTypeRef(provider).Declaration})
		}
	}
}

func spanFact(span Span) lintsdk.Span {
	return lintsdk.Span{Offset: span.Offset, Length: span.Length, Line: span.Line, Column: span.Column}
}

func sourceSpanFact(span Span) *lintsdk.Span {
	fact := spanFact(span)
	return &fact
}

func (d *lintFactDeriver) failureIdentity(name string) string {
	if slices.Contains(builtinErrors(), name) {
		return builtinFailureIdentity(name)
	}
	return d.c.declarationIdentity("error", "module", name)
}

// rows maps checked row labels to nominal identities. A label is a row
// parameter identity, or a failure (service) declaration admitted by the
// checker; anything else is a broken invariant, not a guess.
func (d *lintFactDeriver) rows(failures, requirements RowID) (lintsdk.Rows, error) {
	return d.labelRows(d.c.retainedRowLabels(failures), d.c.retainedRowLabels(requirements))
}

func (d *lintFactDeriver) labelRows(failures, requirements []string) (lintsdk.Rows, error) {
	var rows lintsdk.Rows
	for _, label := range failures {
		if parameter, ok := d.c.rowDefinitions[label]; ok {
			rows.Parameters = append(rows.Parameters, parameter.ID)
		} else if d.c.errors[label] != nil {
			rows.Failures = append(rows.Failures, d.failureIdentity(label))
		} else {
			return lintsdk.Rows{}, fmt.Errorf("failure row label %s has no admitted declaration", label)
		}
	}
	for _, label := range requirements {
		if parameter, ok := d.c.rowDefinitions[label]; ok {
			rows.Parameters = append(rows.Parameters, parameter.ID)
		} else if d.c.services[label] != nil {
			rows.Requirements = append(rows.Requirements, serviceIdentity(label))
		} else {
			return lintsdk.Rows{}, fmt.Errorf("requirement row label %s has no admitted service", label)
		}
	}
	rows.Failures, rows.Requirements, rows.Parameters = normalized(rows.Failures), normalized(rows.Requirements), normalized(rows.Parameters)
	return rows, nil
}

func (d *lintFactDeriver) declarations(snapshot *lintsdk.Snapshot) error {
	sourceServices := map[*Service]bool{}
	for _, service := range d.r.Program.Services {
		sourceServices[service] = true
	}
	for name, service := range d.c.services {
		fact := lintsdk.Service{Identity: serviceIdentity(name), Name: name, Builtin: !sourceServices[service]}
		if !fact.Builtin {
			fact.Span = sourceSpanFact(service.Span)
		}
		for _, operation := range service.Methods {
			fact.Operations = append(fact.Operations, operation.Identity)
		}
		snapshot.Services = append(snapshot.Services, fact)
	}
	sourceProviders := map[*Provider]bool{}
	for _, provider := range d.r.Program.Providers {
		sourceProviders[provider] = true
	}
	for _, provider := range d.c.providers {
		fact := lintsdk.Provider{Identity: providerTypeRef(provider).Declaration, Name: provider.Name, Service: serviceIdentity(provider.Service), Builtin: !sourceProviders[provider], Configured: len(provider.Params) > 0}
		for _, captured := range normalized(provider.Services) {
			fact.Requires = append(fact.Requires, serviceIdentity(captured))
		}
		if !fact.Builtin {
			fact.Span = sourceSpanFact(provider.Span)
		}
		snapshot.Providers = append(snapshot.Providers, fact)
	}
	for name, declaration := range d.c.errors {
		fact := lintsdk.Failure{Identity: d.failureIdentity(name), Name: name, Builtin: slices.Contains(builtinErrors(), name)}
		if !fact.Builtin {
			fact.Span = sourceSpanFact(declaration.Span)
		}
		snapshot.Failures = append(snapshot.Failures, fact)
	}
	slices.SortFunc(snapshot.Services, func(a, b lintsdk.Service) int { return strings.Compare(a.Identity, b.Identity) })
	slices.SortFunc(snapshot.Providers, func(a, b lintsdk.Provider) int { return strings.Compare(a.Identity, b.Identity) })
	slices.SortFunc(snapshot.Failures, func(a, b lintsdk.Failure) int { return strings.Compare(a.Identity, b.Identity) })
	return nil
}

func (d *lintFactDeriver) callables(snapshot *lintsdk.Snapshot) error {
	for _, source := range d.sources {
		f := source.function
		declared, err := d.rows(f.failureID, f.serviceID)
		if err != nil {
			return err
		}
		fact := lintsdk.Callable{Identity: f.Identity, Name: f.Name, Kind: source.kind, Owner: source.owner, Effect: f.Effect, Span: spanFact(f.Span), Extent: spanFact(f.Extent), Declared: declared}
		if checked, ok := d.r.checkedFunctions[f]; ok {
			body, err := d.rows(checked.body.failureRow(), checked.body.serviceRow())
			if err != nil {
				return err
			}
			fact.Body = &body
		}
		if recorded, ok := d.r.checkedSymbols[f.Identity]; ok && recorded.declaration == f {
			for _, contribution := range recorded.contributions {
				members, err := d.contributionMembers(contribution)
				if err != nil {
					return err
				}
				fact.Contributions = append(fact.Contributions, lintsdk.Contribution{Kind: contribution.Kind, Members: members, Span: spanFact(contribution.Span)})
			}
		}
		snapshot.Callables = append(snapshot.Callables, fact)
	}
	slices.SortStableFunc(snapshot.Callables, func(a, b lintsdk.Callable) int {
		return cmp.Or(cmp.Compare(a.Span.Offset, b.Span.Offset), strings.Compare(a.Identity, b.Identity))
	})
	return nil
}

// contributionMembers maps a contribution's labels by its row kind.
func (d *lintFactDeriver) contributionMembers(contribution Contribution) ([]string, error) {
	var rows lintsdk.Rows
	var err error
	switch contribution.Kind {
	case "failure", "owned-child":
		rows, err = d.labelRows(contribution.Names, nil)
	case "requirement", "layer-construction-input", "layer-provision":
		rows, err = d.labelRows(nil, contribution.Names)
	default:
		return nil, fmt.Errorf("unknown contribution kind %s", contribution.Kind)
	}
	if err != nil {
		return nil, err
	}
	return normalized(append(append(append([]string{}, rows.Failures...), rows.Requirements...), rows.Parameters...)), nil
}

// edges walks every source callable body once, collecting resolved call and
// provision edges in source walk order.
func (d *lintFactDeriver) edges(snapshot *lintsdk.Snapshot, family lintsdk.Family) error {
	if !d.edgesDone {
		d.edgesDone = true
		for _, source := range d.sources {
			d.block(source.function.Body, source.function.Identity)
		}
		slices.SortStableFunc(d.calls, func(a, b lintsdk.Call) int { return cmp.Compare(a.Span.Offset, b.Span.Offset) })
		slices.SortStableFunc(d.provisions, func(a, b lintsdk.Provision) int { return cmp.Compare(a.Span.Offset, b.Span.Offset) })
	}
	if d.edgesErr != nil {
		return d.edgesErr
	}
	if family == lintsdk.FamilyCalls {
		snapshot.Calls = slices.Clone(d.calls)
	} else {
		snapshot.Provisions = slices.Clone(d.provisions)
	}
	return nil
}

func (d *lintFactDeriver) block(b *Block, owner string) {
	if b == nil {
		return
	}
	for _, statement := range b.Statements {
		d.expr(statement.Value, owner)
		d.expr(statement.Payload, owner)
	}
}

func (d *lintFactDeriver) expr(e *Expr, owner string) {
	if e == nil || d.edgesErr != nil {
		return
	}
	switch e.Kind {
	case "call":
		d.call(e, owner)
	case "provide":
		d.provide(e, owner)
	case "provideLayer":
		if plan := e.layerPlan; plan != nil {
			for _, node := range plan.Nodes {
				selection := spanFact(node.SelectionSpan)
				d.provisions = append(d.provisions, lintsdk.Provision{Callable: owner, Kind: lintsdk.ProvisionLayer, Service: node.ServiceIdentity, Provider: node.ImplementationIdentity, Layer: plan.ID, Span: spanFact(e.Span), Selection: &selection})
			}
		} else {
			d.edgesErr = fmt.Errorf("layer provision at offset %d has no checked plan", e.Span.Offset)
		}
	}
	forEachExprChild(e, func(child *Expr) { d.expr(child, owner) })
	d.block(e.Then, owner)
	d.block(e.Else, owner)
	for _, arm := range e.Arms {
		d.block(arm.Body, owner)
	}
}

func (d *lintFactDeriver) call(e *Expr, owner string) {
	call := lintsdk.Call{Caller: owner, Span: spanFact(e.Span)}
	switch e.Text {
	case "data":
		return
	case "callable":
		call.Kind = lintsdk.CallDynamic
	case "fiber":
		call.Kind = lintsdk.CallFiber
	case "foreign":
		binding, ok := d.r.Program.Bindings[e.Name]
		if !ok {
			d.edgesErr = fmt.Errorf("foreign call at offset %d has no checked binding", e.Span.Offset)
			return
		}
		call.Kind, call.Callee = lintsdk.CallForeign, "go:"+binding.Package+"."+binding.member
	case "provider-constructor":
		provider := d.provider(e)
		if provider == "" {
			return
		}
		call.Kind, call.Callee = lintsdk.CallProviderConstructor, provider
	default:
		f := e.ResolvedFunction
		if f == nil {
			return
		}
		call.Kind, call.Callee = lintsdk.CallFunction, f.Identity
		if d.operations[f] != nil {
			call.Kind = lintsdk.CallOperation
		}
	}
	d.calls = append(d.calls, call)
}

func (d *lintFactDeriver) provide(e *Expr, owner string) {
	provider := d.provider(e.Right)
	if provider == "" {
		return
	}
	receiver, err := d.rows(emptyRowID, e.Left.checked.serviceRow())
	if err != nil {
		d.edgesErr = err
		return
	}
	d.provisions = append(d.provisions, lintsdk.Provision{Callable: owner, Kind: lintsdk.ProvisionDirect, Service: serviceIdentity(e.Name), Provider: provider, Span: spanFact(e.Span), Receiver: append(receiver.Requirements, receiver.Parameters...)})
}

// provider resolves a provider value or recipe through its canonical node.
func (d *lintFactDeriver) provider(e *Expr) string {
	node := e.checked.node()
	if node != nil && node.Kind == "providerRecipe" {
		node = d.c.node(node.Result)
	}
	if node == nil || node.Kind != "provider" || node.Declaration == "" {
		d.edgesErr = fmt.Errorf("provider at offset %d has no checked provider identity", e.Span.Offset)
		return ""
	}
	return node.Declaration
}

func (d *lintFactDeriver) bindings(snapshot *lintsdk.Snapshot) error {
	lexical := d.r.lexical
	parent := make([]int, len(lexical.syntax))
	for i := range parent {
		parent[i] = -1
	}
	for id, node := range lexical.syntax {
		for _, child := range node.Children {
			parent[child] = id
		}
	}
	owners := map[int]string{}
	for _, source := range d.sources {
		if id, ok := lexical.functions[source.function]; ok {
			owners[id] = source.function.Identity
		}
	}
	for _, provider := range d.r.Program.Providers {
		for _, parameter := range provider.Params {
			if id, ok := lexical.parameters[parameter.Span.Offset]; ok {
				owners[id] = providerTypeRef(provider).Declaration
			}
		}
	}
	uses := map[string][]lintsdk.Span{}
	for e, id := range lexical.uses {
		uses[id] = append(uses[id], spanFact(e.Span))
	}
	for id, binding := range lexical.bindings {
		owner := ""
		for node := binding.Owner; node >= 0 && owner == ""; node = parent[node] {
			owner = owners[node]
		}
		if owner == "" {
			return fmt.Errorf("binding %s has no source callable", id)
		}
		bindingUses := uses[id]
		slices.SortFunc(bindingUses, func(a, b lintsdk.Span) int { return cmp.Compare(a.Offset, b.Offset) })
		snapshot.Bindings = append(snapshot.Bindings, lintsdk.Binding{Identity: id, Kind: binding.Kind, Name: binding.Name, Callable: owner, Span: spanFact(binding.NameSpan), Effect: binding.Checked.isEffect(), Uses: bindingUses})
	}
	slices.SortFunc(snapshot.Bindings, func(a, b lintsdk.Binding) int {
		return cmp.Or(cmp.Compare(a.Span.Offset, b.Span.Offset), strings.Compare(a.Identity, b.Identity))
	})
	return nil
}

func (d *lintFactDeriver) imports(snapshot *lintsdk.Snapshot) {
	for _, imported := range d.r.Program.Imports {
		snapshot.Imports = append(snapshot.Imports, lintsdk.Import{Alias: imported.Alias, Path: imported.Path, Used: d.r.Program.UsedImports[imported.Alias], Span: spanFact(imported.Span)})
	}
}
