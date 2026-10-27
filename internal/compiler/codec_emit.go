package compiler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	rt "ergo.local/prototype/runtime/ergo"
)

// codecLoweringHelper names the lowering helpers that execute a derived
// codec direction. The application plan retains them, the codec runtime
// module and each executed plan only through a reachable direction.
const codecLoweringHelper = "codec"

// goCodecHelpers adapt the native engine and a plan's generated writer to a
// direction function's recipe. A structural failure carries the bounded
// *er.CodecError as its payload; a transformation step runs its function's
// recipe on the caller's context and halts with the recipe's exact cause, so
// its typed failure, defect or interruption propagates unchanged and is never
// a structural failure. A plan the engine refuses is a compiler defect, so it
// panics at initialization.
const goCodecHelpers = `func efCodecCompile(plan er.CodecPlan)*er.Codec{codec,err:=er.CompileCodec(plan);if err!=nil{panic(err)};return codec}
type efCodecHalt struct{cause er.Cause}
func (h *efCodecHalt)Error()string{return "codec transformation halted: "+h.cause.Error()}
func efCodecStep[A any](exit efExit[A])(any,error){if exit.IsFailure(){return nil,&efCodecHalt{cause:exit.Cause()}};return exit.Value,nil}
func efCodecTyped[A any](exit efExit[A])(A,error){if exit.IsFailure(){var zero A;return zero,&efCodecHalt{cause:exit.Cause()}};return exit.Value,nil}
func efCodecExit[A any](tag string,err error)efExit[A]{if halt,ok:=err.(*efCodecHalt);ok{return er.FromCause[A](halt.cause)};return er.Fail[A](tag,err)}
func efCodecDecode[A any](codec *er.Codec,tag string,input string,steps []er.CodecStep)efExit[A]{value,err:=codec.DecodeSteps([]byte(input),steps);if err!=nil{return efCodecExit[A](tag,err)};return er.Succeed(value.(A))}
func efCodecEncoded(tag string,encoded []byte,err error)efExit[string]{if err!=nil{return efCodecExit[string](tag,err)};return er.Succeed(string(encoded))}
`

// codecOperation retains what one executed direction needs: its plan, the
// nominal declarations the plan's adapters construct, the codec runtime
// module and the lowering helpers.
func (p *applicationPlanner) codecOperation(e *Expr, owner applicationOrigin) {
	if e.codec == nil || e.codec.plan == nil {
		p.err = fmt.Errorf("application codec operation at offset %d has no checked plan", e.Span.Offset)
		return
	}
	plan := e.codec.plan
	if p.require(RequiresCodecPlan, plan.ID, owner, "codec-"+e.Text) {
		planOrigin := applicationOrigin{RequiresCodecPlan, plan.ID}
		for _, node := range plan.Nodes {
			p.typeID(node.typeID, planOrigin)
		}
	}
	p.runtimeModule(rt.RuntimeModuleCodec, applicationOrigin{RequiresCodecPlan, plan.ID}, "codec-plan")
	p.helper(codecLoweringHelper, owner)
}

// goLocalPattern matches a generated local variable, which an encode entry
// can read through a pointer without holding a copy.
var goLocalPattern = regexp.MustCompile(`^efLocal_[A-Za-z0-9_]+$`)

func goCodecPlanName(plan *CodecPlan) string { return "efCodecPlan_" + strconv.Itoa(plan.index) }

// goCodecEncodeName is the generated encode entry of a plan: it takes a
// pointer to the boundary value, which it reads in place, and, when a step
// is an effect function, the direction's context; it returns the output or
// the error that ended the pass.
func goCodecEncodeName(plan *CodecPlan) string { return "efCodecEncode_" + strconv.Itoa(plan.index) }

// goCodecKinds names the runtime constant of each plan node kind.
var goCodecKinds = map[rt.CodecKind]string{rt.CodecString: "er.CodecString", rt.CodecBool: "er.CodecBool", rt.CodecVoid: "er.CodecVoid", rt.CodecI64: "er.CodecI64", rt.CodecRecord: "er.CodecRecord", rt.CodecUnion: "er.CodecUnion", rt.CodecTransform: "er.CodecTransform"}

