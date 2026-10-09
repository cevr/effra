package compiler

// Post-check lowering shared by both emitters.
//
// Effra has no loop construct, so a self-recursive function is the only way to
// iterate. Goroutine stacks grow, but a V8 stack does not: the same program
// that runs on Go overflows on Node. The pass finds the self tail calls of a
// pure function so each emitter can turn them into a loop (Go `for`, JS
// `while (true)`) with the same meaning on both targets.
//
// The pass only reads the checked tree and returns annotations; nothing is
// rewritten in place and no intermediate tree is built. A second consumer,
// statement-position `if`/`match` lowering, should reuse tailSpine: it names
// the expressions whose branches complete the function.
//
// Scope, as in MoonBit's contification pass and Gleam's tail_call_loop:
//   - only a direct call of the function being lowered, by the checker's
//     resolved function (a shadowing local or a callable value never matches);
//   - only a pure function, so there is no `run` and no recipe whose
//     laziness an argument rewrite could disturb;
//   - only a call in tail position: the last statement of the body, and,
//     recursively, the last statement of every `if` branch and `match` arm
//     that is itself in tail position;
//   - mutual recursion stays recursive.

// tailLoop is the lowering plan for one pure function that has at least one
// self tail call.
type tailLoop struct {
	function *Function
	calls    map[*Expr]bool
	spine    map[*Expr]bool
}

// lowerTail returns the plan for f, or nil when f must stay a plain function.
func lowerTail(f *Function) *tailLoop {
	if f == nil || f.Effect || f.Body == nil {
		return nil
	}
	plan := &tailLoop{function: f, calls: map[*Expr]bool{}, spine: map[*Expr]bool{}}
	plan.block(f.Body)
	if len(plan.calls) == 0 {
		return nil
	}
	return plan
}

// isCall reports whether e is a self tail call to rewrite.
func (t *tailLoop) isCall(e *Expr) bool { return t != nil && t.calls[e] }

// onSpine reports whether e is an `if` or `match` in tail position with a self
// tail call below it. Its branches are lowered as statements that return or
// continue; every other `if` and `match` keeps its expression form.
func (t *tailLoop) onSpine(e *Expr) bool { return t != nil && t.spine[e] }

// block records the tail of b and reports whether a self tail call was found.
func (t *tailLoop) block(b *Block) bool {
	if b == nil || len(b.Statements) == 0 {
		return false
	}
	last := b.Statements[len(b.Statements)-1]
	if last.Kind == "let" || last.Kind == "fail" || last.Value == nil {
		return false
	}
	return t.tail(last.Value)
}

func (t *tailLoop) tail(e *Expr) bool {
	switch e.Kind {
	case "call":
		if t.selfCall(e) {
			t.calls[e] = true
			return true
		}
	case "if":
		// Both branches are visited: one may hold a call even if the other does not.
		then, otherwise := t.block(e.Then), t.block(e.Else)
		if then || otherwise {
			t.spine[e] = true
			return true
		}
	case "match":
		found := false
		for _, arm := range e.Arms {
			if t.block(arm.Body) {
				found = true
			}
		}
		if found && e.matchPlan != nil {
			t.spine[e] = true
			return true
		}
	}
	return false
}

// selfCall recognises the plain module-function application the checker
// resolved to the function being lowered. Special applications (data
// construction, foreign and host calls, fibers, callable values, provider
// constructors) carry their own Text and never name a module function.
func (t *tailLoop) selfCall(e *Expr) bool {
	switch e.Text {
	case "callable", "data", "foreign", "hostConvert", "fiber", "provider-constructor":
		return false
	}
	f := t.function
	if e.ResolvedFunction != f || f.Owner != "module" || e.Left == nil || e.Left.Kind != "name" || len(e.boundArguments()) != len(f.Params) {
		return false
	}
	arguments := e.parameterArguments()
	if len(arguments) != len(f.Params) {
		return false
	}
	for _, argument := range arguments {
		if argument == nil {
			return false
		}
	}
	return true
}

// unchanged reports whether the canonical argument for parameter i passes
// that parameter through untouched. Labels keep authored Args in source order,
// so compare the checked parameter-order view by binder identity.
func (t *tailLoop) unchanged(call *Expr, i int) bool {
	if t == nil || call == nil || i < 0 || i >= len(t.function.Params) {
		return false
	}
	arguments := call.parameterArguments()
	if i >= len(arguments) || arguments[i] == nil {
		return false
	}
	param, arg := t.function.Params[i], arguments[i]
	return arg.Kind == "name" && arg.Text != "function" && arg.Text != "provider" && param.binding != nil && arg.binding == param.binding
}
