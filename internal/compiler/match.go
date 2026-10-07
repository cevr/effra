package compiler

import (
	"slices"
	"strconv"
	"strings"
)

// maxMatchCoverageWork bounds usefulness and exhaustiveness analysis for one
// match expression. Work counts visited decision nodes plus the arm rows
// scanned while splitting each subject, so product subjects cost what their
// distinguishable combinations cost rather than their full Cartesian size.
const maxMatchCoverageWork = 1 << 16

// matchCoverageExhaustedCode refuses a match whose analysis exceeds its bound.
// No plan is published: a partial analysis could not prove exhaustiveness.
const matchCoverageExhaustedCode = "EF137"

// maxReportedMissingMatchArms limits witness diagnostics for one match.
const maxReportedMissingMatchArms = 8

// matchPlan is the checked first-match contract consumed by both backends.
// Subjects are evaluated once, in source order. Arms are tried in source order
// and an arm is selected when every subject's variant is one of that subject's
// cell alternatives. Each arm body is lowered once however many alternatives
// reach it; neither backend re-derives coverage.
type matchPlan struct {
	subjects []matchPlanSubject
	arms     []matchPlanArm
}

type matchPlanSubject struct {
	value    checkedExpression
	enum     *Enum
	variants []string
	// tested is false when every arm admits every variant of this subject, so
	// lowering need not decode its variant at all.
	tested bool
}

type matchPlanArm struct {
	cells    []matchPlanCell
	bindings []matchPlanBinding
	body     *Block
}

type matchPlanCell struct {
	alternatives []*MatchPattern
	// total is true when the alternatives name every declared variant.
	total bool
}

// matchPlanBinding binds one name from one subject. fields holds the payload
// field read for each alternative of that subject's cell, in cell order.
type matchPlanBinding struct {
	name    string
	subject int
	fields  []string
	value   checkedExpression
}

func (s matchPlanSubject) index(variant string) int {
	return slices.Index(s.variants, variant)
}

func (c *checker) match(e *Expr, env map[string]checkedExpression, inEffect bool) checkedExpression {
	plan := &matchPlan{}
	subjectEvaluation := c.evaluation(emptyRowID, emptyRowID)
	declared := []map[string]Variant{}
	valid := true
	for _, subject := range e.Args {
		value := c.expr(subject, env, inEffect)
		subjectEvaluation = c.unionEvaluationFacts(subjectEvaluation, value.evaluation)
		if value.isEffect() {
			c.diagnostic("EF106", "match subject must be a value; execute an Effect with run", subject.Span)
		}
		enum, variants, isEnum := c.checkedVariants(value.valueID())
		if !isEnum {
			c.diagnostic("EF116", "match requires a closed enum value", subject.Span)
			valid = false
			continue
		}
		names := map[string]Variant{}
		planned := matchPlanSubject{value: value, enum: enum}
		for _, variant := range variants {
			names[variant.Name] = variant
			planned.variants = append(planned.variants, variant.Name)
		}
		declared = append(declared, names)
		plan.subjects = append(plan.subjects, planned)
	}
	if !valid {
		return c.checkedData("invalid")
	}
	coverage := newMatchCoverage(plan.subjects)
	result := c.checkedData("never")
	branchEvaluation := c.evaluation(emptyRowID, emptyRowID)
	haveResult := false
	for _, arm := range e.Arms {
		if len(arm.Patterns) != len(plan.subjects) {
			c.diagnostic("EF118", "match arm has "+strconv.Itoa(len(arm.Patterns))+" subject pattern(s); the match has "+strconv.Itoa(len(plan.subjects))+" subject(s)", arm.Span)
			continue
		}
		row, cells, ok := c.matchArmCells(arm, plan, declared)
		if !ok {
			continue
		}
		if !coverage.exhausted {
			if !coverage.reachable(row) {
				if !coverage.exhausted {
					coverage.unreachableArm(arm, plan)
					continue
				}
			} else {
				coverage.unreachableAlternatives(arm, row, plan)
			}
		}
		coverage.rows = append(coverage.rows, row)
		branchEnv := clone(env)
		bindings := c.matchArmBindings(arm, plan, declared, branchEnv)
		branch := c.block(arm.Body, branchEnv, inEffect)
		plan.arms = append(plan.arms, matchPlanArm{cells: cells, bindings: bindings, body: arm.Body})
		if !c.isKind(branch, "never") {
			if !haveResult {
				result, haveResult = branch, true
			} else if !c.sameValues(result, branch) || result.isEffect() != branch.isEffect() {
				c.diagnostic("EF106", "match branches must return the same type", arm.Span)
			} else {
				result = c.joinContractRows(result, branch)
				result.fields = c.joinExpressionFields(result, branch, arm.Span, 0, new(int))
				result.setOwnership(mergeFacts(result.ownershipFacts(), branch.ownershipFacts()))
				result.setCaptures(mergeFacts(result.captureFacts(), branch.captureFacts()))
			}
		}
		branchEvaluation = c.unionEvaluationFacts(branchEvaluation, branch.evaluation)
	}
	if !coverage.exhausted {
		missing, more := coverage.missing()
		for _, witness := range missing {
			parts := make([]string, len(witness))
			for index, variant := range witness {
				parts[index] = plan.subjects[index].enum.Name + "." + plan.subjects[index].variants[variant]
			}
			coverage.claim("EF117", "missing match arm for "+strings.Join(parts, ", "), e.Span)
		}
		if more {
			coverage.claim("EF117", "further uncovered match combinations omitted", e.Span)
		}
	}
	if coverage.exhausted {
		// The staged claims came from an incomplete analysis; none is published.
		c.diagnostic(matchCoverageExhaustedCode, "match coverage analysis exceeds its "+strconv.Itoa(maxMatchCoverageWork)+" work budget; split the decision into smaller matches", e.Span)
	} else {
		for _, claim := range coverage.claims {
			c.diagnostic(claim.Code, claim.Message, claim.Span)
		}
		for _, arm := range plan.arms {
			for subject, cell := range arm.cells {
				if !cell.total {
					plan.subjects[subject].tested = true
				}
			}
		}
		e.matchPlan = plan
	}
	if !haveResult {
		result = c.checkedData("never")
	}
	result.evaluation = c.unionEvaluationFacts(subjectEvaluation, branchEvaluation)
	return result
}

