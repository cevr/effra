package compiler

import (
	"errors"
	"fmt"
	"slices"
)

// TypeQuerySchemaVersion 2 adds the selected declaration target and the
// shared plaintext presentation to every selected-type response.
const TypeQuerySchemaVersion = 2

// Selection refusals that adapters classify. Their text is the public message.
var (
	ErrUncheckedSource = errors.New("type queries require checked source")
	ErrNoSelection     = errors.New("no checked expression or lexical declaration at byte offset")
	ErrToolingLimits   = errors.New("original syntax or lexical facts exceed tooling limits")
)

const projectionUnavailable = "selected type projection unavailable: "

// TypeSelection names exactly one selected fact family. Definition identities
// must be expanded against the revision that originally published them.
type TypeSelection struct {
	Symbol           string
	Offset           *int
	Definition       string
	ExpectedRevision string
}

type BindingInfo struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	DeclarationSpan   Span   `json:"declarationSpan"`
	DeclarationExtent Span   `json:"declarationExtent"`
}

// DeclarationTarget is the declaration a selected name denotes. Span and
// Extent are coordinates in Source. LocationAvailable is true only when that
// source is this snapshot's own text; bundled and builtin declarations keep
// their identity without a location in the selected document.
type DeclarationTarget struct {
	Kind              string             `json:"kind"`
	Name              string             `json:"name"`
	Owner             string             `json:"owner,omitempty"`
	Identity          string             `json:"identity,omitempty"`
	Module            string             `json:"module,omitempty"`
	Source            string             `json:"source,omitempty"`
	LocationAvailable bool               `json:"locationAvailable"`
	Span              Span               `json:"span"`
	Extent            Span               `json:"extent"`
	Callable          *DeclaredSignature `json:"callable,omitempty"`
	Type              *TypeRef           `json:"type,omitempty"`
}

// DeclaredSignature is a function, operation or method's own declared
// signature by canonical references. Unlike CallableType it names no callable
// contract node, which a declaration never used as a value does not have.
type DeclaredSignature struct {
	Kind           string                  `json:"kind"`
	TypeParameters []TemplateParameterView `json:"typeParameters,omitempty"`
	RowParameters  []RowParameter          `json:"rowParameters,omitempty"`
	Parameters     []Param                 `json:"parameters"`
	Result         TypeRef                 `json:"result"`
	FailureRow     string                  `json:"failureRow,omitempty"`
	ServiceRow     string                  `json:"serviceRow,omitempty"`
}

type SelectedType struct {
	Kind              string             `json:"kind"`
	LocationAvailable bool               `json:"locationAvailable"`
	Span              Span               `json:"span"`
	Extent            Span               `json:"extent"`
	Binding           *BindingInfo       `json:"binding,omitempty"`
	Expression        *ExpressionInfo    `json:"expression,omitempty"`
	Symbol            *Symbol            `json:"symbol,omitempty"`
	Declaration       *Declaration       `json:"declaration,omitempty"`
	Definition        string             `json:"definition,omitempty"`
	Target            *DeclarationTarget `json:"target,omitempty"`
	Presentation      string             `json:"presentation"`
}

func within(span Span, offset int) bool {
	return span.Length > 0 && offset >= span.Offset && offset-span.Offset < span.Length
}

func bindingInfo(binding lexicalBinding) *BindingInfo {
	return &BindingInfo{ID: binding.ID, Kind: binding.Kind, Name: binding.Name, DeclarationSpan: binding.NameSpan, DeclarationExtent: binding.Extent}
}

func bindingTarget(binding lexicalBinding) *DeclarationTarget {
	return &DeclarationTarget{Kind: binding.Kind, Name: binding.Name, Identity: binding.ID, Source: userSourceID, LocationAvailable: true, Span: binding.NameSpan, Extent: binding.Extent}
}

const userSourceID = "source:user"

// TypeQuery is one validated selected-type answer: the complete response
// CLI and MCP encode, and the same selection editor adapters read typed.
type TypeQuery struct {
	Selection SelectedType
	Response  map[string]any
}

// SelectType answers CLI and MCP selected-type requests.
func (r *Result) SelectType(request TypeSelection) (map[string]any, error) {
	query, err := r.QueryType(request)
	return query.Response, err
}

