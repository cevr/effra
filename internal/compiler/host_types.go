package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	gotoken "go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Host types are the admitted Effra view of native Go declarations. go/types
// remains the identity authority: a host node's declaration is the
// package-path-qualified native type, and the checker never rebuilds a native
// type from its display spelling. Native values have no nil/null source value;
// a nullable native result adapts through bundled Option, and a nullable
// native parameter receives only a present value.

const (
	hostOptionModule   = "effra/data"
	hostOptionMember   = "Option"
	hostOptionTemplate = "template:" + hostOptionModule + ":module:" + hostOptionMember
)

// Native component adaptations reported by inspection.
const (
	hostAdaptDirect   = "direct"   // exact representation, never nil
	hostAdaptPresent  = "present"  // nullable native parameter; source supplies a present value
	hostAdaptOption   = "option"   // nullable native result; nil is None, any other value is Some
	hostAdaptError    = "error"    // trailing native error retained by GoResult
	hostAdaptContext  = "context"  // first context.Context forwarded from the managed fiber
	hostAdaptReceiver = "receiver" // method receiver, passed as the original native value
	hostAdaptBuffer   = "buffer"   // read buffer: source supplies its length, Go fills a fresh slice
	hostAdaptFilled   = "filled"   // read count: the filled prefix of the buffer, as fresh bytes
	hostAdaptWritten  = "written"  // write count: a short write with a nil error is io.ErrShortWrite
)

// HostComponent is one native parameter or result of a binding with its
// explicit adaptation. Native is the package-path-qualified go/types spelling;
// Type is the adapted Effra display.
type HostComponent struct {
	Native     string `json:"native"`
	Type       string `json:"type"`
	Adaptation string `json:"adaptation"`
}

// hostImports is the loaded native declaration universe of one program.
type hostImports struct {
	// aliases maps an imported package path to its first source alias, so a
	// displayed host type round-trips through a source annotation.
	aliases map[string]string
	// types holds the exported type declarations of directly imported
	// packages, keyed by their alias-qualified source spelling.
	types map[string]*types.TypeName
	// unsupported records why an exported function has no binding.
	unsupported map[string]string
	// contracts are the reviewed effra.bindings.json behavior assertions,
	// keyed by go/types full name: example.com/sdk.Lookup for a function,
	// (*example.com/sdk.Client).Lookup for a method.
	contracts map[string]behavior
}

// hostType is one admitted native type. native is normalized: aliases are
// removed at every level, so its go/types spelling names only declared types.
// primitive is the Effra primitive with exactly the native representation.
type hostType struct {
	native    types.Type
	primitive string
	nullable  bool
}

var hostErrorType = hostType{native: types.Universe.Lookup("error").Type(), nullable: true}

func (h *hostImports) unsupportedReason(symbol string) string {
	if h == nil {
		return ""
	}
	return h.unsupported[symbol]
}

func hostPathQualifier(p *types.Package) string { return p.Path() }

// admitHostType is the single admission rule for native parameter, result and
// annotation types. Unsupported forms return the reason they are refused.
func admitHostType(t types.Type) (hostType, error) {
	t = types.Unalias(t)
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.String:
			return hostType{native: t, primitive: "string"}, nil
		case types.Bool:
			return hostType{native: t, primitive: "bool"}, nil
		case types.Int64:
			return hostType{native: t, primitive: "i64"}, nil
		case types.Int, types.Uintptr:
			// Native-width scalars keep their own identity rather than becoming i64.
			return hostType{native: t}, nil
		}
		return hostType{}, fmt.Errorf("native scalar %s has no admitted Effra width (supported: string, bool, int64, int, uintptr)", t.Name())
	case *types.Named:
		obj := t.Obj()
		name := types.TypeString(t, hostPathQualifier)
		if t.TypeParams().Len() > 0 || t.TypeArgs().Len() > 0 {
			return hostType{}, fmt.Errorf("generic host type %s is unsupported", name)
		}
		if obj.Pkg() == nil {
			if obj.Name() == "error" {
				return hostErrorType, nil
			}
			return hostType{}, fmt.Errorf("unsupported host type %s", name)
		}
		if !obj.Exported() {
			return hostType{}, fmt.Errorf("unexported host type %s cannot be named by generated code", name)
		}
		if !hostPackageImportable(obj.Pkg()) {
			return hostType{}, fmt.Errorf("host type %s is declared in a package generated code cannot import", name)
		}
		return hostType{native: t, nullable: hostNullable(t.Underlying())}, nil
	case *types.Pointer:
		element, err := admitHostType(t.Elem())
		if err != nil {
			return hostType{}, err
		}
		return hostType{native: types.NewPointer(element.native), nullable: true}, nil
	case *types.Slice:
		if basic, ok := types.Unalias(t.Elem()).(*types.Basic); ok && basic.Kind() == types.Uint8 {
			// A present byte slice is exactly an Effra bytes value, but the
			// native slice can still be nil: it adapts like every other slice.
			return hostType{native: types.NewSlice(types.Typ[types.Uint8]), primitive: "bytes", nullable: true}, nil
		}
		element, err := admitHostType(t.Elem())
		if err != nil {
			return hostType{}, err
		}
		return hostType{native: types.NewSlice(element.native), nullable: true}, nil
	case *types.Map:
		key, err := admitHostType(t.Key())
		if err != nil {
			return hostType{}, err
		}
		element, err := admitHostType(t.Elem())
		if err != nil {
			return hostType{}, err
		}
		return hostType{native: types.NewMap(key.native, element.native), nullable: true}, nil
	case *types.Interface:
		// An unnamed interface (including any) is admitted when generated code
		// can spell every method it requires.
		if t.Empty() {
			// The empty interface is spelled any, so its display round-trips
			// into a source annotation.
			return hostType{native: hostUniverseAnnotations["any"], nullable: true}, nil
		}
		if !hostSpellable(t, map[types.Type]bool{}) {
			return hostType{}, fmt.Errorf("interface %s mentions a type generated code cannot name", types.TypeString(t, hostPathQualifier))
		}
		return hostType{native: t, nullable: true}, nil
	case *types.TypeParam:
		return hostType{}, fmt.Errorf("generic host type %s is unsupported", t.Obj().Name())
	}
	return hostType{}, fmt.Errorf("unsupported host type form %s", types.TypeString(t, hostPathQualifier))
}

