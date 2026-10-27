package check

import "ergo.local/prototype/internal/sketch/syntax"

// switchUnion is a type switch subject's union as the switch reads it
// (round-6 M5; D16: nothing before the solvers' counters is superlinear).
// For an instance of a generic union (U[A]) it keeps the origin's union
// shape and the type arguments, and substitutes a variant only when a case
// or a binding needs it. A switch over n subjects of distinct
// instantiations of a k-variant union then does O(n + k) work before its
// counters, not n × k: a default-only switch instantiates no variant, and
// the origin's variant index is built once and shared by every instance.
type switchUnion struct {
	c    *checker
	orig *Union              // the origin's union, or the subject's own
	m    map[*TypeParam]Type // the instance's type arguments; nil for a non-instance
	inst map[int]Type        // the variants substituted so far
}

// subjectUnion returns t's union as a switch subject, or nil when t is not
// a union.
func (c *checker) subjectUnion(t Type) *switchUnion {
	if n, ok := t.(*Named); ok && n.Orig != nil && n.under == nil {
		if ou, ok := n.Orig.under.(*Union); ok {
			return &switchUnion{c: c, orig: ou, m: paramMap(n.Orig.TParams, n.TArgs)}
		}
	}
	if u := asUnion(t); u != nil {
		return &switchUnion{c: c, orig: u}
	}
	return nil
}

func (s *switchUnion) len() int  { return len(s.orig.Variants) }
func (s *switchUnion) bad() bool { return s.orig.Bad }

// at returns variant i, substituted on first use.
func (s *switchUnion) at(i int) Type {
	if s.m == nil {
		return s.orig.Variants[i]
	}
	if v, ok := s.inst[i]; ok {
		return v
	}
	if s.inst == nil {
		s.inst = map[int]Type{}
	}
	v := s.c.subst(s.orig.Variants[i], s.m)
	s.inst[i] = v
	return v
}

// lookup returns the index of the variant t names: the variant identical
// to t, or, for a generic type written without type arguments, the first
// variant instantiated from it; -1 when none.
func (s *switchUnion) lookup(t Type, ops *int) int {
	cands := s.orig.candidates(t, ops)
	if n, ok := t.(*Named); ok && n.Orig == nil && len(n.TParams) > 0 {
		if len(cands) > 0 {
			return cands[0]
		}
		return -1
	}
	for _, i := range cands {
		*ops++
		if identical(s.at(i), t) {
			return i
		}
	}
	return -1
}

// caseVariant resolves a case type to its variant index in the union, -1
// for _, or -2 on an error (reported). It is a keyed lookup, not a scan:
// resolving the cases stays linear before the solvers' counters (round-5
// M2; D16 as amended in round 5).
func (c *checker) caseVariant(e syntax.Expr, u *switchUnion, subj Type) int {
	if id, ok := e.(*syntax.Ident); ok && id.Name == "_" {
		return -1
	}
	t := c.typExpr0(e, c.localTypeCtx())
	if isInvalid(t) {
		return -2
	}
	if i := u.lookup(t, &c.variantOps); i >= 0 {
		return i
	}
	if !u.bad() {
		what := t.String()
		if n, ok := t.(*Named); ok && n.Orig == nil && len(n.TParams) > 0 {
			what = n.Obj.Name
		}
		c.errorf(e, CodeType, "%s is not a variant of %s (rows.md, Totality 1)", what, subj)
	}
	return -2
}
