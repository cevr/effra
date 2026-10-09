// Ownership matrix generator (lane E2 R5): a Go port of the design unit's
// gen_matrix.py, whose program sources it reproduces byte for byte. It is
// now the authority: it adds the handler-acquisition oracle and the raising
// twins below, which the design unit's script predates. testdata/ownership_matrix/
// truth.json records each program's runtime truth, decided by executing it
// with ownership refusals erased; regenerate it only when this generator
// changes, never to absorb a checker verdict.
//
// Truth covers every failure the program's rows can raise, not only the path
// one execution takes. A row whose use after close happens only on a failure
// path its fixture never raises has a raising twin (family "raising", named
// <row>_raising) which takes that path; the row records its twin's executed
// truth, and TestOwnershipMatrixRaisingTwins keeps the two equal.
//
// The lane E2 R5 ownership probe-matrix generator. Every program acquires one File in
// layer A of a producer chain and observes it after the consumer has executed the
// chain, across channel (success | field | pair | failure), depth 1..3, boundary
// (run | timeout | fork | scope | provision) at layer b, wrappers, callback/helper
// producers and if/match branches. The explicit families add recovery handler
// variants, a handler returning the payload handle, forked-child failures and
// handle-free row-soundness programs that also execute on JS.

package compiler

import (
	"slices"
	"strconv"
	"strings"
)

type ownershipMatrixProgram struct {
	Name, Source, Oracle string
	JSRunnable           bool
}

const (
	ownershipMatrixR       = "{ IoError, Nope }"
	ownershipMatrixRF      = "{ WithFile, IoError, Nope }"
	ownershipMatrixRowsAll = "{ IoError, Nope, Timeout, WithFile }"
)

func ownershipMatrixEff(success, row string) string {
	return "Effect<" + success + ", " + row + ">"
}

type ownershipMatrixKey struct {
	channel string
	depth   int
}

// ownershipMatrixSucc is the success type of each layer, per (channel, depth); index 0 = layer 1.
var ownershipMatrixSucc = map[ownershipMatrixKey][]string{
	{"success", 1}: {"File"},
	{"success", 2}: {ownershipMatrixEff("string", ownershipMatrixR), "string"},
	{"success", 3}: {ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR), ownershipMatrixEff("string", ownershipMatrixR), "string"},
	{"field", 1}:   {"Holder"},
	{"field", 2}:   {ownershipMatrixEff("Holder", ownershipMatrixR), "Holder"},
	{"field", 3}:   {ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR), ownershipMatrixEff("Holder", ownershipMatrixR), "Holder"},
	{"pair", 2}:    {ownershipMatrixEff("Pair", ownershipMatrixR), "Pair"},
	{"pair", 3}:    {ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR), ownershipMatrixEff("Pair", ownershipMatrixR), "Pair"},
	{"failure", 1}: {"string"},
	{"failure", 2}: {ownershipMatrixEff("string", ownershipMatrixRF), "string"},
	{"failure", 3}: {ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR), ownershipMatrixEff("string", ownershipMatrixRF), "string"},
}

// ownershipMatrixAcq lists the layer(s) whose execution acquires the File.
var ownershipMatrixAcq = map[ownershipMatrixKey][]int{
	{"success", 1}: {1}, {"success", 2}: {1}, {"success", 3}: {2},
	{"field", 1}: {1}, {"field", 2}: {1}, {"field", 3}: {2},
	{"pair", 2}: {1, 2}, {"pair", 3}: {1, 3},
	{"failure", 1}: {1}, {"failure", 2}: {1}, {"failure", 3}: {2},
}

// ownershipMatrixProducer maps a channel to its (acquiring, borrowed) producer prefixes.
var ownershipMatrixProducer = map[string][2]string{
	"success": {"s", "t"}, "field": {"h", "k"}, "pair": {"p", "q"}, "failure": {"f", "g"},
}

var (
	ownershipMatrixClosing = []string{"timeout", "fork", "scope", "provision"}
	ownershipMatrixRecord  = []string{"field", "pair"}
)

func ownershipMatrixProducerRow(channel string, depth int) string {
	if channel == "failure" && depth == 1 {
		return ownershipMatrixRF
	}
	return ownershipMatrixR
}

// ownershipMatrixHandler names the recover<Nope> handler returning the layer's success type.
var ownershipMatrixHandler = map[string]string{
	"string": "hStr",
	"File":   "hFile",
	"Holder": "hHolder",
	ownershipMatrixEff("Holder", ownershipMatrixR):                                       "hH",
	ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR): "hHH",
	"Pair": "hPair",
	ownershipMatrixEff("Pair", ownershipMatrixR):                                          "hP",
	ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR):    "hPP",
	ownershipMatrixEff("string", ownershipMatrixR):                                        "hT",
	ownershipMatrixEff("string", ownershipMatrixRF):                                       "hTF",
	ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR):  "hTT",
	ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR): "hTTF",
}

var ownershipMatrixBox = map[string]string{
	ownershipMatrixEff("string", ownershipMatrixR):                                        "BoxS",
	ownershipMatrixEff("string", ownershipMatrixRF):                                       "BoxF",
	ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR):  "BoxSS",
	ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR): "BoxFF",
	ownershipMatrixEff("Holder", ownershipMatrixR):                                        "BoxH",
	ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR):  "BoxHH",
	ownershipMatrixEff("Pair", ownershipMatrixR):                                          "BoxP",
	ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR):    "BoxPP",
}

func ownershipMatrixOpen(fix string) string {
	return `run Files.openRead("` + fix + `").provide<Files>(LiveFiles)`
}