// hostSpellable reports whether generated code outside the declaring
// packages can name a native type exactly.
func hostSpellable(t types.Type, visiting map[types.Type]bool) bool {
	t = types.Unalias(t)
	if visiting[t] {
		return true
	}
	visiting[t] = true
	switch t := t.(type) {
	case *types.Basic:
		return t.Kind() != types.UnsafePointer && t.Info()&types.IsUntyped == 0
	case *types.Named:
		obj := t.Obj()
		if obj.Pkg() == nil {
			return obj.Name() == "error"
		}
		return obj.Exported() && hostPackageImportable(obj.Pkg()) && t.TypeArgs().Len() == 0 && t.TypeParams().Len() == 0
	case *types.Pointer:
		return hostSpellable(t.Elem(), visiting)
	case *types.Slice:
		return hostSpellable(t.Elem(), visiting)
	case *types.Array:
		return hostSpellable(t.Elem(), visiting)
	case *types.Chan:
		return hostSpellable(t.Elem(), visiting)
	case *types.Map:
		return hostSpellable(t.Key(), visiting) && hostSpellable(t.Elem(), visiting)
	case *types.Signature:
		if t.TypeParams().Len() > 0 {
			return false
		}
		for _, tuple := range []*types.Tuple{t.Params(), t.Results()} {
			for i := 0; i < tuple.Len(); i++ {
				if !hostSpellable(tuple.At(i).Type(), visiting) {
					return false
				}
			}
		}
		return true
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			if !t.Field(i).Exported() || !hostSpellable(t.Field(i).Type(), visiting) {
				return false
			}
		}
		return true
	case *types.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			if !t.ExplicitMethod(i).Exported() || !hostSpellable(t.ExplicitMethod(i).Type(), visiting) {
				return false
			}
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			if !hostSpellable(t.EmbeddedType(i), visiting) {
				return false
			}
		}
		return true
	}
	return false
}

// hostNullable reports whether a native representation has a nil value.
func hostNullable(underlying types.Type) bool {
	switch underlying := underlying.(type) {
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		return true
	case *types.Basic:
		return underlying.Kind() == types.UnsafePointer
	}
	return false
}

// hostPackageImportable excludes packages the generated effra.generated module
// cannot import, so an admitted type always has a native spelling.
func hostPackageImportable(p *types.Package) bool {
	if p.Name() == "main" {
		return false
	}
	return !slices.ContainsFunc(strings.Split(p.Path(), "/"), func(segment string) bool {
		return segment == "internal" || segment == "vendor"
	})
}

func hostIdentity(t types.Type) string { return "go:" + types.TypeString(t, hostPathQualifier) }

func (h *hostImports) display(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if alias, ok := h.aliases[p.Path()]; ok {
			return alias
		}
		return p.Path()
	})
}

func (h *hostImports) adaptedDisplay(t hostType, result bool) string {
	display := t.primitive
	if display == "" {
		display = h.display(t.native)
	}
	if result && t.nullable {
		return hostOptionMember + "<" + display + ">"
	}
	return display
}

func (h *hostImports) component(t hostType, result bool) HostComponent {
	adaptation := hostAdaptDirect
	if t.nullable && result {
		adaptation = hostAdaptOption
	} else if t.nullable {
		adaptation = hostAdaptPresent
	}
	return HostComponent{Native: types.TypeString(t.native, hostPathQualifier), Type: h.adaptedDisplay(t, result), Adaptation: adaptation}
}

// hostState is the checker's canonical view of admitted native types.
type hostState struct {
	native      map[TypeID]types.Type
	annotations map[string]TypeID
	bindings    map[string]hostBindingTypes
}

// hostBindingTypes are the canonical types of one used binding. value is the
// complete non-error result: void, one component, or a goValues tuple.
type hostBindingTypes struct {
	receiver    TypeID
	params      []TypeID
	value       TypeID
	result      TypeID
	errorOption TypeID
}

func (c *checker) hostState() *hostState {
	if c.host == nil {
		c.host = &hostState{native: map[TypeID]types.Type{}, annotations: map[string]TypeID{}, bindings: map[string]hostBindingTypes{}}
	}
	return c.host
}

// hostTypeID interns one admitted native type. Composite host nodes carry
// their element nodes as arguments, so reachability and projection see the
// complete native graph.
func (c *checker) hostTypeID(t hostType) TypeID {
	if t.primitive != "" {
		return c.canonicalRef(typeRef(t.primitive))
	}
	var elements []types.Type
	switch native := t.native.(type) {
	case *types.Pointer:
		elements = []types.Type{native.Elem()}
	case *types.Slice:
		elements = []types.Type{native.Elem()}
	case *types.Map:
		elements = []types.Type{native.Key(), native.Elem()}
	}
	args := []TypeID{}
	for _, element := range elements {
		admitted, err := admitHostType(element)
		if err != nil {
			return invalidTypeID
		}
		args = append(args, c.hostTypeID(admitted))
	}
	id := c.internTypeWithDeclaration("host", c.program.host.display(t.native), args, hostIdentity(t.native))
	c.hostState().native[id] = t.native
	return id
}

