package ergo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CodecProfileJSON identifies the structural JSON rules shared by this engine
// and the frozen pinned Effect Schema comparator vectors:
// required declared keys, ignored excess keys, void as null, i64 as a signed
// decimal string, "_tag" union discriminators, declared-order first failure
// and output, and bounded admission that rejects duplicate keys, invalid
// Unicode and over-limit bodies or nesting. A stricter profile is a new
// identity, never a silent change to this one.
const CodecProfileJSON = "ergo/json-structural-1"

// CodecTagKey is the union discriminator of CodecProfileJSON.
const CodecTagKey = "_tag"

// CodecMaxDepth bounds every plan's nesting allowance, which in turn bounds
// parser/decoder recursion and the length of failure paths.
const CodecMaxDepth = 512

const maxCodecPlanNodes = 4096
const maxCodecPlanEdges = 65536

type CodecKind string

const (
	CodecString CodecKind = "string"
	CodecBool   CodecKind = "bool"
	CodecVoid   CodecKind = "void"
	CodecI64    CodecKind = "i64"
	CodecRecord CodecKind = "record"
	CodecUnion  CodecKind = "union"
	// CodecTransform applies one checked transformation between a
	// representation (its Child node) and a domain value. It writes no JSON
	// of its own: the wire form is the representation's.
	CodecTransform CodecKind = "transform"
)

// CodecPlan is a bounded DAG over canonical types. Nodes reference children
// by index, so repeated substructure is one shared node rather than a copied
// path. Each primitive kind and each nominal Type appears at most once, every
// node is reachable from Root, and cycles (recursive layouts) are rejected.
type CodecPlan struct {
	Profile string
	Bounds  CodecBounds
	Root    int
	Nodes   []CodecNode
}

// CodecBounds are explicit per-plan limits; the engine supplies no defaults.
// MaxBodyBytes bounds decoded input and encoded output. MaxDepth bounds input
// object/array nesting and must cover the plan's own record/union nesting.
type CodecBounds struct {
	MaxBodyBytes int
	MaxDepth     int
}

// CodecNode describes one canonical type. Primitive carriers are fixed:
// string, bool, struct{}{} for void and int64. Record and union nodes carry a
// nominal Type identity and a compiler-generated Construct adapter, which
// receives a variant index (0 for records) and the decoded field values in
// declared order. Encoding does not read the plan's nodes: compiler-generated
// writers walk the value through a CodecWriter (see Codec.Writer). Adapters
// are trusted compiler output, so a mismatch is a defect (panic), never a
// typed codec failure.
//
// A transform node carries its witness identity as Type, its representation
// node as Child and the index of its transformation among the plan's Steps.
// Each transform node has its own step: indices are dense from 0.
type CodecNode struct {
	Kind      CodecKind
	Type      string
	Fields    []CodecField
	Variants  []CodecVariant
	Construct func(variant int, fields []any) any
	Child     int
	Step      int
}

// CodecStep is one decode transformation: it receives the decoded
// representation and returns the domain value. A non-nil error halts the
// boundary and is returned unchanged, so a step can carry any outcome its
// caller distinguishes (generated code carries the transformation's exact
// cause) and is never confused with a structural *CodecError. The engine
// itself stays independent of effect execution. Encode steps are typed:
// a generated writer calls them directly (see Codec.Writer).
type CodecStep func(value any) (any, error)

// CodecField names a wire key and the node decoding its value. In this
// profile the wire key is the source field name.
type CodecField struct {
	Name string
	Node int
}

// CodecVariant is one closed alternative, selected by its wire Tag.
type CodecVariant struct {
	Tag    string
	Fields []CodecField
}

type CodecDirection string

const (
	CodecDecode CodecDirection = "decode"
	CodecEncode CodecDirection = "encode"
)

type CodecReason string

const (
	CodecBodyTooLarge   CodecReason = "body-too-large"
	CodecSyntax         CodecReason = "syntax"
	CodecInvalidUnicode CodecReason = "invalid-unicode"
	CodecDepth          CodecReason = "depth"
	CodecDuplicateKey   CodecReason = "duplicate-key"
	CodecMissing        CodecReason = "missing"
	CodecType           CodecReason = "type"
	CodecTag            CodecReason = "tag"
	CodecInteger        CodecReason = "integer"
	CodecRange          CodecReason = "range"
)

