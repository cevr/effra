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
// It returns the parameter index each argument binds (-1 when it binds none)
// and whether the binding is complete. A positional-only call keeps the
// caller's arity diagnostic and continues checking by position. A complete
// labelled binding that is not the identity is retained on the call for
// emission; see ArgumentParameters.
func (c *checker) bindCallArguments(e *Expr, params []Param, arity string) ([]int, bool) {
	e.ArgumentParameters = nil
	order := make([]int, len(e.Args))
	if len(e.Fields) == 0 {
		if len(e.Args) != len(params) {
			c.diagnostic("EF106", arity, e.Span)
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
		switch {
		case index < 0:
			c.diagnostic("EF106", "unknown argument label "+field.Name, field.Label)
		case bound[index] >= 0:
			if _, previous := labels[e.Args[bound[index]]]; previous {
				c.diagnostic("EF106", "duplicate argument label "+field.Name, field.Label)
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
	missing := []string{}
	for index, arg := range bound {
		if arg < 0 {
			missing = append(missing, params[index].Name)
		}
	}
	if len(missing) > 0 {
		c.diagnostic("EF106", "missing argument for parameter "+strings.Join(missing, ", "), e.Span)
		complete = false
	}
	if !complete {
		return order, false
	}
	for i, parameter := range order {
		if parameter != i {
			e.ArgumentParameters = order
			break
		}
	}
	return order, true
}

// rejectArgumentLabels diagnoses labels on a callee whose parameters have no
// declared names: callable values and Go host functions take arguments by
// position only. Checking continues positionally.
func (c *checker) rejectArgumentLabels(e *Expr, callee string) {
	for _, field := range e.Fields {
		c.diagnostic("EF106", callee+" take positional arguments; remove label "+field.Name, field.Label)
	}
}

// argumentParameter returns the parameter bound by the source argument at
// index. Unlabelled and identity-labelled calls bind by position.
func (e *Expr) argumentParameter(index int) int {
	if e.ArgumentParameters == nil {
		return index
	}
	return e.ArgumentParameters[index]
}

// parameterArguments returns a checked call's arguments in parameter order.
// Emitters evaluate e.Args in source order and pass these positions.
func (e *Expr) parameterArguments() []*Expr {
	if e.ArgumentParameters == nil {
		return e.Args
	}
	arguments := make([]*Expr, len(e.Args))
	for i, parameter := range e.ArgumentParameters {
		arguments[parameter] = e.Args[i]
	}
	return arguments
}
