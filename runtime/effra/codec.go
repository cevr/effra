package effra

import (
	"fmt"
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
// variant index (0 for records) and decoded field values in declared order;
// Project returns them for encoding. Adapters are trusted compiler output, so
// a carrier mismatch is a defect (panic), never a typed codec failure.
type CodecNode struct {
	Kind      CodecKind
	Type      string
	Fields    []CodecField
	Variants  []CodecVariant
	Construct func(variant int, fields []any) any
	Project   func(value any) (variant int, fields []any)
}

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
}

type codecNode struct {
	kind      CodecKind
	fields    []codecField
	variants  []codecVariant
	tags      map[string]int
	construct func(int, []any) any
	project   func(any) (int, []any)
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
			if node.Type != "" || len(node.Fields) > 0 || len(node.Variants) > 0 || node.Construct != nil || node.Project != nil {
				return invalid("primitive node %d carries nominal structure", index)
			}
			if previous, ok := primitives[node.Kind]; ok {
				return invalid("nodes %d and %d repeat primitive %s instead of sharing it", previous, index, node.Kind)
			}
			primitives[node.Kind] = index
		case CodecRecord, CodecUnion:
			if node.Type == "" || node.Construct == nil || node.Project == nil {
				return invalid("nominal node %d needs a type identity and both adapters", index)
			}
			if previous, ok := types[node.Type]; ok {
				return invalid("nodes %d and %d repeat type %q instead of sharing it", previous, index, node.Type)
			}
			types[node.Type] = index
		default:
			return invalid("node %d has unsupported kind %q", index, node.Kind)
		}
		compiled[index] = codecNode{kind: node.Kind, construct: node.Construct, project: node.Project}
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
			key := codecAppendString(nil, field.Name)
			out[i] = codecField{name: field.Name, key: append(key, ':'), node: &compiled[field.Node]}
		}
		return out
	}
	for index, node := range plan.Nodes {
		switch node.Kind {
		case CodecRecord:
			compiled[index].fields = link(node.Fields)
		case CodecUnion:
			compiled[index].variants = make([]codecVariant, len(node.Variants))
			for variant, alternative := range node.Variants {
				open := append(codecAppendString([]byte{'{'}, CodecTagKey), ':')
				compiled[index].variants[variant] = codecVariant{open: codecAppendString(open, alternative.Tag), fields: link(alternative.Fields)}
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
		for _, child := range children {
			if err := visit(child.Node, containers); err != nil {
				return err
			}
			deepest = max(deepest, depth[child.Node])
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
	return &Codec{bounds: plan.Bounds, root: &compiled[plan.Root]}, nil
}

// Decode parses body once into a bounded JSON value, then decodes the plan in
// declared order. The first failure is returned as a *CodecError.
func (c *Codec) Decode(body []byte) (any, error) {
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
	return value, nil
}

// Encode writes compact JSON in declared field order, with each union's
// discriminator first. Output larger than MaxBodyBytes is a typed failure.
func (c *Codec) Encode(value any) ([]byte, error) {
	encoder := codecEncoder{limit: c.bounds.MaxBodyBytes}
	if failure := c.root.encode(&encoder, value, nil); failure != nil {
		return nil, failure
	}
	return encoder.out, nil
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
	case CodecRecord:
		if value.kind == codecJSONObject {
			fields, failure := decodeCodecFields(n.fields, value, path)
			if failure != nil {
				return nil, failure
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

type codecEncoder struct {
	out   []byte
	limit int
}

// grow reports whether the output is still within its bound after a write.
func (e *codecEncoder) grow() *CodecError {
	if len(e.out) > e.limit {
		return codecFailure(CodecEncode, CodecBodyTooLarge, nil)
	}
	return nil
}

func (e *codecEncoder) write(bytes ...byte) *CodecError {
	if len(e.out)+len(bytes) > e.limit {
		return codecFailure(CodecEncode, CodecBodyTooLarge, nil)
	}
	e.out = append(e.out, bytes...)
	return nil
}

func (n *codecNode) encode(e *codecEncoder, value any, path *codecPath) *CodecError {
	switch n.kind {
	case CodecString:
		text, ok := value.(string)
		if !ok {
			panic(fmt.Sprintf("effra codec: string node received %T", value))
		}
		if !utf8.ValidString(text) {
			return codecFailure(CodecEncode, CodecInvalidUnicode, path)
		}
		e.out = codecAppendString(e.out, text)
		return e.grow()
	case CodecBool:
		boolean, ok := value.(bool)
		if !ok {
			panic(fmt.Sprintf("effra codec: bool node received %T", value))
		}
		e.out = strconv.AppendBool(e.out, boolean)
		return e.grow()
	case CodecVoid:
		if _, ok := value.(struct{}); !ok {
			panic(fmt.Sprintf("effra codec: void node received %T", value))
		}
		e.out = append(e.out, "null"...)
		return e.grow()
	case CodecI64:
		number, ok := value.(int64)
		if !ok {
			panic(fmt.Sprintf("effra codec: i64 node received %T", value))
		}
		e.out = append(strconv.AppendInt(append(e.out, '"'), number, 10), '"')
		return e.grow()
	case CodecRecord:
		_, fields := n.project(value)
		if failure := e.write('{'); failure != nil {
			return failure
		}
		if failure := encodeCodecFields(e, n.fields, fields, path, false); failure != nil {
			return failure
		}
		return e.write('}')
	case CodecUnion:
		index, fields := n.project(value)
		if index < 0 || index >= len(n.variants) {
			panic(fmt.Sprintf("effra codec: union adapter returned variant %d of %d", index, len(n.variants)))
		}
		variant := n.variants[index]
		if failure := e.write(variant.open...); failure != nil {
			return failure
		}
		if failure := encodeCodecFields(e, variant.fields, fields, path, true); failure != nil {
			return failure
		}
		return e.write('}')
	}
	panic("effra codec: unvalidated node kind " + string(n.kind))
}

func encodeCodecFields(e *codecEncoder, fields []codecField, values []any, path *codecPath, separated bool) *CodecError {
	if len(values) != len(fields) {
		panic(fmt.Sprintf("effra codec: adapter returned %d fields for %d declared", len(values), len(fields)))
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
		if failure := field.node.encode(e, values[i], &codecPath{parent: path, name: field.name}); failure != nil {
			return failure
		}
	}
	return nil
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