// CodecError is the first failure of one boundary execution. Path contains
// only plan-declared keys, outermost first; admission failures instead carry
// the input byte Offset (-1 when not applicable). It never contains input
// text, so its size is bounded by the plan.
type CodecError struct {
	Direction CodecDirection
	Reason    CodecReason
	Path      []string
	Offset    int
}

func (e *CodecError) Error() string {
	var out strings.Builder
	out.WriteString("codec ")
	out.WriteString(string(e.Direction))
	out.WriteString(": ")
	out.WriteString(string(e.Reason))
	if e.Offset >= 0 {
		out.WriteString(" at byte ")
		out.WriteString(strconv.Itoa(e.Offset))
	}
	if len(e.Path) > 0 {
		out.WriteString(" at ")
		for _, name := range e.Path {
			out.WriteString("[" + strconv.Quote(name) + "]")
		}
	}
	return out.String()
}

// Codec is a validated immutable plan, safe for concurrent use.
type Codec struct {
	bounds CodecBounds
	root   *codecNode
	steps  int
	// nesting is the plan's record/union nesting: the most declared keys
	// an encode path holds.
	nesting int
}

type codecNode struct {
	kind      CodecKind
	fields    []codecField
	variants  []codecVariant
	tags      map[string]int
	construct func(int, []any) any
	child     *codecNode
	step      int
	// pending marks a node whose subtree contains a transformation. Its
	// structural phase carries codecPending values instead of carriers, and
	// its transformation phase completes them in declared order.
	pending bool
}

// codecPending is the not yet constructed form of a nominal node on a path
// to a decode transformation. It is private, so it can never be mistaken
// for a generated carrier.
type codecPending struct {
	variant int
	fields  []any
}

type codecField struct {
	name string
	node *codecNode
}

type codecVariant struct {
	fields []codecField
}

