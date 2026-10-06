package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

type ValueType struct {
	Success  string   `json:"success"`
	Type     TypeRef  `json:"type"`
	Effect   bool     `json:"effect"`
	Errors   []string `json:"failures"`
	Services []string `json:"requirements"`
}

// TypeRef is the canonical semantic identity used by checking, emission and
// inspection. Success remains a stable rendered string for existing clients;
// TypeRef prevents the compiler from growing another string-encoded type
// grammar as nominal application data is added. Kind is intentionally open:
// provider, opaque-handle and ownership wrappers can be added without
// changing the public contract shape, with Args carrying nested identities.
type TypeRef struct {
	Kind string    `json:"kind"`
	Name string    `json:"name,omitempty"`
	Args []TypeRef `json:"args,omitempty"`
}

const SemanticSchemaVersion = 2

type Contribution struct {
	Kind  string   `json:"kind"`
	Names []string `json:"names"`
	Span  Span     `json:"span"`
}
type Symbol struct {
	Name          string         `json:"name"`
	Params        []Param        `json:"parameters"`
	Contract      ValueType      `json:"contract"`
	Actual        ValueType      `json:"bodyContract"`
	Span          Span           `json:"span"`
	Contributions []Contribution `json:"contributions"`
}
type Timings struct {
	ImportMicros int64 `json:"importMicros"`
	ParseMicros  int64 `json:"parseMicros"`
	CheckMicros  int64 `json:"checkMicros"`
	TotalMicros  int64 `json:"totalMicros"`
}
type Result struct {
	ModuleSum     []byte        `json:"-"`
	Bindings      []Binding     `json:"bindings,omitempty"`
	SchemaVersion int           `json:"schemaVersion"`
	Revision      string        `json:"revision"`
	Target        string        `json:"target"`
	Checked       bool          `json:"checked"`
	Diagnostics   []Diagnostic  `json:"diagnostics"`
	Symbols       []Symbol      `json:"symbols"`
	Declarations  []Declaration `json:"declarations,omitempty"`
	Timings       Timings       `json:"timings"`
	Program       *Program      `json:"-"`
}
type checker struct {
	program   *Program
	result    *Result
	functions map[string]*Function
	services  map[string]*Service
	providers map[string]*Provider
	records   map[string]*Record
	enums     map[string]*Enum
	errors    map[string]*ErrorDecl
	reasons   []Contribution
}