// hostOption applies bundled Option to a present host value.
func (c *checker) hostOption(element TypeID) TypeID {
	template := c.templates[hostOptionTemplate]
	if template == nil || element == invalidTypeID {
		return invalidTypeID
	}
	id, err := c.templateApplication(template, []TypeID{element})
	if err != nil {
		return invalidTypeID
	}
	return id
}

// hostResultID adapts a native result component: a nullable value is Option.
func (c *checker) hostResultID(t hostType) TypeID {
	id := c.hostTypeID(t)
	if t.nullable {
		return c.hostOption(id)
	}
	return id
}

// hostBinding interns the canonical parameter and result types of a binding.
// It reports false when an adaptation is unavailable.
func (c *checker) hostBinding(b Binding) (hostBindingTypes, bool) {
	state := c.hostState()
	if known, ok := state.bindings[b.Identity]; ok {
		return known, true
	}
	var result hostBindingTypes
	valid := true
	if b.receiver != nil {
		result.receiver = c.hostTypeID(*b.receiver)
		valid = result.receiver != invalidTypeID
	}
	for _, param := range b.params {
		id := c.hostTypeID(param)
		valid = valid && id != invalidTypeID
		result.params = append(result.params, id)
	}
	components := []TypeID{}
	for _, component := range b.results {
		id := c.hostResultID(component)
		valid = valid && id != invalidTypeID
		components = append(components, id)
	}
	switch len(components) {
	case 0:
		result.value = c.canonicalRef(typeRef(voidTypeName))
	case 1:
		result.value = components[0]
	default:
		result.value = c.internType("goValues", "", components)
	}
	result.result = result.value
	if b.HasError {
		result.result = c.internType("goResult", "", []TypeID{result.value})
		result.errorOption = c.hostResultID(hostErrorType)
		valid = valid && result.errorOption != invalidTypeID
	}
	if valid {
		state.bindings[b.Identity] = result
	}
	return result, valid
}

// hostUniverseAnnotations are the predeclared Go types a Go-importing program
// can annotate by their Go names. Source records, enums and errors of the same
// name take precedence; a program without Go imports has none of them.
var hostUniverseAnnotations = map[string]types.Type{
	"error":   types.Universe.Lookup("error").Type(),
	"any":     types.Universe.Lookup("any").Type(),
	"int":     types.Typ[types.Int],
	"uintptr": types.Typ[types.Uintptr],
}

// hostAssignable applies Go's assignment rule at a native call boundary. An
// Effra value keeps its own identity rules everywhere else; only a value
// passed to Go may be assigned to a native interface its method set
// satisfies, which Go then converts implicitly without a wrapper.
func (c *checker) hostAssignable(actual, expected TypeID) bool {
	if c.assignable(actual, expected, 0) {
		return true
	}
	from, ok := c.hostNative(actual)
	if !ok {
		return false
	}
	to, ok := c.hostNative(expected)
	return ok && types.AssignableTo(from, to)
}

// hostSelection resolves a member of a host value. In callee position it
// selects a Go method on the executed receiver, which hostMethodCall checks;
// native fields and method values are not admitted. It reports whether inner
// is a host value.
func (c *checker) hostSelection(e *Expr, inner checkedExpression) (checkedExpression, bool) {
	if c.host == nil {
		return checkedExpression{}, false
	}
	if _, ok := c.host.native[inner.resultID()]; !ok {
		return checkedExpression{}, false
	}
	switch {
	case inner.isEffect():
		c.diagnostic("EF106", "Go method "+e.Name+" requires an executed receiver", e.Span)
	case c.hostCallee != e:
		c.diagnostic("EF106", "Go method "+e.Name+" must be called; native fields and method values are not admitted", e.Span)
	default:
		e.Text = "hostMethod"
		return inner, true
	}
	return c.checkedData("invalid"), true
}

// hostMethodCall checks a native method call on an executed host receiver.
// Go's method set is the authority. Effra values are not addressable, so a
// pointer-receiver method needs a pointer value: no copy is made to take an
// address, and a value-receiver method on a pointer is Go's own selection.
// Any receiver expression is evaluated once, when the recipe is constructed.
func (c *checker) hostMethodCall(e *Expr, receiver checkedExpression, env localEnv, inEffect bool) checkedExpression {
	b, reason := c.hostMethodBinding(c.host.native[receiver.valueID()], e.Left.Name)
	var host hostBindingTypes
	if reason == "" {
		var ok bool
		if host, ok = c.hostBinding(b); !ok {
			reason = "its signature requires bundled " + hostOptionModule + " " + hostOptionMember + " for absence adaptation"
		}
	}
	if reason != "" {
		c.diagnostic("EF112", "unsupported Go method "+e.Left.Name+": "+reason, e.Span)
		for _, arg := range e.Args {
			c.expr(arg, env, inEffect)
		}
		return c.checkedData("invalid")
	}
	c.checkForeignCall(e, b, host, env, inEffect)
	return e.checked
}