// QueryType is the shared selected-query owner. It projects retained numeric
// checker facts, not display strings or a reconstructed lexical environment,
// and admits a selection only when its complete response is admitted.
func (r *Result) QueryType(request TypeSelection) (TypeQuery, error) {
	if !r.Checked || r.projector == nil {
		return TypeQuery{}, ErrUncheckedSource
	}
	selectors := 0
	if request.Symbol != "" {
		selectors++
	}
	if request.Offset != nil {
		selectors++
	}
	if request.Definition != "" {
		selectors++
	}
	if selectors != 1 {
		return TypeQuery{}, fmt.Errorf("select exactly one symbol, byte offset or type definition")
	}
	if request.ExpectedRevision != "" && request.ExpectedRevision != r.Revision {
		return TypeQuery{}, fmt.Errorf("stale semantic revision")
	}
	selected := SelectedType{}
	switch {
	case request.Definition != "":
		if request.ExpectedRevision == "" {
			return TypeQuery{}, fmt.Errorf("type definition lookup requires expectedRevision")
		}
		if _, exists := r.canonical.publicToID[request.Definition]; !exists {
			return TypeQuery{}, fmt.Errorf("type definition unavailable in this snapshot")
		}
		selected.Kind, selected.Definition = "typeDefinition", request.Definition
	case request.Symbol != "":
		if symbol := r.Find(request.Symbol); symbol != nil {
			r.selectSymbol(symbol, &selected)
		} else if declaration := r.FindDeclaration(request.Symbol); declaration != nil {
			r.selectDeclaration(declaration, &selected)
		} else {
			return TypeQuery{}, fmt.Errorf("named type declaration unavailable")
		}
	case request.Offset != nil:
		if err := r.selectOffset(*request.Offset, &selected); err != nil {
			return TypeQuery{}, err
		}
	}
	projection := r.projectSelection(&selected)
	if !projection.Complete {
		return TypeQuery{}, fmt.Errorf("%s%s", projectionUnavailable, projection.Error)
	}
	selected.Presentation = presentSelection(&selected, projection)
	declarations := r.ProjectionDeclarations(projection)
	response := map[string]any{"schemaVersion": r.SchemaVersion, "querySchemaVersion": TypeQuerySchemaVersion, "revision": r.Revision, "target": r.Target, "checked": true, "selection": selected, "types": projection.Types, "rows": projection.Rows, "declarations": declarations, "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": true}
	r.AddSourceInputs(response)
	if _, err := r.ValidateProjectionResponse(projection, response); err != nil {
		return TypeQuery{}, err
	}
	return TypeQuery{Selection: selected, Response: response}, nil
}

func (r *Result) selectSymbol(symbol *Symbol, selected *SelectedType) {
	selected.Kind, selected.Symbol, selected.Span = "declaration", symbol, symbol.Span
	f := r.checkedSymbols[symbol.Identity].declaration
	if f == nil {
		return
	}
	selected.Target, _ = r.declarationTarget(lexicalTarget{kind: "function", function: f})
	if r.lexical != nil {
		if _, original := r.lexical.functions[f]; original {
			selected.Extent, selected.LocationAvailable = f.Extent, true
		}
	}
}

func (r *Result) selectDeclaration(declaration *Declaration, selected *SelectedType) {
	selected.Kind, selected.Declaration, selected.Span = "declaration", declaration, declaration.Span
	if r.lexical == nil {
		return
	}
	// Local generic data carries the user source ID; only another source is
	// a bundled declaration without a location in this snapshot.
	if declaration.Source != "" && declaration.Source != userSourceID {
		for _, template := range r.Program.BundledTemplates {
			if template.Identity == declaration.Identity {
				selected.Target, _ = r.declarationTarget(lexicalTarget{kind: template.Kind, data: template})
			}
		}
		return
	}
	for _, item := range r.Program.Items {
		data := item.Record
		if data == nil {
			data = item.Enum
		}
		switch {
		case data != nil && data.Span == declaration.Span && data.Name == declaration.Name:
			selected.Target, _ = r.declarationTarget(lexicalTarget{kind: data.Kind, data: data})
		case item.Error != nil && item.Error.Span == declaration.Span && item.Error.Name == declaration.Name:
			selected.Target, _ = r.declarationTarget(lexicalTarget{kind: "error", failure: item.Error})
		default:
			continue
		}
		selected.Extent, selected.LocationAvailable = item.Extent, true
		return
	}
}

