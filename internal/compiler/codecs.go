package compiler

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	rt "effra.local/prototype/runtime/effra"
)

// codecDerivationCode refuses a derive declaration whose checked type, bounds
// or options cannot be represented by its profile. No partial plan is kept.
const codecDerivationCode = "EF138"

// codecDomainLayoutWork bounds the plain-data walk of one transformed
// domain: one unit per type occurrence visited. It equals the plan node
// ceiling, the size of the largest structural plan a codec may derive.
const codecDomainLayoutWork = maxCodecPlanNodes

// maxCodecMaxBodyBytes is the ceiling of a plan's maxBodyBytes. A derive
// declaration states both bounds explicitly; neither the checker nor the
// engines supply a default, so every plan's budget is visible at its
// declaration.
const maxCodecMaxBodyBytes = 1 << 30

// codecBoundNames are the required bounds of a derive declaration, in
// canonical order.
var codecBoundNames = []string{"maxBodyBytes", "maxDepth"}

// CodecDeclaration is one opt-in structural derivation:
//
//	derive userJson = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 2)
//
// It names an explicit witness. The checker synthesizes the ordinary effect
// functions userJson.decode and userJson.encode, whose signatures carry the
// direction contracts; several declarations may derive the same type.
//
// A transformed witness also names one ordinary function per direction
// between a representation, which the plan derives structurally, and its
// domain type; a witness composes transformed witnesses with `with`:
//
//	derive emailJson = Json.codec<Email>(maxBodyBytes: 256, maxDepth: 1, decode: parseEmail, encode: renderEmail)
//	derive accountJson = Json.codec<Account>(maxBodyBytes: 4096, maxDepth: 2) with emailJson
//
// A failure witness derives a source failure declaration. Its plan encodes
// the failure as a one-variant union tagged with the failure's name, the
// carrier the failure already has, and it is used only as an endpoint's
// mapped failure body (see EndpointDeclaration):
//
//	derive outOfStockJson = Json.codec<OutOfStock>(maxBodyBytes: 256, maxDepth: 1)
type CodecDeclaration struct {
	Name           string
	Span           Span
	Alias          string
	Member         string
	DerivationSpan Span
	Type           string
	TypeSpan       Span
	Options        []CodecOption
	With           []CodecWitnessReference
	sourceType     *sourceType
	derivation     *codecDerivation
	domainID       TypeID
	plan           *CodecPlan
	decode         *Function
	encode         *Function
	// transform is the witness's own checked transformation, if any.
	transform *codecTransform
	// with selects, by domain type, the transformed witnesses applied in
	// this witness's representation; withKey is its canonical spelling, and
	// withSettled reports that every entry resolved to a checked witness.
	with        map[TypeID]*CodecDeclaration
	withKey     string
	withSettled bool
	// headerChecked reports that the declaration's derivation and type
	// resolved, so its direction functions exist.
	headerChecked bool
	// failure marks a failure witness: a structural witness of a source
	// failure declaration, which encodes that failure's payload as a mapped
	// endpoint failure body. It has no direction functions.
	failure bool
}

// CodecOption is one option of a derive declaration: an integer plan bound,
// or the function name of a transformation direction.
type CodecOption struct {
	Name     string
	Value    int64
	Function string
	// Text is a string option's decoded value; Quoted marks it a string.
	// Only endpoint options take strings.
	Text      string
	Quoted    bool
	Span      Span
	ValueSpan Span
}

// CodecWitnessReference names a transformed witness in a `with` clause.
type CodecWitnessReference struct {
	Name string
	Span Span
}

// codecTransform is a checked transformation: decode maps the
// representation to the domain type and encode maps it back. Each is an
// ordinary first-order module function, pure or effectful.
type codecTransform struct {
	decode     *Function
	encode     *Function
	repID      TypeID
	repDisplay string
}

// codecTransformOptions are the option names that select a transformation.
var codecTransformOptions = []string{"decode", "encode"}

