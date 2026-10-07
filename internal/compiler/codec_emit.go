package compiler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	rt "effra.local/prototype/runtime/effra"
)

// codecLoweringHelper names the lowering helpers that execute a derived
// codec direction. The application plan retains them, the codec runtime
// module and each executed plan only through a reachable direction.
const codecLoweringHelper = "codec"

// goCodecHelpers adapt the native engine to a direction function's recipe.
// A failure carries the bounded *er.CodecError as its payload; a plan the
// engine refuses is a compiler defect, so it panics at initialization.
const goCodecHelpers = `func efCodecCompile(plan er.CodecPlan)*er.Codec{codec,err:=er.CompileCodec(plan);if err!=nil{panic(err)};return codec}
func efCodecDecode[A any](codec *er.Codec,tag string,input string)efExit[A]{value,err:=codec.Decode([]byte(input));if err!=nil{return er.Fail[A](tag,err)};return er.Succeed(value.(A))}
func efCodecEncode(codec *er.Codec,tag string,value any)efExit[string]{encoded,err:=codec.Encode(value);if err!=nil{return er.Fail[string](tag,err)};return er.Succeed(string(encoded))}
`

// codecOperation retains what one executed direction needs: its plan, the
// nominal declarations the plan's adapters construct, the codec runtime
// module and the lowering helpers.
func (p *applicationPlanner) codecOperation(e *Expr, owner string) {
	if e.codec == nil || e.codec.plan == nil {
		p.err = fmt.Errorf("application codec operation at offset %d has no checked plan", e.Span.Offset)
		return
	}
	plan := e.codec.plan
	if p.require(RequiresCodecPlan, plan.ID, owner, "codec-"+e.Text) {
		for _, node := range plan.Nodes {
			p.typeID(node.typeID, plan.ID)
		}
	}
	p.runtimeModule(rt.RuntimeModuleCodec, plan.ID, "codec-plan")
	p.helper(codecLoweringHelper, owner)
}

func goCodecPlanName(plan *CodecPlan) string { return "efCodecPlan_" + strconv.Itoa(plan.index) }

// goCodecKinds names the runtime constant of each plan node kind.
var goCodecKinds = map[rt.CodecKind]string{rt.CodecString: "er.CodecString", rt.CodecBool: "er.CodecBool", rt.CodecVoid: "er.CodecVoid", rt.CodecI64: "er.CodecI64", rt.CodecRecord: "er.CodecRecord", rt.CodecUnion: "er.CodecUnion"}

// codecPlans emits every retained plan once, in derivation order, as a
// compiled package-level codec. Nominal nodes reference one pair of
// Construct/Project adapters per declaration, shared by every plan.
func (g *goEmitter) codecPlans(plans []*CodecPlan) string {
	var adapters, compiled strings.Builder
	emitted := map[TypeID]bool{}
	fields := func(declared []CodecPlanField) string {
		out := []string{}
		for _, field := range declared {
			out = append(out, "{Name:"+strconv.Quote(field.Name)+",Node:"+strconv.Itoa(field.Node)+"}")
		}
		return "[]er.CodecField{" + strings.Join(out, ",") + "}"
	}
	for _, plan := range plans {
		if !g.plan.Requires(RequiresCodecPlan, plan.ID) {
			continue
		}
		nodes := []string{}
		for _, node := range plan.Nodes {
			literal := "{Kind:" + goCodecKinds[node.Kind]
			if node.data != nil {
				if !emitted[node.typeID] {
					emitted[node.typeID] = true
					adapters.WriteString(g.codecAdapters(node))
				}
				literal += ",Type:" + strconv.Quote(node.ID)
				if node.Kind == rt.CodecRecord {
					literal += ",Fields:" + fields(node.Fields)
				} else {
					variants := []string{}
					for _, variant := range node.Variants {
						variants = append(variants, "{Tag:"+strconv.Quote(variant.Tag)+",Fields:"+fields(variant.Fields)+"}")
					}
					literal += ",Variants:[]er.CodecVariant{" + strings.Join(variants, ",") + "}"
				}
				literal += ",Construct:efCodecConstruct_" + goIdent(node.data.Name) + ",Project:efCodecProject_" + goIdent(node.data.Name)
			}
			nodes = append(nodes, literal+"}")
		}
		compiled.WriteString("var " + goCodecPlanName(plan) + " = efCodecCompile(er.CodecPlan{Profile:" + strconv.Quote(plan.Profile) + ",Bounds:er.CodecBounds{MaxBodyBytes:" + strconv.Itoa(plan.Bounds.MaxBodyBytes) + ",MaxDepth:" + strconv.Itoa(plan.Bounds.MaxDepth) + "},Root:" + strconv.Itoa(plan.Root) + ",Nodes:[]er.CodecNode{\n" + strings.Join(nodes, ",\n") + "}})\n")
	}
	return adapters.String() + compiled.String()
}