func ownershipMatrixHeader(fix string) string {
	open := ownershipMatrixOpen(fix)
	return `error WithFile { file: File }
error Nope { code: i64 }
enum Pick { A, B }
record BoxS { task: ` + ownershipMatrixEff("string", ownershipMatrixR) + ` }
record BoxF { task: ` + ownershipMatrixEff("string", ownershipMatrixRF) + ` }
record BoxSS { task: ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR) + ` }
record BoxFF { task: ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR) + ` }
record Holder { file: File }
record BoxH { task: ` + ownershipMatrixEff("Holder", ownershipMatrixR) + ` }
record BoxHH { task: ` + ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR) + ` }
record Pair { a: File, b: File }
record BoxP { task: ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` }
record BoxPP { task: ` + ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR) + ` }
layer Clocked { Clock = LiveClock }
effect fn readFile(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
effect fn rejected(file: File) -> string raises { WithFile } {
    fail WithFile { file: file }
}
effect fn retrieve(failure: WithFile) -> string raises { IoError } { run readFile(failure.file) }
effect fn s1() -> File raises ` + ownershipMatrixR + ` {
    ` + open + `
}
effect fn t1(f: File) -> File raises ` + ownershipMatrixR + ` { f }
effect fn s2() -> ` + ownershipMatrixEff("string", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    readFile(g)
}
effect fn t2(f: File) -> ` + ownershipMatrixEff("string", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { readFile(f) }
effect fn s3() -> ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { s2() }
effect fn t3(f: File) -> ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { t2(f) }
effect fn f1() -> string raises ` + ownershipMatrixRF + ` {
    let file = ` + open + `
    run rejected(file)
}
effect fn g1(f: File) -> string raises ` + ownershipMatrixRF + ` { run rejected(f) }
effect fn f2() -> ` + ownershipMatrixEff("string", ownershipMatrixRF) + ` raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    rejected(g)
}
effect fn g2(f: File) -> ` + ownershipMatrixEff("string", ownershipMatrixRF) + ` raises ` + ownershipMatrixR + ` { rejected(f) }
effect fn f3() -> ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { f2() }
effect fn g3(f: File) -> ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { g2(f) }
effect fn cStr() -> string raises ` + ownershipMatrixR + ` { "alt" }
effect fn cStrF() -> string raises ` + ownershipMatrixRF + ` { "alt" }
effect fn cT() -> ` + ownershipMatrixEff("string", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { cStr() }
effect fn cTF() -> ` + ownershipMatrixEff("string", ownershipMatrixRF) + ` raises ` + ownershipMatrixR + ` { cStrF() }
fn hStr(n: Nope) -> string { "alt" }
effect fn hFile(n: Nope) -> File raises { IoError } {
    ` + open + `
}
fn hT(n: Nope) -> ` + ownershipMatrixEff("string", ownershipMatrixR) + ` { cStr() }
fn hTF(n: Nope) -> ` + ownershipMatrixEff("string", ownershipMatrixRF) + ` { cStrF() }
fn hTT(n: Nope) -> ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixR), ownershipMatrixR) + ` { cT() }
fn hTTF(n: Nope) -> ` + ownershipMatrixEff(ownershipMatrixEff("string", ownershipMatrixRF), ownershipMatrixR) + ` { cTF() }
effect fn h1() -> Holder raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    Holder { file: g }
}
effect fn k1(f: File) -> Holder raises ` + ownershipMatrixR + ` { Holder { file: f } }
effect fn h2() -> ` + ownershipMatrixEff("Holder", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    k1(g)
}
effect fn k2(f: File) -> ` + ownershipMatrixEff("Holder", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { k1(f) }
effect fn h3() -> ` + ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { h2() }
effect fn k3(f: File) -> ` + ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { k2(f) }
effect fn hHolder(n: Nope) -> Holder raises { IoError } {
    let g = ` + open + `
    Holder { file: g }
}
effect fn cH() -> Holder raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    Holder { file: g }
}
effect fn cHH() -> ` + ownershipMatrixEff("Holder", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { cH() }
fn hH(n: Nope) -> ` + ownershipMatrixEff("Holder", ownershipMatrixR) + ` { cH() }
fn hHH(n: Nope) -> ` + ownershipMatrixEff(ownershipMatrixEff("Holder", ownershipMatrixR), ownershipMatrixR) + ` { cHH() }
effect fn pairB(f: File) -> Pair raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    Pair { a: f, b: g }
}
effect fn pairQ(f: File) -> Pair raises ` + ownershipMatrixR + ` { Pair { a: f, b: f } }
effect fn pairMid(f: File) -> ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { pairB(f) }
effect fn pairMidQ(f: File) -> ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { pairQ(f) }
effect fn p2() -> ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    pairB(g)
}
effect fn q2(f: File) -> ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { pairQ(f) }
effect fn p3() -> ` + ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    pairMid(g)
}
effect fn q3(f: File) -> ` + ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { pairMidQ(f) }
effect fn hPair(n: Nope) -> Pair raises { IoError } {
    let g = ` + open + `
    Pair { a: g, b: g }
}
effect fn cP() -> Pair raises ` + ownershipMatrixR + ` {
    let g = ` + open + `
    Pair { a: g, b: g }
}
effect fn cPP() -> ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` raises ` + ownershipMatrixR + ` { cP() }
fn hP(n: Nope) -> ` + ownershipMatrixEff("Pair", ownershipMatrixR) + ` { cP() }
fn hPP(n: Nope) -> ` + ownershipMatrixEff(ownershipMatrixEff("Pair", ownershipMatrixR), ownershipMatrixR) + ` { cPP() }
` + ownershipMatrixViaFunctions() + `
`
}

func ownershipMatrixViaFunctions() string {
	keys := make([]ownershipMatrixKey, 0, len(ownershipMatrixSucc))
	for k := range ownershipMatrixSucc {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b ownershipMatrixKey) int {
		if c := strings.Compare(a.channel, b.channel); c != 0 {
			return c
		}
		return a.depth - b.depth
	})
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		succ := ownershipMatrixSucc[k]
		row := ownershipMatrixProducerRow(k.channel, k.depth)
		out = append(out, "effect fn via_"+k.channel+strconv.Itoa(k.depth)+"(cb: effect fn() -> "+succ[0]+" raises "+row+") -> "+succ[0]+" raises "+row+" { run cb() }")
	}
	return strings.Join(out, "\n")
}

// ownershipMatrixBranchExpr: order "main-first" puts MAIN in the first source branch. flag/pick select MAIN.
func ownershipMatrixBranchExpr(shape, order, main, alt string) string {
	if shape == "if" {
		if order == "main-first" {
			return "if flag { " + main + " } else { " + alt + " }"
		}
		return "if flag { " + alt + " } else { " + main + " }"
	}
	if order == "main-first" {
		return "match pick {\n            Pick.A => " + main + "\n            Pick.B => " + alt + "\n        }"
	}
	return "match pick {\n            Pick.B => " + alt + "\n            Pick.A => " + main + "\n        }"
}

type ownershipMatrixBranch struct {
	shape, order, pos string
}

func ownershipMatrixFlagFor(branch *ownershipMatrixBranch) string {
	if branch == nil {
		return "true"
	}
	if branch.shape == "if" {
		if branch.order == "main-first" {
			return "true"
		}
		return "false"
	}
	return "true"
}

// ownershipMatrixIndent mirrors Python's `prefix + l for l in "\n".join(lines).split("\n")`.
func ownershipMatrixIndent(prefix string, lines []string) []string {
	split := strings.Split(strings.Join(lines, "\n"), "\n")
	out := make([]string, len(split))
	for i, l := range split {
		out[i] = prefix + l
	}
	return out
}

// ownershipMatrixCase is one matrix-family program (Python class Program). An empty
// recipe string stands for Python's None (alt recipes and boundary_layer results).
type ownershipMatrixCase struct {
	fix, channel string
	depth        int
	boundary     string
	b            int
	wrapper      string
	wpos         string
	branch       *ownershipMatrixBranch
	consumerIn   bool // failure channel: recover<WithFile> inside the boundary
	succ         []string
	acq          []int
	producer     string // direct | callback (producer routed through an effect callback parameter)
	helper       bool   // boundary layers 1..b execute inside a helper function that returns the produced value
	raising      bool   // the recovered boundary-layer recipe raises Nope, so the handler runs
	n            int
}

func ownershipMatrixNewCase(fix, channel string, depth int, boundary string, b int, wrapper, wpos string, branch *ownershipMatrixBranch, consumerIn bool, producer string, helper bool) *ownershipMatrixCase {
	key := ownershipMatrixKey{channel, depth}
	return &ownershipMatrixCase{
		fix: fix, channel: channel, depth: depth, boundary: boundary, b: b,
		wrapper: wrapper, wpos: wpos, branch: branch, consumerIn: consumerIn,
		succ: ownershipMatrixSucc[key], acq: ownershipMatrixAcq[key],
		producer: producer, helper: helper,
	}
}

// ---- naming / oracle -------------------------------------------------

func (p *ownershipMatrixCase) name() string {
	parts := []string{ownershipMatrixProducer[p.channel][0] + strconv.Itoa(p.depth), p.boundary + "@" + strconv.Itoa(p.b)}
	if p.producer == "callback" {
		parts = append(parts, "cb")
	}
	if p.helper {
		parts = append(parts, "helper")
	}
	if p.wrapper == "none" {
		parts = append(parts, "plain")
	} else {
		parts = append(parts, p.wrapper+"-"+p.wpos)
	}
	if p.consumerIn {
		parts = append(parts, "handler-in")
	}
	if p.branch != nil {
		parts = append(parts, p.branch.shape+"-"+p.branch.order+"-"+p.branch.pos)
	}
	if p.raising {
		parts = append(parts, "raising")
	}
	return strings.Join(parts, "_")
}

// ownershipMatrixAcquiringHandlers are the recover<Nope> handlers whose own
// invocation acquires the File they return. A pure handler returning a
// recipe (hH, hHH, hP, hPP) acquires only when a later layer executes it.
var ownershipMatrixAcquiringHandlers = []string{"hFile", "hHolder", "hPair"}

// handlerAcquiresAtBoundary reports a recover<Nope> wrapper placed inside the
// boundary whose handler acquires: Nope is in every producer row, so the
// handler may run inside the boundary layer's execution, and the handle it
// returns is acquired at layer b whether or not one run raises Nope.
func (p *ownershipMatrixCase) handlerAcquiresAtBoundary() bool {
	inside := p.wpos == "in" || p.boundary == "run" || p.boundary == "scope"
	return p.wrapper == "recover" && inside && slices.Contains(ownershipMatrixAcquiringHandlers, ownershipMatrixHandler[p.succ[p.b-1]])
}

// oracle is the program's class: executed truth, or the declared-row policy
// for a branch between two forked children. When both fail, the owner closes
// over the joined child's failure and the unselected child's, a composite
// cause no handler matches (design §5.3 rule 6); the checker refuses it as a
// row failure (EF107), whatever the handles do.
func (p *ownershipMatrixCase) oracle() string {
	if p.oracleUnsafe() {
		return "unsafe"
	}
	if p.forkBranchPolicy() {
		return ownershipDeclaredRowPolicy
	}
	return "safe"
}

// ownershipDeclaredRowPolicy is the truth of a row refused for what its
// declared rows admit, not for what its bodies raise. A branch between two
// forked children leaves one of them unobserved while the other is joined.
// The producers of these rows only build values, so the recorded schedule
// executes safe; their declared rows contain IoError and Nope, and a producer
// declared to raise those could fail, so the checker charges the declared
// rows (a7bc5ae did too). The row is therefore not executed truth: its
// raising twin (<row>_raising), whose children actually fail, executes
// row-unsafe and witnesses the shape the policy refuses.
const ownershipDeclaredRowPolicy = "declared-row-policy"

// forkBranch reports a branch between two forked children.
func (p *ownershipMatrixCase) forkBranch() bool { return p.boundary == "fork" && p.branch != nil }

// forkBranchPolicy reports a row of the declared-row policy.
func (p *ownershipMatrixCase) forkBranchPolicy() bool {
	return p.forkBranch() && !p.raising
}

func (p *ownershipMatrixCase) oracleUnsafe() bool {
	acquires := slices.Contains(p.acq, p.b) || p.handlerAcquiresAtBoundary()
	if !slices.Contains(ownershipMatrixClosing, p.boundary) || !acquires {
		return false
	}
	if p.channel == "failure" && p.consumerIn && p.b == p.depth {
		return false // the handler reads the payload before the owner closes
	}
	if p.channel == "success" && p.boundary == "scope" && p.b == p.depth && p.depth > 1 {
		return false
	}
	return true
}

// ---- code fragments --------------------------------------------------

func (p *ownershipMatrixCase) tmp(base string) string {
	p.n++
	return base + strconv.Itoa(p.n)
}

func (p *ownershipMatrixCase) wIn(y string, layer int) string {
	if p.raising {
		y = "raiseNope(" + y + ")"
	}
	if p.wrapper == "none" || (p.wpos != "in" && p.boundary != "run" && p.boundary != "scope") {
		return y
	}
	return p.wrap(y, layer)
}

func (p *ownershipMatrixCase) wOut(y string, layer int) string {
	if p.wrapper == "none" || p.wpos != "out" || p.boundary == "run" || p.boundary == "scope" {
		return y
	}
	return p.wrap(y, layer)
}

func (p *ownershipMatrixCase) wrap(y string, layer int) string {
	switch p.wrapper {
	case "provide":
		return y + ".provide<Console>(Stdout)"
	case "recover":
		return y + ".recover<Nope>(" + ownershipMatrixHandler[p.succ[layer-1]] + ")"
	case "catch":
		return y + `.catch<Nope>("alt")`
	}
	panic(p.wrapper)
}

// observeTail executes the final layer recipe and observes the File.
func (p *ownershipMatrixCase) observeTail(recipe string, lines *[]string, layer int) {
	switch {
	case p.channel == "success" && p.depth == 1:
		v := p.tmp("v")
		*lines = append(*lines, "let "+v+" = run "+recipe)
		*lines = append(*lines, "run readFile("+v+`).catch<IoError>("closed")`)
	case slices.Contains(ownershipMatrixRecord, p.channel):
		v := p.tmp("v")
		*lines = append(*lines, "let "+v+" = run "+recipe)
		p.observeValue(v, lines)
	case p.channel == "success":
		*lines = append(*lines, "run "+recipe+`.catch<IoError>("closed")`)
	case p.consumerIn && p.b == layer:
		*lines = append(*lines, "run "+recipe+`.catch<IoError>("closed")`)
	default:
		*lines = append(*lines, "run "+recipe+`.recover<WithFile>(retrieve).catch<IoError>("closed")`)
	}
}

// observeValue observes a produced value that contains the handle(s); the last line is the scope result.
func (p *ownershipMatrixCase) observeValue(v string, lines *[]string) {
	if p.channel == "pair" {
		ra, rb := p.tmp("ra"), p.tmp("rb")
		*lines = append(*lines, "let "+ra+" = run readFile("+v+`.a).catch<IoError>("closed")`)
		*lines = append(*lines, "let "+rb+" = run readFile("+v+`.b).catch<IoError>("closed")`)
		*lines = append(*lines, "if "+ra+` == "closed" { "closed" } else { `+rb+" }")
		return
	}
	target := v
	if p.channel == "field" {
		target = v + ".file"
	}
	*lines = append(*lines, "run readFile("+target+`).catch<IoError>("closed")`)
}

func (p *ownershipMatrixCase) handlerIn(y string, layer int) string {
	if p.channel == "failure" && p.consumerIn && layer == p.depth && p.depth == p.b {
		return y + ".recover<WithFile>(retrieve)"
	}
	return y
}

// boundaryRecipe applies wrapper(s) and the boundary combinator to recipe expression y.
// It returns a recipe expression whose execution is the boundary layer.
func (p *ownershipMatrixCase) boundaryRecipe(y string, layer int, lines *[]string) string {
	inner := p.handlerIn(p.wIn(y, layer), layer)
	switch p.boundary {
	case "timeout":
		return p.wOut(inner+".timeout(5000)", layer)
	case "fork":
		fk := p.tmp("fiber")
		*lines = append(*lines, "let "+fk+" = fork "+inner)
		return p.wOut(fk+".join()", layer)
	case "provision":
		return p.wOut(inner+".provide(Clocked)", layer)
	}
	return inner // run
}

// settle lets the forked children of a raising twin fail before the consumer
// joins one of them, so the failure of the unselected child is not racing the
// close of the owner (a child cancelled before it fails raises nothing).
func (p *ownershipMatrixCase) settle(lines *[]string) {
	if p.raising && p.forkBranch() {
		*lines = append(*lines, "run Scheduler.sleep(30)")
	}
}

func (p *ownershipMatrixCase) forkFiber(y string, layer int, lines *[]string) string {
	inner := p.handlerIn(p.wIn(y, layer), layer)
	fk := p.tmp("fiber")
	*lines = append(*lines, "let "+fk+" = fork "+inner)
	return fk
}

// scopeExec emits `scope { run y }` with the produced value escaping (or observed inside).
func (p *ownershipMatrixCase) scopeExec(y string, layer int, final bool) ([]string, string) {
	inner := p.handlerIn(p.wIn(y, layer), layer)
	succ := p.succ[layer-1]
	if final {
		// success depth>1: observe inside the scope (control)
		var tmp []string
		p.observeTail(inner, &tmp, layer)
		return []string{"scope { " + tmp[len(tmp)-1] + " }"}, ""
	}
	if succ == "File" || succ == "Holder" || succ == "Pair" {
		v := p.tmp("v")
		return []string{"let " + v + " = scope { run " + inner + " }"}, v
	}
	bx := p.tmp("box")
	v := p.tmp("m")
	return []string{"let " + bx + " = scope {", "    let inner = run " + inner, "    " + ownershipMatrixBox[succ] + " { task: inner }", "}",
		"let " + v + " = " + bx + ".task"}, v
}

// ---- whole consumer --------------------------------------------------

// chain emits layers 1..depth into lines. main/alt are the layer-1 recipe expressions.
func (p *ownershipMatrixCase) chain(lines *[]string, main, alt string) {
	for layer := 1; layer <= p.depth; layer++ {
		final := layer == p.depth
		if layer != p.b {
			if final {
				p.observeTail(main, lines, layer)
				return
			}
			v := p.tmp("m")
			*lines = append(*lines, "let "+v+" = run "+main)
			main = v
			if alt != "" && layer < p.b {
				a := p.tmp("a")
				*lines = append(*lines, "let "+a+" = run "+alt)
				alt = a
			}
			continue
		}
		main = p.boundaryLayer(lines, main, alt, layer, final)
		alt = ""
		if main == "" {
			return
		}
	}
}

func (p *ownershipMatrixCase) boundaryLayer(lines *[]string, main, alt string, layer int, final bool) string {
	br := p.branch
	pos := ""
	if br != nil {
		pos = br.pos
	}
	if p.boundary == "scope" {
		// success depth 1: the produced File escapes the scope and is read outside
		inside := final && !(p.channel == "success" && p.depth == 1) && !slices.Contains(ownershipMatrixRecord, p.channel)
		var v string
		switch {
		case br != nil && pos == "value":
			rc := p.tmp("rc")
			*lines = append(*lines, "let "+rc+" = "+ownershipMatrixBranchExpr(br.shape, br.order, main, alt))
			var body []string
			body, v = p.scopeExec(rc, layer, inside)
			*lines = append(*lines, body...)
		case br != nil && pos == "result":
			bm, vm := p.scopeExec(main, layer, inside)
			ba, va := p.scopeExec(alt, layer, inside)
			*lines = append(*lines, bm...)
			*lines = append(*lines, ba...)
			v = p.tmp("m")
			*lines = append(*lines, "let "+v+" = "+ownershipMatrixBranchExpr(br.shape, br.order, vm, va))
		default:
			var body []string
			body, v = p.scopeExec(main, layer, inside)
			*lines = append(*lines, body...)
		}
		if inside {
			return ""
		}
		if final {
			p.observeValue(v, lines)
			return ""
		}
		return v
	}
	// run / timeout / fork / provision
	var recipe string
	switch {
	case br != nil && pos == "value":
		if p.boundary == "fork" {
			fm := p.forkFiber(main, layer, lines)
			fa := p.forkFiber(alt, layer, lines)
			p.settle(lines)
			fc := p.tmp("fiber")
			*lines = append(*lines, "let "+fc+" = "+ownershipMatrixBranchExpr(br.shape, br.order, fm, fa))
			if p.channel == "failure" && layer == p.depth {
				// the unselected child also fails; join it so its failure does
				// not end the scope before the observation
				*lines = append(*lines, "let drained = run "+fa+`.join().recover<WithFile>(retrieve).catch<IoError>("drained")`)
			}
			recipe = p.wOut(fc+".join()", layer)
		} else {
			rm := p.boundaryRecipe(main, layer, lines)
			ra := p.boundaryRecipe(alt, layer, lines)
			rc := p.tmp("rc")
			*lines = append(*lines, "let "+rc+" = "+ownershipMatrixBranchExpr(br.shape, br.order, rm, ra))
			recipe = rc
		}
	case br != nil && pos == "result":
		rm := p.boundaryRecipe(main, layer, lines)
		ra := p.boundaryRecipe(alt, layer, lines)
		p.settle(lines)
		vm, va, v := p.tmp("vm"), p.tmp("va"), p.tmp("m")
		*lines = append(*lines, "let "+vm+" = run "+rm)
		*lines = append(*lines, "let "+va+" = run "+ra)
		*lines = append(*lines, "let "+v+" = "+ownershipMatrixBranchExpr(br.shape, br.order, vm, va))
		if final { // success depth 1 / record channels: the produced value is observed
			p.observeValue(v, lines)
			return ""
		}
		return v
	default:
		recipe = p.boundaryRecipe(main, layer, lines)
	}
	if final {
		p.observeTail(recipe, lines, layer)
		return ""
	}
	v := p.tmp("m")
	*lines = append(*lines, "let "+v+" = run "+recipe)
	return v
}

func (p *ownershipMatrixCase) source() string {
	prod := ownershipMatrixProducer[p.channel]
	d := strconv.Itoa(p.depth)
	main, alt := prod[0]+d+"()", ""
	if p.branch != nil {
		alt = prod[1] + d + "(outer)"
	}
	if p.producer == "callback" {
		main = "via_" + p.channel + d + "(" + prod[0] + d + ")"
	}
	if p.helper {
		return p.helperSource(main)
	}
	out := []string{ownershipMatrixHeader(p.fix)}
	open := ownershipMatrixOpen(p.fix)
	stage := p.channel == "failure" && p.boundary == "scope" && p.b == p.depth
	var body []string
	if stage {
		// The failure must leave the scope as a failure: the chain lives in a
		// helper whose scope encloses only the failing layer.
		var lines []string
		p.chainStage(&lines, main, alt)
		out = append(out, "effect fn stage(flag: bool, pick: Pick, outer: File) -> string raises "+ownershipMatrixRowsAll+" uses { Scheduler } {")
		out = append(out, ownershipMatrixIndent("    ", lines)...)
		out = append(out, "}")
		tail := `run stage(flag, pick, outer).recover<WithFile>(retrieve).catch<IoError>("closed")`
		if p.consumerIn {
			tail = `run stage(flag, pick, outer).catch<IoError>("closed")`
		}
		body = []string{ownershipMatrixOpenAnd(open), tail}
	} else {
		if p.branch != nil {
			body = append(body, ownershipMatrixOpenAnd(open))
		}
		p.chain(&body, main, alt)
	}
	if p.raising {
		// The recovered recipe still runs, then fails with Nope so the
		// handler takes over inside the boundary layer.
		row := ownershipMatrixR
		failing := p.channel == "failure" && p.succ[p.b-1] == "string"
		if failing {
			row = ownershipMatrixRF // the failing layer's recipe also raises WithFile
		}
		layer := ownershipMatrixEff(p.succ[p.b-1], row)
		out = append(out, "effect fn raiseNope(recipe: "+layer+") -> "+p.succ[p.b-1]+" raises "+row+" {")
		if !failing {
			out = append(out, "    let produced = run recipe")
		} // else the recipe fails with WithFile first, which the consumer recovers: fail with Nope instead
		out = append(out, "    fail Nope { code: 1 }")
		out = append(out, "}")
	}
	out = append(out, "effect fn consume(flag: bool, pick: Pick) -> string raises "+ownershipMatrixRowsAll+" uses { Scheduler } {")
	out = append(out, "    scope {")
	out = append(out, ownershipMatrixIndent("        ", body)...)
	out = append(out, "    }")
	out = append(out, "}")
	out = append(out, "effect fn main() -> void raises "+ownershipMatrixRowsAll+" {")
	handled := ""
	if p.raising && p.forkBranch() {
		// A solitary Nope is handled here: only a composite cause escapes.
		handled = `.catch<Nope>("n")`
	}
	out = append(out, "    run Console.log(run consume("+ownershipMatrixFlagFor(p.branch)+", Pick.A {})"+handled+".provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)")
	out = append(out, "}")
	return strings.Join(out, "\n") + "\n"
}

// helperSource: layers 1..b (the boundary at b) run inside `hop`, which returns layer b's produced value;
// the consumer executes the remaining layers and observes outside the helper.
func (p *ownershipMatrixCase) helperSource(main string) string {
	out := []string{ownershipMatrixHeader(p.fix)}
	var hl []string
	for layer := 1; layer < p.b; layer++ {
		v := p.tmp("m")
		hl = append(hl, "let "+v+" = run "+main)
		main = v
	}
	succ := p.succ[p.b-1]
	if p.boundary == "scope" {
		if succ == "File" || succ == "Holder" || succ == "Pair" {
			hl = append(hl, "scope { run "+main+" }")
		} else {
			bx := p.tmp("box")
			hl = append(hl, "let "+bx+" = scope {", "    let inner = run "+main, "    "+ownershipMatrixBox[succ]+" { task: inner }", "}", bx+".task")
		}
	} else {
		recipe := p.boundaryRecipe(main, p.b, &hl)
		hl = append(hl, "run "+recipe)
	}
	out = append(out, "effect fn hop() -> "+succ+" raises "+ownershipMatrixRowsAll+" uses { Scheduler } {")
	out = append(out, ownershipMatrixIndent("    ", hl)...)
	out = append(out, "}")
	var body []string
	cur := p.tmp("m")
	body = append(body, "let "+cur+" = run hop()")
	for layer := p.b + 1; layer <= p.depth; layer++ {
		if layer == p.depth {
			p.observeTail(cur, &body, layer)
			break
		}
		v := p.tmp("m")
		body = append(body, "let "+v+" = run "+cur)
		cur = v
	}
	out = append(out, "effect fn consume(flag: bool, pick: Pick) -> string raises "+ownershipMatrixRowsAll+" uses { Scheduler } {")
	out = append(out, "    scope {")
	out = append(out, ownershipMatrixIndent("        ", body)...)
	out = append(out, "    }")
	out = append(out, "}")
	out = append(out, "effect fn main() -> void raises "+ownershipMatrixRowsAll+" {")
	out = append(out, "    run Console.log(run consume(true, Pick.A {}).provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)")
	out = append(out, "}")
	return strings.Join(out, "\n") + "\n"
}

func (p *ownershipMatrixCase) chainStage(lines *[]string, main, alt string) {
	for layer := 1; layer < p.depth; layer++ {
		v := p.tmp("m")
		*lines = append(*lines, "let "+v+" = run "+main)
		main = v
		if alt != "" {
			a := p.tmp("a")
			*lines = append(*lines, "let "+a+" = run "+alt)
			alt = a
		}
	}
	if p.branch != nil {
		rc := p.tmp("rc")
		*lines = append(*lines, "let "+rc+" = "+ownershipMatrixBranchExpr(p.branch.shape, p.branch.order, main, alt))
		main = rc
	}
	y := p.handlerIn(p.wIn(main, p.depth), p.depth)
	*lines = append(*lines, "scope { run "+y+" }")
}

func ownershipMatrixOpenAnd(open string) string {
	return "let outer = " + open
}

// ownershipMatrixCases enumerates the matrix family (Python programs()), before name dedupe.
func ownershipMatrixCases(fix string) []*ownershipMatrixCase {
	type variant struct {
		wrapper, wpos string
		cin           bool
	}
	var out []*ownershipMatrixCase
	for _, channel := range []string{"success", "field", "pair", "failure"} {
		for _, depth := range []int{1, 2, 3} {
			succ, ok := ownershipMatrixSucc[ownershipMatrixKey{channel, depth}]
			if !ok {
				continue
			}
			for _, boundary := range []string{"run", "timeout", "fork", "scope", "provision"} {
				last := depth
				if boundary == "run" {
					last = 1
				}
				for b := 1; b <= last; b++ {
					variants := []variant{{"none", "", false}}
					wposes := []string{"in", "out"}
					if boundary == "run" || boundary == "scope" {
						wposes = []string{"in"}
					}
					for _, wrapper := range []string{"provide", "recover", "catch"} {
						if wrapper == "catch" && succ[b-1] != "string" {
							continue
						}
						for _, wpos := range wposes {
							variants = append(variants, variant{wrapper, wpos, false})
						}
					}
					if channel == "failure" && b == depth && boundary != "run" {
						variants = append(variants, variant{"none", "", true})
					}
					for _, v := range variants {
						out = append(out, ownershipMatrixNewCase(fix, channel, depth, boundary, b, v.wrapper, v.wpos, nil, v.cin, "direct", false))
					}
					// the producer routed through an effect callback parameter (plain rows)
					out = append(out, ownershipMatrixNewCase(fix, channel, depth, boundary, b, "none", "", nil, false, "callback", false))
					// the boundary inside a helper whose summary carries the boundary's owners
					if boundary != "run" && b < depth {
						out = append(out, ownershipMatrixNewCase(fix, channel, depth, boundary, b, "none", "", nil, false, "direct", true))
					}
					// branches: no wrapper
					positions := []string{"value"}
					finalHasHandle := (channel == "success" && depth == 1) || slices.Contains(ownershipMatrixRecord, channel)
					if b < depth || finalHasHandle {
						positions = append(positions, "result")
					}
					if channel == "failure" && boundary == "scope" && b == depth {
						positions = []string{"value"}
					}
					for _, shape := range []string{"if", "match"} {
						for _, order := range []string{"main-first", "main-second"} {
							for _, pos := range positions {
								out = append(out, ownershipMatrixNewCase(fix, channel, depth, boundary, b, "none", "", &ownershipMatrixBranch{shape, order, pos}, false, "direct", false))
							}
						}
					}
				}
			}
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Explicit families (review M7): recovery handler variants, a handler that
// returns the payload handle, forked-child failure through join / interrupt /
// owner closure, and handle-free row-soundness programs runnable on JS.

const ownershipMatrixBaseErrors = `error WithFile { file: File }
effect fn readFile(file: File) -> string raises { IoError } {
    run Files.readText(file).provide<Files>(LiveFiles)
}
layer Clocked { Clock = LiveClock }
`

func ownershipMatrixOracle(unsafe bool) string {
	if unsafe {
		return "unsafe"
	}
	return "safe"
}

func ownershipMatrixPositions(boundary string) []string {
	if boundary == "run" {
		return []string{"in"}
	}
	return []string{"in", "out"}
}

// ownershipMatrixHandlerFamily: recover<WithFile> with a reading or ignoring handler, inside or outside each boundary.
func ownershipMatrixHandlerFamily(fix string) []ownershipMatrixProgram {
	open := ownershipMatrixOpen(fix)
	head := ownershipMatrixBaseErrors + `effect fn rejected(file: File) -> string raises { WithFile } {
    fail WithFile { file: file }
}
effect fn f1() -> string raises { WithFile, IoError } {
    let file = ` + open + `
    run rejected(file)
}
effect fn read(failure: WithFile) -> string raises { IoError } { run readFile(failure.file).catch<IoError>("closed") }
fn ignore(failure: WithFile) -> string { "fixture-ok" }
`
	var out []ownershipMatrixProgram
	for _, boundary := range []string{"run", "timeout", "fork", "scope", "provision"} {
		for _, pos := range ownershipMatrixPositions(boundary) {
			for _, h := range []string{"read", "ignore"} {
				rec := ".recover<WithFile>(" + h + ")"
				var pre []string
				extra, tail := "", ""
				switch boundary {
				case "run":
					tail = "run f1()" + rec
				case "timeout":
					if pos == "in" {
						tail = "run f1()" + rec + ".timeout(5000)"
					} else {
						tail = "run f1().timeout(5000)" + rec
					}
				case "provision":
					if pos == "in" {
						tail = "run f1()" + rec + ".provide(Clocked)"
					} else {
						tail = "run f1().provide(Clocked)" + rec
					}
				case "fork":
					inRec, outRec := "", ""
					if pos == "in" {
						inRec = rec
					} else {
						outRec = rec
					}
					pre = []string{"let fiber = fork f1()" + inRec}
					tail = "run fiber.join()" + outRec
				default:
					if pos == "in" {
						tail = "scope { run f1()" + rec + " }"
					} else {
						extra = "effect fn stage() -> string raises { WithFile, IoError } { scope { run f1() } }\n"
						tail = "run stage()" + rec
					}
				}
				body := append(pre, tail)
				src := head + extra + ownershipMatrixConsumeMain(body, "{ WithFile, IoError, Timeout }")
				unsafe := h == "read" && slices.Contains(ownershipMatrixClosing, boundary) && pos == "out"
				out = append(out, ownershipMatrixProgram{Name: "hd_" + boundary + "_" + pos + "_" + h, Source: src, Oracle: ownershipMatrixOracle(unsafe)})
			}
		}
	}
	return out
}

// ownershipMatrixKeepFamily: a pure handler returns the payload handle; the File is read outside the boundary.
func ownershipMatrixKeepFamily(fix string) []ownershipMatrixProgram {
	open := ownershipMatrixOpen(fix)
	head := ownershipMatrixBaseErrors + `effect fn rejectedF(file: File) -> File raises { WithFile } {
    fail WithFile { file: file }
}
effect fn e1() -> File raises { WithFile, IoError } {
    let file = ` + open + `
    run rejectedF(file)
}
fn keep(failure: WithFile) -> File { failure.file }
`
	var out []ownershipMatrixProgram
	for _, boundary := range []string{"run", "timeout", "fork", "scope", "provision"} {
		for _, pos := range ownershipMatrixPositions(boundary) {
			rec := ".recover<WithFile>(keep)"
			var pre []string
			extra, get := "", ""
			switch boundary {
			case "run":
				get = "run e1()" + rec
			case "timeout":
				if pos == "in" {
					get = "run e1()" + rec + ".timeout(5000)"
				} else {
					get = "run e1().timeout(5000)" + rec
				}
			case "provision":
				if pos == "in" {
					get = "run e1()" + rec + ".provide(Clocked)"
				} else {
					get = "run e1().provide(Clocked)" + rec
				}
			case "fork":
				inRec, outRec := "", ""
				if pos == "in" {
					inRec = rec
				} else {
					outRec = rec
				}
				pre = []string{"let fiber = fork e1()" + inRec}
				get = "run fiber.join()" + outRec
			default:
				if pos == "in" {
					get = "scope { run e1()" + rec + " }"
				} else {
					extra = "effect fn stageE() -> File raises { WithFile, IoError } { scope { run e1() } }\n"
					get = "run stageE()" + rec
				}
			}
			body := append(pre, "let kept = "+get, `run readFile(kept).catch<IoError>("closed")`)
			src := head + extra + ownershipMatrixConsumeMain(body, "{ WithFile, IoError, Timeout }")
			unsafe := slices.Contains(ownershipMatrixClosing, boundary)
			out = append(out, ownershipMatrixProgram{Name: "kp_" + boundary + "_" + pos, Source: src, Oracle: ownershipMatrixOracle(unsafe)})
		}
	}
	return out
}

func ownershipMatrixConsumeMain(body []string, row string) string {
	out := []string{"effect fn consume() -> string raises " + row + " uses { Scheduler } {", "    scope {"}
	for _, l := range body {
		out = append(out, "        "+l)
	}
	out = append(out, "    }", "}", "effect fn main() -> void raises "+row+" {",
		"    run Console.log(run consume().provide<Scheduler>(LiveScheduler)).provide<Console>(Stdout)", "}")
	return strings.Join(out, "\n") + "\n"
}

func ownershipMatrixRow(labels ...string) string {
	var ls []string
	for _, l := range labels {
		if l != "" {
			ls = append(ls, l)
		}
	}
	if len(ls) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(ls, ", ") + " }"
}

// ownershipMatrixChildPrograms: a forked child acquires a handle (payload=file) or nothing (payload=boom) and fails.
// observe: join | interrupt | none (owner closure); position of recover<E>:
//
//	child    - inside the child (the handler runs while the child owner is open)
//	observer - on the join/interrupt recipe inside the forking body
//	stage    - on stage() inside the scope that owns the fork
//	outside  - on scope { run stage() } from an outer function
func ownershipMatrixChildPrograms(fix, payload string) []ownershipMatrixProgram {
	file := payload == "file"
	open := ownershipMatrixOpen(fix)
	var head, e, jrow string
	var handlers []string
	if file {
		head = ownershipMatrixBaseErrors + `effect fn rejected(file: File) -> string raises { WithFile } {
    fail WithFile { file: file }
}
effect fn job() -> string raises { WithFile, IoError } {
    let file = ` + open + `
    run rejected(file)
}
effect fn read(failure: WithFile) -> string raises { IoError } { run readFile(failure.file).catch<IoError>("closed") }
fn ignore(failure: WithFile) -> string { "fixture-ok" }
effect fn readV(failure: WithFile) -> void raises { IoError } {
    let text = run readFile(failure.file).catch<IoError>("closed")
    run Console.log(text).provide<Console>(Stdout)
}
fn ignoreV(failure: WithFile) -> void { void }
`
		e, handlers, jrow = "WithFile", []string{"read", "ignore"}, "IoError"
	} else {
		head = `error Boom { code: i64 }