// CompileCodec validates a plan once and retains a private copy of its
// structure. Validation is linear in nodes and edges, independent of the
// number of paths through shared nodes.
func CompileCodec(plan CodecPlan) (*Codec, error) {
	invalid := func(format string, args ...any) (*Codec, error) {
		return nil, fmt.Errorf("invalid codec plan: "+format, args...)
	}
	if plan.Profile != CodecProfileJSON {
		return invalid("unsupported profile %q", plan.Profile)
	}
	if plan.Bounds.MaxBodyBytes < 1 {
		return invalid("MaxBodyBytes must be positive")
	}
	if plan.Bounds.MaxDepth < 1 || plan.Bounds.MaxDepth > CodecMaxDepth {
		return invalid("MaxDepth must be between 1 and %d", CodecMaxDepth)
	}
	if len(plan.Nodes) == 0 || len(plan.Nodes) > maxCodecPlanNodes {
		return invalid("plan must have between 1 and %d nodes", maxCodecPlanNodes)
	}
	if plan.Root < 0 || plan.Root >= len(plan.Nodes) {
		return invalid("root %d is not a node", plan.Root)
	}
	compiled := make([]codecNode, len(plan.Nodes))
	primitives := map[CodecKind]int{}
	types := map[string]int{}
	steps := map[int]int{}
	edges := 0
	checkFields := func(index int, fields []CodecField, reserved string) error {
		names := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			edges++
			if field.Name == "" || !utf8.ValidString(field.Name) {
				return fmt.Errorf("invalid codec plan: node %d has an empty or invalid field name", index)
			}
			if field.Name == reserved {
				return fmt.Errorf("invalid codec plan: node %d variant field %q collides with the discriminator", index, field.Name)
			}
			if _, ok := names[field.Name]; ok {
				return fmt.Errorf("invalid codec plan: node %d repeats field %q", index, field.Name)
			}
			names[field.Name] = struct{}{}
			if field.Node < 0 || field.Node >= len(plan.Nodes) {
				return fmt.Errorf("invalid codec plan: node %d field %q references missing node %d", index, field.Name, field.Node)
			}
		}
		return nil
	}
	for index, node := range plan.Nodes {
		switch node.Kind {
		case CodecString, CodecBool, CodecVoid, CodecI64:
			if node.Type != "" || len(node.Fields) > 0 || len(node.Variants) > 0 || node.Construct != nil || node.Child != 0 || node.Step != 0 {
				return invalid("primitive node %d carries nominal structure", index)
			}
			if previous, ok := primitives[node.Kind]; ok {
				return invalid("nodes %d and %d repeat primitive %s instead of sharing it", previous, index, node.Kind)
			}
			primitives[node.Kind] = index
		case CodecRecord, CodecUnion:
			if node.Type == "" || node.Construct == nil {
				return invalid("nominal node %d needs a type identity and Construct", index)
			}
			if node.Child != 0 || node.Step != 0 {
				return invalid("nominal node %d carries transform structure", index)
			}
			if previous, ok := types[node.Type]; ok {
				return invalid("nodes %d and %d repeat type %q instead of sharing it", previous, index, node.Type)
			}
			types[node.Type] = index
		case CodecTransform:
			if node.Type == "" || len(node.Fields) > 0 || len(node.Variants) > 0 || node.Construct != nil {
				return invalid("transform node %d needs a witness identity and no structure of its own", index)
			}
			if previous, ok := types[node.Type]; ok {
				return invalid("nodes %d and %d repeat witness %q instead of sharing it", previous, index, node.Type)
			}
			types[node.Type] = index
			edges++
			if node.Child < 0 || node.Child >= len(plan.Nodes) {
				return invalid("transform node %d references missing node %d", index, node.Child)
			}
			if node.Step < 0 || node.Step >= len(plan.Nodes) {
				return invalid("transform node %d has step %d out of range", index, node.Step)
			}
			if previous, ok := steps[node.Step]; ok {
				return invalid("nodes %d and %d share step %d", previous, index, node.Step)
			}
			steps[node.Step] = index
		default:
			return invalid("node %d has unsupported kind %q", index, node.Kind)
		}
		compiled[index] = codecNode{kind: node.Kind, construct: node.Construct, step: node.Step}
		switch node.Kind {
		case CodecRecord:
			if len(node.Variants) > 0 {
				return invalid("record node %d has variants", index)
			}
			if err := checkFields(index, node.Fields, ""); err != nil {
				return nil, err
			}
		case CodecUnion:
			if len(node.Fields) > 0 || len(node.Variants) == 0 {
				return invalid("union node %d needs variants and no record fields", index)
			}
			compiled[index].tags = make(map[string]int, len(node.Variants))
			for variant, alternative := range node.Variants {
				edges++
				if alternative.Tag == "" || !utf8.ValidString(alternative.Tag) {
					return invalid("union node %d has an empty or invalid tag", index)
				}
				if _, ok := compiled[index].tags[alternative.Tag]; ok {
					return invalid("union node %d repeats tag %q", index, alternative.Tag)
				}
				compiled[index].tags[alternative.Tag] = variant
				if err := checkFields(index, alternative.Fields, CodecTagKey); err != nil {
					return nil, err
				}
			}
		}
		if edges > maxCodecPlanEdges {
			return invalid("more than %d fields and variants", maxCodecPlanEdges)
		}
	}
	link := func(fields []CodecField) []codecField {
		out := make([]codecField, len(fields))
		for i, field := range fields {
			out[i] = codecField{name: field.Name, node: &compiled[field.Node]}
		}
		return out
	}
	for step := range steps {
		if step >= len(steps) {
			return invalid("steps are not dense: %d of %d", step, len(steps))
		}
	}
	for index, node := range plan.Nodes {
		switch node.Kind {
		case CodecTransform:
			compiled[index].child = &compiled[node.Child]
		case CodecRecord:
			compiled[index].fields = link(node.Fields)
		case CodecUnion:
			compiled[index].variants = make([]codecVariant, len(node.Variants))
			for variant, alternative := range node.Variants {
				compiled[index].variants[variant] = codecVariant{fields: link(alternative.Fields)}
			}
		}
	}
	// Depth-first validation with memoized depths: each node and edge is
	// visited once. The active path is at most MaxDepth containers deep.
	const (
		unvisited = iota
		active
		done
	)
	state := make([]uint8, len(plan.Nodes))
	depth := make([]int, len(plan.Nodes))
	var visit func(index, containers int) error
	visit = func(index, containers int) error {
		switch state[index] {
		case done:
			return nil
		case active:
			return fmt.Errorf("invalid codec plan: node %d is part of a recursive layout", index)
		}
		node := plan.Nodes[index]
		if node.Kind == CodecRecord || node.Kind == CodecUnion {
			containers++
			if containers > plan.Bounds.MaxDepth {
				return fmt.Errorf("invalid codec plan: plan nesting exceeds MaxDepth %d", plan.Bounds.MaxDepth)
			}
		}
		state[index] = active
		deepest := 0
		children := append([]CodecField{}, node.Fields...)
		for _, variant := range node.Variants {
			children = append(children, variant.Fields...)
		}
		if node.Kind == CodecTransform {
			children = append(children, CodecField{Node: node.Child})
			compiled[index].pending = true
		}
		for _, child := range children {
			if err := visit(child.Node, containers); err != nil {
				return err
			}
			deepest = max(deepest, depth[child.Node])
			compiled[index].pending = compiled[index].pending || compiled[child.Node].pending
		}
		if node.Kind == CodecRecord || node.Kind == CodecUnion {
			deepest++
		}
		depth[index] = deepest
		state[index] = done
		return nil
	}
	if err := visit(plan.Root, 0); err != nil {
		return nil, err
	}
	if depth[plan.Root] > plan.Bounds.MaxDepth {
		return invalid("plan nesting exceeds MaxDepth %d", plan.Bounds.MaxDepth)
	}
	for index := range plan.Nodes {
		if state[index] != done {
			return invalid("node %d is unreachable from root", index)
		}
	}
	return &Codec{bounds: plan.Bounds, root: &compiled[plan.Root], steps: len(steps), nesting: depth[plan.Root]}, nil
}