// codecPlans emits every retained plan once, in derivation order, as a
// compiled package-level codec and its generated writer. Nominal nodes
// reference one Construct adapter per declaration, shared by every plan;
// decoding runs on the plan. Encoding runs the plan's writer, which reads the
// generated structs in place (see goCodecWriters).
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
			if node.Kind == rt.CodecTransform {
				literal += ",Type:" + strconv.Quote(node.ID) + ",Child:" + strconv.Itoa(*node.Child) + ",Step:" + strconv.Itoa(*node.Step)
			}
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
				literal += ",Construct:efCodecConstruct_" + goIdent(node.data.Name)
			}
			nodes = append(nodes, literal+"}")
		}
		compiled.WriteString("var " + goCodecPlanName(plan) + " = efCodecCompile(er.CodecPlan{Profile:" + strconv.Quote(plan.Profile) + ",Bounds:er.CodecBounds{MaxBodyBytes:" + strconv.Itoa(plan.Bounds.MaxBodyBytes) + ",MaxDepth:" + strconv.Itoa(plan.Bounds.MaxDepth) + "},Root:" + strconv.Itoa(plan.Root) + ",Nodes:[]er.CodecNode{\n" + strings.Join(nodes, ",\n") + "}})\n")
		if g.codecEncodes(plan) {
			compiled.WriteString(goCodecWriters(goCodecPlanName(plan), strconv.Itoa(plan.index), plan.Root, g.codecWriterNodes(plan), g.codecWriterSteps(plan)))
		}
	}
	return adapters.String() + compiled.String()
}

// codecEncodes reports whether the application encodes through plan. A
// writer calls each step's encode function directly, so a transformed plan
// gets one only when a retained encode direction runs it, which retains
// those functions; a decode-only plan has no writer. A plan without
// transformations always gets one: an endpoint's failure witness encodes
// through it with no direction function.
func (g *goEmitter) codecEncodes(plan *CodecPlan) bool {
	if len(plan.transforms) == 0 {
		return true
	}
	for _, codec := range g.program.Codecs {
		if codec.plan == plan && codec.encode != nil && g.plan.Requires(RequiresFunction, codec.encode.Identity) {
			return true
		}
	}
	return false
}

// codecWriterSteps names the fixed encode function of each plan step, the
// encode transformation of the step's witness, in its Go calling
// convention.
func (g *goEmitter) codecWriterSteps(plan *CodecPlan) []goCodecWriterStep {
	steps := make([]goCodecWriterStep, 0, len(plan.transforms))
	for _, witness := range plan.transforms {
		f := witness.transform.encode
		step := goCodecWriterStep{Function: f.goEmissionName(), Form: goCodecStepPure}
		switch {
		case f.Effect:
			step.Form = goCodecStepEffect
		case canonicalVoidType(g.program.semantic, f.returnID):
			step.Form = goCodecStepPureVoid
		}
		steps = append(steps, step)
	}
	return steps
}

// codecAdapters emits the Construct adapter of one nominal node. It asserts
// each decoded field to its declared layout, rendered from the field's
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
	if node.Kind == rt.CodecRecord {
		return "func efCodecConstruct_" + name + "(_ int,fields []any)any{return " + typeName + "{" + strings.Join(construct(node.Fields), ",") + "}}\n"
	}
	if node.data.Kind == "error" {
		// A failure witness's one variant is the failure's payload: its
		// declaration's struct, or nil for a fieldless failure.
		if fields := node.Variants[0].Fields; len(fields) > 0 {
			return "func efCodecConstruct_" + name + "(_ int,fields []any)any{return " + typeName + "{" + strings.Join(construct(fields), ",") + "}}\n"
		}
		return "func efCodecConstruct_" + name + "(_ int,_ []any)any{return nil}\n"
	}
	var out strings.Builder
	out.WriteString("func efCodecConstruct_" + name + "(variant int,fields []any)any{switch variant{\n")
	for index, variant := range node.Variants {
		out.WriteString("case " + strconv.Itoa(index) + ":return " + typeName + "(" + goVariantType(node.data.Name, variant.Tag) + "{" + strings.Join(construct(variant.Fields), ",") + "})\n")
	}
	out.WriteString("};panic(" + strconv.Quote("ergo codec: unknown variant of "+node.data.Name) + ")}\n")
	return out.String()
}

