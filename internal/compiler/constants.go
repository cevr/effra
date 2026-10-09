package compiler

import "strconv"

func constantIdentity(module, name string) string {
	return "constant:" + module + ":module:" + name
}

func isI64IntegerLiteral(text string) bool {
	_, err := strconv.ParseInt(text, 10, 64)
	return err == nil
}

func cloneConstantValue(value *ConstantValue) *ConstantValue {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneParam(parameter *Param) *Param {
	if parameter == nil {
		return nil
	}
	copy := publicParams([]Param{*parameter})[0]
	return &copy
}

func sameConstantValue(left, right *ConstantValue) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func (c *checker) registerConstants(claim func(string, Span)) {
	for _, declaration := range c.program.Constants {
		module := declaration.Module
		if module == "" {
			module = currentModuleIdentity
			declaration.Module = module
			declaration.SourceID = userSourceID
		}
		declaration.Identity = constantIdentity(module, declaration.Name)
		byName := c.constantsByModule[module]
		if byName == nil {
			byName = map[string]*Constant{}
			c.constantsByModule[module] = byName
		}
		if previous := byName[declaration.Name]; previous != nil && previous != declaration {
			c.diagnostic("EF101", "duplicate constant "+declaration.Name, declaration.Span)
			declaration.invalid = true
			continue
		}
		byName[declaration.Name] = declaration
		if module == currentModuleIdentity {
			claim(declaration.Name, declaration.Span)
			c.constants[declaration.Name] = declaration
		}
	}
}

func (c *checker) checkConstants() {
	state := map[*Constant]uint8{}
	for _, declaration := range c.program.Constants {
		c.resolveConstant(declaration, state)
		if declaration.Module != currentModuleIdentity || declaration.Value == nil || declaration.typeID == invalidTypeID {
			continue
		}
		ref := c.identityRef(declaration.typeID)
		value := *declaration.Value
		c.result.Declarations = append(c.result.Declarations, Declaration{
			Kind: "constant", Name: declaration.Name, Identity: declaration.Identity,
			Source: declaration.SourceID, Type: &ref, Constant: &value, Span: declaration.Span,
		})
	}
}

func (c *checker) resolveConstant(declaration *Constant, state map[*Constant]uint8) *ConstantValue {
	if declaration == nil {
		return nil
	}
	if declaration.Value != nil {
		return declaration.Value
	}
	if declaration.invalid {
		return nil
	}
	switch state[declaration] {
	case 1:
		c.diagnostic("EF102", "constant alias cycle involving "+declaration.Name, declaration.Span)
		declaration.invalid = true
		return nil
	case 2:
		return declaration.Value
	}
	state[declaration] = 1
	if declaration.Type != "i64" && declaration.Type != "string" && declaration.Type != "bool" {
		c.diagnostic("EF102", "constant type must be i64, string or bool", declaration.TypeSpan)
		declaration.invalid = true
		state[declaration] = 2
		return nil
	}
	declaration.typeID = c.canonicalRef(typeRef(declaration.Type))
	value, ok := c.constantExpression(declaration.Module, declaration.Expr, state)
	if ok && value.Kind != declaration.Type {
		c.diagnostic("EF106", "constant value must be "+declaration.Type, declaration.Expr.Span)
		ok = false
	}
	if ok {
		copy := *value
		declaration.Value = &copy
	} else {
		declaration.invalid = true
	}
	state[declaration] = 2
	return declaration.Value
}

func (c *checker) constantExpression(module string, expression *Expr, state map[*Constant]uint8) (*ConstantValue, bool) {
	if expression == nil {
		return nil, false
	}
	switch expression.Kind {
	case "integer":
		value, err := strconv.ParseInt(expression.Text, 10, 64)
		if err != nil {
			c.diagnostic("EF106", "i64 constant is outside the signed 64-bit range", expression.Span)
			return nil, false
		}
		return &ConstantValue{Kind: "i64", Value: strconv.FormatInt(value, 10)}, true
	case "string":
		return &ConstantValue{Kind: "string", Value: expression.Text}, true
	case "bool":
		return &ConstantValue{Kind: "bool", Value: expression.Text}, true
	case "name", "member":
		declaration := c.constantReference(module, expression)
		if declaration == nil {
			c.diagnostic("EF102", "constant expression must name an admitted constant", expression.Span)
			return nil, false
		}
		value := c.resolveConstant(declaration, state)
		if value == nil {
			return nil, false
		}
		c.observeReference(expression, expression.Span, lexicalTarget{kind: "constant", constant: declaration})
		if expression.Kind == "member" && expression.Left != nil {
			c.observeModuleAlias(expression, expression.Left)
		}
		copy := *value
		return &copy, true
	default:
		c.diagnostic("EF102", "constant expression must be a scalar literal or direct constant alias", expression.Span)
		return nil, false
	}
}

func (c *checker) constantReference(module string, expression *Expr) *Constant {
	if expression == nil {
		return nil
	}
	if expression.Kind == "name" {
		if expression.binding != nil {
			return nil
		}
		if byName := c.constantsByModule[module]; byName != nil {
			return byName[expression.Name]
		}
		return nil
	}
	if expression.Kind == "member" && expression.Left != nil && expression.Left.Kind == "name" && expression.Left.binding == nil {
		return c.program.BundledConstantBindings[expression.Left.Name][expression.Name]
	}
	return nil
}
