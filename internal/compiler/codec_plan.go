package compiler

import (
	"slices"
	"strconv"
	"strings"

	rt "effra.local/prototype/runtime/effra"
)

// The structural ceilings every derived plan must respect. They equal the
// runtime engines' validation limits, so an admitted plan is never refused
// by either engine.
const (
	maxCodecPlanNodes = 4096
	maxCodecPlanEdges = 65536
)

// CodecPlan is the checked structural plan of one domain type under one
// profile and bounds. It is a DAG: each primitive kind and each nominal type
// is one node, children are referenced by index, and nodes are listed in
// depth-first discovery order from the root, fields in declared order. Its
// node IDs are stable canonical identities, so a graph view can project the
// plan without re-deriving it. Both target literals are emitted from it.
type CodecPlan struct {
	ID      string          `json:"id"`
	Profile string          `json:"profile"`
	Bounds  CodecPlanBounds `json:"bounds"`
	Root    int             `json:"root"`
	Nesting int             `json:"nesting"`
	Nodes   []CodecPlanNode `json:"nodes"`
	// index is the plan's position in Result.CodecPlans; emitted plan names
	// derive from it, so they follow checked derivation order.
	index int
	// transforms lists the witness of each transform node by step, and
	// paths the first plan path at which each step applies.
	transforms []*CodecDeclaration
	paths      []string
}

// CodecPlanBounds are the explicit limits a plan carries to both engines,
// which supply no defaults. MaxBodyBytes bounds decoded input and encoded
// output; MaxDepth bounds input nesting and covers the plan's own nesting.
type CodecPlanBounds struct {
	MaxBodyBytes int `json:"maxBodyBytes"`
	MaxDepth     int `json:"maxDepth"`
}

// A transform node's ID is its witness's declaration identity. It names
// the witness, its representation node (Child), its step index and the
// function identity of each direction's transformation.
type CodecPlanNode struct {
	ID       string             `json:"id"`
	Kind     rt.CodecKind       `json:"kind"`
	Fields   []CodecPlanField   `json:"fields,omitempty"`
	Variants []CodecPlanVariant `json:"variants,omitempty"`
	Witness  string             `json:"witness,omitempty"`
	Child    *int               `json:"child,omitempty"`
	Step     *int               `json:"step,omitempty"`
	Decode   string             `json:"decode,omitempty"`
	Encode   string             `json:"encode,omitempty"`
	typeID   TypeID
	data     *DataDeclaration
	witness  *CodecDeclaration
}

type CodecPlanField struct {
	Name string `json:"name"`
	Node int    `json:"node"`
	// typeID is the field's canonical type, from which emitters render the
	// field's carrier in its declared layout.
	typeID TypeID
}

// CodecPlanVariant is selected on the wire by Tag, the variant's source name.
type CodecPlanVariant struct {
	Tag    string           `json:"tag"`
	Fields []CodecPlanField `json:"fields,omitempty"`
}

// codecPlanPath is a parent-linked field path of the derivation. It is
// rendered only for a refusal, so the derivation never copies a path.
type codecPlanPath struct {
	parent *codecPlanPath
	name   string
}

func (p *codecPlanPath) child(name string) *codecPlanPath { return &codecPlanPath{p, name} }

// maxCodecRefusalPathSegments bounds the path a refusal renders: a path
// longer than this keeps its root and its innermost segments.
const maxCodecRefusalPathSegments = 16

func (p *codecPlanPath) String() string {
	segments := []string{}
	for at := p; at != nil; at = at.parent {
		segments = append(segments, at.name)
	}
	slices.Reverse(segments)
	if len(segments) > maxCodecRefusalPathSegments {
		elided := len(segments) - maxCodecRefusalPathSegments
		segments = append([]string{segments[0], "(" + strconv.Itoa(elided) + " fields)"}, segments[len(segments)-maxCodecRefusalPathSegments+1:]...)
	}
	return strings.Join(segments, ".")
}

