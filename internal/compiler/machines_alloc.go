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
	"unsafe"
)

// machineTextUnit is the number of generated text bytes one budget unit buys.
// Text is charged by bytes because copying one identifier into many generated
// strings multiplies source size without adding expressions to walk.
const machineTextUnit = 64

func machineTextUnits(bytes int) int { return (bytes + machineTextUnit - 1) / machineTextUnit }

// machineDeclarationUnits is a declaration's fixed state, charged as bytes
// before the plan is allocated: the plan, its copy into the result, the
// declaration's machineCheck and the empty maps run allocates.
const machineDeclarationUnits = (2*int(unsafe.Sizeof(MachinePlan{})) + int(unsafe.Sizeof(machineCheck{})) + machineDeclarationMapBytes + machineTextUnit - 1) / machineTextUnit

// machineDeclarationMapBytes bounds the maps every declaration creates (its
// report set, transition keys, indexes and narrowing memos) before they hold
// an entry: a runtime map header and its first bucket each.
const machineDeclarationMapBytes = 8 * 256

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
	displayCosts  map[TypeID]machineDisplay
	texts         map[TypeID]machineTypeText
	layouts       map[TypeID]machineLayout
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
// by what displayTypeID allocates to build it. A refused analysis displays
// nothing.
func (m *machineCheck) display(id TypeID) string {
	if m.refused {
		return ""
	}
	if text, ok := m.shared.displays[id]; ok {
		return text
	}
	if !m.charge(machineTextUnits(m.displayCost(id).bytes)) {
		return ""
	}
	text := m.c.displayTypeID(id)
	if m.shared.displays == nil {
		m.shared.displays = map[TypeID]string{}
	}
	m.shared.displays[id] = text
	return text
}

// machineDisplay is the length of the checker's display of one type and the
// bytes displayTypeID allocates to build it.
type machineDisplay struct{ length, bytes int }

// displayCost bounds displayTypeID. Wrapper kinds (fiber, goResult, goValues,
// recipe) display their children and copy the children's text into their own
// at every layer, so the build cost grows with nesting depth times the text
// below, not with the text itself. Each distinct node is walked once and
// memoized for the module; children are summed as often as they occur, as
// displayTypeID rebuilds them. The walk needs no charge of its own: only
// wrapper nodes have children, and each adds at least 16 bytes to the cost
// the caller charges for, which pays for its memo entry.
func (m *machineCheck) displayCost(id TypeID) machineDisplay {
	if cost, ok := m.shared.displayCosts[id]; ok {
		return cost
	}
	if m.shared.displayCosts == nil {
		m.shared.displayCosts = map[TypeID]machineDisplay{}
	}
	m.shared.displayCosts[id] = machineDisplay{}
	cost := m.nodeDisplay(id)
	cost.length, cost.bytes = min(cost.length, machineTextCap), min(cost.bytes, machineTextCap)
	m.shared.displayCosts[id] = cost
	return cost
}

// nodeDisplay follows displayTypeID case by case.
func (m *machineCheck) nodeDisplay(id TypeID) machineDisplay {
	n := m.c.node(id)
	if n == nil {
		return machineDisplay{length: len("invalid")}
	}
	switch n.Kind {
	case "never", "invalid":
		return machineDisplay{length: len(n.Kind)}
	case "fiber", "goResult":
		if len(n.Args) == 1 {
			child := m.displayCost(n.Args[0])
			length := len("GoResult:") + child.length
			// One concatenation of the prefix and the child's text.
			return machineDisplay{length: length, bytes: child.bytes + length}
		}
	case "goValues":
		// A component slice, then strings.Join and the parenthesised copy.
		cost := machineDisplay{length: 2, bytes: 16 * len(n.Args)}
		for i, arg := range n.Args {
			child := m.displayCost(arg)
			if i > 0 {
				cost.length += 2
			}
			cost.length = min(cost.length+child.length, machineTextCap)
			cost.bytes = min(cost.bytes+child.bytes, machineTextCap)
		}
		cost.bytes += 2 * cost.length
		return cost
	case "provider":
		length := len("provider:") + len(n.Name)
		return machineDisplay{length: length, bytes: length}
	case "recipe":
		child := m.displayCost(n.Result)
		// recipeDisplay copies each row (rowLabels, then normalized), joins
		// it, and concatenates the growing text: twice without rows, up to
		// four times with them.
		length, labels, copies := len("Effect<>")+child.length, 0, 2
		failures, services := m.c.retainedRowLabels(n.FailureRow), m.c.retainedRowLabels(n.ServiceRow)
		for _, row := range [2][]string{failures, services} {
			if len(failures) == 0 && len(services) == 0 {
				break
			}
			copies = 5
			length += len(", {}")
			for _, label := range row {
				length = min(length+len(label)+2, machineTextCap)
				labels++
			}
		}
		return machineDisplay{length: length, bytes: child.bytes + copies*length + 48*labels + 64}
	}
	return machineDisplay{length: len(n.Name)}
}