func value(success string) ValueType {
	return ValueType{Success: success, Type: typeRef(success), Errors: []string{}, Services: []string{}}
}
func typeRef(name string) TypeRef {
	switch {
	case name == "":
		return TypeRef{}
	case name == "string", name == "bool", name == "i64", name == "bytes", name == "()":
		return TypeRef{Kind: "primitive", Name: name}
	case name == "File", name == "Handler":
		return TypeRef{Kind: "opaque", Name: name}
	case strings.HasPrefix(name, "Fiber:"):
		return TypeRef{Kind: "fiber", Args: []TypeRef{typeRef(strings.TrimPrefix(name, "Fiber:"))}}
	case strings.HasPrefix(name, "GoResult:"):
		return TypeRef{Kind: "goResult", Args: []TypeRef{typeRef(strings.TrimPrefix(name, "GoResult:"))}}
	case strings.HasPrefix(name, "provider:"):
		return TypeRef{Kind: "provider", Name: strings.TrimPrefix(name, "provider:")}
	case name == "never":
		return TypeRef{Kind: "never"}
	case name == "invalid":
		return TypeRef{Kind: "invalid"}
	default:
		return TypeRef{Kind: "named", Name: name}
	}
}
func contract(f *Function) ValueType {
	v := value(f.Return)
	v.Effect = f.Effect
	v.Errors = normalized(f.Errors)
	v.Services = normalized(f.Services)
	return v
}
func normalized(names []string) []string {
	out := append([]string{}, names...)
	slices.Sort(out)
	return slices.Compact(out)
}
func union(a, b []string) []string { return normalized(append(append([]string{}, a...), b...)) }
func remove(a []string, n string) []string {
	out := []string{}
	for _, v := range a {
		if v != n {
			out = append(out, v)
		}
	}
	return out
}
func difference(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}
func Compile(source string) *Result { return CompileFor(source, "go") }
func CompileFor(source, target string) *Result {
	return CompileAt(source, target, ".")
}
func CompileAt(source, target, dir string) *Result {
	start := time.Now()
	hash := sha256.Sum256([]byte(source))
	r := &Result{SchemaVersion: SemanticSchemaVersion, Revision: hex.EncodeToString(hash[:]), Target: target, Diagnostics: []Diagnostic{}, Symbols: []Symbol{}}
	if target != "go" && target != "js" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF110", Message: "unsupported target " + target})
		return r
	}
	program, diagnostics := parse(source)
	r.Timings.ParseMicros = time.Since(start).Microseconds()
	if len(diagnostics) > 0 {
		r.Diagnostics = diagnostics
		r.Timings.TotalMicros = time.Since(start).Microseconds()
		return r
	}
	r.Program = program
	r.loadImports(dir)
	c := &checker{program: program, result: r, functions: map[string]*Function{}, services: map[string]*Service{}, providers: map[string]*Provider{}, records: map[string]*Record{}, enums: map[string]*Enum{}, errors: map[string]*ErrorDecl{}}
	checkStart := time.Now()
	c.check()
	r.Timings.CheckMicros = time.Since(checkStart).Microseconds()
	r.Timings.TotalMicros = time.Since(start).Microseconds()
	r.Checked = len(r.Diagnostics) == 0
	return r
}
func (c *checker) diagnostic(code, message string, span Span) {
	c.result.Diagnostics = append(c.result.Diagnostics, Diagnostic{code, message, span})
}
func (c *checker) check() {
	names := map[string]bool{}
	for _, s := range builtins() {
		c.services[s.Name] = s
		names[s.Name] = true
	}
	for _, p := range builtinProviders() {
		c.providers[p.Name] = p
		names[p.Name] = true
	}
	for _, name := range builtinErrors() {
		names[name] = true
		c.errors[name] = &ErrorDecl{Name: name}
	}
	claim := func(name string, span Span) {
		if names[name] {
			c.diagnostic("EF101", "duplicate declaration "+name, span)
		}
		names[name] = true
	}
	claimData := func(name string, span Span) {
		switch name {
		case "string", "bool", "i64", "bytes", "File", "Handler", "Fiber", "Context", "Effect", "Scope", "Exit", "Cause", "Option", "never", "invalid":
			c.diagnostic("EF101", "reserved data declaration "+name, span)
		}
		claim(name, span)
	}
	for _, imp := range c.program.Imports {
		claim(imp.Alias, imp.Span)
	}
	errors := make([]string, 0, len(c.program.Errors))
	for name := range c.program.Errors {
		errors = append(errors, name)
	}
	slices.Sort(errors)
	for _, name := range errors {
		claimData(name, c.program.Errors[name])
	}
	for _, name := range builtinErrors() {
		c.program.Errors[name] = Span{}
	}
	for _, decl := range c.program.ErrorDecls {
		if c.errors[decl.Name] == nil {
			c.errors[decl.Name] = decl
		}
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "error", Name: decl.Name, Fields: decl.Fields, Span: decl.Span})
	}
	for _, record := range c.program.Records {
		claimData(record.Name, record.Span)
		c.records[record.Name] = record
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "record", Name: record.Name, Fields: record.Fields, Span: record.Span})
	}
	for _, enum := range c.program.Enums {
		claimData(enum.Name, enum.Span)
		c.enums[enum.Name] = enum
		c.result.Declarations = append(c.result.Declarations, Declaration{Kind: "enum", Name: enum.Name, Variants: enum.Variants, Span: enum.Span})
	}
	for _, decl := range c.program.ErrorDecls {
		for i := range decl.Fields {
			decl.Fields[i].TypeRef = c.typeRef(decl.Fields[i].Type)
		}
	}
	for _, record := range c.program.Records {
		for i := range record.Fields {
			record.Fields[i].TypeRef = c.typeRef(record.Fields[i].Type)
		}
	}
	for _, enum := range c.program.Enums {
		for i := range enum.Variants {
			for j := range enum.Variants[i].Fields {
				enum.Variants[i].Fields[j].TypeRef = c.typeRef(enum.Variants[i].Fields[j].Type)
			}
		}
	}
	for _, decl := range c.program.ErrorDecls {
		c.validateFields(decl.Fields, decl.Name+"Error", true)
	}
	for _, record := range c.program.Records {
		c.validateFields(record.Fields, record.Name, false)
	}
	for _, enum := range c.program.Enums {
		variants := map[string]bool{}
		for _, variant := range enum.Variants {
			if variants[variant.Name] {
				c.diagnostic("EF101", "duplicate variant "+variant.Name+" in "+enum.Name, variant.Span)
			}
			variants[variant.Name] = true
			c.validateFields(variant.Fields, enum.Name+"."+variant.Name, true)
		}
	}
	c.validateDataLayouts()
	slices.SortStableFunc(c.result.Declarations, func(a, b Declaration) int {
		return a.Span.Offset - b.Span.Offset
	})
	for _, s := range c.program.Services {
		claim(s.Name, s.Span)
		c.services[s.Name] = s
	}
	for _, p := range c.program.Providers {
		claim(p.Name, p.Span)
		c.providers[p.Name] = p
	}
	for _, f := range c.program.Functions {
		claim(f.Name, f.Span)
		c.functions[f.Name] = f
	}
	for _, s := range c.program.Services {
		methods := map[string]bool{}
		for _, f := range s.Methods {
			if methods[f.Name] {
				c.diagnostic("EF101", "duplicate method "+f.Name, f.Span)
			}
			methods[f.Name] = true
			c.signature(f)
			if len(f.Services) > 0 {
				c.diagnostic("EF103", "service methods cannot declare uses in this prototype", f.Span)
			}
		}
	}
	for _, f := range c.program.Functions {
		c.signature(f)
	}
	for _, p := range c.program.Providers {
		s, exists := c.services[p.Service]
		if !exists {
			c.diagnostic("EF102", "unknown service "+p.Service, p.Span)
			continue
		}
		methods := map[string]*Function{}
		for _, f := range p.Methods {
			if methods[f.Name] != nil {
				c.diagnostic("EF101", "duplicate implementation method "+f.Name, f.Span)
			}
			methods[f.Name] = f
			c.signature(f)
			c.function(f, false)
			if len(f.Services) > 0 {
				c.diagnostic("EF103", "providers must be self-contained in this prototype", f.Span)
			}
		}
		for _, want := range s.Methods {
			got := methods[want.Name]
			if got == nil {
				c.diagnostic("EF104", "provider "+p.Name+" is missing method "+want.Name, p.Span)
				continue
			}
			equal := got.Effect && got.Return == want.Return && len(got.Params) == len(want.Params)
			if equal {
				for i := range got.Params {
					equal = equal && got.Params[i].Type == want.Params[i].Type
				}
			}
			if !equal || len(difference(got.Errors, want.Errors)) > 0 {
				c.diagnostic("EF104", "implementation does not satisfy service method "+s.Name+"."+want.Name, got.Span)
			}
		}
		for _, f := range p.Methods {
			found := false
			for _, want := range s.Methods {
				found = found || want.Name == f.Name
			}
			if !found {
				c.diagnostic("EF104", "unexpected implementation method "+f.Name, f.Span)
			}
		}
	}
	for _, f := range c.program.Functions {
		c.function(f, true)
	}
	c.validateJSDeclarationNames()
}