func (p *parser) codecDeclaration() *CodecDeclaration {
	name := p.name()
	p.expect("=")
	alias := p.name()
	p.expect(".")
	member := p.memberName()
	codec := &CodecDeclaration{Name: name.text, Span: name.span, Alias: alias.text, Member: member.text, DerivationSpan: alias.span}
	codec.DerivationSpan.Length = member.span.Offset + member.span.Length - alias.span.Offset
	p.expectGenericOpen()
	codec.TypeSpan = p.peek().span
	codec.Type = p.typ()
	codec.sourceType = p.types[codec.Type]
	p.expectGenericClose()
	if p.accept("(") {
		for !p.accept(")") {
			option := p.name()
			p.expect(":")
			value := p.take()
			parsed := CodecOption{Name: option.text, Span: option.span, ValueSpan: value.span}
			switch value.kind {
			case "integer":
				parsed.Value, _ = strconv.ParseInt(value.text, 10, 64)
			case "name":
				parsed.Function = value.text
			default:
				p.fail(value, "derive options take integer bounds or transformation function names")
			}
			codec.Options = append(codec.Options, parsed)
			if !p.accept(",") {
				p.expect(")")
				break
			}
		}
	}
	if p.accept("with") {
		for {
			witness := p.name()
			codec.With = append(codec.With, CodecWitnessReference{Name: witness.text, Span: witness.span})
			if !p.accept(",") {
				break
			}
		}
	}
	return codec
}

// codecDerivation is the contract of a compiler-distributed derivation: the
// profile its plans follow, the wire type and the named failure of each
// direction. Structural derivation introduces no service requirement.
type codecDerivation struct {
	Module        string
	Member        string
	Profile       string
	Wire          string
	DecodeFailure string
	EncodeFailure string
}

func (d *codecDerivation) identity() string { return d.Module + "." + d.Member }

var jsonCodecDerivation = &codecDerivation{Module: "effra/json", Member: "codec", Profile: rt.CodecProfileJSON, Wire: "string", DecodeFailure: "JsonDecodeFailure", EncodeFailure: "JsonEncodeFailure"}

// bundledFailures are the failure declarations a bundled module's interface
// admits on import. They are tag failures whose payload is a bounded
// message, like the builtin failures, and their names are claimed against
// every other declaration.
var bundledFailures = map[string][]string{"effra/json": {jsonCodecDerivation.DecodeFailure, jsonCodecDerivation.EncodeFailure}, i64TextModule: {i64ParseFailure}}

// memberFunction resolves a qualified function member: a bundled module
// function through its import alias, or an operation of a derived codec.
func (p *Program) memberFunction(e *Expr) *Function {
	if f := p.bundledFunction(e); f != nil {
		return f
	}
	if e == nil || e.Kind != "member" || e.Left == nil || e.Left.Kind != "name" {
		return nil
	}
	return p.DerivedBindings[e.Left.Name][e.Name]
}

// admitBundledFailures registers the failure declarations of every imported
// bundled module that declares some.
func (c *checker) admitBundledFailures(claim func(string, Span)) {
	imports := append([]BundledImport{}, c.program.BundledImports...)
	if c.program.interfaceProducer {
		// The isolated interface producer checks a module's members without
		// an import declaration; it admits the failures their rows name.
		for _, f := range c.program.BundledFunctions {
			imports = append(imports, BundledImport{Path: f.Module})
		}
	}
	for _, imported := range imports {
		for _, name := range bundledFailures[imported.Path] {
			if c.errors[name] != nil {
				continue
			}
			claim(name, imported.Span)
			c.errors[name] = &ErrorDecl{Name: name}
			c.program.Errors[name] = Span{}
		}
	}
}