effect fn job() -> string raises { Boom } {
    fail Boom { code: 7 }
}
fn ignore(failure: Boom) -> string { "fixture-ok" }
fn ignoreV(failure: Boom) -> void { void }
`
		e, handlers, jrow = "Boom", []string{"ignore"}, ""
	}
	var out []ownershipMatrixProgram
	for _, observe := range []string{"join", "interrupt", "none"} {
		for _, pos := range []string{"child", "observer", "stage", "outside"} {
			if pos == "observer" && observe == "none" {
				continue
			}
			for _, h := range handlers {
				hv := h + "V"
				job := "job()"
				if pos == "child" {
					job = "job().recover<" + e + ">(" + h + ")"
				}
				lines := []string{"let fiber = fork " + job, "run Scheduler.sleep(300)"}
				switch observe {
				case "join":
					rec := ""
					if pos == "observer" {
						rec = ".recover<" + e + ">(" + h + ")"
					}
					lines = append(lines, "run fiber.join()"+rec)
				case "interrupt":
					rec := ""
					if pos == "observer" {
						rec = ".recover<" + e + ">(" + hv + ")"
					}
					lines = append(lines, "run fiber.interrupt()"+rec)
					lines = append(lines, `"fixture-ok"`)
				default:
					lines = append(lines, `"fixture-ok"`)
				}
				stageRaises := ownershipMatrixRow(e, jrow)
				if pos == "child" || pos == "observer" {
					stageRaises = ownershipMatrixRow(jrow)
				}
				src := head + "effect fn stage() -> string raises " + stageRaises + " uses { Scheduler } {\n"
				for _, l := range lines {
					src += "    " + l + "\n"
				}
				src += "}\n"
				var body []string
				switch pos {
				case "outside":
					src += "effect fn inner() -> string raises " + ownershipMatrixRow(e, jrow) + " uses { Scheduler } {\n    scope { run stage() }\n}\n"
					body = []string{"run inner().recover<" + e + ">(" + h + ")"}
				case "stage":
					body = []string{"run stage().recover<" + e + ">(" + h + ")"}
				default:
					body = []string{"run stage()"}
				}
				// consume and main declare only the handle-free rows: an escaping E is a row violation
				src += ownershipMatrixConsumeMain(body, ownershipMatrixRow(jrow))
				var oracle string
				switch {
				case observe == "none" && pos == "stage":
					oracle = "row-unsafe" // the child's failure surfaces at consume's scope close, outside recover
				case file && h == "read" && (pos == "observer" || pos == "stage" || pos == "outside"):
					oracle = "unsafe" // the payload's File belongs to the child, closed before dispatch
				default:
					oracle = "safe"
				}
				prefix := "rw"
				if file {
					prefix = "ch"
				}
				out = append(out, ownershipMatrixProgram{Name: prefix + "_" + observe + "_" + pos + "_" + h, Source: src, Oracle: oracle, JSRunnable: !file})
			}
		}
	}
	return out
}

// ownershipMatrixFactoryFamily: recover<WithFile> inside a factory's produced layer (NB1). The factory opens the file
// (inside) or receives it (param); the boundary closes the factory's execution; the produced recipe runs later.
func ownershipMatrixFactoryFamily(fix string) []ownershipMatrixProgram {
	open := ownershipMatrixOpen(fix)
	head := ownershipMatrixBaseErrors + `effect fn rejected(file: File) -> string raises { WithFile } {
    fail WithFile { file: file }
}
effect fn read(failure: WithFile) -> string raises { IoError } { run readFile(failure.file).catch<IoError>("closed") }
fn ignore(failure: WithFile) -> string { "fixture-ok" }
`
	var out []ownershipMatrixProgram
	for _, form := range []string{"inside", "param"} {
		for _, h := range []string{"read", "ignore"} {
			var fac string
			if form == "inside" {
				fac = "effect fn w2() -> Effect<string, { IoError }> raises { IoError } {\n" +
					"    let g = " + open + "\n    rejected(g).recover<WithFile>(" + h + ")\n}\n"
			} else {
				fac = "effect fn wrapP(f: File) -> Effect<string, { IoError }> raises { IoError } {\n" +
					"    rejected(f).recover<WithFile>(" + h + ")\n}\n" +
					"effect fn w2() -> Effect<string, { IoError }> raises { IoError } {\n" +
					"    let g = " + open + "\n    run wrapP(g)\n}\n"
			}
			// a scope cannot yield an unexecuted recipe (EF105), so the scope edge has no factory form
			for _, boundary := range []string{"run", "timeout", "fork", "provision"} {
				var body []string
				switch boundary {
				case "run":
					body = []string{"let m = run w2()", "run m"}
				case "timeout":
					body = []string{"let m = run w2().timeout(5000)", "run m"}
				case "provision":
					body = []string{"let m = run w2().provide(Clocked)", "run m"}
				case "fork":
					body = []string{"let fiber = fork w2()", "let m = run fiber.join()", "run m"}
				default:
					panic(boundary)
				}
				src := head + fac + ownershipMatrixConsumeMain(body, "{ IoError, Timeout }")
				unsafe := h == "read" && slices.Contains(ownershipMatrixClosing, boundary)
				out = append(out, ownershipMatrixProgram{Name: "fr_" + boundary + "_" + form + "_" + h, Source: src, Oracle: ownershipMatrixOracle(unsafe)})
			}
		}
	}
	return out
}

// ownershipMatrixExitShapes (NB2): pending must be checked at every exit; a join observes only where it completes.
var ownershipMatrixExitShapes = []struct {
	shape, stage string
	body         []string
	oracle       string
}{
	{"exitjoin", `effect fn stage() -> string raises { Nope, E } uses { Scheduler } {
    let fiber = fork job()
    let early = run bail()
    run fiber.join()
}
`, []string{`let r = run stage().catch<Nope>("recovered-early").catch<E>("joined")`, "run Scheduler.sleep(200)", "r"}, "row-unsafe"},
	{"exitjoincontrol", `effect fn stage() -> string raises { Nope, E } uses { Scheduler } {
    let fiber = fork job()
    run fiber.join()
}
`, []string{`let r = run stage().catch<Nope>("recovered-early").catch<E>("joined")`, "run Scheduler.sleep(200)", "r"}, "safe"},
	{"timeoutjoin", `effect fn stage() -> string raises { E } uses { Scheduler } {
    let fiber = fork slowJob()
    let r = run fiber.join().timeout(10).catch<Timeout>("timed-out")
    run Scheduler.sleep(300)
    r
}
`, []string{`run stage().catch<E>("joined")`}, "row-unsafe"},
	{"forkedobserver", `effect fn stage() -> string raises { E } uses { Scheduler } {
    let f = fork slowJob()
    let observer = fork f.join()
    run Scheduler.sleep(10)
    run observer.interrupt().catch<E>(void)
    run Scheduler.sleep(300)
    "done"
}
`, []string{`run stage().catch<E>("joined")`}, "row-unsafe"},
	{"nestedscope", `effect fn stage() -> string raises { E } uses { Scheduler } {
    let fiber = fork job()
    run Scheduler.sleep(50)
    scope { run fiber.join() }
}
`, []string{`run stage().catch<E>("joined")`}, "safe"},
}

// ownershipMatrixExitPrograms: NB2 shapes on a handle-free payload (Go and JS) and on a File payload (Go).
func ownershipMatrixExitPrograms(fix string) []ownershipMatrixProgram {
	open := ownershipMatrixOpen(fix)
	boom := `error Boom { code: i64 }