// codecWriterNodes describes a plan's nodes in the generated Go layout its
// writer reads: each node's carrier type (a transform node's is its domain),
// each field's Go selector and each variant's struct.
func (g *goEmitter) codecWriterNodes(plan *CodecPlan) []goCodecWriterNode {
	c := g.program.semantic
	fields := func(declared []CodecPlanField) []goCodecWriterField {
		out := make([]goCodecWriterField, 0, len(declared))
		for _, field := range declared {
			out = append(out, goCodecWriterField{Name: field.Name, GoField: goFieldName(field.Name), Node: field.Node})
		}
		return out
	}
	nodes := make([]goCodecWriterNode, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		writer := goCodecWriterNode{Kind: node.Kind, GoType: canonicalGoType(c, node.typeID, map[TypeID]bool{})}
		switch node.Kind {
		case rt.CodecTransform:
			writer.Child, writer.Step = *node.Child, *node.Step
		case rt.CodecRecord:
			writer.Fields = fields(node.Fields)
		case rt.CodecUnion:
			if node.data.Kind == "error" {
				// A failure's payload is its declaration's struct, or nil
				// for a fieldless failure; it reaches the writer as any.
				writer.GoType, writer.Failure = "any", true
			}
			for _, variant := range node.Variants {
				goType := goVariantType(node.data.Name, variant.Tag)
				if writer.Failure {
					goType = "efType_" + goIdent(node.data.Name)
				}
				writer.Variants = append(writer.Variants, goCodecWriterVariant{Tag: variant.Tag, GoType: goType, Fields: fields(variant.Fields)})
			}
		}
		nodes = append(nodes, writer)
	}
	return nodes
}

// goCodecWriterNode is one plan node in the Go layout a generated writer
// reads: GoType is the node's carrier (a transform node's domain; a failure
// root's payload, any). A union's variants name their structs; a failure's
// one variant names its declaration's struct.
type goCodecWriterNode struct {
	Kind     rt.CodecKind
	GoType   string
	Failure  bool
	Fields   []goCodecWriterField
	Variants []goCodecWriterVariant
	Child    int
	Step     int
}

type goCodecWriterField struct {
	Name    string
	GoField string
	Node    int
}

type goCodecWriterVariant struct {
	Tag    string
	GoType string
	Fields []goCodecWriterField
}

// goCodecStepForm is the Go calling convention of a transform node's fixed
// encode step.
type goCodecStepForm int

const (
	// goCodecStepPure is a pure function returning its representation.
	goCodecStepPure goCodecStepForm = iota
	// goCodecStepPureVoid is a pure function whose void representation has
	// no Go result.
	goCodecStepPureVoid
	// goCodecStepEffect is an effect function: its recipe runs on the
	// direction's own context, and a failed exit halts the pass with the
	// recipe's exact cause.
	goCodecStepEffect
	// goCodecStepFallible returns its representation and an error that
	// halts the pass unchanged.
	goCodecStepFallible
)

// goCodecWriterStep is the function a transform node's writer calls: one
// fixed Go function per plan step, named at compile time.
type goCodecWriterStep struct {
	Function string
	Form     goCodecStepForm
}

