package compiler

import "strings"

func cloneFieldOccurrences(fields map[string]checkedExpression) map[string]checkedExpression {
	if fields == nil {
		return nil
	}
	copy := make(map[string]checkedExpression, len(fields))
	for name, value := range fields {
		// Field tables are immutable after construction. Copy the table without
		// expanding shared nested occurrences into an exponential tree.
		copy[name] = value
	}
	return copy
}

func (c *checker) parameterFields(f *Function, p Param) map[string]checkedExpression {
	layouts := map[TypeID]bool{}
	if !c.callableFieldLayout(p.typeID, layouts, map[TypeID]bool{}, 0) {
		return nil
	}
	nodes := 0
	return c.parameterFieldOccurrences(f, p.Name, "", p.typeID, map[TypeID]bool{}, 0, &nodes, layouts)
}

// Value-only shared layouts need no field evidence. Memoize canonical nodes
// before constructing declaration paths so a shared data DAG stays shared.
func (c *checker) callableFieldLayout(id TypeID, memo map[TypeID]bool, visiting map[TypeID]bool, depth int) bool {
	if value, found := memo[id]; found {
		return value
	}
	n := c.node(id)
	if n == nil {
		return false
	}
	if n.Kind == "callable" {
		memo[id] = true
		return true
	}
	if n.Kind == "type-variable" {
		// An unresolved declaration variable is not a proof of an empty layout.
		memo[id] = true
		return true
	}
	if depth > 64 || len(memo) > 4096 || visiting[id] {
		return true
	}
	visiting[id] = true
	defer delete(visiting, id)
	value := false
	c.walkDataFields(id, func(_ string, field Field) {
		value = c.callableFieldLayout(field.typeID, memo, visiting, depth+1) || value
	})
	memo[id] = value
	return value
}

func (c *checker) checkedFields(id TypeID) ([]Field, bool) {
	if fields, ok := c.applicationFields(id); ok {
		return fields, true
	}
	n := c.node(id)
	if n == nil || n.Kind != "record" {
		return nil, false
	}
	r := c.records[n.Name]
	if r == nil || n.Declaration != c.declarationQualifier("record", r.Name) {
		return nil, false
	}
	return r.Fields, true
}

// occurrenceLayoutID identifies the data value whose fields are retained on
// an expression occurrence. A callable/recipe occurrence keeps its own
// execution contract while carrying field evidence for the value it returns.
// Summary transport must validate that evidence against the returned layout,
// not against the callable contract node itself.
func (c *checker) occurrenceLayoutID(id TypeID) TypeID {
	n := c.node(id)
	if n == nil {
		return invalidTypeID
	}
	switch n.Kind {
	case "callable", "recipe", "providerRecipe":
		return n.Result
	default:
		return id
	}
}

func (c *checker) checkedVariants(id TypeID) (*Enum, []Variant, bool) {
	n := c.node(id)
	if n == nil {
		return nil, nil, false
	}
	if n.Kind == "application" {
		owner := c.templates[n.Declaration]
		variants, ok := c.applicationVariants(id)
		return owner, variants, ok
	}
	if n.Kind != "enum" {
		return nil, nil, false
	}
	owner := c.enums[n.Name]
	if owner == nil || n.Declaration != c.declarationQualifier("enum", owner.Name) {
		return nil, nil, false
	}
	return owner, owner.Variants, true
}

// The canonical data layout is shared by evidence and ownership traversal.
// Enum paths include the variant, distinguishing absent from present payloads.
func (c *checker) walkDataFields(id TypeID, visit func(string, Field)) bool {
	if fields, ok := c.checkedFields(id); ok {
		for _, field := range fields {
			visit(field.Name, field)
		}
		return true
	}
	if _, variants, ok := c.checkedVariants(id); ok {
		for _, variant := range variants {
			for _, field := range variant.Fields {
				visit(variant.Name+"."+field.Name, field)
			}
		}
		return true
	}
	return false
}

func (c *checker) parameterFieldOccurrences(f *Function, parameter, path string, id TypeID, visiting map[TypeID]bool, depth int, nodes *int, layouts map[TypeID]bool) map[string]checkedExpression {
	if !layouts[id] {
		return nil
	}
	if depth > 32 || visiting[id] {
		c.diagnostic("EF127", "recursive or excessive field occurrence layout", f.Span)
		return nil
	}
	visiting[id] = true
	defer delete(visiting, id)
	if _, variants, ok := c.checkedVariants(id); ok {
		values := map[string]checkedExpression{}
		for _, variant := range variants {
			*nodes++
			if *nodes > 4096 {
				c.diagnostic("EF127", "field occurrence layout exceeds budget", f.Span)
				return nil
			}
			variantPath := variant.Name
			if path != "" {
				variantPath = path + "." + variantPath
			}
			payload := c.checkedData("()")
			payload.fields = c.parameterPayloadFields(f, parameter, variantPath, variant.Fields, visiting, depth, nodes, layouts)
			values[variant.Name] = payload
		}
		return values
	}
	fields, ok := c.checkedFields(id)
	if !ok {
		if n := c.node(id); n != nil && n.Kind == "type-variable" {
			c.diagnostic("EF127", "field layout of an unresolved type parameter is unavailable", f.Span)
		}
		return nil
	}
	return c.parameterPayloadFields(f, parameter, path, fields, visiting, depth, nodes, layouts)
}