error Nope { code: i64 }
effect fn job() -> string raises { Boom } {
    fail Boom { code: 7 }
}
effect fn slowJob() -> string raises { Boom } uses { Scheduler } {
    run Scheduler.sleep(100)
    fail Boom { code: 7 }
}
effect fn bail() -> string raises { Nope } {
    fail Nope { code: 1 }
}
`
	var out []ownershipMatrixProgram
	for _, s := range ownershipMatrixExitShapes {
		stage := strings.ReplaceAll(s.stage, " E ", " Boom ")
		stage = strings.ReplaceAll(stage, "<E>", "<Boom>")
		stage = strings.ReplaceAll(stage, "{ E }", "{ Boom }")
		stage = strings.ReplaceAll(stage, ", E }", ", Boom }")
		body := make([]string, len(s.body))
		for i, l := range s.body {
			body[i] = strings.ReplaceAll(l, "<E>", "<Boom>")
		}
		src := boom + stage + ownershipMatrixConsumeMain(body, "{}")
		out = append(out, ownershipMatrixProgram{Name: "rw_" + s.shape, Source: src, Oracle: s.oracle, JSRunnable: true})
	}
	// the File form of the early exit: the unobserved child's payload reaches a reading handler
	src := ownershipMatrixBaseErrors + "error Nope { code: i64 }\n" + `effect fn rejected(file: File) -> string raises { WithFile } {
    fail WithFile { file: file }
}
effect fn retrieve(failure: WithFile) -> string raises { IoError } { run readFile(failure.file).catch<IoError>("closed") }
effect fn job() -> string raises { WithFile, IoError } {
    let file = ` + open + `
    run rejected(file)
}
effect fn bail() -> string raises { Nope } {
    fail Nope { code: 1 }
}
effect fn stage() -> string raises { Nope, WithFile, IoError } uses { Scheduler } {
    let fiber = fork job()
    let early = run bail()
    run fiber.join()
}
effect fn inner() -> string raises { WithFile, IoError } uses { Scheduler } {
    scope {
        let r = run stage().catch<Nope>("early").catch<WithFile>("joined")
        run Scheduler.sleep(200)
        r
    }
}
`
	src += ownershipMatrixConsumeMain([]string{"run inner().recover<WithFile>(retrieve)"}, "{ IoError }")
	out = append(out, ownershipMatrixProgram{Name: "ch_exitjoin", Source: src, Oracle: "unsafe"})
	return out
}

// ownershipMatrixRaisingTwins: for every matrix row whose verdict rests on
// an acquiring recover<Nope> handler inside a closing boundary, the same
// program with the recovered recipe raising Nope, so execution takes the
// path the row's truth covers.
func ownershipMatrixRaisingTwins(fix string) []ownershipMatrixProgram {
	var out []ownershipMatrixProgram
	names := map[string]bool{}
	for _, p := range ownershipMatrixCases(fix) {
		if !p.handlerAcquiresAtBoundary() || slices.Contains(p.acq, p.b) || !slices.Contains(ownershipMatrixClosing, p.boundary) {
			continue
		}
		p.raising = true
		if name := p.name(); !names[name] {
			names[name] = true
			out = append(out, ownershipMatrixProgram{Name: name, Source: p.source(), Oracle: p.oracle()})
		}
	}
	return out
}

// ownershipMatrixPolicyTwins: for every declared-row-policy row, the same
// program whose forked children fail with Nope (after producing their
// values) and whose consumer handles a solitary Nope. The joined child's
// failure is then handled and the unselected child's is not alone, so the
// owner raises a composite cause: executed row-unsafe on the recorded
// schedule.
func ownershipMatrixPolicyTwins(fix string) []ownershipMatrixProgram {
	var out []ownershipMatrixProgram
	names := map[string]bool{}
	for _, p := range ownershipMatrixCases(fix) {
		if !p.forkBranchPolicy() {
			continue
		}
		p.raising = true
		if name := p.name(); !names[name] {
			names[name] = true
			out = append(out, ownershipMatrixProgram{Name: name, Source: p.source(), Oracle: "row-unsafe"})
		}
	}
	return out
}

func ownershipMatrixExplicitPrograms(fix string) []ownershipMatrixProgram {
	var out []ownershipMatrixProgram
	out = append(out, ownershipMatrixHandlerFamily(fix)...)
	out = append(out, ownershipMatrixKeepFamily(fix)...)
	out = append(out, ownershipMatrixChildPrograms(fix, "file")...)
	out = append(out, ownershipMatrixChildPrograms(fix, "boom")...)
	out = append(out, ownershipMatrixFactoryFamily(fix)...)
	out = append(out, ownershipMatrixExitPrograms(fix)...)
	out = append(out, ownershipMatrixRaisingTwins(fix)...)
	out = append(out, ownershipMatrixPolicyTwins(fix)...)
	out = append(out, ownershipMatrixFieldPayloadPrograms(fix)...)
	out = append(out, ownershipMatrixFieldRowPrograms()...)
	out = append(out, ownershipMatrixNestedPrograms()...)
	out = append(out, ownershipMatrixCompositePrograms()...)
	return out
}

// ownershipMatrixPrograms returns every program in manifest order (Python main()): the matrix
// family first, keeping the first program of each name, then the explicit families.
func ownershipMatrixPrograms(fixture string) []ownershipMatrixProgram {
	var out []ownershipMatrixProgram
	names := map[string]bool{}
	for _, p := range ownershipMatrixCases(fixture) {
		name := p.name()
		if names[name] {
			continue
		}
		names[name] = true
		out = append(out, ownershipMatrixProgram{Name: name, Source: p.source(), Oracle: p.oracle()})
	}
	for _, p := range ownershipMatrixExplicitPrograms(fixture) {
		if names[p.Name] {
			panic("duplicate explicit program " + p.Name)
		}
		names[p.Name] = true
		out = append(out, p)
	}
	return out
}