// goCodecWriters emits one plan's writer: one function per record, union
// and transform node, calling one another directly, and its encode entry
// (goCodecEncodeName). The encode pass reads the value in place:
//
//   - a record's writer passes a pointer to each field to the field's
//     writer, so no nested record is copied;
//   - a union's writer binds the variant it holds once, in a type switch, as
//     any Go match over the generated enum does, and reads that binding
//     through pointers;
//   - a transform node's writer calls its step's function (steps[node.Step])
//     directly, with the domain value, by value, and writes the
//     representation the function returns, so neither is boxed and no step
//     is dispatched through a function value.
//
// Keys, tags and separators are spelled at compile time. A writer whose
// subtree holds no transformation stops once the pass has failed; one that
// holds a transformation keeps going, so every transformation runs in
// declared order before a deferred structural failure is reported. A writer
// whose subtree holds a step that can halt (an effect or fallible step)
// returns that step's error, and one whose subtree holds an effect step
// takes the direction's context, ctx; the entry takes ctx exactly when the
// root's writer does.
//
// Two invariants belong to trusted lowering, not to Go's type system: the
// compiler supplies one function per step, and a union carrier holds one of
// its declared variants. Trusted Go can still build a nil or foreign enum
// interface; the writer panics on it as a defect rather than reporting a
// codec failure, since no checked Ergo value or decode result is one.
func goCodecWriters(planName, suffix string, root int, nodes []goCodecWriterNode, steps []goCodecWriterStep) string {
	// subtree reports, memoized per node, whether any transform node below
	// index (inclusive) satisfies holds.
	subtree := func(holds func(goCodecWriterNode) bool) func(int) bool {
		memo := make([]int8, len(nodes))
		var visit func(int) bool
		visit = func(index int) bool {
			if memo[index] != 0 {
				return memo[index] > 0
			}
			node := nodes[index]
			result := node.Kind == rt.CodecTransform && holds(node)
			children := []int{}
			if node.Kind == rt.CodecTransform {
				children = append(children, node.Child)
			}
			for _, field := range node.Fields {
				children = append(children, field.Node)
			}
			for _, variant := range node.Variants {
				for _, field := range variant.Fields {
					children = append(children, field.Node)
				}
			}
			for _, child := range children {
				result = visit(child) || result
			}
			memo[index] = -1
			if result {
				memo[index] = 1
			}
			return result
		}
		return visit
	}
	transforms := subtree(func(goCodecWriterNode) bool { return true })
	halts := subtree(func(node goCodecWriterNode) bool {
		form := steps[node.Step].Form
		return form == goCodecStepEffect || form == goCodecStepFallible
	})
	effects := subtree(func(node goCodecWriterNode) bool { return steps[node.Step].Form == goCodecStepEffect })
	name := func(index int) string { return "efCodecWrite_" + suffix + "_" + strconv.Itoa(index) }
	arguments := func(index int, value string) string {
		if effects(index) {
			return "(w,ctx," + value + ")"
		}
		return "(w," + value + ")"
	}
	raw := func(text string) string { return "w.Raw(" + strconv.Quote(text) + ")\n" }
	// write renders the statements writing node index from a value
	// expression and a pointer to it.
	write := func(index int, value, pointer string) string {
		switch nodes[index].Kind {
		case rt.CodecString:
			return "w.String(" + value + ")\n"
		case rt.CodecBool:
			return "w.Bool(" + value + ")\n"
		case rt.CodecI64:
			return "w.I64(" + value + ")\n"
		case rt.CodecVoid:
			return "w.Void()\n"
		}
		if halts(index) {
			return "if err:=" + name(index) + arguments(index, pointer) + ";err!=nil{return err}\n"
		}
		return name(index) + arguments(index, pointer) + "\n"
	}
	// object writes an object that opens with head: each field's key
	// follows head (after separator) or the previous field's value.
	object := func(head, separator string, fields []goCodecWriterField, base string) string {
		var out strings.Builder
		for _, field := range fields {
			out.WriteString(raw(head + separator + rt.CodecSpelling(field.Name) + ":"))
			out.WriteString("w.Enter(" + strconv.Quote(field.Name) + ")\n")
			out.WriteString(write(field.Node, base+"."+field.GoField, "&"+base+"."+field.GoField))
			out.WriteString("w.Leave()\n")
			head, separator = "", ","
		}
		return out.String() + raw(head+"}")
	}
	var out strings.Builder
	for index, node := range nodes {
		if node.Kind != rt.CodecRecord && node.Kind != rt.CodecUnion && node.Kind != rt.CodecTransform {
			continue
		}
		out.WriteString("func " + name(index) + "(w *er.CodecWriter,")
		if effects(index) {
			out.WriteString("ctx efContext,")
		}
		out.WriteString("v *" + node.GoType + ")")
		done := "}\n"
		if halts(index) {
			out.WriteString("error")
			done = "return nil\n}\n"
		}
		out.WriteString("{\n")
		if !transforms(index) {
			out.WriteString("if !w.Live(){return}\n")
		}
		switch node.Kind {
		case rt.CodecRecord:
			out.WriteString(object("{", "", node.Fields, "v"))
		case rt.CodecUnion:
			binds := false
			for _, variant := range node.Variants {
				for _, field := range variant.Fields {
					binds = binds || nodes[field.Node].Kind != rt.CodecVoid
				}
			}
			if binds {
				out.WriteString("switch x:=(*v).(type){\n")
			} else {
				out.WriteString("switch (*v).(type){\n")
			}
			for _, variant := range node.Variants {
				held := variant.GoType
				if node.Failure && len(variant.Fields) == 0 {
					held = "nil," + held
				}
				out.WriteString("case " + held + ":\n")
				out.WriteString(object("{"+rt.CodecSpelling(rt.CodecTagKey)+":"+rt.CodecSpelling(variant.Tag), ",", variant.Fields, "x"))
			}
			out.WriteString("default:panic(" + strconv.Quote("ergo codec: "+node.GoType+" carrier holds no declared variant") + ")\n}\n")
		case rt.CodecTransform:
			step := steps[node.Step]
			represented := "r"
			if nodes[node.Child].Kind == rt.CodecVoid {
				represented = "_"
			}
			switch {
			case step.Form == goCodecStepPure && represented == "r":
				out.WriteString("r:=" + step.Function + "(*v)\n")
			case step.Form == goCodecStepPure, step.Form == goCodecStepPureVoid:
				out.WriteString(step.Function + "(*v)\n")
			case step.Form == goCodecStepEffect:
				out.WriteString(represented + ",err:=efCodecTyped(" + step.Function + "(*v)(ctx))\nif err!=nil{return err}\n")
			case step.Form == goCodecStepFallible:
				out.WriteString(represented + ",err:=" + step.Function + "(*v)\nif err!=nil{return err}\n")
			}
			out.WriteString(write(node.Child, "r", "&r"))
		}
		out.WriteString(done)
	}
	out.WriteString("func efCodecEncode_" + suffix + "(value *" + nodes[root].GoType)
	if effects(root) {
		out.WriteString(",ctx efContext")
	}
	out.WriteString(")([]byte,error){\nw:=" + planName + ".Writer()\n")
	if halts(root) {
		out.WriteString("return w.Finish(" + name(root) + arguments(root, "value") + ")\n}\n")
		return out.String()
	}
	out.WriteString(write(root, "*value", "value") + "return w.Finish(nil)\n}\n")
	return out.String()
}