// Per-node text bound: the fixed fields of an interning key or public-ID key,
// and the bytes each name, argument and row label adds to one.
const (
	machineNodeText  = 128
	machineArgText   = 24
	machineLabelText = 24
	// machineTextCap is more text than the module budget can buy, so a capped
	// bound is still refused.
	machineTextCap = (maxMachineModuleWork + 1) * machineTextUnit
)

// machineReachCap is one more node than templateDataArgument and
// substituteCanonical memoize before they give up, so a capped count bounds
// either walk.
const machineReachCap = 4098

// machineWalkNodes is the number of template-argument nodes one budget unit
// buys: templateDataArgument allocates about 26 bytes per node it memoizes.
const machineWalkNodes = 8

// machineTypeText is what the checker can build or walk from one type, summed
// over the tree the type unfolds to: the bytes of its keys, its nodes through
// arguments and results (capped at machineReachCap), and the
// template-argument validations its applications run, by parameter kind.
type machineTypeText struct {
	bytes, nodes, typeWalks, callableWalks int
}

// typeText bounds the key text the checker builds from the type under id (its
// public-ID key and the interning keys a template substitution rebuilds;
// displays are bounded by displayCost) and the nodes a substitution walks in
// it. Keys and walks unfold a shared DAG into a tree, so the bound sums
// children as often as they occur, which over-counts shared nodes; each
// distinct node is walked once, charged one unit and memoized for the module,
// so an exponential unfolding is refused by its charge rather than walked.
func (m *machineCheck) typeText(id TypeID) (machineTypeText, bool) {
	if text, ok := m.shared.texts[id]; ok {
		return text, true
	}
	if !m.spend() {
		return machineTypeText{}, false
	}
	if m.shared.texts == nil {
		m.shared.texts = map[TypeID]machineTypeText{}
	}
	// A back edge measures nothing; the checker displays it as a cycle. A
	// refused walk removes its partial bounds, so no later declaration of the
	// module reads one.
	m.shared.texts[id] = machineTypeText{}
	text, ok := m.nodeText(id)
	if !ok {
		delete(m.shared.texts, id)
		return machineTypeText{}, false
	}
	m.shared.texts[id] = text
	return text, true
}

func (m *machineCheck) nodeText(id TypeID) (machineTypeText, bool) {
	n := m.c.node(id)
	text := machineTypeText{bytes: machineNodeText, nodes: 1}
	if n == nil {
		return text, true
	}
	text.bytes += 2*(len(n.Kind)+len(n.Name)+len(n.Declaration)+len(n.Mode)) + machineArgText*len(n.Args)
	for _, row := range [2]RowID{n.FailureRow, n.ServiceRow} {
		for _, label := range m.c.retainedRowLabels(row) {
			text.bytes = min(text.bytes+2*len(label)+machineLabelText, machineTextCap)
		}
	}
	if template := m.c.templates[n.Declaration]; n.Kind == "application" && template != nil {
		for _, parameter := range template.Parameters {
			if parameter.Kind == "type" {
				text.typeWalks++
			} else if parameter.Constraint != nil {
				text.callableWalks += len(parameter.Constraint.Parameters) + 1
			}
		}
	}
	for i := 0; i <= len(n.Args); i++ {
		child := n.Result
		if i < len(n.Args) {
			child = n.Args[i]
		} else if child == invalidTypeID {
			break
		}
		below, ok := m.typeText(child)
		if !ok {
			return machineTypeText{}, false
		}
		text.bytes = min(text.bytes+below.bytes, machineTextCap)
		text.nodes = min(text.nodes+below.nodes, machineReachCap)
		text.typeWalks = min(text.typeWalks+below.typeWalks, machineTextCap)
		text.callableWalks = min(text.callableWalks+below.callableWalks, machineTextCap)
	}
	return text, true
}

// machineLayout is the resolved fields or variants of one data type, shared
// by every declaration of the module.
type machineLayout struct {
	enum     *Enum
	fields   []Field
	variants []Variant
	record   bool
	sum      bool
}