// deriveCodecs checks every derive declaration, derives its plan and
// synthesizes its two direction functions. It runs after data layouts are
// resolved and before function signatures are checked. Headers (type,
// options and transformation) are checked first, then `with` clauses, which
// may name later witnesses, and finally plans and direction contracts.
func (c *checker) deriveCodecs(claim func(string, Span)) {
	c.program.DerivedBindings = map[string]map[string]*Function{}
	plans := map[string]*CodecPlan{}
	named := map[string]*CodecDeclaration{}
	checked := []*CodecDeclaration{}
	bounded := map[*CodecDeclaration]CodecPlanBounds{}
	for _, codec := range c.program.Codecs {
		claim(codec.Name, codec.Span)
		if named[codec.Name] != nil {
			continue
		}
		named[codec.Name] = codec
		codec.derivation = c.program.BundledDerivations[codec.Alias][codec.Member]
		if codec.derivation == nil {
			if c.program.BundledBindings[codec.Alias] == nil {
				c.diagnostic("EF102", "unknown derivation "+codec.Alias+"."+codec.Member+"; import a bundled module that provides it", codec.DerivationSpan)
			} else if _, loaded := c.program.BundledBindings[codec.Alias][codec.Member]; loaded || c.program.BundledTypeBindings[codec.Alias][codec.Member] != nil {
				c.diagnostic(codecDerivationCode, codec.Alias+"."+codec.Member+" is not a codec derivation", codec.DerivationSpan)
			}
			continue
		}
		if c.requiresTemplateArguments(codec.Type) {
			c.diagnostic("EF127", "generic type "+codec.Type+" requires complete application arguments", codec.TypeSpan)
			continue
		}
		if c.errors[codec.Type] != nil && !c.sourceFailure(codec.Type) {
			c.diagnostic(codecDerivationCode, "failure witness "+codec.Name+" of "+codec.Type+" must derive a failure declared in this module; builtin and bundled failures are answered by their profile", codec.TypeSpan)
			continue
		}
		if !c.typeKnown(codec.Type) && !c.sourceFailure(codec.Type) {
			c.diagnostic("EF102", "unknown or unsupported value type "+codec.Type, codec.TypeSpan)
			continue
		}
		codec.domainID = c.canonicalRef(c.typeRef(codec.Type))
		c.bindSourceSyntax(codec.sourceType, codec.domainID)
		bounds, transformation, ok := c.codecOptions(codec)
		if node := c.node(codec.domainID); node != nil && node.Kind == "error" {
			if !c.failureWitness(codec, transformation) {
				continue
			}
		} else {
			codec.transform = c.codecTransformation(codec, transformation)
		}
		if ok {
			bounded[codec] = bounds
		}
		codec.headerChecked = true
		checked = append(checked, codec)
	}
	for _, codec := range checked {
		if !codec.failure {
			c.codecWith(codec, named)
		}
	}
	for _, codec := range checked {
		if bounds, ok := bounded[codec]; ok && codec.transformValid(transformationNamed(codec)) && c.codecComposable(codec) {
			if plan, refusal := c.deriveCodecPlan(codec, bounds); refusal != "" {
				c.diagnostic(codecDerivationCode, "codec "+codec.Name+" cannot derive "+codec.derivation.Profile+" for "+codec.Type+": "+refusal, codec.TypeSpan)
			} else if shared := plans[plan.ID]; shared != nil {
				codec.plan = shared
			} else {
				plan.index = len(c.result.CodecPlans)
				plans[plan.ID] = plan
				c.result.CodecPlans = append(c.result.CodecPlans, plan)
				codec.plan = plan
			}
		}
		if codec.failure {
			continue
		}
		decodeErrors, decodeServices := []string{codec.derivation.DecodeFailure}, []string{}
		encodeErrors, encodeServices := []string{codec.derivation.EncodeFailure}, []string{}
		decodeSteps, encodeSteps := []*Expr{}, []*Expr{}
		for _, witness := range codec.transformations() {
			decodeErrors, decodeServices = c.transformationRows(witness.transform.decode, decodeErrors, decodeServices)
			encodeErrors, encodeServices = c.transformationRows(witness.transform.encode, encodeErrors, encodeServices)
			decodeSteps = append(decodeSteps, &Expr{Kind: "name", Name: witness.transform.decode.Name})
			encodeSteps = append(encodeSteps, &Expr{Kind: "name", Name: witness.transform.encode.Name})
		}
		codec.decode = c.codecFunction(codec, "decode", "input", codec.derivation.Wire, nil, codec.Type, codec.sourceType, decodeErrors, decodeServices, decodeSteps)
		codec.encode = c.codecFunction(codec, "encode", "value", codec.Type, codec.sourceType, codec.derivation.Wire, nil, encodeErrors, encodeServices, encodeSteps)
		c.program.DerivedBindings[codec.Name] = map[string]*Function{"decode": codec.decode, "encode": codec.encode}
		c.program.DerivedFunctions = append(c.program.DerivedFunctions, codec.decode, codec.encode)
	}
}