// hostMethodBinding admits one method in a native receiver's method set. The
// binding is keyed by the receiver's canonical type and the method name, which
// select exactly one native method; the receiver's displayed spelling names it
// for CLI/MCP inspection beside package functions, and two receivers can
// display alike.
func (c *checker) hostMethodBinding(receiver types.Type, name string) (Binding, string) {
	display := c.program.host.display(receiver)
	symbol := "(" + display + ")." + name
	identity := "go:(" + types.TypeString(receiver, hostPathQualifier) + ")." + name
	if b, ok := c.program.Bindings[identity]; ok {
		return b, ""
	}
	selection := types.NewMethodSet(receiver).Lookup(nil, name)
	if selection == nil {
		if !types.IsInterface(receiver) && types.NewMethodSet(types.NewPointer(receiver)).Lookup(nil, name) != nil {
			return Binding{}, "it has a pointer receiver and a " + display + " value is not addressable; call it on *" + display
		}
		return Binding{}, display + " has no method " + name
	}
	fn := selection.Obj().(*types.Func)
	if !fn.Exported() {
		return Binding{}, "unexported method " + name
	}
	admitted, err := admitHostType(receiver)
	if err != nil {
		return Binding{}, err.Error()
	}
	// A universe method such as error.Error has no package: its binding names
	// none and needs no import.
	b := Binding{Symbol: symbol, Identity: identity, receiver: &admitted}
	if fn.Pkg() != nil {
		b.Package = fn.Pkg().Path()
	}
	if reason := admitCallable(&b, fn, c.program.host); reason != "" {
		return Binding{}, reason
	}
	for _, protocol := range hostIOProtocols {
		if protocol.method == name && types.Implements(receiver, protocol.contract) {
			protocol.adapt(&b)
		}
	}
	receiverComponent := HostComponent{Native: types.TypeString(admitted.native, hostPathQualifier), Type: display, Adaptation: hostAdaptReceiver}
	b.HostParameters = append([]HostComponent{receiverComponent}, b.HostParameters...)
	c.program.Bindings[identity] = b
	return b, ""
}

// hostIOProtocol is one standard I/O method contract.
type hostIOProtocol struct {
	name, method string
	contract     *types.Interface
	reader       bool
}

// hostIOProtocols are identified by Go's interface satisfaction, as io.Copy
// identifies them: a receiver whose method set implements io.Reader follows
// io.Reader's documented contract for Read. Effra bytes are immutable, so a
// read takes the buffer length and Go fills a fresh native buffer whose filled
// prefix is the result; data returned with io.EOF or another error is kept
// beside that error. A write checks its count: fewer bytes with a nil error
// is reported as io.ErrShortWrite, as io.Copy reports it. A count outside the
// buffer is a defect.
var hostIOProtocols = []hostIOProtocol{
	{"io.Reader", "Read", hostIOContract("Read"), true},
	{"io.ReaderAt", "ReadAt", hostIOContract("ReadAt", types.Typ[types.Int64]), true},
	{"io.Writer", "Write", hostIOContract("Write"), false},
}

// hostIOContract builds the one-method interface method(p []byte, extra...)
// (n int, err error); interface satisfaction is structural, so it need not be
// loaded from package io.
func hostIOContract(method string, extra ...types.Type) *types.Interface {
	params := []*types.Var{types.NewParam(gotoken.NoPos, nil, "p", types.NewSlice(types.Typ[types.Byte]))}
	for _, t := range extra {
		params = append(params, types.NewParam(gotoken.NoPos, nil, "", t))
	}
	results := types.NewTuple(types.NewParam(gotoken.NoPos, nil, "n", types.Typ[types.Int]), types.NewParam(gotoken.NoPos, nil, "err", hostErrorType.native))
	signature := types.NewSignatureType(nil, nil, nil, types.NewTuple(params...), results, false)
	return types.NewInterfaceType([]*types.Func{types.NewFunc(gotoken.NoPos, nil, method, signature)}, nil).Complete()
}

// ioProtocol returns the I/O protocol a method binding follows.
func (b Binding) ioProtocol() (hostIOProtocol, bool) {
	index := slices.IndexFunc(hostIOProtocols, func(protocol hostIOProtocol) bool { return protocol.name == b.Protocol })
	if index < 0 {
		return hostIOProtocol{}, false
	}
	return hostIOProtocols[index], true
}

// adapt rewrites an admitted protocol method's buffer and count components.
func (protocol hostIOProtocol) adapt(b *Binding) {
	b.Protocol = protocol.name
	if !protocol.reader {
		b.HostResults[0].Adaptation = hostAdaptWritten
		return
	}
	b.params[0] = hostType{native: types.Typ[types.Int]}
	b.Params[0] = "int"
	b.HostParameters[0] = HostComponent{Native: b.HostParameters[0].Native, Type: "int", Adaptation: hostAdaptBuffer}
	b.results[0] = hostType{native: types.NewSlice(types.Typ[types.Uint8]), primitive: "bytes"}
	b.HostResults[0] = HostComponent{Native: b.HostResults[0].Native, Type: "bytes", Adaptation: hostAdaptFilled}
	b.Return = "bytes"
}