func (c *checker) parameterPayloadFields(f *Function, parameter, path string, fields []Field, visiting map[TypeID]bool, depth int, nodes *int, layouts map[TypeID]bool) map[string]checkedExpression {
	values := map[string]checkedExpression{}
	for _, field := range fields {
		*nodes++
		if *nodes > 4096 {
			c.diagnostic("EF127", "field occurrence layout exceeds budget", f.Span)
			return nil
		}
		fieldPath := field.Name
		if path != "" {
			fieldPath = path + "." + field.Name
		}
		value := c.checkedDataID(field.typeID, nil, nil)
		if c.node(field.typeID).Kind == "callable" {
			value.callableEvidence = callableEvidence{parameter: f, parameterName: parameter, parameterPath: fieldPath}
		}
		value.fields = c.parameterFieldOccurrences(f, parameter, fieldPath, field.typeID, visiting, depth+1, nodes, layouts)
		values[field.Name] = value
	}
	return values
}

func (c *checker) joinExpressionFields(a, b checkedExpression, span Span, depth int, nodes *int) map[string]checkedExpression {
	if _, _, enum := c.checkedVariants(a.valueID()); !enum {
		return c.joinFieldOccurrences(a.fields, b.fields, span, depth, nodes)
	}
	if depth > 32 {
		c.diagnostic("EF127", "excessive variant occurrence join", span)
		return nil
	}
	// Missing enum evidence can denote any alternative. It must not be
	// replaced by the narrower initialized alternatives from the other branch.
	if a.fields == nil || b.fields == nil {
		return nil
	}
	joined := cloneFieldOccurrences(a.fields)
	if joined == nil {
		joined = map[string]checkedExpression{}
	}
	for name, right := range b.fields {
		*nodes++
		if *nodes > 4096 {
			c.diagnostic("EF127", "variant occurrence join exceeds budget", span)
			return nil
		}
		if left, exists := joined[name]; exists {
			left.fields = c.joinFieldOccurrences(left.fields, right.fields, span, depth+1, nodes)
			joined[name] = left
		} else {
			joined[name] = right
		}
	}
	return joined
}

func (c *checker) projectFieldOccurrence(inner checkedExpression, field Field) checkedExpression {
	value, known := inner.fields[field.Name]
	if known {
		value = value.clone()
	} else {
		value = c.checkedDataID(field.typeID, nil, nil)
		if c.node(field.typeID).Kind == "callable" {
			value.callableEvidence = callableEvidence{unresolved: true}
		}
	}
	value.setOwnership(projectFacts(inner.ownershipFacts(), field.Name))
	value.setCaptures(projectFacts(inner.captureFacts(), field.Name))
	// A public product annotation exposes its declared field contract even when
	// the retained supplied declaration has narrower rows.
	value.value = c.values.occurrence(field.typeID, value.ownershipFacts(), value.captureFacts())
	value.evaluation = c.evaluation(emptyRowID, emptyRowID)
	value.executed = value.evaluation
	return value
}