// failureWitness admits a derive declaration of a failure type as a failure
// witness: the failure is declared in source, so its payload is the
// declaration's own data, and the witness is purely structural. It reports
// whether the header is admitted.
func (c *checker) failureWitness(codec *CodecDeclaration, transformation map[string]*CodecOption) bool {
	refuse := func(message string, span Span) bool {
		c.diagnostic(codecDerivationCode, "failure witness "+codec.Name+" of "+codec.Type+" "+message, span)
		return false
	}
	if !c.sourceFailure(codec.Type) {
		return refuse("must derive a failure declared in this module; builtin and bundled failures are answered by their profile", codec.TypeSpan)
	}
	for _, name := range codecTransformOptions {
		if option := transformation[name]; option != nil {
			return refuse("is structural; a failure body takes no "+name+" transformation", option.Span)
		}
	}
	if len(codec.With) > 0 {
		return refuse("is structural; a failure body composes no witnesses", codec.With[0].Span)
	}
	codec.failure = true
	codec.with, codec.withSettled = map[TypeID]*CodecDeclaration{}, true
	return true
}

// sourceFailure reports whether name is a failure declared in this module.
func (c *checker) sourceFailure(name string) bool {
	return slices.ContainsFunc(c.program.ErrorDecls, func(decl *ErrorDecl) bool { return decl.Name == name })
}

// failureData is the one-variant union a failure witness's plan derives: the
// variant is tagged with the failure's name and carries its fields.
func (c *checker) failureData(name string) *DataDeclaration {
	decl := c.errors[name]
	if decl == nil {
		return nil
	}
	return &DataDeclaration{Kind: "error", Name: name, Variants: []Variant{{Name: name, Fields: decl.Fields}}}
}

// transformationNamed reports whether a declaration names a transformation
// option at all.
func transformationNamed(codec *CodecDeclaration) bool {
	return slices.ContainsFunc(codec.Options, func(option CodecOption) bool { return slices.Contains(codecTransformOptions, option.Name) })
}

// transformValid reports that a declaration naming a transformation has a
// checked one.
func (codec *CodecDeclaration) transformValid(named bool) bool {
	return !named || codec.transform != nil
}

// codecComposable reports whether every witness a declaration composes,
// transitively, resolved: an unresolved entry has its own diagnostic, so the
// plan is not derived again to repeat it.
func (c *checker) codecComposable(codec *CodecDeclaration) bool {
	seen := map[*CodecDeclaration]bool{}
	var visit func(*CodecDeclaration) bool
	visit = func(witness *CodecDeclaration) bool {
		if seen[witness] {
			return true
		}
		seen[witness] = true
		if !witness.withSettled || !witness.transformValid(transformationNamed(witness)) {
			return false
		}
		for _, inner := range witness.with {
			if !visit(inner) {
				return false
			}
		}
		return true
	}
	return visit(codec)
}

// transformations lists the witnesses whose transformations a direction of
// codec runs: in plan step order when the plan was derived, otherwise in the
// declaration's own composition order. A witness appears once.
func (codec *CodecDeclaration) transformations() []*CodecDeclaration {
	if codec.plan != nil {
		return codec.plan.transforms
	}
	out := []*CodecDeclaration{}
	seen := map[*CodecDeclaration]bool{}
	var visit func(*CodecDeclaration)
	visit = func(witness *CodecDeclaration) {
		if seen[witness] {
			return
		}
		seen[witness] = true
		if witness.transform != nil {
			out = append(out, witness)
		}
		for _, reference := range witness.With {
			for _, inner := range witness.with {
				if inner.Name == reference.Name {
					visit(inner)
				}
			}
		}
	}
	visit(codec)
	return out
}

// transformationRows adds a transformation function's declared rows to a
// direction's rows. Names the function's own signature cannot resolve are
// diagnosed there, so they are not repeated on the witness.
func (c *checker) transformationRows(f *Function, errors, services []string) ([]string, []string) {
	for _, name := range f.Errors {
		if _, known := c.program.Errors[name]; known {
			errors = union(errors, []string{name})
		}
	}
	for _, name := range f.Services {
		if c.services[name] != nil {
			services = union(services, []string{name})
		}
	}
	return errors, services
}