// codecAdapters emits the engine's carrier adapters of one nominal node.
// Each field is asserted to its declared layout, rendered from the field's
// canonical type: derived plans hold only first-order data, so no template
// bindings apply. A carrier mismatch is a defect and panics.
func (g *goEmitter) codecAdapters(node CodecPlanNode) string {
	c := g.program.semantic
	name := goIdent(node.data.Name)
	typeName := "efType_" + name
	construct := func(declared []CodecPlanField) []string {
		values := []string{}
		for index, field := range declared {
			values = append(values, goFieldName(field.Name)+":fields["+strconv.Itoa(index)+"].("+canonicalGoType(c, field.typeID, map[TypeID]bool{})+")")
		}
		return values
	}
	project := func(local string, declared []CodecPlanField) string {
		values := []string{}
		for _, field := range declared {
			values = append(values, local+"."+goFieldName(field.Name))
		}
		return "[]any{" + strings.Join(values, ",") + "}"
	}
	var out strings.Builder
	if node.Kind == rt.CodecRecord {
		out.WriteString("func efCodecConstruct_" + name + "(_ int,fields []any)any{return " + typeName + "{" + strings.Join(construct(node.Fields), ",") + "}}\n")
		if len(node.Fields) == 0 {
			out.WriteString("func efCodecProject_" + name + "(value any)(int,[]any){_=value.(" + typeName + ");return 0,[]any{}}\n")
		} else {
			out.WriteString("func efCodecProject_" + name + "(value any)(int,[]any){record:=value.(" + typeName + ");return 0," + project("record", node.Fields) + "}\n")
		}
		return out.String()
	}
	defect := "panic(" + strconv.Quote("effra codec: unknown variant of "+node.data.Name) + ")"
	out.WriteString("func efCodecConstruct_" + name + "(variant int,fields []any)any{switch variant{\n")
	for index, variant := range node.Variants {
		out.WriteString("case " + strconv.Itoa(index) + ":return " + typeName + "(" + goVariantType(node.data.Name, variant.Tag) + "{" + strings.Join(construct(variant.Fields), ",") + "})\n")
	}
	out.WriteString("};" + defect + "}\n")
	bound := false
	for _, variant := range node.Variants {
		bound = bound || len(variant.Fields) > 0
	}
	if bound {
		out.WriteString("func efCodecProject_" + name + "(value any)(int,[]any){switch variant:=value.(type){\n")
	} else {
		out.WriteString("func efCodecProject_" + name + "(value any)(int,[]any){switch value.(type){\n")
	}
	for index, variant := range node.Variants {
		out.WriteString("case " + goVariantType(node.data.Name, variant.Tag) + ":return " + strconv.Itoa(index) + "," + project("variant", variant.Fields) + "\n")
	}
	out.WriteString("};" + defect + "}\n")
	return out.String()
}

// codecOperation lowers one executed direction inside its function's
// recipe: the operation runs now, and its typed failure propagates.
func (g *goEmitter) codecOperation(e *Expr, ret string, out *strings.Builder) string {
	codec := e.codec
	input := g.expr(e.Left, true, ret, out)
	call := "efCodecEncode(" + goCodecPlanName(codec.plan) + "," + strconv.Quote(codec.derivation.EncodeFailure) + "," + input + ")"
	if e.Text == "decode" {
		call = "efCodecDecode[" + g.valueType(e) + "](" + goCodecPlanName(codec.plan) + "," + strconv.Quote(codec.derivation.DecodeFailure) + "," + input + ")"
	}
	name := g.temp()
	out.WriteString(name + " := " + call + "\n" + g.failed(name, ret))
	return name + ".Value"
}

// jsCodecAdapters run a compiled JS codec as one direction's Effect. Decode
// refuses text that is not well-formed Unicode instead of letting UTF-8
// encoding replace it; such text exists only in host strings and has no byte
// offset. A failure is the direction's tagged failure carrying the engine's
// bounded issue and the message Go's CodecError renders: path names are
// ASCII identifiers, so JSON.stringify quotes them as strconv.Quote does.
const jsCodecAdapters = `const __ef_codecText = new TextEncoder();
const __ef_codecBytes = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true });
const __ef_codecMessage = issue => "codec " + issue.direction + ": " + issue.reason + (issue.offset >= 0 ? " at byte " + issue.offset : "") + (issue.path.length > 0 ? " at " + issue.path.map(name => "[" + JSON.stringify(name) + "]").join("") : "");
const __ef_codecFailure = (tag, issue) => Effect.fail({ _tag: tag, message: __ef_codecMessage(issue), issue });
const __ef_codecDecode = (codec, tag, text) => Effect.suspend(() => {
  if (!text.isWellFormed()) return __ef_codecFailure(tag, { direction: "decode", reason: "invalid-unicode", path: [], offset: -1 });
  const result = codec.decode(__ef_codecText.encode(text));
  return result.ok ? Effect.succeed(result.value) : __ef_codecFailure(tag, result.issue);
});
const __ef_codecEncode = (codec, tag, value) => Effect.suspend(() => {
  const result = codec.encode(value);
  return result.ok ? Effect.succeed(__ef_codecBytes.decode(result.bytes)) : __ef_codecFailure(tag, result.issue);
});
`