// Steps is the number of transformations a direction must supply.
func (c *Codec) Steps() int { return c.steps }

func (c *Codec) checkSteps(steps []CodecStep) {
	if len(steps) != c.steps {
		panic(fmt.Sprintf("ergo codec: plan has %d transformations; got %d steps", c.steps, len(steps)))
	}
}

// Decode decodes a plan without transformations; see DecodeSteps.
func (c *Codec) Decode(body []byte) (any, error) { return c.DecodeSteps(body, nil) }

// DecodeSteps parses body once into a bounded JSON value, decodes the plan's
// structure in declared order, and only then runs its decode transformations
// in declared depth-first order. Malformed input therefore never reaches a
// transformation. The first structural failure is returned as a *CodecError;
// a step's error is returned unchanged. steps must hold one decode step per
// transformation, indexed by transform node Step.
func (c *Codec) DecodeSteps(body []byte, steps []CodecStep) (any, error) {
	c.checkSteps(steps)
	if len(body) > c.bounds.MaxBodyBytes {
		return nil, &CodecError{Direction: CodecDecode, Reason: CodecBodyTooLarge, Path: []string{}, Offset: -1}
	}
	parser := codecParser{data: body, maxDepth: c.bounds.MaxDepth}
	document, failure := parser.document()
	if failure != nil {
		return nil, failure
	}
	value, failure := c.root.decode(&document, nil)
	if failure != nil {
		return nil, failure
	}
	if c.root.pending {
		return c.root.resolve(value, steps)
	}
	return value, nil
}

// resolve runs the decode transformations under a pending node in declared
// order and constructs the carriers that waited for them.
func (n *codecNode) resolve(value any, steps []CodecStep) (any, error) {
	if n.kind == CodecTransform {
		if n.child.pending {
			inner, err := n.child.resolve(value, steps)
			if err != nil {
				return nil, err
			}
			value = inner
		}
		return steps[n.step](value)
	}
	pending := value.(codecPending)
	fields := n.fields
	if n.kind == CodecUnion {
		fields = n.variants[pending.variant].fields
	}
	for i, field := range fields {
		if field.node.pending {
			resolved, err := field.node.resolve(pending.fields[i], steps)
			if err != nil {
				return nil, err
			}
			pending.fields[i] = resolved
		}
	}
	return n.construct(pending.variant, pending.fields), nil
}