func (c *checker) validateJSDeclarationNames() {
	if c.result.Target != "js" {
		return
	}
	reserved := map[string]bool{
		"as": true, "asserts": true, "async": true, "await": true, "break": true,
		"case": true, "catch": true, "class": true, "const": true, "continue": true,
		"debugger": true, "default": true, "delete": true, "do": true, "else": true,
		"enum": true, "export": true, "extends": true, "finally": true, "for": true,
		"false": true, "from": true, "function": true, "get": true, "if": true, "implements": true,
		"import": true, "in": true, "infer": true, "instanceof": true, "interface": true,
		"is": true, "keyof": true, "let": true, "module": true, "namespace": true,
		"new": true, "null": true, "number": true, "object": true, "of": true, "package": true, "private": true, "protected": true,
		"public": true, "readonly": true, "return": true, "satisfies": true, "set": true,
		"static": true, "string": true, "super": true, "switch": true, "symbol": true, "throw": true, "this": true, "true": true, "try": true,
		"type": true, "typeof": true, "undefined": true, "unknown": true, "using": true, "var": true,
		"any": true, "bigint": true, "boolean": true, "never": true, "void": true, "while": true, "with": true, "yield": true,
	}
	validateIdentifier := func(name, owner string, span Span) {
		if reserved[name] {
			c.diagnostic("EF110", "JavaScript declaration name "+name+" is reserved by TypeScript for "+owner, span)
		}
	}
	occupied := map[string]string{}
	claim := func(name, owner string, span Span) {
		if previous, exists := occupied[name]; exists {
			c.diagnostic("EF110", "JavaScript declaration name "+name+" collides between "+previous+" and "+owner, span)
			return
		}
		occupied[name] = owner
	}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			validateIdentifier(declaration.Name+"Error", "error payload declaration", declaration.Span)
		} else {
			validateIdentifier(declaration.Name, "data declaration", declaration.Span)
		}
		claim(declaration.Name, "data declaration", declaration.Span)
	}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			claim(declaration.Name+"Error", "error payload declaration", declaration.Span)
		}
	}
	services := append(append([]*Service{}, builtins()...), c.program.Services...)
	for _, service := range services {
		validateIdentifier(service.Name, "service export", service.Span)
		claim(service.Name+"Requirement", "service requirement declaration", service.Span)
		claim(service.Name+"Provider", "service provider declaration", service.Span)
	}
	for _, provider := range c.program.Providers {
		validateIdentifier(provider.Name, "provider export", provider.Span)
	}
	for _, function := range c.program.Functions {
		validateIdentifier(function.Name, "function export", function.Span)
	}
}
func (c *checker) signature(f *Function) {
	valid := func(t string, span Span) {
		if !c.typeKnown(t) {
			c.diagnostic("EF102", "unknown or unsupported value type "+t, span)
		}
	}
	valid(f.Return, f.Span)
	names := map[string]bool{}
	for i := range f.Params {
		p := &f.Params[i]
		valid(p.Type, p.Span)
		p.TypeRef = c.typeRef(p.Type)
		if names[p.Name] {
			c.diagnostic("EF101", "duplicate parameter "+p.Name, p.Span)
		}
		names[p.Name] = true
	}
	for _, name := range normalized(f.Errors) {
		if _, exists := c.program.Errors[name]; !exists {
			c.diagnostic("EF102", "unknown failure "+name, f.Span)
		}
	}
	for _, name := range normalized(f.Services) {
		if _, exists := c.services[name]; !exists {
			c.diagnostic("EF102", "unknown service "+name, f.Span)
		}
	}
	if !f.Effect && (len(f.Errors) > 0 || len(f.Services) > 0) {
		c.diagnostic("EF103", "ordinary functions cannot declare effect rows", f.Span)
	}
}
func (c *checker) typeKnown(name string) bool {
	switch name {
	case "string", "bool", "()", "i64", "File", "bytes", "Handler":
		return true
	}
	if c.records[name] != nil || c.enums[name] != nil {
		return true
	}
	return false
}
func (c *checker) typeRef(name string) TypeRef {
	var canonical func(TypeRef) TypeRef
	canonical = func(ref TypeRef) TypeRef {
		for i := range ref.Args {
			ref.Args[i] = canonical(ref.Args[i])
		}
		if ref.Kind == "named" {
			if c.records[ref.Name] != nil {
				ref.Kind = "record"
			} else if c.enums[ref.Name] != nil {
				ref.Kind = "enum"
			} else if c.errors[ref.Name] != nil {
				ref.Kind = "error"
			}
		}
		return ref
	}
	return canonical(typeRef(name))
}
func (c *checker) validateFields(fields []Field, owner string, reserveTag bool) {
	names := map[string]bool{}
	for _, field := range fields {
		if names[field.Name] {
			c.diagnostic("EF101", "duplicate field "+field.Name+" in "+owner, field.Span)
		}
		names[field.Name] = true
		if field.Name == "_tag" && reserveTag {
			c.diagnostic("EF120", "_tag is reserved for closed variant/error discriminators", field.Span)
		}
		if !c.typeKnown(field.Type) {
			c.diagnostic("EF102", "unknown or unsupported field type "+field.Type, field.Span)
		}
	}
}
func (c *checker) validateDataLayouts() {
	state := map[string]int{}
	reported := map[string]bool{}
	var visit func(string, []string)
	visit = func(name string, path []string) {
		if state[name] == 1 {
			cycle := append(path, name)
			key := strings.Join(cycle, "->")
			if !reported[key] {
				reported[key] = true
				span := Span{}
				if record := c.records[name]; record != nil {
					span = record.Span
				} else if enum := c.enums[name]; enum != nil {
					span = enum.Span
				} else if failure := c.errors[name]; failure != nil {
					span = failure.Span
				}
				c.diagnostic("EF119", "recursive data layout is unsupported: "+key, span)
			}
			return
		}
		if state[name] == 2 {
			return
		}
		state[name] = 1
		fields := []Field{}
		if record := c.records[name]; record != nil {
			fields = record.Fields
		} else if enum := c.enums[name]; enum != nil {
			for _, variant := range enum.Variants {
				fields = append(fields, variant.Fields...)
			}
		} else if failure := c.errors[name]; failure != nil {
			fields = failure.Fields
		}
		for _, field := range fields {
			if c.records[field.Type] != nil || c.enums[field.Type] != nil || c.errors[field.Type] != nil {
				visit(field.Type, append(path, name))
			}
		}
		state[name] = 2
	}
	for _, record := range c.program.Records {
		visit(record.Name, nil)
	}
	for _, enum := range c.program.Enums {
		visit(enum.Name, nil)
	}
	for _, failure := range c.program.ErrorDecls {
		visit(failure.Name, nil)
	}
}
func (c *checker) function(f *Function, record bool) {
	env := map[string]ValueType{}
	for _, p := range f.Params {
		env[p.Name] = value(p.Type)
	}
	c.reasons = []Contribution{}
	actual := c.block(f.Body, env, f.Effect)
	if actual.Success != "never" && (actual.Success != f.Return || actual.Effect) {
		c.diagnostic("EF106", fmt.Sprintf("body returns %s; expected %s", display(actual), f.Return), f.Span)
	}
	if missing := difference(actual.Errors, f.Errors); len(missing) > 0 {
		c.diagnostic("EF107", "undeclared failures: "+strings.Join(missing, ", "), f.Span)
	}
	if missing := difference(actual.Services, f.Services); len(missing) > 0 {
		c.diagnostic("EF108", "missing service requirements: "+strings.Join(missing, ", "), f.Span)
	}
	actual.Effect = f.Effect
	actual.Type = c.typeRef(actual.Success)
	if record {
		declared := contract(f)
		declared.Type = c.typeRef(f.Return)
		c.result.Symbols = append(c.result.Symbols, Symbol{f.Name, f.Params, declared, actual, f.Span, append([]Contribution{}, c.reasons...)})
	}
}
func display(t ValueType) string {
	if t.Effect {
		return "Effect<" + t.Success + ", {" + strings.Join(t.Errors, ", ") + "}, {" + strings.Join(t.Services, ", ") + "}>"
	}
	return t.Success
}
func clone(env map[string]ValueType) map[string]ValueType {
	copy := map[string]ValueType{}
	for n, t := range env {
		copy[n] = t
	}
	return copy
}
func sameType(actual, expected string) bool {
	return actual == expected || actual == "never"
}
func fieldsFor(c *checker, typeName, variantName string) ([]Field, bool) {
	if variantName == "" {
		if record := c.records[typeName]; record != nil {
			return record.Fields, true
		}
		return nil, false
	}
	if enum := c.enums[typeName]; enum != nil {
		for _, variant := range enum.Variants {
			if variant.Name == variantName {
				return variant.Fields, true
			}
		}
	}
	return nil, false
}
func fieldsMap(fields []Field) map[string]Field {
	result := make(map[string]Field, len(fields))
	for _, field := range fields {
		result[field.Name] = field
	}
	return result
}
func sortedBindingNames(bindings map[string]string) []string {
	names := make([]string, 0, len(bindings))
	for name := range bindings {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
func (c *checker) payload(e *Expr, fields []Field, env map[string]ValueType, span Span) {
	declared := fieldsMap(fields)
	seen := map[string]bool{}
	for _, field := range e.Fields {
		want, exists := declared[field.Name]
		if !exists {
			c.diagnostic("EF114", "unknown payload field "+field.Name, field.Span)
			continue
		}
		if seen[field.Name] {
			c.diagnostic("EF114", "duplicate payload field "+field.Name, field.Span)
		}
		seen[field.Name] = true
		got := c.expr(field.Value, env, false)
		if got.Effect || !sameType(got.Success, want.Type) {
			c.diagnostic("EF115", "payload field "+field.Name+" must be "+want.Type, field.Span)
		}
	}
	for _, field := range fields {
		if !seen[field.Name] {
			c.diagnostic("EF114", "missing payload field "+field.Name, span)
		}
	}
}
func (c *checker) block(b *Block, env map[string]ValueType, effect bool) ValueType {
	out := value("()")
	env = clone(env)
	terminated := false
	for _, s := range b.Statements {
		if terminated {
			c.diagnostic("EF109", "unreachable statement after fail", s.Span)
		}
		if s.Kind == "fail" {
			if !effect {
				c.diagnostic("EF105", "fail is only valid inside effect functions", s.Span)
			}
			if _, exists := c.program.Errors[s.Name]; !exists {
				c.diagnostic("EF102", "unknown failure "+s.Name, s.Span)
			}
			if s.Payload != nil {
				if s.Payload.Kind == "payload" {
					fields := []Field(nil)
					if decl := c.errors[s.Name]; decl != nil {
						fields = decl.Fields
					}
					c.payload(s.Payload, fields, env, s.Payload.Span)
				} else {
					payload := c.expr(s.Payload, env, false)
					if payload.Effect {
						c.diagnostic("EF105", "failure payload must be pure", s.Payload.Span)
					}
					if decl := c.errors[s.Name]; decl != nil {
						if len(decl.Fields) != 1 || !sameType(payload.Success, decl.Fields[0].Type) {
							c.diagnostic("EF115", "failure payload for "+s.Name+" must match its declared fields", s.Payload.Span)
						} else {
							s.Payload = &Expr{Kind: "payload", Fields: []FieldValue{{Name: decl.Fields[0].Name, Value: s.Payload, Span: s.Payload.Span}}, Span: s.Payload.Span}
						}
					}
				}
			} else if decl := c.errors[s.Name]; decl != nil {
				// The shorthand `fail Error` and `fail Error()` still need to
				// satisfy every declared payload field.
				c.payload(&Expr{Kind: "payload", Span: s.Span}, decl.Fields, env, s.Span)
			}
			out.Errors = union(out.Errors, []string{s.Name})
			out.Success = "never"
			out.Effect = false
			terminated = true
			c.reasons = append(c.reasons, Contribution{"failure", []string{s.Name}, s.Span})
			continue
		}
		t := c.expr(s.Value, env, effect)
		out.Errors = union(out.Errors, tExecutedErrors(s.Value))
		out.Services = union(out.Services, tExecutedServices(s.Value))
		if s.Kind == "let" {
			if _, exists := env[s.Name]; exists {
				c.diagnostic("EF101", "duplicate local "+s.Name, s.Span)
			}
			env[s.Name] = t
			out.Success = "()"
			out.Effect = false
		} else {
			if t.Effect {
				c.diagnostic("EF105", "unused lazy effect; execute with run or bind it with let", s.Span)
			}
			out.Success = t.Success
			out.Effect = t.Effect
		}
	}
	return out
}

// Effect values carry deferred rows. Only run (and executed branch bodies) contribute to the enclosing computation.
func tExecutedErrors(e *Expr) []string   { return executed(e, true) }
func tExecutedServices(e *Expr) []string { return executed(e, false) }
func executed(e *Expr, errors bool) []string {
	if e == nil {
		return []string{}
	}
	row := func(t ValueType) []string {
		if errors {
			return t.Errors
		}
		return t.Services
	}
	if e.Kind == "run" || e.Kind == "if" || e.Kind == "match" || e.Kind == "scope" || e.Kind == "fork" {
		return row(e.Type)
	}
	out := union(executed(e.Left, errors), executed(e.Right, errors))
	for _, a := range e.Args {
		out = union(out, executed(a, errors))
	}
	return out
}
func (c *checker) expr(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	t := value("invalid")
	switch e.Kind {
	case "integer":
		t = value("i64")
	case "string":
		t = value("string")
	case "bool":
		t = value("bool")
	case "unit":
		t = value("()")
	case "name":
		if v, exists := env[e.Name]; exists {
			t = v
			e.Text = "local"
		} else if p, exists := c.providers[e.Name]; exists {
			t = value("provider:" + p.Service)
			if p.Service == "Files" || p.Service == "Runtime" || p.Service == "Foreign" || p.Service == "Http" {
				c.requireGo(e.Span, "native provider "+p.Service)
			}
			e.Text = "provider"
		} else if f := c.functions[e.Name]; f != nil && f.Effect && f.Return == "string" && len(f.Params) == 1 && f.Params[0].Type == "string" {
			t = contract(f)
			t.Success = "Handler"
			t.Effect = false
			e.Text = "handler"
		} else {
			c.diagnostic("EF102", "unknown value "+e.Name, e.Span)
		}
	case "call":
		if data, ok := c.dataCall(e, env, inEffect); ok {
			t = data
			break
		}
		if c.foreignCall(e, env, inEffect) {
			t = e.Type
			break
		}
		if c.fiberCall(e, env, inEffect) {
			t = e.Type
			break
		}
		var f *Function
		if e.Left.Kind == "name" {
			f = c.functions[e.Left.Name]
			if _, shadow := env[e.Left.Name]; shadow {
				c.diagnostic("EF103", "calling local values is not supported in this prototype", e.Span)
				f = nil
			}
		} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
			key := e.Left.Left.Name
			if key == "Files" || key == "Runtime" || key == "Http" {
				c.requireGo(e.Span, "native service "+key)
			}
			if _, shadow := env[key]; shadow {
				c.diagnostic("EF103", "a local shadows service "+key, e.Span)
			} else if s := c.services[key]; s != nil {
				for _, m := range s.Methods {
					if m.Name == e.Left.Name {
						f = m
						t.Services = []string{key}
						break
					}
				}
			}
		}
		if f == nil {
			c.diagnostic("EF102", "unknown function or service method", e.Span)
			for _, a := range e.Args {
				c.expr(a, env, inEffect)
			}
			break
		}
		services := t.Services
		t = contract(f)
		t.Services = union(t.Services, services)
		if len(e.Args) != len(f.Params) {
			c.diagnostic("EF106", "incorrect argument count", e.Span)
		}
		for i, a := range e.Args {
			arg := c.expr(a, env, inEffect)
			if e.Left.Kind == "member" && e.Left.Left.Kind == "name" && e.Left.Left.Name == "Http" && i == 1 && arg.Success == "Handler" {
				t.Services = union(t.Services, arg.Services)
			}
			if i < len(f.Params) && (arg.Effect || arg.Success != f.Params[i].Type) {
				c.diagnostic("EF106", "argument must be "+f.Params[i].Type, a.Span)
			}
		}
	case "member":
		inner := c.expr(e.Left, env, inEffect)
		if inner.Effect {
			c.diagnostic("EF106", "field access requires an executed value", e.Span)
			break
		}
		if fields, ok := fieldsFor(c, inner.Success, ""); ok {
			for _, field := range fields {
				if field.Name == e.Name {
					t = value(field.Type)
					e.Text = "field"
					break
				}
			}
			if t.Success != "invalid" {
				break
			}
			c.diagnostic("EF114", "unknown field "+e.Name+" on "+inner.Success, e.Span)
			break
		}
		if !strings.HasPrefix(inner.Success, "GoResult:") {
			c.diagnostic("EF106", "field access requires an executed GoResult", e.Span)
			break
		}
		switch e.Name {
		case "value":
			t = value(strings.TrimPrefix(inner.Success, "GoResult:"))
		case "hasError":
			t = value("bool")
		default:
			c.diagnostic("EF102", "GoResult exposes value and hasError", e.Span)
		}
	case "orFail":
		t = c.expr(e.Left, env, inEffect)
		if !t.Effect || !strings.HasPrefix(t.Success, "GoResult:") {
			c.diagnostic("EF106", "orFail requires an Effect returning GoResult", e.Span)
			break
		}
		t.Success = strings.TrimPrefix(t.Success, "GoResult:")
		t.Errors = union(t.Errors, []string{"GoError"})
	case "scope":
		if !inEffect {
			c.diagnostic("EF105", "scope requires an effect function", e.Span)
		}
		t = c.block(e.Then, env, inEffect)
	case "fork":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect || !inner.Effect {
			c.diagnostic("EF105", "fork requires an Effect inside an effect function", e.Span)
		}
		t = inner
		t.Success = "Fiber:" + inner.Success
		t.Effect = false
		t.Errors = union(t.Errors, executed(e.Left, true))
		t.Services = union(t.Services, executed(e.Left, false))
		c.reasons = append(c.reasons, Contribution{"owned-child", inner.Errors, e.Span})
	case "timeout":
		t = c.expr(e.Left, env, inEffect)
		duration := c.expr(e.Right, env, inEffect)
		if !t.Effect || duration.Effect || duration.Success != "i64" {
			c.diagnostic("EF106", "timeout requires an Effect and an i64 millisecond duration", e.Span)
		}
		t.Errors = union(t.Errors, []string{"Timeout"})
	case "run":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect {
			c.diagnostic("EF105", "run is only valid inside effect functions", e.Span)
		}
		if !inner.Effect {
			c.diagnostic("EF105", "run requires an Effect value", e.Span)
		}
		t = inner
		t.Effect = false
		t.Errors = union(t.Errors, executed(e.Left, true))
		t.Services = union(t.Services, executed(e.Left, false))
		if len(t.Errors) > 0 {
			c.reasons = append(c.reasons, Contribution{"failure", t.Errors, e.Span})
		}
		if len(t.Services) > 0 {
			c.reasons = append(c.reasons, Contribution{"requirement", t.Services, e.Span})
		}
	case "provide":
		t = c.expr(e.Left, env, inEffect)
		provider := c.expr(e.Right, env, inEffect)
		if !t.Effect {
			c.diagnostic("EF105", "provide requires an Effect value", e.Span)
		}
		if c.services[e.Name] == nil {
			c.diagnostic("EF102", "unknown service "+e.Name, e.Span)
		}
		if provider.Success != "provider:"+e.Name || provider.Effect {
			c.diagnostic("EF104", "provider must implement "+e.Name, e.Right.Span)
		}
		t.Services = remove(t.Services, e.Name)
	case "catch":
		t = c.expr(e.Left, env, inEffect)
		fallback := c.expr(e.Right, env, false)
		if !t.Effect {
			c.diagnostic("EF105", "catch requires an Effect value", e.Span)
		}
		if _, exists := c.program.Errors[e.Name]; !exists {
			c.diagnostic("EF102", "unknown failure "+e.Name, e.Span)
		} else if !slices.Contains(t.Errors, e.Name) {
			c.diagnostic("EF107", "effect does not admit failure "+e.Name, e.Span)
		}
		if fallback.Effect || fallback.Success != t.Success {
			c.diagnostic("EF106", "prototype catch fallback must be a pure "+t.Success, e.Right.Span)
		}
		t.Errors = remove(t.Errors, e.Name)
	case "construct":
		t = c.construct(e, env, inEffect)
	case "match":
		t = c.match(e, env, inEffect)
	case "binary":
		left, right := c.expr(e.Left, env, inEffect), c.expr(e.Right, env, inEffect)
		if left.Effect || right.Effect || left.Success != right.Success || (left.Success != "string" && left.Success != "bool" && left.Success != "i64") || (e.Name == "+" && left.Success != "string") {
			c.diagnostic("EF106", "operator requires matching primitive values; + accepts strings", e.Span)
		}
		t = value(left.Success)
		if e.Name == "==" {
			t.Success = "bool"
		}
	case "if":
		condition := c.expr(e.Left, env, inEffect)
		if condition.Effect || condition.Success != "bool" {
			c.diagnostic("EF106", "if condition must be bool", e.Left.Span)
		}
		a, b := c.block(e.Then, env, inEffect), c.block(e.Else, env, inEffect)
		if a.Success == "never" {
			t = b
		} else if b.Success == "never" {
			t = a
		} else {
			t = a
			if a.Success != b.Success || a.Effect != b.Effect {
				c.diagnostic("EF106", "if branches must return the same type", e.Span)
			}
		}
		if a.Effect || b.Effect {
			c.diagnostic("EF103", "returning Effect values from branches is not supported in this prototype", e.Span)
		}
		t.Errors = union(union(a.Errors, b.Errors), executed(e.Left, true))
		t.Services = union(union(a.Services, b.Services), executed(e.Left, false))
		t.Effect = false
	default:
		c.diagnostic("EF103", "unsupported expression "+e.Kind, e.Span)
	}
	t.Type = c.typeRef(t.Success)
	e.Type = t
	return t
}

func (c *checker) dataCall(e *Expr, env map[string]ValueType, inEffect bool) (ValueType, bool) {
	if e.Left == nil {
		return ValueType{}, false
	}
	typeName, variantName := "", ""
	switch e.Left.Kind {
	case "name":
		typeName = e.Left.Name
	case "member":
		if e.Left.Left.Kind != "name" {
			return ValueType{}, false
		}
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	default:
		return ValueType{}, false
	}
	if variantName == "" && c.errors[typeName] != nil {
		c.diagnostic("EF102", "error declarations are failure payloads, not success values", e.Span)
		return value("invalid"), true
	}
	fields, ok := fieldsFor(c, typeName, variantName)
	if !ok || (variantName == "" && c.enums[typeName] != nil) {
		if variantName != "" && c.enums[typeName] == nil {
			return ValueType{}, false
		}
		if variantName == "" && c.records[typeName] == nil {
			return ValueType{}, false
		}
	}
	if variantName != "" {
		enum := c.enums[typeName]
		if enum == nil {
			return ValueType{}, false
		}
		found := false
		for _, variant := range enum.Variants {
			found = found || variant.Name == variantName
		}
		if !found {
			c.diagnostic("EF116", "unknown variant "+typeName+"."+variantName, e.Span)
			return value("invalid"), true
		}
	}
	if len(e.Fields) > 0 {
		if len(e.Fields) != len(e.Args) {
			c.diagnostic("EF122", "constructor arguments cannot mix named and positional forms", e.Span)
			for _, arg := range e.Args {
				c.expr(arg, env, false)
			}
			e.Text = "data"
			return value("invalid"), true
		}
		payloadExpr := &Expr{Kind: "payload", Fields: e.Fields, Span: e.Span}
		c.payload(payloadExpr, fields, env, e.Span)
		e.Text = "data"
		return value(typeName), true
	}
	if len(e.Args) != len(fields) {
		c.diagnostic("EF115", "constructor "+typeName+" expects "+fmt.Sprint(len(fields))+" payload fields", e.Span)
	}
	for i, arg := range e.Args {
		got := c.expr(arg, env, false)
		if i < len(fields) && (got.Effect || !sameType(got.Success, fields[i].Type)) {
			c.diagnostic("EF115", "payload field "+fields[i].Name+" must be "+fields[i].Type, arg.Span)
		}
	}
	e.Text = "data"
	e.Fields = make([]FieldValue, 0, len(e.Args))
	for i, arg := range e.Args {
		if i < len(fields) {
			e.Fields = append(e.Fields, FieldValue{Name: fields[i].Name, Value: arg, Span: arg.Span})
		}
	}
	return value(typeName), true
}

func (c *checker) construct(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	if e.Left == nil {
		return value("invalid")
	}
	typeName, variantName := "", ""
	if e.Left.Kind == "name" {
		typeName = e.Left.Name
	} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
		typeName, variantName = e.Left.Left.Name, e.Left.Name
	} else {
		c.diagnostic("EF114", "invalid data constructor", e.Span)
		return value("invalid")
	}
	if variantName == "" && c.errors[typeName] != nil {
		c.diagnostic("EF102", "error declarations are failure payloads, not success values", e.Span)
		return value("invalid")
	}
	fields, ok := fieldsFor(c, typeName, variantName)
	if !ok {
		if variantName != "" && c.enums[typeName] != nil {
			c.diagnostic("EF116", "unknown variant "+typeName+"."+variantName, e.Span)
		} else {
			c.diagnostic("EF102", "unknown data declaration "+typeName, e.Span)
		}
		return value("invalid")
	}
	if variantName != "" {
		if c.enums[typeName] == nil {
			c.diagnostic("EF116", typeName+" is not a closed enum", e.Span)
			return value("invalid")
		}
	}
	c.payload(e, fields, env, e.Span)
	return value(typeName)
}