// selectOffset prefers, in order: an original declaration name, a binding
// name, a checker-resolved reference token, then the smallest checked
// expression. Only the token that names a declaration carries a target.
func (r *Result) selectOffset(offset int, selected *SelectedType) error {
	if offset < 0 {
		return fmt.Errorf("offset must be non-negative")
	}
	facts := r.lexical
	if facts == nil || !facts.complete {
		return ErrToolingLimits
	}
	for _, name := range facts.declarations {
		if !within(name.Span, offset) {
			continue
		}
		if f := name.Target.function; name.Target.kind == "function" {
			if symbol := r.Find(f.Name); symbol != nil && r.checkedSymbols[symbol.Identity].declaration == f {
				r.selectSymbol(symbol, selected)
				return nil
			}
		}
		target, declaration := r.declarationTarget(name.Target)
		if target == nil {
			break
		}
		selected.Kind, selected.Span, selected.Extent, selected.LocationAvailable = "declaration", name.Span, target.Extent, true
		selected.Target, selected.Declaration = target, declaration
		return nil
	}
	var value checkedExpression
	for _, binding := range facts.bindings {
		if within(binding.NameSpan, offset) {
			selected.Kind, selected.Span, selected.Extent, selected.Binding = "bindingDeclaration", binding.NameSpan, binding.Extent, bindingInfo(binding)
			selected.Target = bindingTarget(binding)
			value = binding.Checked
			break
		}
	}
	if selected.Binding == nil {
		var reference *lexicalName
		for _, name := range facts.references {
			if within(name.Span, offset) {
				reference = &name
				break
			}
		}
		found, foundID := r.smallestCheckedExpression(offset)
		anchored := found != nil && reference != nil && (facts.syntax[foundID].Anchor == reference.Span || facts.syntax[foundID].NameSpan == reference.Span)
		if reference != nil && !anchored {
			// The token names a declaration but no checked expression is
			// anchored at it, such as a qualifier, pattern segment or label.
			target, declaration := r.declarationTarget(reference.Target)
			if target != nil {
				selected.Kind, selected.Span, selected.Extent, selected.LocationAvailable = "reference", reference.Span, reference.Span, true
				selected.Target, selected.Declaration = target, declaration
				return nil
			}
		}
		if found == nil {
			return ErrNoSelection
		}
		original, expression := facts.syntax[foundID], r.facts[found]
		selected.Kind, selected.Span, selected.Extent = "expression", original.Anchor, original.Extent
		value = expression.Checked
		if id := facts.uses[found]; id != "" && within(original.NameSpan, offset) {
			selected.Kind, selected.Binding = "bindingUse", bindingInfo(facts.bindings[id])
			selected.Target = bindingTarget(facts.bindings[id])
		} else if reference != nil {
			selected.Target, selected.Declaration = r.declarationTarget(reference.Target)
		}
		selected.Expression = &ExpressionInfo{Kind: original.Kind, Span: original.Anchor, ExecutedFailures: append([]string{}, expression.Executed.Failures...), ExecutedRequirements: append([]string{}, expression.Executed.Requirements...), Evaluation: expression.Evaluation}
	}
	typeOf := r.projector.projectChecked(value)
	selected.LocationAvailable = true
	if typeOf.ProjectionError != "" {
		return fmt.Errorf("%s%s", projectionUnavailable, typeOf.ProjectionError)
	}
	if selected.Expression == nil {
		executed := r.projector.projectEvaluation(value.executed)
		selected.Expression = &ExpressionInfo{Kind: selected.Kind, Span: selected.Span, ExecutedFailures: append([]string{}, executed.Failures...), ExecutedRequirements: append([]string{}, executed.Requirements...), Evaluation: r.projector.projectEvaluation(value.evaluation)}
	}
	selected.Expression.Type = typeOf
	return nil
}

