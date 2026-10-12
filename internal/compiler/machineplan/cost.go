package machineplan

// The cost model of the checker work that a type's layout and display stand
// for. The checker does that work while preparing the input; analysis charges
// these costs to its budget when it reads a prepared layout or display, so a
// declaration is refused (EF142) at the same point however the work is split.
// The checker prepares a layout or display only when its cost fits a
// declaration's budget, using this same model, so the model also bounds the
// preparation work.

// textUnit is the number of generated text bytes one budget unit buys. Text is
// charged by bytes because copying one identifier into many generated strings
// multiplies source size without adding expressions to walk.
const textUnit = 64

// TextUnits is the number of budget units text of the given length costs.
func TextUnits(bytes int) int { return (bytes + textUnit - 1) / textUnit }

// Per-node text bound: the fixed fields of an interning key or public-ID key,
// and the bytes each name, argument and row label adds to one.
const (
	nodeTextBytes  = 128
	argTextBytes   = 24
	labelTextBytes = 24
	// textCap is more text than the module budget can buy, so a capped bound
	// is still refused.
	textCap = (MaxModuleWork + 1) * textUnit
)

// reachCap is one more node than the checker's template-argument and
// substitution walks memoize before they give up, so a capped count bounds
// either walk.
const reachCap = 4098

// walkNodes is the number of template-argument nodes one budget unit buys: the
// checker's argument validation allocates about 26 bytes per node it
// memoizes.
const walkNodes = 8

// typeText is what the checker can build or walk from one type, summed over
// the tree the type unfolds to: the bytes of its keys, its nodes through
// arguments and results (capped at reachCap), and the template-argument
// validations its applications run, by parameter kind.
type typeText struct {
	bytes, nodes, typeWalks, callableWalks int
}

// display is the length of the checker's display of one type and the bytes
// building it allocates.
type display struct{ length, bytes int }

// costs is the module-wide memo of the cost model.
type costs struct {
	texts    map[TypeRef]typeText
	displays map[TypeRef]display
}

// text bounds the key text the checker builds from the type under id (its
// public-ID key and the interning keys a template substitution rebuilds) and
// the nodes a substitution walks in it. Keys and walks unfold a shared DAG
// into a tree, so the bound sums children as often as they occur, which
// over-counts shared nodes; each distinct node is walked once, charged one
// unit through spend and memoized for the module, so an exponential unfolding
// is refused by its charge rather than walked.
func (c *costs) text(t *Types, id TypeRef, spend func() bool) (typeText, bool) {
	if text, ok := c.texts[id]; ok {
		return text, true
	}
	if !spend() {
		return typeText{}, false
	}
	if c.texts == nil {
		c.texts = map[TypeRef]typeText{}
	}
	// A back edge measures nothing; the checker displays it as a cycle. A
	// refused walk removes its partial bounds, so no later declaration of the
	// module reads one.
	c.texts[id] = typeText{}
	text, ok := c.nodeText(t, id, spend)
	if !ok {
		delete(c.texts, id)
		return typeText{}, false
	}
	c.texts[id] = text
	return text, true
}

func (c *costs) nodeText(t *Types, id TypeRef, spend func() bool) (typeText, bool) {
	n := t.Nodes[id]
	text := typeText{bytes: nodeTextBytes, nodes: 1}
	if n == nil {
		return text, true
	}
	text.bytes += 2*(len(n.Kind)+len(n.Name)+len(n.Declaration)+len(n.Mode)) + argTextBytes*len(n.Args)
	for _, row := range [2]Row{n.Failures, n.Services} {
		for label := range row.All() {
			text.bytes = min(text.bytes+2*len(label)+labelTextBytes, textCap)
		}
	}
	if template := t.Templates[n.Declaration]; n.Kind == "application" && template != nil {
		for _, parameter := range template.Params {
			if parameter.Type {
				text.typeWalks++
			} else if parameter.Constraint >= 0 {
				text.callableWalks += parameter.Constraint + 1
			}
		}
	}
	for i := 0; i <= len(n.Args); i++ {
		child := n.Result
		if i < len(n.Args) {
			child = n.Args[i]
		} else if child == 0 {
			break
		}
		below, ok := c.text(t, child, spend)
		if !ok {
			return typeText{}, false
		}
		text.bytes = min(text.bytes+below.bytes, textCap)
		text.nodes = min(text.nodes+below.nodes, reachCap)
		text.typeWalks = min(text.typeWalks+below.typeWalks, textCap)
		text.callableWalks = min(text.callableWalks+below.callableWalks, textCap)
	}
	return text, true
}