func (c *checker) match(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	scrutinee := c.expr(e.Left, env, inEffect)
	if scrutinee.Effect {
		c.diagnostic("EF106", "match scrutinee must be a value; execute an Effect with run", e.Left.Span)
	}
	enum := c.enums[scrutinee.Success]
	if enum == nil {
		c.diagnostic("EF116", "match requires a closed enum value", e.Left.Span)
		return value("invalid")
	}
	declared := map[string]Variant{}
	for _, variant := range enum.Variants {
		declared[variant.Name] = variant
	}
	seen := map[string]bool{}
	result := value("never")
	branchErrors := []string{}
	branchServices := []string{}
	haveResult := false
	for _, arm := range e.Arms {
		pattern := arm.Pattern
		if pattern.TypeName == "_" {
			c.diagnostic("EF118", "catch-all match arms cannot claim exhaustive closed interpretation", pattern.Span)
			continue
		}
		if pattern.TypeName != enum.Name {
			c.diagnostic("EF116", "match pattern belongs to "+pattern.TypeName+", expected "+enum.Name, pattern.Span)
			continue
		}
		if pattern.VariantName == "" {
			c.diagnostic("EF118", "match arm must name a declared variant", pattern.Span)
			continue
		}
		variant, exists := declared[pattern.VariantName]
		if !exists {
			c.diagnostic("EF116", "unknown variant "+enum.Name+"."+pattern.VariantName, pattern.Span)
			continue
		}
		if seen[pattern.VariantName] {
			c.diagnostic("EF117", "duplicate match arm for "+enum.Name+"."+pattern.VariantName, pattern.Span)
			continue
		}
		seen[pattern.VariantName] = true
		branchEnv := clone(env)
		fields := fieldsMap(variant.Fields)
		aliases := map[string]bool{}
		for _, fieldName := range sortedBindingNames(pattern.Bindings) {
			binding := pattern.Bindings[fieldName]
			if binding != "_" {
				if aliases[binding] {
					c.diagnostic("EF121", "duplicate pattern binding "+binding, pattern.Span)
					continue
				}
				aliases[binding] = true
			}
			field, ok := fields[fieldName]
			if !ok {
				c.diagnostic("EF114", "unknown payload field "+fieldName+" in match arm", pattern.Span)
				continue
			}
			if binding == "_" {
				continue
			}
			branchEnv[binding] = value(field.Type)
		}
		branch := c.block(arm.Body, branchEnv, inEffect)
		if branch.Success != "never" {
			if !haveResult {
				result, haveResult = branch, true
			} else if !sameType(result.Success, branch.Success) || result.Effect != branch.Effect {
				c.diagnostic("EF106", "match branches must return the same type", arm.Span)
			}
		}
		branchErrors = union(branchErrors, branch.Errors)
		branchServices = union(branchServices, branch.Services)
	}
	for _, variant := range enum.Variants {
		if !seen[variant.Name] {
			c.diagnostic("EF117", "missing match arm for "+enum.Name+"."+variant.Name, e.Span)
		}
	}
	if !haveResult {
		result = value("never")
	}
	result.Errors = union(branchErrors, tExecutedErrors(e.Left))
	result.Services = union(branchServices, tExecutedServices(e.Left))
	result.Effect = false
	return result
}
func (r *Result) Find(name string) *Symbol {
	for i := range r.Symbols {
		if r.Symbols[i].Name == name {
			return &r.Symbols[i]
		}
	}
	return nil
}
func (r *Result) FindDeclaration(name string) *Declaration {
	for i := range r.Declarations {
		if r.Declarations[i].Name == name {
			return &r.Declarations[i]
		}
	}
	return nil
}
func (r *Result) Entry() error {
	if !r.Checked {
		return fmt.Errorf("source has diagnostics")
	}
	main := r.Find("main")
	if main == nil {
		return fmt.Errorf("entry requires an effect fn main")
	}
	if !main.Contract.Effect || len(main.Params) > 0 {
		return fmt.Errorf("main must be an effect function with no parameters")
	}
	if len(main.Contract.Services) > 0 {
		return fmt.Errorf("main has unprovided services: %s", strings.Join(main.Contract.Services, ", "))
	}
	return nil
}

func (c *checker) fiberCall(e *Expr, env map[string]ValueType, inEffect bool) bool {
	if e.Left.Kind != "member" || e.Left.Left.Kind != "name" {
		return false
	}
	inner, exists := env[e.Left.Left.Name]
	if !exists || !strings.HasPrefix(inner.Success, "Fiber:") {
		return false
	}
	if len(e.Args) != 0 {
		c.diagnostic("EF106", "fiber operations take no arguments", e.Span)
	}
	t := value(strings.TrimPrefix(inner.Success, "Fiber:"))
	t.Effect = true
	t.Errors = inner.Errors
	switch e.Left.Name {
	case "join":
	case "interrupt":
		t.Success = "()"
	case "cancel":
		t.Success = "()"
		t.Errors = []string{}
	default:
		c.diagnostic("EF102", "unknown fiber operation "+e.Left.Name, e.Span)
	}
	e.Text = "fiber"
	e.Type = t
	return true
}

func (c *checker) requireGo(span Span, feature string) {
	c.program.GoOnly = true
	if c.result.Target != "go" {
		c.diagnostic("EF110", feature+" is currently implemented only for Go", span)
	}
}