// hostAssert checks value.as<T>() on a native interface value. The result is
// Go's complete assertion outcome as a tuple: v0 is Option<T> and v1 the match
// status, so a matched typed-nil pointer (None, true) stays distinct from a
// failed match (None, false). An impossible assertion is refused, as in Go.
func (c *checker) hostAssert(e *Expr, env localEnv, inEffect bool) checkedExpression {
	left := c.expr(e.Left, env, inEffect)
	invalid := c.checkedData("invalid")
	var from types.Type
	if c.host != nil && !left.isEffect() {
		from = c.host.native[left.valueID()]
	}
	if from == nil || !types.IsInterface(from) {
		c.diagnostic("EF106", "as<"+e.Name+"> requires a native interface value", e.Span)
		return invalid
	}
	target := c.canonicalRef(typeRef(e.Name))
	to, ok := c.hostNative(target)
	if !ok {
		c.diagnostic("EF106", "as<"+e.Name+"> requires a native Go type", e.Span)
		return invalid
	}
	if !types.IsInterface(to) && !types.Implements(to, from.Underlying().(*types.Interface)) {
		c.diagnostic("EF106", "impossible assertion: "+e.Name+" does not implement "+c.displayTypeID(left.valueID()), e.Span)
		return invalid
	}
	option := c.hostOption(target)
	if option == invalidTypeID {
		c.diagnostic("EF112", "native assertion requires bundled "+hostOptionModule+" "+hostOptionMember, e.Span)
		return invalid
	}
	return c.checkedDataID(c.internType("goValues", "", []TypeID{option, c.canonicalRef(typeRef("bool"))}), nil, nil)
}

// hostConversion checks the explicit native integer conversions of a
// Go-importing program. i64(n) widens a native int and is total. int(x)
// narrows an i64 to Option<int>: None when the value does not fit the
// platform's int, since a failed narrowing has no partial value to keep.
func (c *checker) hostConversion(e *Expr, env localEnv, inEffect bool) (checkedExpression, bool) {
	if c.program == nil || c.program.host == nil || e.Left.Kind != "name" || (e.Left.Name != "i64" && e.Left.Name != "int") {
		return checkedExpression{}, false
	}
	if e.Left.binding != nil || c.namedFunction(e.Left.Name) != nil {
		return checkedExpression{}, false
	}
	e.Text = "hostConvert"
	nativeInt := c.hostAnnotation("int")
	if len(e.Args) != 1 {
		c.diagnostic("EF106", e.Left.Name+" conversion takes one argument", e.Span)
		for _, arg := range e.Args {
			c.expr(arg, env, inEffect)
		}
		return c.checkedData("invalid"), true
	}
	arg := c.expr(e.Args[0], env, inEffect)
	if e.Left.Name == "i64" {
		if arg.isEffect() || arg.valueID() != nativeInt {
			c.diagnostic("EF106", "i64 conversion requires a native int", e.Args[0].Span)
		}
		return c.checkedData("i64"), true
	}
	if arg.isEffect() || !c.sameType(arg, "i64") {
		c.diagnostic("EF106", "int conversion requires an i64", e.Args[0].Span)
	}
	return c.checkedDataID(c.hostOption(nativeInt), nil, nil), true
}

// hostAnnotation resolves an alias-qualified source annotation to an exported
// type of a directly imported package, or a predeclared Go type by name.
func (c *checker) hostAnnotation(name string) TypeID {
	if c.program == nil || c.program.host == nil {
		return invalidTypeID
	}
	if id, ok := c.hostState().annotations[name]; ok {
		return id
	}
	var native types.Type
	if declaration := c.program.host.types[name]; declaration != nil {
		native = declaration.Type()
	} else if universe, ok := hostUniverseAnnotations[name]; ok {
		native = universe
	} else {
		return invalidTypeID
	}
	admitted, err := admitHostType(native)
	if err != nil {
		return invalidTypeID
	}
	id := c.hostTypeID(admitted)
	if id != invalidTypeID {
		c.hostState().annotations[name] = id
	}
	return id
}

// hostNative returns the native Go type an admitted value has at a Go call
// boundary: a host node's own type, or the exact representation of an Effra
// primitive.
func (c *checker) hostNative(id TypeID) (types.Type, bool) {
	if c.host != nil {
		if native, ok := c.host.native[id]; ok {
			return native, true
		}
	}
	node := c.node(id)
	if node == nil || node.Kind != "primitive" {
		return nil, false
	}
	switch node.Name {
	case "string":
		return types.Typ[types.String], true
	case "bool":
		return types.Typ[types.Bool], true
	case "i64":
		return types.Typ[types.Int64], true
	case "bytes":
		return types.NewSlice(types.Typ[types.Uint8]), true
	}
	return nil, false
}

// sourceHostType admits a native pointer, slice or map annotation. Element
// spellings resolve through the ordinary annotation owner, and the composed
// native type passes the same admission rule as an imported signature.
func (c *checker) sourceHostType(t *sourceType) TypeID {
	if c.program == nil || c.program.host == nil || len(t.HostArguments) != len(t.HostArgumentTypes) {
		return invalidTypeID
	}
	elements := make([]types.Type, len(t.HostArguments))
	for i, name := range t.HostArguments {
		id := invalidTypeID
		if t.HostArgumentTypes[i] != nil {
			id = c.sourceCallable(t.HostArgumentTypes[i])
		} else {
			id = c.canonicalRef(typeRef(name))
		}
		native, ok := c.hostNative(id)
		if !ok {
			return invalidTypeID
		}
		elements[i] = native
	}
	var native types.Type
	switch t.HostForm {
	case "pointer":
		native = types.NewPointer(elements[0])
	case "slice":
		native = types.NewSlice(elements[0])
	case "map":
		if !types.Comparable(elements[0]) {
			return invalidTypeID
		}
		native = types.NewMap(elements[0], elements[1])
	default:
		return invalidTypeID
	}
	admitted, err := admitHostType(native)
	if err != nil {
		return invalidTypeID
	}
	id := c.hostTypeID(admitted)
	if id != invalidTypeID {
		t.owner, t.hostID = c, id
	}
	return id
}