// codecOptions resolves the explicit bounds of a declaration and collects
// its transformation options. Both bounds are required: a declaration
// missing either one is refused rather than given a default.
func (c *checker) codecOptions(codec *CodecDeclaration) (CodecPlanBounds, map[string]*CodecOption, bool) {
	bounds := CodecPlanBounds{}
	transformation := map[string]*CodecOption{}
	seen := map[string]bool{}
	ok := true
	for i := range codec.Options {
		option := &codec.Options[i]
		refuse := func(message string) {
			c.diagnostic(codecDerivationCode, "codec "+codec.Name+" option "+option.Name+" "+message, option.Span)
			ok = false
		}
		if seen[option.Name] {
			refuse("is repeated")
			continue
		}
		seen[option.Name] = true
		switch option.Name {
		case "maxBodyBytes", "maxDepth":
			if option.Function != "" {
				refuse("takes an integer bound")
				continue
			}
		case "decode", "encode":
			if option.Function == "" {
				refuse("names a transformation function")
				continue
			}
			transformation[option.Name] = option
			continue
		}
		switch option.Name {
		case "maxBodyBytes":
			if option.Value < 1 || option.Value > maxCodecMaxBodyBytes {
				refuse("must be between 1 and " + strconv.Itoa(maxCodecMaxBodyBytes))
				continue
			}
			bounds.MaxBodyBytes = int(option.Value)
		case "maxDepth":
			if option.Value < 1 || option.Value > rt.CodecMaxDepth {
				refuse("must be between 1 and " + strconv.Itoa(rt.CodecMaxDepth))
				continue
			}
			bounds.MaxDepth = int(option.Value)
		default:
			refuse("is unknown; supported options are maxBodyBytes, maxDepth, decode and encode")
		}
	}
	for _, name := range codecBoundNames {
		if !seen[name] {
			c.diagnostic(codecDerivationCode, "codec "+codec.Name+" requires the explicit bound "+name+"; codec bounds have no default", codec.Span)
			ok = false
		}
	}
	return bounds, transformation, ok
}

// codecTransformation checks a declaration's transformation options. Both
// directions are required; each names an ordinary first-order module
// function of one parameter: decode from the representation to the derived
// type, encode back to the same representation. A function's own signature
// diagnoses its unknown types, so a shape is compared only when known.
func (c *checker) codecTransformation(codec *CodecDeclaration, options map[string]*CodecOption) *codecTransform {
	decode, encode := options["decode"], options["encode"]
	if decode == nil && encode == nil {
		return nil
	}
	if decode == nil || encode == nil {
		present, missing := decode, "encode"
		if present == nil {
			present, missing = encode, "decode"
		}
		c.diagnostic(codecDerivationCode, "codec "+codec.Name+" option "+present.Name+" requires "+missing+": a transformation states both directions", present.Span)
		return nil
	}
	resolve := func(option *CodecOption) (*Function, TypeID, TypeID, bool) {
		f := c.functions[option.Function]
		if f == nil {
			c.diagnostic("EF102", "unknown function "+option.Function+" for codec "+codec.Name+" option "+option.Name, option.ValueSpan)
			return nil, 0, 0, false
		}
		if len(f.TypeParameters) > 0 || len(f.RowParameters) > 0 || len(f.Params) != 1 {
			c.diagnostic(codecDerivationCode, "codec "+codec.Name+" "+option.Name+" transformation "+f.Name+" must be a first-order function of exactly one parameter without type or row parameters", option.ValueSpan)
			return nil, 0, 0, false
		}
		if !c.typeKnown(f.Params[0].Type) || !c.typeKnown(f.Return) || c.requiresTemplateArguments(f.Params[0].Type) || c.requiresTemplateArguments(f.Return) {
			return nil, 0, 0, false
		}
		return f, c.canonicalRef(c.typeRef(f.Params[0].Type)), c.canonicalRef(c.typeRef(f.Return)), true
	}
	decodeFunction, representation, decoded, decodeOK := resolve(decode)
	encodeFunction, encoded, represented, encodeOK := resolve(encode)
	if !decodeOK || !encodeOK {
		return nil
	}
	ok := true
	if decoded != codec.domainID {
		c.diagnostic(codecDerivationCode, "codec "+codec.Name+" decode transformation "+decodeFunction.Name+" returns "+c.displayTypeID(decoded)+", not the derived type "+codec.Type, decode.ValueSpan)
		ok = false
	}
	if encoded != codec.domainID {
		c.diagnostic(codecDerivationCode, "codec "+codec.Name+" encode transformation "+encodeFunction.Name+" takes "+c.displayTypeID(encoded)+", not the derived type "+codec.Type, encode.ValueSpan)
		ok = false
	}
	if ok && represented != representation {
		c.diagnostic(codecDerivationCode, "codec "+codec.Name+" encode transformation "+encodeFunction.Name+" returns "+c.displayTypeID(represented)+", not the representation "+c.displayTypeID(representation)+" that "+decodeFunction.Name+" decodes", encode.ValueSpan)
		ok = false
	}
	if !ok {
		return nil
	}
	work := codecDomainLayoutWork
	refusal := c.codecValueLayout(codec.domainID, codec.Type, codec.Type, map[TypeID]bool{}, &work)
	if work < 0 {
		c.diagnostic("EF127", "codec "+codec.Name+" cannot transform "+codec.Type+": its layout exceeds the "+strconv.Itoa(codecDomainLayoutWork)+" work budget of a transformed domain", codec.TypeSpan)
		return nil
	}
	if refusal != "" {
		c.diagnostic(codecDerivationCode, "codec "+codec.Name+" cannot transform "+codec.Type+": "+refusal+"; a transformed witness decodes plain data, never a resource, function or runtime value", codec.TypeSpan)
		return nil
	}
	return &codecTransform{decode: decodeFunction, encode: encodeFunction, repID: representation, repDisplay: decodeFunction.Params[0].Type}
}