// Writer starts one encode pass of the plan for its compiler-generated
// writer.
//
// A generated writer is a family of functions, one per plan node, that call
// one another directly: a record's writer passes a pointer to each field to
// the field's writer, so the value is read in place and no nested record is
// copied; a union's writer binds the variant it holds once, in a type
// switch, as any Go match over the generated enum does; and a transform
// node's writer calls its typed encode step with the domain value, by
// value, and writes the representation the step returns. It writes compact
// JSON in declared field order, with each union's discriminator first,
// spelling keys and tags at compile time with CodecSpelling.
//
// Output larger than MaxBodyBytes is a typed failure, detected before any
// byte beyond the bound is constructed, and nothing else is retained:
// encoder memory is O(MaxBodyBytes) plus the plan's nesting, whatever the
// value's size or sharing. The first structural failure is deferred rather
// than returned: from then on nothing is written, a writer whose subtree
// has no transformation returns at once (Live), and the writers of the
// remaining transformations still run them in declared depth-first
// (pre)order. So every transformation runs before any structural failure
// is reported, and the error of a step that halts the pass, returned
// unchanged by Finish, takes precedence over every structural failure. Structural failures surface in
// encode order; a string's Unicode is validated before any of its bytes are
// written, so ill-formed text reports invalid-unicode even when its encoding
// would also exceed the remaining allowance.
func (c *Codec) Writer() *CodecWriter {
	return &CodecWriter{encoder: codecEncoder{limit: c.bounds.MaxBodyBytes}, path: make([]string, 0, c.nesting)}
}

// CodecWriter is one encode pass. Its path holds the declared keys of the
// field being written, outermost first; it is copied only to report a
// structural failure.
type CodecWriter struct {
	encoder codecEncoder
	path    []string
}

// Live reports that no structural failure is deferred. A writer whose
// subtree runs no transformation does nothing once the pass has failed.
func (w *CodecWriter) Live() bool { return w.encoder.failure == nil }

// Raw writes compiler-spelled plan text: braces, separators, keys and tags.
func (w *CodecWriter) Raw(text string) {
	if w.encoder.failure == nil {
		w.encoder.failure = w.encoder.writeText(text)
	}
}

// Enter and Leave bracket the value of the declared field name.
func (w *CodecWriter) Enter(name string) { w.path = append(w.path, name) }
func (w *CodecWriter) Leave()            { w.path = w.path[:len(w.path)-1] }

// String writes a JSON string, refusing ill-formed Unicode at the current
// path before writing any of its bytes.
func (w *CodecWriter) String(text string) {
	if w.encoder.failure != nil {
		return
	}
	if !utf8.ValidString(text) {
		w.encoder.failure = &CodecError{Direction: CodecEncode, Reason: CodecInvalidUnicode, Path: append([]string{}, w.path...), Offset: -1}
		return
	}
	w.encoder.failure = w.encoder.writeString(text)
}

func (w *CodecWriter) Bool(value bool) {
	if value {
		w.Raw("true")
	} else {
		w.Raw("false")
	}
}

// I64 writes a signed decimal string.
func (w *CodecWriter) I64(value int64) {
	if w.encoder.failure == nil {
		var scratch [22]byte
		w.encoder.failure = w.encoder.write(append(strconv.AppendInt(append(scratch[:0], '"'), value, 10), '"')...)
	}
}

func (w *CodecWriter) Void() { w.Raw("null") }

// Finish ends the pass: the error of the step that halted it, else the
// deferred structural failure, else the output.
func (w *CodecWriter) Finish(halted error) ([]byte, error) {
	if halted != nil {
		return nil, halted
	}
	if w.encoder.failure != nil {
		return nil, w.encoder.failure
	}
	return w.encoder.out, nil
}

// codecPath is a parent-linked failure path; it is materialized only when a
// failure is reported.
type codecPath struct {
	parent *codecPath
	name   string
}

func codecFailure(direction CodecDirection, reason CodecReason, path *codecPath) *CodecError {
	count := 0
	for at := path; at != nil; at = at.parent {
		count++
	}
	names := make([]string, count)
	for at := path; at != nil; at = at.parent {
		count--
		names[count] = at.name
	}
	return &CodecError{Direction: direction, Reason: reason, Path: names, Offset: -1}
}