// layoutUnits is what resolving the fields or variants of a record, enum or
// application costs, or false when id is no data type: the declaration
// qualifier the checker rebuilds for a nominal type, and for an application
// the instantiated copy of its template plus, for every declared field type,
// the interning keys substitution builds and the template-argument walks it
// repeats.
func (c *costs) layoutUnits(t *Types, id TypeRef, spend func() bool) (int, bool) {
	n := t.Nodes[id]
	if n == nil || n.Kind != "record" && n.Kind != "enum" && n.Kind != "application" {
		return 0, false
	}
	units := 1 + TextUnits(len(n.Declaration)+64)
	if n.Kind != "application" {
		return units, true
	}
	template := t.Templates[n.Declaration]
	if template == nil {
		return units, true
	}
	// Substitution binds each template parameter to an argument of this
	// application, so a substituted field type has at most its declared nodes
	// plus the arguments' nodes.
	arguments := 0
	for _, arg := range n.Args {
		text, ok := c.text(t, arg, spend)
		if !ok {
			return 0, false
		}
		arguments = min(arguments+text.nodes, reachCap)
	}
	text, walked := 0, 0
	declared := func(fields []TypeRef) bool {
		units += 1 + 4*len(fields)
		for _, field := range fields {
			if field == 0 {
				continue
			}
			declared, ok := c.text(t, field, spend)
			if !ok {
				return false
			}
			text = min(text+declared.bytes, textCap)
			// Substitution runs once per field with a fresh memo over the
			// declared nodes, and every application it rebuilds validates its
			// arguments afresh: a type parameter's walk covers the substituted
			// argument, a callable parameter's walks are capped by the checker.
			substituted := min(declared.nodes+arguments, reachCap)
			walked = min(walked+declared.nodes+declared.typeWalks*substituted+declared.callableWalks*reachCap, textCap)
		}
		return true
	}
	if !declared(template.Fields) {
		return 0, false
	}
	for _, variant := range template.Variants {
		if !declared(variant) {
			return 0, false
		}
	}
	return units + TextUnits(text) + (walked+walkNodes-1)/walkNodes, true
}

// displayCost bounds the checker's type display. Wrapper kinds (fiber,
// goResult, goValues, recipe) display their children and copy the children's
// text into their own at every layer, so the build cost grows with nesting
// depth times the text below, not with the text itself. Each distinct node is
// walked once and memoized for the module; children are summed as often as
// they occur, as the display rebuilds them. The walk needs no charge of its
// own: only wrapper nodes have children, and each adds at least 16 bytes to
// the cost the caller charges for, which pays for its memo entry.
func (c *costs) displayCost(t *Types, id TypeRef) display {
	if cost, ok := c.displays[id]; ok {
		return cost
	}
	if c.displays == nil {
		c.displays = map[TypeRef]display{}
	}
	c.displays[id] = display{}
	cost := c.nodeDisplay(t, id)
	cost.length, cost.bytes = min(cost.length, textCap), min(cost.bytes, textCap)
	c.displays[id] = cost
	return cost
}

// nodeDisplay follows the checker's display case by case.
func (c *costs) nodeDisplay(t *Types, id TypeRef) display {
	n := t.Nodes[id]
	if n == nil {
		return display{length: len("invalid")}
	}
	switch n.Kind {
	case "never", "invalid":
		return display{length: len(n.Kind)}
	case "fiber", "goResult":
		if len(n.Args) == 1 {
			child := c.displayCost(t, n.Args[0])
			length := len("GoResult:") + child.length
			// One concatenation of the prefix and the child's text.
			return display{length: length, bytes: child.bytes + length}
		}
	case "goValues":
		// A component slice, then strings.Join and the parenthesised copy.
		cost := display{length: 2, bytes: 16 * len(n.Args)}
		for i, arg := range n.Args {
			child := c.displayCost(t, arg)
			if i > 0 {
				cost.length += 2
			}
			cost.length = min(cost.length+child.length, textCap)
			cost.bytes = min(cost.bytes+child.bytes, textCap)
		}
		cost.bytes += 2 * cost.length
		return cost
	case "provider":
		length := len("provider:") + len(n.Name)
		return display{length: length, bytes: length}
	case "recipe":
		child := c.displayCost(t, n.Result)
		// The recipe display copies each row (its labels, then normalized),
		// joins it, and concatenates the growing text: twice without rows, up
		// to four times with them.
		length, labels, copies := len("Effect<>")+child.length, 0, 2
		for _, row := range [2]Row{n.Failures, n.Services} {
			if n.Failures.Len() == 0 && n.Services.Len() == 0 {
				break
			}
			copies = 5
			length += len(", {}")
			for label := range row.All() {
				length = min(length+len(label)+2, textCap)
				labels++
			}
		}
		return display{length: length, bytes: child.bytes + copies*length + 48*labels + 64}
	}
	return display{length: len(n.Name)}
}

// Costs is the cost model for the checker's preparation: the same costs
// analysis charges, over the same prepared table, memoized for the module.
// Preparation passes no spend, so its costs omit the one unit per distinct
// node that analysis also charges for walking them; it therefore prepares at
// least what analysis can afford.
type Costs struct{ memo costs }

// LayoutUnits is the cost of resolving id's layout, or false when id is no
// data type. The type table must hold id's node, its arguments' trees and its
// template's declared field types.
func (c *Costs) LayoutUnits(t *Types, id TypeRef) (int, bool) {
	return c.memo.layoutUnits(t, id, func() bool { return true })
}

// DisplayUnits is the cost of building id's display.
func (c *Costs) DisplayUnits(t *Types, id TypeRef) int {
	return TextUnits(c.memo.displayCost(t, id).bytes)
}
