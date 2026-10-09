package compiler

import (
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// Failure payload evidence (design §4.1, §5.2). A recipe occurrence carries
// one payload occurrence per failure label of each of its layers: the owners
// of the handles the payload of that failure holds. A payload of layer d is
// produced while layer d executes, so it is numbered under the binders of
// layers 0 ... d: Exec(d) is an acquisition by the failing execution itself.
//
// An entry is evidence, possibly empty. A handle-carrying label of a layer's
// failure row without an entry has unknown payload owners: missing evidence
// fails closed (completeFailures), and only a handle-free payload needs no
// entry. A Fiber occurrence is numbered like its joined recipe: its child's
// failures are layer 0, closed terms owned by the closed child owner.

// failureKey names the payload of failure label raised by layer layer.
//
// A pending key (design §5.3) names a failure of a forked child which may
// be unobserved when the executor's owner closes: the runtime raises it at
// that owner's close, so recover and catch around the layer cannot handle
// it. fork names the child inside the body that forked it: "fork:<offset>"
// while the forking body is checked, where a join or interrupt can observe
// it. Across a function edge no caller can name the child's Fiber, but the
// grouping survives so an owner can count its children: the edge numbers the
// children ("#<n>") and the caller prefixes the call it runs ("<offset>#<n>").
// An empty fork on a pending key is an unknown number of children (⊤).
// An ordinary failure has pending false and an empty fork.
//
// with is, for a pending key, the ordinary failures of the body which may
// be raised together with the child's: the labels of every exit taken while
// the child was unobserved, sorted and comma separated (composite causes,
// design §5.3 rule 6). It is empty on every other key.
//
// alt is the path of the exclusive alternatives (if and match arms) of the
// forking body which contain the child: "<branch>.<arm>" segments, outermost
// first. Children of different arms of one branch never exist together, so
// the owner counts the widest arm, not the sum (forkWidth). It is
// intra-function evidence: a function edge renumbers the children so that
// exclusive ones share a number, and the summary carries no alt.
type failureKey struct {
	layer   int
	label   string
	pending bool
	fork    string
	with    string
	alt     string
}

// raisedKey is the key of the ordinary failure label raised by layer.
func raisedKey(layer int, label string) failureKey {
	return failureKey{layer: layer, label: label}
}

// failureEvidence maps each failure of an occurrence's layers to its payload
// facts. Maps are shared between occurrences and never mutated in place.
type failureEvidence map[failureKey][]OwnershipFact

// unknownPayloadOrigin labels the ⊤ owner of a payload without evidence.
const unknownPayloadOrigin = "failure-payload"

func cloneFailures(f failureEvidence) failureEvidence {
	if len(f) == 0 {
		return nil
	}
	out := make(failureEvidence, len(f))
	for key, facts := range f {
		out[key] = cloneFacts(facts)
	}
	return out
}

func clonePayloads(p map[string][]OwnershipFact) map[string][]OwnershipFact {
	if len(p) == 0 {
		return nil
	}
	out := make(map[string][]OwnershipFact, len(p))
	for label, facts := range p {
		out[label] = cloneFacts(facts)
	}
	return out
}

// mergeFailures joins two alternatives' payload evidence per failure. Both
// sides must already be complete for their own rows.
func mergeFailures(a, b failureEvidence) failureEvidence {
	if len(a) == 0 {
		return cloneFailures(b)
	}
	if len(b) == 0 {
		return cloneFailures(a)
	}
	out := cloneFailures(a)
	for key, facts := range b {
		if present, ok := out[key]; ok {
			out[key] = mergeFacts(present, facts)
			continue
		}
		out[key] = cloneFacts(facts)
	}
	return out
}

func mergePayloads(a, b map[string][]OwnershipFact) map[string][]OwnershipFact {
	if len(a) == 0 {
		return clonePayloads(b)
	}
	if len(b) == 0 {
		return clonePayloads(a)
	}
	out := clonePayloads(a)
	for label, facts := range b {
		if present, ok := out[label]; ok {
			out[label] = mergeFacts(present, facts)
			continue
		}
		out[label] = cloneFacts(facts)
	}
	return out
}

// shiftFailures moves every payload by delta layers and drops the payloads
// of layers which fall below zero (an executed layer).
func shiftFailures(f failureEvidence, delta int) failureEvidence {
	if len(f) == 0 {
		return nil
	}
	out := failureEvidence{}
	for key, facts := range f {
		key.layer += delta
		if key.layer < 0 {
			continue
		}
		out[key] = cloneFacts(facts)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// layerPayloads is the payload evidence of the failures of layer layer.
func layerPayloads(f failureEvidence, layer int) map[string][]OwnershipFact {
	var out map[string][]OwnershipFact
	for key, facts := range f {
		if key.layer != layer || key.pending {
			continue
		}
		if out == nil {
			out = map[string][]OwnershipFact{}
		}
		out[key.label] = cloneFacts(facts)
	}
	return out
}

// withLayerPayloads replaces the ordinary payloads of layer layer.
func withLayerPayloads(f failureEvidence, layer int, payloads map[string][]OwnershipFact) failureEvidence {
	out := failureEvidence{}
	for key, facts := range f {
		if key.layer != layer || key.pending {
			out[key] = cloneFacts(facts)
		}
	}
	for label, facts := range payloads {
		out[raisedKey(layer, label)] = cloneFacts(facts)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mapFailureFacts rewrites the facts of every payload, keeping each entry.
func mapFailureFacts(f failureEvidence, rewrite func([]OwnershipFact) []OwnershipFact) failureEvidence {
	if len(f) == 0 {
		return nil
	}
	out := make(failureEvidence, len(f))
	for key, facts := range f {
		out[key] = rewrite(cloneFacts(facts))
	}
	return out
}

func mapPayloadFacts(p map[string][]OwnershipFact, rewrite func([]OwnershipFact) []OwnershipFact) map[string][]OwnershipFact {
	if len(p) == 0 {
		return nil
	}
	out := make(map[string][]OwnershipFact, len(p))
	for label, facts := range p {
		out[label] = rewrite(cloneFacts(facts))
	}
	return out
}

func sortedFailureKeys(f failureEvidence) []failureKey {
	keys := slices.Collect(maps.Keys(f))
	slices.SortFunc(keys, compareFailureKeys)
	return keys
}

// compareFailureKeys orders keys by layer, ordinary before pending, fork
// instance and label.
func compareFailureKeys(a, b failureKey) int {
	if a.layer != b.layer {
		return a.layer - b.layer
	}
	if a.pending != b.pending {
		if a.pending {
			return 1
		}
		return -1
	}
	if c := strings.Compare(a.fork, b.fork); c != 0 {
		return c
	}
	if c := strings.Compare(a.with, b.with); c != 0 {
		return c
	}
	if c := strings.Compare(a.alt, b.alt); c != 0 {
		return c
	}
	return strings.Compare(a.label, b.label)
}

// failurePayloadType is the payload contract of a declared failure label.
// Builtin failures and row variables carry no handle (builtins.go).
func (c *checker) failurePayloadType(label string) (TypeID, bool) {
	if c.errors[label] == nil || slices.Contains(builtinErrors(), label) {
		return invalidTypeID, false
	}
	id := c.canonicalRef(typeRef(label))
	return id, id != invalidTypeID
}

// unknownPayload is ⊤ payload evidence for label: every handle path of its
// payload has an unknown owner. It is nil for a handle-free payload, which
// needs no evidence.
func (c *checker) unknownPayload(label string) []OwnershipFact {
	id, ok := c.failurePayloadType(label)
	if !ok {
		return nil
	}
	var out []OwnershipFact
	for _, path := range c.ownershipPathsID(id, "") {
		out = append(out, OwnershipFact{Path: path, Status: "unknown", Origin: unknownPayloadOrigin, potentialOwner: true})
	}
	return normalizeFacts(out)
}

// failureLayerRows is the failure row of each failure-carrying layer of a
// value contract: the layers of a recipe chain, or a Fiber's child followed
// by its result's layers.
func (c *checker) failureLayerRows(id TypeID) [][]string {
	var rows [][]string
	for range 256 {
		node := c.node(id)
		if node == nil {
			return rows
		}
		switch node.Kind {
		case "recipe":
			rows = append(rows, c.rowLabels(node.FailureRow))
			id = node.Result
		case "fiber":
			// A Fiber's evidence is numbered like its joined recipe.
			if len(node.Args) != 1 {
				return append(rows, c.rowLabels(node.FailureRow))
			}
			rows = append(rows, c.rowLabels(node.FailureRow))
			id = node.Args[0]
		case "providerRecipe":
			return append(rows, c.rowLabels(node.FailureRow))
		default:
			return rows
		}
	}
	return rows
}

// callFailureRows is the failure row of each layer produced by invoking a
// callable: an effect callee's own row first, then its result's layers.
func (c *checker) callFailureRows(result TypeID, effect bool, callRow RowID) [][]string {
	rows := c.failureLayerRows(result)
	if effect {
		rows = append([][]string{c.rowLabels(callRow)}, rows...)
	}
	return rows
}

// completePayloads keeps the evidence of the labels a row raises and makes
// every handle-carrying label without evidence unknown.
func (c *checker) completePayloads(labels []string, payloads map[string][]OwnershipFact) map[string][]OwnershipFact {
	var out map[string][]OwnershipFact
	for _, label := range labels {
		facts, ok := payloads[label]
		if !ok {
			facts = c.unknownPayload(label)
			if facts == nil {
				continue
			}
		}
		if out == nil {
			out = map[string][]OwnershipFact{}
		}
		out[label] = cloneFacts(facts)
	}
	return out
}

// completeFailures makes an occurrence's payload evidence complete for its
// own contract: entries for labels its rows no longer raise are dropped, and
// a handle-carrying label without evidence is unknown (fail closed, §4.1).
// It is idempotent.
func (c *checker) completeFailures(e checkedExpression) checkedExpression {
	if len(e.failures) == 0 && !c.raisesFailures(e.valueID()) {
		return e
	}
	e.failures = c.completeFailureRows(e.failures, c.failureLayerRows(e.valueID()))
	return e
}

// raisesFailures reports whether some layer of a contract has a failure row.
func (c *checker) raisesFailures(id TypeID) bool {
	return slices.ContainsFunc(c.failureLayerRows(id), func(labels []string) bool { return len(labels) > 0 })
}

// completeFailureRows is completeFailures for evidence of layers with the
// given failure rows.
func (c *checker) completeFailureRows(failures failureEvidence, rows [][]string) failureEvidence {
	var out failureEvidence
	// Pending entries are evidence of their own; their labels stay in the
	// rows of the layers which carry them.
	for key, facts := range failures {
		if key.pending {
			if out == nil {
				out = failureEvidence{}
			}
			out[key] = cloneFacts(facts)
		}
	}
	for layer, labels := range rows {
		for label, facts := range c.completePayloads(labels, layerPayloads(failures, layer)) {
			if out == nil {
				out = failureEvidence{}
			}
			out[raisedKey(layer, label)] = facts
		}
	}
	return out
}

// completeEvaluation is completeFailures for the failures an evaluation
// incurs.
func (c *checker) completeEvaluation(e ExpressionEvaluation) ExpressionEvaluation {
	e.payloads = c.completePayloads(c.rowLabels(e.failureRow), e.payloads)
	return e
}

// payloadOccurrence is the argument a recovery handler receives for label:
// the payload contract with the failing layer's evidence.
func (c *checker) payloadOccurrence(payload TypeID, facts []OwnershipFact) checkedExpression {
	return c.checkedDataID(payload, normalizeFacts(cloneFacts(facts)), nil)
}

// closeRegionFacts is the closing edge of a lexical region on payload
// evidence (design §5.4): a payload the region owns outlives it only as a
// handle whose owner has closed.
func (c *checker) closeRegionFacts(facts []OwnershipFact, region string, kind ownershipOwnerKind) []OwnershipFact {
	r := ownerRewrite{level: execOwner, region: region, regionOwner: closedOwner(kind, region)}
	return c.rewriteFacts(facts, r)
}

// withoutRegionUses discharges the obligations a closing region satisfied:
// each was checked open where its layer executed inside the region.
func withoutRegionUses(uses []OwnershipFact, region string) []OwnershipFact {
	var out []OwnershipFact
	for _, fact := range uses {
		if fact.ownerKind == ownershipOwnerLexical && fact.Region == region {
			continue
		}
		out = append(out, fact)
	}
	return normalizeFacts(out)
}

// bottomFailures is ⊥ payload evidence for a function's call layers: every
// failure it may raise, with no handle owner proven yet.
func (c *checker) bottomFailures(f *Function) failureEvidence {
	var out failureEvidence
	for layer, labels := range c.callFailureRows(f.returnID, f.Effect, f.failureID) {
		for _, label := range labels {
			if out == nil {
				out = failureEvidence{}
			}
			out[raisedKey(layer, label)] = nil
		}
	}
	return out
}

// equalFailures compares two payload evidence maps entry by entry.
func equalFailures(a, b failureEvidence) bool {
	if len(a) != len(b) {
		return false
	}
	for key, facts := range a {
		other, ok := b[key]
		if !ok || !slices.Equal(facts, other) {
			return false
		}
	}
	return true
}

// closeRegionFailures applies the closing edge of a region to every failure
// payload of an occurrence, including those of the recipes it stores.
func (c *checker) closeRegionFailures(e checkedExpression, region string, kind ownershipOwnerKind) checkedExpression {
	return mapOccurrenceFacts(e, func(_ checkedExpression, slot occurrenceFactSlot, facts []OwnershipFact) []OwnershipFact {
		if slot != failuresSlot {
			return facts
		}
		return c.closeRegionFacts(facts, region, kind)
	})
}

// storedRecipeEvidence is what the recipes a value stores in its fields hold
// and will produce. A closing edge refuses a value whose stored recipe would
// produce a handle the closing owner owns (design §5.4 escapes): the value
// itself carries only the handles those recipes hold.
//
// The result is a set: a field table a record graph shares is visited once,
// so the walk is linear in the distinct tables rather than in leaf paths.
func storedRecipeEvidence(e checkedExpression) []OwnershipFact {
	var out []OwnershipFact
	visited := map[uintptr]bool{}
	var visit func(fields map[string]checkedExpression)
	visit = func(fields map[string]checkedExpression) {
		if fields == nil || visited[reflect.ValueOf(fields).Pointer()] {
			return
		}
		visited[reflect.ValueOf(fields).Pointer()] = true
		for _, name := range slices.Sorted(maps.Keys(fields)) {
			field := fields[name]
			if field.isEffect() {
				out = append(out, field.ownershipFacts()...)
				out = append(out, field.captureFacts()...)
			}
			visit(field.fields)
		}
	}
	visit(e.fields)
	return out
}

// declaredFailures widens a summary's payload evidence to its declared
// rows: a label the declaration admits but the checked body cannot raise at
// that layer has proven-empty evidence, not unknown evidence.
func (c *checker) declaredFailures(failures failureEvidence, actual, declared [][]string) failureEvidence {
	out := cloneFailures(failures)
	for layer, labels := range declared {
		for _, label := range labels {
			if layer < len(actual) && slices.Contains(actual[layer], label) {
				continue
			}
			if _, ok := out[raisedKey(layer, label)]; ok {
				continue
			}
			if out == nil {
				out = failureEvidence{}
			}
			out[raisedKey(layer, label)] = nil
		}
	}
	return out
}

// Pending evidence (design §5.3). A forked child's failures are pending
// on the forking body until a join or interrupt observes the child. The
// runtime raises the failures of an unobserved child where its owner
// closes (scope.go close, effect.go withCleanup), so they are charged to
// the rows of every exit they may cross and raised at the owner edge.

// layerPending is the pending evidence of layer, keyed at layer 0.
func layerPending(f failureEvidence, layer int) failureEvidence {
	var out failureEvidence
	for key, facts := range f {
		if !key.pending || key.layer != layer {
			continue
		}
		if out == nil {
			out = failureEvidence{}
		}
		key.layer = 0
		out[key] = cloneFacts(facts)
	}
	return out
}

// pendingLabels is the labels pending on layer.
func pendingLabels(f failureEvidence, layer int) []string {
	var out []string
	for key := range f {
		if key.pending && key.layer == layer && !slices.Contains(out, key.label) {
			out = append(out, key.label)
		}
	}
	slices.Sort(out)
	return out
}

// Composite causes (design §5.3 rule 6). The runtime raises a closing
// owner's failure as the sequence of its body's failure and the causes of
// the children it closes over, and recover and catch handle only a solitary
// typed failure (effect.go solitaryFailure). An owner edge therefore raises
// an ordinary failure only when exactly one failure reaches it:
//
//   - a child failure pending together with a failure of the body (a failing
//     exit taken while the child was unobserved, or a timeout which may fire
//     at any time) is a composite cause, as are
//   - the failures of two children, even with the same label.
//
// No handler matches a composite cause, so it is a failure of the owner which
// no row can declare: compositeCause stands for it in the rows, and it is
// refused where the first function would have to declare it.
const compositeCause = "composite cause"

// withLabels is the ordinary failures a pending key may be raised with.
func (k failureKey) withLabels() []string {
	if k.with == "" {
		return nil
	}
	return strings.Split(k.with, ",")
}

// joinWith is the canonical encoding of a set of with labels.
func joinWith(labels []string) string {
	labels = slices.Clone(labels)
	slices.Sort(labels)
	return strings.Join(slices.Compact(labels), ",")
}

// rewriteWith rewrites the with labels of every pending key.
func rewriteWith(f failureEvidence, rewrite func([]string) []string) failureEvidence {
	if !slices.ContainsFunc(slices.Collect(maps.Keys(f)), func(key failureKey) bool { return key.pending }) {
		return f
	}
	out := failureEvidence{}
	for key, facts := range f {
		if key.pending {
			key.with = joinWith(rewrite(key.withLabels()))
		}
		out[key] = mergeFacts(out[key], facts)
	}
	return out
}

// exitWith is an exit of the body which raises labels while every pending
// child is unobserved: they may be raised together.
func exitWith(f failureEvidence, labels []string) failureEvidence {
	if len(labels) == 0 {
		return f
	}
	return rewriteWith(f, func(with []string) []string { return union(with, labels) })
}

// handleWith is the recovery of label by a handler which raises handler: a
// failure of the body that was handled no longer reaches the owner with the
// children, and the handler's own failures do.
func handleWith(f failureEvidence, label string, handler []string) failureEvidence {
	return rewriteWith(f, func(with []string) []string {
		if !slices.Contains(with, label) {
			return with
		}
		return union(remove(with, label), handler)
	})
}

// isComposite reports whether the pending failures of layer reach their
// owner as a composite cause.
func isComposite(pending failureEvidence, layer int) bool {
	var children []forkAlternatives
	for key := range pending {
		if !key.pending || key.layer != layer {
			continue
		}
		if key.with != "" || key.fork == "" {
			return true
		}
		if !slices.ContainsFunc(children, func(child forkAlternatives) bool { return child.fork == key.fork }) {
			children = append(children, forkAlternatives{fork: key.fork, path: altSegments(key.alt)})
		}
	}
	return numberForks(children, nil) > 1
}

// forkAlternatives is a child and the alternatives it sits in.
type forkAlternatives struct {
	fork string
	path []string
}

func altSegments(alt string) []string {
	if alt == "" {
		return nil
	}
	return strings.Split(alt, "/")
}

// tagAlternative marks every child pending or live in evidence as created
// in one arm of a branch: segment is "<branch>.<arm>".
func tagAlternative(f failureEvidence, segment string) failureEvidence {
	if !slices.ContainsFunc(slices.Collect(maps.Keys(f)), func(key failureKey) bool { return key.pending && key.fork != "" }) {
		return f
	}
	out := failureEvidence{}
	for key, facts := range f {
		if key.pending && key.fork != "" {
			key.alt = strings.TrimSuffix(segment+"/"+key.alt, "/")
		}
		out[key] = mergeFacts(out[key], facts)
	}
	return out
}

// numberForks counts the children that can exist together and, when ids is
// not nil, numbers them so that exclusive children share a number. Children
// outside any branch are all simultaneous; the arms of one branch are
// alternatives, so a branch is as wide as its widest arm; branches and
// plain children follow one another.
func numberForks(children []forkAlternatives, ids map[string]int) int {
	return numberForksFrom(children, ids, 0)
}

func numberForksFrom(children []forkAlternatives, ids map[string]int, start int) int {
	var direct []string
	branches := map[string]map[string][]forkAlternatives{}
	for _, child := range children {
		if len(child.path) == 0 {
			direct = append(direct, child.fork)
			continue
		}
		branch, arm, _ := strings.Cut(child.path[0], ".")
		if branches[branch] == nil {
			branches[branch] = map[string][]forkAlternatives{}
		}
		branches[branch][arm] = append(branches[branch][arm], forkAlternatives{fork: child.fork, path: child.path[1:]})
	}
	slices.Sort(direct)
	next := start
	for _, fork := range direct {
		if ids != nil {
			ids[fork] = next
		}
		next++
	}
	for _, branch := range slices.Sorted(maps.Keys(branches)) {
		widest := 0
		for _, arm := range slices.Sorted(maps.Keys(branches[branch])) {
			widest = max(widest, numberForksFrom(branches[branch][arm], ids, next))
		}
		next += widest
	}
	return next - start
}

// dischargePending is an owner edge on layer: the closing owner raises the
// pending failures, so they become ordinary failures of that layer whose
// payloads keep their owners.
func dischargePending(f failureEvidence, layer int) failureEvidence {
	pending := layerPending(f, layer)
	if len(pending) == 0 {
		return f
	}
	composite := isComposite(f, layer)
	out := failureEvidence{}
	for key, facts := range f {
		if key.pending && key.layer == layer {
			continue
		}
		out[key] = cloneFacts(facts)
	}
	for key, facts := range pending {
		raised := raisedKey(layer, key.label)
		out[raised] = mergeFacts(out[raised], facts)
	}
	if composite {
		out[raisedKey(layer, compositeCause)] = nil
	}
	return out
}

// dischargeOccurrence is an owner edge on the recipe e: the closing owner
// raises the failures of the children it owns, and a composite cause joins
// the row.
func (c *checker) dischargeOccurrence(e checkedExpression) checkedExpression {
	e.failures = dischargePending(e.failures, 0)
	if _, composite := e.failures[raisedKey(0, compositeCause)]; composite && !slices.Contains(c.rowLabels(e.failureRow()), compositeCause) {
		e.value = c.recontractRows(e, c.internRow(union(c.rowLabels(e.failureRow()), []string{compositeCause})), e.serviceRow())
	}
	return e
}

// forgetForks abstracts the fork instance keys at a function edge: no
// caller can name a child forked by the callee, so its pending failures
// can no longer be observed. The children stay distinct, numbered so that
// children of exclusive alternatives share a number, and the owner which
// closes later still counts them.
func forgetForks(f failureEvidence) failureEvidence {
	var children []forkAlternatives
	for key := range f {
		if key.fork != "" && !slices.ContainsFunc(children, func(child forkAlternatives) bool { return child.fork == key.fork }) {
			children = append(children, forkAlternatives{fork: key.fork, path: altSegments(key.alt)})
		}
	}
	if len(children) == 0 {
		return f
	}
	ids := map[string]int{}
	numberForks(children, ids)
	out := failureEvidence{}
	for key, facts := range f {
		if key.fork != "" {
			key.fork, key.alt = "#"+strconv.Itoa(ids[key.fork]), ""
		}
		out[key] = mergeFacts(out[key], facts)
	}
	return out
}

// atCall names the children a callee left pending after the call which
// runs it, so two calls of one function stay two children.
func atCall(f failureEvidence, offset int) failureEvidence {
	if !slices.ContainsFunc(slices.Collect(maps.Keys(f)), func(key failureKey) bool { return key.pending && key.fork != "" }) {
		return f
	}
	out := failureEvidence{}
	for key, facts := range f {
		if key.pending && key.fork != "" {
			key.fork = strconv.Itoa(offset) + key.fork
		}
		out[key] = mergeFacts(out[key], facts)
	}
	return out
}

// killForks removes the live failures of the observed fork instances.
func killForks(f failureEvidence, forks []string) failureEvidence {
	if len(f) == 0 || len(forks) == 0 {
		return f
	}
	var out failureEvidence
	for key, facts := range f {
		if key.fork != "" && slices.Contains(forks, key.fork) {
			continue
		}
		if out == nil {
			out = failureEvidence{}
		}
		out[key] = facts
	}
	return out
}

// exitPending is an exit of an evaluation which live crosses: those
// failures may be raised by the owner that closes after the exit, so they
// become pending and join the row. A label the row did not raise before
// has a proven-empty ordinary payload: it is raised only as pending.
func (c *checker) exitPending(e ExpressionEvaluation, live failureEvidence, raises []string) ExpressionEvaluation {
	// The exit raises raises together with every child still unobserved:
	// those already pending and those it leaves pending.
	e.pending = exitWith(e.pending, raises)
	live = exitWith(live, raises)
	if len(live) == 0 {
		return e
	}
	row := c.rowLabels(e.failureRow)
	var added []string
	for _, key := range sortedFailureKeys(live) {
		if !slices.Contains(row, key.label) && !slices.Contains(added, key.label) {
			added = append(added, key.label)
		}
	}
	if len(added) > 0 {
		// Complete the labels the row already raises first: one raised
		// without evidence stays unknown.
		e = c.completeEvaluation(e)
		payloads := clonePayloads(e.payloads)
		if payloads == nil {
			payloads = map[string][]OwnershipFact{}
		}
		for _, label := range added {
			payloads[label] = nil
		}
		e.payloads = payloads
		e.failureRow = c.internRow(union(row, added))
	}
	e.pending = mergeFailures(e.pending, live)
	return e
}

// exitAll is the success exit of a body: every live failure crosses it.
func (c *checker) exitAll(e ExpressionEvaluation, raises []string) ExpressionEvaluation {
	live := e.live
	e.live = nil
	return c.exitPending(e, live, raises)
}

// dischargeEvaluation is an owner edge on an evaluation (design §5.3 rule
// 4): the closing owner raises every pending and live failure of the
// children it owns, with the payloads they carry.
func (c *checker) dischargeEvaluation(e ExpressionEvaluation) ExpressionEvaluation {
	e = c.completeEvaluation(c.exitAll(e, nil))
	if len(e.pending) == 0 {
		return e
	}
	payloads := clonePayloads(e.payloads)
	if isComposite(e.pending, 0) {
		if payloads == nil {
			payloads = map[string][]OwnershipFact{}
		}
		payloads[compositeCause] = nil
		e.failureRow = c.internRow(union(c.rowLabels(e.failureRow), []string{compositeCause}))
	}
	for key, facts := range e.pending {
		if payloads == nil {
			payloads = map[string][]OwnershipFact{}
		}
		payloads[key.label] = mergeFacts(payloads[key.label], facts)
	}
	e.payloads = payloads
	e.pending = nil
	return e
}

// topPending is ⊤ pending evidence for a function's call layers: every
// failure it may raise may come from an unobserved child.
func (c *checker) topPending(f *Function) failureEvidence {
	var out failureEvidence
	for layer, labels := range c.callFailureRows(f.returnID, f.Effect, f.failureID) {
		for _, label := range labels {
			if out == nil {
				out = failureEvidence{}
			}
			out[failureKey{layer: layer, label: label, pending: true}] = c.unknownPayload(label)
		}
	}
	return out
}

// anyPendingLabels is the sorted labels pending on any layer.
func anyPendingLabels(f failureEvidence) []string {
	var labels []string
	for key := range f {
		if key.pending && !slices.Contains(labels, key.label) {
			labels = append(labels, key.label)
		}
	}
	slices.Sort(labels)
	return labels
}