// matchArmCells resolves every alternative to a declared variant of its
// subject. The returned row holds each subject's admitted variant set.
func (c *checker) matchArmCells(arm *MatchArm, plan *matchPlan, declared []map[string]Variant) ([]variantSet, []matchPlanCell, bool) {
	row := make([]variantSet, len(plan.subjects))
	cells := make([]matchPlanCell, len(plan.subjects))
	ok := true
	for subject, cell := range arm.Patterns {
		enum := plan.subjects[subject].enum
		row[subject] = newVariantSet(len(plan.subjects[subject].variants))
		for _, pattern := range cell {
			if pattern.TypeName == "_" {
				c.diagnostic("EF118", "catch-all match arms cannot claim exhaustive closed interpretation", pattern.Span)
				ok = false
				continue
			}
			patternOwner := c.templateByName(pattern.TypeName)
			if patternOwner != enum && !(len(enum.Parameters) == 0 && pattern.TypeName == enum.Name) {
				c.diagnostic("EF116", "match pattern belongs to "+pattern.TypeName+", expected "+enum.Name, pattern.Span)
				ok = false
				continue
			}
			pattern.ResolvedEnum = enum
			c.observePattern(pattern, enum)
			if pattern.VariantName == "" {
				c.diagnostic("EF118", "match arm must name a declared variant", pattern.Span)
				ok = false
				continue
			}
			if _, exists := declared[subject][pattern.VariantName]; !exists {
				c.diagnostic("EF116", "unknown variant "+enum.Name+"."+pattern.VariantName, pattern.Span)
				ok = false
				continue
			}
			variant := plan.subjects[subject].index(pattern.VariantName)
			if row[subject].has(variant) {
				c.diagnostic("EF117", "duplicate alternative "+enum.Name+"."+pattern.VariantName, pattern.Span)
				ok = false
				continue
			}
			row[subject].add(variant)
		}
		cells[subject] = matchPlanCell{alternatives: cell, total: row[subject].count() == len(plan.subjects[subject].variants)}
	}
	return row, cells, ok
}

