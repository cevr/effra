package compiler

import (
	"strconv"

	rt "effra.local/prototype/runtime/effra"
)

// codecDerivationCode refuses a derive declaration whose checked type, bounds
// or options cannot be represented by its profile. No partial plan is kept.
const codecDerivationCode = "EF138"

// Default bounds of a derived plan. A derive declaration may lower either
// one explicitly, and maxBodyBytes may also be raised to its ceiling; the
// engines themselves never supply a default.
const (
	defaultCodecMaxBodyBytes = 1 << 20
	maxCodecMaxBodyBytes     = 1 << 30
)

// CodecDeclaration is one opt-in structural derivation:
//
//	derive userJson = Json.codec<User>(maxBodyBytes: 4096)
//
// It names an explicit witness. The checker synthesizes the ordinary effect
// functions userJson.decode and userJson.encode, whose signatures carry the
// direction contracts; several declarations may derive the same type.
type CodecDeclaration struct {
	Name           string
	Span           Span
	Alias          string
	Member         string
	DerivationSpan Span
	Type           string
	TypeSpan       Span
	Options        []CodecOption
	sourceType     *sourceType
	derivation     *codecDerivation
	domainID       TypeID
	plan           *CodecPlan
	decode         *Function
	encode         *Function
}

// CodecOption is one explicit plan bound of a derive declaration.
type CodecOption struct {
	Name  string
	Value int64
	Span  Span
}