// codecValueLayout refuses a transformed witness's domain type unless every
// type its layout reaches is plain data: primitives, records, enums and
// generic data over them. The structural plan derives only the
// representation, so this is the domain's own admission. A decoded value is
// constructed inside the codec operation, which records no ownership or
// capture facts for it; a resource (File, Latch, Fiber), a function or
// recipe value, or a type variable could carry those facts, so admitting it
// would let a scope-owned value escape unchecked.
//
// The walk is memoized by type and charged one unit of work per type
// occurrence it visits, memoized or not, against the caller's budget, so it
// stops after codecDomainLayoutWork visits whatever the layout's width or
// depth; an exhausted budget leaves work negative (EF127, a finite-profile
// limit like the plan's own ceilings).
func (c *checker) codecValueLayout(id TypeID, display, path string, seen map[TypeID]bool, work *int) string {
	if *work--; *work < 0 {
		return ""
	}
	if seen[id] {
		return ""
	}
	seen[id] = true
	node := c.node(id)
	if node == nil {
		return "unresolved type at " + path
	}
	switch node.Kind {
	case "primitive":
		return ""
	case "record", "enum", "application":
		refusal := ""
		c.walkDataFields(id, func(name string, field Field) {
			if refusal == "" && *work >= 0 {
				refusal = c.codecValueLayout(field.typeID, field.Type, path+"."+name, seen, work)
			}
		})
		return refusal
	case "callable", "callable-shape", "recipe", "providerRecipe":
		return display + " at " + path + " is a function or effect recipe"
	default:
		return display + " at " + path + " is a host or runtime type"
	}
}

// codecWith resolves a declaration's `with` clause. Each entry names
// another transformed witness of a record or enum type; one witness per
// domain type, so selection is never ambiguous or implicit.
func (c *checker) codecWith(codec *CodecDeclaration, named map[string]*CodecDeclaration) {
	codec.with = map[TypeID]*CodecDeclaration{}
	codec.withSettled = true
	listed := map[string]bool{}
	names := []string{}
	for _, reference := range codec.With {
		refuse := func(message string) {
			c.diagnostic(codecDerivationCode, "codec "+codec.Name+" with "+reference.Name+": "+message, reference.Span)
			codec.withSettled = false
		}
		witness := named[reference.Name]
		switch {
		case witness == nil:
			c.diagnostic("EF102", "unknown codec witness "+reference.Name+" in codec "+codec.Name, reference.Span)
			codec.withSettled = false
		case listed[reference.Name]:
			refuse("is repeated")
		case witness == codec:
			refuse("a witness cannot compose itself")
		case !witness.headerChecked:
			// The witness has its own diagnostic.
			codec.withSettled = false
		case witness.derivation != codec.derivation:
			refuse("it derives " + witness.derivation.identity() + ", not " + codec.derivation.identity())
		case witness.transform == nil && transformationNamed(witness):
			codec.withSettled = false
		case witness.transform == nil:
			refuse("it has no transformation; with lists witnesses that transform their domain type")
		case !c.nominalData(witness.domainID):
			refuse("it transforms " + witness.Type + "; with admits witnesses of record and enum types")
		case codec.with[witness.domainID] != nil:
			refuse("codec " + codec.with[witness.domainID].Name + " already transforms " + witness.Type)
		default:
			codec.with[witness.domainID] = witness
			names = append(names, witness.Name)
		}
		listed[reference.Name] = true
	}
	slices.Sort(names)
	if len(names) > 0 {
		codec.withKey = strings.Join(names, ",")
	}
}

