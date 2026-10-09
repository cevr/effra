package compiler

import (
	"slices"
	"strconv"
	"strings"
)

// bindCallArguments binds the source-ordered arguments of a call to the
// callee's declared parameters. Positional arguments fill parameters from the
// left and must precede labelled ones; a label `name: value` binds the
// parameter declared with that name. An unknown label, a repeated label, a
// label naming a parameter a positional argument already fills, and a
// parameter left without an argument are errors anchored at the offending
// token. Binding never changes evaluation: arguments run in source order.
//
// A parameter the call leaves without an argument binds its declared
// constant default instead, when it has one: the call receives the checked
// literal of that constant in the parameter's slot (see bindDefaults), as if
// the author had written the value there. Constants have no evaluation, so
// authored arguments keep their source order and run once.
//
// It returns the parameter index each authored argument binds (-1 when it
// binds none) and whether the binding is complete. A positional-only call
// keeps the caller's arity diagnostic and continues checking by position. A
// complete binding that is not the identity is retained on the call for
// emission; see ArgumentParameters. Each label naming a declared parameter
// is recorded as a reference to that parameter of owner.
func (c *checker) bindCallArguments(e *Expr, params []Param, arity string, owner lexicalTarget) ([]int, bool) {
	e.ArgumentParameters = nil
	e.BoundArguments = nil
	order := make([]int, len(e.Args))
	if len(e.Fields) == 0 {
		if len(e.Args) < len(params) && hasDefault(params) {
			bound := make([]int, len(params))
			for i := range bound {
				bound[i] = -1
				if i < len(e.Args) {
					bound[i] = i
				}
			}
			for i := range e.Args {
				order[i] = i
			}
			if missing := omittedWithoutDefault(params, bound); len(missing) > 0 {
				c.diagnostic("EF106", "missing argument for parameter "+strings.Join(missing, ", "), e.Span)
				return order, false
			}
			bindDefaults(e, params, bound, order)
			return order, true
		}
		if len(e.Args) != len(params) {
			if e.PipeSpan.Length > 0 && len(params) == 0 {
				c.diagnostic("EF106", expressionName(e.Left)+" takes no parameters, so it cannot receive the piped value", e.PipeSpan)
			} else {
				c.diagnostic("EF106", arity, e.Span)
			}
		}
		for i := range order {
			order[i] = -1
			if i < len(params) {
				order[i] = i
			}
		}
		return order, true
	}
	labels := make(map[*Expr]FieldValue, len(e.Fields))
	for _, field := range e.Fields {
		labels[field.Value] = field
	}
	bound := make([]int, len(params))
	for i := range bound {
		bound[i] = -1
	}
	complete, labelled, overflow := true, false, false
	for i, arg := range e.Args {
		order[i] = -1
		field, isLabel := labels[arg]
		if !isLabel {
			switch {
			case labelled:
				c.diagnostic("EF106", "positional argument cannot follow a labelled argument", arg.Span)
				complete = false
			case i >= len(params):
				overflow = true
			default:
				bound[i], order[i] = i, i
			}
			continue
		}
		labelled = true
		index := slices.IndexFunc(params, func(p Param) bool { return p.Name == field.Name })
		if index >= 0 {
			owner.kind, owner.parameter = "parameter", field.Name
			c.observeReference(e, field.Label, owner)
		}
		switch {
		case index < 0:
			c.diagnostic("EF106", "unknown argument label "+field.Name, field.Label)
		case bound[index] >= 0:
			if _, previous := labels[e.Args[bound[index]]]; previous {
				c.diagnostic("EF106", "duplicate argument label "+field.Name, field.Label)
			} else if e.PipeSpan.Length > 0 && bound[index] == 0 {
				c.diagnostic("EF106", "the piped value already binds parameter "+field.Name+"; remove label "+field.Name, field.Label)
			} else {
				c.diagnostic("EF106", "argument label "+field.Name+" names a parameter already bound by positional argument "+strconv.Itoa(bound[index]+1), field.Label)
			}
		default:
			bound[index], order[i] = i, index
			continue
		}
		complete = false
	}
	if overflow {
		c.diagnostic("EF106", arity, e.Span)
		complete = false
	}
	if missing := omittedWithoutDefault(params, bound); len(missing) > 0 {
		c.diagnostic("EF106", "missing argument for parameter "+strings.Join(missing, ", "), e.Span)
		complete = false
	}
	if !complete {
		return order, false
	}
	bindDefaults(e, params, bound, order)
	return order, true
}

