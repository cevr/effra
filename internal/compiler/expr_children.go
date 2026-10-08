package compiler

// forEachExprChild visits each expression-valued child exactly once. The order
// is the node's field order (Left, Right, Args, ...), not source order: a pipe
// call's Left is its callee, which follows Args[0], its subject, in the source.
// The extents of sibling children are disjoint; no consumer may rely on the
// order. Checked data calls retain named arguments in both Args and Fields so
// later tooling can inspect their labels; the value is still one child.
// Construction and payload expressions use Fields as their only child list.
func forEachExprChild(expression *Expr, visit func(*Expr)) {
	forEachExprChildUntil(expression, func(child *Expr) bool {
		visit(child)
		return true
	})
}

// forEachExprChildUntil stops when visit returns false. Bounded source capture
// uses this to stop walking a wide node as soon as its fact budget is spent.
func forEachExprChildUntil(expression *Expr, visit func(*Expr) bool) {
	if expression == nil {
		return
	}
	if expression.Left != nil && !visit(expression.Left) {
		return
	}
	if expression.Right != nil && !visit(expression.Right) {
		return
	}
	for _, argument := range expression.Args {
		if argument != nil && !visit(argument) {
			return
		}
	}
	if expression.Kind == "call" && len(expression.Args) > 0 {
		return
	}
	for _, field := range expression.Fields {
		if field.Value != nil && !visit(field.Value) {
			return
		}
	}
}
