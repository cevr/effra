package compiler

// Charged construction for machine analysis.
//
// Machine analysis builds three kinds of program-sized values: narrowings
// (variant sets), lists (plan rows, paths, transition lists) and text (IDs and
// diagnostic messages). Each is built here, and each builder charges its width
// before it allocates, text by bytes as well as by count. A narrowing, list or
// string whose size the program controls cannot be built without paying for
// it first, so a missed allocation site cannot appear at a call site: call
// sites have nothing to allocate with but these builders. The budget is
// exhausted by refusal (EF142), never by a fallback.
//
// Narrowings are canonical: sorted variant positions interned per enum, so
// equal narrowings share storage and compare by pointer. Refining a narrowing
// by a match cell is memoized by (narrowing, cell), so walking one arm again
// under the same narrowing reuses the earlier result instead of copying it.

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// machineTextUnit is the number of generated text bytes one budget unit buys.
// Text is charged by bytes because copying one identifier into many generated
// strings multiplies source size without adding expressions to walk.
const machineTextUnit = 64

func machineTextUnits(bytes int) int { return (bytes + machineTextUnit - 1) / machineTextUnit }

// machineAnalysisProbe, when a test sets it, brackets one module's machine
// analysis so the test can compare measured allocation with the units the
// analysis charged. It is nil in production.
var machineAnalysisProbe func(done bool, units int)

// machineBudget is the module-wide budget every declaration also charges, and
// the module-wide memo of projections that do not depend on the declaration:
// a function, enum or type bound by many declarations is projected once.
type machineBudget struct {
	work      int
	exhausted bool

	displays      map[TypeID]string
	dataRefs      map[*Enum]MachineDataRef
	values        map[TypeID]ValueType
	functions     map[machineFunctionKey]MachineFunction
	contributions map[[2]*Function]machineRows
}

type machineFunctionKey struct {
	role     string
	function *Function
}

type machineRows struct {
	failures, requirements []MachineRowContribution
}

// text concatenates parts after charging their bytes.
func (m *machineCheck) text(parts ...string) (string, bool) {
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	if !m.charge(machineTextUnits(n)) {
		return "", false
	}
	var b strings.Builder
	b.Grow(n)
	for _, part := range parts {
		b.WriteString(part)
	}
	return b.String(), true
}

// join is strings.Join after charging the joined bytes.
func (m *machineCheck) join(separator string, parts []string) (string, bool) {
	n := 0
	for i, part := range parts {
		if i > 0 {
			n += len(separator)
		}
		n += len(part)
	}
	if !m.charge(machineTextUnits(n)) {
		return "", false
	}
	return strings.Join(parts, separator), true
}

// textf is fmt.Sprintf after charging an upper bound of its output. Arguments
// are strings and integers only; anything else is a programming error.
func (m *machineCheck) textf(format string, args ...any) (string, bool) {
	n := len(format)
	for _, arg := range args {
		switch value := arg.(type) {
		case string:
			n += len(value)
		case int:
			n += 20
		default:
			panic(fmt.Sprintf("machine text argument %T", arg))
		}
	}
	if !m.charge(machineTextUnits(n)) {
		return "", false
	}
	return fmt.Sprintf(format, args...), true
}

// reportf reports a diagnostic whose message is charged text. When the budget
// cannot afford the message, the declaration is already refused with EF142.
func (m *machineCheck) reportf(code string, span Span, format string, args ...any) {
	if message, ok := m.textf(format, args...); ok {
		m.report(code, message, span)
	}
}

// display is the checker's type display, memoized for the module and charged
// by its bytes the first time. A refused analysis displays nothing.
func (m *machineCheck) display(id TypeID) string {
	if m.refused {
		return ""
	}
	if text, ok := m.shared.displays[id]; ok {
		return text
	}
	text := m.c.displayTypeID(id)
	if !m.charge(machineTextUnits(len(text))) {
		return ""
	}
	if m.shared.displays == nil {
		m.shared.displays = map[TypeID]string{}
	}
	m.shared.displays[id] = text
	return text
}

// layoutCharge pays for what the checker allocates to resolve the fields or
// variants of a type: the declaration qualifier it rebuilds for a nominal
// record or enum, and the instantiated copy of an application's template.
func (m *machineCheck) layoutCharge(id TypeID) bool {
	n := m.c.node(id)
	if n == nil {
		return true
	}
	switch n.Kind {
	case "record", "enum":
		return m.charge(machineTextUnits(len(n.Declaration) + 64))
	case "application":
		template := m.c.templates[n.Declaration]
		if template == nil {
			return m.charge(1)
		}
		width := len(template.Fields) + len(template.Variants)
		for _, variant := range template.Variants {
			width += len(variant.Fields)
		}
		return m.charge(1 + 4*width)
	}
	return true
}

func (m *machineCheck) variantsOf(id TypeID) (*Enum, []Variant, bool) {
	if n := m.c.node(id); n == nil || n.Kind != "enum" && n.Kind != "application" || !m.layoutCharge(id) {
		return nil, nil, false
	}
	return m.c.checkedVariants(id)
}