func (n *codecNode) decode(value *codecJSON, path *codecPath) (any, *CodecError) {
	switch n.kind {
	case CodecString:
		if value.kind == codecJSONString {
			return value.text, nil
		}
	case CodecBool:
		if value.kind == codecJSONBool {
			return value.boolean, nil
		}
	case CodecVoid:
		if value.kind == codecJSONNull {
			return struct{}{}, nil
		}
	case CodecI64:
		if value.kind == codecJSONString {
			number, reason := codecParseI64(value.text)
			if reason != "" {
				return nil, codecFailure(CodecDecode, reason, path)
			}
			return number, nil
		}
	case CodecTransform:
		return n.child.decode(value, path)
	case CodecRecord:
		if value.kind == codecJSONObject {
			fields, failure := decodeCodecFields(n.fields, value, path)
			if failure != nil {
				return nil, failure
			}
			if n.pending {
				return codecPending{fields: fields}, nil
			}
			return n.construct(0, fields), nil
		}
	case CodecUnion:
		if value.kind == codecJSONObject {
			tag := value.member(CodecTagKey)
			if tag == nil || tag.kind != codecJSONString {
				return nil, codecFailure(CodecDecode, CodecTag, path)
			}
			variant, ok := n.tags[tag.text]
			if !ok {
				return nil, codecFailure(CodecDecode, CodecTag, path)
			}
			fields, failure := decodeCodecFields(n.variants[variant].fields, value, path)
			if failure != nil {
				return nil, failure
			}
			if n.pending {
				return codecPending{variant: variant, fields: fields}, nil
			}
			return n.construct(variant, fields), nil
		}
	}
	return nil, codecFailure(CodecDecode, CodecType, path)
}

func decodeCodecFields(fields []codecField, object *codecJSON, path *codecPath) ([]any, *CodecError) {
	values := make([]any, len(fields))
	for i, field := range fields {
		at := &codecPath{parent: path, name: field.name}
		member := object.member(field.name)
		if member == nil {
			return nil, codecFailure(CodecDecode, CodecMissing, at)
		}
		value, failure := field.node.decode(member, at)
		if failure != nil {
			return nil, failure
		}
		values[i] = value
	}
	return values, nil
}

// codecEncoder accumulates output; every write checks the remaining
// allowance before appending, so out never grows past limit.
type codecEncoder struct {
	out   []byte
	limit int
	// failure is the pass's first structural failure; once it is set,
	// nothing more is written.
	failure *CodecError
}

// write and writeText build the body-too-large failure (the root path,
// empty) as a literal rather than through codecFailure: a call would put
// them, and so CodecWriter.Raw, over Go's inlining budget, and every key,
// brace and separator a generated writer spells goes through Raw.
func (e *codecEncoder) write(bytes ...byte) *CodecError {
	if len(e.out)+len(bytes) > e.limit {
		return &CodecError{Direction: CodecEncode, Reason: CodecBodyTooLarge, Path: []string{}, Offset: -1}
	}
	e.out = append(e.out, bytes...)
	return nil
}

func (e *codecEncoder) writeText(text string) *CodecError {
	if len(e.out)+len(text) > e.limit {
		return &CodecError{Direction: CodecEncode, Reason: CodecBodyTooLarge, Path: []string{}, Offset: -1}
	}
	e.out = append(e.out, text...)
	return nil
}

// CodecSpelling renders compiler-owned plan text (a key or tag) as the JSON
// string CodecProfileJSON writes for it, with JSON.stringify escaping. A
// compiler spells a generated writer's keys and tags with it. Plan text is
// not body output, so it is not bounded by MaxBodyBytes.
func CodecSpelling(text string) string {
	e := codecEncoder{limit: math.MaxInt}
	_ = e.writeString(text)
	return string(e.out)
}

// codecParseI64 accepts an optional '-' and ASCII digits. Leading zeros and
// negative zero normalize; values outside the signed 64-bit range fail.
func codecParseI64(text string) (int64, CodecReason) {
	digits := strings.TrimPrefix(text, "-")
	if digits == "" {
		return 0, CodecInteger
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, CodecInteger
		}
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return 0, ""
	}
	if len(digits) > 19 {
		return 0, CodecRange
	}
	if len(text) > 0 && text[0] == '-' {
		digits = "-" + digits
	}
	number, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, CodecRange
	}
	return number, ""
}
