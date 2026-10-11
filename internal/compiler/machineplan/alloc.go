package machineplan

// Charged construction for machine analysis.
//
// Analysis builds three kinds of program-sized values: narrowings (variant
// sets), lists (plan rows, paths, transition lists) and text (IDs and
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
	"unsafe"
)

// declarationUnits is a declaration's fixed state, charged as bytes before the
// plan is allocated: the plan, its copy into the result, the declaration's
// check and the empty maps run allocates.
func declarationUnits[V any]() int {
	return (2*int(unsafe.Sizeof(Plan[V]{})) + int(unsafe.Sizeof(check[V]{})) + declarationMapBytes + textUnit - 1) / textUnit
}

// declarationMapBytes bounds the maps every declaration creates (its report
// set, transition keys, indexes and narrowing memos) before they hold an
// entry: a runtime map header and its first bucket each.
const declarationMapBytes = 8 * 256

// budget is the module-wide budget every declaration also charges, and the
// module-wide memo of projections that do not depend on the declaration: a
// function, enum or type bound by many declarations is projected once.
type budget[V any] struct {
	in        *Input[V]
	report    func(Diagnostic)
	work      int
	exhausted bool

	costs         costs
	displays      map[TypeRef]string
	layouts       map[TypeRef]*Layout
	dataRefs      map[*Enum]DataRef[V]
	values        map[TypeRef]V
	functions     map[functionKey]BoundFunction[V]
	contributions map[[2]*Function]rows
}

type functionKey struct {
	role     string
	function *Function
}

type rows struct {
	failures, requirements []RowContribution
}

