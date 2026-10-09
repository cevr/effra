package effra

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

// CodecProfileJSON identifies the structural JSON rules shared by this engine,
// the JavaScript engine and the pinned Effect Schema comparator vectors:
// required declared keys, ignored excess keys, void as null, i64 as a signed
// decimal string, "_tag" union discriminators, declared-order first failure
// and output, and bounded admission that rejects duplicate keys, invalid
// Unicode and over-limit bodies or nesting. A stricter profile is a new
// identity, never a silent change to this one.
const CodecProfileJSON = "effra/json-structural-1"

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
// nominal Type identity and compiler-generated adapters: Construct receives a
// variant index (0 for records) and decoded field values in declared order.
// A union node's Variant names the variant its carrier holds. For encoding,
// a record carrier, and the value a union carrier holds, is a struct whose
// fields are the declared fields in declared order; the engine reads those
// fields in place, so encoding never copies a nested record. A carrier
// without declared fields is not read (a fieldless failure's is nil). Adapters and carriers are trusted compiler output,
// so a mismatch is a defect (panic), never a typed codec failure.
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
	Variant   func(value any) int
	Child     int
	Step      int
}

// CodecStep is one transformation of the direction being executed. A decode
// step receives the decoded representation and returns the domain value; an
// encode step receives the domain value and returns its representation. A
// non-nil error halts the boundary and is returned unchanged, so a step can
// carry any outcome its caller distinguishes (generated code carries the
// transformation's exact cause) and is never confused with a structural
// *CodecError. The engine itself stays independent of effect execution.
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
}

type codecNode struct {
	kind      CodecKind
	fields    []codecField
	variants  []codecVariant
	tags      map[string]int
	construct func(int, []any) any
	variant   func(any) int
	// open is a record's opening brace; a union's opening bytes are its
	// variants'.
	open  []byte
	child *codecNode
	step  int
	// pending marks a node whose subtree contains a transformation. Its
	// structural phase carries codecPending values instead of carriers, and
	// its transformation phase completes them in declared order.
	pending bool
}

// codecPending is the not yet constructed (decode) or already projected
// (encode) form of a nominal node on a path to a transformation. It is
// private, so it can never be mistaken for a generated carrier.
type codecPending struct {
	variant int
	fields  []any
}

type codecField struct {
	name string
	key  []byte
	node *codecNode
}