// codecOperation lowers one executed direction inside its function's
// recipe: the operation runs now, and its typed failure propagates. Each
// transformation step adapts its function value to the engine: it asserts
// the step's input carrier, runs an effect function's recipe on the
// direction's own context and returns a pure function's result directly.
func (g *goEmitter) codecOperation(e *Expr, ret string, out *strings.Builder) string {
	codec := e.codec
	input := g.expr(e.Left, true, ret, out)
	if e.Text == "encode" {
		return g.codecEncodeOperation(e, input, ret, out)
	}
	steps := "nil"
	if len(e.Args) > 0 {
		adapted := []string{}
		for _, step := range e.Args {
			value := g.expr(step, true, ret, out)
			f := step.ResolvedFunction
			call := value + "(value.(" + canonicalGoType(g.program.semantic, f.Params[0].typeID, map[TypeID]bool{}) + "))"
			body := "return " + call + ",nil"
			if f.Effect {
				body = "return efCodecStep(" + call + "(ctx))"
			}
			adapted = append(adapted, "func(value any)(any,error){"+body+"}")
		}
		steps = "[]er.CodecStep{" + strings.Join(adapted, ",") + "}"
	}
	name := g.temp()
	if e.Text == "decode" {
		out.WriteString(name + " := efCodecDecode[" + g.valueType(e) + "](" + goCodecPlanName(codec.plan) + "," + strconv.Quote(codec.derivation.DecodeFailure) + "," + input + "," + steps + ")\n" + g.failed(name, ret))
		return name + ".Value"
	}
	panic("unreachable codec direction " + e.Text)
}

// codecEncodeOperation runs an encode direction through its plan's
// generated writer, which calls each step's encode function directly (see
// codecWriterSteps), so the operation passes no steps: only the direction's
// context, when a step is an effect function whose recipe runs on it.
func (g *goEmitter) codecEncodeOperation(e *Expr, input, ret string, out *strings.Builder) string {
	plan := e.codec.plan
	if len(e.Args) != len(plan.transforms) {
		panic(fmt.Sprintf("codec plan %s has %d transformations; its encode direction passes %d", plan.ID, len(plan.transforms), len(e.Args)))
	}
	ctxArg := ""
	for _, step := range g.codecWriterSteps(plan) {
		if step.Form == goCodecStepEffect {
			ctxArg = ",ctx"
		}
	}
	// A local is read in place; any other value is held once. A failure's
	// payload reaches its writer as any.
	if root := plan.Nodes[plan.Root]; root.data != nil && root.data.Kind == "error" {
		held := g.temp()
		out.WriteString("var " + held + " any = " + input + "\n")
		input = held
	} else if !goLocalPattern.MatchString(input) {
		held := g.temp()
		out.WriteString(held + " := " + input + "\n")
		input = held
	}
	name, encoded, err := g.temp(), g.temp(), g.temp()
	out.WriteString(encoded + "," + err + " := " + goCodecEncodeName(plan) + "(&" + input + ctxArg + ")\n" + name + " := efCodecEncoded(" + strconv.Quote(e.codec.derivation.EncodeFailure) + "," + encoded + "," + err + ")\n" + g.failed(name, ret))
	return name + ".Value"
}