func (p *parser) codecDeclaration() *CodecDeclaration {
	name := p.name()
	p.expect("=")
	alias := p.name()
	p.expect(".")
	member := p.memberName()
	codec := &CodecDeclaration{Name: name.text, Span: name.span, Alias: alias.text, Member: member.text, DerivationSpan: alias.span}
	codec.DerivationSpan.Length = member.span.Offset + member.span.Length - alias.span.Offset
	p.expect("<")
	codec.TypeSpan = p.peek().span
	codec.Type = p.typ()
	codec.sourceType = p.types[codec.Type]
	p.expect(">")
	if p.accept("(") {
		for !p.accept(")") {
			option := p.name()
			p.expect(":")
			value := p.take()
			if value.kind != "integer" {
				p.fail(value, "derive options take integer values")
			}
			number, _ := strconv.ParseInt(value.text, 10, 64)
			codec.Options = append(codec.Options, CodecOption{Name: option.text, Value: number, Span: option.span})
			if !p.accept(",") {
				p.expect(")")
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
var bundledFailures = map[string][]string{"effra/json": {jsonCodecDerivation.DecodeFailure, jsonCodecDerivation.EncodeFailure}}

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
	for _, imported := range c.program.BundledImports {
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
// resolved and before function signatures are checked.
func (c *checker) deriveCodecs(claim func(string, Span)) {
	c.program.DerivedBindings = map[string]map[string]*Function{}
	plans := map[string]*CodecPlan{}
	for _, codec := range c.program.Codecs {
		claim(codec.Name, codec.Span)
		if c.program.DerivedBindings[codec.Name] != nil {
			continue
		}
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
		if !c.typeKnown(codec.Type) {
			c.diagnostic("EF102", "unknown or unsupported value type "+codec.Type, codec.TypeSpan)
			continue
		}
		codec.domainID = c.canonicalRef(c.typeRef(codec.Type))
		c.bindSourceSyntax(codec.sourceType, codec.domainID)
		bounds, ok := c.codecBounds(codec)
		if ok {
			if plan, refusal := c.deriveCodecPlan(codec.derivation.Profile, codec.domainID, codec.Type, bounds); refusal != "" {
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
		codec.decode = c.codecFunction(codec, "decode", "input", codec.derivation.Wire, nil, codec.Type, codec.sourceType, codec.derivation.DecodeFailure)
		codec.encode = c.codecFunction(codec, "encode", "value", codec.Type, codec.sourceType, codec.derivation.Wire, nil, codec.derivation.EncodeFailure)
		c.program.DerivedBindings[codec.Name] = map[string]*Function{"decode": codec.decode, "encode": codec.encode}
		c.program.DerivedFunctions = append(c.program.DerivedFunctions, codec.decode, codec.encode)
	}
}

// codecBounds resolves the explicit or default bounds of a declaration.
func (c *checker) codecBounds(codec *CodecDeclaration) (CodecPlanBounds, bool) {
	bounds := CodecPlanBounds{MaxBodyBytes: defaultCodecMaxBodyBytes, MaxDepth: rt.CodecMaxDepth}
	seen := map[string]bool{}
	ok := true
	for _, option := range codec.Options {
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
			refuse("is unknown; supported options are maxBodyBytes and maxDepth")
		}
	}
	return bounds, ok
}

// codecFunction synthesizes one direction as an ordinary effect function.
// Its body is a single compiler-owned codec operation on its parameter, so
// the common checker derives its rows and every consumer sees an ordinary
// named function, value and call target.
func (c *checker) codecFunction(codec *CodecDeclaration, direction, parameter, parameterType string, parameterSource *sourceType, result string, resultSource *sourceType, failure string) *Function {
	name := codec.Name + "." + direction
	parameterBinding := &localBinding{Kind: "parameter", Name: parameter}
	operation := &Expr{Kind: "codec", Name: codec.Name, Text: direction, Left: &Expr{Kind: "name", Name: parameter, Span: codec.Span, binding: parameterBinding}, Span: codec.Span, codec: codec}
	f := &Function{
		Name:         name,
		Module:       currentModuleIdentity,
		SourceID:     "source:user",
		Owner:        "module",
		EmissionName: codec.Name + "_" + direction,
		Params:       []Param{{Name: parameter, Type: parameterType, sourceType: parameterSource, Span: codec.Span, binding: parameterBinding}},
		Return:       result,
		returnType:   resultSource,
		Effect:       true,
		Errors:       []string{failure},
		Body:         &Block{Statements: []*Statement{{Kind: "expr", Value: operation, Span: codec.Span}}},
		Span:         codec.Span,
		DeclSpan:     codec.Span,
		codec:        codec,
	}
	f.Identity = c.declarationIdentity("function", f.Owner, name)
	return f
}

// codecOperation checks the body of a synthesized direction function. The
// operation executes now, inside its function's recipe: it yields the
// direction's result and contributes only the direction's named failure.
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
	t := c.checkedDataID(result, nil, nil)
	t.evaluation = c.evaluation(c.internRow([]string{failure}), emptyRowID)
	c.reasons = append(c.reasons, Contribution{"failure", []string{failure}, e.Span})
	return t
}

// CodecInspection is the public, target-independent view of one derive
// declaration: its witness name, domain and wire types, plan and the checked
// contract of each direction function.
type CodecInspection struct {
	Name       string                   `json:"name"`
	Derivation string                   `json:"derivation"`
	Profile    string                   `json:"profile"`
	Domain     string                   `json:"domain"`
	DomainType string                   `json:"domainType"`
	Wire       string                   `json:"wire"`
	Plan       string                   `json:"plan,omitempty"`
	Decode     CodecDirectionInspection `json:"decode"`
	Encode     CodecDirectionInspection `json:"encode"`
	Span       Span                     `json:"span"`
}

// CodecDirectionInspection is the contract of one direction function.
type CodecDirectionInspection struct {
	Function     string   `json:"function"`
	Failures     []string `json:"failures"`
	Requirements []string `json:"requirements"`
}

// publishCodecs projects every checked derive declaration once the direction
// functions have been checked, reading their checked rows.
func (c *checker) publishCodecs() {
	for _, codec := range c.program.Codecs {
		if codec.decode == nil || codec.encode == nil || codec.derivation == nil {
			continue
		}
		direction := func(f *Function) CodecDirectionInspection {
			return CodecDirectionInspection{Function: f.Identity, Failures: append([]string{}, c.rowLabels(f.failureID)...), Requirements: append([]string{}, c.rowLabels(f.serviceID)...)}
		}
		inspection := CodecInspection{
			Name:       codec.Name,
			Derivation: codec.derivation.identity(),
			Profile:    codec.derivation.Profile,
			Domain:     c.displayTypeID(codec.domainID),
			DomainType: c.ref(codec.domainID).ID,
			Wire:       codec.derivation.Wire,
			Decode:     direction(codec.decode),
			Encode:     direction(codec.encode),
			Span:       codec.Span,
		}
		if codec.plan != nil {
			inspection.Plan = codec.plan.ID
		}
		c.result.Codecs = append(c.result.Codecs, inspection)
	}
}