func (r *Result) smallestCheckedExpression(offset int) (*Expr, int) {
	var found *Expr
	foundID := -1
	for e, id := range r.lexical.expressions {
		original := r.lexical.syntax[id]
		if _, checked := r.facts[e]; !checked || !within(original.Extent, offset) {
			continue
		}
		if found == nil || original.Extent.Length < r.lexical.syntax[foundID].Extent.Length || (original.Extent.Length == r.lexical.syntax[foundID].Extent.Length && id > foundID) {
			found, foundID = e, id
		}
	}
	return found, foundID
}

// declarationTarget publishes one retained declaration pointer. Nominal
// targets also return their owning declaration view, so variants and fields
// project the same declaration closure as a named declaration query.
func (r *Result) declarationTarget(t lexicalTarget) (*DeclarationTarget, *Declaration) {
	c, facts := r.projector, r.lexical
	if c == nil || facts == nil {
		return nil, nil
	}
	target := &DeclarationTarget{Kind: t.kind}
	located := func(original bool, source, module string, span, extent Span) {
		if original {
			target.Source, target.LocationAvailable = userSourceID, true
		} else if source != "" && source != userSourceID {
			target.Source, target.Module = source, module
		} else {
			// Builtin declarations have no source text in any snapshot.
			return
		}
		target.Span, target.Extent = span, extent
	}
	var declaration *Declaration
	switch {
	case t.function != nil:
		f := t.function
		target.Name, target.Identity, target.Callable = f.Name, f.Identity, c.declaredSignature(f)
		if t.service != nil {
			target.Owner = t.service.Name
		} else if t.provider != nil {
			target.Owner = t.provider.Name
		}
		_, original := facts.functions[f]
		located(original, f.SourceID, f.Module, f.Span, f.Extent)
	case t.provider != nil:
		p := t.provider
		target.Name, target.Owner, target.Identity = p.Name, p.Service, providerTypeRef(p).Declaration
		if root, checked := r.checkedProviderRoots[p]; checked {
			ref := c.identityRef(root.contractID())
			target.Type = &ref
		}
		item := facts.items[p]
		located(item != nil, "", "", p.Span, itemExtent(item, p.Span))
	case t.service != nil:
		item := facts.items[t.service]
		target.Name = t.service.Name
		located(item != nil, "", "", t.service.Span, itemExtent(item, t.service.Span))
	case t.data != nil || t.failure != nil:
		var fields []Field
		var variants []Variant
		var span Span
		source, module := "", ""
		var item *SyntaxItem
		if d := t.data; d != nil {
			target.Name, fields, variants, span, item = d.Name, d.Fields, d.Variants, d.Span, facts.items[d]
			source, module = d.SourceID, d.Module
			identity := d.Identity
			if len(d.Parameters) == 0 {
				identity = c.declarationIdentity(d.Kind, "module", d.Name)
			}
			declaration = r.declarationByIdentity(identity)
		} else {
			target.Name, fields, span, item = t.failure.Name, t.failure.Fields, t.failure.Span, facts.items[t.failure]
			declaration = r.declarationByIdentity(c.declarationIdentity("error", "module", t.failure.Name))
		}
		if declaration != nil && t.variant == "" && t.field == "" {
			target.Identity = declaration.Identity
		}
		extent := itemExtent(item, span)
		if t.variant != "" {
			index := slices.IndexFunc(variants, func(v Variant) bool { return v.Name == t.variant })
			if index < 0 {
				return nil, nil
			}
			target.Owner, target.Name, fields, span, extent = target.Name, t.variant, variants[index].Fields, variants[index].Span, variants[index].Span
		}
		if t.field != "" {
			index := slices.IndexFunc(fields, func(f Field) bool { return f.Name == t.field })
			if index < 0 {
				return nil, nil
			}
			if target.Owner != "" {
				target.Owner += "." + target.Name
			} else {
				target.Owner = target.Name
			}
			target.Name, span, extent = t.field, fields[index].Span, fields[index].Span
			if ref := declaredFieldType(declaration, t.variant, t.field); ref != nil {
				target.Type = ref
			}
		}
		located(item != nil, source, module, span, extent)
	case t.module != nil:
		if imported := t.module.BundledImport; imported != nil {
			target.Name, target.Module, target.Span = imported.Alias, imported.Path, imported.Span
		} else {
			target.Name, target.Module, target.Span = t.module.Import.Alias, t.module.Import.Path, t.module.Import.Span
		}
		target.Source, target.LocationAvailable, target.Extent = userSourceID, true, t.module.Extent
	case t.layer != nil:
		item := facts.items[t.layer]
		target.Name = t.layer.Name
		located(item != nil, "", "", t.layer.Span, itemExtent(item, t.layer.Span))
	default:
		return nil, nil
	}
	return target, declaration
}