// layout resolves the fields or variants of a record, enum or application
// once per module. Resolution is charged first: the declaration qualifier the
// checker rebuilds for a nominal type, and for an application the instantiated
// copy of its template plus, for every declared field type, the interning keys
// substitution builds and the template-argument walks it repeats.
func (m *machineCheck) layout(id TypeID) (machineLayout, bool) {
	if layout, ok := m.shared.layouts[id]; ok {
		return layout, true
	}
	n := m.c.node(id)
	if n == nil || n.Kind != "record" && n.Kind != "enum" && n.Kind != "application" {
		return machineLayout{}, false
	}
	units := 1 + machineTextUnits(len(n.Declaration)+64)
	if n.Kind == "application" {
		if template := m.c.templates[n.Declaration]; template != nil {
			// Substitution binds each template parameter to an argument of
			// this application, so a substituted field type has at most its
			// declared nodes plus the arguments' nodes.
			arguments := 0
			for _, arg := range n.Args {
				text, ok := m.typeText(arg)
				if !ok {
					return machineLayout{}, false
				}
				arguments = min(arguments+text.nodes, machineReachCap)
			}
			text, walked := 0, 0
			declared := func(fields []Field) bool {
				units += 1 + 4*len(fields)
				for _, field := range fields {
					if field.typeID == invalidTypeID {
						continue
					}
					declared, ok := m.typeText(field.typeID)
					if !ok {
						return false
					}
					text = min(text+declared.bytes, machineTextCap)
					// substituteCanonical runs once per field with a fresh
					// memo over the declared nodes, and every application it
					// rebuilds validates its arguments afresh: a type
					// parameter's walk covers the substituted argument, a
					// callable parameter's walks are capped by the checker.
					substituted := min(declared.nodes+arguments, machineReachCap)
					walked = min(walked+declared.nodes+declared.typeWalks*substituted+declared.callableWalks*machineReachCap, machineTextCap)
				}
				return true
			}
			if !declared(template.Fields) {
				return machineLayout{}, false
			}
			for _, variant := range template.Variants {
				if !declared(variant.Fields) {
					return machineLayout{}, false
				}
			}
			units += machineTextUnits(text) + (walked+machineWalkNodes-1)/machineWalkNodes
		}
	}
	if !m.charge(units) {
		return machineLayout{}, false
	}
	var layout machineLayout
	if n.Kind != "enum" {
		layout.fields, layout.record = m.c.checkedFields(id)
	}
	if !layout.record && n.Kind != "record" {
		layout.enum, layout.variants, layout.sum = m.c.checkedVariants(id)
	}
	if m.shared.layouts == nil {
		m.shared.layouts = map[TypeID]machineLayout{}
	}
	m.shared.layouts[id] = layout
	return layout, true
}

func (m *machineCheck) variantsOf(id TypeID) (*Enum, []Variant, bool) {
	layout, ok := m.layout(id)
	if !ok || !layout.sum {
		return nil, nil, false
	}
	return layout.enum, layout.variants, true
}

func (m *machineCheck) fieldsOf(id TypeID) ([]Field, bool) {
	layout, ok := m.layout(id)
	if !ok || !layout.record {
		return nil, false
	}
	return layout.fields, true
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
	projected, ok := m.project(m.c.checkedDataID(id, nil, nil))
	if !ok {
		return MachineDataRef{}, false
	}
	ref := MachineDataRef{Name: enum.Name, Identity: m.c.declarationQualifier("enum", enum.Name), Variants: (*machineSet)(nil).names(enum), Type: projected}
	if m.shared.dataRefs == nil {
		m.shared.dataRefs = map[*Enum]MachineDataRef{}
	}
	m.shared.dataRefs[enum] = ref
	return ref, true
}

// project is the checker's compact projection of a checked value. It builds
// no program-sized text, and it requires that: the contract and result
// already have public IDs (declaration publication interns every signature
// type before machines are checked), the success display is a retained name
// (a payload that displays by building text is refused before projection),
// row IDs are the rows' retained canonical IDs and row labels are shared. A
// value that would break the requirement is refused, never projected
// unbounded.
func (m *machineCheck) project(e checkedExpression) (ValueType, bool) {
	if !m.charge(1 + machineTextUnits(len(e.identity)+len(currentModuleIdentity))) {
		return ValueType{}, false
	}
	success := e.resultID()
	if e.kind() == checkedFiberValue {
		success = e.contractID()
	}
	retained := true
	if n := m.c.node(success); n != nil {
		switch n.Kind {
		case "fiber", "goResult", "goValues", "provider", "recipe":
			retained = false
		}
	}
	_, contract := m.c.typePublicIDs[e.contractID()]
	_, result := m.c.typePublicIDs[e.resultID()]
	if !contract || !result || !retained {
		m.reportf(machineBudgetCode, m.machine.Span, "machine %s projects a type the checker has not published; no plan is published", m.machine.Name)
		m.refused = true
		return ValueType{}, false
	}
	return m.c.projectCheckedBase(e), true
}

// value projects a data type once per module.
func (m *machineCheck) value(id TypeID) (ValueType, bool) {
	if value, ok := m.shared.values[id]; ok {
		return value, true
	}
	value, ok := m.project(m.c.checkedDataID(id, nil, nil))
	if !ok {
		return ValueType{}, false
	}
	if m.shared.values == nil {
		m.shared.values = map[TypeID]ValueType{}
	}
	m.shared.values[id] = value
	return value, true
}

// function projects one bound function once per module. Its failure and
// service rows are charged by width before they are copied, and its contract
// value by its parameters before the checker interns it.
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
	if !m.charge(1 + len(f.Params) + machineTextUnits(machineNodeText+machineArgText*len(f.Params))) {
		return MachineFunction{}, false
	}
	contract, ok := m.project(m.c.checkedFunction(f, true, false))
	if !ok {
		return MachineFunction{}, false
	}
	projected := MachineFunction{Role: role, Name: f.Name, Identity: f.Identity, Span: f.Span, Kind: kind, Failures: failures, Services: services, Contract: contract}
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