func (m *matchCoverage) unreachableArm(arm *MatchArm, plan *matchPlan) {
	if len(arm.Patterns) == 1 && len(arm.Patterns[0]) == 1 {
		pattern := arm.Patterns[0][0]
		m.claim("EF117", "duplicate match arm for "+plan.subjects[0].enum.Name+"."+pattern.VariantName, pattern.Span)
		return
	}
	m.claim("EF117", "unreachable match arm; earlier arms cover every combination it names", arm.Span)
}

// unreachableAlternatives reports an alternative whose every combination with
// the arm's other cells is already selected by an earlier arm.
func (m *matchCoverage) unreachableAlternatives(arm *MatchArm, row []variantSet, plan *matchPlan) {
	for subject, cell := range arm.Patterns {
		if len(cell) < 2 {
			continue
		}
		for _, pattern := range cell {
			query := slices.Clone(row)
			query[subject] = newVariantSet(len(plan.subjects[subject].variants))
			query[subject].add(plan.subjects[subject].index(pattern.VariantName))
			if m.reachable(query) {
				continue
			}
			if m.exhausted {
				return
			}
			m.claim("EF117", "unreachable alternative "+plan.subjects[subject].enum.Name+"."+pattern.VariantName+"; earlier arms cover it", pattern.Span)
		}
	}
}

// matchArmBindings checks payload binders and binds each name once in env.
// Every alternative of a cell must bind the same names with identical payload
// types; the bound value joins the alternatives' payload provenance. The
// lexical binding is declared by the first alternative's token, lists every
// later alternative's token as an occurrence and records the completed join.
func (c *checker) matchArmBindings(arm *MatchArm, plan *matchPlan, declared []map[string]Variant, env map[string]checkedExpression) []matchPlanBinding {
	var bindings []matchPlanBinding
	// tokens holds each binding's declaring pattern and binder name spans,
	// the first alternative's token first.
	type binderTokens struct {
		pattern *MatchPattern
		spans   []Span
	}
	var tokens []binderTokens
	armNames := map[string]bool{}
	for subject, cell := range arm.Patterns {
		scrutinee := plan.subjects[subject].value
		var first *MatchPattern
		var firstNames []string
		cellBindings := map[string]int{}
		for alternative, pattern := range cell {
			fields := fieldsMap(declared[subject][pattern.VariantName].Fields)
			aliases := map[string]bool{}
			names := []string{}
			for _, fieldName := range sortedBindingNames(pattern.Bindings) {
				binding := pattern.Bindings[fieldName]
				if binding != "_" {
					if aliases[binding] {
						c.diagnostic("EF121", "duplicate pattern binding "+binding, pattern.Span)
						continue
					}
					aliases[binding] = true
				}
				field, ok := fields[fieldName]
				if !ok {
					c.diagnostic("EF114", "unknown payload field "+fieldName+" in match arm", pattern.Span)
					continue
				}
				if binding == "_" {
					continue
				}
				names = append(names, binding)
				bound := c.checkedDataID(field.typeID, nil, nil)
				if payload, exists := scrutinee.fields[pattern.VariantName]; exists {
					bound = c.projectFieldOccurrence(payload, field)
				} else if c.node(field.typeID) != nil && c.node(field.typeID).Kind == "callable" {
					bound.callableEvidence = callableEvidence{unresolved: true}
				}
				bound.setOwnership(projectVariantFacts(scrutinee.ownershipFacts(), pattern.VariantName, fieldName))
				bound.setCaptures(projectVariantFacts(scrutinee.captureFacts(), pattern.VariantName, fieldName))
				if len(bound.ownershipFacts()) == 0 {
					bound.setOwnership(c.unknownOwnershipID(field.typeID))
				}
				if alternative == 0 {
					if armNames[binding] {
						c.diagnostic("EF121", "duplicate pattern binding "+binding, pattern.Span)
						continue
					}
					armNames[binding] = true
					cellBindings[binding] = len(bindings)
					bindings = append(bindings, matchPlanBinding{name: binding, subject: subject, fields: []string{fieldName}, value: bound})
					tokens = append(tokens, binderTokens{pattern: pattern, spans: []Span{pattern.bindingSpan(fieldName)}})
					continue
				}
				index, shared := cellBindings[binding]
				if !shared {
					continue
				}
				tokens[index].spans = append(tokens[index].spans, pattern.bindingSpan(fieldName))
				joined := bindings[index].value
				// The payload contract is the canonical type, callable failure
				// and service rows included: the call recipe reads them from the
				// bound value, so a row present in only one alternative would be
				// silently dropped or invented by whichever alternative is first.
				if joined.valueID() != bound.valueID() {
					c.diagnostic("EF121", "alternative binding "+binding+" has a different type in "+plan.subjects[subject].enum.Name+"."+pattern.VariantName+" than in "+plan.subjects[subject].enum.Name+"."+first.VariantName+c.callableRowDifference(bound.valueID(), joined.valueID()), pattern.Span)
					continue
				}
				joined.fields = c.joinExpressionFields(joined, bound, pattern.Span, 0, new(int))
				joined.setOwnership(mergeFacts(joined.ownershipFacts(), bound.ownershipFacts()))
				joined.setCaptures(mergeFacts(joined.captureFacts(), bound.captureFacts()))
				joined.callableEvidence = joinCallableEvidence(joined.callableEvidence, bound.callableEvidence)
				if joined.callableDecl != bound.callableDecl {
					joined.callableDecl = nil
				}
				bindings[index].value = joined
				bindings[index].fields = append(bindings[index].fields, fieldName)
			}
			slices.Sort(names)
			if alternative == 0 {
				first, firstNames = pattern, names
			} else if !slices.Equal(names, firstNames) {
				c.diagnostic("EF121", "alternatives must bind the same names: "+plan.subjects[subject].enum.Name+"."+first.VariantName+" binds "+bindingList(firstNames)+", "+plan.subjects[subject].enum.Name+"."+pattern.VariantName+" binds "+bindingList(names), pattern.Span)
			}
		}
	}
	for index := range bindings {
		binding := &bindings[index]
		if c.lexicalOwner != nil {
			declaration := tokens[index]
			binding.value = c.bindLocal("pattern", binding.name, declaration.spans[0], declaration.pattern.Extent, c.result.lexical.patterns[declaration.pattern], binding.value, declaration.spans[1:]...)
		}
		env[binding.name] = binding.value
	}
	return bindings
}

