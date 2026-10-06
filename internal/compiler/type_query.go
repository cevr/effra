package compiler

import "fmt"

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

type SelectedType struct {
	Kind              string          `json:"kind"`
	LocationAvailable bool            `json:"locationAvailable"`
	Span              Span            `json:"span"`
	Extent            Span            `json:"extent"`
	Binding           *BindingInfo    `json:"binding,omitempty"`
	Expression        *ExpressionInfo `json:"expression,omitempty"`
	Symbol            *Symbol         `json:"symbol,omitempty"`
	Declaration       *Declaration    `json:"declaration,omitempty"`
	Definition        string          `json:"definition,omitempty"`
}

func within(span Span, offset int) bool {
	return span.Length > 0 && offset >= span.Offset && offset-span.Offset < span.Length
}

func bindingInfo(binding lexicalBinding) *BindingInfo {
	return &BindingInfo{ID: binding.ID, Kind: binding.Kind, Name: binding.Name, DeclarationSpan: binding.NameSpan, DeclarationExtent: binding.Extent}
}

// SelectType is the shared selected-query owner. It projects retained numeric
// checker facts, not display strings or a reconstructed lexical environment.
func (r *Result) SelectType(request TypeSelection) (map[string]any, error) {
	if !r.Checked || r.projector == nil {
		return nil, fmt.Errorf("type queries require checked source")
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
		return nil, fmt.Errorf("select exactly one symbol, byte offset or type definition")
	}
	if request.ExpectedRevision != "" && request.ExpectedRevision != r.Revision {
		return nil, fmt.Errorf("stale semantic revision")
	}
	selected := SelectedType{}
	var projection TypeProjection
	switch {
	case request.Definition != "":
		if request.ExpectedRevision == "" {
			return nil, fmt.Errorf("type definition lookup requires expectedRevision")
		}
		if _, exists := r.canonical.publicToID[request.Definition]; !exists {
			return nil, fmt.Errorf("type definition unavailable in this snapshot")
		}
		selected.Kind, selected.Definition = "typeDefinition", request.Definition
		projection = r.projectionRefs([]TypeRef{{ID: request.Definition}}, false, 0, len(request.Definition))
	case request.Symbol != "":
		selected.Kind = "declaration"
		if symbol := r.Find(request.Symbol); symbol != nil {
			projection = r.ProjectSymbol(symbol)
			selected.Symbol, selected.Span = symbol, symbol.Span
			if r.lexical != nil {
				f := r.checkedSymbols[symbol.Identity].declaration
				if _, original := r.lexical.functions[f]; original {
					selected.Extent, selected.LocationAvailable = f.Extent, true
				}
			}
		} else if declaration := r.FindDeclaration(request.Symbol); declaration != nil {
			selected.Declaration, selected.Span = declaration, declaration.Span
			for _, item := range r.Program.Items {
				local := (item.Record != nil && item.Record.Span == declaration.Span && item.Record.Name == declaration.Name) || (item.Enum != nil && item.Enum.Span == declaration.Span && item.Enum.Name == declaration.Name) || (item.Error != nil && item.Error.Span == declaration.Span && item.Error.Name == declaration.Name)
				if local && declaration.Source == "" {
					selected.Extent, selected.LocationAvailable = item.Extent, true
					break
				}
			}
			projection = r.ProjectDeclaration(declaration)
		} else {
			return nil, fmt.Errorf("named type declaration unavailable")
		}
	case request.Offset != nil:
		if *request.Offset < 0 {
			return nil, fmt.Errorf("offset must be non-negative")
		}
		if r.lexical == nil || !r.lexical.complete {
			return nil, fmt.Errorf("original syntax or lexical facts exceed tooling limits")
		}
		offset := *request.Offset
		for f := range r.lexical.functions {
			if within(f.Span, offset) {
				if symbol := r.Find(f.Name); symbol != nil && r.checkedSymbols[symbol.Identity].declaration == f {
					return r.SelectType(TypeSelection{Symbol: f.Name, ExpectedRevision: request.ExpectedRevision})
				}
			}
		}
		var value checkedExpression
		for _, binding := range r.lexical.bindings {
			if within(binding.NameSpan, offset) {
				selected.Kind, selected.Span, selected.Extent, selected.Binding = "bindingDeclaration", binding.NameSpan, binding.Extent, bindingInfo(binding)
				value = binding.Checked
				break
			}
		}
		var found *Expr
		foundID := -1
		if selected.Binding == nil {
			for e, id := range r.lexical.expressions {
				original := r.lexical.syntax[id]
				if _, checked := r.facts[e]; !checked || !within(original.Extent, offset) {
					continue
				}
				if found == nil || original.Extent.Length < r.lexical.syntax[foundID].Extent.Length || (original.Extent.Length == r.lexical.syntax[foundID].Extent.Length && id > foundID) {
					found, foundID = e, id
				}
			}
			if found == nil {
				return nil, fmt.Errorf("no checked expression or lexical declaration at byte offset")
			}
			original, facts := r.lexical.syntax[foundID], r.facts[found]
			selected.Kind, selected.Span, selected.Extent = "expression", original.Anchor, original.Extent
			value = facts.Checked
			if id := r.lexical.uses[found]; id != "" && within(original.NameSpan, offset) {
				selected.Kind, selected.Binding = "bindingUse", bindingInfo(r.lexical.bindings[id])
			}
			selected.Expression = &ExpressionInfo{Kind: original.Kind, Span: original.Anchor, ExecutedFailures: append([]string{}, facts.Executed.Failures...), ExecutedRequirements: append([]string{}, facts.Executed.Requirements...), Evaluation: facts.Evaluation}
		}
		typeOf := r.projector.projectChecked(value)
		selected.LocationAvailable = true
		if typeOf.ProjectionError != "" {
			return nil, fmt.Errorf("selected type projection unavailable: %s", typeOf.ProjectionError)
		}
		if selected.Expression == nil {
			executed := r.projector.projectEvaluation(value.executed)
			selected.Expression = &ExpressionInfo{Kind: selected.Kind, Span: selected.Span, ExecutedFailures: append([]string{}, executed.Failures...), ExecutedRequirements: append([]string{}, executed.Requirements...), Evaluation: r.projector.projectEvaluation(value.evaluation)}
		}
		selected.Expression.Type = typeOf
		projection = r.ProjectExpression(selected.Expression)
	}
	if !projection.Complete {
		return nil, fmt.Errorf("selected type projection unavailable: %s", projection.Error)
	}
	response := map[string]any{"schemaVersion": r.SchemaVersion, "querySchemaVersion": 1, "revision": r.Revision, "target": r.Target, "checked": true, "selection": selected, "types": projection.Types, "rows": projection.Rows, "declarations": r.ProjectionDeclarations(projection), "typeProjectionLimits": projection.Limits, "typeProjectionUsage": projection.Usage, "typeProjectionComplete": true}
	r.AddSourceInputs(response)
	if _, err := r.ValidateProjectionResponse(projection, response); err != nil {
		return nil, err
	}
	return response, nil
}