func itemExtent(item *SyntaxItem, fallback Span) Span {
	if item == nil {
		return fallback
	}
	return item.Extent
}

func (r *Result) declarationByIdentity(identity string) *Declaration {
	for i := range r.Declarations {
		if r.Declarations[i].Identity == identity {
			return &r.Declarations[i]
		}
	}
	return nil
}

// declaredFieldType reads the canonical field reference from the published
// declaration view rather than from the field's source spelling.
func declaredFieldType(declaration *Declaration, variant, field string) *TypeRef {
	if declaration == nil {
		return nil
	}
	fields := declaration.Fields
	if variant != "" {
		index := slices.IndexFunc(declaration.Variants, func(v Variant) bool { return v.Name == variant })
		if index < 0 {
			return nil
		}
		fields = declaration.Variants[index].Fields
	}
	index := slices.IndexFunc(fields, func(f Field) bool { return f.Name == field })
	if index < 0 || fields[index].TypeRef.ID == "" {
		return nil
	}
	ref := fields[index].TypeRef
	return &ref
}

// declaredSignature reads retained numeric signature facts only; it never
// interns a callable contract after publication.
func (c *checker) declaredSignature(f *Function) *DeclaredSignature {
	view := callableIdentity(c, f)
	signature := &DeclaredSignature{Kind: view.Kind, TypeParameters: view.TypeParameters, RowParameters: view.RowParameters, Parameters: view.Parameters, Result: view.Result}
	if f.Effect && f.signatureChecked {
		signature.FailureRow, signature.ServiceRow = c.rowNodeID(f.failureID), c.rowNodeID(f.serviceID)
	}
	return signature
}

// projectSelection admits the union of every root a selection publishes in
// one bounded, response-local closure.
func (r *Result) projectSelection(selected *SelectedType) TypeProjection {
	limits := r.projectionLimits()
	refs := []TypeRef{}
	if selected.Definition != "" {
		refs = append(refs, TypeRef{ID: selected.Definition})
	}
	if selected.Symbol != nil {
		if err := r.appendSymbolRefs(&refs, selected.Symbol); err != nil {
			return refusedProjection(limits, ProjectionUsage{}, err.Error())
		}
	}
	if selected.Expression != nil {
		if selected.Expression.Type.ProjectionError != "" {
			return refusedProjection(limits, ProjectionUsage{}, selected.Expression.Type.ProjectionError)
		}
		appendExpressionRefs(&refs, selected.Expression)
	}
	if selected.Declaration != nil {
		appendDeclarationRefs(&refs, selected.Declaration)
	}
	if target := selected.Target; target != nil {
		if target.Type != nil {
			appendProjectionRef(&refs, *target.Type)
		}
		if callable := target.Callable; callable != nil {
			for _, parameter := range callable.TypeParameters {
				appendProjectionRef(&refs, parameter.Variable)
				if parameter.Shape != nil {
					appendProjectionRef(&refs, *parameter.Shape)
				}
			}
			for _, parameter := range callable.Parameters {
				appendProjectionRef(&refs, parameter.TypeRef)
			}
			appendProjectionRef(&refs, callable.Result)
			appendProjectionRef(&refs, TypeRef{FailureRow: callable.FailureRow, ServiceRow: callable.ServiceRow})
		}
	}
	compatibilityBytes, err := encodedSize(selected, limits.CompatibilityBytes)
	if err != nil {
		return refusedProjection(limits, ProjectionUsage{CompatibilityBytes: compatibilityBytes}, err.Error())
	}
	return r.projectionRefs(refs, false, compatibilityBytes, stringBytes(selected, limits.NameBytes))
}