// callableRowDifference names the rows that distinguish two callable payload
// types of the same shape, which otherwise display identically.
func (c *checker) callableRowDifference(later, first TypeID) string {
	left, right := c.node(later), c.node(first)
	if left == nil || right == nil || left.Kind != "callable" || right.Kind != "callable" || left.Mode != right.Mode || left.Result != right.Result || !slices.Equal(left.Args, right.Args) {
		return ""
	}
	var differences []string
	if left.FailureRow != right.FailureRow {
		differences = append(differences, "raises "+rowList(c.rowLabels(left.FailureRow))+" versus "+rowList(c.rowLabels(right.FailureRow)))
	}
	if left.ServiceRow != right.ServiceRow {
		differences = append(differences, "uses "+rowList(c.rowLabels(left.ServiceRow))+" versus "+rowList(c.rowLabels(right.ServiceRow)))
	}
	if len(differences) == 0 {
		return ""
	}
	return ": callable " + strings.Join(differences, ", ")
}

func rowList(labels []string) string { return "{" + strings.Join(labels, ", ") + "}" }

func bindingList(names []string) string {
	if len(names) == 0 {
		return "nothing"
	}
	return "{" + strings.Join(names, ", ") + "}"
}

// variantSet is a dense set of variant indexes in declaration order.
type variantSet []uint64

func newVariantSet(size int) variantSet { return make(variantSet, (size+63)/64) }
func (s variantSet) add(variant int)    { s[variant/64] |= 1 << (variant % 64) }
func (s variantSet) has(variant int) bool {
	return variant >= 0 && variant/64 < len(s) && s[variant/64]&(1<<(variant%64)) != 0
}
func (s variantSet) count() int {
	total := 0
	for _, word := range s {
		for ; word != 0; word &= word - 1 {
			total++
		}
	}
	return total
}

// matchCoverage answers usefulness questions over the arms admitted so far
// (Maranget-style specialization over closed constructor sets; no wildcard
// rows exist under the closed-data policy). Variants that select exactly the
// same live arms at a subject lead to identical subproblems, so each such
// class is explored once. All questions share one work budget. Coverage
// diagnostics are staged as claims and published only if analysis completes.
type matchCoverage struct {
	widths    []int
	rows      [][]variantSet
	work      int
	exhausted bool
	claims    []Diagnostic
}