type codecVariant struct {
	open   []byte
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
			if node.Type != "" || len(node.Fields) > 0 || len(node.Variants) > 0 || node.Construct != nil || node.Variant != nil || node.Child != 0 || node.Step != 0 {
				return invalid("primitive node %d carries nominal structure", index)
			}
			if previous, ok := primitives[node.Kind]; ok {
				return invalid("nodes %d and %d repeat primitive %s instead of sharing it", previous, index, node.Kind)
			}
			primitives[node.Kind] = index
		case CodecRecord, CodecUnion:
			if node.Type == "" || node.Construct == nil || (node.Variant != nil) != (node.Kind == CodecUnion) {
				return invalid("nominal node %d needs a type identity, Construct and, for a union only, Variant", index)
			}
			if node.Child != 0 || node.Step != 0 {
				return invalid("nominal node %d carries transform structure", index)
			}
			if previous, ok := types[node.Type]; ok {
				return invalid("nodes %d and %d repeat type %q instead of sharing it", previous, index, node.Type)
			}
			types[node.Type] = index
		case CodecTransform:
			if node.Type == "" || len(node.Fields) > 0 || len(node.Variants) > 0 || node.Construct != nil || node.Variant != nil {
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
		compiled[index] = codecNode{kind: node.Kind, construct: node.Construct, variant: node.Variant, step: node.Step}
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
			out[i] = codecField{name: field.Name, key: codecSpell("", field.Name, ":"), node: &compiled[field.Node]}
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
			compiled[index].fields, compiled[index].open = link(node.Fields), []byte{'{'}
		case CodecUnion:
			compiled[index].variants = make([]codecVariant, len(node.Variants))
			for variant, alternative := range node.Variants {
				open := codecSpell(string(codecSpell("{", CodecTagKey, ":")), alternative.Tag, "")
				compiled[index].variants[variant] = codecVariant{open: open, fields: link(alternative.Fields)}
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
	return &Codec{bounds: plan.Bounds, root: &compiled[plan.Root], steps: len(steps)}, nil
}

// Steps is the number of transformations a direction must supply.
func (c *Codec) Steps() int { return c.steps }

func (c *Codec) checkSteps(steps []CodecStep) {
	if len(steps) != c.steps {
		panic(fmt.Sprintf("effra codec: plan has %d transformations; got %d steps", c.steps, len(steps)))
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

// Encode encodes a plan without transformations; see EncodeSteps.
func (c *Codec) Encode(value any) ([]byte, error) { return c.EncodeSteps(value, nil) }

// EncodeSteps writes compact JSON in declared field order, with each
// union's discriminator first, running each encode transformation when the
// writer reaches it. Output larger than MaxBodyBytes is a typed failure,
// detected before any byte beyond the bound is constructed, and nothing
// else is retained: encoder memory is O(MaxBodyBytes) plus the plan's
// nesting, whatever the value's size or sharing. The value is read in
// place: a nested record is never copied, and only a transformation's
// domain value is handed to its step. Transformations run in
// declared depth-first (pre)order, and every one runs before any
// structural failure is reported, so a step's error, returned unchanged,
// takes precedence over every structural failure. Structural failures
// surface in encode order; a string's Unicode is validated before any of its
// bytes are written, so ill-formed text reports invalid-unicode even when
// its encoding would also exceed the remaining allowance.
func (c *Codec) EncodeSteps(value any, steps []CodecStep) ([]byte, error) {
	c.checkSteps(steps)
	encoder := codecEncoder{limit: c.bounds.MaxBodyBytes}
	// ValueOf of an interface value is not addressable, so every field,
	// element and held value derived from it aliases the caller's memory.
	carrier := reflect.ValueOf(value)
	if !c.root.pending {
		if failure := c.root.encode(&encoder, carrier, nil); failure != nil {
			return nil, failure
		}
		return encoder.out, nil
	}
	if err := c.root.stream(&encoder, carrier, nil, steps); err != nil {
		return nil, err
	}
	if encoder.failure != nil {
		return nil, encoder.failure
	}
	return encoder.out, nil
}

// stream encodes a pending node in one encode-order pass: a transformation
// runs when the writer reaches it and its result is written at once, so no
// prepared tree is ever built. The first structural failure is deferred on
// the encoder rather than returned: from then on nothing is written or
// retained, but the pass continues through pending nodes only, running the
// remaining transformations, so the observable steps and the precedence of
// a step's error are those of running every transformation first.
func (n *codecNode) stream(e *codecEncoder, value reflect.Value, path *codecPath, steps []CodecStep) error {
	if !n.pending {
		if e.failure == nil {
			e.failure = n.encode(e, value, path)
		}
		return nil
	}
	if n.kind == CodecTransform {
		var domain any
		if value = codecHeld(value); value.IsValid() {
			// Interface on a non-addressable value shares its memory.
			domain = value.Interface()
		}
		represented, err := steps[n.step](domain)
		if err != nil {
			return err
		}
		return n.child.stream(e, reflect.ValueOf(represented), path, steps)
	}
	fields, open, separated, carrier := n.carrier(value)
	e.emit(open...)
	for i, field := range fields {
		if separated {
			e.emit(',')
		}
		separated = true
		e.emit(field.key...)
		at := path
		if e.failure == nil {
			// A path exists only to report a structural failure, so a
			// pass that already failed allocates none.
			at = &codecPath{parent: path, name: field.name}
		}
		if err := field.node.stream(e, carrier.Field(i), at, steps); err != nil {
			return err
		}
	}
	e.emit('}')
	return nil
}

// codecHeld unwraps an interface-typed field to the value it holds, without
// copying it.
func codecHeld(value reflect.Value) reflect.Value {
	if value.Kind() == reflect.Interface {
		return value.Elem()
	}
	return value
}

// codecCarrierType names a carrier's Go type for a defect message.
func codecCarrierType(value reflect.Value) string {
	if !value.IsValid() {
		return "nil"
	}
	return value.Type().String()
}

// carrier resolves a nominal node's carrier in place: the declared fields of
// the record or of the variant it holds, the bytes that open its object, and
// the struct whose fields are those fields in declared order. A carrier
// without declared fields is not read, so a fieldless failure's carrier may
// be nil.
func (n *codecNode) carrier(value reflect.Value) (fields []codecField, open []byte, separated bool, carrier reflect.Value) {
	carrier = codecHeld(value)
	fields, open = n.fields, n.open
	if n.kind == CodecUnion {
		var held any
		if carrier.IsValid() {
			held = carrier.Interface()
		}
		index := n.variant(held)
		if index < 0 || index >= len(n.variants) {
			panic(fmt.Sprintf("effra codec: union adapter returned variant %d of %d", index, len(n.variants)))
		}
		fields, open, separated = n.variants[index].fields, n.variants[index].open, true
	}
	if len(fields) == 0 {
		// Nothing is read from a carrier without declared fields.
		return fields, open, separated, carrier
	}
	if !carrier.IsValid() || carrier.Kind() != reflect.Struct || carrier.NumField() != len(fields) {
		panic(fmt.Sprintf("effra codec: %s carrier %s does not hold its %d declared fields", n.kind, codecCarrierType(carrier), len(fields)))
	}
	return fields, open, separated, carrier
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
	// failure is the first structural failure of a pass that runs
	// transformations; once set, emit writes nothing.
	failure *CodecError
}

// emit writes bytes unless a structural failure is already deferred, and
// defers the failure its write reports.
func (e *codecEncoder) emit(bytes ...byte) {
	if e.failure == nil {
		e.failure = e.write(bytes...)
	}
}

func (e *codecEncoder) write(bytes ...byte) *CodecError {
	if len(e.out)+len(bytes) > e.limit {
		return codecFailure(CodecEncode, CodecBodyTooLarge, nil)
	}
	e.out = append(e.out, bytes...)
	return nil
}

// codecSpell renders compiler-owned plan text (a key or tag) as a JSON
// string between prefix and suffix. Plan text is not body output, so it is
// not bounded by MaxBodyBytes.
func codecSpell(prefix, text, suffix string) []byte {
	e := codecEncoder{out: []byte(prefix), limit: math.MaxInt}
	_ = e.writeString(text)
	return append(e.out, suffix...)
}

func (n *codecNode) encode(e *codecEncoder, value reflect.Value, path *codecPath) *CodecError {
	value = codecHeld(value)
	leaf := func(want reflect.Type) {
		if !value.IsValid() || value.Type() != want {
			panic(fmt.Sprintf("effra codec: %s node received %s", n.kind, codecCarrierType(value)))
		}
	}
	switch n.kind {
	case CodecString:
		leaf(reflect.TypeFor[string]())
		text := value.String()
		if !utf8.ValidString(text) {
			return codecFailure(CodecEncode, CodecInvalidUnicode, path)
		}
		return e.writeString(text)
	case CodecBool:
		leaf(reflect.TypeFor[bool]())
		var scratch [5]byte
		return e.write(strconv.AppendBool(scratch[:0], value.Bool())...)
	case CodecVoid:
		leaf(reflect.TypeFor[struct{}]())
		return e.write('n', 'u', 'l', 'l')
	case CodecI64:
		leaf(reflect.TypeFor[int64]())
		var scratch [22]byte
		quoted := append(strconv.AppendInt(append(scratch[:0], '"'), value.Int(), 10), '"')
		return e.write(quoted...)
	case CodecRecord, CodecUnion:
		fields, open, separated, carrier := n.carrier(value)
		if failure := e.write(open...); failure != nil {
			return failure
		}
		for i, field := range fields {
			if separated {
				if failure := e.write(','); failure != nil {
					return failure
				}
			}
			separated = true
			if failure := e.write(field.key...); failure != nil {
				return failure
			}
			if failure := field.node.encode(e, carrier.Field(i), &codecPath{parent: path, name: field.name}); failure != nil {
				return failure
			}
		}
		return e.write('}')
	}
	panic("effra codec: unvalidated node kind " + string(n.kind))
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