// hostIntegerLiteral applies the checked literal rule for native-width
// integer parameters: a literal is admitted only within the range every Go
// platform gives the parameter type. It reports whether the rule applied.
func (c *checker) hostIntegerLiteral(arg *Expr, expected TypeID) bool {
	basic, ok := c.hostState().native[expected].(*types.Basic)
	if !ok || arg.Kind != "integer" {
		return false
	}
	value, err := strconv.ParseInt(arg.Text, 10, 64)
	minimum, maximum := int64(-1<<31), int64(1<<31-1)
	if basic.Kind() == types.Uintptr {
		minimum, maximum = 0, 1<<32-1
	}
	if err != nil || value < minimum || value > maximum {
		c.diagnostic("EF106", "integer literal "+arg.Text+" exceeds the portable range of Go "+basic.Name(), arg.Span)
	}
	return true
}

// goResultField types one field of an executed native call result. GoResult
// exposes the complete value, whether Go returned a non-nil error, and that
// error as Option<error>; a multi-value result exposes positional v0..vN.
func (c *checker) goResultField(inner checkedExpression, e *Expr) checkedExpression {
	invalid := c.checkedData("invalid")
	node := c.resultNode(inner)
	if node == nil || (node.Kind != "goResult" && node.Kind != "goValues") {
		c.diagnostic("EF106", "field access requires an executed GoResult", e.Span)
		return invalid
	}
	e.Text = "goField"
	if node.Kind == "goValues" {
		if index, ok := goValuesIndex(e.Name); ok && index < len(node.Args) {
			return c.checkedDataID(node.Args[index], nil, nil)
		}
		c.diagnostic("EF102", fmt.Sprintf("Go result values expose v0 through v%d", len(node.Args)-1), e.Span)
		return invalid
	}
	switch e.Name {
	case "value":
		if len(node.Args) == 1 {
			return c.checkedDataID(node.Args[0], nil, nil)
		}
	case "hasError":
		return c.checkedData("bool")
	case "error":
		if id := c.hostResultID(hostErrorType); id != invalidTypeID {
			return c.checkedDataID(id, nil, nil)
		}
		c.diagnostic("EF112", "native error adaptation requires bundled "+hostOptionModule+" "+hostOptionMember, e.Span)
		return invalid
	}
	c.diagnostic("EF102", "GoResult exposes value, hasError and error", e.Span)
	return invalid
}

func goValuesIndex(name string) (int, bool) {
	digits, ok := strings.CutPrefix(name, "v")
	if !ok || digits == "" || (len(digits) > 1 && digits[0] == '0') {
		return 0, false
	}
	index, err := strconv.Atoi(digits)
	return index, err == nil
}

// hostPackageAlias names an imported package in generated Go. Host type
// packages use their own aliases, independent of foreign-call imports.
func hostPackageAlias(path string) string {
	sum := sha256.Sum256([]byte(path))
	return "efGoType_" + hex.EncodeToString(sum[:6])
}

// hostAnnotationGoName is the generated alias of an alias-qualified source
// annotation; legacy source rendering reaches it through goType.
func hostAnnotationGoName(name string) string {
	alias, member, qualified := strings.Cut(name, ".")
	if !qualified {
		return "efType_" + goIdent(name)
	}
	return "efHostType_" + strconv.Itoa(len(alias)) + "_" + goIdent(alias) + "_" + goIdent(member)
}

func (c *checker) hostGoType(id TypeID) string {
	native, ok := c.hostState().native[id]
	if !ok {
		return "struct{}"
	}
	return types.TypeString(native, func(p *types.Package) string { return hostPackageAlias(p.Path()) })
}

// hostGoDeclarations returns the package imports and top-level declarations
// generated code needs to spell every interned host type. Each named host
// type is referenced once, so its package import is always used.
func (c *checker) hostGoDeclarations() ([]string, string) {
	if c == nil || c.host == nil {
		return nil, ""
	}
	named := c.hostNamedTypes()
	var out strings.Builder
	for _, key := range slices.Sorted(maps.Keys(named)) {
		out.WriteString("var _ *" + types.TypeString(named[key], func(p *types.Package) string { return hostPackageAlias(p.Path()) }) + "\n")
	}
	if c.hostWritesNeedIO() {
		out.WriteString("var _ = " + hostPackageAlias("io") + ".ErrShortWrite\n")
	}
	annotations := []string{}
	for name := range c.host.annotations {
		annotations = append(annotations, name)
	}
	slices.Sort(annotations)
	for _, name := range annotations {
		out.WriteString("type " + hostAnnotationGoName(name) + " = " + canonicalGoType(c, c.host.annotations[name], map[TypeID]bool{}) + "\n")
	}
	imports := []string{}
	for _, path := range c.hostDeclarationPackages() {
		imports = append(imports, hostPackageAlias(path)+" "+strconv.Quote(path))
	}
	return imports, out.String()
}

// hostNamedTypes is every named host type the generated declarations spell,
// by canonical spelling.
func (c *checker) hostNamedTypes() map[string]*types.Named {
	named := map[string]*types.Named{}
	for _, native := range c.host.native {
		hostSpelledNamed(native, map[types.Type]bool{}, func(declared *types.Named) {
			named[types.TypeString(declared, hostPathQualifier)] = declared
		})
	}
	return named
}

// hostWritesNeedIO reports whether a checked write reports a short write with
// io.ErrShortWrite.
func (c *checker) hostWritesNeedIO() bool {
	return slices.ContainsFunc(c.result.Bindings, func(b Binding) bool { protocol, ok := b.ioProtocol(); return ok && !protocol.reader })
}