func (c *checker) instantiateFieldOccurrences(fields map[string]checkedExpression, f *Function, args []checkedExpression, types map[TypeID]TypeID, rows map[string][]string, depth int, budgets ...*int) map[string]checkedExpression {
	if fields == nil {
		return nil
	}
	if depth > 32 {
		c.diagnostic("EF127", "field occurrence substitution exceeds budget", f.Span)
		return nil
	}
	values := map[string]checkedExpression{}
	nodes := new(int)
	if len(budgets) > 0 {
		nodes = budgets[0]
	}
	for name, value := range fields {
		*nodes++
		if *nodes > 4096 {
			c.diagnostic("EF127", "field occurrence substitution exceeds budget", f.Span)
			return nil
		}
		if value.callableEvidence.parameter == f {
			for i, p := range f.Params {
				if p.Name == value.callableEvidence.parameterName && i < len(args) {
					if actual, known := fieldOccurrence(args[i], value.callableEvidence.parameterPath); known {
						evaluation, executed := value.evaluation, value.executed
						value = actual.clone()
						value.evaluation, value.executed = evaluation, executed
					}
					break
				}
			}
		}
		id := c.substituteCanonical(value.contractID(), types, rows)
		if id == invalidTypeID {
			c.diagnostic("EF127", "field contract substitution unavailable", f.Span)
			continue
		}
		value.value = c.values.occurrence(id, c.instantiateCallbackFacts(value.ownershipFacts(), f, args, 0), c.instantiateCallbackFacts(value.captureFacts(), f, args, 0))
		value.callableEvidence = substituteCallableEvidence(value.callableEvidence, f, args)
		value.evaluation = c.evaluation(c.instantiateRow(value.evaluation.failureRowID(), rows), c.instantiateRow(value.evaluation.serviceRowID(), rows))
		value.executed = c.evaluation(c.instantiateRow(value.executed.failureRowID(), rows), c.instantiateRow(value.executed.serviceRowID(), rows))
		value.child = c.instantiateCallbackFacts(value.child, f, args, 0)
		value.fields = c.instantiateFieldOccurrences(value.fields, f, args, types, rows, depth+1, nodes)
		values[name] = value
	}
	return values
}

func (c *checker) joinFieldOccurrences(a, b map[string]checkedExpression, span Span, depth int, budgets ...*int) map[string]checkedExpression {
	if a == nil && b == nil {
		return nil
	}
	nodes := new(int)
	if len(budgets) > 0 {
		nodes = budgets[0]
	}
	if depth > 32 || len(a) != len(b) {
		c.diagnostic("EF127", "incompatible or excessive field occurrence join", span)
		return nil
	}
	joined := map[string]checkedExpression{}
	for name, left := range a {
		*nodes++
		right, exists := b[name]
		if !exists || *nodes > 4096 {
			c.diagnostic("EF127", "incompatible or excessive field occurrence join", span)
			return nil
		}
		value := c.joinContractRows(left, right)
		value.setOwnership(mergeFacts(left.ownershipFacts(), right.ownershipFacts()))
		value.setCaptures(mergeFacts(left.captureFacts(), right.captureFacts()))
		value.callableEvidence = joinCallableEvidence(left.callableEvidence, right.callableEvidence)
		value.child = mergeFacts(left.child, right.child)
		value.evaluation = c.unionEvaluationFacts(left.evaluation, right.evaluation)
		value.executed = c.unionEvaluationFacts(left.executed, right.executed)
		value.fields = c.joinExpressionFields(left, right, span, depth+1, nodes)
		joined[name] = value
	}
	return joined
}

func fieldOccurrence(value checkedExpression, path string) (checkedExpression, bool) {
	if path == "" {
		return value, true
	}
	for _, name := range strings.Split(path, ".") {
		var ok bool
		value, ok = value.fields[name]
		if !ok {
			return checkedExpression{}, false
		}
	}
	return value, true
}

func (c *checker) fieldContract(id TypeID, path string) TypeID {
	if path == "" {
		return id
	}
	parts := strings.Split(path, ".")
	if len(parts) > 32 {
		return invalidTypeID
	}
	for i := 0; i < len(parts); i++ {
		name := parts[i]
		fields, ok := c.checkedFields(id)
		if !ok {
			if _, variants, enum := c.checkedVariants(id); enum && i+1 < len(parts) {
				for _, variant := range variants {
					if variant.Name == name {
						fields, ok = variant.Fields, true
						break
					}
				}
				i++
				name = parts[i]
			}
		}
		if !ok {
			return invalidTypeID
		}
		found := invalidTypeID
		for _, field := range fields {
			if field.Name == name {
				found = field.typeID
				break
			}
		}
		if found == invalidTypeID {
			return invalidTypeID
		}
		id = found
	}
	return id
}

func initializedFieldOccurrences(fields []FieldValue) (map[string]checkedExpression, []OwnershipFact) {
	values := map[string]checkedExpression{}
	captures := []OwnershipFact{}
	for _, field := range fields {
		value := field.Value.checked.clone()
		values[field.Name] = value
		captures = append(captures, prependFacts(field.Name, value.captureFacts())...)
	}
	return values, normalizeFacts(captures)
}

func (c *checker) retainDataPayload(result *checkedExpression, fields []FieldValue, variant string) {
	values, captures := initializedFieldOccurrences(fields)
	if variant != "" {
		payload := c.checkedData("()")
		payload.fields = values
		result.fields = map[string]checkedExpression{variant: payload}
		result.setCaptures(prependFacts(variant, captures))
	} else {
		result.fields = values
		result.setCaptures(captures)
	}
}