func (m *matchCoverage) claim(code, message string, span Span) {
	m.claims = append(m.claims, Diagnostic{Code: code, Message: message, Span: span})
}

func newMatchCoverage(subjects []matchPlanSubject) *matchCoverage {
	widths := make([]int, len(subjects))
	for index, subject := range subjects {
		widths[index] = len(subject.variants)
	}
	return &matchCoverage{widths: widths}
}

func (m *matchCoverage) spend(work int) bool {
	m.work += work
	if m.work > maxMatchCoverageWork {
		m.exhausted = true
	}
	return !m.exhausted
}

func (m *matchCoverage) allRows() []int {
	live := make([]int, len(m.rows))
	for index := range live {
		live[index] = index
	}
	return live
}

// reachable reports whether some combination admitted by query is selected
// by no earlier arm. An exhausted budget reports false; callers check it.
func (m *matchCoverage) reachable(query []variantSet) bool {
	return m.useful(m.allRows(), 0, query)
}

func (m *matchCoverage) useful(live []int, column int, query []variantSet) bool {
	if !m.spend(1) {
		return false
	}
	if column == len(m.widths) {
		return len(live) == 0
	}
	for _, class := range m.classes(live, column, query[column]) {
		if m.exhausted {
			return false
		}
		if m.useful(class.rows, column+1, query) {
			return true
		}
	}
	return false
}

// missing returns uncovered combinations in declaration order, at most
// maxReportedMissingMatchArms of them, and whether more exist. Variants are
// visited in declaration order so the cutoff keeps the earliest witnesses;
// once one variant of a class proves its subproblem covered, the class's
// other variants are skipped because they reach the identical subproblem.
func (m *matchCoverage) missing() ([][]int, bool) {
	full := make([]variantSet, len(m.widths))
	for index, width := range m.widths {
		full[index] = newVariantSet(width)
		for variant := range width {
			full[index].add(variant)
		}
	}
	var found [][]int
	more := false
	prefix := make([]int, len(m.widths))
	var walk func(live []int, column int)
	walk = func(live []int, column int) {
		if more || !m.spend(1) {
			return
		}
		if column == len(m.widths) {
			if len(live) > 0 {
				return
			}
			if len(found) == maxReportedMissingMatchArms {
				more = true
				return
			}
			found = append(found, slices.Clone(prefix))
			return
		}
		classes := m.classes(live, column, full[column])
		if m.exhausted {
			return
		}
		classOf := make([]int, m.widths[column])
		for index, class := range classes {
			for _, variant := range class.variants {
				classOf[variant] = index
			}
		}
		covered := make([]bool, len(classes))
		for variant := range m.widths[column] {
			if more || m.exhausted {
				return
			}
			class := classOf[variant]
			if covered[class] {
				continue
			}
			before := len(found)
			prefix[column] = variant
			walk(classes[class].rows, column+1)
			covered[class] = len(found) == before && !more && !m.exhausted
		}
	}
	walk(m.allRows(), 0)
	if m.exhausted {
		return nil, false
	}
	return found, more
}

type matchClass struct {
	variants []int
	rows     []int
}

// classes partitions the queried variants of one subject by the live arms
// that admit them, preserving declaration order of each class's first variant.
func (m *matchCoverage) classes(live []int, column int, query variantSet) []matchClass {
	var classes []matchClass
	index := map[string]int{}
	for variant := range m.widths[column] {
		if !query.has(variant) {
			continue
		}
		if !m.spend(len(live)) {
			return nil
		}
		key := make([]byte, (len(live)+7)/8)
		var rows []int
		for position, row := range live {
			if m.rows[row][column].has(variant) {
				key[position/8] |= 1 << (position % 8)
				rows = append(rows, row)
			}
		}
		if existing, ok := index[string(key)]; ok {
			classes[existing].variants = append(classes[existing].variants, variant)
			continue
		}
		index[string(key)] = len(classes)
		classes = append(classes, matchClass{variants: []int{variant}, rows: rows})
	}
	return classes
}