func (m *machineCheck) fieldsOf(id TypeID) ([]Field, bool) {
	if n := m.c.node(id); n == nil || n.Kind != "record" && n.Kind != "application" || !m.layoutCharge(id) {
		return nil, false
	}
	return m.c.checkedFields(id)
}

// machineSet is a canonical narrowing of one enum: the sorted declaration
// positions of the variants a subject may have here. Equal narrowings are one
// value. A nil *machineSet is unnarrowed: every declared variant.
type machineSet struct {
	id        int
	positions []int32
}

// admits reports whether position i is in the narrowing.
func (v *machineSet) admits(i int32) bool {
	if v == nil {
		return true
	}
	_, found := slices.BinarySearch(v.positions, i)
	return found
}

func (v *machineSet) size(enum *Enum) int {
	if v == nil {
		return len(enum.Variants)
	}
	return len(v.positions)
}

func (v *machineSet) ordinal() int {
	if v == nil {
		return 0
	}
	return v.id
}

// names lists the narrowing's variant names in declaration order. The caller
// has charged its size.
func (v *machineSet) names(enum *Enum) []string {
	if v == nil {
		out := make([]string, len(enum.Variants))
		for i, variant := range enum.Variants {
			out[i] = variant.Name
		}
		return out
	}
	out := make([]string, len(v.positions))
	for j, i := range v.positions {
		out[j] = enum.Variants[i].Name
	}
	return out
}

// intern returns the canonical narrowing for sorted, distinct positions. The
// caller has charged the positions; interning charges the key it stores.
func (m *machineCheck) intern(enum *Enum, positions []int32) (*machineSet, bool) {
	if !m.charge(1 + len(positions)) {
		return nil, false
	}
	key := make([]byte, 0, 2*len(positions))
	for _, i := range positions {
		key = binary.AppendUvarint(key, uint64(i))
	}
	table := m.sets[enum]
	if set, ok := table[string(key)]; ok {
		return set, true
	}
	if table == nil {
		if m.sets == nil {
			m.sets = map[*Enum]map[string]*machineSet{}
		}
		table = map[string]*machineSet{}
		m.sets[enum] = table
	}
	m.setCount++
	set := &machineSet{id: m.setCount, positions: positions}
	table[string(key)] = set
	return set, true
}

// empty is the canonical narrowing with no variant.
func (m *machineCheck) empty(enum *Enum) (*machineSet, bool) { return m.intern(enum, nil) }

type machineRefinement struct {
	enum  *Enum
	set   *machineSet
	arm   *MatchArm
	index int
}

// refine intersects a narrowing with one match cell. The cell's width is
// charged before its positions are collected; the result is memoized, so an
// arm walked again under the same narrowing allocates nothing.
func (m *machineCheck) refine(enum *Enum, set *machineSet, arm *MatchArm, index int) (*machineSet, bool) {
	key := machineRefinement{enum: enum, set: set, arm: arm, index: index}
	if out, ok := m.refinements[key]; ok {
		return out, true
	}
	cell := arm.Patterns[index]
	if !m.charge(1 + len(cell)) {
		return nil, false
	}
	variants := m.variantIndex(enum)
	if variants == nil {
		return nil, false
	}
	positions := make([]int32, 0, len(cell))
	for _, pattern := range cell {
		if i, ok := variants[pattern.VariantName]; ok && set.admits(int32(i)) {
			positions = append(positions, int32(i))
		}
	}
	slices.Sort(positions)
	out, ok := m.intern(enum, slices.Compact(positions))
	if !ok {
		return nil, false
	}
	if m.refinements == nil {
		m.refinements = map[machineRefinement]*machineSet{}
	}
	m.refinements[key] = out
	return out, true
}

// subset reports whether every variant of w is in v; both are narrowed. The
// comparison is charged by both widths and memoized.
func (m *machineCheck) subset(w, v *machineSet) bool {
	if w == v {
		return true
	}
	key := [2]*machineSet{w, v}
	if result, ok := m.subsets[key]; ok {
		return result
	}
	if !m.charge(1 + len(w.positions) + len(v.positions)) {
		return false
	}
	result, j := true, 0
	for _, i := range w.positions {
		for j < len(v.positions) && v.positions[j] < i {
			j++
		}
		if j == len(v.positions) || v.positions[j] != i {
			result = false
			break
		}
	}
	if m.subsets == nil {
		m.subsets = map[[2]*machineSet]bool{}
	}
	m.subsets[key] = result
	return result
}

// variantIndex maps an enum's variant names to declaration positions. It is
// built and charged once per enum and declaration.
func (m *machineCheck) variantIndex(enum *Enum) map[string]int {
	if index, ok := m.indexes[enum]; ok {
		return index
	}
	if !m.charge(1 + len(enum.Variants)) {
		return nil
	}
	index := make(map[string]int, len(enum.Variants))
	for i, variant := range enum.Variants {
		index[variant.Name] = i
	}
	if m.indexes == nil {
		m.indexes = map[*Enum]map[string]int{}
	}
	m.indexes[enum] = index
	return index
}