// nominalData reports whether id is a first-order record or enum.
func (c *checker) nominalData(id TypeID) bool {
	node := c.node(id)
	return node != nil && (node.Kind == "record" || node.Kind == "enum")
}

// codecFunction synthesizes one direction as an ordinary effect function.
// Its body is a single compiler-owned codec operation on its parameter, so
// the common checker derives its rows and every consumer sees an ordinary
// named function, value and call target.
//
// steps are the direction's transformation functions in plan step order,
// referenced as ordinary function values: the operation runs them, so the
// common checker contributes their rows and the application plan retains
// exactly the functions this direction executes.
func (c *checker) codecFunction(codec *CodecDeclaration, direction, parameter, parameterType string, parameterSource *sourceType, result string, resultSource *sourceType, errors, services []string, steps []*Expr) *Function {
	name := codec.Name + "." + direction
	parameterBinding := &localBinding{Kind: "parameter", Name: parameter}
	// Every node below is compiler-generated. Keep its source spans empty so
	// declaration inspection cannot mistake the derive name for a synthetic
	// parameter or body location; the origin is resolved through codec's
	// retained SyntaxItem by the shared type-query owner.
	operation := &Expr{Kind: "codec", Name: codec.Name, Text: direction, Left: &Expr{Kind: "name", Name: parameter, binding: parameterBinding}, Args: steps, codec: codec}
	f := &Function{
		Name:         name,
		Module:       currentModuleIdentity,
		SourceID:     "source:user",
		Owner:        "module",
		EmissionName: codec.Name + "_" + direction,
		Params:       []Param{{Name: parameter, Type: parameterType, sourceType: parameterSource, binding: parameterBinding}},
		Return:       result,
		returnType:   resultSource,
		Effect:       true,
		Errors:       errors,
		Services:     services,
		Body:         &Block{Statements: []*Statement{{Kind: "expr", Value: operation}}},
		Span:         codec.Span,
		DeclSpan:     codec.Span,
		codec:        codec,
	}
	f.Identity = c.declarationIdentity("function", f.Owner, name)
	return f
}

// codecOperation checks the body of a synthesized direction function. The
// operation executes now, inside its function's recipe: it yields the
// direction's result and contributes the direction's named failure plus the
// checked rows of each transformation it runs.
func (c *checker) codecOperation(e *Expr, env localEnv) checkedExpression {
	codec := e.codec
	c.expr(e.Left, env, false)
	if codec == nil || codec.derivation == nil {
		return c.checkedData("invalid")
	}
	result, failure := c.canonicalRef(typeRef(codec.derivation.Wire)), codec.derivation.EncodeFailure
	if e.Text == "decode" {
		result, failure = codec.domainID, codec.derivation.DecodeFailure
	}
	failures, services := []string{failure}, []string{}
	c.reasons = append(c.reasons, Contribution{"failure", []string{failure}, e.Span})
	for _, step := range e.Args {
		c.expr(step, env, false)
		f := step.ResolvedFunction
		if f == nil {
			continue
		}
		if labels := c.rowLabels(f.failureID); len(labels) > 0 {
			failures = union(failures, labels)
			c.reasons = append(c.reasons, Contribution{"failure", labels, e.Span})
		}
		if labels := c.rowLabels(f.serviceID); len(labels) > 0 {
			services = union(services, labels)
			c.reasons = append(c.reasons, Contribution{"requirement", labels, e.Span})
		}
	}
	t := c.checkedDataID(result, nil, nil)
	t.evaluation = c.evaluation(c.internRow(failures), c.internRow(services))
	return t
}