// hostDeclarationPackages lists, sorted, the packages hostGoDeclarations
// imports under their host aliases. The application plan names the same
// packages through namedGoImport, so emission, retention and inspection read
// one decision.
func (c *checker) hostDeclarationPackages() []string {
	if c == nil || c.host == nil {
		return nil
	}
	paths := map[string]bool{}
	for _, declared := range c.hostNamedTypes() {
		paths[declared.Obj().Pkg().Path()] = true
	}
	if c.hostWritesNeedIO() {
		paths["io"] = true
	}
	return slices.Sorted(maps.Keys(paths))
}

// hostPackages lists the packages generated code imports to spell a host
// node's own native type: a named type's declaring package, or every package
// an unnamed interface's method signatures and embedded types mention. Pointer,
// slice and map elements are separate nodes reached through Args.
func (c *checker) hostPackages(id TypeID) []string {
	if c.host == nil {
		return nil
	}
	native := c.host.native[id]
	switch native.(type) {
	case nil, *types.Pointer, *types.Slice, *types.Map:
		return nil
	}
	paths := map[string]bool{}
	hostSpelledNamed(native, map[types.Type]bool{}, func(declared *types.Named) { paths[declared.Obj().Pkg().Path()] = true })
	return slices.Sorted(maps.Keys(paths))
}

// hostSpelledNamed visits every package-declared named type that spelling the
// native type t names. A named type is spelled by its own name; unnamed
// interfaces, signatures, structs and element types are spelled through their
// parts, so their packages are needed even when source never imports them.
func hostSpelledNamed(t types.Type, visiting map[types.Type]bool, visit func(*types.Named)) {
	t = types.Unalias(t)
	if visiting[t] {
		return
	}
	visiting[t] = true
	switch t := t.(type) {
	case *types.Named:
		if t.Obj().Pkg() != nil {
			visit(t)
		}
	case *types.Pointer:
		hostSpelledNamed(t.Elem(), visiting, visit)
	case *types.Slice:
		hostSpelledNamed(t.Elem(), visiting, visit)
	case *types.Array:
		hostSpelledNamed(t.Elem(), visiting, visit)
	case *types.Chan:
		hostSpelledNamed(t.Elem(), visiting, visit)
	case *types.Map:
		hostSpelledNamed(t.Key(), visiting, visit)
		hostSpelledNamed(t.Elem(), visiting, visit)
	case *types.Signature:
		for _, tuple := range []*types.Tuple{t.Params(), t.Results()} {
			for i := 0; i < tuple.Len(); i++ {
				hostSpelledNamed(tuple.At(i).Type(), visiting, visit)
			}
		}
	case *types.Struct:
		for i := 0; i < t.NumFields(); i++ {
			hostSpelledNamed(t.Field(i).Type(), visiting, visit)
		}
	case *types.Interface:
		for i := 0; i < t.NumExplicitMethods(); i++ {
			hostSpelledNamed(t.ExplicitMethod(i).Type(), visiting, visit)
		}
		for i := 0; i < t.NumEmbeddeds(); i++ {
			hostSpelledNamed(t.EmbeddedType(i), visiting, visit)
		}
	}
}

func canonicalGoValuesType(c *checker, node *semanticTypeNode, visiting map[TypeID]bool) string {
	fields := make([]string, len(node.Args))
	for index, arg := range node.Args {
		fields[index] = "V" + strconv.Itoa(index) + " " + canonicalGoType(c, arg, visiting)
	}
	return "struct{" + strings.Join(fields, "; ") + "}"
}

// foreign lowers one lazy imported call. The native call runs only when the
// returned effect executes under a Foreign provider; every native result is
// retained, and nullable results adapt through Option.
func (g *goEmitter) foreign(e *Expr, effect bool, ret string, out *strings.Builder) string {
	c := g.program.semantic
	b := g.bindings[e.Name]
	host := c.hostState().bindings[b.Identity]
	var callee string
	if b.receiver != nil {
		// The receiver is captured once with the arguments; the native method
		// runs on that original value when the recipe executes.
		receiver := g.temp()
		out.WriteString(receiver + " := " + g.expr(e.Left.Left, effect, ret, out) + "\n")
		callee = receiver + "." + b.member
	} else {
		// Canonical bindings are shared by every alias of one package, so a
		// function call names the alias written at this call.
		callee = "efGo_" + e.Left.Left.Name + "." + b.member
	}
	args := []string{}
	if b.Context {
		args = append(args, "fc.Context()")
	}
	for i, arg := range e.Args {
		var expr string
		if i >= len(host.params) {
			expr = g.expr(arg, effect, ret, out)
		} else if _, native := c.hostState().native[host.params[i]].(*types.Basic); native && arg.Kind == "integer" {
			expr = c.hostGoType(host.params[i]) + "(" + arg.Text + ")"
		} else {
			expr = g.expr(arg, effect, ret, out)
		}
		temp := g.temp()
		out.WriteString(temp + " := " + expr + "\n")
		args = append(args, temp)
	}
	result := g.resultType(e)
	var body strings.Builder
	// A protocol count refers to the native buffer passed as the first source
	// argument; a read fills a fresh buffer of the requested length.
	protocol, checked := b.ioProtocol()
	buffer := ""
	if checked {
		buffer = args[len(args)-len(e.Args)]
		if protocol.reader {
			body.WriteString("if " + buffer + "<0{return er.Die[" + result + "](fmt.Errorf(\"%s buffer length %d is negative\"," + strconv.Quote(b.Symbol) + "," + buffer + "))};efBuffer:=make([]byte," + buffer + ");")
			args[len(args)-len(e.Args)], buffer = "efBuffer", "efBuffer"
		}
	}
	call := callee + "(" + strings.Join(args, ",") + ")"
	components := []TypeID{}
	if node := c.node(host.value); node != nil && node.Kind == "goValues" {
		components = node.Args
	} else if !canonicalVoidType(c, host.value) {
		components = []TypeID{host.value}
	}
	names := []string{}
	for index := range components {
		names = append(names, "nativeR"+strconv.Itoa(index))
	}
	results := append([]string{}, names...)
	if b.HasError {
		results = append(results, "nativeErr")
	}
	if len(results) > 0 {
		body.WriteString(strings.Join(results, ",") + " := ")
	}
	body.WriteString(call + ";")
	if checked {
		body.WriteString("if nativeR0<0||nativeR0>len(" + buffer + "){return er.Die[" + result + "](fmt.Errorf(\"%s returned %d bytes for a %d-byte buffer\"," + strconv.Quote(b.Symbol) + ",nativeR0,len(" + buffer + ")))};")
		if protocol.reader {
			body.WriteString("efFilled:=efBuffer[:nativeR0:nativeR0];")
			names[0] = "efFilled"
		} else {
			body.WriteString("if nativeErr==nil&&nativeR0<len(" + buffer + "){nativeErr=" + hostPackageAlias("io") + ".ErrShortWrite};")
		}
	}
	value := "struct{}{}"
	switch len(components) {
	case 0:
	case 1:
		value = g.hostAdapt(components[0], names[0])
	default:
		fields := []string{}
		for index, component := range components {
			fields = append(fields, "V"+strconv.Itoa(index)+":"+g.hostAdapt(component, names[index]))
		}
		value = canonicalGoType(c, host.value, map[TypeID]bool{}) + "{" + strings.Join(fields, ",") + "}"
	}
	if b.HasError {
		value = canonicalGoType(c, host.result, map[TypeID]bool{}) + "{Value:" + value + ",Err:nativeErr}"
	}
	body.WriteString("return er.Succeed(" + value + ")")
	return "func(ctx efContext)efExit[" + result + "]{if ctx.s_Foreign==nil{return er.Die[" + result + "](fmt.Errorf(\"missing Foreign provider\"))};return er.Invoke(ctx.Runtime,func(fc *er.FiberContext)er.Exit[" + result + "]{" + body.String() + "})}"
}