func jsCodecPlanName(plan *CodecPlan) string { return "__ef_codecPlan_" + strconv.Itoa(plan.index) }

type jsCodecField struct {
	Name string `json:"name"`
	Node int    `json:"node"`
}

type jsCodecVariant struct {
	Tag       string         `json:"tag"`
	DomainTag string         `json:"domainTag"`
	Fields    []jsCodecField `json:"fields"`
}

type jsCodecNode struct {
	Kind     rt.CodecKind     `json:"kind"`
	Type     string           `json:"type,omitempty"`
	Fields   *[]jsCodecField  `json:"fields,omitempty"`
	Variants []jsCodecVariant `json:"variants,omitempty"`
}

type jsCodecPlan struct {
	Profile string          `json:"profile"`
	Bounds  CodecPlanBounds `json:"bounds"`
	Root    int             `json:"root"`
	Nodes   []jsCodecNode   `json:"nodes"`
}

// jsCodecPlanLiteral renders the JS form of a plan. It differs from the Go
// form only in each variant's domainTag: the in-memory _tag of the Effra JS
// variant carrier, which Go represents by adapters instead.
func jsCodecPlanLiteral(plan *CodecPlan) string {
	fields := func(declared []CodecPlanField) []jsCodecField {
		out := make([]jsCodecField, 0, len(declared))
		for _, field := range declared {
			out = append(out, jsCodecField{Name: field.Name, Node: field.Node})
		}
		return out
	}
	literal := jsCodecPlan{Profile: plan.Profile, Bounds: plan.Bounds, Root: plan.Root, Nodes: make([]jsCodecNode, 0, len(plan.Nodes))}
	for _, node := range plan.Nodes {
		converted := jsCodecNode{Kind: node.Kind}
		if node.data != nil {
			converted.Type = node.ID
		}
		switch node.Kind {
		case rt.CodecRecord:
			declared := fields(node.Fields)
			converted.Fields = &declared
		case rt.CodecUnion:
			for _, variant := range node.Variants {
				converted.Variants = append(converted.Variants, jsCodecVariant{Tag: variant.Tag, DomainTag: node.data.Name + "." + variant.Tag, Fields: fields(variant.Fields)})
			}
		}
		literal.Nodes = append(literal.Nodes, converted)
	}
	encoded, err := json.Marshal(literal)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// jsCodecSupport compiles one JS codec per plan the module's application
// plan retains, after the `codec` prelude chunk that the codec lowering
// helper selects. A plan no executed direction reaches is neither compiled
// nor able to load the engine.
func (r *Result) jsCodecSupport(application *ApplicationPlan) string {
	var out strings.Builder
	for _, plan := range r.CodecPlans {
		if !application.Requires(RequiresCodecPlan, plan.ID) {
			continue
		}
		out.WriteString("const " + jsCodecPlanName(plan) + " = __ef_codecCompile(" + jsCodecPlanLiteral(plan) + ");\n")
	}
	return out.String()
}

func jsCodecOperation(e *Expr) string {
	codec := e.codec
	if e.Text == "decode" {
		return "(yield* __ef_codecDecode(" + jsCodecPlanName(codec.plan) + ", " + quoted(codec.derivation.DecodeFailure) + ", " + jsExpr(e.Left, true) + "))"
	}
	return "(yield* __ef_codecEncode(" + jsCodecPlanName(codec.plan) + ", " + quoted(codec.derivation.EncodeFailure) + ", " + jsExpr(e.Left, true) + "))"
}

// jsCodecExports exports each witness as a frozen object holding its two
// direction functions, matching the source spelling witness.decode. Like a
// function, the object is bound under a generated name and exported under
// its source name, so a witness never shadows a host global, an import or a
// generated binding of the module. Only a witness whose plan retains both
// directions is exported: the object must never reference a direction the
// plan pruned.
func (r *Result) jsCodecExports(plan *ApplicationPlan, out, decl *strings.Builder, declarations map[string]Declaration) {
	for _, codec := range r.Program.Codecs {
		if codec.decode == nil || codec.encode == nil || !plan.Requires(RequiresFunction, codec.decode.Identity) || !plan.Requires(RequiresFunction, codec.encode.Identity) {
			continue
		}
		binding := jsCodecWitnessName(codec)
		export := "export { " + binding + " as " + codec.Name + " };\n"
		out.WriteString("const " + binding + " = Object.freeze({ decode: " + codec.decode.jsEmissionName() + ", encode: " + codec.encode.jsEmissionName() + " });\n" + export)
		decl.WriteString("declare const " + binding + ": { readonly decode: " + jsRowFunctionSignature(r.Program, codec.decode, declarations) + "; readonly encode: " + jsRowFunctionSignature(r.Program, codec.encode, declarations) + " };\n" + export)
	}
}

func jsCodecWitnessName(codec *CodecDeclaration) string { return "__ef_codec_witness_" + codec.Name }
