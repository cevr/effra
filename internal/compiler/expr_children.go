package compiler

// forEachExprChild visits each expression-valued child exactly once in source
// order. Checked data calls retain named arguments in both Args and Fields so
// later tooling can inspect their labels; the value is still one child.
// Construction and payload expressions use Fields as their only child list.
func forEachExprChild(expression *Expr, visit func(*Expr)) {
	if expression == nil {
		return
	}
	if expression.Left != nil {
		visit(expression.Left)
	}
	if expression.Right != nil {
		visit(expression.Right)
	}
	for _, argument := range expression.Args {
		if argument != nil {
			visit(argument)
		}
	}
	if expression.Kind == "call" && len(expression.Args) > 0 {
		return
	}
	for _, field := range expression.Fields {
		if field.Value != nil {
			visit(field.Value)
		}
	}
}