// hostAdapt lowers one native result component to its checked adaptation.
func (g *goEmitter) hostAdapt(id TypeID, value string) string {
	node := g.program.semantic.node(id)
	if node == nil || node.Kind != "application" || node.Declaration != hostOptionTemplate || len(node.Args) != 1 {
		return value
	}
	return g.hostOptional(id, value)
}

// hostOptional adapts a nullable native value: nil is None and every other
// value, including an interface holding a nil dynamic pointer, is Some with
// the original native value.
func (g *goEmitter) hostOptional(optionID TypeID, value string) string {
	return g.hostOptionWhen(optionID, value, "efHostValue==nil")
}

// hostOptionWhen adapts value, bound as efHostValue, to None when the Go
// condition absent holds and to Some otherwise.
func (g *goEmitter) hostOptionWhen(optionID TypeID, value, absent string) string {
	c := g.program.semantic
	template := c.templates[hostOptionTemplate]
	option := canonicalGoType(c, optionID, map[TypeID]bool{})
	element := canonicalGoType(c, c.node(optionID).Args[0], map[TypeID]bool{})
	none := option + "(" + g.variantType(optionID, template, "None") + "{})"
	some := option + "(" + g.variantType(optionID, template, "Some") + "{" + goFieldName("value") + ":efHostValue})"
	return "func(efHostValue " + element + ") " + option + "{if " + absent + "{return " + none + "};return " + some + "}(" + value + ")"
}

// hostAssert lowers value.as<T>() to Go's comma-ok assertion. A nullable
// target is absent when the asserted value is nil, so a matched typed-nil
// pointer is (None, true); any other target is absent only on a failed match.
func (g *goEmitter) hostAssert(e *Expr, value string) string {
	c := g.program.semantic
	tupleID := e.checked.resultID()
	optionID := c.node(tupleID).Args[0]
	target := c.node(optionID).Args[0]
	native, _ := c.hostNative(target)
	absent := "!efHostOk"
	if hostNullable(native.Underlying()) {
		absent = "efHostValue==nil"
	}
	tuple := canonicalGoType(c, tupleID, map[TypeID]bool{})
	return "func() " + tuple + "{efHostAsserted, efHostOk := " + value + ".(" + canonicalGoType(c, target, map[TypeID]bool{}) + ");return " + tuple + "{V0:" + g.hostOptionWhen(optionID, "efHostAsserted", absent) + ",V1:efHostOk}}()"
}

// hostConversion lowers a checked native integer conversion. Narrowing
// round-trips through the platform int and is None when the value changed.
func (g *goEmitter) hostConversion(e *Expr, value string) string {
	if e.Left.Name == "i64" {
		return "int64(" + value + ")"
	}
	optionID := e.checked.resultID()
	option := canonicalGoType(g.program.semantic, optionID, map[TypeID]bool{})
	return "func(efHostWide int64) " + option + "{return " + g.hostOptionWhen(optionID, "int(efHostWide)", "int64(efHostValue)!=efHostWide") + "}(" + value + ")"
}

// goResultField lowers a checked native result field.
func (g *goEmitter) goResultField(e *Expr, left string) string {
	switch e.Name {
	case "value":
		return left + ".Value"
	case "hasError":
		return "(" + left + ".Err != nil)"
	case "error":
		return g.hostOptional(e.checked.resultID(), left+".Err")
	}
	return left + ".V" + strings.TrimPrefix(e.Name, "v")
}