// text concatenates parts after charging their bytes.
func (m *check[V]) text(parts ...string) (string, bool) {
	n := 0
	for _, part := range parts {
		n += len(part)
	}
	if !m.charge(TextUnits(n)) {
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
func (m *check[V]) join(separator string, parts []string) (string, bool) {
	n := 0
	for i, part := range parts {
		if i > 0 {
			n += len(separator)
		}
		n += len(part)
	}
	if !m.charge(TextUnits(n)) {
		return "", false
	}
	return strings.Join(parts, separator), true
}

// textf is fmt.Sprintf after charging an upper bound of its output. Arguments
// are strings and integers only; anything else is a programming error.
func (m *check[V]) textf(format string, args ...any) (string, bool) {
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
	if !m.charge(TextUnits(n)) {
		return "", false
	}
	return fmt.Sprintf(format, args...), true
}

// reportf reports a diagnostic whose message is charged text. When the budget
// cannot afford the message, the declaration is already refused with EF142.
func (m *check[V]) reportf(code string, span Span, format string, args ...any) {
	if message, ok := m.textf(format, args...); ok {
		m.report(code, message, span)
	}
}

// display is the checker's type display, memoized for the module and charged
// by what building it costs the checker. A refused analysis displays nothing.
func (m *check[V]) display(id TypeRef) string {
	if m.refused {
		return ""
	}
	if text, ok := m.shared.displays[id]; ok {
		return text
	}
	if !m.charge(TextUnits(m.shared.costs.displayCost(&m.in.Types, id).bytes)) {
		return ""
	}
	text, ok := m.in.Types.Displays[id]
	if !ok {
		m.unprepared()
		return ""
	}
	if m.shared.displays == nil {
		m.shared.displays = map[TypeRef]string{}
	}
	m.shared.displays[id] = text
	return text
}

// layout reads the fields or variants of a record, enum or application once
// per module. Resolution is charged first at the checker's cost of resolving
// it (cost.go); the checker resolved it while preparing the input.
func (m *check[V]) layout(id TypeRef) (*Layout, bool) {
	if layout, ok := m.shared.layouts[id]; ok {
		return layout, true
	}
	units, ok := m.shared.costs.layoutUnits(&m.in.Types, id, m.spend)
	if !ok || !m.charge(units) {
		return nil, false
	}
	layout := m.in.Types.Layouts[id]
	if layout == nil {
		m.unprepared()
		return nil, false
	}
	if m.shared.layouts == nil {
		m.shared.layouts = map[TypeRef]*Layout{}
	}
	m.shared.layouts[id] = layout
	return layout, true
}

func (m *check[V]) variantsOf(id TypeRef) (*Enum, []Variant, bool) {
	layout, ok := m.layout(id)
	if !ok || !layout.Sum {
		return nil, nil, false
	}
	return layout.Enum, layout.Variants, true
}

func (m *check[V]) fieldsOf(id TypeRef) ([]Field, bool) {
	layout, ok := m.layout(id)
	if !ok || !layout.Record {
		return nil, false
	}
	return layout.Fields, true
}

// set is a canonical narrowing of one enum: the sorted declaration positions
// of the variants a subject may have here. Equal narrowings are one value. A
// nil *set is unnarrowed: every declared variant.
type set struct {
	id        int
	positions []int32
}

// admits reports whether position i is in the narrowing.
func (v *set) admits(i int32) bool {
	if v == nil {
		return true
	}
	_, found := slices.BinarySearch(v.positions, i)
	return found
}

func (v *set) size(enum *Enum) int {
	if v == nil {
		return len(enum.Variants)
	}
	return len(v.positions)
}

func (v *set) ordinal() int {
	if v == nil {
		return 0
	}
	return v.id
}

// names lists the narrowing's variant names in declaration order. The caller
// has charged its size.
func (v *set) names(enum *Enum) []string {
	if v == nil {
		return slices.Clone(enum.Variants)
	}
	out := make([]string, len(v.positions))
	for j, i := range v.positions {
		out[j] = enum.Variants[i]
	}
	return out
}

// intern returns the canonical narrowing for sorted, distinct positions. The
// caller has charged the positions; interning charges the key it stores.
func (m *check[V]) intern(enum *Enum, positions []int32) (*set, bool) {
	if !m.charge(1 + len(positions)) {
		return nil, false
	}
	key := make([]byte, 0, 2*len(positions))
	for _, i := range positions {
		key = binary.AppendUvarint(key, uint64(i))
	}
	table := m.sets[enum]
	if interned, ok := table[string(key)]; ok {
		return interned, true
	}
	if table == nil {
		if m.sets == nil {
			m.sets = map[*Enum]map[string]*set{}
		}
		table = map[string]*set{}
		m.sets[enum] = table
	}
	m.setCount++
	interned := &set{id: m.setCount, positions: positions}
	table[string(key)] = interned
	return interned, true
}

// empty is the canonical narrowing with no variant.
func (m *check[V]) empty(enum *Enum) (*set, bool) { return m.intern(enum, nil) }

type refinement struct {
	enum  *Enum
	set   *set
	arm   *Arm
	index int
}

// refine intersects a narrowing with one match cell. The cell's width is
// charged before its positions are collected; the result is memoized, so an
// arm walked again under the same narrowing allocates nothing.
func (m *check[V]) refine(enum *Enum, narrowed *set, arm *Arm, index int) (*set, bool) {
	key := refinement{enum: enum, set: narrowed, arm: arm, index: index}
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
	for _, name := range cell {
		if i, ok := variants[name]; ok && narrowed.admits(int32(i)) {
			positions = append(positions, int32(i))
		}
	}
	slices.Sort(positions)
	out, ok := m.intern(enum, slices.Compact(positions))
	if !ok {
		return nil, false
	}
	if m.refinements == nil {
		m.refinements = map[refinement]*set{}
	}
	m.refinements[key] = out
	return out, true
}

// subset reports whether every variant of w is in v; both are narrowed. The
// comparison is charged by both widths and memoized.
func (m *check[V]) subset(w, v *set) bool {
	if w == v {
		return true
	}
	key := [2]*set{w, v}
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
		m.subsets = map[[2]*set]bool{}
	}
	m.subsets[key] = result
	return result
}

// variantIndex maps an enum's variant names to declaration positions. It is
// built and charged once per enum and declaration.
func (m *check[V]) variantIndex(enum *Enum) map[string]int {
	if index, ok := m.indexes[enum]; ok {
		return index
	}
	if !m.charge(1 + len(enum.Variants)) {
		return nil
	}
	index := make(map[string]int, len(enum.Variants))
	for i, variant := range enum.Variants {
		index[variant] = i
	}
	if m.indexes == nil {
		m.indexes = map[*Enum]map[string]int{}
	}
	m.indexes[enum] = index
	return index
}

// position resolves a variant name of enum.
func (m *check[V]) position(enum *Enum, name string) (int32, bool) {
	index := m.variantIndex(enum)
	i, ok := index[name]
	return int32(i), ok
}

// transitionID is positional: the declaration's ordinal and the transition's
// index. The machine's source name is stored once, in the plan.
func (m *check[V]) transitionID(index int) string {
	return strconv.Itoa(m.ordinal) + ":transition:" + strconv.Itoa(index)
}

// dataRef projects a state, event or outcome enum once per module. Its
// variant list and identity text are charged before they are built.
func (m *check[V]) dataRef(id TypeRef, enum *Enum) (DataRef[V], bool) {
	if ref, ok := m.shared.dataRefs[enum]; ok {
		return ref, true
	}
	if !m.charge(1 + len(enum.Variants) + TextUnits(2*len(enum.Name)+2*len(m.in.Module)+192)) {
		return DataRef[V]{}, false
	}
	projection, prepared := m.in.Data[id]
	projected, ok := m.project(projection, prepared)
	if !ok {
		return DataRef[V]{}, false
	}
	ref := DataRef[V]{Name: enum.Name, Identity: enum.Qualifier, Variants: (*set)(nil).names(enum), Type: projected}
	if m.shared.dataRefs == nil {
		m.shared.dataRefs = map[*Enum]DataRef[V]{}
	}
	m.shared.dataRefs[enum] = ref
	return ref, true
}

// project reads the checker's compact projection of a checked value. The
// checker projects only a value whose contract and result already have public
// IDs (declaration publication interns every signature type before machines
// are checked) and whose success display is a retained name (a payload that
// displays by building text is refused before projection). A value that would
// break the requirement is refused, never projected unbounded.
func (m *check[V]) project(p Projection[V], prepared bool) (V, bool) {
	var zero V
	if !m.charge(1 + TextUnits(p.IdentityBytes+len(m.in.Module))) {
		return zero, false
	}
	if !prepared {
		m.unprepared()
		return zero, false
	}
	if !p.Published {
		m.reportf(BudgetCode, m.machine.Span, "machine %s projects a type the checker has not published; no plan is published", m.machine.Name)
		m.refused = true
		return zero, false
	}
	return p.Value, true
}

// value projects a data type once per module.
func (m *check[V]) value(id TypeRef) (V, bool) {
	if value, ok := m.shared.values[id]; ok {
		return value, true
	}
	projection, prepared := m.in.Data[id]
	value, ok := m.project(projection, prepared)
	if !ok {
		return value, false
	}
	if m.shared.values == nil {
		m.shared.values = map[TypeRef]V{}
	}
	m.shared.values[id] = value
	return value, true
}

// function projects one bound function once per module. Its failure and
// service rows are charged by width, and its contract value by its
// parameters.
func (m *check[V]) function(role string, f *Function) (BoundFunction[V], bool) {
	key := functionKey{role: role, function: f}
	if projected, ok := m.shared.functions[key]; ok {
		return projected, true
	}
	kind := "pure"
	failures, services := []string{}, []string{}
	if f.Effect {
		kind = "effect"
		if !m.charge(2 + 2*(len(f.Failures)+len(f.Services))) {
			return BoundFunction[V]{}, false
		}
		failures, services = slices.Clone(f.Failures), slices.Clone(f.Services)
	}
	if !m.charge(1 + len(f.Params) + TextUnits(nodeTextBytes+argTextBytes*len(f.Params))) {
		return BoundFunction[V]{}, false
	}
	projection, prepared := m.in.Contracts[f]
	contract, ok := m.project(projection, prepared)
	if !ok {
		return BoundFunction[V]{}, false
	}
	projected := BoundFunction[V]{Role: role, Name: f.Name, Identity: f.Identity, Span: f.Span, Kind: kind, Failures: failures, Services: services, Contract: contract}
	if m.shared.functions == nil {
		m.shared.functions = map[functionKey]BoundFunction[V]{}
	}
	m.shared.functions[key] = projected
	return projected, true
}

// rows builds the terminal-failure and requirement contributions of the bound
// functions once per (step, complete) pair, charged by width first.
func (m *check[V]) rows(step, complete *Function, functions []BoundFunction[V]) (rows, bool) {
	key := [2]*Function{step, complete}
	if out, ok := m.shared.contributions[key]; ok {
		return out, true
	}
	width := 0
	for _, f := range functions {
		width += len(f.Failures) + len(f.Services)
	}
	if !m.charge(1 + 3*width) {
		return rows{}, false
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
	out := rows{failures: contributions(failures), requirements: contributions(services)}
	if m.shared.contributions == nil {
		m.shared.contributions = map[[2]*Function]rows{}
	}
	m.shared.contributions[key] = out
	return out, true
}

func contributions(rows map[string][]string) []RowContribution {
	out := make([]RowContribution, 0, len(rows))
	for name, roles := range rows {
		sorted := append([]string{}, roles...)
		slices.Sort(sorted)
		out = append(out, RowContribution{Name: name, Roles: slices.Compact(sorted)})
	}
	slices.SortFunc(out, func(a, b RowContribution) int { return strings.Compare(a.Name, b.Name) })
	return out
}