// position resolves a variant name of enum.
func (m *machineCheck) position(enum *Enum, name string) (int32, bool) {
	index := m.variantIndex(enum)
	i, ok := index[name]
	return int32(i), ok
}

// transitionID is positional: the declaration's ordinal and the transition's
// index. The machine's source name is stored once, in the plan.
func (m *machineCheck) transitionID(index int) string {
	return strconv.Itoa(m.ordinal) + ":transition:" + strconv.Itoa(index)
}

// dataRef projects a state, event or outcome enum once per module. Its
// variant list and identity text are charged before they are built.
func (m *machineCheck) dataRef(id TypeID, enum *Enum) (MachineDataRef, bool) {
	if ref, ok := m.shared.dataRefs[enum]; ok {
		return ref, true
	}
	if !m.charge(1 + len(enum.Variants) + machineTextUnits(2*len(enum.Name)+2*len(currentModuleIdentity)+192)) {
		return MachineDataRef{}, false
	}
	ref := MachineDataRef{Name: enum.Name, Identity: m.c.declarationQualifier("enum", enum.Name), Variants: (*machineSet)(nil).names(enum), Type: m.c.projectCheckedBase(m.c.checkedDataID(id, nil, nil))}
	if m.shared.dataRefs == nil {
		m.shared.dataRefs = map[*Enum]MachineDataRef{}
	}
	m.shared.dataRefs[enum] = ref
	return ref, true
}

// value projects a data type once per module, charged by its display.
func (m *machineCheck) value(id TypeID) (ValueType, bool) {
	if value, ok := m.shared.values[id]; ok {
		return value, true
	}
	display := m.display(id)
	if m.refused || !m.charge(machineTextUnits(len(display)+len(currentModuleIdentity)+192)) {
		return ValueType{}, false
	}
	value := m.c.projectCheckedBase(m.c.checkedDataID(id, nil, nil))
	if m.shared.values == nil {
		m.shared.values = map[TypeID]ValueType{}
	}
	m.shared.values[id] = value
	return value, true
}

// function projects one bound function once per module. Its failure and
// service rows are charged by width before they are copied, and its contract
// by the display of its type.
func (m *machineCheck) function(role string, f *Function) (MachineFunction, bool) {
	key := machineFunctionKey{role: role, function: f}
	if projected, ok := m.shared.functions[key]; ok {
		return projected, true
	}
	kind := "pure"
	failures, services := []string{}, []string{}
	if f.Effect {
		kind = "effect"
		failureRow, serviceRow := m.c.retainedRowLabels(f.failureID), m.c.retainedRowLabels(f.serviceID)
		if !m.charge(2 + 2*(len(failureRow)+len(serviceRow))) {
			return MachineFunction{}, false
		}
		failures, services = normalized(failureRow), normalized(serviceRow)
	}
	contract := m.c.checkedFunction(f, true, false)
	display := m.display(contract.resultID())
	if m.refused || !m.charge(machineTextUnits(len(display)+len(f.Identity)+192)) {
		return MachineFunction{}, false
	}
	projected := MachineFunction{Role: role, Name: f.Name, Identity: f.Identity, Span: f.Span, Kind: kind, Failures: failures, Services: services, Contract: m.c.projectCheckedBase(contract)}
	if m.shared.functions == nil {
		m.shared.functions = map[machineFunctionKey]MachineFunction{}
	}
	m.shared.functions[key] = projected
	return projected, true
}

// rows builds the terminal-failure and requirement contributions of the bound
// functions once per (step, complete) pair, charged by width first.
func (m *machineCheck) rows(step, complete *Function, functions []MachineFunction) (machineRows, bool) {
	key := [2]*Function{step, complete}
	if rows, ok := m.shared.contributions[key]; ok {
		return rows, true
	}
	width := 0
	for _, f := range functions {
		width += len(f.Failures) + len(f.Services)
	}
	if !m.charge(1 + 3*width) {
		return machineRows{}, false
	}
	failures, services := map[string][]string{}, map[string][]string{}
	for _, f := range functions {
		for _, name := range f.Failures {
			failures[name] = append(failures[name], f.Role)
		}
		for _, name := range f.Services {
			services[name] = append(services[name], f.Role)
		}
	}
	rows := machineRows{failures: machineContributions(failures), requirements: machineContributions(services)}
	if m.shared.contributions == nil {
		m.shared.contributions = map[[2]*Function]machineRows{}
	}
	m.shared.contributions[key] = rows
	return rows, true
}

func machineContributions(rows map[string][]string) []MachineRowContribution {
	out := make([]MachineRowContribution, 0, len(rows))
	for name, roles := range rows {
		out = append(out, MachineRowContribution{Name: name, Roles: normalized(roles)})
	}
	slices.SortFunc(out, func(a, b MachineRowContribution) int { return strings.Compare(a.Name, b.Name) })
	return out
}