// omittedWithoutDefault names the parameters a binding leaves without an
// argument and without a declared constant default.
func omittedWithoutDefault(params []Param, bound []int) []string {
	missing := []string{}
	for index, arg := range bound {
		if arg < 0 && params[index].DefaultValue == nil {
			missing = append(missing, params[index].Name)
		}
	}
	return missing
}

func hasDefault(params []Param) bool {
	return slices.ContainsFunc(params, func(p Param) bool { return p.DefaultValue != nil })
}

// bindDefaults completes a binding whose every omitted parameter has a
// default. Each omitted parameter receives its constant's literal, placed in
// the bound vector before the first authored argument that binds a later
// parameter. Constants evaluate nothing, so authored arguments keep their
// source order, and a call whose authored arguments bind parameters in
// increasing order becomes the identity: the plain positional call with every
// constant written in its own slot. bound maps each parameter to its
// authored argument (-1 when omitted) and order each authored argument to its
// parameter; the binding is retained on the call when it is not the identity.
func bindDefaults(e *Expr, params []Param, bound []int, order []int) {
	arguments := make([]*Expr, 0, len(params))
	parameters := make([]int, 0, len(params))
	next := 0
	defaultsBefore := func(limit int) {
		for ; next < limit; next++ {
			if bound[next] < 0 {
				arguments = append(arguments, constantArgument(*params[next].DefaultValue, e.Span))
				parameters = append(parameters, next)
			}
		}
	}
	for i, arg := range e.Args {
		defaultsBefore(order[i])
		arguments, parameters = append(arguments, arg), append(parameters, order[i])
	}
	defaultsBefore(len(params))
	if len(arguments) > len(e.Args) {
		e.BoundArguments = arguments
	}
	for i, parameter := range parameters {
		if parameter != i {
			e.ArgumentParameters = parameters
			break
		}
	}
}

// constantArgument is the checked argument expression of a constant value:
// the literal an author would write for it. A negative i64 is one signed
// literal, as in a constant declaration, rather than a runtime negation.
func constantArgument(value ConstantValue, span Span) *Expr {
	kind := value.Kind
	if kind == "i64" {
		kind = "integer"
	}
	return &Expr{Kind: kind, Text: value.Value, Span: span, Extent: span, defaultArgument: true}
}

// checkDefaultArguments checks a call's constant default arguments and
// returns each one's checked value by the parameter it binds. They are closed
// literals, so they need no environment and record no source facts.
func (c *checker) checkDefaultArguments(e *Expr) map[int]checkedExpression {
	if e.BoundArguments == nil {
		return nil
	}
	previous := c.recordFacts
	c.recordFacts = false
	defer func() { c.recordFacts = previous }()
	checked := map[int]checkedExpression{}
	for i, argument := range e.BoundArguments {
		if argument.defaultArgument {
			checked[e.argumentParameter(i)] = c.expr(argument, nil, false)
		}
	}
	return checked
}

// boundArguments is a checked call's complete argument vector in evaluation
// order: the authored Args with any constant defaults in their slots.
func (e *Expr) boundArguments() []*Expr {
	if e.BoundArguments != nil {
		return e.BoundArguments
	}
	return e.Args
}

// rejectArgumentLabels diagnoses labels on a callee whose parameters have no
// declared names: callable values and Go host functions take arguments by
// position only. Checking continues positionally.
func (c *checker) rejectArgumentLabels(e *Expr, callee string) {
	for _, field := range e.Fields {
		c.diagnostic("EF106", callee+" take positional arguments; remove label "+field.Name, field.Label)
	}
}

// argumentParameter returns the parameter bound by the bound argument at
// index. Unlabelled and identity-labelled calls bind by position.
func (e *Expr) argumentParameter(index int) int {
	if e.ArgumentParameters == nil {
		return index
	}
	return e.ArgumentParameters[index]
}

// parameterArguments returns a checked call's bound arguments in parameter
// order. Emitters evaluate boundArguments in order and pass these positions.
func (e *Expr) parameterArguments() []*Expr {
	bound := e.boundArguments()
	if e.ArgumentParameters == nil {
		return bound
	}
	arguments := make([]*Expr, len(bound))
	for i, parameter := range e.ArgumentParameters {
		arguments[parameter] = bound[i]
	}
	return arguments
}