// CodecInspection is the public, target-independent view of one derive
// declaration: its witness name, domain and wire types, plan and the checked
// contract of each direction function. A transformed witness also reports
// the representation its plan derives structurally; With lists the
// witnesses it composes. A failure witness reports Failure and has no
// direction functions.
type CodecInspection struct {
	Name           string                    `json:"name"`
	Derivation     string                    `json:"derivation"`
	Profile        string                    `json:"profile"`
	Domain         string                    `json:"domain"`
	DomainType     string                    `json:"domainType"`
	Wire           string                    `json:"wire"`
	Representation string                    `json:"representation,omitempty"`
	With           []string                  `json:"with,omitempty"`
	Plan           string                    `json:"plan,omitempty"`
	Failure        bool                      `json:"failure,omitempty"`
	Decode         *CodecDirectionInspection `json:"decode,omitempty"`
	Encode         *CodecDirectionInspection `json:"encode,omitempty"`
	Span           Span                      `json:"span"`
}

// CodecDirectionInspection is the contract of one direction function.
// Transformations lists each transformation's witness once, in the
// direction's static plan order, with the first plan path at which it
// applies, its function and that function's own checked rows: the
// contribution paths of the direction's rows beyond its structural failure.
// Encoding orders a transformation before its representation's (preorder);
// decoding after them (postorder), so nested witnesses are listed in
// opposite orders. The order describes the plan, not one value's run: a
// value invokes only the witnesses on the union variants it holds.
type CodecDirectionInspection struct {
	Function        string                          `json:"function"`
	Failures        []string                        `json:"failures"`
	Requirements    []string                        `json:"requirements"`
	Transformations []CodecTransformationInspection `json:"transformations,omitempty"`
}

// CodecTransformationInspection is one transformation a direction runs.
type CodecTransformationInspection struct {
	Witness        string   `json:"witness"`
	Path           string   `json:"path"`
	Domain         string   `json:"domain"`
	Representation string   `json:"representation"`
	Function       string   `json:"function"`
	Failures       []string `json:"failures"`
	Requirements   []string `json:"requirements"`
}

// publishCodecs projects every checked derive declaration once the direction
// functions have been checked, reading their checked rows.
func (c *checker) publishCodecs() {
	for _, codec := range c.program.Codecs {
		if codec.derivation == nil || !codec.failure && (codec.decode == nil || codec.encode == nil) || codec.failure && codec.plan == nil {
			continue
		}
		direction := func(f *Function, decode bool) *CodecDirectionInspection {
			if f == nil {
				return nil
			}
			out := &CodecDirectionInspection{Function: f.Identity, Failures: append([]string{}, c.rowLabels(f.failureID)...), Requirements: append([]string{}, c.rowLabels(f.serviceID)...)}
			if codec.plan == nil {
				return out
			}
			order := codec.plan.encodeOrder()
			if decode {
				order = codec.plan.decodeOrder()
			}
			for _, step := range order {
				witness := codec.plan.transforms[step]
				transformation := witness.transform.encode
				if decode {
					transformation = witness.transform.decode
				}
				out.Transformations = append(out.Transformations, CodecTransformationInspection{
					Witness:        witness.Name,
					Path:           codec.plan.paths[step],
					Domain:         c.displayTypeID(witness.domainID),
					Representation: c.displayTypeID(witness.transform.repID),
					Function:       transformation.Identity,
					Failures:       append([]string{}, c.rowLabels(transformation.failureID)...),
					Requirements:   append([]string{}, c.rowLabels(transformation.serviceID)...),
				})
			}
			return out
		}
		inspection := CodecInspection{
			Name:       codec.Name,
			Derivation: codec.derivation.identity(),
			Profile:    codec.derivation.Profile,
			Domain:     c.displayTypeID(codec.domainID),
			DomainType: c.ref(codec.domainID).ID,
			Wire:       codec.derivation.Wire,
			Decode:     direction(codec.decode, true),
			Encode:     direction(codec.encode, false),
			Failure:    codec.failure,
			Span:       codec.Span,
		}
		if codec.transform != nil {
			inspection.Representation = c.displayTypeID(codec.transform.repID)
		}
		for _, reference := range codec.With {
			if slices.ContainsFunc(slices.Collect(maps.Values(codec.with)), func(w *CodecDeclaration) bool { return w.Name == reference.Name }) {
				inspection.With = append(inspection.With, reference.Name)
			}
		}
		if codec.plan != nil {
			inspection.Plan = codec.plan.ID
		}
		c.result.Codecs = append(c.result.Codecs, inspection)
	}
}
