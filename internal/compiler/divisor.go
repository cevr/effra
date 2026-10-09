package compiler

import (
	"math"
	"strconv"
)

// Signed-64 division and remainder are total only for a divisor that cannot
// be zero. Rather than add a hidden failure row or a host panic to a pure
// operator, the checker admits `/` and `%` only when the divisor is proven
// nonzero in checked source: a nonzero integer literal (optionally negated),
// or a local i64 binding inside an if branch whose condition compares it with
// an integer literal and excludes zero on that branch. Every other divisor is
// refused with EF150, so a division by zero is unrepresentable rather than a
// runtime defect. MIN / -1 wraps to MIN (and MIN % -1 is 0) under the profile's
// signed 64-bit wrap; see docs/research/numeric-arithmetic.md.

// i64Constant reports the value of an integer literal or a negated integer
// literal. Parentheses are syntax only and do not produce an expression node.
func i64Constant(e *Expr) (int64, bool) {
	if e == nil {
		return 0, false
	}
	if e.Kind == "integer" {
		value, err := strconv.ParseInt(normalizedI64Literal(e.Text), 10, 64)
		return value, err == nil
	}
	if e.Kind == "unary" && e.Name == "-" && e.Left != nil && e.Left.Kind == "integer" {
		if isMinI64Literal(e.Left.Text) {
			return math.MinInt64, true
		}
		value, err := strconv.ParseInt(normalizedI64Literal(e.Left.Text), 10, 64)
		return -value, err == nil
	}
	return 0, false
}

// divisorProof reports whether the checked divisor expression is proven
// nonzero.
func (c *checker) divisorProof(divisor *Expr) bool {
	if value, ok := i64Constant(divisor); ok {
		return value != 0
	}
	return divisor != nil && divisor.Kind == "name" && divisor.binding != nil && c.nonzero[divisor.binding] > 0
}

func divisorRefusal(e *Expr) string {
	if value, ok := i64Constant(e.Right); ok && value == 0 {
		return "i64 " + e.Name + " by zero is refused"
	}
	return "i64 " + e.Name + " requires a divisor proven nonzero: a nonzero integer literal, or a local name inside an if branch that excludes zero, such as `if d == 0 { ... } else { n " + e.Name + " d }`"
}

// nonzeroBranchProofs reads an if condition `name OP literal` (or
// `literal OP name`) over a local binding. Evaluating the comparison with the
// binding at zero decides the branch: when it is false, zero cannot reach the
// then branch; when it is true, zero cannot reach the else branch.
func nonzeroBranchProofs(condition *Expr) (then, otherwise *localBinding) {
	if condition == nil || condition.Kind != "binary" {
		return nil, nil
	}
	op := condition.Name
	name, constant := condition.Left, condition.Right
	nameOnLeft := true
	if name.Kind != "name" {
		name, constant, nameOnLeft = condition.Right, condition.Left, false
	}
	if name == nil || name.Kind != "name" || name.binding == nil {
		return nil, nil
	}
	value, ok := i64Constant(constant)
	if !ok {
		return nil, nil
	}
	left, right := int64(0), value
	if !nameOnLeft {
		left, right = value, 0
	}
	var atZero bool
	switch op {
	case "==":
		atZero = left == right
	case "<":
		atZero = left < right
	case "<=":
		atZero = left <= right
	case ">":
		atZero = left > right
	case ">=":
		atZero = left >= right
	default:
		return nil, nil
	}
	if atZero {
		return nil, name.binding
	}
	return name.binding, nil
}

// withNonzero checks a branch with binding proven nonzero.
func (c *checker) withNonzero(binding *localBinding, check func() checkedExpression) checkedExpression {
	if binding == nil {
		return check()
	}
	if c.nonzero == nil {
		c.nonzero = map[*localBinding]int{}
	}
	c.nonzero[binding]++
	defer func() { c.nonzero[binding]-- }()
	return check()
}