// deriveCodecPlan derives the plan of a declaration. Each canonical type is
// one node per governing `with` scope, and each transformed witness is one
// transform node whose representation is derived under that witness's own
// scope, so the cost is linear in distinct (type, scope) pairs and fields
// however many paths share them. A refusal names the field path at which the
// profile cannot represent the type, or an unused `with` entry; no partial
// plan is returned.
func (c *checker) deriveCodecPlan(codec *CodecDeclaration, bounds CodecPlanBounds) (*CodecPlan, string) {
	profile := codec.derivation.Profile
	plan := &CodecPlan{Profile: profile, Bounds: bounds}
	indices := map[string]int{}
	active := map[string]bool{}
	depths := []int{}
	used := map[*CodecDeclaration]bool{}
	// Fields, variants and transform children are all edges under one
	// ceiling, as the runtime engines count them.
	edges := 0
	edge := func(path *codecPlanPath) string {
		edges++
		if edges > maxCodecPlanEdges {
			return "plan exceeds " + strconv.Itoa(maxCodecPlanEdges) + " fields and variants at " + path.String()
		}
		return ""
	}
	room := func(path *codecPlanPath) string {
		if len(plan.Nodes) >= maxCodecPlanNodes {
			return "plan exceeds " + strconv.Itoa(maxCodecPlanNodes) + " nodes at " + path.String()
		}
		return ""
	}
	root := &codecPlanPath{name: codec.Type}
	var visit func(id TypeID, scope *CodecDeclaration, path *codecPlanPath, display string) (int, string)
	// transform derives witness's transform node: its representation under
	// the witness's own scope, at the same path.
	transform := func(witness *CodecDeclaration, path *codecPlanPath) (int, string) {
		key := "witness:" + witness.Name
		if active[key] {
			return 0, "recursive layout through codec " + witness.Name + " at " + path.String()
		}
		if index, done := indices[key]; done {
			return index, ""
		}
		if refusal := room(path); refusal != "" {
			return 0, refusal
		}
		if refusal := edge(path); refusal != "" {
			return 0, refusal
		}
		index, step := len(plan.Nodes), len(plan.transforms)
		indices[key] = index
		plan.Nodes = append(plan.Nodes, CodecPlanNode{ID: c.declarationIdentity("codec", "module", witness.Name), Kind: rt.CodecTransform, Witness: witness.Name, Step: &step, Decode: witness.transform.decode.Identity, Encode: witness.transform.encode.Identity, typeID: witness.domainID, witness: witness})
		depths = append(depths, 0)
		plan.transforms = append(plan.transforms, witness)
		plan.paths = append(plan.paths, path.String())
		active[key] = true
		child, refusal := visit(witness.transform.repID, witness, path, witness.transform.repDisplay)
		if refusal != "" {
			return 0, refusal
		}
		active[key] = false
		plan.Nodes[index].Child = &child
		depths[index] = depths[child]
		return index, ""
	}
	// display is the source spelling of the type at path, which names
	// callable and generic types readably in a refusal.
	visit = func(id TypeID, scope *CodecDeclaration, path *codecPlanPath, display string) (int, string) {
		if witness := scope.with[id]; witness != nil {
			used[witness] = true
			return transform(witness, path)
		}
		node := c.node(id)
		if node == nil {
			return 0, "unresolved type at " + path.String()
		}
		key := strconv.Itoa(int(id))
		if node.Kind == "record" || node.Kind == "enum" {
			key += "@" + scope.withKey
		}
		if active[key] {
			return 0, "recursive layout through " + display + " at " + path.String()
		}
		if index, done := indices[key]; done {
			return index, ""
		}
		unsupported := func(reason string) (int, string) {
			return 0, display + " at " + path.String() + " " + reason
		}
		var data *DataDeclaration
		kind := rt.CodecKind("")
		switch node.Kind {
		case "primitive":
			switch node.Name {
			case "string":
				kind = rt.CodecString
			case "bool":
				kind = rt.CodecBool
			case "i64":
				kind = rt.CodecI64
			case voidTypeName:
				kind = rt.CodecVoid
			default:
				return unsupported("is not representable in this profile")
			}
		case "record":
			data, kind = c.records[node.Name], rt.CodecRecord
		case "enum":
			data, kind = c.enums[node.Name], rt.CodecUnion
		case "application":
			return unsupported("is generic data; generic applications are not in this profile")
		case "callable", "callable-shape", "recipe", "providerRecipe":
			return unsupported("is a function or effect recipe and cannot cross a codec boundary")
		case "error":
			// Only a failure witness's root is a failure; a failure is never
			// a field of codec data.
			if !codec.failure || path != root {
				return unsupported("is a failure declaration, not codec data")
			}
			data, kind = c.failureData(node.Name), rt.CodecUnion
		default:
			return unsupported("is a host or runtime type and cannot cross a codec boundary")
		}
		if (kind == rt.CodecRecord || kind == rt.CodecUnion) && (data == nil || len(data.Parameters) > 0) {
			return unsupported("has no first-order declaration")
		}
		if kind == rt.CodecUnion && len(data.Variants) == 0 {
			return unsupported("has no variants to decode")
		}
		if refusal := room(path); refusal != "" {
			return 0, refusal
		}
		index := len(plan.Nodes)
		indices[key] = index
		identity := "primitive:" + node.Name
		if data != nil {
			identity = c.declarationIdentity(node.Kind, "module", node.Name)
			if scope.withKey != "" {
				identity += "@with:" + scope.withKey
			}
		}
		plan.Nodes = append(plan.Nodes, CodecPlanNode{ID: identity, Kind: kind, typeID: id, data: data})
		depths = append(depths, 0)
		if data == nil {
			return index, ""
		}
		active[key] = true
		deepest := 0
		fields := func(declared []Field, owner *codecPlanPath) ([]CodecPlanField, string) {
			out := make([]CodecPlanField, 0, len(declared))
			for _, field := range declared {
				if refusal := edge(owner); refusal != "" {
					return nil, refusal
				}
				if kind == rt.CodecUnion && field.Name == rt.CodecTagKey {
					return nil, "variant field " + owner.child(field.Name).String() + " collides with the " + rt.CodecTagKey + " discriminator"
				}
				child, refusal := visit(field.typeID, scope, owner.child(field.Name), field.Type)
				if refusal != "" {
					return nil, refusal
				}
				deepest = max(deepest, depths[child])
				out = append(out, CodecPlanField{Name: field.Name, Node: child, typeID: field.typeID})
			}
			return out, ""
		}
		if kind == rt.CodecRecord {
			declared, refusal := fields(data.Fields, path)
			if refusal != "" {
				return 0, refusal
			}
			plan.Nodes[index].Fields = declared
		} else {
			variants := make([]CodecPlanVariant, 0, len(data.Variants))
			for _, variant := range data.Variants {
				if refusal := edge(path.child(variant.Name)); refusal != "" {
					return 0, refusal
				}
				declared, refusal := fields(variant.Fields, path.child(variant.Name))
				if refusal != "" {
					return 0, refusal
				}
				variants = append(variants, CodecPlanVariant{Tag: variant.Name, Fields: declared})
			}
			plan.Nodes[index].Variants = variants
		}
		active[key] = false
		depths[index] = deepest + 1
		return index, ""
	}
	var index int
	var refusal string
	if codec.transform != nil {
		index, refusal = transform(codec, root)
	} else {
		index, refusal = visit(codec.domainID, codec, root, codec.Type)
	}
	if refusal != "" {
		return nil, refusal
	}
	for _, reference := range codec.With {
		for _, witness := range codec.with {
			if witness.Name == reference.Name && !used[witness] {
				return nil, "with entry " + witness.Name + " is unused: " + witness.Type + " does not occur in the representation of " + codec.Type
			}
		}
	}
	plan.Root, plan.Nesting = index, depths[index]
	if plan.Nesting > bounds.MaxDepth {
		return nil, "plan nesting " + strconv.Itoa(plan.Nesting) + " exceeds maxDepth " + strconv.Itoa(bounds.MaxDepth)
	}
	plan.ID = "codec-plan:" + profile + ":" + plan.Nodes[index].ID + ":" + strconv.Itoa(bounds.MaxBodyBytes) + ":" + strconv.Itoa(bounds.MaxDepth)
	return plan, ""
}
